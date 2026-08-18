// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package sharing

import (
	oupkg "github.com/thunder-id/thunderid/internal/ou"
	"github.com/thunder-id/thunderid/internal/system/cache"
	"github.com/thunder-id/thunderid/internal/system/sysauthz"
)

// Initialize wires the sharing service. It exposes no HTTP routes of its own — like
// system/resourcedependency, it is a pure cross-cutting service consumed by each onboarded
// resource type's own handler (e.g. role exposes /roles/{id}/share backed by this service).
//
// ouHierarchyResolver is the same sysauthz.OUHierarchyResolver instance produced by ou.Initialize
// and already used to complete sysauthz's own two-phase init; reusing it here means the sharing
// package needs no direct dependency on internal/ou for tree traversal, only on
// OrganizationUnitServiceInterface for the (rare, admin-path) subtree enumeration used by Unshare.
func Initialize(
	cacheManager cache.CacheManagerInterface,
	ouHierarchyResolver sysauthz.OUHierarchyResolver,
	ouService oupkg.OrganizationUnitServiceInterface,
	allowChildOUCrossTreeSharing bool,
) (ServiceInterface, error) {
	store, transactioner, err := newSharingStore()
	if err != nil {
		return nil, err
	}

	var editabilityCache cache.CacheInterface[bool]
	var visibilityCache cache.CacheInterface[bool]
	var visibleIDsCache cache.CacheInterface[[]string]
	if cacheManager != nil {
		editabilityCache = cache.GetCache[bool](cacheManager, "SharingEditabilityCache")
		visibilityCache = cache.GetCache[bool](cacheManager, "SharingVisibilityCache")
		visibleIDsCache = cache.GetCache[[]string](cacheManager, "SharingVisibleIDsCache")
	}

	svc := newService(store, ouHierarchyResolver, ouService, transactioner,
		editabilityCache, visibilityCache, visibleIDsCache, allowChildOUCrossTreeSharing)

	return svc, nil
}
