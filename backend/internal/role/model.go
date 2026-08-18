// Copyright 2025 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package role

import (
	"github.com/thunder-id/thunderid/internal/sharing"
	"github.com/thunder-id/thunderid/internal/system/utils"
)

// AssigneeType represents the type of assignee principal.
type AssigneeType string

// Public assignee types accepted in requests and returned in responses.
const (
	// AssigneeTypeUser is the public type for user principals.
	AssigneeTypeUser AssigneeType = "user"
	// AssigneeTypeApp is the public type for application principals.
	AssigneeTypeApp AssigneeType = "app"
	// AssigneeTypeAgent is the public type for agent principals.
	AssigneeTypeAgent AssigneeType = "agent"
	// AssigneeTypeGroup is the public type for group principals.
	AssigneeTypeGroup AssigneeType = "group"
)

// Internal assignee types used only for storage.
const (
	assigneeTypeEntity AssigneeType = "entity"
)

// IsEntityType reports whether t is an entity type (user, app, agent) that maps
// to the internal entity storage type.
func (t AssigneeType) IsEntityType() bool {
	switch t {
	case AssigneeTypeUser, AssigneeTypeApp, AssigneeTypeAgent:
		return true
	}
	return false
}

// AssignmentResponse represents an assignment of a role to a user or group.
type AssignmentResponse struct {
	ID      string       `json:"id"`
	Type    AssigneeType `json:"type"`
	Display string       `json:"display,omitempty"`
}

// AssignmentRequest represents an assignment of a role to a user or group.
type AssignmentRequest struct {
	ID   string       `json:"id"   native:"required"`
	Type AssigneeType `json:"type" native:"required,oneof=user app agent group"`
}

// RoleSummaryResponse represents the basic information of a role.
type RoleSummaryResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	OUID        string `json:"ouId"`
	OUHandle    string `json:"ouHandle,omitempty"`
	IsReadOnly  bool   `json:"isReadOnly"`
}

// RoleResponse represents a complete role with permissions.
type RoleResponse struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	OUID        string                `json:"ouId"`
	OUHandle    string                `json:"ouHandle,omitempty"`
	Permissions []ResourcePermissions `json:"permissions"`
}

// CreateRoleRequest represents the request body for creating a role.
type CreateRoleRequest struct {
	Name        string                `json:"name"                  native:"required,min=1,max=100"`
	Description string                `json:"description,omitempty"`
	OUID        string                `json:"ouId"                  native:"required"`
	Permissions []ResourcePermissions `json:"permissions"`
	Assignments []AssignmentRequest   `json:"assignments,omitempty"`
}

// CreateRoleResponse represents the response body for creating a role.
type CreateRoleResponse struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	OUID        string                `json:"ouId"`
	OUHandle    string                `json:"ouHandle,omitempty"`
	Permissions []ResourcePermissions `json:"permissions"`
	Assignments []AssignmentResponse  `json:"assignments,omitempty"`
}

// UpdateRoleRequest represents the request body for updating a role.
type UpdateRoleRequest struct {
	Name        string                `json:"name"                  native:"required,min=1,max=100"`
	Description string                `json:"description,omitempty"`
	OUID        string                `json:"ouId"                  native:"required"`
	Permissions []ResourcePermissions `json:"permissions"`
}

// AssignmentsRequest represents the request body for adding or removing assignments.
type AssignmentsRequest struct {
	Assignments []AssignmentRequest `json:"assignments" native:"required,min=1,dive"`
}

// RoleListResponse represents the response for listing roles with pagination.
type RoleListResponse struct {
	TotalResults int                   `json:"totalResults"`
	StartIndex   int                   `json:"startIndex"`
	Count        int                   `json:"count"`
	Roles        []RoleSummaryResponse `json:"roles"`
	Links        []utils.Link          `json:"links"`
}

// AssignmentListResponse represents the response for listing role assignments with pagination.
type AssignmentListResponse struct {
	TotalResults int                  `json:"totalResults"`
	StartIndex   int                  `json:"startIndex"`
	Count        int                  `json:"count"`
	Assignments  []AssignmentResponse `json:"assignments"`
	Links        []utils.Link         `json:"links"`
}

// Internal service layer structs - used for business logic processing

// ResourcePermissions represents permissions grouped by resource server.
type ResourcePermissions struct {
	ResourceServerID string   `json:"resourceServerId" yaml:"resourceServerId"`
	Permissions      []string `json:"permissions"      yaml:"permissions"`
}

