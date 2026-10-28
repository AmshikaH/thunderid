// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package providers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Only a VARIABLES node has 'inputs' to read: they declare its variables. The 'inputs' list that other nodes carry,
// which nothing reads, stays unread, whatever it holds.

const legacyTaskJSON = `{"id":"verify","type":"TASK_EXECUTION","onSuccess":"next","inputs":[
	{"ref":"input_003","identifier":"inviteToken","type":"HIDDEN","required":true}]}`

const variablesJSON = `{"id":"vars","type":"VARIABLES","onSuccess":"next","inputs":[
	{"identifier":"otp","type":"NUMBER_INPUT","defaultValue":120,"min":30,"max":600}]}`

const legacyTaskYAML = `
id: verify
type: TASK_EXECUTION
inputs:
- ref: input_003
  identifier: inviteToken
  type: HIDDEN
  required: true
onSuccess: next
`

const variablesYAML = `
id: vars
type: VARIABLES
inputs:
- identifier: otp
  type: NUMBER_INPUT
  defaultValue: 120
  min: 30
onSuccess: next
`

func TestNodeDefinitionJSONLeavesTheInputsOfOtherNodesUnread(t *testing.T) {
	var node NodeDefinition
	require.NoError(t, json.Unmarshal([]byte(legacyTaskJSON), &node))

	require.Equal(t, "verify", node.ID)
	require.Equal(t, "next", node.OnSuccess)
	require.Empty(t, node.Inputs)

	encoded, err := json.Marshal(node)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "inputs", "the node is stored as it always was")
}

func TestNodeDefinitionJSONReadsTheInputsOfAVariablesNode(t *testing.T) {
	var node NodeDefinition
	require.NoError(t, json.Unmarshal([]byte(variablesJSON), &node))

	require.Equal(t, "vars", node.ID)
	require.Len(t, node.Inputs, 1)
	require.Equal(t, "otp", node.Inputs[0].Identifier)
	require.Equal(t, float64(120), node.Inputs[0].DefaultValue)
	require.Equal(t, 600.0, *node.Inputs[0].Max)

	var nodes []NodeDefinition
	require.NoError(t, json.Unmarshal([]byte("["+variablesJSON+","+legacyTaskJSON+"]"), &nodes))
	require.Len(t, nodes[0].Inputs, 1)
	require.Empty(t, nodes[1].Inputs)
}

func TestNodeDefinitionYAMLLeavesTheInputsOfOtherNodesUnread(t *testing.T) {
	var node NodeDefinition
	require.NoError(t, yaml.Unmarshal([]byte(legacyTaskYAML), &node))

	require.Equal(t, "verify", node.ID)
	require.Equal(t, "next", node.OnSuccess)
	require.Empty(t, node.Inputs)
}

func TestNodeDefinitionYAMLReadsTheInputsOfAVariablesNode(t *testing.T) {
	var node NodeDefinition
	require.NoError(t, yaml.Unmarshal([]byte(variablesYAML), &node))

	require.Equal(t, "next", node.OnSuccess)
	require.Len(t, node.Inputs, 1)
	require.Equal(t, "otp", node.Inputs[0].Identifier)
	require.EqualValues(t, 120, node.Inputs[0].DefaultValue)

	encoded, err := yaml.Marshal(&node)
	require.NoError(t, err)
	var again NodeDefinition
	require.NoError(t, yaml.Unmarshal(encoded, &again))
	require.Equal(t, node.Inputs, again.Inputs, "a VARIABLES node survives being written and read")
}

func TestNodeDefinitionDecodeErrorsAreStillErrors(t *testing.T) {
	var node NodeDefinition
	require.Error(t, json.Unmarshal([]byte(`{"id": 5}`), &node))
	require.Error(t, json.Unmarshal([]byte(`{"id":"v","type":"VARIABLES","inputs":"nope"}`), &node))
}
