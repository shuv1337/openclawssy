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

func TestRecordToolExecutionIncludesCallbackErrorInTrace(t *testing.T) {
	collector := newRunTraceCollector("run_2", "session_2", "dashboard", "list files")
	collector.RecordToolExecution([]agent.ToolCallRecord{{
		Request:     agent.ToolCallRequest{ID: "tool-json-2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)},
		Result:      agent.ToolCallResult{ID: "tool-json-2", Output: `{"entries":["README.md"]}`},
		CallbackErr: "runtime: append tool message: permission denied",
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
	if entry["callback_error"] != "runtime: append tool message: permission denied" {
		t.Fatalf("expected callback_error in trace entry, got %#v", entry)
	}
}

func TestSummarizeToolExecutionShellFallback(t *testing.T) {
	summary := summarizeToolExecution("shell.exec", `{"stdout":"ok","stderr":"","exit_code":0,"shell_fallback":"sh"}`, "")
	if summary != "shell command completed via sh fallback (exit 0)" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestRecordThinkingPersistsThinkingFields(t *testing.T) {
	collector := newRunTraceCollector("run_3", "session_3", "dashboard", "hello")
	collector.RecordThinking("redacted notes", true)

	snapshot := collector.Snapshot()
	if snapshot["thinking"] != "redacted notes" {
		t.Fatalf("expected thinking in trace snapshot, got %#v", snapshot["thinking"])
	}
	if snapshot["thinking_present"] != true {
		t.Fatalf("expected thinking_present=true, got %#v", snapshot["thinking_present"])
	}
}