// RoleCreationDetail represents the parameters for creating a role.
// ID is optional; if empty, the service generates a new UUID.
type RoleCreationDetail struct {
	ID          string
	Name        string
	Description string
	OUID        string
	Permissions []ResourcePermissions
	Assignments []RoleAssignment
}

// RoleWithPermissionsAndAssignments represents the parameters for creating a role.
type RoleWithPermissionsAndAssignments struct {
	ID          string
	Name        string
	Description string
	OUID        string
	OUHandle    string
	Permissions []ResourcePermissions
	Assignments []RoleAssignment
}

// RoleAssignment represents an assignment used internally by the service layer.
type RoleAssignment struct {
	ID   string       `yaml:"id"`
	Type AssigneeType `yaml:"type"`
}

// RoleAssignmentWithDisplay represents an assignment used internally by the service layer.
type RoleAssignmentWithDisplay struct {
	ID      string
	Type    AssigneeType
	Display string
}

// Role represents basic role information used internally by the service layer.
type Role struct {
	ID          string
	Name        string
	Description string
	OUID        string
	OUHandle    string
	IsReadOnly  bool
}

// RoleWithPermissions represents complete role details used internally by the service layer.
type RoleWithPermissions struct {
	ID          string
	Name        string
	Description string
	OUID        string
	OUHandle    string
	Permissions []ResourcePermissions
}

// RoleUpdateDetail represents the parameters for creating a role.
type RoleUpdateDetail struct {
	Name        string
	Description string
	OUID        string
	Permissions []ResourcePermissions
}

// RoleList represents the result of listing roles.
type RoleList struct {
	TotalResults int
	StartIndex   int
	Count        int
	Roles        []Role
	Links        []utils.Link
}

// Role origin values distinguishing whether a role returned for an OU is owned by that OU or
// was shared (directly or via reshare) to it. Core config (name, permissions) is always resolved
// from the owning OU regardless of origin — Role.OUID/OUHandle already reflect the owner.
const (
	RoleOriginOwned  = "owned"
	RoleOriginShared = "shared"
)

// RoleForOU represents a role visible to a given OU (owned or shared), with an explicit origin.
type RoleForOU struct {
	Role
	Origin string
}

// RoleListForOU represents the result of listing roles owned by or shared to an OU.
type RoleListForOU struct {
	TotalResults int
	StartIndex   int
	Count        int
	Roles        []RoleForOU
	Links        []utils.Link
}

// RoleSummaryForOUResponse represents a role visible to a given OU, over HTTP.
type RoleSummaryForOUResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	OUID        string `json:"ouId"`
	OUHandle    string `json:"ouHandle,omitempty"`
	IsReadOnly  bool   `json:"isReadOnly"`
	Origin      string `json:"origin"`
}

// RoleListForOUResponse represents the response for listing roles owned by or shared to an OU.
type RoleListForOUResponse struct {
	TotalResults int                        `json:"totalResults"`
	StartIndex   int                        `json:"startIndex"`
	Count        int                        `json:"count"`
	Roles        []RoleSummaryForOUResponse `json:"roles"`
	Links        []utils.Link               `json:"links"`
}

// ShareRequest represents the request body for creating a share grant on a role. Exactly one of
// two target-scope modes must be selected: root-targeting (allRoots or rootOuIds — only valid when
// ouId is the role's own owning OU) or children-targeting (allChildren or ouIds — valid for any
// ouId currently visible for the role).
//
// This same shape is reused, unchanged, for a role's declarative YAML shareGrants entries (see
// roleDeclarativeResource) and for POST /import's shareGrants — both apply each entry via the
// identical Share() call the REST endpoint uses, so a hand-authored declarative grant, an exported
// grant, and a live API call are indistinguishable to the sharing framework. The yaml tags exist
// for those two paths; the json tags remain the REST contract.
type ShareRequest struct {
	// OUID is the organization unit performing this share. Optional; defaults to the role's own
	// owning organization unit when omitted (the common case: the owner sharing for the first
	// time). Set explicitly when a different, already-visible organization unit is sharing further
	// within its own subtree.
	OUID      string   `json:"ouId,omitempty"       yaml:"ouId,omitempty"`
	AllRoots  bool     `json:"allRoots,omitempty"   yaml:"allRoots,omitempty"`
	RootOUIDs []string `json:"rootOuIds,omitempty"  yaml:"rootOuIds,omitempty"`
	// ExcludedRootOUIDs carves these Root OUs (and their subtrees) out of an AllRoots share.
	// Ignored unless AllRoots is true.
	ExcludedRootOUIDs []string `json:"excludedRootOuIds,omitempty" yaml:"excludedRootOuIds,omitempty"`
	AllChildren       bool     `json:"allChildren,omitempty"       yaml:"allChildren,omitempty"`
	// OUIDs, when set, must each be a direct child of ouId — reaching a grandchild selectively
	// requires that child to issue its own share call in turn.
	OUIDs []string `json:"ouIds,omitempty" yaml:"ouIds,omitempty"`
	// ExcludedOUIDs carves these OUs (and their subtrees) out of an AllChildren share. Ignored
	// unless AllChildren is true.
	ExcludedOUIDs []string `json:"excludedOuIds,omitempty" yaml:"excludedOuIds,omitempty"`
	// EditableFields names the templated fields ("assignments", "assignments.user",
	// "assignments.group", "assignments.app", "assignments.agent") made editable through this
	// grant. Omit for "everything": every field, when ouId is the role's own owning organization
	// unit; exactly ouId's own current editable set, when ouId is a sharee reshare — a reshare may
	// only narrow this set relative to its own, never widen it.
	EditableFields []string `json:"editableFields,omitempty" yaml:"editableFields,omitempty"`
}

