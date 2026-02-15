package scheduler

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreAddListRemove(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	job := Job{
		ID:       "job-1",
		Schedule: "@every 1m",
		AgentID:  "agent-alpha",
		Message:  "hello",
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != job.ID {
		t.Fatalf("expected job id %q, got %q", job.ID, jobs[0].ID)
	}

	if err := store.Remove(job.ID); err != nil {
		t.Fatalf("remove job: %v", err)
	}
	if got := len(store.List()); got != 0 {
		t.Fatalf("expected 0 jobs after remove, got %d", got)
	}
}

func TestStorePersistenceReload(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	job := Job{
		ID:           "job-persist",
		Schedule:     "@every 10s",
		AgentID:      "agent-beta",
		Message:      "persist me",
		Mode:         "isolated",
		NotifyTarget: "stdout",
		Enabled:      true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	reloaded, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}

	jobs := reloaded.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 reloaded job, got %d", len(jobs))
	}
	if jobs[0].ID != job.ID || jobs[0].AgentID != job.AgentID || jobs[0].Message != job.Message {
		t.Fatalf("reloaded job mismatch: %+v", jobs[0])
	}
}

func TestExecutorTriggerExecution(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	job := Job{
		ID:       "job-run",
		Schedule: "@every 1ms",
		AgentID:  "agent-gamma",
		Message:  "run now",
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add job: %v", err)
	}

	var mu sync.Mutex
	runs := 0
	var gotAgent string
	var gotMessage string

	exec := NewExecutor(store, time.Millisecond, func(agentID string, message string) {
		mu.Lock()
		defer mu.Unlock()
		runs++
		gotAgent = agentID
		gotMessage = message
	})

	now := time.Now().UTC()
	exec.check(now)

	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Fatalf("expected 1 run trigger, got %d", runs)
	}
	if gotAgent != job.AgentID || gotMessage != job.Message {
		t.Fatalf("unexpected run args: agent=%q message=%q", gotAgent, gotMessage)
	}

	updated := store.List()[0]
	if updated.LastRun == "" {
		t.Fatal("expected lastRun to be updated")
	}
}
