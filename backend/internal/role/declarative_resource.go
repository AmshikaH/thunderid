// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package role

import (
	"context"
	"fmt"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"

	"gopkg.in/yaml.v3"

	"github.com/thunder-id/thunderid/internal/sharing"
	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	declarativeresource "github.com/thunder-id/thunderid/internal/system/declarative_resource"
	"github.com/thunder-id/thunderid/internal/system/log"
)

const (
	resourceTypeRole = "role"
	paramTypeRole    = "Role"
)

// roleExporter implements declarativeresource.ResourceExporter for roles.
type roleExporter struct {
	service           RoleServiceInterface
	assignmentService RoleAssignmentServiceInterface
	sharingService    sharing.ServiceInterface
}

// newRoleExporter creates a new role exporter.
func newRoleExporter(
	service RoleServiceInterface,
	assignmentService RoleAssignmentServiceInterface,
	sharingService sharing.ServiceInterface,
) *roleExporter {
	return &roleExporter{service: service, assignmentService: assignmentService, sharingService: sharingService}
}

// GetResourceType returns the resource type for roles.
func (e *roleExporter) GetResourceType() string {
	return resourceTypeRole
}

// GetParameterizerType returns the parameterizer type for roles.
func (e *roleExporter) GetParameterizerType() string {
	return paramTypeRole
}

// GetAllResourceIDs retrieves all role IDs from the database store.
// In composite mode, this excludes declarative (YAML-based) roles.
func (e *roleExporter) GetAllResourceIDs(ctx context.Context) ([]string, *tidcommon.ServiceError) {
	offset := 0
	limit := serverconst.MaxPageSize
	ids := []string{}

	for {
		roles, err := e.service.GetRoleList(ctx, limit, offset)
		if err != nil {
			return nil, err
		}

		for _, role := range roles.Roles {
			isDeclarative, svcErr := e.service.IsRoleDeclarative(ctx, role.ID)
			if svcErr != nil {
				return nil, svcErr
			}
			if !isDeclarative {
				ids = append(ids, role.ID)
			}
		}

		offset += len(roles.Roles)

		// Continue fetching while we get results; stop only on empty page
		if len(roles.Roles) == 0 {
			break
		}
	}

	return ids, nil
}

// GetResourceByID retrieves a role by its ID.
func (e *roleExporter) GetResourceByID(
	ctx context.Context, id string) (interface{}, string, *tidcommon.ServiceError) {
	roleWithPermissions, err := e.service.GetRoleWithPermissions(ctx, id)
	if err != nil {
		return nil, "", err
	}

	assignments, err := e.getAllRoleAssignments(ctx, id, roleWithPermissions.OUID)
	if err != nil {
		return nil, "", err
	}

	sharedAssignments, err := e.getSharedAssignments(ctx, id, roleWithPermissions.OUID)
	if err != nil {
		return nil, "", err
	}

	shareGrants, err := e.getShareGrants(ctx, id)
	if err != nil {
		return nil, "", err
	}

	perms := make([]roleDeclarativePermission, 0, len(roleWithPermissions.Permissions))
	for _, p := range roleWithPermissions.Permissions {
		perms = append(perms, roleDeclarativePermission(p))
	}

	role := &roleDeclarativeResource{
		ID:                roleWithPermissions.ID,
		Name:              roleWithPermissions.Name,
		Description:       roleWithPermissions.Description,
		OUID:              roleWithPermissions.OUID,
		Permissions:       perms,
		Assignments:       assignments,
		ShareGrants:       shareGrants,
		SharedAssignments: sharedAssignments,
	}

	return role, role.Name, nil
}

// getSharedAssignments exports every sharee OU's independent assignment set for the role (the
// owning OU's own assignments are exported separately, via Assignments).
func (e *roleExporter) getSharedAssignments(
	ctx context.Context, roleID, owningOUID string,
) ([]RoleSharedAssignments, *tidcommon.ServiceError) {
	ouIDs, err := e.assignmentService.GetAssigningOUIDs(ctx, roleID)
	if err != nil {
		return nil, err
	}

	sharedAssignments := make([]RoleSharedAssignments, 0, len(ouIDs))
	for _, ouID := range ouIDs {
		if ouID == owningOUID {
			continue
		}
		assignments, err := e.getAllRoleAssignments(ctx, roleID, ouID)
		if err != nil {
			return nil, err
		}
		sharedAssignments = append(sharedAssignments, RoleSharedAssignments{
			OUID:        ouID,
			Assignments: assignments,
		})
	}

	return sharedAssignments, nil
}

// getShareGrants exports the role's share grants as replayable ShareRequests, in an order safe to
// apply sequentially (a reshare is always ordered after the grant that made its issuing OU visible).
func (e *roleExporter) getShareGrants(ctx context.Context, roleID string) ([]ShareRequest, *tidcommon.ServiceError) {
	replayable, err := e.sharingService.ExportGrants(ctx, roleSharingResourceType, roleID)
	if err != nil {
		return nil, err
	}

	grants := make([]ShareRequest, 0, len(replayable))
	for _, g := range replayable {
		grants = append(grants, shareRequestFromReplayableGrant(g))
	}

	return grants, nil
}