// ToSharePolicy converts req's target-scope fields into the sharing.SharePolicy Share() expects.
// OUID is not part of SharePolicy — it selects the acting OU, resolved by the caller.
func (req ShareRequest) ToSharePolicy() sharing.SharePolicy {
	return sharing.SharePolicy{
		AllRoots:          req.AllRoots,
		RootOUIDs:         req.RootOUIDs,
		ExcludedRootOUIDs: req.ExcludedRootOUIDs,
		AllChildren:       req.AllChildren,
		OUIDs:             req.OUIDs,
		ExcludedOUIDs:     req.ExcludedOUIDs,
		EditableFields:    req.EditableFields,
	}
}

// shareRequestFromReplayableGrant converts one sharing.ReplayableGrant (see
// sharing.ServiceInterface.ExportGrants) back into the ShareRequest shape used for declarative
// YAML and POST /import — the inverse of ToSharePolicy, with ActingOUID carried as OUID.
func shareRequestFromReplayableGrant(g sharing.ReplayableGrant) ShareRequest {
	return ShareRequest{
		OUID:              g.ActingOUID,
		AllRoots:          g.Policy.AllRoots,
		RootOUIDs:         g.Policy.RootOUIDs,
		ExcludedRootOUIDs: g.Policy.ExcludedRootOUIDs,
		AllChildren:       g.Policy.AllChildren,
		OUIDs:             g.Policy.OUIDs,
		ExcludedOUIDs:     g.Policy.ExcludedOUIDs,
		EditableFields:    g.Policy.EditableFields,
	}
}

// RoleSharedAssignments is one sharee organization unit's independent templated-config
// (assignments) state for a role — the export/import/declarative-YAML counterpart of a share
// grant's target OU actually having assignments configured. Not every OU a grant's scope reaches
// necessarily has any (assignments are opt-in per OU), so this is populated per OU actually found
// to have assignment rows, not derived from grant target scopes.
type RoleSharedAssignments struct {
	OUID        string           `yaml:"ouId"`
	Assignments []RoleAssignment `yaml:"assignments"`
}

// ShareGrantResponse represents a single share grant over HTTP.
type ShareGrantResponse struct {
	ID          string `json:"id"`
	Stage       string `json:"stage"`
	TargetScope string `json:"targetScope"`
	TargetOUID  string `json:"targetOuId,omitempty"`
	OwningOUID  string `json:"owningOuId"`
	// ExcludedOUIDs is only ever non-empty for the all_roots/all_children target scopes.
	ExcludedOUIDs []string `json:"excludedOuIds,omitempty"`
	// EditableFields is the materialized set of templated fields editable through this grant.
	EditableFields []string `json:"editableFields,omitempty"`
}

// ShareGrantListResponse represents the response for listing a role's share grants.
type ShareGrantListResponse struct {
	Grants []ShareGrantResponse `json:"grants"`
}

// EditableFieldsResponse represents the response for the "which templated fields can this
// organization unit edit" metadata endpoint.
type EditableFieldsResponse struct {
	Fields []string `json:"fields"`
}

// AssignmentList represents the result of listing role assignments.
type AssignmentList struct {
	TotalResults int
	StartIndex   int
	Count        int
	Assignments  []RoleAssignmentWithDisplay
	Links        []utils.Link
}
