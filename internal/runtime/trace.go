package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"

	"openclawssy/internal/agent"
)

type runTraceEnvelope struct {
	RunID                string                   `json:"run_id"`
	SessionID            string                   `json:"session_id,omitempty"`
	Channel              string                   `json:"channel,omitempty"`
	InputMessageHash     string                   `json:"input_message_hash"`
	ModelInputs          []modelInputTrace        `json:"model_inputs,omitempty"`
	ExtractedToolCalls   []toolExtractionTrace    `json:"extracted_tool_calls,omitempty"`
	ToolExecutionResults []toolExecutionResultLog `json:"tool_execution_results,omitempty"`
}

type modelInputTrace struct {
	Iteration       int    `json:"iteration"`
	Message         string `json:"message"`
	PromptLength    int    `json:"prompt_length"`
	HistoryInjected bool   `json:"history_injected"`
	RequestJSON     string `json:"request_json"`
}

type toolExtractionTrace struct {
	RawSnippet      string `json:"raw_snippet"`
	ParsedToolName  string `json:"parsed_tool_name,omitempty"`
	ParsedArguments string `json:"parsed_arguments,omitempty"`
	Accepted        bool   `json:"accepted"`
	Reason          string `json:"reason,omitempty"`
}

type toolExecutionResultLog struct {
	Tool       string `json:"tool"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Arguments  string `json:"arguments,omitempty"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

type runTraceCollector struct {
	mu      sync.Mutex
	env     runTraceEnvelope
	current int
}

func newRunTraceCollector(runID, sessionID, channel, message string) *runTraceCollector {
	hash := sha256.Sum256([]byte(message))
	return &runTraceCollector{
		env: runTraceEnvelope{
			RunID:            strings.TrimSpace(runID),
			SessionID:        strings.TrimSpace(sessionID),
			Channel:          strings.TrimSpace(channel),
			InputMessageHash: hex.EncodeToString(hash[:]),
		},
	}
}

func (c *runTraceCollector) RecordModelInput(message string, promptLength int, historyInjected bool, requestJSON string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current++
	c.env.ModelInputs = append(c.env.ModelInputs, modelInputTrace{
		Iteration:       c.current,
		Message:         message,
		PromptLength:    promptLength,
		HistoryInjected: historyInjected,
		RequestJSON:     requestJSON,
	})
}

func (c *runTraceCollector) RecordToolExtraction(rawSnippet, parsedTool string, parsedArguments json.RawMessage, accepted bool, reason string) {
	if c == nil {
		return
	}
	entry := toolExtractionTrace{
		RawSnippet:     strings.TrimSpace(rawSnippet),
		ParsedToolName: strings.TrimSpace(parsedTool),
		Accepted:       accepted,
		Reason:         strings.TrimSpace(reason),
	}
	if len(parsedArguments) > 0 {
		entry.ParsedArguments = strings.TrimSpace(string(parsedArguments))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env.ExtractedToolCalls = append(c.env.ExtractedToolCalls, entry)
}

func (c *runTraceCollector) RecordToolExecution(records []agent.ToolCallRecord) {
	if c == nil || len(records) == 0 {
		return
	}
	items := make([]toolExecutionResultLog, 0, len(records))
	for _, rec := range records {
		item := toolExecutionResultLog{
			Tool:       strings.TrimSpace(rec.Request.Name),
			ToolCallID: strings.TrimSpace(rec.Request.ID),
			Output:     strings.TrimSpace(rec.Result.Output),
			Error:      strings.TrimSpace(rec.Result.Error),
		}
		if len(rec.Request.Arguments) > 0 {
			item.Arguments = strings.TrimSpace(string(rec.Request.Arguments))
		}
		items = append(items, item)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env.ToolExecutionResults = append(c.env.ToolExecutionResults, items...)
}

func (c *runTraceCollector) Snapshot() map[string]any {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	copyEnv := c.env
	c.mu.Unlock()

	b, err := json.Marshal(copyEnv)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

type traceContextKey struct{}

func withRunTraceCollector(ctx context.Context, collector *runTraceCollector) context.Context {
	if collector == nil {
		return ctx
	}
	return context.WithValue(ctx, traceContextKey{}, collector)
}

func runTraceCollectorFromContext(ctx context.Context) *runTraceCollector {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(traceContextKey{})
	collector, _ := v.(*runTraceCollector)
	return collector
}
