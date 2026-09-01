// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package executor

import (
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/flow/common"
	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	"github.com/thunder-id/thunderid/internal/system/security"
	"github.com/thunder-id/thunderid/tests/mocks/flow/coremock"
	"github.com/thunder-id/thunderid/tests/mocks/oumock"
)

const testParentOUID = "parent-ou-123"
const testChildOUID = "child-ou-456"

type OUResolverExecutorTestSuite struct {
	suite.Suite
	mockFlowFactory *coremock.FlowFactoryInterfaceMock
	mockOUService   *oumock.OrganizationUnitServiceInterfaceMock
	executor        *ouResolverExecutor
}

func (suite *OUResolverExecutorTestSuite) SetupTest() {
	suite.mockFlowFactory = coremock.NewFlowFactoryInterfaceMock(suite.T())
	suite.mockOUService = oumock.NewOrganizationUnitServiceInterfaceMock(suite.T())

	defaultInputs := []providers.Input{
		{
			Ref:        "ou_selection_input",
			Identifier: ouIDKey,
			Type:       providers.InputTypeOUSelect,
			Required:   true,
		},
	}

	suite.mockFlowFactory.On("CreateExecutor",
		ExecutorNameOUResolver,
		providers.ExecutorTypeUtility,
		defaultInputs,
		[]providers.Input{}, mock.Anything).Return(
		newMockExecutor("OUResolverExecutor", providers.ExecutorTypeUtility, defaultInputs, []providers.Input{}))

	suite.executor = newOUResolverExecutor(suite.mockFlowFactory, suite.mockOUService)
}

// --- Caller strategy tests ---

func (suite *OUResolverExecutorTestSuite) TestExecute_ResolveFromCaller_Success() {
	callerOUID := "caller-ou-123"
	httpCtx := context.Background()
	authCtx := security.NewSecurityContextForTest(
		"caller-user", callerOUID, "token",
		[]string{"system"}, nil,
	)
	httpCtx = security.WithSecurityContextTest(httpCtx, authCtx)

	ctx := &providers.NodeContext{
		ExecutionID: "test-flow",
		Context:     httpCtx,
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromCaller,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: "default-ou-456",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, resp.Status)
	assert.Equal(suite.T(), callerOUID, resp.RuntimeData[ouIDKey])
}

func (suite *OUResolverExecutorTestSuite) TestExecute_ResolveFromCaller_CallerOUMissing() {
	httpCtx := context.Background()
	// Security context without OU.
	authCtx := security.NewSecurityContextForTest(
		"caller-user", "", "token",
		[]string{"system"}, nil,
	)
	httpCtx = security.WithSecurityContextTest(httpCtx, authCtx)

	ctx := &providers.NodeContext{
		ExecutionID: "test-flow",
		Context:     httpCtx,
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromCaller,
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecFailure, resp.Status)
	assert.Equal(suite.T(), ErrOUResolutionFailed.Error.DefaultValue, resp.Error.Error.DefaultValue)
}

