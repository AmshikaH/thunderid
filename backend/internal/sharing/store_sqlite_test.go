// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package sharing

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	_ "modernc.org/sqlite"
)

// SQLiteVisibilityTestSuite runs buildRelevantGrantsQuery's generated SQL against a real,
// in-memory SQLite database. Every other sharing test mocks QueryContext, which validates that
// the store calls the DB client with the right arguments but never proves the generated SQL text
// itself is valid, or that the dynamic IN (...) placeholder list lines up correctly with SQLite's
// positional '?' binding. This suite exercises the real driver to close that gap. The actual
// coverage-evaluation logic (chain walking, exclusion checks, chain integrity) lives in
// evaluateChainVisibility and is covered by service_test.go's pure-Go tests instead, since it has
// no SQL of its own to validate.
type SQLiteVisibilityTestSuite struct {
	suite.Suite
	db *sql.DB
}

func TestSQLiteVisibilityTestSuite(t *testing.T) {
	suite.Run(t, new(SQLiteVisibilityTestSuite))
}

func (suite *SQLiteVisibilityTestSuite) SetupTest() {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(suite.T(), err)
	suite.db = db

	_, err = db.Exec(`
		CREATE TABLE "SHARE_GRANT" (
			DEPLOYMENT_ID   VARCHAR(255) NOT NULL,
			ID              VARCHAR(36) PRIMARY KEY,
			RESOURCE_TYPE   VARCHAR(50) NOT NULL,
			RESOURCE_ID     VARCHAR(36) NOT NULL,
			OWNING_OU_ID    VARCHAR(36) NOT NULL,
			SHARE_STAGE     VARCHAR(7) NOT NULL,
			TARGET_SCOPE    VARCHAR(12) NOT NULL,
			TARGET_OU_ID    VARCHAR(36),
			PARENT_GRANT_ID VARCHAR(36),
			CREATED_AT      TEXT DEFAULT (datetime('now')),
			UPDATED_AT      TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE "SHARE_GRANT_EXCLUSION" (
			DEPLOYMENT_ID  VARCHAR(255) NOT NULL,
			GRANT_ID       VARCHAR(36) NOT NULL,
			EXCLUDED_OU_ID VARCHAR(36) NOT NULL,
			PRIMARY KEY (GRANT_ID, EXCLUDED_OU_ID, DEPLOYMENT_ID)
		);
		CREATE TABLE "SHARE_GRANT_EDITABLE_FIELD" (
			DEPLOYMENT_ID VARCHAR(255) NOT NULL,
			GRANT_ID      VARCHAR(36) NOT NULL,
			FIELD_KEY     VARCHAR(100) NOT NULL,
			PRIMARY KEY (GRANT_ID, FIELD_KEY, DEPLOYMENT_ID)
		);
	`)
	require.NoError(suite.T(), err)
}

func (suite *SQLiteVisibilityTestSuite) TearDownTest() {
	suite.NoError(suite.db.Close())
}

// testDeploymentID is used for every grant inserted in this suite; TestDeploymentIsolation
// queries a different, literal deployment id directly to prove isolation.
const testDeploymentID = "dep1"

func (suite *SQLiteVisibilityTestSuite) insertGrant(grant ShareGrant) {
	_, err := suite.db.Exec(queryCreateShareGrant.GetQuery("sqlite"),
		grant.ID, string(grant.ResourceType), grant.ResourceID, grant.OwningOUID, string(grant.Stage),
		string(grant.TargetScope), nullableString(grant.TargetOUID), nullableString(grant.ParentGrantID),
		testDeploymentID)
	require.NoError(suite.T(), err)
	for _, excluded := range grant.ExcludedOUIDs {
		_, err := suite.db.Exec(queryInsertGrantExclusion.GetQuery("sqlite"), grant.ID, excluded, testDeploymentID)
		require.NoError(suite.T(), err)
	}
	for _, fieldKey := range grant.EditableFields {
		_, err := suite.db.Exec(queryInsertGrantEditableField.GetQuery("sqlite"), grant.ID, fieldKey, testDeploymentID)
		require.NoError(suite.T(), err)
	}
}

// relevantGrantIDs returns the IDs of every "role" grant buildRelevantGrantsQuery considers
// relevant to chainOUIDs, using the real SQLite driver end to end.
func (suite *SQLiteVisibilityTestSuite) relevantGrantIDs(chainOUIDs []string, deploymentID string) []string {
	query, args := buildRelevantGrantsQuery("role", chainOUIDs, deploymentID)
	rows, err := suite.db.Query(query.GetQuery("sqlite"), args...)
	require.NoError(suite.T(), err)
	defer func() { require.NoError(suite.T(), rows.Close()) }()

	var ids []string
	for rows.Next() {
		var id, resourceType, resourceID, owningOUID, stage, targetScope string
		var targetOUID, parentGrantID sql.NullString
		require.NoError(suite.T(), rows.Scan(
			&id, &resourceType, &resourceID, &owningOUID, &stage, &targetScope, &targetOUID, &parentGrantID))
		ids = append(ids, id)
	}
	return ids
}

