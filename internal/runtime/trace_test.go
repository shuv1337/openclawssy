package runtime

import (
	"testing"

	"openclawssy/internal/agent"
)

func TestSummarizeToolExecutionFsWrite(t *testing.T) {
	summary := summarizeToolExecution("fs.write", `{"path":"templates/index.html","bytes":1200,"lines":42}`, "")
	if summary != "wrote 42 line(s) to templates/index.html" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestSummarizeToolExecutionError(t *testing.T) {
	summary := summarizeToolExecution("fs.read", "", "permission denied")
	if summary != "error: permission denied" {
		t.Fatalf("unexpected error summary: %q", summary)
	}
}

func TestRecordToolExecutionAddsSummaryToTrace(t *testing.T) {
	collector := newRunTraceCollector("run_1", "session_1", "dashboard", "write file")
	collector.RecordToolExecution([]agent.ToolCallRecord{{
		Request: agent.ToolCallRequest{ID: "tool-json-1", Name: "fs.write"},
		Result:  agent.ToolCallResult{ID: "tool-json-1", Output: `{"path":"Dockerfile","bytes":320,"lines":12}`},
	}})

	snapshot := collector.Snapshot()
	items, ok := snapshot["tool_execution_results"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one tool trace item, got %#v", snapshot["tool_execution_results"])
	}
	entry, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected trace entry shape: %#v", items[0])
	}
	if entry["summary"] != "wrote 12 line(s) to Dockerfile" {
		t.Fatalf("expected summary in trace entry, got %#v", entry)
	}
}
