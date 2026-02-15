package agent

import (
	"context"
	"errors"
	"testing"
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

	_, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if !errors.Is(err, ErrToolIterationCapExceeded) {
		t.Fatalf("expected ErrToolIterationCapExceeded, got %v", err)
	}
}
