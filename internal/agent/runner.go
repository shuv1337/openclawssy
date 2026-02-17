package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrModelRequired            = errors.New("agent runner requires model")
	ErrToolExecutorRequired     = errors.New("agent runner requires tool executor for tool calls")
	ErrToolIterationCapExceeded = errors.New("agent runner tool iteration cap exceeded")
)

const (
	DefaultToolIterationCap          = 120
	DefaultToolTimeout               = 900 * time.Second
	repeatedNoProgressLoopCapTrigger = 6
)

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

	messages := append([]ChatMessage(nil), input.Messages...)
	if len(messages) == 0 {
		messages = []ChatMessage{{Role: "user", Content: input.Message}}
	}

	toolResults := make([]ToolCallResult, 0)
	toolIterations := 0
	toolCallOrdinal := 0
	usedToolCallIDs := make(map[string]struct{})
	cachedToolResults := make(map[string]ToolCallResult)
	cachedFailedToolResults := make(map[string]ToolCallResult)
	failedToolCallCounts := make(map[string]int)
	failedToolCallErrors := make(map[string]string)
	toolTimeout := time.Duration(input.ToolTimeoutMS) * time.Millisecond
	if toolTimeout <= 0 {
		toolTimeout = DefaultToolTimeout
	}
	noProgressIterations := 0

	for {
		resp, err := r.Model.Generate(ctx, ModelRequest{
			SystemPrompt: out.Prompt,
			Messages:     append([]ChatMessage(nil), messages...),
			AllowedTools: append([]string(nil), input.AllowedTools...),
			Prompt:       out.Prompt,
			Message:      input.Message,
			ToolResults:  append([]ToolCallResult(nil), toolResults...),
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
			if len(toolResults) > 0 {
				if finalized := finalizeFromToolResults(ctx, r.Model, out.Prompt, messages, input.Message, toolResults); finalized != "" {
					out.FinalText = finalized
					out.CompletedAt = time.Now().UTC()
					return out, nil
				}
				out.FinalText = fallbackFromToolResults(toolResults, toolCap)
				out.CompletedAt = time.Now().UTC()
				return out, nil
			}
			out.CompletedAt = time.Now().UTC()
			return out, ErrToolIterationCapExceeded
		}

		hadFreshExecution := false
		for _, incoming := range resp.ToolCalls {
			toolCallOrdinal++
			call := incoming
			call.ID = uniqueToolCallID(call.ID, toolCallOrdinal, usedToolCallIDs)

			callKey := call.Name + "|" + string(call.Arguments)
			if callKey != "|" {
				if cached, ok := cachedToolResults[callKey]; ok {
					now := time.Now().UTC()
					cached.ID = call.ID
					record := ToolCallRecord{Request: call, Result: cached, StartedAt: now, CompletedAt: now}
					out.ToolCalls = append(out.ToolCalls, record)
					toolResults = append(toolResults, record.Result)
					if input.OnToolCall != nil {
						if err := input.OnToolCall(record); err != nil {
							out.CompletedAt = time.Now().UTC()
							return out, err
						}
					}
					continue
				}
				if cached, ok := cachedFailedToolResults[callKey]; ok {
					now := time.Now().UTC()
					cached.ID = call.ID
					record := ToolCallRecord{Request: call, Result: cached, StartedAt: now, CompletedAt: now}
					out.ToolCalls = append(out.ToolCalls, record)
					toolResults = append(toolResults, record.Result)
					if input.OnToolCall != nil {
						if err := input.OnToolCall(record); err != nil {
							out.CompletedAt = time.Now().UTC()
							return out, err
						}
					}
					continue
				}
			}

			record := ToolCallRecord{
				Request:   call,
				StartedAt: time.Now().UTC(),
			}

			execCtx, cancel := context.WithTimeout(ctx, toolTimeout)
			result, execErr := r.ToolExecutor.Execute(execCtx, call)
			cancel()
			if result.ID == "" {
				result.ID = call.ID
			}
			if execErr != nil {
				result.Error = execErr.Error()
			}

			record.Result = result
			record.CompletedAt = time.Now().UTC()
			hadFreshExecution = true

			if callKey != "|" {
				if strings.TrimSpace(result.Error) == "" {
					cachedToolResults[callKey] = ToolCallResult{Output: result.Output}
					delete(cachedFailedToolResults, callKey)
					delete(failedToolCallCounts, callKey)
					delete(failedToolCallErrors, callKey)
				} else {
					errText := strings.TrimSpace(result.Error)
					if failedToolCallErrors[callKey] == errText {
						failedToolCallCounts[callKey]++
					} else {
						failedToolCallErrors[callKey] = errText
						failedToolCallCounts[callKey] = 1
					}
					if failedToolCallCounts[callKey] >= 2 {
						cachedFailedToolResults[callKey] = ToolCallResult{Output: result.Output, Error: result.Error}
					}
				}
			}

			out.ToolCalls = append(out.ToolCalls, record)
			toolResults = append(toolResults, result)
			if input.OnToolCall != nil {
				if err := input.OnToolCall(record); err != nil {
					out.CompletedAt = time.Now().UTC()
					return out, err
				}
			}
		}

		if hadFreshExecution {
			noProgressIterations = 0
		} else {
			noProgressIterations++
		}

		if noProgressIterations >= repeatedNoProgressLoopCapTrigger && len(toolResults) > 0 {
			if finalized := finalizeFromToolResults(ctx, r.Model, out.Prompt, messages, input.Message, toolResults); finalized != "" {
				out.FinalText = finalized
			} else {
				out.FinalText = fallbackFromToolResults(toolResults, toolCap)
			}
			out.CompletedAt = time.Now().UTC()
			return out, nil
		}

		toolIterations++
	}
}

