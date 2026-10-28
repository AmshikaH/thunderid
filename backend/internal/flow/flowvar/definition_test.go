// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowvar

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

func floatPtr(v float64) *float64 { return &v }

func rule(t string, v interface{}) providers.ValidationRuleDefinition {
	return providers.ValidationRuleDefinition{Type: t, Value: v}
}

func TestParseDefinitions(t *testing.T) {
	t.Run("no variables property", func(t *testing.T) {
		defs, err := ParseDefinitions(map[string]interface{}{})
		require.NoError(t, err)
		require.Empty(t, defs)
	})
	t.Run("reads the declarations", func(t *testing.T) {
		defs, err := ParseDefinitions(map[string]interface{}{
			PropertyVariables: []interface{}{
				map[string]interface{}{
					"identifier": "allowSignup", "label": "Allow sign-up",
					"type": "BOOLEAN_INPUT", "defaultValue": false,
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, defs, 1)
		require.Equal(t, "allowSignup", defs[0].Identifier)
		require.Equal(t, providers.InputTypeBoolean, defs[0].Type)
		require.Equal(t, false, defs[0].DefaultValue)
	})
	t.Run("rejects a malformed property", func(t *testing.T) {
		_, err := ParseDefinitions(map[string]interface{}{PropertyVariables: "nope"})
		require.Error(t, err)
	})
	t.Run("properties carry the declarations back out", func(t *testing.T) {
		in := []providers.VariableDefinition{{
			Identifier: "brand", Type: providers.InputTypeText, DefaultValue: "acme",
			Validation: []providers.ValidationRuleDefinition{rule("maxLength", 8)},
		}}
		props, err := PropertiesFor(in)
		require.NoError(t, err)
		out, err := ParseDefinitions(props)
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, "brand", out[0].Identifier)
		require.Len(t, out[0].Validation, 1)
	})
}

func TestValidateDefinitions(t *testing.T) {
	valid := func() providers.VariableDefinition {
		return providers.VariableDefinition{Identifier: "brand", Type: providers.InputTypeText, DefaultValue: "acme"}
	}
	cases := []struct {
		name    string
		mutate  func(*providers.VariableDefinition)
		wantErr string
	}{
		{"valid text", func(*providers.VariableDefinition) {}, ""},
		{"bad identifier", func(d *providers.VariableDefinition) { d.Identifier = "1bad" }, "is not valid"},
		{"unknown type", func(d *providers.VariableDefinition) { d.Type = "NOPE" }, "not supported"},
		{"missing default", func(d *providers.VariableDefinition) { d.DefaultValue = nil }, "no default"},
		{"default of the wrong type", func(d *providers.VariableDefinition) { d.DefaultValue = true }, "its default"},
		{"rules on a number", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeNumber
			d.DefaultValue = 1.0
			d.Validation = []providers.ValidationRuleDefinition{rule("regex", "x")}
		}, "text, email or phone"},
		{"bad regex", func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("regex", "(")}
		}, "not a valid expression"},
		{"default off the regex", func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("regex", "^[0-9]+$")}
		}, "does not match"},
		{"unknown rule", func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("startsWith", "a")}
		}, "not supported"},
		{"length rules reversed", func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("minLength", 5), rule("maxLength", 2)}
		}, "maximum length is below"},
		{"number bounds reversed", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeNumber
			d.Min, d.Max = floatPtr(5), floatPtr(1)
			d.DefaultValue = 3.0
		}, "maximum is below"},
		{"bound on text", func(d *providers.VariableDefinition) { d.Min = floatPtr(1) }, "number input only"},
		{"select with no options", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
		}, "at least one option"},
		{"select default not an option", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{"a"}
			d.DefaultValue = "b"
		}, "not one of the options"},
		{"duplicate option", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{"a", "a"}
			d.DefaultValue = "a"
		}, "more than once"},
		{"options on text", func(d *providers.VariableDefinition) { d.Options = []interface{}{"a"} }, "select only"},
		{"connection without a category", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeConnectionSelect
			d.DefaultValue = "conn-1"
		}, "names the category"},
		{"category on text", func(d *providers.VariableDefinition) {
			d.Category = CategoryIdentityProvider
		}, "connection input only"},
		{"valid connection", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeConnectionSelect
			d.Category = CategoryIdentityProvider
			d.ConnectionTypes = []string{"google"}
			d.DefaultValue = "conn-1"
		}, ""},
		{"select of booleans", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{true, false}
			d.DefaultValue = true
		}, ""},
		{"select of numbers", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{1.0, 2.5}
			d.DefaultValue = 2.5
		}, ""},
		{"select of mixed values", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{"apple", 1.0, true}
			d.DefaultValue = 1.0
		}, ""},
		{"select option that is not a scalar", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{[]interface{}{"a"}}
			d.DefaultValue = "a"
		}, "not a string, a number or a boolean"},
		{"select options that read the same", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{"1", 1.0}
			d.DefaultValue = "1"
		}, "more than once"},
		{"boolean select default given as text", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeSelect
			d.Options = []interface{}{true, false}
			d.DefaultValue = "true"
		}, "not one of the options"},
		{"valid boolean", func(d *providers.VariableDefinition) {
			d.Type = providers.InputTypeBoolean
			d.DefaultValue = true
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := valid()
			c.mutate(&def)
			err := ValidateDefinitions([]providers.VariableDefinition{def})
			if c.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), c.wantErr)
		})
	}

	t.Run("duplicate identifiers", func(t *testing.T) {
		err := ValidateDefinitions([]providers.VariableDefinition{valid(), valid()})
		require.Error(t, err)
		require.Contains(t, err.Error(), "more than once")
	})
}

