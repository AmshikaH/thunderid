// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/internal/flow/common"
	"github.com/thunder-id/thunderid/internal/flow/flowvar"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

type VariablesNodeTestSuite struct {
	suite.Suite
}

func TestVariablesNodeTestSuite(t *testing.T) {
	suite.Run(t, new(VariablesNodeTestSuite))
}

func (s *VariablesNodeTestSuite) newNode(defs []providers.VariableDefinition, onSuccess string) NodeInterface {
	properties, err := flowvar.PropertiesFor(defs)
	s.Require().NoError(err)
	node := newVariablesNode("vars", properties, false, false)
	node.(RepresentationNodeInterface).SetOnSuccess(onSuccess)
	return node
}

func (s *VariablesNodeTestSuite) nodeContext() *providers.NodeContext {
	return &providers.NodeContext{Context: context.Background(), ExecutionID: "exec-1"}
}

func (s *VariablesNodeTestSuite) TestNewVariablesNode() {
	node := newVariablesNode("vars", nil, true, false)

	s.Equal("vars", node.GetID())
	s.Equal(common.NodeTypeVariables, node.GetType())
	s.True(node.IsStartNode())
	s.False(node.IsFinalNode())
	s.NotNil(node.GetProperties())
}

func (s *VariablesNodeTestSuite) TestResolvesEveryVariableToItsDefault() {
	node := s.newNode([]providers.VariableDefinition{
		{Identifier: "supportEmail", Type: providers.InputTypeEmail, DefaultValue: "support@example.com"},
		{Identifier: "otpValiditySeconds", Type: providers.InputTypeNumber, DefaultValue: 120.0},
		{Identifier: "showRegistrationLink", Type: providers.InputTypeBoolean, DefaultValue: true},
	}, "next")

	resp, err := node.Execute(s.nodeContext())

	s.Nil(err)
	s.Equal(common.NodeStatusComplete, resp.Status)
	s.Equal("next", resp.NextNodeID)
	s.Equal(map[string]string{
		"var.supportEmail":         `"support@example.com"`,
		"var.otpValiditySeconds":   "120",
		"var.showRegistrationLink": "true",
	}, resp.RuntimeData)
}

func (s *VariablesNodeTestSuite) TestAFlowWithNoVariablesResolvesNothing() {
	resp, err := s.newNode(nil, "next").Execute(s.nodeContext())

	s.Nil(err)
	s.Equal(common.NodeStatusComplete, resp.Status)
	s.Empty(resp.RuntimeData)
}

func (s *VariablesNodeTestSuite) TestFailsWhenTheDeclarationsAreNotReadable() {
	node := newVariablesNode("vars", map[string]interface{}{flowvar.PropertyVariables: "not a list"}, false, false)

	resp, err := node.Execute(s.nodeContext())

	s.Nil(resp)
	s.NotNil(err)
}

func (s *VariablesNodeTestSuite) TestFailsWhenADefaultIsNotReadable() {
	node := s.newNode([]providers.VariableDefinition{
		{Identifier: "limit", Type: providers.InputTypeNumber, DefaultValue: []string{"x"}},
	}, "next")

	resp, err := node.Execute(s.nodeContext())

	s.Nil(resp)
	s.NotNil(err)
}

func (s *VariablesNodeTestSuite) TestCloneKeepsTheOnSuccess() {
	node := s.newNode(nil, "next")

	clone, err := newFlowFactory().CloneNode(node)

	s.Require().NoError(err)
	s.Equal(common.NodeTypeVariables, clone.GetType())
	s.Equal("next", clone.(RepresentationNodeInterface).GetOnSuccess())
}
