package appops

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// Schema describes the arguments of an operation. It is a small subset of JSON Schema, exactly the part that
// Validate enforces, so what a model or an MCP client is told (Schema marshals as JSON Schema) is what is checked:
// nothing is advertised that is not enforced, and an argument that is not described is refused.
type Schema struct {
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
func (s *Schema) Validate(v any) error { return s.validate(v, "arguments") }

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
