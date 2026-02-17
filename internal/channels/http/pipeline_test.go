package httpchannel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type traceExecutor struct {
	result ExecutionResult
	err    error
}

type blockingExecutor struct {
	release <-chan struct{}
}

type flakyRetryableExecutor struct {
	mu    sync.Mutex
	calls int
}

func (f *flakyRetryableExecutor) Execute(_ context.Context, _ ExecutionInput) (ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		return ExecutionResult{Trace: map[string]any{"first_attempt": true}}, context.DeadlineExceeded
	}
	return ExecutionResult{Output: "ok", Trace: map[string]any{"attempt": f.calls}}, nil
}

func (t traceExecutor) Execute(_ context.Context, _ ExecutionInput) (ExecutionResult, error) {
	return t.result, t.err
}

func (b blockingExecutor) Execute(_ context.Context, _ ExecutionInput) (ExecutionResult, error) {
	<-b.release
	return ExecutionResult{Output: "done"}, nil
}

func TestQueueRunPersistsSessionAndTrace(t *testing.T) {
	store := NewInMemoryRunStore()
	queued, err := QueueRun(context.Background(), store, traceExecutor{result: ExecutionResult{Output: "ok", Trace: map[string]any{"run_id": "trace-run", "prompt_length": float64(12)}}}, "agent-1", "hello", "dashboard", "chat_123")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "completed" {
			if run.SessionID != "chat_123" {
				t.Fatalf("expected session_id chat_123, got %q", run.SessionID)
			}
			if run.Trace == nil || run.Trace["run_id"] != "trace-run" {
				t.Fatalf("expected trace to persist, got %#v", run.Trace)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete in time, last status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQueueRunPersistsTraceOnFailure(t *testing.T) {
	store := NewInMemoryRunStore()
	queued, err := QueueRun(context.Background(), store, traceExecutor{result: ExecutionResult{Trace: map[string]any{"run_id": "trace-fail"}}, err: context.DeadlineExceeded}, "agent-1", "hello", "dashboard", "chat_123")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "failed" {
			if run.Trace == nil || run.Trace["run_id"] != "trace-fail" {
				t.Fatalf("expected failure trace to persist, got %#v", run.Trace)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not fail in time, last status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWaitForQueuedRunsBlocksUntilCompletion(t *testing.T) {
	store := NewInMemoryRunStore()
	release := make(chan struct{})
	_, err := QueueRun(context.Background(), store, blockingExecutor{release: release}, "agent-1", "hello", "dashboard", "chat_123")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shortCancel()
	err = WaitForQueuedRuns(shortCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}

	close(release)
	longCtx, longCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer longCancel()
	if err := WaitForQueuedRuns(longCtx); err != nil {
		t.Fatalf("expected in-flight drain success, got %v", err)
	}
}

func TestQueueRunRetriesRetryableExecutorFailureOnce(t *testing.T) {
	store := NewInMemoryRunStore()
	exec := &flakyRetryableExecutor{}
	queued, err := QueueRun(context.Background(), store, exec, "agent-1", "hello", "dashboard", "chat_123")
	if err != nil {
		t.Fatalf("queue run: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		run, getErr := store.Get(context.Background(), queued.ID)
		if getErr != nil {
			t.Fatalf("get run: %v", getErr)
		}
		if run.Status == "completed" {
			if run.Output != "ok" {
				t.Fatalf("expected successful retry output, got %q", run.Output)
			}
			if run.Trace == nil || run.Trace["queue_retry_attempts"] != 1 {
				t.Fatalf("expected retry metadata in trace, got %#v", run.Trace)
			}
			if exec.calls != 2 {
				t.Fatalf("expected exactly two executor attempts, got %d", exec.calls)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete in time, last status=%q", run.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
