// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"errors"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"

	"github.com/thunder-id/thunderid/internal/flow/common"
	"github.com/thunder-id/thunderid/internal/flow/core"
	"github.com/thunder-id/thunderid/internal/ou"
	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	"github.com/thunder-id/thunderid/internal/system/log"
	"github.com/thunder-id/thunderid/internal/system/security"
)

// OU resolve from strategy values.
const (
	// ouResolveFromCaller indicates that the caller's OU should be used when creating the user.
	ouResolveFromCaller = "caller"
	// ouResolveFromPrompt indicates that the user should be prompted to select an OU.
	ouResolveFromPrompt = "prompt"
	// ouResolveFromPromptAll shows the full OU tree without depending on UserTypeResolver.
	ouResolveFromPromptAll = "promptAll"
)

// ouResolverExecutor resolves the organization unit for a user being onboarded.
type ouResolverExecutor struct {
	providers.Executor
	ouService ou.OrganizationUnitServiceInterface
	logger    *log.Logger
}

// newOUResolverExecutor creates a new OU resolver executor.
func newOUResolverExecutor(
	flowFactory core.FlowFactoryInterface,
	ouService ou.OrganizationUnitServiceInterface,
) *ouResolverExecutor {
	logger := log.GetLogger().With(log.String(log.LoggerKeyComponentName, "OUResolverExecutor"))

	defaultInputs := []providers.Input{
		{
			Ref:        "ou_selection_input",
			Identifier: ouIDKey,
			Type:       providers.InputTypeOUSelect,
			Required:   true,
		},
	}

	base := flowFactory.CreateExecutor(
		ExecutorNameOUResolver,
		providers.ExecutorTypeUtility,
		defaultInputs,
		[]providers.Input{},
		&providers.ExecutorMeta{
			SupportedProperties: []providers.ExecutorSupportedProperties{
				{Property: common.NodePropertyOUResolveFrom},
			},
		},
	)
	return &ouResolverExecutor{
		Executor:  base,
		ouService: ouService,
		logger:    logger,
	}
}

// Execute resolves the organization unit for the user being onboarded.
// It reads the "resolveFrom" node property to determine the OU resolution strategy.
// Supported strategies:
//   - "caller": overrides the default OU with the caller's OU from the security context.
//   - "prompt": checks for child OUs and prompts the user to select one if applicable.
//   - "promptAll": shows the full OU tree from root, independent of UserTypeResolver.
func (e *ouResolverExecutor) Execute(ctx *providers.NodeContext) (*providers.ExecutorResponse, error) {
	logger := e.logger.With(log.String(log.LoggerKeyExecutionID, ctx.ExecutionID))

	execResp := &providers.ExecutorResponse{
		Status:      providers.ExecComplete,
		RuntimeData: make(map[string]string),
	}

	resolveFrom := e.getResolveFrom(ctx)
	if resolveFrom == "" {
		logger.Debug(ctx.Context, "resolveFrom not configured, skipping OU override")
		return execResp, nil
	}

	switch resolveFrom {
	case ouResolveFromCaller:
		return e.resolveFromCaller(ctx, execResp, logger)
	case ouResolveFromPrompt:
		return e.resolveFromPrompt(ctx, logger)
	case ouResolveFromPromptAll:
		return e.resolveFromPromptAll(ctx, logger)
	default:
		logger.Error(ctx.Context, "Unsupported resolveFrom value", log.String("resolveFrom", resolveFrom))
		execResp.Status = providers.ExecFailure
		execResp.Error = tidcommon.CustomServiceError(ErrOUResolutionFailed, tidcommon.I18nMessage{
			Key:          ErrOUResolutionFailed.ErrorDescription.Key,
			DefaultValue: "Unsupported OU resolution strategy: {{param(strategy)}}",
			Params:       map[string]string{"strategy": resolveFrom},
		})
		return execResp, nil
	}
}

