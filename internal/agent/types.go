package agent

import (
	"context"
	"encoding/json"
	"time"
)

// ArtifactDoc is a prompt source document.
type ArtifactDoc struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// RunInput is the input contract for a single runner invocation.
type RunInput struct {
	AgentID           string        `json:"agent_id"`
	RunID             string        `json:"run_id"`
	Message           string        `json:"message"`
	ArtifactDocs      []ArtifactDoc `json:"artifact_docs"`
	PerFileByteLimit  int           `json:"per_file_byte_limit"`
	MaxToolIterations int           `json:"max_tool_iterations"`
}

// RunOutput is the finalized output contract for a run.
type RunOutput struct {
	Prompt      string           `json:"prompt"`
	FinalText   string           `json:"final_text"`
	ToolCalls   []ToolCallRecord `json:"tool_calls"`
	StartedAt   time.Time        `json:"started_at"`
	CompletedAt time.Time        `json:"completed_at"`
}

// ModelRequest is sent to the model on each loop iteration.
type ModelRequest struct {
	Prompt      string           `json:"prompt"`
	Message     string           `json:"message"`
	ToolResults []ToolCallResult `json:"tool_results"`
}

// ToolCallRequest is a model-requested tool invocation.
type ToolCallRequest struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ModelResponse returns final text and optional tool calls.
type ModelResponse struct {
	FinalText string            `json:"final_text"`
	ToolCalls []ToolCallRequest `json:"tool_calls"`
}

// ToolCallResult is the result returned by a tool executor.
type ToolCallResult struct {
	ID     string `json:"id"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// ToolCallRecord stores request + result for audit/output.
type ToolCallRecord struct {
	Request     ToolCallRequest `json:"request"`
	Result      ToolCallResult  `json:"result"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt time.Time       `json:"completed_at"`
}

// Model is an injectable model backend.
type Model interface {
	Generate(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

// ToolExecutor is an injectable tool backend.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCallRequest) (ToolCallResult, error)
}
