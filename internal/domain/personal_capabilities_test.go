package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func ptrNumber(v float64) *float64          { return &v }
func formulaField(k string) PersonalFormula { return PersonalFormula{Op: "field", Field: k} }
func TestPersonalArithmeticAndConstraints(t *testing.T) {
	s := PersonalCollectionSpec{Name: "Calculator", SchemaVersion: 1, Fields: []PersonalField{{Key: "amount", Name: "Amount", Type: "number", Min: ptrNumber(0)}, {Key: "factor", Name: "Factor", Type: "number"}, {Key: "result", Name: "Result", Type: "number", Formula: &PersonalFormula{Op: "multiply", Args: []PersonalFormula{formulaField("amount"), formulaField("factor")}}}}}
	if err := ValidatePersonalCollection(s); err != nil {
		t.Fatal(err)
	}
	values := map[string]json.RawMessage{"amount": json.RawMessage(`12.5`), "factor": json.RawMessage(`2`), "result": json.RawMessage(`999`)}
	out := PersonalCalculatedValues(s, values)
	if string(out["result"]) != "25" || string(values["result"]) != "999" {
		t.Fatal(out, values)
	}
	delete(values, "factor")
	if _, ok := PersonalCalculatedValues(s, values)["result"]; ok {
		t.Fatal("missing operand fabricated a result")
	}
	values["factor"] = json.RawMessage(`0`)
	s.Fields[2].Formula.Op = "divide"
	if _, ok := PersonalCalculatedValues(s, values)["result"]; ok {
		t.Fatal("division by zero")
	}
	s.Fields[2].Formula.Args[1] = formulaField("result")
	if ValidatePersonalCollection(s) == nil {
		t.Fatal("accepted cycle")
	}
	s.Fields[2].Formula.Args[1] = formulaField("amount")
	s.Fields[2].Formula.Op = "eval"
	if ValidatePersonalCollection(s) == nil {
		t.Fatal("accepted code")
	}
	s.Fields = s.Fields[:2]
	if ValidatePersonalValues(s, map[string]json.RawMessage{"amount": json.RawMessage(`-1`)}) == nil {
		t.Fatal("accepted out of range")
	}
	old := s
	s.Fields = append([]PersonalField{}, s.Fields...)
	s.Fields[0].Min = ptrNumber(1)
	if ValidatePersonalEvolution(old, s) == nil {
		t.Fatal("tightened bound without migration")
	}
	s.Fields[0].Min = ptrNumber(-1)
	if err := ValidatePersonalEvolution(old, s); err != nil {
		t.Fatal(err)
	}
}
func TestPersonalScheduleOccurrenceAndRules(t *testing.T) {
	s := PersonalCollectionSpec{Name: "Reminders", SchemaVersion: 1, Fields: []PersonalField{{Key: "due", Name: "Due", Type: "date"}, {Key: "last", Name: "Last", Type: "datetime"}, {Key: "reminder", Name: "Reminder", Type: "boolean"}}, Rules: []PersonalRule{{ID: "daily", Set: map[string]json.RawMessage{"reminder": json.RawMessage(`true`)}, Schedule: &PersonalRuleSchedule{DateField: "due", LastRunField: "last", EveryDays: 1}}}}
	if err := ValidatePersonalCollection(s); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	out := PersonalApplyRules(s, map[string]json.RawMessage{"due": json.RawMessage(`"2026-10-20"`)}, now, true)
	if string(out["last"]) != `"2026-10-24T00:00:00Z"` || string(out["reminder"]) != "true" {
		t.Fatal(out)
	}
	out["reminder"] = json.RawMessage(`false`)
	again := PersonalApplyRules(s, out, now, true)
	if string(again["reminder"]) != "false" {
		t.Fatal("same occurrence repeated")
	}
	later := PersonalApplyRules(s, again, now.Add(24*time.Hour), true)
	if string(later["reminder"]) != "true" {
		t.Fatal("next occurrence missing")
	}
}
