package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/config"
)

type requestCapture struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type staticToolExecutor struct {
	result agent.ToolCallResult
}

func (s *staticToolExecutor) Execute(_ context.Context, call agent.ToolCallRequest) (agent.ToolCallResult, error) {
	res := s.result
	if res.ID == "" {
		res.ID = call.ID
	}
	return res, nil
}

func TestProviderModelRoutesToolResultsBackThroughModel(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []requestCapture
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var payload requestCapture
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		mu.Lock()
		requests = append(requests, payload)
		callNum := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch callNum {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]string{
						"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
					}},
				},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]string{"content": "There is one file in the folder."}},
				},
			})
		default:
			t.Fatalf("unexpected extra provider call: %d", callNum)
		}
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	runner := agent.Runner{
		Model:             model,
		ToolExecutor:      &staticToolExecutor{result: agent.ToolCallResult{Output: `{"entries":["README.md"]}`}},
		MaxToolIterations: 4,
	}

	out, err := runner.Run(context.Background(), agent.RunInput{
		Message:      "what is in this folder?",
		ArtifactDocs: []agent.ArtifactDoc{{Name: "SOUL.md", Content: "help the user"}},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if out.FinalText != "There is one file in the folder." {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(out.ToolCalls))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected 2 provider requests, got %d", len(requests))
	}
	if len(requests[1].Messages) < 1 {
		t.Fatalf("expected second request messages")
	}
	systemPrompt := requests[1].Messages[0].Content
	if !strings.Contains(systemPrompt, "## Tool Results") {
		t.Fatalf("expected tool results section in follow-up prompt")
	}
	if !strings.Contains(systemPrompt, `{"entries":["README.md"]}`) {
		t.Fatalf("expected tool output included in follow-up prompt")
	}
}

func TestAppendToolResultsPromptLimitsToolResultCountAndSize(t *testing.T) {
	results := make([]agent.ToolCallResult, 0, maxPromptToolResults+3)
	for i := 1; i <= maxPromptToolResults+3; i++ {
		results = append(results, agent.ToolCallResult{
			ID:     "tool-" + strconv.Itoa(i),
			Output: strings.Repeat("x", maxPromptToolOutput+200),
		})
	}

	prompt := appendToolResultsPrompt("system", results)
	if !strings.Contains(prompt, "older_results_omitted: 3") {
		t.Fatalf("expected omitted-result marker, got %q", prompt)
	}
	if strings.Contains(prompt, "- id: tool-1\n") || strings.Contains(prompt, "- id: tool-2\n") || strings.Contains(prompt, "- id: tool-3\n") {
		t.Fatalf("expected oldest tool IDs to be omitted from prompt")
	}
	if !strings.Contains(prompt, "- id: tool-4\n") || !strings.Contains(prompt, "- id: tool-15\n") {
		t.Fatalf("expected newest tool IDs to remain in prompt")
	}
	if strings.Count(prompt, strings.Repeat("x", maxPromptToolOutput+50)) > 0 {
		t.Fatalf("expected oversized tool outputs to be truncated")
	}
}