func (suite *OUResolverExecutorTestSuite) TestExecute_ResolveFromNotConfigured() {
	httpCtx := context.Background()
	authCtx := security.NewSecurityContextForTest(
		"caller-user", "caller-ou-123", "token",
		[]string{"system"}, nil,
	)
	httpCtx = security.WithSecurityContextTest(httpCtx, authCtx)

	ctx := &providers.NodeContext{
		ExecutionID:    "test-flow",
		Context:        httpCtx,
		NodeProperties: map[string]interface{}{},
		RuntimeData: map[string]string{
			defaultOUIDKey: "default-ou-456",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, resp.Status)
	assert.Empty(suite.T(), resp.RuntimeData[ouIDKey])
}

func (suite *OUResolverExecutorTestSuite) TestExecute_UnsupportedResolveFrom() {
	httpCtx := context.Background()

	ctx := &providers.NodeContext{
		ExecutionID: "test-flow",
		Context:     httpCtx,
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: "unsupported",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecFailure, resp.Status)
	assert.Contains(suite.T(), resp.Error.ErrorDescription.String(),
		"Unsupported OU resolution strategy: unsupported")
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PropertyMissing() {
	httpCtx := context.Background()

	ctx := &providers.NodeContext{
		ExecutionID:    "test-flow",
		Context:        httpCtx,
		NodeProperties: map[string]interface{}{},
		RuntimeData: map[string]string{
			defaultOUIDKey: "default-ou-456",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, resp.Status)
	assert.Empty(suite.T(), resp.RuntimeData[ouIDKey])
}

func (suite *OUResolverExecutorTestSuite) TestExecute_NilNodeProperties() {
	httpCtx := context.Background()

	ctx := &providers.NodeContext{
		ExecutionID:    "test-flow",
		Context:        httpCtx,
		NodeProperties: nil,
		RuntimeData: map[string]string{
			defaultOUIDKey: "default-ou-456",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, resp.Status)
	assert.Empty(suite.T(), resp.RuntimeData[ouIDKey])
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PropertyWrongType() {
	httpCtx := context.Background()
	authCtx := security.NewSecurityContextForTest(
		"caller-user", "caller-ou-123", "token",
		[]string{"system"}, nil,
	)
	httpCtx = security.WithSecurityContextTest(httpCtx, authCtx)

	ctx := &providers.NodeContext{
		ExecutionID: "test-flow",
		Context:     httpCtx,
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: 123, // Not a string.
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, resp.Status)
	assert.Empty(suite.T(), resp.RuntimeData[ouIDKey])
}

func (suite *OUResolverExecutorTestSuite) TestExecute_NilContext() {
	ctx := &providers.NodeContext{
		ExecutionID: "test-flow",
		Context:     nil,
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromCaller,
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecFailure, resp.Status)
	assert.Equal(suite.T(), ErrOUResolutionFailed.Error.DefaultValue, resp.Error.Error.DefaultValue)
}

// --- Prompt strategy tests ---

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_NoDefaultOUID_ReturnsError() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{},
		UserInputs:  map[string]string{},
	}

	result, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), result)
	assert.Contains(suite.T(), err.Error(), "no defaultOUID in runtime data")
	suite.mockOUService.AssertNotCalled(
		suite.T(), "GetOrganizationUnitChildren", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	)
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_UserSelectedOU_Valid() {
	parentOUID := testParentOUID
	selectedOUID := testChildOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{
			ouIDKey: selectedOUID,
		},
	}

	suite.mockOUService.On("IsParent", mock.Anything, parentOUID, selectedOUID).
		Return(true, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, result.Status)
	assert.Equal(suite.T(), selectedOUID, result.RuntimeData[ouIDKey])
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_UserSelectedOU_ResolvedByHandle() {
	parentOUID := testParentOUID
	selectedHandle := "acme-corp"
	resolvedOUID := testChildOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{
			ouHandleKey: selectedHandle,
		},
	}

	suite.mockOUService.On("GetOrganizationUnitIDByHandle", mock.Anything, selectedHandle, &parentOUID).
		Return(resolvedOUID, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("IsParent", mock.Anything, parentOUID, resolvedOUID).
		Return(true, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, result.Status)
	assert.Equal(suite.T(), resolvedOUID, result.RuntimeData[ouIDKey])
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_BothOUIDAndOUHandleSubmitted_Rejected() {
	parentOUID := testParentOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{
			ouIDKey:     testChildOUID,
			ouHandleKey: "acme-corp",
		},
	}

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.Equal(suite.T(), &ErrInvalidOU, result.Error)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_UserSelectedOU_NotInSubtree() {
	parentOUID := testParentOUID
	selectedOUID := "unrelated-ou-789"

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{
			ouIDKey: selectedOUID,
		},
	}

	suite.mockOUService.On("IsParent", mock.Anything, parentOUID, selectedOUID).
		Return(false, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.Contains(suite.T(), result.Error.ErrorDescription.DefaultValue,
		ErrOUNotValidForUserType.ErrorDescription.DefaultValue)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_UserSelectedOU_ServerError() {
	parentOUID := testParentOUID
	selectedOUID := testChildOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{
			ouIDKey: selectedOUID,
		},
	}

	svcErr := &tidcommon.ServiceError{
		Type:  tidcommon.ServerErrorType,
		Code:  "OU-50001",
		Error: tidcommon.I18nMessage{Key: "error.test.internal_error", DefaultValue: "internal error"},
	}
	suite.mockOUService.On("IsParent", mock.Anything, parentOUID, selectedOUID).
		Return(false, svcErr)

	result, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), result)
	assert.Contains(suite.T(), err.Error(), "failed to validate selected organization unit")
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_UserSelectedOU_ClientError() {
	parentOUID := testParentOUID
	selectedOUID := testChildOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{
			ouIDKey: selectedOUID,
		},
	}

	svcErr := &tidcommon.ServiceError{
		Type:  tidcommon.ClientErrorType,
		Code:  "OU-40001",
		Error: tidcommon.I18nMessage{Key: "error.test.not_found", DefaultValue: "not found"},
	}
	suite.mockOUService.On("IsParent", mock.Anything, parentOUID, selectedOUID).
		Return(false, svcErr)

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.Contains(suite.T(), result.Error.ErrorDescription.DefaultValue, "not valid")
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_NoChildOUs_Skips() {
	parentOUID := testParentOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{},
	}

	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, parentOUID, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{TotalResults: 0}, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, result.Status)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_HasChildOUs_RequestsInput() {
	parentOUID := testParentOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{},
	}

	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, parentOUID, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults: 3,
			OrganizationUnits: []providers.OrganizationUnitBasic{
				{Handle: "acme-corp"},
				{Handle: "beta-inc"},
				{Handle: "gamma-llc"},
			},
		}, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.Equal(suite.T(), parentOUID, result.AdditionalData[common.DataRootOUID])
	assert.NotEmpty(suite.T(), result.Inputs)
	assert.Equal(suite.T(), ouHandleKey, result.Inputs[0].Identifier)
	assert.Equal(suite.T(), providers.InputTypeOUSelect, result.Inputs[0].Type)
	assert.Equal(suite.T(), []string{"acme-corp", "beta-inc", "gamma-llc"}, result.Inputs[0].Options)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_Prompt_GetChildrenError_ReturnsError() {
	parentOUID := testParentOUID

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPrompt,
		},
		RuntimeData: map[string]string{
			defaultOUIDKey: parentOUID,
		},
		UserInputs: map[string]string{},
	}

	svcErr := &tidcommon.ServiceError{
		Type:  tidcommon.ServerErrorType,
		Code:  "OU-50001",
		Error: tidcommon.I18nMessage{Key: "error.test.internal_error", DefaultValue: "internal error"},
	}
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, parentOUID, serverconst.MaxPageSize, 0, mock.Anything).
		Return((*providers.OrganizationUnitListResponse)(nil), svcErr)

	result, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), result)
	assert.Contains(suite.T(), err.Error(), "failed to check child organization units")
	suite.mockOUService.AssertExpectations(suite.T())
}

