package domain

import (
	"encoding/json"
	"math"
	"reflect"
	"time"
)

// Formula is a bounded arithmetic tree, never source code. Missing operands and
// division by zero produce an absent value, not a fabricated zero or NaN.
type PersonalFormula struct {
	Op    string            `json:"op"`
	Field string            `json:"field,omitempty"`
	Value *float64          `json:"value,omitempty"`
	Args  []PersonalFormula `json:"args,omitempty"`
}
type PersonalMetric struct {
	Operation string `json:"operation"`
	Field     string `json:"field,omitempty"`
}
type PersonalStep struct {
	Operation string                     `json:"operation"`
	Values    map[string]json.RawMessage `json:"values,omitempty"`
	Clear     []string                   `json:"clear,omitempty"`
}
type PersonalAction struct {
	Prompt string           `json:"prompt,omitempty"`
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	When   []PersonalFilter `json:"when,omitempty"`
	Steps  []PersonalStep   `json:"steps"`
}

// Rules update only their own record, once in declaration order. Scheduled rules
// use a persisted datetime watermark; they cannot invoke an agent or an app API.
type PersonalRule struct {
	ID       string                     `json:"id"`
	When     []PersonalFilter           `json:"when,omitempty"`
	Set      map[string]json.RawMessage `json:"set"`
	Schedule *PersonalRuleSchedule      `json:"schedule,omitempty"`
}
type PersonalRuleSchedule struct {
	DateField    string `json:"dateField"`
	LastRunField string `json:"lastRunField"`
	EveryDays    int    `json:"everyDays,omitempty"`
}

