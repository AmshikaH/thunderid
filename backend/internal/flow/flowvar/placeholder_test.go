// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowvar

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReferences(t *testing.T) {
	props := map[string]interface{}{
		"idpId":  "{{var(idpConnection)}}",
		"nested": map[string]interface{}{"a": []interface{}{"x {{ var( brand ) }} y", 3}},
		"other":  "{{ctx(key)}}",
		"again":  "{{var(idpConnection)}}",
	}
	require.Equal(t, []string{"brand", "idpConnection"}, References(props))
	require.Empty(t, References("plain"))
}

func TestResolve(t *testing.T) {
	values := map[string]string{
		"var.allowSignup":   "true",
		"var.limit":         "42",
		"var.idpConnection": `"conn-1"`,
		"var.brand":         `"acme"`,
	}

	t.Run("a whole placeholder keeps the value's type", func(t *testing.T) {
		require.Equal(t, true, Resolve("{{var(allowSignup)}}", values))
		require.Equal(t, float64(42), Resolve("{{var(limit)}}", values))
		require.Equal(t, "conn-1", Resolve("{{var(idpConnection)}}", values))
	})
	t.Run("a placeholder inside text becomes its text", func(t *testing.T) {
		require.Equal(t, "Sign in to acme (42)", Resolve("Sign in to {{var(brand)}} ({{var(limit)}})", values))
	})
	t.Run("a placeholder with no value is left as it is", func(t *testing.T) {
		require.Equal(t, "{{var(missing)}}", Resolve("{{var(missing)}}", values))
		require.Equal(t, "a {{var(missing)}}", Resolve("a {{var(missing)}}", values))
	})
	t.Run("walks maps and lists and leaves the input alone", func(t *testing.T) {
		in := map[string]interface{}{
			"idpId": "{{var(idpConnection)}}",
			"list":  []interface{}{"{{var(allowSignup)}}", "keep"},
			"n":     7,
		}
		out := Resolve(in, values).(map[string]interface{})
		require.Equal(t, "conn-1", out["idpId"])
		require.Equal(t, []interface{}{true, "keep"}, out["list"])
		require.Equal(t, 7, out["n"])
		require.Equal(t, "{{var(idpConnection)}}", in["idpId"], "the original is not changed")
	})
	t.Run("other placeholders are untouched", func(t *testing.T) {
		require.Equal(t, "{{ctx(ouId)}}", Resolve("{{ctx(ouId)}}", values))
	})
}
