// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"github.com/thunder-id/thunderid/internal/flow/common"
	"github.com/thunder-id/thunderid/internal/flow/flowvar"
	"github.com/thunder-id/thunderid/internal/system/log"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// variablesNode is a VARIABLES node. It resolves the variables of the flow into the flow's context, where the
// {{var(name)}} placeholders of the steps after it read them, and goes on to its onSuccess node. It asks the user for
// nothing and shows nothing.
//
// The node carries every variable the flow declares, on all its VARIABLES nodes, not only the ones it declares
// itself. Each time one runs it resolves the whole set again, so a value that depends on something that changed
// since the previous one, such as the organization unit the flow is running for, is current from there on.
type variablesNode struct {
	*node
	onSuccess string
	logger    *log.Logger
}

var _ RepresentationNodeInterface = (*variablesNode)(nil)

// newVariablesNode creates a new variables node. The properties carry the variable declarations.
func newVariablesNode(id string, properties map[string]interface{}, isStartNode, isFinalNode bool) NodeInterface {
	if properties == nil {
		properties = make(map[string]interface{})
	}
	return &variablesNode{
		node: &node{
			id:               id,
			_type:            common.NodeTypeVariables,
			properties:       properties,
			isStartNode:      isStartNode,
			isFinalNode:      isFinalNode,
			nextNodeList:     []string{},
			previousNodeList: []string{},
		},
		logger: log.GetLogger().With(log.String(log.LoggerKeyComponentName, "VariablesNode"),
			log.String(log.LoggerKeyNodeID, id)),
	}
}

// Execute resolves every variable of the flow into the runtime data and goes on to the onSuccess node.
func (n *variablesNode) Execute(ctx *providers.NodeContext) (*common.NodeResponse, *tidcommon.ServiceError) {
	logger := n.logger.With(log.String(log.LoggerKeyExecutionID, ctx.ExecutionID))

	defs, err := flowvar.ParseDefinitions(n.GetProperties())
	if err != nil {
		logger.Error(ctx.Context, "The variables are not readable", log.Error(err))
		return nil, &tidcommon.InternalServerError
	}

	values, err := flowvar.DefaultRuntimeValues(defs)
	if err != nil {
		logger.Error(ctx.Context, "The default of a variable is not readable", log.Error(err))
		return nil, &tidcommon.InternalServerError
	}

	return &common.NodeResponse{
		Status:         common.NodeStatusComplete,
		RuntimeData:    values,
		AdditionalData: make(map[string]string),
		NextNodeID:     n.onSuccess,
	}, nil
}

// GetOnSuccess returns the ID of the node the flow goes on to.
func (n *variablesNode) GetOnSuccess() string {
	return n.onSuccess
}

// SetOnSuccess sets the ID of the node the flow goes on to.
func (n *variablesNode) SetOnSuccess(nodeID string) {
	n.onSuccess = nodeID
}