// resolveFromCaller resolves the OU from the caller's security context.
func (e *ouResolverExecutor) resolveFromCaller(ctx *providers.NodeContext,
	execResp *providers.ExecutorResponse, logger *log.Logger) (*providers.ExecutorResponse, error) {
	callerOUID := security.GetOUID(ctx.Context)
	if callerOUID == "" {
		logger.Error(ctx.Context, "Caller OU not found in security context")
		execResp.Status = providers.ExecFailure
		execResp.Error = tidcommon.CustomServiceError(ErrOUResolutionFailed, tidcommon.I18nMessage{
			Key:          ErrOUResolutionFailed.ErrorDescription.Key,
			DefaultValue: "Unable to resolve caller organization unit from  context",
		})
		return execResp, nil
	}

	logger.Debug(ctx.Context, "Overriding user OU with caller's OU", log.String("callerOUID", callerOUID))
	execResp.RuntimeData[ouIDKey] = callerOUID

	return execResp, nil
}

// resolveFromPrompt checks whether the user type's OU has child OUs and,
// if so, prompts the admin to select one during the onboarding flow.
func (e *ouResolverExecutor) resolveFromPrompt(ctx *providers.NodeContext,
	logger *log.Logger) (*providers.ExecutorResponse, error) {
	execResp := &providers.ExecutorResponse{
		RuntimeData:    make(map[string]string),
		AdditionalData: make(map[string]string),
		ForwardedData:  make(map[string]interface{}),
	}

	// Read the default OU set by UserTypeResolver.
	// The "prompt" strategy requires UserTypeResolver to have run first and set the defaultOUID.
	parentOUID := ctx.RuntimeData[defaultOUIDKey]
	if parentOUID == "" {
		return nil, errors.New(
			"no defaultOUID in runtime data; UserTypeResolver must run before OUResolver with prompt strategy",
		)
	}

	// The user may submit either a literal OU ID (ouId) or a handle scoped to the default OU's
	// children (ouHandle), but not both — each identifies the same selection unambiguously on its
	// own, so accepting both at once would leave which one takes precedence undefined.
	selectedID, hasID := ctx.UserInputs[ouIDKey]
	hasID = hasID && selectedID != ""
	selectedHandle, hasHandle := ctx.UserInputs[ouHandleKey]
	hasHandle = hasHandle && selectedHandle != ""

	if hasID && hasHandle {
		logger.Debug(ctx.Context, "Both ouId and ouHandle were submitted; exactly one is expected")
		execResp.Status = providers.ExecUserInputRequired
		execResp.Inputs = e.promptInputs()
		execResp.Error = &ErrInvalidOU
		return execResp, nil
	}

	if hasID || hasHandle {
		resolvedOUID := selectedID
		if hasHandle {
			handleOUID, svcErr := e.ouService.GetOrganizationUnitIDByHandle(ctx.Context, selectedHandle, &parentOUID)
			if svcErr != nil {
				if svcErr.Type == tidcommon.ClientErrorType {
					logger.Debug(ctx.Context, "Selected OU handle could not be resolved",
						log.String(ouHandleKey, selectedHandle))
					execResp.Status = providers.ExecUserInputRequired
					execResp.Inputs = e.promptInputs()
					execResp.Error = &ErrInvalidOU
					return execResp, nil
				}
				return nil, errors.New("failed to resolve organization unit by handle: " + svcErr.Error.DefaultValue)
			}
			resolvedOUID = handleOUID
		}

		// Validate that the resolved OU belongs to the parent OU's subtree.
		isDescendant, svcErr := e.ouService.IsParent(ctx.Context, parentOUID, resolvedOUID)
		if svcErr != nil {
			if svcErr.Type == tidcommon.ClientErrorType {
				execResp.Status = providers.ExecUserInputRequired
				execResp.Inputs = e.promptInputs()
				execResp.Error = &ErrInvalidOU
				return execResp, nil
			}

			return nil, errors.New("failed to validate selected organization unit: " + svcErr.Error.DefaultValue)
		}
		if !isDescendant {
			logger.Debug(ctx.Context, "Selected OU is not a descendant of the parent OU",
				log.String(ouIDKey, resolvedOUID),
				log.String("parentOUID", parentOUID))
			execResp.Status = providers.ExecUserInputRequired
			execResp.Inputs = e.promptInputs()
			execResp.Error = &ErrOUNotValidForUserType
			return execResp, nil
		}

		logger.Debug(ctx.Context, "OU selected by user", log.String(ouIDKey, resolvedOUID))
		execResp.RuntimeData[ouIDKey] = resolvedOUID
		execResp.Status = providers.ExecComplete
		return execResp, nil
	}

	// Check if the parent OU has child OUs, fetching enough of them to offer as selectable options.
	children, svcErr := e.ouService.GetOrganizationUnitChildren(ctx.Context, parentOUID, serverconst.MaxPageSize, 0, nil)
	if svcErr != nil {
		return nil, errors.New("failed to check child organization units: " + svcErr.Error.DefaultValue)
	}

	if children.TotalResults == 0 {
		logger.Debug(ctx.Context, "No child OUs found, skipping OU selection")
		execResp.Status = providers.ExecComplete
		return execResp, nil
	}

	// Child OUs exist — prompt the user to select one.
	logger.Debug(ctx.Context, "Child OUs found, requesting OU selection",
		log.String("parentOUID", parentOUID),
		log.Int("totalChildren", children.TotalResults))

	execResp.Status = providers.ExecUserInputRequired

	inputs := e.promptInputs()
	if len(inputs) > 0 {
		input := inputs[0]
		// Offer each child's handle as a selectable option under ouHandle; a caller that already
		// knows the target OU's ID may submit it directly under ouId instead.
		input.Options = organizationUnitHandles(children.OrganizationUnits)
		execResp.Inputs = []providers.Input{input}
		// Forward the root OU ID so the frontend knows where to start the tree picker.
		execResp.AdditionalData[common.DataRootOUID] = parentOUID
		execResp.ForwardedData[common.ForwardedDataKeyInputs] = execResp.Inputs
	}

	return execResp, nil
}

