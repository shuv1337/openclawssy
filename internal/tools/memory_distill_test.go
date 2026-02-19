package tools

import "testing"

func TestParseStrictCheckpointJSONAcceptsValidSchema(t *testing.T) {
	raw := `{"new_items":[{"kind":"preference","title":"Tone","content":"Be concise","importance":4,"confidence":0.9}],"updates":[{"id":"mem_1","new_content":"Updated content","confidence":0.8}]}`
	out, err := parseStrictCheckpointJSON(raw)
	if err != nil {
		t.Fatalf("parse strict checkpoint json: %v", err)
	}
	if len(out.NewItems) != 1 || len(out.Updates) != 1 {
		t.Fatalf("unexpected parsed output: %+v", out)
	}
}

func TestParseStrictCheckpointJSONRejectsUnknownFields(t *testing.T) {
	raw := `{"new_items":[{"kind":"preference","title":"Tone","content":"Be concise","importance":4,"confidence":0.9,"extra":true}],"updates":[]}`
	if _, err := parseStrictCheckpointJSON(raw); err == nil {
		t.Fatal("expected strict parser to reject unknown field")
	}
}
