package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type mockModel struct {
	responses []ModelResponse
	idx       int
	reqs      []ModelRequest
}

func (m *mockModel) Generate(_ context.Context, req ModelRequest) (ModelResponse, error) {
	m.reqs = append(m.reqs, req)
	if m.idx >= len(m.responses) {
		return ModelResponse{}, errors.New("no more responses")
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

type mockTools struct {
	results map[string]ToolCallResult
	calls   []ToolCallRequest
}

func (m *mockTools) Execute(_ context.Context, call ToolCallRequest) (ToolCallResult, error) {
	m.calls = append(m.calls, call)
	result, ok := m.results[call.ID]
	if !ok {
		result = ToolCallResult{ID: call.ID, Output: "ok:" + call.Name}
	}
	return result, nil
}

type slowToolExecutor struct{}

func (s slowToolExecutor) Execute(ctx context.Context, call ToolCallRequest) (ToolCallResult, error) {
	_ = call
	<-ctx.Done()
	return ToolCallResult{}, ctx.Err()
}

func TestRunnerBasicLoopWithTools(t *testing.T) {
	model := &mockModel{
		responses: []ModelResponse{
			{
				ToolCalls: []ToolCallRequest{{ID: "call-1", Name: "time.now"}},
			},
			{
				FinalText: "done",
			},
		},
	}

	tools := &mockTools{
		results: map[string]ToolCallResult{
			"call-1": {ID: "call-1", Output: "2026-02-15T00:00:00Z"},
		},
	}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 2}
	out, err := runner.Run(context.Background(), RunInput{
		Message:      "What time is it?",
		ArtifactDocs: []ArtifactDoc{{Name: "SOUL.md", Content: "help user"}},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call record, got %d", len(out.ToolCalls))
	}
	if len(model.reqs) != 2 {
		t.Fatalf("expected 2 model calls, got %d", len(model.reqs))
	}
	if len(model.reqs[1].ToolResults) != 1 {
		t.Fatalf("expected second model call to include tool result")
	}
	if model.reqs[1].ToolResults[0].Output != "2026-02-15T00:00:00Z" {
		t.Fatalf("unexpected tool result passed to model: %q", model.reqs[1].ToolResults[0].Output)
	}
}

func TestRunnerToolIterationCap(t *testing.T) {
	model := &mockModel{
		responses: []ModelResponse{
			{ToolCalls: []ToolCallRequest{{ID: "1", Name: "a"}}},
			{ToolCalls: []ToolCallRequest{{ID: "2", Name: "b"}}},
		},
	}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 1}

	out, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if err != nil {
		t.Fatalf("expected graceful fallback, got %v", err)
	}
	if out.FinalText == "" {
		t.Fatal("expected fallback final text when cap reached after tool results")
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected first tool call record retained, got %d", len(out.ToolCalls))
	}
}

func TestRunnerCachesRepeatedToolCallResult(t *testing.T) {
	model := &mockModel{
		responses: []ModelResponse{
			{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "4", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{FinalText: "done"},
		},
	}
	tools := &mockTools{results: map[string]ToolCallResult{"1": {ID: "1", Output: `{"entries":["a.txt"]}`}}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 4 {
		t.Fatalf("expected four tool call records, got %d", len(out.ToolCalls))
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected tool executor to run once and reuse cached result, got %d calls", len(tools.calls))
	}
	for i, rec := range out.ToolCalls {
		if rec.Result.Error != "" {
			t.Fatalf("expected no repeated-call guard errors, got record %d: %+v", i, rec)
		}
		if rec.Result.Output != `{"entries":["a.txt"]}` {
			t.Fatalf("expected cached output for record %d, got %q", i, rec.Result.Output)
		}
	}
}

func TestRunnerAllowsRepeatedCallAndLetsModelRecover(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{"1": {ID: "1", Output: "ok"}}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}
	out, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected repeated call to be served from cache, got %d tool executions", len(tools.calls))
	}
	if out.ToolCalls[0].Result.Output != out.ToolCalls[1].Result.Output {
		t.Fatalf("expected repeated call output to be reused, got %+v", out.ToolCalls)
	}
}

func TestRunnerAllowsRepeatedCallAfterPreviousToolError(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{FinalText: "done"},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "temporary failure"},
		"2": {ID: "2", Output: "ok"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{Message: "retry"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 2 {
		t.Fatalf("expected both repeated calls to execute after first error, got %d", len(tools.calls))
	}
}

func TestRunnerAppliesPerToolTimeout(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}}, {FinalText: "done"}}}
	runner := Runner{Model: model, ToolExecutor: slowToolExecutor{}, MaxToolIterations: 4}

	start := time.Now()
	out, err := runner.Run(context.Background(), RunInput{Message: "timeout", ToolTimeoutMS: 20})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected one tool call record, got %d", len(out.ToolCalls))
	}
	if out.ToolCalls[0].Result.Error == "" {
		t.Fatal("expected timeout error in tool result")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("expected run to finish quickly due to timeout")
	}
}

func TestRunnerNormalizesDuplicateToolCallIDs(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{
			{ID: "tool-json-1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)},
			{ID: "tool-json-1", Name: "fs.read", Arguments: []byte(`{"path":"README.md"}`)},
		}},
		{FinalText: "done"},
	}}

	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 4}
	out, err := runner.Run(context.Background(), RunInput{Message: "inspect files"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(out.ToolCalls))
	}

	firstID := out.ToolCalls[0].Request.ID
	secondID := out.ToolCalls[1].Request.ID
	if firstID != "tool-json-1" {
		t.Fatalf("expected first ID to be preserved, got %q", firstID)
	}
	if secondID == firstID {
		t.Fatalf("expected second call ID to be rewritten, got duplicate %q", secondID)
	}
	if !strings.HasPrefix(secondID, "tool-json-1-") {
		t.Fatalf("expected rewritten ID to keep base prefix, got %q", secondID)
	}

	if out.ToolCalls[0].Result.ID != firstID || out.ToolCalls[1].Result.ID != secondID {
		t.Fatalf("expected results to use normalized IDs, got %+v", out.ToolCalls)
	}

	if len(model.reqs) != 2 || len(model.reqs[1].ToolResults) != 2 {
		t.Fatalf("expected two tool results in follow-up model request, got %#v", model.reqs)
	}
	if model.reqs[1].ToolResults[0].ID != firstID || model.reqs[1].ToolResults[1].ID != secondID {
		t.Fatalf("unexpected tool result IDs passed to model: %+v", model.reqs[1].ToolResults)
	}
}

func TestRunnerInvokesOnToolCallForEachRecord(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "tool-1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "tool-2", Name: "fs.read", Arguments: []byte(`{"path":"README.md"}`)}}},
		{FinalText: "done"},
	}}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 8}

	notifications := make([]ToolCallRecord, 0, 2)
	out, err := runner.Run(context.Background(), RunInput{
		Message: "inspect project",
		OnToolCall: func(rec ToolCallRecord) error {
			notifications = append(notifications, rec)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(out.ToolCalls))
	}
	if len(notifications) != 2 {
		t.Fatalf("expected two tool notifications, got %d", len(notifications))
	}
	if notifications[0].Request.Name != "fs.list" || notifications[1].Request.Name != "fs.read" {
		t.Fatalf("unexpected notification order: %+v", notifications)
	}
}