// promptInputs returns the default OU-selection input, keyed by ouHandleKey rather than ouIDKey:
// the "prompt" strategy's frontend submits a child OU's handle, not its ID.
func (e *ouResolverExecutor) promptInputs() []providers.Input {
	defaults := e.GetDefaultInputs()
	if len(defaults) == 0 {
		return defaults
	}
	input := defaults[0]
	input.Identifier = ouHandleKey
	return []providers.Input{input}
}

// resolveFromPromptAll shows the full OU tree from root, allowing selection of any OU at any
// depth. The tree is forwarded as providers.OrganizationUnitTreeNode data (see
// buildOrganizationUnitTree) rather than a flat option list, so the frontend can render real
// expand/collapse navigation. Unlike "prompt", this strategy does not depend on UserTypeResolver
// having run first.
func (e *ouResolverExecutor) resolveFromPromptAll(ctx *providers.NodeContext,
	logger *log.Logger) (*providers.ExecutorResponse, error) {
	execResp := &providers.ExecutorResponse{
		RuntimeData:    make(map[string]string),
		AdditionalData: make(map[string]string),
		ForwardedData:  make(map[string]interface{}),
	}

	// If the user already provided an OU selection, validate and accept it. The tree UI submits the
	// clicked node's real ID directly (ids are globally unique, unlike handles which can repeat
	// across branches), so there is no handle to resolve here — only an existence check.
	if selectedValue, ok := ctx.UserInputs[ouIDKey]; ok && selectedValue != "" {
		exists, existsErr := e.ouService.IsOrganizationUnitExists(ctx.Context, selectedValue)
		if existsErr != nil {
			return nil, errors.New("failed to validate selected organization unit: " + existsErr.Error.DefaultValue)
		}
		if !exists {
			execResp.Status = providers.ExecUserInputRequired
			execResp.Inputs = e.GetDefaultInputs()
			execResp.Error = &ErrOUNotFound
			return execResp, nil
		}

		logger.Debug(ctx.Context, "OU selected by user", log.String(ouIDKey, selectedValue))
		execResp.RuntimeData[ouIDKey] = selectedValue
		execResp.Status = providers.ExecComplete
		return execResp, nil
	}

	// No selection yet — prompt the user with the full OU tree.
	logger.Debug(ctx.Context, "Requesting OU selection from full tree")

	roots, svcErr := fetchAllOrganizationUnits(
		func(limit, offset int) (*providers.OrganizationUnitListResponse, *tidcommon.ServiceError) {
			return e.ouService.GetOrganizationUnitList(ctx.Context, limit, offset, nil)
		})
	if svcErr != nil {
		return nil, errors.New("failed to list organization units: " + svcErr.Error.DefaultValue)
	}

	tree, err := e.buildOrganizationUnitTree(ctx, roots)
	if err != nil {
		return nil, err
	}

	execResp.Status = providers.ExecUserInputRequired

	inputs := e.GetDefaultInputs()
	if len(inputs) > 0 {
		input := inputs[0]
		// Offer the full OU hierarchy as a tree; the user's selection is submitted back as the
		// chosen node's ID (not its handle), validated above by a plain existence check.
		input.Tree = tree
		execResp.Inputs = []providers.Input{input}
		execResp.ForwardedData[common.ForwardedDataKeyInputs] = execResp.Inputs
	}

	return execResp, nil
}