// --- PromptAll strategy tests ---

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_FirstInvocation_RequestsInput() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs:  map[string]string{},
	}

	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults: 2,
			OrganizationUnits: []providers.OrganizationUnitBasic{
				{ID: "ou-acme", Handle: "acme-corp", Name: "Acme Corp"},
				{ID: "ou-beta", Handle: "beta-inc", Name: "Beta Inc"},
			},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-acme", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-beta", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.NotEmpty(suite.T(), result.Inputs)
	assert.Equal(suite.T(), ouIDKey, result.Inputs[0].Identifier)
	assert.Equal(suite.T(), providers.InputTypeOUSelect, result.Inputs[0].Type)
	assert.Empty(suite.T(), result.Inputs[0].Options)
	assert.Equal(suite.T(), []providers.OrganizationUnitTreeNode{
		{ID: "ou-acme", Handle: "acme-corp", Name: "Acme Corp"},
		{ID: "ou-beta", Handle: "beta-inc", Name: "Beta Inc"},
	}, result.Inputs[0].Tree)
	// PromptAll should NOT set DataRootOUID (frontend shows full tree)
	assert.Empty(suite.T(), result.AdditionalData[common.DataRootOUID])
	suite.mockOUService.AssertNotCalled(suite.T(), "IsOrganizationUnitExists", mock.Anything, mock.Anything)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_NestedChildren_BuildsFullTree() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs:  map[string]string{},
	}

	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults: 1,
			OrganizationUnits: []providers.OrganizationUnitBasic{
				{ID: "ou-root", Handle: "root-org", Name: "Root Org"},
			},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-root", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults: 1,
			OrganizationUnits: []providers.OrganizationUnitBasic{
				{ID: "ou-child", Handle: "child-org", Name: "Child Org"},
			},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-child", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults: 1,
			OrganizationUnits: []providers.OrganizationUnitBasic{
				{ID: "ou-grandchild", Handle: "grandchild-org", Name: "Grandchild Org"},
			},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-grandchild", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.Equal(suite.T(), []providers.OrganizationUnitTreeNode{
		{
			ID: "ou-root", Handle: "root-org", Name: "Root Org",
			Children: []providers.OrganizationUnitTreeNode{
				{
					ID: "ou-child", Handle: "child-org", Name: "Child Org",
					Children: []providers.OrganizationUnitTreeNode{
						{ID: "ou-grandchild", Handle: "grandchild-org", Name: "Grandchild Org"},
					},
				},
			},
		},
	}, result.Inputs[0].Tree)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_ChildrenListError_ReturnsError() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs:  map[string]string{},
	}

	svcErr := &tidcommon.ServiceError{
		Type:  tidcommon.ServerErrorType,
		Code:  "OU-50001",
		Error: tidcommon.I18nMessage{Key: "error.test.internal_error", DefaultValue: "internal error"},
	}
	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults: 1,
			OrganizationUnits: []providers.OrganizationUnitBasic{
				{ID: "ou-root", Handle: "root-org", Name: "Root Org"},
			},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-root", serverconst.MaxPageSize, 0, mock.Anything).
		Return((*providers.OrganizationUnitListResponse)(nil), svcErr)

	result, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), result)
	assert.Contains(suite.T(), err.Error(), "failed to list child organization units")
	suite.mockOUService.AssertExpectations(suite.T())
}

