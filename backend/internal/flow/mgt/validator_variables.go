// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowmgt

import (
	"context"
	"fmt"
	"strings"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/thunder-id/thunderid/internal/flow/common"
	"github.com/thunder-id/thunderid/internal/flow/flowvar"
	"github.com/thunder-id/thunderid/internal/system/security"
)

// identityProviderVendors are the connection vendors a connection variable of the identity provider category may name.
var identityProviderVendors = []string{"google", "github", "oidc", "oauth"}

// IDPReader reads the connections a connection variable names.
type IDPReader interface {
	GetIdentityProvider(ctx context.Context, idpID string) (*providers.IDPDTO, *tidcommon.ServiceError)
}

// validateVariables checks the variables a flow declares and the placeholders that use them:
//   - a VARIABLES node declares at least one variable, goes on to a next node and does nothing else;
//   - each declaration is well formed with a valid default, and a variable is declared once across all the nodes;
//   - the default of a connection variable is a connection of the kind the variable takes;
//   - every {{var(name)}} in the properties of a step, or in the text of a prompt, names a declared variable, and
//     sits in a step that every route from the start reaches only through the node that declares it.
func (v *flowValidator) validateVariables(
	ctx context.Context, flowDef *FlowDefinition,
) *tidcommon.ServiceError {
	nodes := flowDef.Nodes
	declaredAt := map[string]string{}
	for i := range nodes {
		node := &nodes[i]
		if node.Type != string(common.NodeTypeVariables) {
			continue
		}
		if err := validateVariablesNodeFormat(node); err != nil {
			return variableError(node.ID, err.Error())
		}
		if err := flowvar.ValidateDefinitions(node.Inputs); err != nil {
			return variableError(node.ID, err.Error())
		}
		for _, def := range node.Inputs {
			if other, seen := declaredAt[def.Identifier]; seen {
				return variableError(node.ID, fmt.Sprintf(
					"variable %q is already declared on node '%s'", def.Identifier, other))
			}
			declaredAt[def.Identifier] = node.ID
			if svcErr := v.validateConnectionVariable(ctx, def, flowDef.OUID); svcErr != nil {
				return svcErr
			}
		}
	}
	return validateVariableReferences(nodes, declaredAt)
}

// validateVariablesNodeFormat checks that a VARIABLES node declares variables and carries nothing a node that shows
// and asks for nothing could not use.
func validateVariablesNodeFormat(node *providers.NodeDefinition) error {
	switch {
	case len(node.Inputs) == 0:
		return fmt.Errorf("a VARIABLES node declares at least one variable in 'inputs'")
	case node.OnSuccess == "":
		return fmt.Errorf("a VARIABLES node must have onSuccess")
	case node.OnFailure != "" || node.OnIncomplete != "" || node.Next != "":
		return fmt.Errorf("a VARIABLES node goes on through onSuccess only, with no onFailure, onIncomplete or next")
	case node.Executor != nil || len(node.Prompts) > 0 || len(node.Properties) > 0 || node.Meta != nil ||
		node.Flow != nil || node.Condition != nil || node.Variant != "" || node.Message != "":
		return fmt.Errorf(
			"a VARIABLES node has no executor, prompts, properties, meta, flow, condition, variant or message")
	}
	return nil
}

// validateConnectionVariable checks the declaration of a connection variable: it asks for a category and kinds of
// connection a value can be checked against, and its default is a connection of the organization unit that owns the
// flow, of a kind it takes.
func (v *flowValidator) validateConnectionVariable(
	ctx context.Context, def providers.VariableDefinition, ownerOUID string,
) *tidcommon.ServiceError {
	if def.Type != providers.InputTypeConnectionSelect {
		return nil
	}
	if def.Category != flowvar.CategoryIdentityProvider {
		return variableError("", fmt.Sprintf("variable %q: the connection category %q is not supported",
			def.Identifier, def.Category))
	}
	for _, t := range def.ConnectionTypes {
		if !containsFold(identityProviderVendors, t) {
			return variableError("", fmt.Sprintf(
				"variable %q: %q is not an identity provider connection type", def.Identifier, t))
		}
	}
	if v.idpReader == nil {
		return nil
	}
	raw, err := flowvar.DefaultRaw(def)
	if err != nil {
		return variableError("", fmt.Sprintf("variable %q: its default %s", def.Identifier, err.Error()))
	}
	if reason := checkConnection(ctx, v.idpReader, def, raw, ownerOUID); reason != "" {
		return variableError("", fmt.Sprintf("variable %q: its default %s", def.Identifier, reason))
	}
	return nil
}

