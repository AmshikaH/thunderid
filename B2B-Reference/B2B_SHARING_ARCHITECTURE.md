# ThunderID B2B Sharing — Architecture Document

Companion to [`B2B_SHARING_DESIGN.md`](B2B_SHARING_DESIGN.md), which covers *why*; this document
covers *how* — module layout, data model, and the concrete request flows, with diagrams. All
diagrams are Mermaid and render directly in GitHub and most Markdown viewers.

## 1. Module Overview

```mermaid
graph TB
    subgraph "Generic Framework"
        SH["internal/sharing<br/>(resource-type-agnostic)"]
    end

    subgraph "Concrete Resource Type"
        ROLE["internal/role"]
        ROLEDECL["role.resource_type_declaration.go<br/>implements sharing.ResourceTypeDeclaration<br/>+ sharing.SharingHooks"]
    end

    subgraph "OU Hierarchy"
        OU["internal/ou<br/>hierarchy_resolver.go"]
        SYSAUTHZ["internal/system/sysauthz<br/>OUHierarchyResolver interface"]
    end

    subgraph "Authorization"
        AUTHZENGINE["internal/authz/engine<br/>rbacengine.go"]
        SECURITY["internal/system/security"]
    end

    subgraph "OAuth Token Issuance"
        CCGRANT["oauth2/granthandlers<br/>client_credentials.go"]
        TXGRANT["oauth2/granthandlers<br/>token_exchange.go"]
        ACGRANT["oauth2/granthandlers<br/>authorization_code.go<br/>(NOT RBAC-filtered)"]
    end

    subgraph "Persistence"
        DB[("configdb<br/>SHARE_GRANT · SHARE_GRANT_EXCLUSION<br/>SHARE_GRANT_EDITABLE_FIELD · RESOURCE_OVERLAY<br/>ROLE_ASSIGNMENT")]
    end

    ROLE -->|registers| ROLEDECL
    ROLEDECL -->|RegisterResourceType| SH
    ROLE -->|Share/Unshare/IsShared/<br/>ResolveEditability/ResolveEditableFields| SH
    SH -->|GetAncestorOUIDs| SYSAUTHZ
    OU -.implements.-> SYSAUTHZ
    SH --> DB
    ROLE -->|ROLE_ASSIGNMENT read/write<br/>ASSIGNING_OU_ID| DB

    CCGRANT -->|EvaluateAccessBatch| AUTHZENGINE
    TXGRANT -->|EvaluateAccessBatch| AUTHZENGINE
    ACGRANT -.never calls.-> AUTHZENGINE
    AUTHZENGINE -->|GetAuthorizedPermissionsByResourceServer| ROLE

    style ACGRANT fill:#f8d7da,stroke:#c00
    style SH fill:#d4edda,stroke:#2a2
```

**Key structural point:** `internal/sharing` never imports `internal/role`. Role depends on
sharing, registers itself with a declaration, and optionally implements `SharingHooks` — the
dependency arrow only ever points one way. This is what makes the framework reusable for a future
second resource type without touching `internal/sharing` at all.

## 2. Data Model

```mermaid
erDiagram
    SHARE_GRANT ||--o{ SHARE_GRANT_EXCLUSION : "excludes OUs from"
    SHARE_GRANT ||--o{ SHARE_GRANT_EDITABLE_FIELD : "materializes editable fields for"
    SHARE_GRANT ||--o{ SHARE_GRANT : "PARENT_GRANT_ID (reshare lineage)"
    ROLE ||--o{ ROLE_PERMISSION : "has"
    ROLE ||--o{ ROLE_ASSIGNMENT : "has (per assigning OU)"

    SHARE_GRANT {
        varchar ID PK
        varchar RESOURCE_TYPE "e.g. role"
        varchar RESOURCE_ID "role.ID, no FK (generic)"
        varchar OWNING_OU_ID
        varchar SHARE_STAGE "share | reshare — WHO issued the grant<br/>(owning OU vs. a sharee), independent<br/>of which target-scope mode it used"
        varchar TARGET_SCOPE "all_roots | root | all_children | ou"
        varchar TARGET_OU_ID "null for all_roots"
        varchar PARENT_GRANT_ID "lineage: links to whichever grant<br/>governs the resharing OU's own access<br/>(share or an earlier reshare); empty<br/>when the resharing OU is the owner itself"
    }

    SHARE_GRANT_EXCLUSION {
        varchar GRANT_ID FK
        varchar EXCLUDED_OU_ID
    }

    SHARE_GRANT_EDITABLE_FIELD {
        varchar GRANT_ID FK
        varchar FIELD_KEY "one templated field editable through<br/>this grant; fixed at creation, immutable"
    }

    RESOURCE_OVERLAY {
        varchar RESOURCE_TYPE
        varchar RESOURCE_ID
        varchar OU_ID
        varchar FIELD_KEY
        jsonb VALUE "generic templated-field store (unused by role)"
    }

    ROLE {
        varchar ID PK
        varchar OU_ID "owning OU"
        varchar NAME
        varchar DESCRIPTION
    }

    ROLE_PERMISSION {
        varchar ROLE_ID FK
        varchar RESOURCE_SERVER_ID
        varchar PERMISSION
    }

    ROLE_ASSIGNMENT {
        varchar ROLE_ID FK
        varchar ASSIGNING_OU_ID "owner's own OU, or a sharee's"
        varchar ASSIGNEE_TYPE "entity | group"
        varchar ASSIGNEE_ID
    }
```

Three things worth calling out:

- `SHARE_GRANT.RESOURCE_ID` has **no foreign key** to `ROLE.ID` (or to any resource table) — this
  is what makes the table generic across resource types. Referential integrity for a specific
  resource type is that type's own responsibility (Role currently does not cascade-clean
  `SHARE_GRANT` rows on role deletion; see design doc's open items).
