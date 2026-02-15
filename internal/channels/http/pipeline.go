package httpchannel

import (
	"context"
	"fmt"
	"time"
)

type ExecutionResult struct {
	Output       string
	ArtifactPath string
	DurationMS   int64
	ToolCalls    int
	Provider     string
	Model        string
}

func QueueRun(ctx context.Context, store RunStore, executor RunExecutor, agentID, message, source string) (Run, error) {
	now := time.Now().UTC()
	run := Run{
		ID:        newRunID(),
		AgentID:   agentID,
		Message:   message,
		Source:    source,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}
	created, err := store.Create(ctx, run)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	go executeQueuedRun(context.Background(), store, executor, created)
	return created, nil
}

func executeQueuedRun(ctx context.Context, store RunStore, executor RunExecutor, run Run) {
	run.Status = "running"
	run.UpdatedAt = time.Now().UTC()
	_ = store.Update(ctx, run)

	result, err := executor.Execute(ctx, run.AgentID, run.Message)
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
	} else {
		run.Status = "completed"
		run.Output = result.Output
		run.ArtifactPath = result.ArtifactPath
		run.DurationMS = result.DurationMS
		run.ToolCalls = result.ToolCalls
		run.Provider = result.Provider
		run.Model = result.Model
	}
	run.UpdatedAt = time.Now().UTC()
	_ = store.Update(ctx, run)
}