// ValidateResource validates a role resource.
func (e *roleExporter) ValidateResource(ctx context.Context,
	resource interface{}, id string, logger *log.Logger,
) (string, *declarativeresource.ExportError) {
	role, ok := resource.(*roleDeclarativeResource)
	if !ok {
		return "", declarativeresource.CreateTypeError(resourceTypeRole, id)
	}

	if err := declarativeresource.ValidateResourceName(ctx,
		role.Name, resourceTypeRole, id, "ROLE_VALIDATION_ERROR", logger); err != nil {
		return "", err
	}

	return role.Name, nil
}

// GetResourceRules returns the parameterization rules for roles.
func (e *roleExporter) GetResourceRules() *declarativeresource.ResourceRules {
	return &declarativeresource.ResourceRules{
		Variables:      []string{},
		ArrayVariables: []string{},
	}
}

// pendingShare captures one declaratively-declared role's share grants (and, if any,
// sharedAssignments) discovered while parsing, for application after every role in the batch has
// been successfully loaded. role is the same pointer handed to the store, so its OUID reflects any
// ou_handle resolution performed by the validator.
type pendingShare struct {
	role              *RoleWithPermissionsAndAssignments
	shareGrants       []ShareRequest
	sharedAssignments []RoleSharedAssignments
}

// loadDeclarativeResources loads immutable role resources from files.
// The dbStore parameter is optional (can be nil) and is used for duplicate checking in composite mode.
// The service parameter is optional (can be nil) and is used to resolve ou_handle to ou_id.
func loadDeclarativeResources(
	fileStore *fileBasedStore, dbStore roleStoreInterface, service RoleServiceInterface,
	sharingService sharing.ServiceInterface,
) error {
	var pending []pendingShare

	resourceConfig := declarativeresource.ResourceConfig{
		ResourceType:  "Role",
		DirectoryName: "roles",
		Parser: func(data []byte) (interface{}, error) {
			role, err := parseToRole(data)
			if err != nil {
				return nil, err
			}

			var resource roleDeclarativeResource
			if err := yaml.Unmarshal(data, &resource); err != nil {
				return nil, err
			}
			if len(resource.ShareGrants) > 0 || len(resource.SharedAssignments) > 0 {
				pending = append(pending, pendingShare{
					role:              role,
					shareGrants:       resource.ShareGrants,
					sharedAssignments: resource.SharedAssignments,
				})
			}

			return role, nil
		},
		Validator: func(data interface{}) error {
			return validateRoleWrapper(data, fileStore, dbStore, service)
		},
		IDExtractor: func(data interface{}) string {
			// Use safe type assertion to prevent panic
			if v, ok := data.(*RoleWithPermissionsAndAssignments); ok {
				return v.ID
			}
			// Log error and return empty string if type assertion fails
			// Declarative resource loading runs during startup, outside any request.
			log.GetLogger().Error(context.Background(),
				"IDExtractor: type assertion failed for RoleWithPermissionsAndAssignments")
			return ""
		},
	}

	loader := declarativeresource.NewResourceLoader(resourceConfig, fileStore)
	if err := loader.LoadResources(); err != nil {
		return fmt.Errorf("failed to load role resources: %w", err)
	}

	return applyPendingShares(pending, sharingService)
}

// parseToRoleWrapper wraps parseToRole to match the generic Parser signature.
func parseToRoleWrapper(data []byte) (interface{}, error) {
	return parseToRole(data)
}

// applyPendingShares replays every declaratively-declared share grant via the normal Share() API,
// in declared order, reusing its existing eligibility checks. sharedAssignments is rejected
// outright: the file-based store has no support for sharee-OU assignment writes (AddAssignments
// unconditionally errors there), so a declarative role cannot hold per-OU templated config.
func applyPendingShares(pending []pendingShare, sharingService sharing.ServiceInterface) error {
	for _, p := range pending {
		if len(p.sharedAssignments) > 0 {
			return fmt.Errorf(
				"role '%s': sharedAssignments is not supported for declarative roles; "+
					"declarative roles cannot hold per-OU templated configuration", p.role.ID)
		}

		for _, req := range p.shareGrants {
			actingOUID := req.OUID
			if actingOUID == "" {
				actingOUID = p.role.OUID
			}
			if _, svcErr := sharingService.Share(
				context.Background(), roleSharingResourceType, p.role.ID, p.role.OUID, actingOUID, req.ToSharePolicy(),
			); svcErr != nil {
				return fmt.Errorf("role '%s': failed to apply declarative share grant: %s", p.role.ID, svcErr.Code)
			}
		}
	}

	return nil
}

type roleDeclarativePermission ResourcePermissions

