package httpchannel

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ExecutionResult struct {
	Output       string
	ArtifactPath string
	DurationMS   int64
	ToolCalls    int
	Provider     string
	Model        string
	Trace        map[string]any
}

var defaultQueuedRunTracker = newQueuedRunTracker()

func QueueRun(ctx context.Context, store RunStore, executor RunExecutor, agentID, message, source, sessionID string) (Run, error) {
	now := time.Now().UTC()
	run := Run{
		ID:        newRunID(),
		AgentID:   agentID,
		Message:   message,
		Source:    source,
		SessionID: sessionID,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}
	created, err := store.Create(ctx, run)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	defaultQueuedRunTracker.begin()
	go executeQueuedRun(context.Background(), store, executor, created)
	return created, nil
}

func executeQueuedRun(ctx context.Context, store RunStore, executor RunExecutor, run Run) {
	defer defaultQueuedRunTracker.done()

	run.Status = "running"
	run.UpdatedAt = time.Now().UTC()
	_ = store.Update(ctx, run)

	result, err := executor.Execute(ctx, ExecutionInput{AgentID: run.AgentID, Message: run.Message, Source: run.Source, SessionID: run.SessionID})
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		run.Trace = result.Trace
		run.Provider = result.Provider
		run.Model = result.Model
		run.ToolCalls = result.ToolCalls
	} else {
		run.Status = "completed"
		run.Output = result.Output
		run.ArtifactPath = result.ArtifactPath
		run.DurationMS = result.DurationMS
		run.ToolCalls = result.ToolCalls
		run.Provider = result.Provider
		run.Model = result.Model
		run.Trace = result.Trace
	}
	run.UpdatedAt = time.Now().UTC()
	_ = store.Update(ctx, run)
}

func WaitForQueuedRuns(ctx context.Context) error {
	return defaultQueuedRunTracker.wait(ctx)
}

type queuedRunTracker struct {
	mu       sync.Mutex
	inFlight int
	waitCh   chan struct{}
}

func newQueuedRunTracker() *queuedRunTracker {
	ch := make(chan struct{})
	close(ch)
	return &queuedRunTracker{waitCh: ch}
}

func (t *queuedRunTracker) begin() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight == 0 {
		t.waitCh = make(chan struct{})
	}
	t.inFlight++
}

func (t *queuedRunTracker) done() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inFlight <= 0 {
		return
	}
	t.inFlight--
	if t.inFlight == 0 {
		close(t.waitCh)
	}
}

func (t *queuedRunTracker) wait(ctx context.Context) error {
	t.mu.Lock()
	if t.inFlight == 0 {
		t.mu.Unlock()
		return nil
	}
	ch := t.waitCh
	t.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