// organizationUnitHandles extracts the handle of each organization unit, in order, for use as a
// selectable input's options.
func organizationUnitHandles(units []providers.OrganizationUnitBasic) []string {
	handles := make([]string, 0, len(units))
	for _, unit := range units {
		handles = append(handles, unit.Handle)
	}
	return handles
}

// buildOrganizationUnitTree recursively descends from the given organization units, fetching each
// one's children, to build the full hierarchy as a set of tree nodes. Depth-first, paging through
// every GetOrganizationUnitChildren result so a parent with more children than MaxPageSize is never
// silently truncated.
func (e *ouResolverExecutor) buildOrganizationUnitTree(
	ctx *providers.NodeContext, units []providers.OrganizationUnitBasic,
) ([]providers.OrganizationUnitTreeNode, error) {
	nodes := make([]providers.OrganizationUnitTreeNode, 0, len(units))
	for _, unit := range units {
		children, svcErr := fetchAllOrganizationUnits(
			func(limit, offset int) (*providers.OrganizationUnitListResponse, *tidcommon.ServiceError) {
				return e.ouService.GetOrganizationUnitChildren(ctx.Context, unit.ID, limit, offset, nil)
			})
		if svcErr != nil {
			return nil, errors.New("failed to list child organization units: " + svcErr.Error.DefaultValue)
		}

		var childNodes []providers.OrganizationUnitTreeNode
		if len(children) > 0 {
			var err error
			childNodes, err = e.buildOrganizationUnitTree(ctx, children)
			if err != nil {
				return nil, err
			}
		}

		nodes = append(nodes, providers.OrganizationUnitTreeNode{
			ID:       unit.ID,
			Handle:   unit.Handle,
			Name:     unit.Name,
			Children: childNodes,
		})
	}
	return nodes, nil
}

// fetchAllOrganizationUnits pages through fetchPage with serverconst.MaxPageSize until every result
// has been collected, so a collection larger than one page is never silently truncated.
func fetchAllOrganizationUnits(
	fetchPage func(limit, offset int) (*providers.OrganizationUnitListResponse, *tidcommon.ServiceError),
) ([]providers.OrganizationUnitBasic, *tidcommon.ServiceError) {
	var all []providers.OrganizationUnitBasic
	offset := 0
	for {
		page, svcErr := fetchPage(serverconst.MaxPageSize, offset)
		if svcErr != nil {
			return nil, svcErr
		}
		all = append(all, page.OrganizationUnits...)
		offset += len(page.OrganizationUnits)
		if len(page.OrganizationUnits) == 0 || offset >= page.TotalResults {
			break
		}
	}
	return all, nil
}

// getResolveFrom retrieves the resolveFrom strategy from the node properties.
func (e *ouResolverExecutor) getResolveFrom(ctx *providers.NodeContext) string {
	if ctx.NodeProperties == nil {
		return ""
	}
	val, ok := ctx.NodeProperties[common.NodePropertyOUResolveFrom]
	if !ok {
		return ""
	}
	strVal, ok := val.(string)
	if !ok {
		return ""
	}
	return strVal
}
