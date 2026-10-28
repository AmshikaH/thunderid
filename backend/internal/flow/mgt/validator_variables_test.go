// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowmgt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

const endNode = "end"

func variablesNode(id, next string, defs ...providers.VariableDefinition) providers.NodeDefinition {
	return providers.NodeDefinition{ID: id, Type: "VARIABLES", OnSuccess: next, Inputs: defs}
}

func booleanVariable(id string) providers.VariableDefinition {
	return providers.VariableDefinition{Identifier: id, Type: providers.InputTypeBoolean, DefaultValue: false}
}

func taskUsing(id, next, variable string) providers.NodeDefinition {
	return providers.NodeDefinition{
		ID: id, Type: "TASK_EXECUTION", OnSuccess: next,
		Executor:   &providers.ExecutorDefinition{Name: "GoogleOIDCAuthExecutor"},
		Properties: map[string]interface{}{"allowRegistrationWithExistingUser": "{{var(" + variable + ")}}"},
	}
}

// linearFlow is START -> a -> b -> c -> END with the given steps in place of a, b and c.
func linearFlow(steps ...providers.NodeDefinition) *FlowDefinition {
	nodes := make([]providers.NodeDefinition, 0, len(steps)+2)
	nodes = append(nodes, providers.NodeDefinition{ID: "start", Type: "START", OnSuccess: steps[0].ID})
	nodes = append(nodes, steps...)
	nodes = append(nodes, providers.NodeDefinition{ID: endNode, Type: "END"})
	return &FlowDefinition{Nodes: nodes}
}

func reasonOf(t *testing.T, svcErr *tidcommon.ServiceError) string {
	t.Helper()
	require.NotNil(t, svcErr)
	require.Equal(t, ErrorInvalidFlowData.Code, svcErr.Code)
	return svcErr.ErrorDescription.Params["reason"]
}

func TestValidateVariables(t *testing.T) {
	v := &flowValidator{}
	ctx := context.Background()

	t.Run("a flow with no variables is valid", func(t *testing.T) {
		require.Nil(t, v.validateVariables(ctx, &FlowDefinition{Nodes: []providers.NodeDefinition{
			{ID: "start", Type: "START", OnSuccess: "end"}, {ID: "end", Type: "END"}}}))
	})
	t.Run("declared variables and a step that uses one later", func(t *testing.T) {
		flow := linearFlow(
			variablesNode("vars", "mid", booleanVariable("allowSignup")),
			providers.NodeDefinition{ID: "mid", Type: "START", OnSuccess: "idp"},
			taskUsing("idp", "end", "allowSignup"),
		)
		require.Nil(t, v.validateVariables(ctx, flow), "the step need not be the next one")
	})
	t.Run("a placeholder naming an undeclared variable", func(t *testing.T) {
		flow := linearFlow(
			variablesNode("vars", "idp", booleanVariable("other")), taskUsing("idp", "end", "allowSignup"))
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flow)), "allowSignup")
	})
	t.Run("a placeholder in a flow with no variables node", func(t *testing.T) {
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, linearFlow(taskUsing("idp", "end", "x")))), "x")
	})
	t.Run("a placeholder in the text a prompt shows is not a reference", func(t *testing.T) {
		prompt := providers.NodeDefinition{
			ID: "p", Type: "PROMPT", Next: "end",
			Meta: map[string]interface{}{"components": []interface{}{
				map[string]interface{}{"label": "Write to {{var(supportEmail)}}"}}},
		}
		require.Nil(t, v.validateVariables(ctx, linearFlow(prompt)))
	})
	t.Run("a step before the node that declares the variable", func(t *testing.T) {
		flow := linearFlow(
			taskUsing("idp", "vars", "allowSignup"), variablesNode("vars", "end", booleanVariable("allowSignup")))
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flow)), "without passing that node")
	})
	t.Run("a step that one route reaches around the node", func(t *testing.T) {
		flow := &FlowDefinition{Nodes: []providers.NodeDefinition{
			{ID: "start", Type: "START", OnSuccess: "choose"},
			{
				ID: "choose", Type: "PROMPT",
				Prompts: []providers.PromptDefinition{
					{Action: &providers.ActionDefinition{Ref: "a", NextNode: "vars"}},
					{Action: &providers.ActionDefinition{Ref: "b", NextNode: "idp"}},
				},
			},
			variablesNode("vars", "idp", booleanVariable("allowSignup")),
			taskUsing("idp", "end", "allowSignup"),
			{ID: "end", Type: "END"},
		}}
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flow)), "without passing that node")
	})
	t.Run("a step on every branch after the node", func(t *testing.T) {
		flow := &FlowDefinition{Nodes: []providers.NodeDefinition{
			{ID: "start", Type: "START", OnSuccess: "vars"},
			variablesNode("vars", "choose", booleanVariable("allowSignup")),
			{
				ID: "choose", Type: "PROMPT",
				Prompts: []providers.PromptDefinition{
					{Action: &providers.ActionDefinition{Ref: "a", NextNode: "one"}},
					{Action: &providers.ActionDefinition{Ref: "b", NextNode: "two"}},
				},
			},
			taskUsing("one", "end", "allowSignup"),
			taskUsing("two", "end", "allowSignup"),
			{ID: "end", Type: "END"},
		}}
		require.Nil(t, v.validateVariables(ctx, flow))
	})
	t.Run("two nodes each declaring variables", func(t *testing.T) {
		flow := linearFlow(
			variablesNode("a", "b", booleanVariable("first")),
			variablesNode("b", "idp", booleanVariable("second")),
			taskUsing("idp", "end", "second"),
		)
		require.Nil(t, v.validateVariables(ctx, flow))

		later := linearFlow(
			variablesNode("a", "idp", booleanVariable("first")),
			taskUsing("idp", "b", "second"),
			variablesNode("b", "end", booleanVariable("second")),
		)
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, later)), "without passing that node")
	})
	t.Run("two nodes declaring the same variable", func(t *testing.T) {
		flow := linearFlow(
			variablesNode("a", "b", booleanVariable("x")), variablesNode("b", "end", booleanVariable("x")))
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flow)), "already declared")
	})
	t.Run("a variable with no default", func(t *testing.T) {
		bad := booleanVariable("allowSignup")
		bad.DefaultValue = nil
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, linearFlow(variablesNode("vars", "end", bad)))),
			"no default")
	})
	t.Run("a duplicate variable on one node", func(t *testing.T) {
		flow := linearFlow(variablesNode("vars", "end", booleanVariable("x"), booleanVariable("x")))
		require.NotNil(t, v.validateVariables(ctx, flow))
	})
}

