package httpchannel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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

const queuedRunMaxAttempts = 2

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

	result, err := executeWithRetry(ctx, executor, ExecutionInput{AgentID: run.AgentID, Message: run.Message, Source: run.Source, SessionID: run.SessionID})
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

func executeWithRetry(ctx context.Context, executor RunExecutor, input ExecutionInput) (ExecutionResult, error) {
	var lastResult ExecutionResult
	var lastErr error
	for attempt := 1; attempt <= queuedRunMaxAttempts; attempt++ {
		result, err := executor.Execute(ctx, input)
		if err == nil {
			if attempt > 1 {
				result.Trace = withRetryMeta(result.Trace, attempt-1)
			}
			return result, nil
		}
		lastResult = result
		lastErr = err
		if !isRetryableExecutionError(err) || attempt == queuedRunMaxAttempts {
			break
		}
	}
	if lastResult.Trace != nil {
		lastResult.Trace = withRetryMeta(lastResult.Trace, 1)
	}
	return lastResult, lastErr
}

func withRetryMeta(trace map[string]any, retries int) map[string]any {
	if retries <= 0 {
		return trace
	}
	if trace == nil {
		trace = map[string]any{}
	}
	trace["queue_retry_attempts"] = retries
	return trace
}

func isRetryableExecutionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "unexpected eof")
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