- `SHARE_GRANT_EDITABLE_FIELD` mirrors `SHARE_GRANT_EXCLUSION` exactly (one row per member,
  cascade-deleted with its grant) rather than a single delimited column, keeping the "is field X
  editable through this grant" check a plain membership lookup. There is deliberately no separate
  OU-wide or per-resource editability table any more — editability lives only on the grant that
  actually governs an organization unit's access (§5).
- `ROLE_ASSIGNMENT.ASSIGNING_OU_ID` is the one deliberate exception to "no per-resource-type
  sharing tables": it's Role's own pre-existing table, extended, not a new table invented for
  sharing.

## 3. Core Abstractions (`internal/sharing`)

```mermaid
classDiagram
    class ResourceTypeDeclaration {
        <<interface>>
        +ResourceType() ResourceType
        +TemplatedFields() []TemplatedFieldDeclaration
    }
    class SharingHooks {
        <<interface, optional>>
        +OnUnshare(ctx, resourceID, ouID) error
    }
    class TemplatedFieldDeclaration {
        +Key string
        +FallbackKey string
    }
    class ServiceInterface {
        <<interface>>
        +Share(...actingOUID, editableFields...) []ShareGrant
        +Unshare(grantID) error
        +ListShareGrants(...) []ShareGrant
        +ExportGrants(resourceType, resourceID) []ReplayableGrant
        +IsShared(...) bool
        +ListSharedResourceIDs(...) []string
        +ResolveEditability(...) bool
        +ResolveEditableFields(...) []string
        +RequireOwnership(ctx, resourceType, owningOUID) error
    }
    class ReplayableGrant {
        +ActingOUID string
        +Policy SharePolicy
    }
    class roleResourceTypeDeclaration {
        -store roleStoreInterface
        +ResourceType() "role"
        +TemplatedFields() "5 fields: assignments,~n assignments.user/group/app/agent"
        +OnUnshare(ctx, roleID, ouID) "DeleteAssignmentsByOUID"
    }

    ResourceTypeDeclaration <|.. roleResourceTypeDeclaration
    SharingHooks <|.. roleResourceTypeDeclaration
    ResourceTypeDeclaration "1" *-- "many" TemplatedFieldDeclaration
    ServiceInterface ..> ResourceTypeDeclaration : registry lookup
    ServiceInterface ..> ReplayableGrant : ExportGrants returns
```

`roleResourceTypeDeclaration` (`backend/internal/role/resource_type_declaration.go`) declares five
templated fields: the blanket `assignments`, plus `assignments.user`, `assignments.group`,
`assignments.app`, `assignments.agent`, each with `FallbackKey: "assignments"`. `OnUnshare` deletes
that OU's `ROLE_ASSIGNMENT` rows the moment its grant is revoked — a sharee never keeps a stale,
permanent permission after being unshared.

## 4. Sharing Model: One Action, Two Target-Scope Modes

`Share` (`POST /roles/{id}/share-grants`) is a single action with two target-scope modes selected
by which fields of the request are populated: root-targeting crosses trees, children-targeting
redistributes within one. A sharee using children-targeting mode to reshare further is not a
separate action or a looser check — it is this exact same call, gated by the exact same visibility
rule, made by an `ouId` other than the resource's own owner.

```mermaid
graph TD
    subgraph "Owner is a Root: single call, no root-targeting needed"
        OWNERROOT["Organization B<br/>(owns the role, is a Root)"]
        BCHILD1["B's Region 1"]
        BCHILD2["B's Region 2 (created AFTER the call)"]
        OWNERROOT -->|"Share (allChildren)<br/>owner shares directly:<br/>it is always trivially visible<br/>to its own resource"| BCHILD1
        OWNERROOT -.->|"dynamic: no new call needed"| BCHILD2
    end

    subgraph "Owner is not a Root: root-targeting crosses up, then children-targeting fans out"
        OWNERSUB["Sub-OU C<br/>(owns the role, not a Root)"]
        ROOTA["C's own Root, Organization A"]
        SIBLING["C's sibling OU"]
        OWNERSUB -->|"Share (root target = C's own Root)<br/>the only way to reach outside<br/>C's own subtree"| ROOTA
        ROOTA -->|"Share (allChildren)"| SIBLING
        ROOTA -.->|"Share (allChildren)<br/>reaches back to C too,<br/>shown as origin=owned there"| OWNERSUB
    end

    style BCHILD2 fill:#fff3cd,stroke:#856404
```

`allChildren` is **never snapshotted**. The grant row stores only the anchor OU; whether a given OU
is "in scope" is answered dynamically at read time.

### Chained Delegation: Explicit Targets Are Immediate-Children-Only

```mermaid
sequenceDiagram
    actor RootB as Organization B (Root)
    actor C1 as B's Region 1 (immediate child)
    actor GC1 as Region 1's Team A (grandchild of B)

    RootB->>+API: POST /roles/{id}/share-grants<br/>{ouId: B, ouIds: [Region1]}
    API-->>-RootB: 201 grant (share, ou, target=Region1)
    Note over RootB,C1: Region1 is B's direct child — allowed.<br/>Stage is "share": B is the role's own owner<br/>distributing directly, not resharing.

    Note over C1,GC1: B CANNOT share directly to Team A in the same call:<br/>ouIds may only name B's own immediate children.<br/>Team A must wait for Region1 to act.

    C1->>+API: POST /roles/{id}/share-grants<br/>{ouId: Region1, ouIds: [TeamA]}
    API-->>-C1: 201 grant (reshare, ou, target=TeamA,<br/>parentGrantId = Region1's own grant)
    Note over C1,GC1: Region1 delegates to its own child in its own,<br/>independent call — one hop at a time.
```

