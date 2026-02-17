package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	EventRunStart          = "run.start"
	EventRunEnd            = "run.end"
	EventToolCall          = "tool.call"
	EventToolResult        = "tool.result"
	EventToolCallbackError = "tool.callback_error"
	EventPolicyDeny        = "policy.denied"
	defaultFileMode        = 0o600
	defaultDirMode         = 0o755
	defaultLineBreak       = '\n'
)

type Event struct {
	Timestamp time.Time      `json:"ts"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id,omitempty"`
	AgentID   string         `json:"agent_id,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type Logger struct {
	path   string
	redact func(any) any
	mu     sync.Mutex
}

func NewLogger(path string, redact func(any) any) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), defaultDirMode); err != nil {
		return nil, err
	}
	if redact == nil {
		redact = func(v any) any { return v }
	}
	return &Logger{path: path, redact: redact}, nil
}

func (l *Logger) LogEvent(ctx context.Context, eventType string, fields map[string]any) error {
	_ = ctx
	e := Event{Type: eventType, Timestamp: time.Now().UTC()}

	if len(fields) > 0 {
		if runID, ok := fields["run_id"].(string); ok {
			e.RunID = runID
		}
		if agentID, ok := fields["agent_id"].(string); ok {
			e.AgentID = agentID
		}
		if tool, ok := fields["tool"].(string); ok {
			e.Tool = tool
		}
		payload := make(map[string]any)
		for k, v := range fields {
			if k == "run_id" || k == "agent_id" || k == "tool" {
				continue
			}
			payload[k] = v
		}
		if len(payload) > 0 {
			if rv, ok := l.redact(payload).(map[string]any); ok {
				e.Payload = rv
			} else {
				e.Payload = payload
			}
		}
	}

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, defaultLineBreak)

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, defaultFileMode)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return err
	}

	return f.Sync()
}