func TestAppendToolResultsPromptIncludesErrorOutputAndRecoveryGuidance(t *testing.T) {
	prompt := appendToolResultsPrompt("system", []agent.ToolCallResult{
		{
			ID:     "tool-1",
			Output: "partial stdout from failed command",
			Error:  "tool_execution_failed (shell.exec): permission denied",
		},
	})

	if !strings.Contains(prompt, "## Tool Failure Recovery") {
		t.Fatalf("expected failure recovery guidance in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "error: tool_execution_failed") {
		t.Fatalf("expected tool error in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "partial stdout from failed command") {
		t.Fatalf("expected failed call output in prompt for diagnosis, got %q", prompt)
	}
}

func TestProviderModelSendsStructuredHistoryMessages(t *testing.T) {
	var captured requestCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{"content": "ok"}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	_, err := model.Generate(context.Background(), agent.ModelRequest{
		SystemPrompt: "system",
		Messages: []agent.ChatMessage{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "done"},
			{Role: "user", Content: "second"},
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(captured.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" {
		t.Fatalf("expected system role first, got %q", captured.Messages[0].Role)
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "first" {
		t.Fatalf("unexpected first history message: %+v", captured.Messages[1])
	}
	if captured.Messages[2].Role != "assistant" || captured.Messages[2].Content != "done" {
		t.Fatalf("unexpected second history message: %+v", captured.Messages[2])
	}
	if captured.Messages[3].Role != "user" || captured.Messages[3].Content != "second" {
		t.Fatalf("unexpected final user message: %+v", captured.Messages[3])
	}
}

func TestProviderModelTraceCapturesModelInputAndToolExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	collector := newRunTraceCollector("run_1", "chat_1", "dashboard", "list files")
	ctx := withRunTraceCollector(context.Background(), collector)

	resp, err := model.Generate(ctx, agent.ModelRequest{
		SystemPrompt: "system",
		Messages:     []agent.ChatMessage{{Role: "user", Content: "list files"}},
		AllowedTools: []string{"fs.list"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}

	trace := collector.Snapshot()
	if trace == nil {
		t.Fatal("expected trace snapshot")
	}
	inputs, ok := trace["model_inputs"].([]any)
	if !ok || len(inputs) != 1 {
		t.Fatalf("expected one model input trace entry, got %#v", trace["model_inputs"])
	}
	entry, ok := inputs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected model input trace shape: %#v", inputs[0])
	}
	if entry["message"] != "list files" {
		t.Fatalf("unexpected traced message: %#v", entry["message"])
	}
	if entry["history_injected"] != false {
		t.Fatalf("expected history_injected=false, got %#v", entry["history_injected"])
	}

	extractions, ok := trace["extracted_tool_calls"].([]any)
	if !ok || len(extractions) == 0 {
		t.Fatalf("expected tool extraction trace entries, got %#v", trace["extracted_tool_calls"])
	}
	extract, ok := extractions[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected extraction trace shape: %#v", extractions[0])
	}
	if extract["accepted"] != true {
		t.Fatalf("expected accepted extraction, got %#v", extract["accepted"])
	}
	if extract["parsed_tool_name"] != "fs.list" {
		t.Fatalf("unexpected parsed tool name: %#v", extract["parsed_tool_name"])
	}
}

func TestProviderModelStripsThinkTagsFromFinalText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<think>internal</think>Hello there</think><think>",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if strings.Contains(resp.FinalText, "<think>") || strings.Contains(resp.FinalText, "</think>") {
		t.Fatalf("think tags leaked in final text: %q", resp.FinalText)
	}
	if resp.FinalText != "Hello there" {
		t.Fatalf("unexpected cleaned final text: %q", resp.FinalText)
	}
}

func TestProviderModelCapturesThinkingInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<think>secret plan</think>Hello there",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if !resp.ThinkingPresent {
		t.Fatal("expected thinking_present=true")
	}
	if resp.Thinking != "secret plan" {
		t.Fatalf("unexpected thinking text: %q", resp.Thinking)
	}
	if resp.FinalText != "Hello there" {
		t.Fatalf("unexpected visible text: %q", resp.FinalText)
	}
}

func TestExtractThinkingNestedTagsDoesNotCrash(t *testing.T) {
	visible, thinking, present := ExtractThinking("before <think>outer <think>inner</think> tail</think> after")
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "before  after" {
		t.Fatalf("unexpected visible text: %q", visible)
	}
	if thinking != "outer <think>inner</think> tail" {
		t.Fatalf("unexpected thinking text: %q", thinking)
	}
}

func TestExtractThinkingMissingClosingTagGraceful(t *testing.T) {
	input := "Hello <analysis>internal plan"
	visible, thinking, present := ExtractThinking(input)
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "Hello internal plan" {
		t.Fatalf("expected content to remain intact, got %q", visible)
	}
	if thinking != "" {
		t.Fatalf("expected no extracted thinking for ambiguous block, got %q", thinking)
	}
}

func TestExtractThinkingMixedVisibleAndThinkingContent(t *testing.T) {
	input := "start <analysis>plan A</analysis> mid <!-- THINK -->plan B<!-- /THINK --> end"
	visible, thinking, present := ExtractThinking(input)
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "start  mid  end" {
		t.Fatalf("unexpected visible text: %q", visible)
	}
	if thinking != "plan A\n\nplan B" {
		t.Fatalf("unexpected thinking text: %q", thinking)
	}
}

func TestExtractThinkingPreservesExistingThinkTagStrippingSemantics(t *testing.T) {
	visible, thinking, present := ExtractThinking("<think>internal</think>Hello there</think><think>")
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "Hello there" {
		t.Fatalf("unexpected visible text: %q", visible)
	}
	if thinking != "internal" {
		t.Fatalf("unexpected thinking text: %q", thinking)
	}
}

func TestProviderModelParsesToolCallsAfterThinkTagStripping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n</think>{\"tool_name\":\"time.now\",\"arguments\":{}}<think>\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "time?"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "time.now" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
}

func TestProviderModelIgnoresBracketStyleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I'll check now.\n[fs.list] path: .",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresUnfencedJSONToolObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I will create it now.\n{\"tool\":\"fs.write\",\"path\":\"test.md\",\"content\":\"test\"}",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "create test.md"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresXMLStyleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<tool_call>fs.read\npath=\"test.md\"</arg_value>",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "read file"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresXMLFunctionBlockToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<tool_call>\n<function=fs.list>\n<path>.</path>\n</function>",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRequiresCanonicalToolJSONFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "{\"tool_code\":\"fs.list\",\"parameters\":{\"path\":\".\"}}",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresShellSnippets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I'll check now.\nls\ncat test.md",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "check files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRetriesOnTimeout(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			time.Sleep(80 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "retry succeeded",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	model.httpClient = &http.Client{Timeout: 30 * time.Millisecond}

	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.FinalText != "retry succeeded" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
	if calls < 2 {
		t.Fatalf("expected retry attempt, got %d call(s)", calls)
	}
}

func TestProviderModelRetriesOnUnexpectedEOF(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"partial`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "retry after eof",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.FinalText != "retry after eof" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
	if calls < 2 {
		t.Fatalf("expected retry attempt, got %d call(s)", calls)
	}
}

func TestProviderModelRetriesUseFreshAttemptTimeouts(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			time.Sleep(80 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "recovered after multiple retry attempts",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	model.httpClient = &http.Client{Timeout: 30 * time.Millisecond}

	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.FinalText != "recovered after multiple retry attempts" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
	if calls < 3 {
		t.Fatalf("expected third attempt to succeed, got %d calls", calls)
	}
}

func TestProviderModelIgnoresUnknownToolNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I'll wait a second. time.sleep(1)",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "wait"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no parsed tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRejectsToolCallsNotInAllowlist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files", AllowedTools: []string{"fs.read"}})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRejectsShellExecWhenNotAllowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "run pwd", AllowedTools: []string{"fs.list"}})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRejectsToolResultAsCallableTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"tool.result\",\"arguments\":{}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "test"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelDoesNotSynthesizeWriteCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "Here you go:\n```python\nprint(\"hello\")\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "create hello.py with a simple print"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresNonJSONFencedBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```bash\nls -la\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "run ls"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelCanonicalizesBashExecAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"bash.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "run bash"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell.exec" {
		t.Fatalf("expected canonical shell.exec, got %q", resp.ToolCalls[0].Name)
	}
}

func TestProviderModelRequestsMaxTokensCap(t *testing.T) {
	var captured requestCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Model.MaxTokens = 50000
	cfg.Providers.Generic.BaseURL = server.URL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""

	model, err := NewProviderModel(cfg, nil)
	if err != nil {
		t.Fatalf("new provider model: %v", err)
	}

	_, err = model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hello"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if captured.MaxTokens != 20000 {
		t.Fatalf("expected max_tokens=20000, got %d", captured.MaxTokens)
	}
}