func TestValidateRaw(t *testing.T) {
	def := func(t string, mutate func(*providers.VariableDefinition)) providers.VariableDefinition {
		d := providers.VariableDefinition{Identifier: "p", Type: t}
		if mutate != nil {
			mutate(&d)
		}
		return d
	}
	cases := []struct {
		name string
		def  providers.VariableDefinition
		raw  string
		ok   bool
	}{
		{"text ok", def(providers.InputTypeText, nil), "hello", true},
		{"text too long", def(providers.InputTypeText, func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("maxLength", 3)}
		}), "hello", false},
		{"text too short", def(providers.InputTypeText, func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("minLength", 10)}
		}), "hello", false},
		{"text regex ok", def(providers.InputTypeText, func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("regex", "^[a-z]+$")}
		}), "abc", true},
		{"text regex is matched as written", def(providers.InputTypeText, func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("regex", "[a-z]+")}
		}), "abc1", true},
		{"text regex anchored", def(providers.InputTypeText, func(d *providers.VariableDefinition) {
			d.Validation = []providers.ValidationRuleDefinition{rule("regex", "^[a-z]+$")}
		}), "abc1", false},
		{"email ok", def(providers.InputTypeEmail, nil), "a@b.io", true},
		{"email with a display name", def(providers.InputTypeEmail, nil), "A <a@b.io>", false},
		{"email without domain", def(providers.InputTypeEmail, nil), "nope", false},
		{"phone ok", def(providers.InputTypePhone, nil), "+94 77 123 4567", true},
		{"phone letters", def(providers.InputTypePhone, nil), "call me", false},
		{"number ok", def(providers.InputTypeNumber, func(d *providers.VariableDefinition) {
			d.Min, d.Max = floatPtr(1), floatPtr(10)
		}), "5", true},
		{"number below min", def(providers.InputTypeNumber, func(d *providers.VariableDefinition) {
			d.Min = floatPtr(1)
		}), "0", false},
		{"number above max", def(providers.InputTypeNumber, func(d *providers.VariableDefinition) {
			d.Max = floatPtr(10)
		}), "11", false},
		{"number not numeric", def(providers.InputTypeNumber, nil), "1234x", false},
		{"date ok", def(providers.InputTypeDate, nil), "2026-10-22", true},
		{"date wrong form", def(providers.InputTypeDate, nil), "22/10/2026", false},
		{"select ok", def(providers.InputTypeSelect, func(d *providers.VariableDefinition) {
			d.Options = []interface{}{"a", "b"}
		}), "b", true},
		{"select unknown", def(providers.InputTypeSelect, func(d *providers.VariableDefinition) {
			d.Options = []interface{}{"a"}
		}), "z", false},
		{"boolean true", def(providers.InputTypeBoolean, nil), "true", true},
		{"boolean number", def(providers.InputTypeBoolean, nil), "1234", false},
		{"connection id", def(providers.InputTypeConnectionSelect, nil), "conn-1", true},
		{"connection empty", def(providers.InputTypeConnectionSelect, nil), " ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateRaw(c.def, c.raw)
			if c.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestRawFromJSONValue(t *testing.T) {
	raw, err := RawFromJSONValue(providers.VariableDataTypeBoolean, true)
	require.NoError(t, err)
	require.Equal(t, "true", raw)

	_, err = RawFromJSONValue(providers.VariableDataTypeBoolean, "true")
	require.Error(t, err, "a boolean takes a boolean, not text")

	raw, err = RawFromJSONValue(providers.VariableDataTypeNumber, 42.0)
	require.NoError(t, err)
	require.Equal(t, "42", raw)

	_, err = RawFromJSONValue(providers.VariableDataTypeString, 1.0)
	require.Error(t, err)
}

func TestEncodeRuntimeAndDefaults(t *testing.T) {
	enc, err := EncodeRuntime(providers.VariableDataTypeBoolean, "true")
	require.NoError(t, err)
	require.Equal(t, "true", enc)

	enc, err = EncodeRuntime(providers.VariableDataTypeNumber, "3.50")
	require.NoError(t, err)
	require.Equal(t, "3.5", enc)

	enc, err = EncodeRuntime(providers.VariableDataTypeString, `a "quoted" value`)
	require.NoError(t, err)
	require.Equal(t, `"a \"quoted\" value"`, enc)

	values, err := DefaultRuntimeValues([]providers.VariableDefinition{
		{Identifier: "allowSignup", Type: providers.InputTypeBoolean, DefaultValue: false},
		{Identifier: "brand", Type: providers.InputTypeText, DefaultValue: "acme"},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"var.allowSignup": "false", "var.brand": `"acme"`}, values)
}

func TestDataTypeOf(t *testing.T) {
	sel := func(options ...interface{}) providers.VariableDefinition {
		return providers.VariableDefinition{Type: providers.InputTypeSelect, Options: options}
	}
	cases := []struct {
		name string
		def  providers.VariableDefinition
		want string
	}{
		{"text", providers.VariableDefinition{Type: providers.InputTypeText}, providers.VariableDataTypeString},
		{"email", providers.VariableDefinition{Type: providers.InputTypeEmail}, providers.VariableDataTypeString},
		{"phone", providers.VariableDefinition{Type: providers.InputTypePhone}, providers.VariableDataTypeString},
		{"date", providers.VariableDefinition{Type: providers.InputTypeDate}, providers.VariableDataTypeString},
		{"connection", providers.VariableDefinition{Type: providers.InputTypeConnectionSelect},
			providers.VariableDataTypeString},
		{"number", providers.VariableDefinition{Type: providers.InputTypeNumber}, providers.VariableDataTypeNumber},
		{"boolean", providers.VariableDefinition{Type: providers.InputTypeBoolean}, providers.VariableDataTypeBoolean},
		{"select of strings", sel("apple", "orange"), providers.VariableDataTypeString},
		{"select of booleans", sel(true, false), providers.VariableDataTypeBoolean},
		{"select of numbers", sel(1.0, 2.5), providers.VariableDataTypeNumber},
		{"select of mixed values", sel("apple", "orange", 1.0, true), providers.VariableDataTypeString},
		{"select of numbers and booleans", sel(1.0, true), providers.VariableDataTypeString},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { require.Equal(t, c.want, DataTypeOf(c.def)) })
	}
}

