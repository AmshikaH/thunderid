// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowmgt

import (
	"context"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"

	"github.com/thunder-id/thunderid/internal/ou"
	"github.com/thunder-id/thunderid/internal/system/log"
	"github.com/thunder-id/thunderid/internal/system/security"
)

// ouResolver resolves and checks the organization unit a flow is owned by.
type ouResolver struct {
	ouSvc  ouProvider
	logger *log.Logger
}

// newOUResolver creates the resolver of the organization unit a flow is owned by.
func newOUResolver(ouSvc ouProvider) *ouResolver {
	return &ouResolver{
		ouSvc:  ouSvc,
		logger: log.GetLogger().With(log.String(log.LoggerKeyComponentName, "FlowOUResolver")),
	}
}

// resolve returns the id of the organization unit a flow is owned by. An id is used as given, and a handle is
// resolved to the id of the unit it names; when both are given the id wins. The unit has to exist. Only a lookup that
// finds no such unit is reported as not found, so a failed or refused lookup is not taken for one.
//
// A handle is resolved with the runtime context: it comes from an import or a declarative file, which name the owner
// by handle on behalf of the deployment and not of whoever runs the import.
func (r *ouResolver) resolve(ctx context.Context, ouID, ouHandle string) (string, *tidcommon.ServiceError) {
	if ouID != "" && ouHandle != "" {
		r.logger.Warn(ctx, "Both ouId and ouHandle provided for a flow; ouHandle ignored")
	}
	if ouID == "" {
		if ouHandle == "" {
			return "", &ErrorFlowOURequired
		}
		unit, svcErr := r.ouSvc.GetOrganizationUnitByPath(security.WithRuntimeContext(ctx), ouHandle)
		if svcErr != nil {
			if svcErr.Code == ou.ErrorOrganizationUnitNotFound.Code {
				return "", &ErrorInvalidFlowOU
			}
			r.logger.Error(ctx, "Failed to look up the organization unit of a flow by handle",
				log.String("errorCode", svcErr.Code))
			return "", &tidcommon.InternalServerError
		}
		return unit.ID, nil
	}
	exists, svcErr := r.ouSvc.IsOrganizationUnitExists(ctx, ouID)
	if svcErr != nil {
		r.logger.Error(ctx, "Failed to look up the organization unit of a flow",
			log.String("errorCode", svcErr.Code))
		return "", &tidcommon.InternalServerError
	}
	if !exists {
		return "", &ErrorInvalidFlowOU
	}
	return ouID, nil
}

// resolveForUpdate returns the owner an updated flow has. An owner cannot change once the flow is created, so an
// update has to name the one the flow already has, by id or by handle.
func (r *ouResolver) resolveForUpdate(
	ctx context.Context, requestedID, requestedHandle, currentID string,
) (string, *tidcommon.ServiceError) {
	if requestedID == "" && requestedHandle == "" {
		return "", &ErrorFlowOURequired
	}
	resolved := requestedID
	if resolved == "" {
		var svcErr *tidcommon.ServiceError
		if resolved, svcErr = r.resolve(ctx, "", requestedHandle); svcErr != nil {
			return "", svcErr
		}
	}
	if resolved != currentID {
		return "", &ErrorOUUpdateNotAllowed
	}
	return currentID, nil
}