type roleDeclarativeResource struct {
	ID          string                      `yaml:"id"`
	Name        string                      `yaml:"name"`
	Description string                      `yaml:"description,omitempty"`
	OUID        string                      `yaml:"ouId,omitempty"`
	OUHandle    string                      `yaml:"ouHandle,omitempty"`
	Permissions []roleDeclarativePermission `yaml:"permissions"`
	Assignments []RoleAssignment            `yaml:"assignments,omitempty"`
	// ShareGrants declares this role's share grants, replayed via sharing.ServiceInterface.Share in
	// declared order when loaded declaratively (see loadDeclarativeResources), or exported here for
	// declarative round-tripping. OUID empty means "the role's own owning OU is the acting OU".
	ShareGrants []ShareRequest `yaml:"shareGrants,omitempty"`
	// SharedAssignments declares independent per-sharee-OU assignment sets. Only meaningful for
	// mutable/composite-mode (DB-backed) roles: a purely declarative (file-store) role cannot hold
	// them, since the file-based store does not support sharee-OU assignment writes.
	SharedAssignments []RoleSharedAssignments `yaml:"sharedAssignments,omitempty"`
}

// toResourcePermissions converts roleDeclarativePermission to ResourcePermissions.
func toResourcePermissions(perm roleDeclarativePermission) ResourcePermissions {
	return ResourcePermissions(perm)
}

// parseToRole parses YAML data to RoleWithPermissionsAndAssignments.
func parseToRole(data []byte) (*RoleWithPermissionsAndAssignments, error) {
	var roleResource roleDeclarativeResource
	if err := yaml.Unmarshal(data, &roleResource); err != nil {
		return nil, err
	}

	permissions := make([]ResourcePermissions, 0, len(roleResource.Permissions))
	for _, perm := range roleResource.Permissions {
		permissions = append(permissions, toResourcePermissions(perm))
	}

	// Translate public 'user'/'app'/'agent' assignment types to the internal 'entity' type.
	for i, a := range roleResource.Assignments {
		if a.Type.IsEntityType() {
			roleResource.Assignments[i].Type = assigneeTypeEntity
		}
	}

	role := &RoleWithPermissionsAndAssignments{
		ID:          roleResource.ID,
		Name:        roleResource.Name,
		Description: roleResource.Description,
		OUID:        roleResource.OUID,
		OUHandle:    roleResource.OUHandle,
		Permissions: permissions,
		Assignments: roleResource.Assignments,
	}

	return role, nil
}

// validateRoleWrapper validates role declarative resources and checks for duplicates.
// When a service is provided, OU handles are resolved before validation runs.
func validateRoleWrapper(
	data interface{}, fileStore *fileBasedStore, dbStore roleStoreInterface, service RoleServiceInterface,
) error {
	role, ok := data.(*RoleWithPermissionsAndAssignments)
	if !ok {
		return fmt.Errorf("invalid type: expected *RoleWithPermissionsAndAssignments")
	}

	if role.ID == "" {
		return fmt.Errorf("role ID is required")
	}
	if role.Name == "" {
		return fmt.Errorf("role name is required")
	}
	if service != nil {
		if svcErr := service.ResolveRoleOUHandle(context.Background(), role); svcErr != nil {
			return fmt.Errorf("organization unit with handle %q not found for role '%s'",
				role.OUHandle, role.Name)
		}
	}
	if role.OUID == "" {
		return fmt.Errorf("ouId or ouHandle is required for role '%s'", role.Name)
	}

	for _, assignment := range role.Assignments {
		if assignment.ID == "" {
			return fmt.Errorf("assignment ID is required")
		}
		if assignment.Type != assigneeTypeEntity && assignment.Type != AssigneeTypeGroup {
			return fmt.Errorf("invalid assignment type '%s'", assignment.Type)
		}
	}

	for _, resourcePerms := range role.Permissions {
		if resourcePerms.ResourceServerID == "" {
			return fmt.Errorf("resource server ID is required")
		}
	}

	if fileStore != nil {
		if existingData, err := fileStore.GenericFileBasedStore.Get(role.ID); err == nil && existingData != nil {
			return fmt.Errorf("duplicate role ID '%s': role already exists in declarative resources", role.ID)
		}
	}

	if dbStore != nil {
		exists, err := dbStore.IsRoleExist(context.Background(), role.ID)
		if err != nil {
			// Fail loudly on DB errors during duplicate check
			return fmt.Errorf("checking role existence for '%s': %w", role.ID, err)
		}
		if exists {
			return fmt.Errorf("duplicate role ID '%s': role already exists in the database store", role.ID)
		}
	}

	return nil
}

func (e *roleExporter) getAllRoleAssignments(
	ctx context.Context,
	roleID, ouID string,
) ([]RoleAssignment, *tidcommon.ServiceError) {
	offset := 0
	limit := serverconst.MaxPageSize
	assignments := []RoleAssignment{}

	for {
		list, err := e.assignmentService.GetRoleAssignments(ctx, roleID, ouID, limit, offset, false)
		if err != nil {
			return nil, err
		}

		for _, assignment := range list.Assignments {
			assignments = append(assignments, RoleAssignment{
				ID:   assignment.ID,
				Type: assignment.Type,
			})
		}

		offset += len(list.Assignments)

		// Continue fetching while we get results; stop only on empty page
		if len(list.Assignments) == 0 {
			break
		}
	}

	return assignments, nil
}
