package appops

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Schema describes the arguments of an operation. It is a small subset of JSON Schema, exactly the part that
// Validate enforces, so what a model or an MCP client is told (Schema marshals as JSON Schema) is what is checked:
// nothing is advertised that is not enforced, and an argument that is not described is refused.
type Schema struct {
	// Raw is a validated, upstream JSON Schema used by injected tools. It is emitted unchanged so provider adapters
	// receive the contract that Validate enforces.
	Raw         json.RawMessage    `json:"-"`
	Type        string             `json:"type"` // object, string, integer, boolean, array
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	MaxLength   int                `json:"maxLength,omitempty"` // in characters
	Pattern     string             `json:"-"`                   // a named format; see formats
	Minimum     *int               `json:"minimum,omitempty"`
	Maximum     *int               `json:"maximum,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	MaxItems    int                `json:"maxItems,omitempty"`
	// Format is advertised ("date"); Pattern is how it is checked.
	Format string `json:"format,omitempty"`
}

// MarshalJSON preserves an injected JSON Schema verbatim; built-in schemas use the small typed representation.
func (s *Schema) MarshalJSON() ([]byte, error) {
	if len(s.Raw) != 0 {
		if !json.Valid(s.Raw) {
			return nil, fmt.Errorf("invalid raw JSON schema")
		}
		return append([]byte(nil), s.Raw...), nil
	}
	type plain Schema
	return json.Marshal((*plain)(s))
}

// Object is a schema for an object with the given properties.
func Object(props map[string]*Schema, required ...string) *Schema {
	return &Schema{Type: "object", Properties: props, Required: required}
}

// Str is a string of at most max characters.
func Str(desc string, max int) *Schema {
	return &Schema{Type: "string", Description: desc, MaxLength: max}
}

// Enum is one of the listed strings.
func Enum(desc string, values ...string) *Schema {
	return &Schema{Type: "string", Description: desc, Enum: values}
}

// Int is a whole number in [min, max].
func Int(desc string, min, max int) *Schema {
	return &Schema{Type: "integer", Description: desc, Minimum: &min, Maximum: &max}
}

// Bool is true or false.
func Bool(desc string) *Schema { return &Schema{Type: "boolean", Description: desc} }

// Day is a calendar day, 2006-01-02. The empty string is allowed where a field may be cleared.
func Day(desc string) *Schema {
	return &Schema{Type: "string", Description: desc, Format: "date", Pattern: "day", MaxLength: 10}
}

// List is an array of at most max items.
func List(desc string, items *Schema, max int) *Schema {
	return &Schema{Type: "array", Description: desc, Items: items, MaxItems: max}
}

// Validate checks v (as decoded by encoding/json) against the schema and returns what is wrong, in words a model can
// act on, or nil.
func (s *Schema) Validate(v any) error {
	if len(s.Raw) != 0 {
		raw, err := s.ValidateDefinition()
		if err != nil {
			return err
		}
		return validateJSONSchema(raw, v, "arguments", 0)
	}
	return s.validate(v, "arguments")
}

// ValidateDefinition checks all constraints up front, including constraints for optional fields the current call omits.
func (s *Schema) ValidateDefinition() (any, error) {
	var raw any
	if len(s.Raw) == 0 || json.Unmarshal(s.Raw, &raw) != nil {
		return nil, fmt.Errorf("input schema is invalid")
	}
	if err := checkJSONSchemaDefinition(raw, "input schema", 0); err != nil {
		return nil, err
	}
	return raw, nil
}

func checkJSONSchemaDefinition(schema any, at string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s is nested too deeply", at)
	}
	m, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be a JSON Schema object", at)
	}
	known := map[string]bool{
		"$schema": true, "title": true, "description": true, "default": true, "examples": true,
		"deprecated": true, "readOnly": true, "writeOnly": true, "format": true,
		"type": true, "properties": true, "required": true, "additionalProperties": true,
		"items": true, "minItems": true, "maxItems": true, "minLength": true, "maxLength": true,
		"pattern": true, "minimum": true, "maximum": true, "exclusiveMinimum": true,
		"exclusiveMaximum": true, "enum": true, "const": true,
	}
	for key := range m {
		if !known[key] {
			return fmt.Errorf("%s uses unsupported keyword %q", at, key)
		}
	}
	if rawType, exists := m["type"]; exists {
		name, ok := rawType.(string)
		if !ok || !matchesJSONTypeName(name) {
			return fmt.Errorf("%s has an unsupported type declaration", at)
		}
	}
	if props, exists := m["properties"]; exists {
		properties, ok := props.(map[string]any)
		if !ok {
			return fmt.Errorf("%s properties must be an object", at)
		}
		for name, child := range properties {
			if err := checkJSONSchemaDefinition(child, at+"."+name, depth+1); err != nil {
				return err
			}
		}
	}
	if rawRequired, exists := m["required"]; exists {
		required, ok := rawRequired.([]any)
		if !ok {
			return fmt.Errorf("%s required must be a list", at)
		}
		for _, item := range required {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("%s required entries must be strings", at)
			}
		}
	}
	if additional, exists := m["additionalProperties"]; exists {
		switch extra := additional.(type) {
		case bool:
		case map[string]any:
			if err := checkJSONSchemaDefinition(extra, at+" additionalProperties", depth+1); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s additionalProperties must be a boolean or schema", at)
		}
	}
	if items, exists := m["items"]; exists {
		if err := checkJSONSchemaDefinition(items, at+" items", depth+1); err != nil {
			return err
		}
	}
	if enum, exists := m["enum"]; exists {
		if _, ok := enum.([]any); !ok {
			return fmt.Errorf("%s enum must be a list", at)
		}
	}
	for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
		if value, exists := m[key]; exists {
			if _, ok := value.(float64); !ok {
				return fmt.Errorf("%s %s must be a number", at, key)
			}
		}
	}
	for _, key := range []string{"minItems", "maxItems", "minLength", "maxLength"} {
		if value, exists := m[key]; exists {
			n, ok := value.(float64)
			if !ok || n < 0 || n != math.Trunc(n) {
				return fmt.Errorf("%s %s must be a non-negative integer", at, key)
			}
		}
	}
	if value, exists := m["pattern"]; exists {
		pattern, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s pattern must be a string", at)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("%s pattern is invalid", at)
		}
	}
	if value, exists := m["format"]; exists {
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s format must be a string", at)
		}
	}
	return nil
}

func matchesJSONTypeName(name string) bool {
	switch name {
	case "object", "array", "string", "integer", "number", "boolean", "null":
		return true
	default:
		return false
	}
}

// validateJSONSchema enforces the JSON Schema keywords Werkbord accepts for injected tools. Unknown assertion
// keywords fail closed so upstream validation never becomes less strict than the advertised contract.
func validateJSONSchema(schema, value any, at string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s schema is nested too deeply", at)
	}
	m, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%s has an invalid JSON schema", at)
	}
	known := map[string]bool{
		"$schema": true, "title": true, "description": true, "default": true, "examples": true,
		"deprecated": true, "readOnly": true, "writeOnly": true, "format": true,
		"type": true, "properties": true, "required": true, "additionalProperties": true,
		"items": true, "minItems": true, "maxItems": true, "minLength": true, "maxLength": true,
		"pattern": true, "minimum": true, "maximum": true, "exclusiveMinimum": true,
		"exclusiveMaximum": true, "enum": true, "const": true,
	}
	for key := range m {
		if !known[key] {
			return fmt.Errorf("%s schema uses unsupported keyword %q", at, key)
		}
	}
	if enum, exists := m["enum"]; exists {
		values, ok := enum.([]any)
		if !ok {
			return fmt.Errorf("%s schema has an invalid enum", at)
		}
		match := false
		for _, candidate := range values {
			if reflect.DeepEqual(candidate, value) {
				match = true
				break
			}
		}
		if !match {
			return fmt.Errorf("%s must be one of the allowed values", at)
		}
	}
	if constant, exists := m["const"]; exists && !reflect.DeepEqual(constant, value) {
		return fmt.Errorf("%s must match the required value", at)
	}
	if rawType, exists := m["type"]; exists {
		typeName, ok := rawType.(string)
		if !ok {
			return fmt.Errorf("%s schema has an unsupported type declaration", at)
		}
		if !matchesJSONType(typeName, value) {
			return fmt.Errorf("%s must be %s", at, typeName)
		}
	}
	if str, ok := value.(string); ok {
		length := utf8.RuneCountInString(str)
		if n, exists := numberKeyword(m, "minLength"); exists && float64(length) < n {
			return fmt.Errorf("%s is shorter than allowed", at)
		}
		if n, exists := numberKeyword(m, "maxLength"); exists && float64(length) > n {
			return fmt.Errorf("%s is longer than allowed", at)
		}
		if pattern, exists := m["pattern"]; exists {
			expr, ok := pattern.(string)
			if !ok {
				return fmt.Errorf("%s schema has an invalid pattern", at)
			}
			re, err := regexp.Compile(expr)
			if err != nil {
				return fmt.Errorf("%s schema has an invalid pattern", at)
			}
			if !re.MatchString(str) {
				return fmt.Errorf("%s does not match the required pattern", at)
			}
		}
	}
	if n, ok := value.(float64); ok {
		for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
			limit, exists := numberKeyword(m, key)
			if !exists {
				continue
			}
			if key == "minimum" && n < limit || key == "maximum" && n > limit || key == "exclusiveMinimum" && n <= limit || key == "exclusiveMaximum" && n >= limit {
				return fmt.Errorf("%s is outside the allowed range", at)
			}
		}
	}
	if obj, ok := value.(map[string]any); ok {
		props, _ := m["properties"].(map[string]any)
		required, _ := m["required"].([]any)
		for _, rawName := range required {
			name, ok := rawName.(string)
			if !ok {
				return fmt.Errorf("%s schema has an invalid required list", at)
			}
			if _, exists := obj[name]; !exists {
				return fmt.Errorf("%s needs %q", at, name)
			}
		}
		additional := m["additionalProperties"]
		for name, child := range obj {
			if childSchema, exists := props[name]; exists {
				if err := validateJSONSchema(childSchema, child, at+"."+name, depth+1); err != nil {
					return err
				}
				continue
			}
			switch extra := additional.(type) {
			case bool:
				if !extra {
					return fmt.Errorf("%s has no %q", at, name)
				}
			case map[string]any:
				if err := validateJSONSchema(extra, child, at+"."+name, depth+1); err != nil {
					return err
				}
			}
		}
	}
	if list, ok := value.([]any); ok {
		if n, exists := numberKeyword(m, "minItems"); exists && float64(len(list)) < n {
			return fmt.Errorf("%s has too few items", at)
		}
		if n, exists := numberKeyword(m, "maxItems"); exists && float64(len(list)) > n {
			return fmt.Errorf("%s has too many items", at)
		}
		if itemSchema, exists := m["items"]; exists {
			for i, item := range list {
				if err := validateJSONSchema(itemSchema, item, fmt.Sprintf("%s[%d]", at, i), depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func matchesJSONType(name string, value any) bool {
	switch name {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		n, ok := value.(float64)
		return ok && n == math.Trunc(n)
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func numberKeyword(m map[string]any, name string) (float64, bool) {
	v, exists := m[name]
	if !exists {
		return 0, false
	}
	n, ok := v.(float64)
	return n, ok
}

func (s *Schema) validate(v any, at string) error {
	switch s.Type {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", at)
		}
		for name := range m {
			if _, known := s.Properties[name]; !known {
				return fmt.Errorf("%s has no %q (it takes: %s)", at, name, strings.Join(s.propertyNames(), ", "))
			}
		}
		for _, name := range s.Required {
			if val, ok := m[name]; !ok || val == nil {
				return fmt.Errorf("%s needs %q", at, name)
			}
		}
		for name, val := range m {
			if val == nil {
				return fmt.Errorf("%s.%s must not be null: leave it out instead", at, name)
			}
			if err := s.Properties[name].validate(val, at+"."+name); err != nil {
				return err
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", at)
		}
		if !utf8.ValidString(str) {
			return fmt.Errorf("%s is not valid text", at)
		}
		if s.MaxLength > 0 && utf8.RuneCountInString(str) > s.MaxLength {
			return fmt.Errorf("%s is longer than %d characters", at, s.MaxLength)
		}
		if len(s.Enum) > 0 {
			for _, e := range s.Enum {
				if str == e {
					return nil
				}
			}
			return fmt.Errorf("%s must be one of: %s", at, strings.Join(s.Enum, ", "))
		}
		if s.Pattern == "day" && str != "" {
			if len(str) != 10 || str[4] != '-' || str[7] != '-' {
				return fmt.Errorf("%s must be a day like 2026-03-31", at)
			}
		}
	case "integer":
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%s must be a whole number", at)
		}
		if s.Minimum != nil && f < float64(*s.Minimum) || s.Maximum != nil && f > float64(*s.Maximum) {
			return fmt.Errorf("%s must be between %d and %d", at, *s.Minimum, *s.Maximum)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be true or false", at)
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s must be a list", at)
		}
		if s.MaxItems > 0 && len(list) > s.MaxItems {
			return fmt.Errorf("%s has more than %d items", at, s.MaxItems)
		}
		for i, item := range list {
			if err := s.Items.validate(item, fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s: schema type %q is not supported", at, s.Type)
	}
	return nil
}

func (s *Schema) propertyNames() []string {
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