func (suite *SQLiteVisibilityTestSuite) TestRelevantGrants_AllRoots_AlwaysReturned() {
	suite.insertGrant(ShareGrant{
		ID: "g1", ResourceType: "role", ResourceID: "role1", OwningOUID: "owner",
		Stage: StageShare, TargetScope: TargetScopeAllRoots,
	})

	// An all_roots grant is a candidate for any chain, since it can apply to any root.
	suite.Equal([]string{"g1"}, suite.relevantGrantIDs([]string{"root1", "child1"}, testDeploymentID))
}

func (suite *SQLiteVisibilityTestSuite) TestRelevantGrants_MatchesTargetOUIDInChain() {
	suite.insertGrant(ShareGrant{
		ID: "g1", ResourceType: "role", ResourceID: "role1", OwningOUID: "owner",
		Stage: StageReshare, TargetScope: TargetScopeAllChildren, TargetOUID: "root1",
		ExcludedOUIDs: []string{"excludedChild"},
	})

	suite.Equal([]string{"g1"}, suite.relevantGrantIDs([]string{"root1", "child1"}, testDeploymentID))
	// A chain that never passes through root1 has no reason to fetch this grant.
	suite.Empty(suite.relevantGrantIDs([]string{"otherRoot", "otherChild"}, testDeploymentID))
}

func (suite *SQLiteVisibilityTestSuite) TestRelevantGrants_ExclusionRowsPersistedAlongsideGrant() {
	suite.insertGrant(ShareGrant{
		ID: "g1", ResourceType: "role", ResourceID: "role1", OwningOUID: "owner",
		Stage: StageReshare, TargetScope: TargetScopeAllChildren, TargetOUID: "root1",
		ExcludedOUIDs: []string{"excludedChild", "excludedChild2"},
	})

	ids := suite.relevantGrantIDs([]string{"root1"}, testDeploymentID)
	require.Equal(suite.T(), []string{"g1"}, ids)

	rows, err := suite.db.Query(queryListGrantExclusions.GetQuery("sqlite"), "g1", testDeploymentID)
	require.NoError(suite.T(), err)
	defer func() { require.NoError(suite.T(), rows.Close()) }()
	var excluded []string
	for rows.Next() {
		var id string
		require.NoError(suite.T(), rows.Scan(&id))
		excluded = append(excluded, id)
	}
	suite.ElementsMatch([]string{"excludedChild", "excludedChild2"}, excluded)
}

func (suite *SQLiteVisibilityTestSuite) TestDeploymentIsolation() {
	suite.insertGrant(ShareGrant{
		ID: "g1", ResourceType: "role", ResourceID: "role1", OwningOUID: "owner",
		Stage: StageShare, TargetScope: TargetScopeAllRoots,
	})

	// A different deployment must not see testDeploymentID's grant.
	suite.Empty(suite.relevantGrantIDs([]string{"root1"}, "other-deployment"))
}

// editableFields returns the field keys persisted for grantID, using the real SQLite driver.
func (suite *SQLiteVisibilityTestSuite) editableFields(grantID string) []string {
	rows, err := suite.db.Query(queryListGrantEditableFields.GetQuery("sqlite"), grantID, testDeploymentID)
	require.NoError(suite.T(), err)
	defer func() { require.NoError(suite.T(), rows.Close()) }()

	var keys []string
	for rows.Next() {
		var key string
		require.NoError(suite.T(), rows.Scan(&key))
		keys = append(keys, key)
	}
	return keys
}

func (suite *SQLiteVisibilityTestSuite) TestEditableFields_PersistedAlongsideGrant() {
	suite.insertGrant(ShareGrant{
		ID: "g1", ResourceType: "role", ResourceID: "role1", OwningOUID: "owner",
		Stage: StageShare, TargetScope: TargetScopeAllRoots,
		EditableFields: []string{"assignments.user", "assignments.group"},
	})

	suite.ElementsMatch([]string{"assignments.user", "assignments.group"}, suite.editableFields("g1"))
}

func (suite *SQLiteVisibilityTestSuite) TestEditableFields_EmptyWhenNoneMaterialized() {
	suite.insertGrant(ShareGrant{
		ID: "g1", ResourceType: "role", ResourceID: "role1", OwningOUID: "owner",
		Stage: StageShare, TargetScope: TargetScopeAllRoots,
	})

	suite.Empty(suite.editableFields("g1"))
}
