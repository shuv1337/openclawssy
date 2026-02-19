package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerWritesEventJSONL(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	mgr, err := NewManager(agentsDir, "default", Options{Enabled: true, BufferSize: 8})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	ts := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)
	err = mgr.IngestEvent(context.Background(), Event{
		Type:      EventTypeUserMessage,
		Text:      "hello world",
		RunID:     "run_1",
		SessionID: "sess_1",
		Timestamp: ts,
		Metadata: map[string]any{
			"source": "dashboard",
		},
	})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}

	path := filepath.Join(agentsDir, "default", "memory", "events", "2026-02-18.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 event line, got %d", len(lines))
	}

	var got Event
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if got.ID == "" {
		t.Fatal("expected event id to be assigned")
	}
	if got.Type != EventTypeUserMessage {
		t.Fatalf("expected type %q, got %q", EventTypeUserMessage, got.Type)
	}
	if got.Text != "hello world" {
		t.Fatalf("expected text to roundtrip, got %q", got.Text)
	}
}

func TestManagerDropsWhenQueueFullWithoutBlocking(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	mgr, err := NewManager(agentsDir, "default", Options{Enabled: true, BufferSize: 1})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	ctx := context.Background()
	var dropped bool
	for i := 0; i < 5000; i++ {
		err := mgr.IngestEvent(ctx, Event{Type: EventTypeToolCall, Text: "x"})
		if errors.Is(err, ErrQueueFull) {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Fatal("expected at least one queue-full drop")
	}
	if mgr.Stats().DroppedEvents == 0 {
		t.Fatal("expected dropped events stat to increase")
	}
}

func TestNewManagerRejectsInvalidAgentID(t *testing.T) {
	_, err := NewManager(t.TempDir(), "../escape", Options{Enabled: true})
	if !errors.Is(err, ErrInvalidAgentID) {
		t.Fatalf("expected ErrInvalidAgentID, got %v", err)
	}
}

func TestAppendEventReadEventsAndCheckpointRecord(t *testing.T) {
	agentsDir := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	now := time.Now().UTC()
	if err := AppendEvent(agentsDir, "default", Event{Type: EventTypeDecisionLog, Text: "keep retries bounded", Timestamp: now}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := AppendEvent(agentsDir, "default", Event{Type: EventTypeError, Text: "timeout", Timestamp: now.Add(time.Second)}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	events, err := ReadEventsSince(agentsDir, "default", now.Add(-time.Second), 10)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	record := CheckpointRecord{AgentID: "default", CreatedAt: now.Add(2 * time.Second), EventCount: len(events)}
	checkpointPath, err := WriteCheckpointRecord(agentsDir, "default", record)
	if err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	if _, err := os.Stat(checkpointPath); err != nil {
		t.Fatalf("expected checkpoint file, got %v", err)
	}
	loaded, ok, err := LoadLatestCheckpointRecord(agentsDir, "default")
	if err != nil {
		t.Fatalf("load latest checkpoint: %v", err)
	}
	if !ok {
		t.Fatal("expected latest checkpoint to exist")
	}
	if loaded.EventCount != 2 {
		t.Fatalf("expected event_count=2, got %d", loaded.EventCount)
	}
}
