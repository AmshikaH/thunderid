// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package graphbuilder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/thunder-id/thunderid/internal/flow/common"
	"github.com/thunder-id/thunderid/internal/flow/core"
	"github.com/thunder-id/thunderid/internal/flow/flowvar"
	"github.com/thunder-id/thunderid/internal/system/cache"
	"github.com/thunder-id/thunderid/internal/system/config"
	"github.com/thunder-id/thunderid/internal/system/log"
	engineconfig "github.com/thunder-id/thunderid/pkg/thunderidengine/config"
	"github.com/thunder-id/thunderid/tests/mocks/flow/coremock"
	"github.com/thunder-id/thunderid/tests/mocks/flow/executormock"
	"github.com/thunder-id/thunderid/tests/mocks/flow/interceptormock"
)

func twoVariablesFlow() []providers.NodeDefinition {
	return []providers.NodeDefinition{
		{ID: "start", Type: "START", OnSuccess: "first"},
		{
			ID: "first", Type: string(common.NodeTypeVariables), OnSuccess: "second",
			Inputs: []providers.VariableDefinition{
				{Identifier: "var1", Type: providers.InputTypeBoolean, DefaultValue: true},
				{Identifier: "var2", Type: providers.InputTypeText, DefaultValue: "a"},
			},
		},
		{
			ID: "second", Type: string(common.NodeTypeVariables), OnSuccess: "end",
			Inputs: []providers.VariableDefinition{
				{Identifier: "var3", Type: providers.InputTypeNumber, DefaultValue: 3.0},
			},
		},
		{ID: "end", Type: "END"},
	}
}

func TestVariablesNodeProperties(t *testing.T) {
	original := twoVariablesFlow()

	properties, err := variablesNodeProperties(original)

	require.NoError(t, err)
	defs, err := flowvar.ParseDefinitions(properties)
	require.NoError(t, err)
	require.Len(t, defs, 3, "every node carries the whole set, so each resolves all of the variables")
	require.Equal(t, "var1", defs[0].Identifier)
	require.Equal(t, "var3", defs[2].Identifier)

	require.Len(t, original[1].Inputs, 2, "the stored flow keeps each node with what it declares")
	require.Empty(t, original[1].Properties)
}

func TestVariablesNodePropertiesWithNoVariables(t *testing.T) {
	properties, err := variablesNodeProperties([]providers.NodeDefinition{{ID: "start", Type: "START"}})

	require.NoError(t, err)
	defs, err := flowvar.ParseDefinitions(properties)
	require.NoError(t, err)
	require.Empty(t, defs)
}

// The graph is built through the real node factory, and a VARIABLES node needs no executor.
func TestBuildGraphWithVariablesNodes(t *testing.T) {
	_ = config.InitializeServerRuntime("test", &config.Config{
		Server: engineconfig.ServerConfig{Identifier: "test-deployment"},
	})
	factory, _ := core.Initialize(cache.Initialize(config.GetServerRuntime().Config.Cache, "test-deployment"))
	registry := executormock.NewExecutorRegistryInterfaceMock(t)
	registry.EXPECT().IsRegistered(mock.Anything).Return(false).Maybe()
	b := &graphBuilder{
		flowFactory:         factory,
		executorRegistry:    registry,
		interceptorRegistry: interceptormock.NewInterceptorRegistryInterfaceMock(t),
		graphCache:          coremock.NewGraphCacheInterfaceMock(t),
		logger:              log.GetLogger().With(log.String(log.LoggerKeyComponentName, "FlowGraphBuilder")),
	}

	graph, err := b.buildGraph(context.Background(), &providers.CompleteFlowDefinition{
		ID: "flow-1", Handle: "h", Name: "n", FlowType: providers.FlowTypeAuthentication, Nodes: twoVariablesFlow(),
	})

	require.NoError(t, err)
	node, ok := graph.GetNode("first")
	require.True(t, ok)
	require.Equal(t, common.NodeTypeVariables, node.GetType())
	onSuccess, ok := node.(core.RepresentationNodeInterface)
	require.True(t, ok)
	require.Equal(t, "second", onSuccess.GetOnSuccess())
	require.False(t, node.IsFinalNode())
	defs, err := flowvar.ParseDefinitions(node.GetProperties())
	require.NoError(t, err)
	require.Len(t, defs, 3)
}
