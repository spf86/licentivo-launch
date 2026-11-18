package app

import (
	"encoding/json"
	"testing"
)

func TestAIToolSchemasAndReadOnlyBoundary(t *testing.T) {
	s := &Service{}
	readonly := s.aiTools(true)
	if len(readonly) != 1 || readonly[0].Name != "query_system" {
		t.Fatal("scheduled task exposes writes")
	}
	var schema map[string]any
	for _, tool := range s.aiTools(false) {
		raw, err := json.Marshal(tool.Parameters)
		if err != nil {
			t.Fatal(err)
		}
		if json.Unmarshal(raw, &schema) != nil {
			t.Fatal("invalid schema")
		}
		if required, ok := schema["required"]; ok {
			if _, ok := required.([]any); !ok {
				t.Fatal(tool.Name, "required must be a JSON array")
			}
		}
	}
	for name, action := range s.aiActions() {
		if action.Validate(json.RawMessage(`{"api_key":"secret","shell":"arbitrary"}`)) == nil {
			t.Fatal(name, "accepted credentials or arbitrary commands")
		}
	}
}
