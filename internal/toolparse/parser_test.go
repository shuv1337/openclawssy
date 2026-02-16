package toolparse

import "testing"

func TestParseStrictIgnoresShellExplanation(t *testing.T) {
	content := "I will check this with ls and then summarize."
	res := ParseStrict(content, []string{"fs.list"}, 1)
	if len(res.Calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.Calls))
	}
}

func TestParseStrictRejectsToolResultObject(t *testing.T) {
	content := "```json\n{\"tool_name\":\"tool.result\",\"arguments\":{}}\n```"
	res := ParseStrict(content, nil, 1)
	if len(res.Calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.Calls))
	}
	if len(res.Extractions) == 0 || res.Extractions[0].Reason == "" {
		t.Fatalf("expected extraction reason, got %#v", res.Extractions)
	}
}

func TestParseStrictParsesValidToolJSONBlock(t *testing.T) {
	content := "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"
	res := ParseStrict(content, []string{"fs.list"}, 1)
	if len(res.Calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(res.Calls))
	}
	if res.Calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", res.Calls[0].Name)
	}
}

func TestParseStrictLimitsToSingleCall(t *testing.T) {
	content := "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```\n```json\n{\"tool_name\":\"fs.read\",\"arguments\":{\"path\":\"a.txt\"}}\n```"
	res := ParseStrict(content, []string{"fs.list", "fs.read"}, 1)
	if len(res.Calls) != 1 {
		t.Fatalf("expected one call due to strict max=1, got %d", len(res.Calls))
	}
	if res.Calls[0].Name != "fs.list" {
		t.Fatalf("expected first call only, got %q", res.Calls[0].Name)
	}
}
