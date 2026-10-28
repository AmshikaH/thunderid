// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowvar

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// placeholderPattern matches {{var(id)}}, with optional whitespace, and captures the id. It is written like
// the other placeholders of a node's properties, {{ctx(key)}} and {{request(selector)}}.
var placeholderPattern = regexp.MustCompile(`\{\{\s*var\(\s*([A-Za-z][A-Za-z0-9_]*)\s*\)\s*\}\}`)

// References lists the variable ids a value names, anywhere in it, once each and in order.
func References(v interface{}) []string {
	seen := map[string]bool{}
	collect(v, seen)
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func collect(v interface{}, seen map[string]bool) {
	switch t := v.(type) {
	case string:
		for _, m := range placeholderPattern.FindAllStringSubmatch(t, -1) {
			seen[m[1]] = true
		}
	case map[string]interface{}:
		for _, e := range t {
			collect(e, seen)
		}
	case []interface{}:
		for _, e := range t {
			collect(e, seen)
		}
	}
}

// Resolve returns a copy of a value with every {{var(id)}} replaced by the variable's value, which values holds
// JSON encoded by the variable's runtime key. A string that is nothing but a placeholder becomes the value itself,
// of its own type: a switch resolves to a boolean, a number to a number. A placeholder inside a longer string is
// replaced by the value's text. A placeholder no value exists for is left as it is, so a missing value shows up as
// itself and not as an empty one.
func Resolve(v interface{}, values map[string]string) interface{} {
	switch t := v.(type) {
	case string:
		return resolveString(t, values)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, e := range t {
			out[k] = Resolve(e, values)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = Resolve(e, values)
		}
		return out
	default:
		return v
	}
}

func resolveString(s string, values map[string]string) interface{} {
	if !strings.Contains(s, "{{") {
		return s
	}
	if loc := placeholderPattern.FindStringSubmatchIndex(s); len(loc) == 4 && loc[0] == 0 && loc[1] == len(s) {
		if encoded, ok := values[RuntimeKey(s[loc[2]:loc[3]])]; ok {
			var decoded interface{}
			if json.Unmarshal([]byte(encoded), &decoded) == nil {
				return decoded
			}
		}
		return s
	}
	return placeholderPattern.ReplaceAllStringFunc(s, func(match string) string {
		id := placeholderPattern.FindStringSubmatch(match)[1]
		encoded, ok := values[RuntimeKey(id)]
		if !ok {
			return match
		}
		var decoded interface{}
		if json.Unmarshal([]byte(encoded), &decoded) != nil {
			return match
		}
		if text, isString := decoded.(string); isString {
			return text
		}
		return encoded
	})
}
