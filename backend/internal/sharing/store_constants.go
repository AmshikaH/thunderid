// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package sharing

import (
	"fmt"
	"strings"

	dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"
)

var (
	// queryCreateShareGrant inserts a new share grant.
	queryCreateShareGrant = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-01",
		Query: `INSERT INTO "SHARE_GRANT"
			(ID, RESOURCE_TYPE, RESOURCE_ID, OWNING_OU_ID, SHARE_STAGE, TARGET_SCOPE, TARGET_OU_ID,
			 PARENT_GRANT_ID, DEPLOYMENT_ID)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
	}

	// queryGetShareGrantByID retrieves a single share grant by id.
	queryGetShareGrantByID = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-02",
		Query: `SELECT ID, RESOURCE_TYPE, RESOURCE_ID, OWNING_OU_ID, SHARE_STAGE, TARGET_SCOPE, TARGET_OU_ID,
			PARENT_GRANT_ID FROM "SHARE_GRANT" WHERE ID = $1 AND DEPLOYMENT_ID = $2`,
	}

	// queryDeleteShareGrant deletes a share grant by id. Reshare grants referencing it via
	// PARENT_GRANT_ID, and exclusion rows referencing it via GRANT_ID, are removed by the
	// database's ON DELETE CASCADE.
	queryDeleteShareGrant = dbmodel.DBQuery{
		ID:    "SHQ-SHARING_MGT-03",
		Query: `DELETE FROM "SHARE_GRANT" WHERE ID = $1 AND DEPLOYMENT_ID = $2`,
	}

	// queryListGrantsForResource lists every share grant for a given resource.
	queryListGrantsForResource = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-04",
		Query: `SELECT ID, RESOURCE_TYPE, RESOURCE_ID, OWNING_OU_ID, SHARE_STAGE, TARGET_SCOPE, TARGET_OU_ID,
			PARENT_GRANT_ID FROM "SHARE_GRANT" WHERE RESOURCE_TYPE = $1 AND RESOURCE_ID = $2 AND DEPLOYMENT_ID = $3
			ORDER BY CREATED_AT`,
	}

	// queryListChildGrants lists reshare grants that derive from the given parent share grant.
	queryListChildGrants = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-05",
		Query: `SELECT ID, RESOURCE_TYPE, RESOURCE_ID, OWNING_OU_ID, SHARE_STAGE, TARGET_SCOPE, TARGET_OU_ID,
			PARENT_GRANT_ID FROM "SHARE_GRANT" WHERE PARENT_GRANT_ID = $1 AND DEPLOYMENT_ID = $2`,
	}

	// queryGetResourceOverlay retrieves a single templated field's sharee override value.
	queryGetResourceOverlay = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-06",
		Query: `SELECT VALUE FROM "RESOURCE_OVERLAY"
			WHERE RESOURCE_TYPE = $1 AND RESOURCE_ID = $2 AND OU_ID = $3 AND FIELD_KEY = $4 AND DEPLOYMENT_ID = $5`,
	}

	// queryUpsertResourceOverlay upserts a templated field's sharee override value.
	queryUpsertResourceOverlay = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-07",
		Query: `INSERT INTO "RESOURCE_OVERLAY"
			(RESOURCE_TYPE, RESOURCE_ID, OU_ID, FIELD_KEY, VALUE, DEPLOYMENT_ID)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (RESOURCE_TYPE, RESOURCE_ID, OU_ID, FIELD_KEY, DEPLOYMENT_ID)
			DO UPDATE SET VALUE = excluded.VALUE, UPDATED_AT = NOW()`,
		SQLiteQuery: `INSERT INTO "RESOURCE_OVERLAY"
			(RESOURCE_TYPE, RESOURCE_ID, OU_ID, FIELD_KEY, VALUE, DEPLOYMENT_ID)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (RESOURCE_TYPE, RESOURCE_ID, OU_ID, FIELD_KEY, DEPLOYMENT_ID)
			DO UPDATE SET VALUE = excluded.VALUE, UPDATED_AT = datetime('now')`,
	}

	// queryDeleteResourceOverlay removes a templated field's sharee override value.
	queryDeleteResourceOverlay = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-08",
		Query: `DELETE FROM "RESOURCE_OVERLAY"
			WHERE RESOURCE_TYPE = $1 AND RESOURCE_ID = $2 AND OU_ID = $3 AND FIELD_KEY = $4 AND DEPLOYMENT_ID = $5`,
	}

	// queryInsertGrantExclusion adds one excluded OU to a grant's exclusion list.
	queryInsertGrantExclusion = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-14",
		Query: `INSERT INTO "SHARE_GRANT_EXCLUSION" (GRANT_ID, EXCLUDED_OU_ID, DEPLOYMENT_ID)
			VALUES ($1, $2, $3)
			ON CONFLICT (GRANT_ID, EXCLUDED_OU_ID, DEPLOYMENT_ID) DO NOTHING`,
	}

	// queryListGrantExclusions lists every OU excluded from a given grant.
	queryListGrantExclusions = dbmodel.DBQuery{
		ID:    "SHQ-SHARING_MGT-15",
		Query: `SELECT EXCLUDED_OU_ID FROM "SHARE_GRANT_EXCLUSION" WHERE GRANT_ID = $1 AND DEPLOYMENT_ID = $2`,
	}

	// queryInsertGrantEditableField adds one templated field key to a grant's materialized
	// editable-field set.
	queryInsertGrantEditableField = dbmodel.DBQuery{
		ID: "SHQ-SHARING_MGT-16",
		Query: `INSERT INTO "SHARE_GRANT_EDITABLE_FIELD" (GRANT_ID, FIELD_KEY, DEPLOYMENT_ID)
			VALUES ($1, $2, $3)
			ON CONFLICT (GRANT_ID, FIELD_KEY, DEPLOYMENT_ID) DO NOTHING`,
	}

	// queryListGrantEditableFields lists every templated field key editable through a given grant.
	queryListGrantEditableFields = dbmodel.DBQuery{
		ID:    "SHQ-SHARING_MGT-17",
		Query: `SELECT FIELD_KEY FROM "SHARE_GRANT_EDITABLE_FIELD" WHERE GRANT_ID = $1 AND DEPLOYMENT_ID = $2`,
	}
)

// buildRelevantGrantsQuery constructs a database-specific query returning every grant of
// resourceType that could plausibly cover any OU in chainOUIDs: an all_roots grant (which can
// apply to any root, so it is always a candidate) or one whose TARGET_OU_ID matches an element of
// chainOUIDs. This is a bounded row fetch only — the caller (evaluateChainVisibility in
// service.go) does the actual per-resource, hop-by-hop coverage evaluation, including exclusion
// checks and chain integrity, in memory.
func buildRelevantGrantsQuery(
	resourceType ResourceType, chainOUIDs []string, deploymentID string,
) (dbmodel.DBQuery, []interface{}) {
	postgresPlaceholders := make([]string, len(chainOUIDs))
	sqlitePlaceholders := make([]string, len(chainOUIDs))
	args := []interface{}{deploymentID, string(resourceType)}
	for i, id := range chainOUIDs {
		postgresPlaceholders[i] = fmt.Sprintf("$%d", i+3) // $1 = deploymentID, $2 = resourceType
		sqlitePlaceholders[i] = "?"
		args = append(args, id)
	}

	columns := `ID, RESOURCE_TYPE, RESOURCE_ID, OWNING_OU_ID, SHARE_STAGE, TARGET_SCOPE, TARGET_OU_ID, PARENT_GRANT_ID`
	postgresQuery := fmt.Sprintf(
		`SELECT %s FROM "SHARE_GRANT" WHERE DEPLOYMENT_ID = $1 AND RESOURCE_TYPE = $2 `+
			`AND (TARGET_SCOPE = 'all_roots' OR TARGET_OU_ID IN (%s))`,
		columns, strings.Join(postgresPlaceholders, ","))
	sqliteQuery := fmt.Sprintf(
		`SELECT %s FROM "SHARE_GRANT" WHERE DEPLOYMENT_ID = ? AND RESOURCE_TYPE = ? `+
			`AND (TARGET_SCOPE = 'all_roots' OR TARGET_OU_ID IN (%s))`,
		columns, strings.Join(sqlitePlaceholders, ","))

	query := dbmodel.DBQuery{
		ID:            "SHQ-SHARING_MGT-13",
		Query:         postgresQuery,
		PostgresQuery: postgresQuery,
		SQLiteQuery:   sqliteQuery,
	}

	return query, args
}