Each hop is a separate, independently-revocable grant, deliberately issued by the OU that
currently holds the resource — not something Organization B can shortcut in a single call.
`allChildren` has no such restriction: it is exempt because it already models the entire subtree,
any depth, as one relationship.

### Visibility Resolution — walk the chain, don't just pattern-match

```mermaid
sequenceDiagram
    participant Caller
    participant Service as sharing.Service
    participant OUResolver as sysauthz.OUHierarchyResolver
    participant Store as SHARE_GRANT (bounded fetch)

    Caller->>Service: IsShared(resourceType, resourceID, ouID)
    Service->>OUResolver: GetAncestorOUIDs(ouID)
    OUResolver-->>Service: [parent, grandparent, ..., root]

    Note over Service: One walk. Reversed into a top-down chain:
    Note over Service: [root, ..., parent, ouID]

    Service->>Store: fetch every grant for resourceID
    Note over Store: Small fetch: admin-managed, not per-OU
    Store-->>Service: []ShareGrant

    Note over Service: evaluateChainVisibility(chain, grants)
    Note over Service: Walk top-down. A position is covered if it is the owner, unconditionally.
    Note over Service: The root is covered with an incoming root-targeting grant.
    Note over Service: An earlier covered position can cover this position with an all_children grant,
    Note over Service: provided this position is not excluded.
    Note over Service: The immediately preceding position can explicitly share to this exact OU.
    Note over Service: A gap anywhere cuts off every position below it.

    Service-->>Caller: bool
```

