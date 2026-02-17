package toolparse

import (
	"fmt"
	"strings"
	"testing"
)

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

func TestParseStrictDisallowedToolReasonIsExplicit(t *testing.T) {
	content := "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```"
	res := ParseStrict(content, []string{"fs.list"}, 1)
	if len(res.Calls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.Calls))
	}
	if len(res.Extractions) != 1 {
		t.Fatalf("expected one extraction, got %d", len(res.Extractions))
	}
	reason := res.Extractions[0].Reason
	if !strings.Contains(reason, "shell.exec") || !strings.Contains(reason, "not allowed") {
		t.Fatalf("expected explicit disallowed-tool reason, got %q", reason)
	}
}

func TestParseToolCallsParsesInlineJSONObjectCandidate(t *testing.T) {
	text := `I'll do it now: {"tool_name":"fs.list","arguments":{"path":"."}}`
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", calls[0].Name)
	}
	if len(diag.Candidates) == 0 {
		t.Fatal("expected diagnostics candidates")
	}
}

func TestParseToolCallsParsesFencedToolBlock(t *testing.T) {
	text := "```tool\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", calls[0].Name)
	}
}

func TestParseToolCallsParsesArrayOfToolCalls(t *testing.T) {
	text := `[{"tool_name":"fs.list","arguments":{"path":"."}},{"tool_name":"fs.read","arguments":{"path":"README.md"}}]`
	calls, _ := ParseToolCalls(text, []string{"fs.list", "fs.read"})
	if len(calls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" || calls[1].Name != "fs.read" {
		t.Fatalf("unexpected call names: %+v", calls)
	}
}

func TestParseToolCallsDeterministicGeneratedIDsWithoutIDs(t *testing.T) {
	text := `[{"tool_name":"fs.list","arguments":{"path":"."}},{"tool_name":"fs.read","arguments":{"path":"README.md"}}]`
	first, _ := ParseToolCalls(text, []string{"fs.list", "fs.read"})
	second, _ := ParseToolCalls(text, []string{"fs.list", "fs.read"})

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected two calls from each parse, got %d and %d", len(first), len(second))
	}
	if first[0].ID != "tool-json-1" || first[1].ID != "tool-json-2" {
		t.Fatalf("unexpected generated ids: %q, %q", first[0].ID, first[1].ID)
	}
	if first[0].ID != second[0].ID || first[1].ID != second[1].ID {
		t.Fatalf("expected deterministic ids across parses, got %+v and %+v", first, second)
	}
}

func TestParseToolCallsCanonicalizesAliases(t *testing.T) {
	text := "```json\n{\"tool_name\":\"bash.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```"
	calls, _ := ParseToolCalls(text, []string{"shell.exec"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	if calls[0].Name != "shell.exec" {
		t.Fatalf("expected canonical shell.exec, got %q", calls[0].Name)
	}
}

func TestParseToolCallsCapsReturnedCallsAtSix(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 8; i++ {
		b.WriteString("```json\n")
		b.WriteString(fmt.Sprintf(`{"tool_name":"fs.list","arguments":{"path":"./%d"}}`, i))
		b.WriteString("\n```\n")
	}
	calls, diag := ParseToolCalls(b.String(), []string{"fs.list"})
	if len(calls) != 6 {
		t.Fatalf("expected 6 capped tool calls, got %d", len(calls))
	}
	if len(diag.Candidates) != 8 {
		t.Fatalf("expected diagnostics for all 8 candidates, got %d", len(diag.Candidates))
	}
}

func TestParseToolCallsDiagnosticsIncludeRejectedReasons(t *testing.T) {
	text := strings.Join([]string{
		"```json\n{\"tool_name\":\"fs.list\"}\n```",
		"```json\n{invalid}\n```",
		"```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```",
	}, "\n")
	_, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(diag.Rejected) != 3 {
		t.Fatalf("expected 3 rejected candidates, got %d", len(diag.Rejected))
	}
	joined := ""
	for _, item := range diag.Rejected {
		joined += " " + strings.ToLower(item.Reason)
	}
	if !strings.Contains(joined, "missing arguments field") {
		t.Fatalf("expected missing arguments reason, got %q", joined)
	}
	if !strings.Contains(joined, "invalid json") {
		t.Fatalf("expected invalid json reason, got %q", joined)
	}
	if !strings.Contains(joined, "not allowed") {
		t.Fatalf("expected not allowed reason, got %q", joined)
	}
}

func TestParseToolCallsRejectsMissingArgumentsField(t *testing.T) {
	text := `{"tool_name":"fs.list"}`
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 0 {
		t.Fatalf("expected no accepted calls, got %d", len(calls))
	}
	if len(diag.Rejected) != 1 {
		t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
	}
	if diag.Rejected[0].Reason != "missing arguments field" {
		t.Fatalf("expected missing-arguments reason, got %q", diag.Rejected[0].Reason)
	}
}

func TestParseToolCallsRejectsNonObjectArguments(t *testing.T) {
	tests := []struct {
		name     string
		jsonText string
	}{
		{name: "null", jsonText: `{"tool_name":"fs.list","arguments":null}`},
		{name: "string", jsonText: `{"tool_name":"fs.list","arguments":"."}`},
		{name: "array", jsonText: `{"tool_name":"fs.list","arguments":[]}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls, diag := ParseToolCalls(tc.jsonText, []string{"fs.list"})
			if len(calls) != 0 {
				t.Fatalf("expected no accepted calls, got %d", len(calls))
			}
			if len(diag.Rejected) != 1 {
				t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
			}
			if diag.Rejected[0].Reason != "arguments must be a JSON object" {
				t.Fatalf("unexpected rejection reason: %q", diag.Rejected[0].Reason)
			}
		})
	}
}

func TestParseToolCallsRepairsTrailingCommaJSON(t *testing.T) {
	text := "```json\nI will call a tool now.\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\",},}\nDone.\n```"
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 1 {
		t.Fatalf("expected one tool call after repair, got %d", len(calls))
	}
	if calls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool name: %q", calls[0].Name)
	}
	if len(diag.Rejected) != 0 {
		t.Fatalf("expected no rejected candidates, got %#v", diag.Rejected)
	}
}

func TestParseToolCallsInvalidJSONHasDebugReason(t *testing.T) {
	text := "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":.}}\n```"
	_, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(diag.Rejected) != 1 {
		t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
	}
	reason := diag.Rejected[0].Reason
	if !strings.Contains(reason, "invalid JSON:") {
		t.Fatalf("expected detailed invalid JSON reason, got %q", reason)
	}
	if !strings.Contains(reason, "invalid character") && !strings.Contains(reason, "repair failed:") {
		t.Fatalf("expected parse detail in rejection reason, got %q", reason)
	}
}

func TestParseToolCallsRepairStillEnforcesAllowlist(t *testing.T) {
	text := "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\",},}\n```"
	calls, diag := ParseToolCalls(text, []string{"fs.list"})
	if len(calls) != 0 {
		t.Fatalf("expected no calls for disallowed tool, got %d", len(calls))
	}
	if len(diag.Rejected) != 1 {
		t.Fatalf("expected one rejected candidate, got %d", len(diag.Rejected))
	}
	reason := strings.ToLower(diag.Rejected[0].Reason)
	if !strings.Contains(reason, "not allowed") {
		t.Fatalf("expected allowlist rejection, got %q", diag.Rejected[0].Reason)
	}
	if strings.Contains(reason, "invalid json") {
		t.Fatalf("expected repaired JSON to pass parser before allowlist check, got %q", diag.Rejected[0].Reason)
	}
}
