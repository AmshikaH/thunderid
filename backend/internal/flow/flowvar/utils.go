// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

// Package flowvar holds the model of the variables a flow uses. A variable is a typed value kept in the context of a
// running flow, and a {{var(name)}} placeholder in the properties of a later step
// takes the variable's value in place of what the step was configured with. A flow declares its variables in
// VARIABLES nodes, in the vocabulary of a prompt's inputs, and each has a default.
package flowvar

import (
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// PropertyVariables is the property of a VARIABLES node, in the graph, that carries the variable declarations.
const PropertyVariables = "variables"

// CategoryIdentityProvider is the category of a connection variable that takes a federated sign-in connection:
// google, github, oidc or oauth.
const CategoryIdentityProvider = "IDENTITY_PROVIDER"

// DataTypeOf is the data type of the value a variable resolves to. Every input type but a select fixes it: text,
// email, phone, date and connection inputs hold a string, a number input a number and a boolean input a boolean. A
// select takes it from its options: all booleans make it a boolean, all numbers a number, and anything else, a mix
// included, a string.
func DataTypeOf(def providers.VariableDefinition) string {
	switch def.Type {
	case providers.InputTypeNumber:
		return providers.VariableDataTypeNumber
	case providers.InputTypeBoolean:
		return providers.VariableDataTypeBoolean
	case providers.InputTypeSelect:
		return optionsDataType(def.Options)
	}
	return providers.VariableDataTypeString
}

func optionsDataType(options []interface{}) string {
	if len(options) == 0 {
		return providers.VariableDataTypeString
	}
	dataType := ""
	for _, o := range options {
		var t string
		switch o.(type) {
		case bool:
			t = providers.VariableDataTypeBoolean
		case float64, int:
			t = providers.VariableDataTypeNumber
		default:
			return providers.VariableDataTypeString
		}
		if dataType != "" && dataType != t {
			return providers.VariableDataTypeString
		}
		dataType = t
	}
	return dataType
}

// RuntimeKeyPrefix prefixes the runtime data key a variable's value is kept under, as a JSON encoded value.
const RuntimeKeyPrefix = "var."

// RuntimeKey is the runtime data key a variable's value is kept under.
func RuntimeKey(id string) string {
	return RuntimeKeyPrefix + id
}