Cost per call: **one ancestor walk + one bounded fetch** (every grant for one resource, or every
grant of a type touching the caller's chain — both small, admin-managed sets), evaluated with a
short in-memory loop — never a per-grant tree walk, regardless of tree depth/breadth or grant
count. Moving evaluation into Go (rather than an ever-growing SQL condition list) is also what made
multi-hop chain integrity directly unit-testable without a database.

**Why a flat match isn't enough once delegation chains exist:** the previous version of this
diagram matched a grant directly against the caller's resolved root — correct when every reshare
was issued by a Root in one hop, but not once an intermediate OU can reshare further (previous
section). A row naming a deep OU directly must not count if the hop from its real parent was never
granted; an exclusion partway down a chain must cut off every OU below it, even one reached via an
otherwise-valid-looking deeper grant. `evaluateChainVisibility` walks the chain precisely to catch
both cases — see the design doc §5.14.

### Cross-Tree Sharing Is Restricted by Default

Root-targeting is the only mode that crosses trees, and it has two distinct shapes depending on
whether the owner sharing the resource is itself a Root:

- **Owner is a Root** sharing to another Root (ordinary Root-to-Root distribution, e.g. `03 - Share
  to Root` in the demo collection) — always allowed, never restricted by anything in this section.
- **Owner is not a Root** ("a child OU") sharing via root-targeting — this is always at least a
  push **up**, and by default it may only reach **its own tree's Root**, the first step of the
  push-to-own-root-then-reshare pattern shown in the diagram above. Reaching a **foreign** tree's
  Root directly — skipping the "push up, then the Root reshares within its own tree" two-step
  entirely — is restricted unless the deployment's
  `resource_sharing.allow_child_ou_cross_tree_sharing` setting explicitly allows it. The rejection
  (`SHR-1009`) is deliberately generic to the caller: it does not reveal the target's relationship
  to the deployment's organization structure or the existence of this setting.

```mermaid
flowchart TD
    START(["Share, root-targeting mode<br/>(actingOUID == owningOUID)"]) --> ISROOT{"is owningOUID<br/>itself a Root?"}
    ISROOT -->|yes| ALLOW["allow — Root-to-Root,<br/>never restricted"]
    ISROOT -->|no| SAMETREE{"target Root ==<br/>owningOUID's own Root?"}
    SAMETREE -->|yes| ALLOW2["allow — pushing up<br/>within its own tree"]
    SAMETREE -->|no| CONFIG{"deployment.yaml:<br/>resource_sharing.allow_child_ou_cross_tree_sharing?"}
    CONFIG -->|true| ALLOW3["allow — cross-tree<br/>sharing explicitly enabled"]
    CONFIG -->|false, default| DENY["ErrorCrossTreeShareRestricted (SHR-1009, HTTP 400)"]

    style ALLOW fill:#d4edda
    style ALLOW2 fill:#d4edda
    style ALLOW3 fill:#d4edda
    style DENY fill:#f8d7da,stroke:#c00
```

`AllRoots` mode is treated as inherently cross-tree for a non-root owner — it reaches every Root in
the deployment, foreign trees included, by definition — so a non-root owner's `AllRoots` share is
rejected outright with the same error unless the setting is enabled; there is no partial "just the
roots you'd be allowed to reach anyway" fallback.

**Deployment setting** (`deployment.yaml`, generic — applies to every resource type onboarded onto
the sharing framework, not just Role): a single static boolean, read once at startup,

```yaml
resource_sharing:
  allow_child_ou_cross_tree_sharing: true
```

Unset (or `false`) is the default and means restricted, matching this framework's deny-by-default
posture elsewhere (e.g. reshare's scope-down-only rule, §5.15 of the design doc). This is
deliberately a **static** setting (`config.Config.ResourceSharing.AllowChildOUCrossTreeSharing`,
part of the same monolithic `deployment.yaml` as `tls.min_version` or `database`), not a
runtime-mutable `internal/serverconfig` section: it changes a deployment's security posture, so
changing it is meant to be a deliberate, restart-triggering act, not a live API call. It's threaded
straight into
`sharing.Initialize(...)` from `cmd/server/servicemanager.go` and stored on the service at
construction — no `ConfigReader`/registry plumbing needed, since `internal/sharing` already reads
`config.GetServerRuntime()` synchronously at startup, unlike the runtime store (`internal/csp`/
`internal/cors`'s own config sections), which is why those two packages need the more elaborate
package-level-reader pattern in the first place.

The check itself lives entirely in `internal/sharing/service.go`'s `shareToRoots` — resolved via
`resolveOwnRootOUID` (an owner's own Root is itself if it has no ancestors, else the topmost entry
of its ancestor chain) — so any future resource type onboarded onto the framework gets the same
restriction for free, with no resource-type-specific code required.

## 5. Editability Resolution

Editability is not a separately-managed policy consulted alongside a grant — it is a property
**of** the grant itself, fixed at the moment `Share` creates it (`ShareGrant.EditableFields`,
persisted in `SHARE_GRANT_EDITABLE_FIELD`). There is no "set editability" mutation endpoint at all;
changing what an organization unit may edit means revoking its grant and sharing again with a new
`editableFields` list.

```mermaid
flowchart TD
    START(["ResolveEditability(resourceType, resourceID,<br/>owningOUID, ouID, fieldKey)"]) --> LOOKUP{"fieldKey<br/>registered?"}
    LOOKUP -->|no| ERR["ErrorFieldNotTemplated"]
    LOOKUP -->|yes| OWNER{"ouID ==<br/>owningOUID?"}
    OWNER -->|yes| ALLOW["true — the owner always<br/>fully controls its own resource"]
    OWNER -->|no| GRANT["resolveNearestGrant(resourceType,<br/>resourceID, ouID)"]
    GRANT -->|not shared| DENY["false"]
    GRANT -->|grant found| MEMBER{"fieldKey or its FallbackKey<br/>in grant.EditableFields?"}
    MEMBER -->|yes| ALLOW2["true"]
    MEMBER -->|no| DENY2["false"]

    style ALLOW fill:#d4edda
    style ALLOW2 fill:#d4edda
    style DENY fill:#f8d7da,stroke:#c00
    style DENY2 fill:#f8d7da,stroke:#c00
```

`resolveNearestGrant` is the same chain-walk `IsShared` uses (§4), returning the single grant that
actually established `ouID`'s access — **the deepest/most specific one**, not merely the first
covering grant found, when more than one grant in the chain happens to reach that OU (e.g. a broad
`allChildren` grant from the owner and a narrower reshare from a closer intermediate OU both
technically reach the same descendant). Preferring the deepest grant is what makes scope-down
(below) actually binding for descendants, not just for the OU that was directly reshared to.

**Materializing `EditableFields` at `Share` time** (`resolveGrantEditableFields` in `service.go`):

| Who is sharing (`actingOUID`) | `editableFields` omitted/empty | `editableFields` given explicitly |
|---|---|---|
| The resource's own owning OU | Every declared templated field | Exactly that list (`SHR-1006` if a name isn't declared) |
| A previously-visible sharee (reshare) | Exactly `actingOUID`'s own current editable set | Must be a subset of that set (`SHR-1008` otherwise) |

The reshare row is the scope-down invariant the feature exists to guarantee: **a reshare can only
narrow editability relative to what the resharing organization unit itself holds, never widen it.**
Because grants are immutable and this check runs once, at creation, every grant's `EditableFields`
is transitively bounded by every grant above it in its delegation chain for the rest of its
lifetime — no drift is possible without an explicit unshare/reshare.

`ResolveEditableFields(resourceType, resourceID, owningOUID, ouID)` — the metadata read backing
`GET /roles/{id}/editable-fields` — runs the identical owner-shortcut / nearest-grant logic once,
then checks every declared field's membership instead of just one, returning the full editable set
in a single call.

## 6. Core Config Is Owner-Only

Editability resolution (§5) exists because templated fields are meant to be configurable — an
owner can choose to let a sharee edit them. Core config (a role's `name`, `description`,
`permissions`) has no such dial: it is **never** editable by a sharee, full stop, no matter how the
resource type or a future admin might want to configure it. That's a deliberate difference in
kind, not degree, so it isn't modeled as a resolvable policy the way `ResolveEditability` is —
it's a single hard invariant, enforced generically for every resource type by
`ServiceInterface.RequireOwnership`/`RequireOwnershipForDeletion(ctx, resourceType, owningOUID)`:

```mermaid
flowchart TD
    START(["RequireOwnership /<br/>RequireOwnershipForDeletion"]) --> ROOT{"caller holds root<br/>permission?"}
    ROOT -->|yes| ALLOW["allow"]
    ROOT -->|no| MATCH{"caller's own OU<br/>== owningOUID?"}
    MATCH -->|yes| ALLOW
    MATCH -->|no| WHICH{"which method?"}
    WHICH -->|"RequireOwnership<br/>(update)"| GENERIC["ErrorCoreConfigOwnerOnly (SHR-1007)"]
    WHICH -->|"RequireOwnershipForDeletion<br/>(delete)"| HOOK{"resource type implements<br/>DeletionOwnershipError?"}
    HOOK -->|yes| SPECIFIC["resource type's own error<br/>(Role: ROL-1024)"]
    HOOK -->|no| GENERIC

    style ALLOW fill:#d4edda
    style GENERIC fill:#f8d7da,stroke:#c00
    style SPECIFIC fill:#f8d7da,stroke:#c00
```

Two layers of control, deliberately redundant:

- **REST API layer.** `POST /roles`, `PUT /roles/{id}`, and `DELETE /roles/{id}` accept no
  acting-OU parameter at all — unlike `POST/GET /roles/{id}/assignments...`, which take `?ouId=` to
  select the acting (possibly sharee) OU. There is no field on these three requests a sharee could
  even populate to name itself as the acting party for core config; the only OU-shaped input on
  `POST`/`PUT` is `ouId`, and that names who will *own* the role, not who is acting on behalf of
  someone else.
- **Service layer.** `internal/role/service.go`'s `UpdateRoleWithPermissions` calls
  `RequireOwnership` against the *existing* role's `OUID` — and, when the update also moves the
  role to a different OU, against the *destination* `ouId` too, so a caller can't move a role it
  owns into an OU it doesn't own (or vice versa) — returning the generic `ErrorCoreConfigOwnerOnly`
  (SHR-1007) on mismatch. `DeleteRole` calls `RequireOwnershipForDeletion` instead, against the
  existing role's `OUID`, so a rejection surfaces the role-specific `ErrorRoleDeletionRestrictedToOwner`
  (ROL-1024) rather than SHR-1007's core-config-edit framing — deleting isn't editing. `CreateRole`
  does **not** call either — there is no existing role yet for either one's ownership framing to
  apply to, so the requested `ouId` is checked instead by `role.requireOwnOUScope` (ROL-1023), the
  same OU-reach check the read/list/assignment paths use (§7). All three are the authoritative
  check; the REST layer above is defense in depth, not a substitute for it.

Because `RequireOwnership`/`RequireOwnershipForDeletion` live on the generic `ServiceInterface`
rather than being hand-rolled in `internal/role`, any future resource type onboarded to the
sharing framework gets this guarantee for free, the same way it already gets `ResolveEditability`
and `IsShared` for free. `RequireOwnershipForDeletion`'s resource-specific error is itself an
optional capability (`DeletionOwnershipError`, checked via type assertion like `SharingHooks`) — a
resource type that doesn't implement it still gets the generic SHR-1007 on delete, so onboarding a
new resource type costs nothing extra unless it wants a more specific message.

## 7. API Access Scopes: `system` vs. `system:roles` vs. `system:roles:view`

Three OAuth scopes reach the Role Management API: `system` (the deployment's root permission —
unrestricted, as everywhere else in ThunderID), `system:roles` (full role-management access,
confined to the caller's own token-issued organization unit), and `system:roles:view` (read-only
access — list, read, and read assignments/share-grants/editable-fields — confined the same way).
This now matches the OU/User/Group/UserType/AgentType APIs' own view/manage split exactly: holding
`system:roles` also satisfies any `system:roles:view`-gated route (the same parent-scope rule
described below), so a full-access caller loses nothing.

```mermaid
flowchart TD
    REQ(["Request to any /roles* path"]) --> GATE{"global gate:<br/>system, system:roles,<br/>or system:roles:view?"}
    GATE -->|neither| REJECT403["403 (global gate)"]
    GATE -->|system| UNRESTRICTED["Unrestricted — acts on any OU"]
    GATE -->|system:roles| SCOPED["Full access,<br/>confined to the caller's own OU<br/>(security.GetOUID(ctx))"]
    GATE -->|system:roles:view only,<br/>write route| REJECT403
    GATE -->|system:roles:view,<br/>read route| SCOPEDVIEW["Read-only,<br/>confined to the caller's own OU"]

    style UNRESTRICTED fill:#d4edda
    style SCOPED fill:#cfe2ff
    style SCOPEDVIEW fill:#cfe2ff
    style REJECT403 fill:#f8d7da,stroke:#c00
```

**Global gate** (`internal/system/security/permissions.go`): read routes (`GET /roles`,
`GET /roles/**`) require `system:roles:view` in `apiPermissionEntries`; write routes (`POST`, `PUT`,
`DELETE`) require the full `system:roles`. `HasSufficientPermission`'s existing parent-scope rule (a
held permission satisfies any required permission it prefixes) means a caller holding `system`, or
`system:roles` on a read route, passes the gate unchanged.

**Service-layer enforcement**, once past the gate, uses the identical "root bypasses, else caller OU
must match" rule from §6, applied by three small functions rather than one, because the OU being
checked (and, for delete, the error returned) differs by endpoint shape:

| Action | OU checked | Enforced by |
|---|---|---|
| `POST /roles` | requested `ouId` | `role.requireOwnOUScope` — no existing role yet for `sharing.RequireOwnership`'s ownership framing to apply to |
| `PUT /roles/{id}` | existing (and, on move, destination) owning OU | `sharing.RequireOwnership` (§6) |
| `DELETE /roles/{id}` | owning OU | `sharing.RequireOwnershipForDeletion` (§6) — returns role-specific `ROL-1024`, not `SHR-1007` |
| `GET /roles/{id}` | owning OU, with a shared-to-caller's-own-OU fallback | `role.requireOwnOUScope` + `IsShared` |
| `GET /roles?ouId=` | the named `ouId` | `role.requireOwnOUScope` |
| `GET /roles` (no `ouId`) | n/a — see below | handler-level branch |
| `GET/POST /roles/{id}/assignments...` | acting `ouId` (empty = role's owner) | `role.requireOwnOUScope`, additive to `IsShared`/`ResolveEditability` |

`role.requireOwnOUScope` (`internal/role/authz.go`) is `internal/role`'s own, non-generic sibling of
`sharing.RequireOwnership`: same rule, but returns `ROL-1023` rather than `SHR-1007`, since these
checks aren't about core-config ownership specifically — they're about whether the caller's scope
itself reaches the named OU at all, a distinction that matters for `GET /roles/{id}`'s shared
fallback (viewing a shared role never requires *owning* it, only that the sharing model — §3-§5 —
actually grants visibility). It applies identically regardless of which of the two non-root role
scopes the caller holds — the view/manage split is decided entirely at the global gate above, not
inside `requireOwnOUScope`.

`GET /roles` with no `ouId` reflects the caller's scope automatically rather than erroring: a
`system` caller gets the unrestricted, deployment-wide listing (unchanged); a `system:roles` or
`system:roles:view` caller is routed to the same code path as `?ouId=<its own OU>` (tagged with
`origin`), decided in `HandleRoleListRequest` by checking `security.HasSystemPermission` before
choosing which service method to call.

## 8. Authorization / RBAC Integration

### 6.1 OU-scoped assignment storage

`ROLE_ASSIGNMENT.ASSIGNING_OU_ID` records which OU wrote a given assignment row (the role's own OU
for its own assignments, a sharee's OU for one it made under a grant). The authorization hot-path
query gains exactly one optional predicate:

```sql
... WHERE ra.ASSIGNEE_TYPE = ? AND ra.ASSIGNEE_ID IN (...)
    -- AND ra.ASSIGNING_OU_ID = ?   (only added when ouID is non-empty)
```

Omitted `ouID` preserves the exact prior, deployment-wide behavior — every pre-existing caller that
never adopted OU-scoping is unaffected.

### 6.2 Client Credentials grant → RBAC → shared-role permissions (verified working)

```mermaid
sequenceDiagram
    participant App as Client App<br/>(entity in Sharee OU)
    participant Token as POST /oauth2/token
    participant CCHandler as granthandlers/<br/>client_credentials.go
    participant RI as resourceindicators<br/>(DownscopeToResourceServer)
    participant AuthZ as authz/engine<br/>EvaluateAccessBatch
    participant RoleSvc as role.Service<br/>GetAuthorizedPermissionsByResourceServer
    participant DB as ROLE_ASSIGNMENT

    App->>Token: grant_type=client_credentials<br/>client_id, client_secret, scope, resource
    Token->>CCHandler: HandleGrant
    CCHandler->>RI: resolve target resource server + definitional scope filter
    CCHandler->>CCHandler: groupIDs = actorProvider.GetActorGroups(app.ID)
    CCHandler->>AuthZ: EvaluateAccessBatch(subject=app.ID, groupIDs, resourceServerID, scopes)
    AuthZ->>RoleSvc: GetAuthorizedPermissionsByResourceServer(entityID=app.ID, groupIDs, rsID, scopes, ouID="")
    RoleSvc->>DB: SELECT permission WHERE ASSIGNEE_ID=app.ID (unscoped — ouID empty in this call path)
    DB-->>RoleSvc: permissions from ANY assignment for this entity,<br/>including one written under a sharee OU's grant
    RoleSvc-->>AuthZ: authorized permissions
    AuthZ-->>CCHandler: filtered scopes
    CCHandler-->>Token: access_token with scope claim = authorized permissions
    Token-->>App: 200 { access_token, scope, ... }
```

This is the concrete proof that a role shared into an OU, and assigned to an entity there, is
honored by real token issuance — not just by the sharing API's own listing/detail endpoints. It was
validated against a running instance: an application entity created directly in a sharee child OU,
assigned to the shared role via that OU's own `POST /roles/{id}/assignments/add?ouId=`, receiving a
`client_credentials` token whose `scope` (and decoded JWT payload) contained the role's permissions.

`token_exchange` follows the identical pattern through the same `EvaluateAccessBatch` call.

### 6.3 The `authorization_code` gap (not fixed, documented)

```mermaid
sequenceDiagram
    participant User
    participant Token as POST /oauth2/token
    participant ACHandler as granthandlers/<br/>authorization_code.go
    participant RI as resourceindicators<br/>(DownscopeToResourceServer)

    User->>Token: grant_type=authorization_code, code, ...
    Token->>ACHandler: HandleGrant
    ACHandler->>RI: definitional scope filter only
    Note over ACHandler: No call to authzService.EvaluateAccessBatch.<br/>Token scope reflects resource-server-registered<br/>permissions, NOT the user's actual role assignments.
    ACHandler-->>Token: access_token with UNFILTERED scope
```

This applies identically whether the user's role is owned or shared — it is a gap in the
`authorization_code` handler generally, discovered while validating this feature, and left
unfixed by explicit decision (see design doc §5.11 and §6). Closing it would mean adding the same
`EvaluateAccessBatch` call `client_credentials`/`token_exchange` already make.

## 9. REST API Surface

| Method | Path | Kind | Notes |
|---|---|---|---|
| `GET` | `/roles` | Resource | `?ouId=` lists owned + shared, each tagged `origin` |
| `POST` | `/roles/{id}/share-grants` | Resource | Create a grant. `ouId` (optional, defaults to the role's owner) → root-targeting (`allRoots` \| `rootOuIds`, optional `excludedRootOuIds`, owner-only) or children-targeting (`allChildren` \| `ouIds` — `ouId`'s direct children only, optional `excludedOuIds`, any currently-visible `ouId`), plus optional `editableFields` (§5) |
| `GET` | `/roles/{id}/share-grants` | Resource | List every grant for a role |
| `DELETE` | `/roles/{id}/share-grants/{grantId}` | Resource | Revoke; cascades `OnUnshare` for affected OUs |
| `GET`/`POST` | `/roles/{id}/assignments...` | Resource | `?ouId=` selects the acting OU (empty = owner) |
| `GET` | `/roles/{id}/editable-fields` | Resource | `?ouId=` (optional, defaults to the role's owner) → templated fields currently editable by that OU (§5) |
| `POST` | `/import` | Resource | A role document may include `shareGrants` (replayed via `Share`, same shape as the share-grants request body) and `sharedAssignments` (replayed via `AddAssignments`, one group per sharee OU) — full round-trip of an exported role's sharing state (§12) |
| `GET` | `/organization-units/{id}/roles`, `/organization-units/tree/{path...}/roles` | Cross-package | Lists roles owned by or shared to the OU, each tagged `origin` — the OU package's own role-listing endpoints, reached through the `OURoleResolver` adapter (`internal/role/ou_resolver.go`) rather than a direct `ROLE` table query, so they can't drift out of sync with `GET /roles?ouId=`'s own sharing-aware behavior |

Creation, listing, and revocation of share grants are now a single, fully-RESTful sub-resource
collection at `/roles/{id}/share-grants` — there is no separate "action" endpoint for either
target-scope mode.

## 10. Caching Strategy

```mermaid
graph LR
    subgraph "Point-invalidated (direct delete on write)"
        C1["Canonical resource record"]
    end
    subgraph "Version-keyed (bump counter, let TTL evict stale keys)"
        C4["Resolved (resourceType, id, OU) view<br/>— can't enumerate every affected sharee OU on a single owner edit"]
    end
    subgraph "Cleared wholesale on any Share/Unshare (rare, admin-only)"
        C5["editabilityCache — editability is now<br/>derived from the grant graph, so a<br/>grant write can change it too"]
        C6["visibilityCache / visibleIDsCache"]
    end
```

`ResolveEditability`/`ResolveEditableFields` and `IsShared`/`ListSharedResourceIDs` all share one
wholesale-clear call (`clearVisibilityCaches`, despite the name — it clears `editabilityCache` too):
a single share/reshare/unshare changes the grant graph both visibility and editability are derived
from, and can affect an unbounded number of cached keys. These writes are rare and admin-only, so a
full clear is simpler and cheaper overall than tracking exact dependency sets.

## 11. End-to-End Walkthrough

```mermaid
sequenceDiagram
    actor OrgA as Organization A (Owner)
    actor OrgB as Organization B (Root, Sharee)
    actor OrgBRegion as B's Region OU (Sharee)
    participant API
    participant Token

    OrgA->>+API: POST /roles {name, permissions, ouId=A}
    API-->>-OrgA: 201 role created (core config: name + permissions)

    OrgA->>+API: POST /roles/{id}/share-grants {rootOuIds:[B]}
    API-->>-OrgA: 201 grant (stage=share, targetScope=root)

    OrgB->>+API: POST /roles/{id}/share-grants {ouId:B, allChildren:true, editableFields:["assignments.app"]}
    API-->>-OrgB: 201 grant (stage=reshare, targetScope=all_children)
    Note over API: editableFields=["assignments.app"] is narrower than B's own set.
    Note over API: Wider permissions would be rejected with SHR-1008.

    OrgBRegion->>+API: GET /roles?ouId=BRegion
    API-->>-OrgBRegion: role listed, origin=shared
    Note over OrgBRegion: Listed alongside any BRegion own roles with origin=owned.

    OrgBRegion->>+API: GET /roles/{id}/editable-fields?ouId=BRegion
    API-->>-OrgBRegion: 200 {fields:["assignments.app"]}

    OrgBRegion->>+API: POST /applications {ouId:BRegion}
    API-->>-OrgBRegion: 201 app (clientId, clientSecret)

    OrgBRegion->>+API: POST /roles/{id}/assignments/add?ouId=BRegion {type:"app", id:appId}
    API-->>-OrgBRegion: 204 editability check passed
    Note over API: ROLE_ASSIGNMENT row written with ASSIGNING_OU_ID=BRegion.

    OrgBRegion->>+Token: POST /oauth2/token grant_type=client_credentials client_id, client_secret, scope, resource
    Token-->>-OrgBRegion: 200 access_token
    Note over OrgBRegion: scope includes the role's permissions.
```

Every step above corresponds to a request in
`samples/api/postman/role-sharing/role_sharing_demo.json`, which exercises this exact flow (plus
exclusion policies, grant-scoped editability with scope-down-only reshare, and negative/
authorization cases) against a live ThunderID instance.

## 12. Export/Import and Declarative-YAML Share Grant Round-Tripping

Before this, a role's declarative export (`roleExporter.GetResourceByID`,
`backend/internal/role/declarative_resource.go`) captured core config, permissions, and the owning
OU's own assignments — but not the role's share grants, nor any sharee OU's independent assignment
set. Re-importing a shared role silently dropped its entire sharing state. Declarative (file-store)
roles, meanwhile, had no way to declare a share grant in YAML at all.

### Generic `ExportGrants`

`sharing.ServiceInterface.ExportGrants(ctx, resourceType, resourceID) ([]ReplayableGrant,
*tidcommon.ServiceError)` (`backend/internal/sharing/service.go`) is the sole mechanism for
reproducing a resource's sharing state elsewhere:

- It fetches every `ShareGrant` for the resource (`ListShareGrants`) and topologically orders them
  via `orderGrantsByDependency`, so a reshare always sorts after the grant that made its issuing OU
  visible — the order a caller must replay them in through `Share()` for a multi-hop chain to stay
  valid at each step.
- Each ordered `ShareGrant` converts back to a `ReplayableGrant{ActingOUID, Policy}`:
  `grantActingOUID` derives who issued the grant purely from its own stored fields (the owner for
  any share-stage grant; the anchor OU for an `all_children` reshare; the immediate parent of
  `TargetOUID` for an explicit-OU reshare — always resolvable with one ancestor lookup, since an
  explicit reshare target must be the issuer's own direct child), and `policyFromGrant` is the exact
  inverse of `shareToRoots`'/`shareToChildren`'s own grant construction.

There is deliberately **no separate "restore" primitive** bypassing `Share()`'s own checks: grant
`ID`s are generated via `utils.GenerateUUIDv7()` and were never meant to be stable, preserved
identifiers across an export/import round trip — only the shape of the grant graph matters — and
`Share()` itself has no per-caller-identity check to bypass in the first place (it only checks
structural eligibility: is `actingOUID` the owner, or already visible?). Replaying
`(ActingOUID, SharePolicy)` pairs through the ordinary `Share()` API therefore goes through the
identical validation a live API call would, and is resource-type-agnostic — it costs nothing extra
when a future resource type onboards onto the sharing framework.

### Role's declarative export additions

`roleExporter` now takes a `sharing.ServiceInterface` dependency. `GetResourceByID` populates two
new fields on `roleDeclarativeResource` — the same struct used both for export output and
declarative-YAML parsing:

- `ShareGrants []role.ShareRequest` (`yaml:"shareGrants,omitempty"`) — the role's share grants,
  converted from `ExportGrants`' `ReplayableGrant`s via `shareRequestFromReplayableGrant`
  (`backend/internal/role/model.go`), the inverse of `ShareRequest.ToSharePolicy`.
- `SharedAssignments []role.RoleSharedAssignments` (`yaml:"sharedAssignments,omitempty"`) — one
  entry per non-owner OU that has made assignments for the role, discovered via
  `RoleAssignmentServiceInterface.GetAssigningOUIDs` (delegating to
  `roleStoreInterface.GetAssigningOUIDs`, `SELECT DISTINCT ASSIGNING_OU_ID FROM ROLE_ASSIGNMENT
  WHERE ROLE_ID = ?`), each holding that OU's independent assignment list.

### Declarative YAML `shareGrants:` support, and why `sharedAssignments` is rejected

A role defined in a declarative YAML file (`loadDeclarativeResources`) may now declare
`shareGrants:` inline — the identical shape as the `POST /roles/{id}/share-grants` request body.
While parsing a batch, the loader's `Parser` closure captures each role's
`shareGrants`/`sharedAssignments` into a local `pending []pendingShare` slice (holding a pointer to
the same `*RoleWithPermissionsAndAssignments` handed to the store, so it reflects any
`ouHandle`→`ouId` resolution done during validation). Once every role in the batch has loaded
successfully, `applyPendingShares` replays each role's `shareGrants` via `sharingService.Share`, in
declared order, defaulting an omitted grant's acting OU to the role's own owning OU.

`sharedAssignments` is **hard-rejected** for declarative roles (fails server startup) — not an
oversight, but a real, pre-existing constraint: `file_based_store.go`'s
`AddAssignments`/`RemoveAssignments` unconditionally return `errors.New("AddAssignments is not
supported in file-based store")` (and the equivalent message for `RemoveAssignments`), and sibling
read methods on the same store already document that declarative roles are not shareable for
assignment purposes. Reversing that would mean rebuilding the file-based store's assignment model —
out of scope here.

### `POST /import` support

`roleDeclarativeYAML` (`backend/internal/system/importer/service_adapters.go`), the import-side
YAML shape (structurally identical to `roleDeclarativeResource`, but a distinct type), gained the
same `shareGrants`/`sharedAssignments` fields. After a role is created or updated via import,
`importRole` calls `applyRoleSharing`, which replays `shareGrants` via a narrow `sharingAdapter`
interface (`backend/internal/system/importer/service.go`, exposing only `Share`) and applies
`sharedAssignments` via the existing `roleAssignmentService.AddAssignments`, per OU group. This
works for `/import` — unlike declarative bootstrap loading — because `/import` always targets real,
DB-backed roles, which do support sharee-OU assignment writes.

`sharingService` is an unexported field on `importService`, set by the public `Initialize(...)`
wrapper (`init.go`) after construction, rather than a `newImportService` constructor parameter —
deliberately, to avoid touching the ~80 existing positional `newImportService(...)` call sites in
tests. `newImportService`'s return type changed from the `ImportServiceInterface` interface to the
concrete `*importService` pointer to make this possible; Go's implicit interface satisfaction means
every existing caller (including wrapper functions that themselves return `ImportServiceInterface`,
converting the concrete pointer at their own `return` statement) still compiles unchanged.

```mermaid
sequenceDiagram
    actor Admin
    participant Export as Declarative export<br/>(roleExporter.GetResourceByID)
    participant Sharing as sharing.Service
    participant YAML as role YAML document<br/>(declarative file or /import payload)
    participant Import as POST /import<br/>(importRole)
    participant RoleAssign as role.RoleAssignmentService

    Admin->>Export: export role {id}
    Export->>Sharing: ExportGrants(role, id)
    Sharing->>Sharing: orderGrantsByDependency(grants)<br/>parent-before-child
    Sharing-->>Export: []ReplayableGrant{ActingOUID, Policy}
    Export-->>Admin: roleDeclarativeResource{..., shareGrants, sharedAssignments}

    Admin->>YAML: save exported document

    Admin->>Import: POST /import (role document)
    loop each shareGrants entry, in declared order
        Import->>Sharing: Share(role, id, ownerOU, actingOU, policy)
        Sharing-->>Import: new ShareGrant (new ID, equivalent graph shape)
    end
    loop each sharedAssignments entry
        Import->>RoleAssign: AddAssignments(id, ouId, assignments)
    end
```

A hand-authored declarative `shareGrants:` entry, an exported grant, and a live `POST
/roles/{id}/share-grants` call are indistinguishable to the sharing framework — all three end up
calling the identical `Share()` method.