func TestProviderModelCompactsAtEightyPercentContext(t *testing.T) {
	var captured requestCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)

	messages := make([]agent.ChatMessage, 0, 260)
	for i := 0; i < 260; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		messages = append(messages, agent.ChatMessage{
			Role:    role,
			Content: strings.Repeat("context-window-line-", 180) + " marker-" + strconv.Itoa(i),
		})
	}
	messages = append(messages, agent.ChatMessage{Role: "user", Content: "latest-question-marker"})

	_, err := model.Generate(context.Background(), agent.ModelRequest{
		SystemPrompt: "system",
		Messages:     messages,
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(captured.Messages) < 3 {
		t.Fatalf("expected compacted request with system + history, got %d message(s)", len(captured.Messages))
	}
	if captured.Messages[1].Role != "system" || !strings.Contains(captured.Messages[1].Content, "Conversation compaction summary") {
		t.Fatalf("expected compaction summary system message, got %+v", captured.Messages[1])
	}
	if captured.Messages[len(captured.Messages)-1].Role != "user" || captured.Messages[len(captured.Messages)-1].Content != "latest-question-marker" {
		t.Fatalf("expected latest user turn preserved, got %+v", captured.Messages[len(captured.Messages)-1])
	}

	reqSystem := captured.Messages[0].Content
	reqHistory := make([]agent.ChatMessage, 0, len(captured.Messages)-1)
	for _, item := range captured.Messages[1:] {
		reqHistory = append(reqHistory, agent.ChatMessage{Role: item.Role, Content: item.Content})
	}
	used := estimateConversationTokens(reqSystem, reqHistory)
	budget := int(float64(model.contextWindow) * contextCompactionRatio)
	if used > budget {
		t.Fatalf("expected compacted context <= %d tokens, got %d", budget, used)
	}
}

func TestProviderModelParsesMultipleToolCallsFromSingleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```\n```json\n{\"tool_name\":\"fs.read\",\"arguments\":{\"path\":\"README.md\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:       "system",
		Message:      "inspect files",
		AllowedTools: []string{"fs.list", "fs.read"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" || resp.ToolCalls[1].Name != "fs.read" {
		t.Fatalf("unexpected parsed tools: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].ID == resp.ToolCalls[1].ID {
		t.Fatalf("expected unique tool IDs, got duplicate %q", resp.ToolCalls[0].ID)
	}
}

func TestProviderModelParsesLooseJSONObjectToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "Let me check that now.\n" +
						`{"tool_name":"shell.exec","arguments":{"command":"bash","args":["-lc","python3 --version"]}}`,
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:       "system",
		Message:      "what version of python is installed?",
		AllowedTools: []string{"shell.exec"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one parsed tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell.exec" {
		t.Fatalf("expected shell.exec tool, got %q", resp.ToolCalls[0].Name)
	}
	if !strings.HasPrefix(resp.ToolCalls[0].ID, "tool-json-") {
		t.Fatalf("expected tool-json id, got %q", resp.ToolCalls[0].ID)
	}
}

func TestProviderModelParsesToolCallArrayFromResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": `[{"tool_name":"fs.list","arguments":{"path":"."}},{"tool_name":"fs.read","arguments":{"path":"README.md"}}]`,
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:       "system",
		Message:      "inspect files",
		AllowedTools: []string{"fs.list", "fs.read"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" || resp.ToolCalls[1].Name != "fs.read" {
		t.Fatalf("unexpected parsed tools: %+v", resp.ToolCalls)
	}
}

