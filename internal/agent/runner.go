package agent

import (
	"context"
	"errors"
	"time"
)

var (
	ErrModelRequired            = errors.New("agent runner requires model")
	ErrToolExecutorRequired     = errors.New("agent runner requires tool executor for tool calls")
	ErrToolIterationCapExceeded = errors.New("agent runner tool iteration cap exceeded")
)

const DefaultToolIterationCap = 8

// Runner executes the model/tool loop for a single run.
type Runner struct {
	Model             Model
	ToolExecutor      ToolExecutor
	PromptAssembler   func([]ArtifactDoc, int) string
	MaxToolIterations int
}

// Run executes: input -> assemble prompt -> model call -> optional tools -> finalize.
func (r Runner) Run(ctx context.Context, input RunInput) (RunOutput, error) {
	if r.Model == nil {
		return RunOutput{}, ErrModelRequired
	}

	assembler := r.PromptAssembler
	if assembler == nil {
		assembler = AssemblePrompt
	}

	toolCap := input.MaxToolIterations
	if toolCap <= 0 {
		toolCap = r.MaxToolIterations
	}
	if toolCap <= 0 {
		toolCap = DefaultToolIterationCap
	}

	out := RunOutput{StartedAt: time.Now().UTC()}
	out.Prompt = assembler(input.ArtifactDocs, input.PerFileByteLimit)

	toolResults := make([]ToolCallResult, 0)
	toolIterations := 0

	for {
		resp, err := r.Model.Generate(ctx, ModelRequest{
			Prompt:      out.Prompt,
			Message:     input.Message,
			ToolResults: append([]ToolCallResult(nil), toolResults...),
		})
		if err != nil {
			out.CompletedAt = time.Now().UTC()
			return out, err
		}

		if len(resp.ToolCalls) == 0 {
			out.FinalText = resp.FinalText
			out.CompletedAt = time.Now().UTC()
			return out, nil
		}

		if r.ToolExecutor == nil {
			out.CompletedAt = time.Now().UTC()
			return out, ErrToolExecutorRequired
		}

		if toolIterations >= toolCap {
			out.CompletedAt = time.Now().UTC()
			return out, ErrToolIterationCapExceeded
		}

		for _, call := range resp.ToolCalls {
			record := ToolCallRecord{
				Request:   call,
				StartedAt: time.Now().UTC(),
			}

			result, execErr := r.ToolExecutor.Execute(ctx, call)
			if result.ID == "" {
				result.ID = call.ID
			}
			if execErr != nil {
				result.Error = execErr.Error()
			}

			record.Result = result
			record.CompletedAt = time.Now().UTC()

			out.ToolCalls = append(out.ToolCalls, record)
			toolResults = append(toolResults, result)
		}

		toolIterations++
	}
}