func samePersonalBounds(a, b PersonalField) bool {
	// Relaxing a bound is safe; tightening it needs a record migration.
	return (b.Min == nil || a.Min != nil && *b.Min <= *a.Min) && (b.Max == nil || a.Max != nil && *b.Max >= *a.Max)
}
func personalFields(s PersonalCollectionSpec) map[string]PersonalField {
	out := map[string]PersonalField{}
	for _, f := range s.Fields {
		out[f.Key] = f
	}
	return out
}
func validatePersonalFilters(filters []PersonalFilter, fields map[string]PersonalField) error {
	if len(filters) > 16 {
		return personalInvalid("too many conditions")
	}
	seen := map[string]bool{}
	for _, f := range filters {
		field, ok := fields[f.Field]
		if !ok || seen[f.Field] || field.Type == "multiselect" || field.Type == "longtext" {
			return personalInvalid("invalid condition field")
		}
		seen[f.Field] = true
		raw, _ := json.Marshal(f.Value)
		if field.Type == "number" || field.Type == "boolean" {
			raw = []byte(f.Value)
		}
		field.Required = false
		if err := ValidatePersonalValues(PersonalCollectionSpec{Fields: []PersonalField{field}}, map[string]json.RawMessage{f.Field: raw}); err != nil {
			return err
		}
	}
	return nil
}
func validatePersonalPatch(values map[string]json.RawMessage, clear []string, fields map[string]PersonalField, allowed map[string]bool) error {
	if len(values)+len(clear) == 0 {
		return personalInvalid("empty action step")
	}
	if values == nil {
		values = map[string]json.RawMessage{}
	}
	schema := PersonalCollectionSpec{}
	for _, f := range fields {
		f.Required = false
		schema.Fields = append(schema.Fields, f)
	}
	if err := ValidatePersonalValues(schema, values); err != nil {
		return err
	}
	seen := map[string]bool{}
	for k := range values {
		if fields[k].Formula != nil || !allowed[k] {
			return personalInvalid("action cannot write this field")
		}
		seen[k] = true
	}
	for _, k := range clear {
		f, ok := fields[k]
		if !ok || f.Required || f.Formula != nil || !allowed[k] || seen[k] {
			return personalInvalid("invalid cleared field")
		}
		seen[k] = true
	}
	return nil
}
func validatePersonalCapabilities(s PersonalCollectionSpec) error {
	fields := personalFields(s)
	for _, f := range s.Fields {
		if f.Min != nil && (f.Type != "number" || math.IsNaN(*f.Min) || math.IsInf(*f.Min, 0)) || f.Max != nil && (f.Type != "number" || math.IsNaN(*f.Max) || math.IsInf(*f.Max, 0)) || f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return personalInvalid("invalid numeric bounds")
		}
		if f.Input != "" && (f.Type != "number" || !inPersonalList(f.Input, []string{"slider", "rating"}) || f.Min == nil || f.Max == nil || *f.Min >= *f.Max) {
			return personalInvalid("numeric controls need finite bounds")
		}
		if f.Formula == nil {
			continue
		}
		if f.Type != "number" || f.Required || f.Min != nil || f.Max != nil || f.Input != "" {
			return personalInvalid("calculated fields are optional numbers without input constraints")
		}
		nodes := 0
		visiting := map[string]bool{f.Key: true}
		var walk func(PersonalFormula, int) error
		walk = func(x PersonalFormula, depth int) error {
			nodes++
			if nodes > 64 || depth > 8 {
				return personalInvalid("formula too complex")
			}
			switch x.Op {
			case "field":
				ref, ok := fields[x.Field]
				if !ok || ref.Type != "number" || visiting[x.Field] || x.Value != nil || len(x.Args) > 0 {
					return personalInvalid("invalid or cyclic formula reference")
				}
				if ref.Formula != nil {
					visiting[x.Field] = true
					err := walk(*ref.Formula, depth+1)
					delete(visiting, x.Field)
					return err
				}
			case "literal":
				if x.Value == nil || math.IsNaN(*x.Value) || math.IsInf(*x.Value, 0) || x.Field != "" || len(x.Args) > 0 {
					return personalInvalid("invalid literal")
				}
			case "add", "subtract", "multiply", "divide", "min", "max":
				if x.Field != "" || x.Value != nil || len(x.Args) < 2 || len(x.Args) > 8 || (x.Op == "divide" || x.Op == "subtract") && len(x.Args) != 2 {
					return personalInvalid("invalid arithmetic")
				}
				for _, arg := range x.Args {
					if err := walk(arg, depth+1); err != nil {
						return err
					}
				}
			default:
				return personalInvalid("unknown formula operator")
			}
			return nil
		}
		if err := walk(*f.Formula, 1); err != nil {
			return err
		}
	}
	if len(s.Rules) > 16 {
		return personalInvalid("too many record rules")
	}
	ids := map[string]bool{}
	allowed := map[string]bool{}
	for k := range fields {
		allowed[k] = true
	}
	for _, rule := range s.Rules {
		if !personalKey.MatchString(rule.ID) || ids[rule.ID] {
			return personalInvalid("invalid rule ID")
		}
		ids[rule.ID] = true
		if err := validatePersonalFilters(rule.When, fields); err != nil {
			return err
		}
		if err := validatePersonalPatch(rule.Set, nil, fields, allowed); err != nil {
			return err
		}
		if sc := rule.Schedule; sc != nil {
			if !inPersonalList(fields[sc.DateField].Type, []string{"date", "datetime"}) || fields[sc.LastRunField].Type != "datetime" || fields[sc.LastRunField].Required || sc.DateField == sc.LastRunField || sc.EveryDays < 0 || sc.EveryDays > 3650 {
				return personalInvalid("invalid reminder schedule")
			}
			if _, ok := rule.Set[sc.LastRunField]; ok {
				return personalInvalid("schedule watermark is reserved")
			}
		}
	}
	return nil
}
func validatePersonalViewCapabilities(s PersonalWorkspaceSpec, collections map[string]PersonalCollectionSpec) error {
	for _, v := range s.Definition.Views {
		fields := personalFields(collections[v.CollectionID])
		allowed := map[string]bool{}
		for _, k := range append(append([]string{}, v.Fields...), v.CheckedField, v.GroupBy, v.DateField, v.BodyField, v.ParentField) {
			allowed[k] = true
		}
		if err := validatePersonalFilters(v.VisibleWhen, fields); err != nil {
			return err
		}
		if v.EndDateField != "" && (v.Kind != "timeline" || !inPersonalList(fields[v.EndDateField].Type, []string{"date", "datetime"})) {
			return personalInvalid("invalid timeline end")
		}
		if v.Kind == "metric" {
			if v.Metric == nil || !inPersonalList(v.Metric.Operation, []string{"count", "sum", "average", "min", "max"}) || v.Metric.Operation != "count" && fields[v.Metric.Field].Type != "number" || v.Metric.Operation == "count" && v.Metric.Field != "" {
				return personalInvalid("invalid metric")
			}
		} else if v.Metric != nil {
			return personalInvalid("unexpected metric")
		}
		if len(v.Actions) > 16 {
			return personalInvalid("too many actions")
		}
		ids := map[string]bool{}
		for _, a := range v.Actions {
			if !personalKey.MatchString(a.ID) || ids[a.ID] || !personalText(a.Name, 120) || (a.Prompt == "" && len(a.Steps) < 1) || len(a.Steps) > 8 || a.Prompt != "" && (!personalText(a.Prompt, 2000) || len(a.Steps) > 0) {
				return personalInvalid("invalid action")
			}
			ids[a.ID] = true
			if err := validatePersonalFilters(a.When, fields); err != nil {
				return err
			}
			for _, step := range a.Steps {
				if !inPersonalList(step.Operation, v.Operations) {
					return personalInvalid("action requires its view's operation")
				}
				switch step.Operation {
				case "create", "update":
					if err := validatePersonalPatch(step.Values, step.Clear, fields, allowed); err != nil {
						return err
					}
					if step.Operation == "create" {
						if len(step.Clear) > 0 {
							return personalInvalid("create cannot clear fields")
						}
						if err := ValidatePersonalValues(collections[v.CollectionID], step.Values); err != nil {
							return err
						}
					}
				case "delete":
					if len(step.Values) > 0 || len(step.Clear) > 0 {
						return personalInvalid("delete takes no values")
					}
				default:
					return personalInvalid("unknown action")
				}
			}
		}
	}
	return nil
}