func TestProviderModelTraceIncludesRejectedParseDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{invalid}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	collector := newRunTraceCollector("run_2", "chat_2", "dashboard", "list files")
	ctx := withRunTraceCollector(context.Background(), collector)

	_, err := model.Generate(ctx, agent.ModelRequest{
		SystemPrompt: "system",
		Messages:     []agent.ChatMessage{{Role: "user", Content: "list files"}},
		AllowedTools: []string{"fs.list"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	trace := collector.Snapshot()
	extractions, ok := trace["extracted_tool_calls"].([]any)
	if !ok || len(extractions) == 0 {
		t.Fatalf("expected extraction trace entries, got %#v", trace["extracted_tool_calls"])
	}
	entry, ok := extractions[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected extraction entry shape: %#v", extractions[0])
	}
	if entry["accepted"] != false {
		t.Fatalf("expected rejected extraction, got %#v", entry["accepted"])
	}
	reason := strings.ToLower(strings.TrimSpace(entry["reason"].(string)))
	if !strings.Contains(reason, "invalid json") {
		t.Fatalf("expected invalid json reason, got %q", reason)
	}
}

func TestProviderModelUsesCurrentMessageForToolDirectiveDetection(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Message: "what should we do now?",
		Messages: []agent.ChatMessage{
			{Role: "user", Content: `/tool fs.list {"path":"."}`},
			{Role: "assistant", Content: "Done."},
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected provider to be called once, got %d", requestCount)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no direct tool calls from historical /tool turn, got %+v", resp.ToolCalls)
	}
	if resp.FinalText != "ok" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
}

func TestProviderModelToolDirectiveSupportsBashAlias(t *testing.T) {
	model := testProviderModel(t, "http://unused.local")
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Message: `/tool bash.exec {"command":"pwd"}`})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell.exec" {
		t.Fatalf("expected canonical shell.exec, got %q", resp.ToolCalls[0].Name)
	}
}

func testProviderModel(t *testing.T, baseURL string) *ProviderModel {
	t.Helper()

	cfg := config.Default()
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Providers.Generic.BaseURL = baseURL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""

	model, err := NewProviderModel(cfg, nil)
	if err != nil {
		t.Fatalf("new provider model: %v", err)
	}
	return model
}