func TestSelectResolvesToTheTypeOfItsOptions(t *testing.T) {
	values, err := DefaultRuntimeValues([]providers.VariableDefinition{
		{Identifier: "flag", Type: providers.InputTypeSelect, Options: []interface{}{true, false}, DefaultValue: false},
		{Identifier: "tier", Type: providers.InputTypeSelect, Options: []interface{}{1.0, 2.0}, DefaultValue: 2.0},
		{Identifier: "fruit", Type: providers.InputTypeSelect,
			Options: []interface{}{"apple", "orange"}, DefaultValue: "apple"},
		{Identifier: "mixed", Type: providers.InputTypeSelect,
			Options: []interface{}{"apple", 1.0, true}, DefaultValue: 1.0},
	})
	require.NoError(t, err)
	require.Equal(t, "false", values[RuntimeKey("flag")])
	require.Equal(t, "2", values[RuntimeKey("tier")])
	require.Equal(t, `"apple"`, values[RuntimeKey("fruit")])
	require.Equal(t, `"1"`, values[RuntimeKey("mixed")])

	def := providers.VariableDefinition{Type: providers.InputTypeSelect, Options: []interface{}{true, false}}
	require.NoError(t, ValidateRaw(def, "true"))
	require.Error(t, ValidateRaw(def, "maybe"))
}