func TestValidateVariablesNodeShape(t *testing.T) {
	v := &flowValidator{}
	ctx := context.Background()
	reason := func(node providers.NodeDefinition) string {
		return reasonOf(t, v.validateVariables(ctx, &FlowDefinition{Nodes: []providers.NodeDefinition{node}}))
	}
	valid := func() providers.NodeDefinition { return variablesNode("vars", "end", booleanVariable("x")) }

	t.Run("it declares at least one variable", func(t *testing.T) {
		require.Contains(t, reason(variablesNode("vars", "end")), "at least one")
	})
	t.Run("it names the node to go on to", func(t *testing.T) {
		node := valid()
		node.OnSuccess = ""
		require.Contains(t, reason(node), "onSuccess")
	})
	t.Run("it goes on through onSuccess only", func(t *testing.T) {
		node := valid()
		node.OnFailure = endNode
		require.Contains(t, reason(node), "onSuccess only")
		node = valid()
		node.Next = endNode
		require.Contains(t, reason(node), "onSuccess only")
	})
	t.Run("it shows and runs nothing", func(t *testing.T) {
		node := valid()
		node.Prompts = []providers.PromptDefinition{{}}
		require.Contains(t, reason(node), "no executor")
		node = valid()
		node.Executor = &providers.ExecutorDefinition{Name: "Anything"}
		require.Contains(t, reason(node), "no executor")
		node = valid()
		node.Properties = map[string]interface{}{"k": "v"}
		require.Contains(t, reason(node), "no executor")
	})
}

type fakeIDPReader struct {
	idps map[string]*providers.IDPDTO
}

func (f fakeIDPReader) GetIdentityProvider(
	_ context.Context, id string,
) (*providers.IDPDTO, *tidcommon.ServiceError) {
	if idp, ok := f.idps[id]; ok {
		return idp, nil
	}
	return nil, &ErrorFlowNotFound
}

func TestValidateVariablesConnectionInputs(t *testing.T) {
	ctx := context.Background()
	connection := func(category string, types ...string) providers.VariableDefinition {
		return providers.VariableDefinition{
			Identifier: "conn", Type: providers.InputTypeConnectionSelect, DefaultValue: "c-1",
			Category: category, ConnectionTypes: types,
		}
	}
	flowWith := func(def providers.VariableDefinition) *FlowDefinition {
		flow := linearFlow(variablesNode("vars", "end", def))
		flow.OUID = "ou-1"
		return flow
	}

	t.Run("the category and the kinds have to be supported", func(t *testing.T) {
		v := &flowValidator{}
		require.Nil(t, v.validateVariables(ctx, flowWith(connection("IDENTITY_PROVIDER", "google", "oidc"))))
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flowWith(connection("SMS_PROVIDER")))), "category")
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flowWith(connection("IDENTITY_PROVIDER", "twilio")))),
			"twilio")
	})

	reader := fakeIDPReader{idps: map[string]*providers.IDPDTO{
		"c-1": {ID: "c-1", Type: providers.IDPTypeGoogle, OUID: "ou-1"},
	}}
	v := &flowValidator{idpReader: reader}
	t.Run("the default is a connection of the owner of a kind the variable takes", func(t *testing.T) {
		require.Nil(t, v.validateVariables(ctx, flowWith(connection("IDENTITY_PROVIDER", "google"))))
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flowWith(connection("IDENTITY_PROVIDER", "github")))),
			"does not take")

		other := flowWith(connection("IDENTITY_PROVIDER"))
		other.OUID = "ou-2"
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, other)), "not a connection of the organization unit")

		missing := connection("IDENTITY_PROVIDER")
		missing.DefaultValue = "c-9"
		require.Contains(t, reasonOf(t, v.validateVariables(ctx, flowWith(missing))), "not an existing connection")
	})
}
