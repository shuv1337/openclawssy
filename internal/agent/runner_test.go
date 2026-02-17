package agent

import (
	"context"
	"errors"
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

func TestRunnerBlocksRepeatedToolCallAfterThreshold(t *testing.T) {
	model := &mockModel{
		responses: []ModelResponse{
			{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "4", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		},
	}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if !errors.Is(err, ErrRepeatedToolCall) {
		t.Fatalf("expected ErrRepeatedToolCall, got %v", err)
	}
	if len(out.ToolCalls) != 4 {
		t.Fatalf("expected four tool call records, got %d", len(out.ToolCalls))
	}
	if out.ToolCalls[1].Result.Error == "" || out.ToolCalls[2].Result.Error == "" || out.ToolCalls[3].Result.Error == "" {
		t.Fatalf("expected repeated-call guard errors in records: %+v", out.ToolCalls)
	}
}

func TestRunnerInjectsGuardErrorAndLetsModelRecover(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{FinalText: "done"},
	}}

	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 8}
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
	if out.ToolCalls[1].Result.Error == "" {
		t.Fatal("expected repeated tool call guard error in second record")
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
