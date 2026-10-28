// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package flowvar

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/thunder-id/thunderid/internal/system/utils"
)

var (
	idPattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	phonePattern = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{4,18}[0-9]$`)
)

// maxTextLength bounds a text value that declares no length of its own.
const maxTextLength = 1024

// ParseDefinitions reads the variable declarations a VARIABLES node carries in its properties. It returns none when
// the property is absent.
func ParseDefinitions(properties map[string]interface{}) ([]providers.VariableDefinition, error) {
	raw, ok := properties[PropertyVariables]
	if !ok || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("the variables are not readable: %w", err)
	}
	var defs []providers.VariableDefinition
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("the variables are malformed: %w", err)
	}
	return defs, nil
}

// PropertiesFor is the properties a VARIABLES node carries in the graph: the declarations, in the form properties are
// held in.
func PropertiesFor(defs []providers.VariableDefinition) (map[string]interface{}, error) {
	data, err := json.Marshal(defs)
	if err != nil {
		return nil, err
	}
	var generic []interface{}
	if err := json.Unmarshal(data, &generic); err != nil {
		return nil, err
	}
	return map[string]interface{}{PropertyVariables: generic}, nil
}

// isTextual reports whether a type holds free text, which the validation rules of a prompt input apply to.
func isTextual(t string) bool {
	return t == providers.InputTypeText || t == providers.InputTypeEmail || t == providers.InputTypePhone
}

// ValidateDefinitions checks a set of declarations: identifiers are well formed and unique, every type is known and
// consistently configured, and every variable has a default that is a valid value of it.
func ValidateDefinitions(defs []providers.VariableDefinition) error {
	seen := make(map[string]bool, len(defs))
	for _, def := range defs {
		if !idPattern.MatchString(def.Identifier) {
			return fmt.Errorf("variable %q is not valid: the identifier starts with a letter and holds letters, "+
				"digits and underscores", def.Identifier)
		}
		if seen[def.Identifier] {
			return fmt.Errorf("variable %q is declared more than once", def.Identifier)
		}
		seen[def.Identifier] = true

		if err := validateShape(def); err != nil {
			return fmt.Errorf("variable %q: %w", def.Identifier, err)
		}
		if def.DefaultValue == nil {
			return fmt.Errorf("variable %q has no default", def.Identifier)
		}
		raw, err := DefaultRaw(def)
		if err != nil {
			return fmt.Errorf("variable %q: its default %w", def.Identifier, err)
		}
		if err := ValidateRaw(def, raw); err != nil {
			return fmt.Errorf("variable %q: its default %w", def.Identifier, err)
		}
	}
	return nil
}

func validateShape(def providers.VariableDefinition) error {
	switch def.Type {
	case providers.InputTypeText, providers.InputTypeEmail, providers.InputTypePhone, providers.InputTypeNumber,
		providers.InputTypeDate, providers.InputTypeBoolean, providers.InputTypeSelect,
		providers.InputTypeConnectionSelect:
	default:
		return fmt.Errorf("input type %q is not supported", def.Type)
	}
	if err := validateRules(def); err != nil {
		return err
	}
	if (def.Min != nil || def.Max != nil) && def.Type != providers.InputTypeNumber {
		return errors.New("a bound applies to a number input only")
	}
	if def.Min != nil && def.Max != nil && *def.Max < *def.Min {
		return errors.New("the maximum is below the minimum")
	}
	if def.Type == providers.InputTypeSelect {
		if len(def.Options) == 0 {
			return errors.New("a select needs at least one option")
		}
		values := make(map[string]bool, len(def.Options))
		for _, o := range def.Options {
			raw, err := rawFromScalar(o)
			if err != nil {
				return fmt.Errorf("the option %v %w", o, err)
			}
			if raw == "" {
				return errors.New("a select option needs a value")
			}
			if values[raw] {
				return fmt.Errorf("the option %v is listed more than once", o)
			}
			values[raw] = true
		}
	} else if len(def.Options) > 0 {
		return errors.New("options apply to a select only")
	}
	if def.Type == providers.InputTypeConnectionSelect {
		if def.Category == "" {
			return errors.New("a connection input names the category of connection it takes")
		}
	} else if def.Category != "" || len(def.ConnectionTypes) > 0 {
		return errors.New("a category and connection types apply to a connection input only")
	}
	return nil
}

// rawFromScalar is the raw string form of a select option or of the default of a select: a boolean is "true" or
// "false", a number its decimal form, a string itself. Nothing else can be one.
func rawFromScalar(v interface{}) (string, error) {
	switch n := v.(type) {
	case string:
		return n, nil
	case bool:
		return strconv.FormatBool(n), nil
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(n), nil
	default:
		return "", errors.New("is not a string, a number or a boolean")
	}
}

// selectDefaultRaw is the raw form of a select's default, which has to be one of the options as the option is written:
// a boolean option is matched by a boolean, never by the text that reads as one.
func selectDefaultRaw(def providers.VariableDefinition) (string, error) {
	raw, err := rawFromScalar(def.DefaultValue)
	if err != nil {
		return "", err
	}
	for _, o := range def.Options {
		if optionRaw, err := rawFromScalar(o); err == nil && optionRaw == raw &&
			fmt.Sprintf("%T", o) == fmt.Sprintf("%T", def.DefaultValue) {
			return raw, nil
		}
	}
	return "", errors.New("is not one of the options")
}

// validateRules checks the validation rules of a prompt input a variable carries.
func validateRules(def providers.VariableDefinition) error {
	if len(def.Validation) > 0 && !isTextual(def.Type) {
		return errors.New("validation rules apply to a text, email or phone input only")
	}
	var minLen, maxLen *float64
	for _, rule := range def.Validation {
		switch providers.ValidationType(rule.Type) {
		case providers.ValidationTypeRegex:
			pattern, ok := rule.Value.(string)
			if !ok || pattern == "" {
				return errors.New("a regex rule needs a pattern")
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("the pattern is not a valid expression: %w", err)
			}
		case providers.ValidationTypeMinLength, providers.ValidationTypeMaxLength:
			n, ok := utils.ToFloat64(rule.Value)
			if !ok || n < 0 {
				return fmt.Errorf("a %s rule needs a length of zero or more", rule.Type)
			}
			if providers.ValidationType(rule.Type) == providers.ValidationTypeMinLength {
				minLen = &n
			} else {
				maxLen = &n
			}
		default:
			return fmt.Errorf("the validation rule %q is not supported", rule.Type)
		}
	}
	if minLen != nil && maxLen != nil && *maxLen < *minLen {
		return errors.New("the maximum length is below the minimum")
	}
	return nil
}

// RawFromJSONValue turns a value read from JSON (a bool, a number or a string) into the raw string form values are
// stored and compared in, for a value of the given data type. A boolean is "true" or "false", a number is its decimal
// form, a string is itself.
func RawFromJSONValue(dataType string, v interface{}) (string, error) {
	switch dataType {
	case providers.VariableDataTypeBoolean:
		b, ok := v.(bool)
		if !ok {
			return "", errors.New("is not a true or false value")
		}
		return strconv.FormatBool(b), nil
	case providers.VariableDataTypeNumber:
		switch n := v.(type) {
		case float64:
			return strconv.FormatFloat(n, 'f', -1, 64), nil
		case int:
			return strconv.Itoa(n), nil
		case string:
			return n, nil
		default:
			return "", errors.New("is not a number")
		}
	default:
		s, ok := v.(string)
		if !ok {
			return "", errors.New("is not a string")
		}
		return s, nil
	}
}

// ValidateRaw checks a raw value against what a variable allows. A connection value is only checked to be an
// identifier here: that the connection exists, is the right kind and belongs to the organization unit setting it is
// checked where the connections can be read.
func ValidateRaw(def providers.VariableDefinition, raw string) error {
	switch def.Type {
	case providers.InputTypeText:
		return validateText(def, raw)
	case providers.InputTypeEmail:
		addr, err := mail.ParseAddress(raw)
		if err != nil || addr.Address != raw || !strings.Contains(raw, "@") {
			return errors.New("is not an email address")
		}
		return validateText(def, raw)
	case providers.InputTypePhone:
		if !phonePattern.MatchString(raw) {
			return errors.New("is not a phone number")
		}
		return validateText(def, raw)
	case providers.InputTypeNumber:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return errors.New("is not a number")
		}
		if def.Min != nil && n < *def.Min {
			return fmt.Errorf("is below the minimum of %s", strconv.FormatFloat(*def.Min, 'f', -1, 64))
		}
		if def.Max != nil && n > *def.Max {
			return fmt.Errorf("is above the maximum of %s", strconv.FormatFloat(*def.Max, 'f', -1, 64))
		}
	case providers.InputTypeDate:
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return errors.New("is not a date in the form YYYY-MM-DD")
		}
	case providers.InputTypeSelect:
		for _, o := range def.Options {
			if optionRaw, err := rawFromScalar(o); err == nil && optionRaw == raw {
				return nil
			}
		}
		return errors.New("is not one of the options")
	case providers.InputTypeBoolean:
		if raw != "true" && raw != "false" {
			return errors.New("is not true or false")
		}
	case providers.InputTypeConnectionSelect:
		if strings.TrimSpace(raw) == "" {
			return errors.New("is not a connection id")
		}
	default:
		return fmt.Errorf("has an unsupported input type %q", def.Type)
	}
	return nil
}

// validateText applies the validation rules of a prompt input to a text value, the way a prompt applies them: a regex
// is matched as it is written. A text value that carries no length rule is bounded by maxTextLength.
func validateText(def providers.VariableDefinition, raw string) error {
	length := utf8.RuneCountInString(raw)
	limit := float64(maxTextLength)
	for _, rule := range def.Validation {
		switch providers.ValidationType(rule.Type) {
		case providers.ValidationTypeMinLength:
			if n, ok := utils.ToFloat64(rule.Value); ok && float64(length) < n {
				return fmt.Errorf("is shorter than %d characters", int(n))
			}
		case providers.ValidationTypeMaxLength:
			if n, ok := utils.ToFloat64(rule.Value); ok {
				limit = n
			}
		case providers.ValidationTypeRegex:
			if pattern, ok := rule.Value.(string); ok && pattern != "" {
				if re, err := regexp.Compile(pattern); err != nil || !re.MatchString(raw) {
					return errors.New("does not match the pattern")
				}
			}
		}
	}
	if float64(length) > limit {
		return fmt.Errorf("is longer than %d characters", int(limit))
	}
	return nil
}

// DefaultRaw is the raw form of a variable's default.
func DefaultRaw(def providers.VariableDefinition) (string, error) {
	if def.Type == providers.InputTypeSelect {
		return selectDefaultRaw(def)
	}
	return RawFromJSONValue(DataTypeOf(def), def.DefaultValue)
}

// EncodeRuntime encodes a raw value as the JSON a runtime data entry holds, so a placeholder resolves to a value of
// the variable's own type: a boolean to a boolean, a number to a number, anything else to a string.
func EncodeRuntime(dataType, raw string) (string, error) {
	switch dataType {
	case providers.VariableDataTypeBoolean:
		return strconv.FormatBool(raw == "true"), nil
	case providers.VariableDataTypeNumber:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return "", errors.New("is not a number")
		}
		return strconv.FormatFloat(n, 'f', -1, 64), nil
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// DefaultRuntimeValues encodes every variable's default, keyed by its runtime data key.
func DefaultRuntimeValues(defs []providers.VariableDefinition) (map[string]string, error) {
	out := make(map[string]string, len(defs))
	for _, def := range defs {
		raw, err := DefaultRaw(def)
		if err != nil {
			return nil, fmt.Errorf("variable %q: its default %w", def.Identifier, err)
		}
		enc, err := EncodeRuntime(DataTypeOf(def), raw)
		if err != nil {
			return nil, fmt.Errorf("variable %q: its default %w", def.Identifier, err)
		}
		out[RuntimeKey(def.Identifier)] = enc
	}
	return out, nil
}