func finalizeFromToolResults(ctx context.Context, model Model, prompt string, messages []ChatMessage, message string, toolResults []ToolCallResult) string {
	if model == nil || len(toolResults) == 0 {
		return ""
	}

	finalPrompt := strings.TrimSpace(prompt)
	if finalPrompt != "" {
		finalPrompt += "\n\n"
	}
	finalPrompt += "# FINAL_RESPONSE_MODE\n- Do not call tools in this turn.\n- Use the latest tool results to answer the user directly.\n- If some commands failed, explain the failure and give the best next step."

	resp, err := model.Generate(ctx, ModelRequest{
		SystemPrompt: finalPrompt,
		Messages:     append([]ChatMessage(nil), messages...),
		AllowedTools: nil,
		Prompt:       finalPrompt,
		Message:      message,
		ToolResults:  append([]ToolCallResult(nil), toolResults...),
	})
	if err != nil {
		return ""
	}
	if len(resp.ToolCalls) > 0 {
		return ""
	}
	return strings.TrimSpace(resp.FinalText)
}

func fallbackFromToolResults(results []ToolCallResult, toolCap int) string {
	if len(results) == 0 {
		return "I hit the tool iteration limit before I could finish."
	}

	var b strings.Builder
	b.WriteString("I reached the tool-iteration limit before producing a final response. Here are the latest tool results:\n")

	start := len(results) - 5
	if start < 0 {
		start = 0
	}
	for i := start; i < len(results); i++ {
		item := results[i]
		idx := i + 1
		if strings.TrimSpace(item.Error) != "" {
			b.WriteString(fmt.Sprintf("- %d) error: %s\n", idx, strings.TrimSpace(item.Error)))
			continue
		}
		out := strings.TrimSpace(item.Output)
		if len(out) > 1200 {
			out = out[:1200] + "..."
		}
		if out == "" {
			out = "(empty output)"
		}
		b.WriteString(fmt.Sprintf("- %d) output: %s\n", idx, out))
	}
	b.WriteString(fmt.Sprintf("\n(Iteration cap: %d)", toolCap))
	return b.String()
}

func uniqueToolCallID(rawID string, ordinal int, used map[string]struct{}) string {
	base := strings.TrimSpace(rawID)
	if base == "" {
		base = fmt.Sprintf("tool-call-%d", ordinal)
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}
