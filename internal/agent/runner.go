package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrModelRequired            = errors.New("agent runner requires model")
	ErrToolExecutorRequired     = errors.New("agent runner requires tool executor for tool calls")
	ErrToolIterationCapExceeded = errors.New("agent runner tool iteration cap exceeded")

	explicitToolCallLimitRE = regexp.MustCompile(`(?i)\b(?:run|execute|perform)\s+(\d{1,3})\s+[^\n]*?\btool calls?\b`)
)

const (
	DefaultToolIterationCap             = 120
	DefaultToolTimeout                  = 900 * time.Second
	repeatedNoProgressLoopCapTrigger    = 6
	repeatedToolPatternLoopCapTrigger   = 2
	repeatedToolPatternMinResultWindow  = 10
	repeatedCallKeyCountThreshold       = 2
	repeatedCallKeyDistinctToolsTrigger = 3
	failureRecoveryTrigger              = 2
	failureGuidanceEscalation           = 3
	followThroughRepromptCap            = 5
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
	successfulCallKeyCounts := make(map[string]int)
	consecutiveToolFailures := 0
	failureRecoveryActive := false
	failuresSinceRecovery := 0
	successesSinceRecovery := 0
	toolTimeout := time.Duration(input.ToolTimeoutMS) * time.Millisecond
	if toolTimeout <= 0 {
		toolTimeout = DefaultToolTimeout
	}
	noProgressIterations := 0
	lastToolPattern := ""
	repeatedToolPatternCount := 0
	explicitToolCallLimit := inferExplicitToolCallLimit(input.Message)
	latestThinking := ""
	thinkingPresent := false
	toolParseFailure := false
	followThroughReprompts := 0

	registerToolOutcome := func(errText string) {
		if strings.TrimSpace(errText) == "" {
			consecutiveToolFailures = 0
			if failureRecoveryActive {
				successesSinceRecovery++
				if successesSinceRecovery >= 3 {
					failureRecoveryActive = false
					failuresSinceRecovery = 0
					successesSinceRecovery = 0
				}
			}
			return
		}
		successesSinceRecovery = 0
		consecutiveToolFailures++
		if !failureRecoveryActive && consecutiveToolFailures >= failureRecoveryTrigger {
			failureRecoveryActive = true
			failuresSinceRecovery = 0
			return
		}
		if failureRecoveryActive {
			failuresSinceRecovery++
		}
	}

	notifyToolCall := func(record *ToolCallRecord) {
		if input.OnToolCall == nil || record == nil {
			return
		}
		if err := input.OnToolCall(*record); err != nil {
			record.CallbackErr = strings.TrimSpace(err.Error())
		}
	}

	finalizeForExplicitToolLimit := func() bool {
		if explicitToolCallLimit <= 0 || len(toolResults) < explicitToolCallLimit {
			return false
		}
		if finalized := finalizeFromToolResults(
			ctx,
			r.Model,
			input.AgentID,
			input.RunID,
			out.Prompt,
			messages,
			input.Message,
			input.ToolTimeoutMS,
			toolResults,
			fmt.Sprintf("# REQUESTED_TOOL_COUNT_MODE\n- The user-requested tool-call count (%d) has been reached.\n- Do not call tools again. Provide the final answer from existing tool results.", explicitToolCallLimit),
			input.OnTextDelta,
		); finalized != "" {
			out.FinalText = finalized
		} else {
			out.FinalText = requestedToolCallLimitFallback(toolResults, explicitToolCallLimit)
		}
		out.Thinking = latestThinking
		out.ThinkingPresent = thinkingPresent
		out.ToolParseFailure = toolParseFailure
		out.CompletedAt = time.Now().UTC()
		return true
	}

	for {
		systemPrompt := out.Prompt
		if failureRecoveryActive {
			systemPrompt = appendPromptDirective(systemPrompt, "# ERROR_RECOVERY_MODE\n- Recent tool calls failed. Analyze the latest errors and outputs before choosing the next action.\n- Try a materially different approach to resolve the error.\n- Do not repeat the same failing command/arguments unless you explain why it should now work.")
		}
		if followThroughReprompts > 0 {
			systemPrompt = appendPromptDirective(systemPrompt, "# ACTION_EXECUTION_MODE\n- You previously replied with intent to act but did not execute.\n- In this turn, either call required tools now or provide a concrete final answer from existing evidence.\n- Do not defer with phrases like 'let me check' or promise future action without execution.")
		}

		resp, err := r.Model.Generate(ctx, ModelRequest{
			AgentID:       input.AgentID,
			RunID:         input.RunID,
			SystemPrompt:  systemPrompt,
			Messages:      append([]ChatMessage(nil), messages...),
			AllowedTools:  append([]string(nil), input.AllowedTools...),
			ToolTimeoutMS: input.ToolTimeoutMS,
			Prompt:        systemPrompt,
			Message:       input.Message,
			ToolResults:   append([]ToolCallResult(nil), toolResults...),
			OnTextDelta:   input.OnTextDelta,
		})
		if resp.ThinkingPresent {
			thinkingPresent = true
			if strings.TrimSpace(resp.Thinking) != "" {
				latestThinking = strings.TrimSpace(resp.Thinking)
			}
		}
		if resp.ToolParseFailure {
			toolParseFailure = true
		}
		if err != nil {
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			if len(toolResults) > 0 {
				out.FinalText = recoverFromModelError(err, toolResults, toolCap)
				out.CompletedAt = time.Now().UTC()
				return out, nil
			}
			out.CompletedAt = time.Now().UTC()
			return out, err
		}

		if len(resp.ToolCalls) == 0 {
			if shouldForceFollowThrough(resp.FinalText, input.AllowedTools, toolResults) {
				if followThroughReprompts < followThroughRepromptCap {
					followThroughReprompts++
					if text := strings.TrimSpace(resp.FinalText); text != "" {
						messages = append(messages, ChatMessage{Role: "assistant", Content: text})
					}
					continue
				}
				out.FinalText = nonActionableFinalText(resp.FinalText)
				out.Thinking = latestThinking
				out.ThinkingPresent = thinkingPresent
				out.ToolParseFailure = toolParseFailure
				out.CompletedAt = time.Now().UTC()
				return out, nil
			}
			out.FinalText = resp.FinalText
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			out.CompletedAt = time.Now().UTC()
			return out, nil
		}

		if r.ToolExecutor == nil {
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			out.CompletedAt = time.Now().UTC()
			return out, ErrToolExecutorRequired
		}

		if toolIterations >= toolCap {
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			if len(toolResults) > 0 {
				if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, out.Prompt, messages, input.Message, input.ToolTimeoutMS, toolResults, "", input.OnTextDelta); finalized != "" {
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

		currentToolPattern := toolCallPattern(resp.ToolCalls)
		if currentToolPattern != "" && currentToolPattern == lastToolPattern {
			repeatedToolPatternCount++
		} else {
			repeatedToolPatternCount = 0
			lastToolPattern = currentToolPattern
		}
		if repeatedToolPatternCount >= repeatedToolPatternLoopCapTrigger && len(toolResults) >= repeatedToolPatternMinResultWindow {
			if finalized := finalizeFromToolResults(
				ctx,
				r.Model,
				input.AgentID,
				input.RunID,
				out.Prompt,
				messages,
				input.Message,
				input.ToolTimeoutMS,
				toolResults,
				"# LOOP_GUARD_MODE\n- You have repeated materially identical tool-call batches across consecutive iterations.\n- Do not call tools again. Synthesize a final answer from existing tool results.",
				input.OnTextDelta,
			); finalized != "" {
				out.FinalText = finalized
			} else {
				out.FinalText = loopGuardFallbackFromToolResults(toolResults)
			}
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			out.CompletedAt = time.Now().UTC()
			return out, nil
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
					errText := toolResultErrorText(record.Result)
					if strings.TrimSpace(record.Result.Error) == "" && errText != "" {
						record.Result.Error = errText
					}
					notifyToolCall(&record)
					out.ToolCalls = append(out.ToolCalls, record)
					toolResults = append(toolResults, record.Result)
					registerToolOutcome(record.Result.Error)
					if strings.TrimSpace(record.Result.Error) == "" {
						successfulCallKeyCounts[callKey]++
					}
					if finalizeForExplicitToolLimit() {
						return out, nil
					}
					continue
				}
				if cached, ok := cachedFailedToolResults[callKey]; ok {
					now := time.Now().UTC()
					cached.ID = call.ID
					record := ToolCallRecord{Request: call, Result: cached, StartedAt: now, CompletedAt: now}
					errText := toolResultErrorText(record.Result)
					if strings.TrimSpace(record.Result.Error) == "" && errText != "" {
						record.Result.Error = errText
					}
					notifyToolCall(&record)
					out.ToolCalls = append(out.ToolCalls, record)
					toolResults = append(toolResults, record.Result)
					registerToolOutcome(record.Result.Error)
					if finalizeForExplicitToolLimit() {
						return out, nil
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
				if isToolTimeoutError(execErr) && !strings.Contains(strings.ToLower(execErr.Error()), "timeout") {
					result.Error = fmt.Sprintf("timeout: tool execution exceeded %dms", int(toolTimeout/time.Millisecond))
				} else {
					result.Error = execErr.Error()
				}
			}
			if strings.TrimSpace(result.Error) == "" {
				if inferred := toolResultErrorText(result); inferred != "" {
					result.Error = inferred
				}
			}

			record.Result = result
			record.CompletedAt = time.Now().UTC()
			hadFreshExecution = true
			registerToolOutcome(result.Error)

			if callKey != "|" {
				if strings.TrimSpace(result.Error) == "" {
					successfulCallKeyCounts[callKey]++
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

			notifyToolCall(&record)
			out.ToolCalls = append(out.ToolCalls, record)
			toolResults = append(toolResults, result)
			if finalizeForExplicitToolLimit() {
				return out, nil
			}
		}

		if hadFreshExecution {
			noProgressIterations = 0
		} else {
			noProgressIterations++
		}

		repeatedCallKeys := countCallKeysAtOrAbove(successfulCallKeyCounts, repeatedCallKeyCountThreshold)
		if repeatedCallKeys >= repeatedCallKeyDistinctToolsTrigger && len(toolResults) >= repeatedToolPatternMinResultWindow {
			if finalized := finalizeFromToolResults(
				ctx,
				r.Model,
				input.AgentID,
				input.RunID,
				out.Prompt,
				messages,
				input.Message,
				input.ToolTimeoutMS,
				toolResults,
				"# LOOP_GUARD_MODE\n- Multiple tool signatures have repeated without converging.\n- Do not call tools again. Synthesize a final answer from existing tool results.",
				input.OnTextDelta,
			); finalized != "" {
				out.FinalText = finalized
			} else {
				out.FinalText = loopGuardFallbackFromToolResults(toolResults)
			}
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			out.CompletedAt = time.Now().UTC()
			return out, nil
		}

		if failureRecoveryActive && failuresSinceRecovery >= failureGuidanceEscalation && len(out.ToolCalls) > 0 {
			out.FinalText = requestUserGuidanceFromFailures(input.Message, out.ToolCalls)
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			out.CompletedAt = time.Now().UTC()
			return out, nil
		}

		if noProgressIterations >= repeatedNoProgressLoopCapTrigger && len(toolResults) > 0 {
			if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, out.Prompt, messages, input.Message, input.ToolTimeoutMS, toolResults, "", input.OnTextDelta); finalized != "" {
				out.FinalText = finalized
			} else {
				out.FinalText = fallbackFromToolResults(toolResults, toolCap)
			}
			out.Thinking = latestThinking
			out.ThinkingPresent = thinkingPresent
			out.ToolParseFailure = toolParseFailure
			out.CompletedAt = time.Now().UTC()
			return out, nil
		}

		toolIterations++
	}
}

func shouldForceFollowThrough(finalText string, allowedTools []string, toolResults []ToolCallResult) bool {
	if len(allowedTools) == 0 || len(toolResults) > 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(finalText))
	if text == "" || len(text) > 480 {
		return false
	}

	if strings.Contains(text, "can't") ||
		strings.Contains(text, "cannot") ||
		strings.Contains(text, "unable") ||
		strings.Contains(text, "permission") ||
		strings.Contains(text, "missing") ||
		strings.Contains(text, "blocked") {
		return false
	}

	deferralPhrases := []string{
		"let me",
		"let me try",
		"let me check",
		"let me verify",
		"let me look",
		"i will",
		"i'll",
		"i am going to",
		"i'm going to",
		"give me a moment",
		"hold on",
	}
	for _, phrase := range deferralPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}

	return false
}

func nonActionableFinalText(lastText string) string {
	_ = lastText
	return "I could not complete an actionable execution step in time. Please retry and I will run it directly and report concrete results."
}

func finalizeFromToolResults(ctx context.Context, model Model, agentID, runID, prompt string, messages []ChatMessage, message string, toolTimeoutMS int, toolResults []ToolCallResult, extraDirective string, onTextDelta func(delta string) error) string {
	if model == nil || len(toolResults) == 0 {
		return ""
	}

	finalPrompt := strings.TrimSpace(prompt)
	if finalPrompt != "" {
		finalPrompt += "\n\n"
	}
	finalPrompt += "# FINAL_RESPONSE_MODE\n- Do not call tools in this turn.\n- Use the latest tool results to answer the user directly.\n- If some commands failed, explain the failure and give the best next step."
	if strings.TrimSpace(extraDirective) != "" {
		finalPrompt += "\n\n" + strings.TrimSpace(extraDirective)
	}

	resp, err := model.Generate(ctx, ModelRequest{
		AgentID:       agentID,
		RunID:         runID,
		SystemPrompt:  finalPrompt,
		Messages:      append([]ChatMessage(nil), messages...),
		AllowedTools:  nil,
		ToolTimeoutMS: toolTimeoutMS,
		Prompt:        finalPrompt,
		Message:       message,
		ToolResults:   append([]ToolCallResult(nil), toolResults...),
		OnTextDelta:   onTextDelta,
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
	b.WriteString(formatLatestToolResults(results))
	b.WriteString(fmt.Sprintf("\n(Iteration cap: %d)", toolCap))
	return b.String()
}

func loopGuardFallbackFromToolResults(results []ToolCallResult) string {
	if len(results) == 0 {
		return "I stopped because the tool-call plan was looping without converging."
	}

	var b strings.Builder
	b.WriteString("I stopped to avoid a repeating tool-call loop. Here are the latest tool results:\n")
	b.WriteString(formatLatestToolResults(results))
	return b.String()
}

func requestedToolCallLimitFallback(results []ToolCallResult, requested int) string {
	if requested <= 0 {
		requested = len(results)
	}
	if len(results) == 0 {
		return fmt.Sprintf("I stopped after reaching the requested tool-call limit (%d).", requested)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("I reached the requested tool-call limit (%d). Here are the latest tool results:\n", requested))
	b.WriteString(formatLatestToolResults(results))
	return b.String()
}

func recoverFromModelError(err error, toolResults []ToolCallResult, toolCap int) string {
	msg := strings.TrimSpace("I hit a model/API error while processing the next step: " + strings.TrimSpace(err.Error()))
	if len(toolResults) == 0 {
		return msg
	}
	return msg + "\n\nLatest tool results before the model/API error:\n" + formatLatestToolResults(toolResults) + fmt.Sprintf("\n(Iteration cap: %d)", toolCap)
}

func formatLatestToolResults(results []ToolCallResult) string {
	if len(results) == 0 {
		return ""
	}

	start := len(results) - 5
	if start < 0 {
		start = 0
	}

	var b strings.Builder
	for i := start; i < len(results); i++ {
		item := results[i]
		idx := i + 1
		if strings.TrimSpace(item.Error) != "" {
			b.WriteString(fmt.Sprintf("- %d) error: %s\n", idx, strings.TrimSpace(item.Error)))
			out := strings.TrimSpace(item.Output)
			if len(out) > 1200 {
				out = out[:1200] + "..."
			}
			if out != "" {
				b.WriteString(fmt.Sprintf("  output: %s\n", out))
			}
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
	return b.String()
}

func isToolTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "deadline exceeded")
}

func appendPromptDirective(prompt, directive string) string {
	base := strings.TrimSpace(prompt)
	extra := strings.TrimSpace(directive)
	if extra == "" {
		return base
	}
	if base == "" {
		return extra
	}
	return base + "\n\n" + extra
}

func requestUserGuidanceFromFailures(userMessage string, records []ToolCallRecord) string {
	if prompt, ok := networkAllowlistPermissionPrompt(userMessage, records); ok {
		return prompt
	}

	var b strings.Builder
	b.WriteString("I hit repeated tool failures and need your guidance before I continue.\n")
	goal := strings.TrimSpace(userMessage)
	if goal != "" {
		b.WriteString("Goal: ")
		b.WriteString(goal)
		b.WriteString("\n")
	}

	failing := make([]ToolCallRecord, 0, 6)
	for i := len(records) - 1; i >= 0 && len(failing) < 6; i-- {
		if strings.TrimSpace(records[i].Result.Error) == "" {
			continue
		}
		failing = append(failing, records[i])
	}
	if len(failing) == 0 {
		b.WriteString("I do not have detailed failing tool outputs to share yet.\n")
		b.WriteString("Please tell me how you want to proceed.")
		return b.String()
	}

	b.WriteString("What I tried and what failed:\n")
	for i := len(failing) - 1; i >= 0; i-- {
		rec := failing[i]
		attempt := rec.Request.Name
		args := truncateGuidanceText(strings.TrimSpace(string(rec.Request.Arguments)), 220)
		if args != "" {
			attempt += " " + args
		}
		errorText := truncateGuidanceText(strings.TrimSpace(rec.Result.Error), 420)
		b.WriteString(fmt.Sprintf("- %d) %s\n", len(failing)-i, attempt))
		b.WriteString("  error: ")
		b.WriteString(errorText)
		b.WriteString("\n")
		output := truncateGuidanceText(strings.TrimSpace(rec.Result.Output), 700)
		if output != "" {
			b.WriteString("  output: ")
			b.WriteString(output)
			b.WriteString("\n")
		}
	}
	b.WriteString("Please guide me on the next step (for example: grant capability/permission, provide auth details, or pick a different approach).")
	return b.String()
}

func networkAllowlistPermissionPrompt(userMessage string, records []ToolCallRecord) (string, bool) {
	hostSet := map[string]struct{}{}
	for _, rec := range records {
		toolName := strings.TrimSpace(strings.ToLower(rec.Request.Name))
		if toolName != "http.request" && toolName != "net.fetch" {
			continue
		}
		errText := strings.TrimSpace(rec.Result.Error)
		if errText == "" {
			continue
		}
		host := extractNetworkAllowlistDeniedHost(errText)
		if host == "" {
			continue
		}
		hostSet[host] = struct{}{}
	}
	if len(hostSet) == 0 {
		return "", false
	}

	hosts := make([]string, 0, len(hostSet))
	for host := range hostSet {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	goal := strings.TrimSpace(userMessage)
	quoted := make([]string, 0, len(hosts))
	for _, host := range hosts {
		quoted = append(quoted, "`"+host+"`")
	}

	var b strings.Builder
	b.WriteString("I can continue, but I need your permission first.\n")
	if goal != "" {
		b.WriteString("Goal: ")
		b.WriteString(goal)
		b.WriteString("\n")
	}
	b.WriteString("The network tool is blocked because these hosts are not in `network.allowed_domains`: ")
	b.WriteString(strings.Join(quoted, ", "))
	b.WriteString(".\n")
	b.WriteString("If you approve, reply exactly: `yes, add allowed domains` and I will add them via `config.set` and retry immediately.")
	return b.String(), true
}

func extractNetworkAllowlistDeniedHost(errText string) string {
	lower := strings.ToLower(strings.TrimSpace(errText))
	if !strings.Contains(lower, "is not in network.allowed_domains") {
		return ""
	}
	const prefix = "host \""
	start := strings.Index(lower, prefix)
	if start == -1 {
		return ""
	}
	start += len(prefix)
	endRel := strings.Index(lower[start:], "\"")
	if endRel <= 0 {
		return ""
	}
	host := strings.TrimSpace(lower[start : start+endRel])
	if host == "" {
		return ""
	}
	return host
}

func truncateGuidanceText(value string, maxChars int) string {
	text := strings.TrimSpace(value)
	if text == "" || maxChars <= 0 {
		return ""
	}
	if len(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return text[:maxChars]
	}
	return strings.TrimSpace(text[:maxChars-3]) + "..."
}

func toolResultErrorText(result ToolCallResult) string {
	if text := strings.TrimSpace(result.Error); text != "" {
		return text
	}
	raw := strings.TrimSpace(result.Output)
	if raw == "" || (!strings.HasPrefix(raw, "{") && !strings.HasPrefix(raw, "[")) {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	if v, ok := payload["error"]; ok {
		errText := strings.TrimSpace(fmt.Sprintf("%v", v))
		if errText != "" && errText != "<nil>" {
			return errText
		}
	}
	if v, ok := payload["exit_code"]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) != 0 {
				return fmt.Sprintf("exit status %d", int(n))
			}
		case int:
			if n != 0 {
				return fmt.Sprintf("exit status %d", n)
			}
		}
	}
	return ""
}

func toolCallPattern(calls []ToolCallRequest) string {
	if len(calls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		args := strings.TrimSpace(string(call.Arguments))
		parts = append(parts, name+"|"+args)
	}
	return strings.Join(parts, "||")
}

func inferExplicitToolCallLimit(message string) int {
	text := strings.TrimSpace(message)
	if text == "" {
		return 0
	}
	matches := explicitToolCallLimitRE.FindStringSubmatch(text)
	if len(matches) != 2 {
		return 0
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil || value <= 0 {
		return 0
	}
	if value > 200 {
		value = 200
	}
	return value
}

func countCallKeysAtOrAbove(counts map[string]int, minCount int) int {
	if len(counts) == 0 || minCount <= 1 {
		return len(counts)
	}
	total := 0
	for _, count := range counts {
		if count >= minCount {
			total++
		}
	}
	return total
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