func PersonalMatches(values map[string]json.RawMessage, filters []PersonalFilter) bool {
	for _, f := range filters {
		raw, ok := values[f.Field]
		if !ok {
			return false
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if text != f.Value {
				return false
			}
		} else {
			var expected any
			if json.Unmarshal([]byte(f.Value), &expected) != nil || !reflect.DeepEqual(v, expected) {
				return false
			}
		}
	}
	return true
}

// PersonalCalculatedValues returns a copy and never modifies the caller's data.
func PersonalCalculatedValues(s PersonalCollectionSpec, values map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range values {
		out[k] = v
	}
	fields := personalFields(s)
	var eval func(PersonalFormula, int) (float64, bool)
	eval = func(x PersonalFormula, depth int) (float64, bool) {
		if depth > 8 {
			return 0, false
		}
		var n float64
		switch x.Op {
		case "literal":
			if x.Value == nil {
				return 0, false
			}
			n = *x.Value
		case "field":
			if f := fields[x.Field]; f.Formula != nil {
				return eval(*f.Formula, depth+1)
			}
			raw, ok := out[x.Field]
			if !ok || json.Unmarshal(raw, &n) != nil {
				return 0, false
			}
		default:
			if len(x.Args) < 2 {
				return 0, false
			}
			var ok bool
			n, ok = eval(x.Args[0], depth+1)
			if !ok {
				return 0, false
			}
			for _, arg := range x.Args[1:] {
				v, ok := eval(arg, depth+1)
				if !ok {
					return 0, false
				}
				switch x.Op {
				case "add":
					n += v
				case "subtract":
					n -= v
				case "multiply":
					n *= v
				case "divide":
					if v == 0 {
						return 0, false
					}
					n /= v
				case "min":
					n = math.Min(n, v)
				case "max":
					n = math.Max(n, v)
				default:
					return 0, false
				}
			}
		}
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	}
	for _, f := range s.Fields {
		if f.Formula != nil {
			delete(out, f.Key)
			if n, ok := eval(*f.Formula, 1); ok {
				out[f.Key], _ = json.Marshal(n)
			}
		}
	}
	return out
}
func PersonalApplyRules(s PersonalCollectionSpec, values map[string]json.RawMessage, now time.Time, scheduled bool) map[string]json.RawMessage {
	out := PersonalCalculatedValues(s, values)
	for _, rule := range s.Rules {
		if (rule.Schedule != nil) != scheduled || !PersonalMatches(out, rule.When) {
			continue
		}
		if sc := rule.Schedule; sc != nil {
			var date, last string
			_ = json.Unmarshal(out[sc.DateField], &date)
			_ = json.Unmarshal(out[sc.LastRunField], &last)
			due, err := time.Parse(time.RFC3339Nano, date)
			if err != nil {
				due, err = time.Parse("2006-01-02", date)
			}
			if err != nil || due.After(now) {
				continue
			}
			if sc.EveryDays > 0 {
				interval := time.Duration(sc.EveryDays) * 24 * time.Hour
				due = due.Add(now.Sub(due) / interval * interval)
			}
			previous, _ := time.Parse(time.RFC3339Nano, last)
			if !previous.Before(due) {
				continue
			}
			out[sc.LastRunField], _ = json.Marshal(due.UTC().Format(time.RFC3339Nano))
		}
		for k, v := range rule.Set {
			out[k] = v
		}
		out = PersonalCalculatedValues(s, out)
	}
	return out
}
