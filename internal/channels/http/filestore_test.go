package httpchannel

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileRunStorePersistsRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.json")
	store, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	run := Run{ID: "run-1", AgentID: "a", Message: "m", Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if _, err := store.Create(context.Background(), run); err != nil {
		t.Fatalf("create: %v", err)
	}

	reloaded, err := NewFileRunStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := reloaded.Get(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get reloaded run: %v", err)
	}
	if got.ID != run.ID || got.AgentID != run.AgentID {
		t.Fatalf("unexpected run: %+v", got)
	}
}
