package appops

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInjectedJSONSchemaValidationMatchesAdvertisedContract(t *testing.T) {
	schema := &Schema{Raw: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":2,"maxLength":12}},"required":["query"],"additionalProperties":false}`)}
	if err := schema.Validate(map[string]any{"query": "meeting"}); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	for _, args := range []map[string]any{
		{},
		{"query": "x"},
		{"query": "a sufficiently long query"},
		{"query": "valid", "token": "secret"},
	} {
		if err := schema.Validate(args); err == nil {
			t.Errorf("invalid input accepted: %#v", args)
		}
	}
	encoded, err := json.Marshal(schema)
	if err != nil || !json.Valid(encoded) || !strings.Contains(string(encoded), `"additionalProperties":false`) {
		t.Fatalf("the raw schema was not preserved for providers: %s, %v", encoded, err)
	}
}

func TestInjectedJSONSchemaFailsClosedOnUnenforcedKeywords(t *testing.T) {
	schema := &Schema{Raw: json.RawMessage(`{"type":"object","oneOf":[{"required":["a"]},{"required":["b"]}]}`)}
	if err := schema.Validate(map[string]any{"a": "ok"}); err == nil || !strings.Contains(err.Error(), "unsupported keyword") {
		t.Fatalf("unsupported schema constraint was ignored: %v", err)
	}
}