// A root collection larger than one page must not be truncated: every page is fetched until
// TotalResults is reached.
func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_PaginatesRootsAcrossMultiplePages() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs:  map[string]string{},
	}

	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults:      2,
			OrganizationUnits: []providers.OrganizationUnitBasic{{ID: "ou-page1", Handle: "page1", Name: "Page 1"}},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 1, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults:      2,
			OrganizationUnits: []providers.OrganizationUnitBasic{{ID: "ou-page2", Handle: "page2", Name: "Page 2"}},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-page1", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-page2", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), []providers.OrganizationUnitTreeNode{
		{ID: "ou-page1", Handle: "page1", Name: "Page 1"},
		{ID: "ou-page2", Handle: "page2", Name: "Page 2"},
	}, result.Inputs[0].Tree, "both pages of root organization units must be present in the tree")
	suite.mockOUService.AssertExpectations(suite.T())
}

// A single parent's children collection larger than one page must also not be truncated.
func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_PaginatesChildrenAcrossMultiplePages() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs:  map[string]string{},
	}

	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults:      1,
			OrganizationUnits: []providers.OrganizationUnitBasic{{ID: "ou-root", Handle: "root-org", Name: "Root Org"}},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-root", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults:      2,
			OrganizationUnits: []providers.OrganizationUnitBasic{{ID: "ou-child1", Handle: "child1", Name: "Child 1"}},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-root", serverconst.MaxPageSize, 1, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{
			TotalResults:      2,
			OrganizationUnits: []providers.OrganizationUnitBasic{{ID: "ou-child2", Handle: "child2", Name: "Child 2"}},
		}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-child1", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))
	suite.mockOUService.On("GetOrganizationUnitChildren", mock.Anything, "ou-child2", serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), []providers.OrganizationUnitTreeNode{
		{
			ID: "ou-root", Handle: "root-org", Name: "Root Org",
			Children: []providers.OrganizationUnitTreeNode{
				{ID: "ou-child1", Handle: "child1", Name: "Child 1"},
				{ID: "ou-child2", Handle: "child2", Name: "Child 2"},
			},
		},
	}, result.Inputs[0].Tree, "both pages of the root's children must be present in the tree")
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_ListError_ReturnsError() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs:  map[string]string{},
	}

	svcErr := &tidcommon.ServiceError{
		Type:  tidcommon.ServerErrorType,
		Code:  "OU-50001",
		Error: tidcommon.I18nMessage{Key: "error.test.internal_error", DefaultValue: "internal error"},
	}
	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 0, mock.Anything).
		Return((*providers.OrganizationUnitListResponse)(nil), svcErr)

	result, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), result)
	assert.Contains(suite.T(), err.Error(), "failed to list organization units")
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_ValidOUSelection() {
	selectedOUID := "valid-ou-123"

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs: map[string]string{
			ouIDKey: selectedOUID,
		},
	}

	suite.mockOUService.On("IsOrganizationUnitExists", mock.Anything, selectedOUID).
		Return(true, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecComplete, result.Status)
	assert.Equal(suite.T(), selectedOUID, result.RuntimeData[ouIDKey])
	suite.mockOUService.AssertNotCalled(suite.T(), "GetOrganizationUnitIDByHandle", mock.Anything, mock.Anything, mock.Anything)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_HandleSubmission_NotResolved() {
	// promptAll's tree submits real IDs, never handles; a handle-looking value is never even
	// tried against GetOrganizationUnitIDByHandle — it goes straight to the existence check,
	// which correctly reports it as not found since it isn't a real OU ID.
	selectedHandle := "acme-corp"

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs: map[string]string{
			ouIDKey: selectedHandle,
		},
	}

	suite.mockOUService.On("IsOrganizationUnitExists", mock.Anything, selectedHandle).
		Return(false, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.Equal(suite.T(), ErrOUNotFound.ErrorDescription.DefaultValue, result.Error.ErrorDescription.DefaultValue)
	suite.mockOUService.AssertNotCalled(suite.T(), "GetOrganizationUnitIDByHandle", mock.Anything, mock.Anything, mock.Anything)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_NonExistentOU() {
	selectedOUID := "nonexistent-ou-999"

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs: map[string]string{
			ouIDKey: selectedOUID,
		},
	}

	suite.mockOUService.On("IsOrganizationUnitExists", mock.Anything, selectedOUID).
		Return(false, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.Equal(suite.T(), ErrOUNotFound.ErrorDescription.DefaultValue, result.Error.ErrorDescription.DefaultValue)
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_ServiceError() {
	selectedOUID := "some-ou-123"

	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs: map[string]string{
			ouIDKey: selectedOUID,
		},
	}

	svcErr := &tidcommon.ServiceError{
		Type:  tidcommon.ServerErrorType,
		Code:  "OU-50001",
		Error: tidcommon.I18nMessage{Key: "error.test.internal_error", DefaultValue: "internal error"},
	}
	suite.mockOUService.On("IsOrganizationUnitExists", mock.Anything, selectedOUID).
		Return(false, svcErr)

	result, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), result)
	assert.Contains(suite.T(), err.Error(), "failed to validate selected organization unit")
	suite.mockOUService.AssertExpectations(suite.T())
}

func (suite *OUResolverExecutorTestSuite) TestExecute_PromptAll_EmptyOUInput_RequestsInput() {
	ctx := &providers.NodeContext{
		ExecutionID: "flow-123",
		NodeProperties: map[string]interface{}{
			common.NodePropertyOUResolveFrom: ouResolveFromPromptAll,
		},
		RuntimeData: map[string]string{},
		UserInputs: map[string]string{
			ouIDKey: "",
		},
	}

	suite.mockOUService.On("GetOrganizationUnitList", mock.Anything, serverconst.MaxPageSize, 0, mock.Anything).
		Return(&providers.OrganizationUnitListResponse{}, (*tidcommon.ServiceError)(nil))

	result, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), providers.ExecUserInputRequired, result.Status)
	assert.NotEmpty(suite.T(), result.Inputs)
	assert.Empty(suite.T(), result.Inputs[0].Tree)
	suite.mockOUService.AssertNotCalled(suite.T(), "IsOrganizationUnitExists", mock.Anything, mock.Anything)
}

func TestOUResolverExecutorSuite(t *testing.T) {
	suite.Run(t, new(OUResolverExecutorTestSuite))
}