// checkConnection checks that a connection variable's value names an existing identity provider connection of a kind
// the variable takes, which belongs to the organization unit. It returns what is wrong, or nothing, and reports a
// failure to read the connection as the error it is.
func checkConnection(
	ctx context.Context, reader IDPReader, def providers.VariableDefinition, connectionID, ouID string,
) string {
	idp, svcErr := reader.GetIdentityProvider(security.WithRuntimeContext(ctx), connectionID)
	if svcErr != nil {
		return "is not an existing connection"
	}
	vendor := strings.ToLower(string(idp.Type))
	if !containsFold(identityProviderVendors, vendor) {
		return "is not an identity provider connection"
	}
	if len(def.ConnectionTypes) > 0 && !containsFold(def.ConnectionTypes, vendor) {
		return fmt.Sprintf("is a %s connection, which the variable does not take", vendor)
	}
	if idp.OUID != ouID {
		return "is not a connection of the organization unit"
	}
	return ""
}

// validateVariableReferences checks every {{var(name)}} of the steps: the variable is declared, and the step can only
// be reached through the node that declares it.
func validateVariableReferences(
	nodes []providers.NodeDefinition, declaredAt map[string]string,
) *tidcommon.ServiceError {
	var startID string
	for i := range nodes {
		if nodes[i].Type == string(common.NodeTypeStart) {
			startID = nodes[i].ID
			break
		}
	}
	adjacency := buildAdjacencyList(nodes)
	// reachableWithout holds, for a declaring node, the nodes a route from the start can reach without passing it.
	reachableWithout := map[string]map[string]bool{}

	for i := range nodes {
		node := &nodes[i]
		refs := flowvar.References(node.Properties)
		for _, id := range refs {
			declaringNode, declared := declaredAt[id]
			if !declared {
				return variableError(node.ID, fmt.Sprintf(
					"it refers to the variable %q, which the flow does not declare", id))
			}
			if _, done := reachableWithout[declaringNode]; !done {
				reachableWithout[declaringNode] = reachableAvoiding(adjacency, startID, declaringNode)
			}
			if node.ID == declaringNode || reachableWithout[declaringNode][node.ID] {
				return variableError(node.ID, fmt.Sprintf(
					"it refers to the variable %q, which the node '%s' declares, but a route reaches it "+
						"without passing that node", id, declaringNode))
			}
		}
	}
	return nil
}

// reachableAvoiding returns the nodes a route from start can reach without passing the avoided node.
func reachableAvoiding(adjacency map[string][]string, start, avoid string) map[string]bool {
	visited := map[string]bool{}
	if start == avoid {
		return visited
	}
	visited[start] = true
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[current] {
			if next == avoid || visited[next] {
				continue
			}
			visited[next] = true
			queue = append(queue, next)
		}
	}
	return visited
}

// variableError is the error for a flow whose variables are not valid.
func variableError(nodeID, reason string) *tidcommon.ServiceError {
	if nodeID == "" {
		return tidcommon.CustomServiceError(ErrorInvalidFlowData, tidcommon.I18nMessage{
			Key:          "error.flowmgtservice.invalid_variable_description",
			DefaultValue: "{{param(reason)}}",
			Params:       map[string]string{"reason": reason},
		})
	}
	return tidcommon.CustomServiceError(ErrorInvalidFlowData, tidcommon.I18nMessage{
		Key:          "error.flowmgtservice.invalid_variables_description",
		DefaultValue: "Node '{{param(nodeID)}}': {{param(reason)}}",
		Params:       map[string]string{"nodeID": nodeID, "reason": reason},
	})
}

// containsFold reports whether a list holds a value, ignoring case.
func containsFold(list []string, v string) bool {
	for _, e := range list {
		if strings.EqualFold(e, v) {
			return true
		}
	}
	return false
}
