package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/config"
)

type requestCapture struct {
	Messages []struct {
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

func TestProviderModelParsesBracketStyleToolCalls(t *testing.T) {
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("decode tool args: %v", err)
	}
	if args["path"] != "." {
		t.Fatalf("unexpected path arg: %#v", args["path"])
	}
}

func TestProviderModelParsesPlainJSONToolObject(t *testing.T) {
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.write" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("decode tool args: %v", err)
	}
	if args["path"] != "test.md" || args["content"] != "test" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestProviderModelParsesXMLStyleToolCalls(t *testing.T) {
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("decode tool args: %v", err)
	}
	if args["path"] != "test.md" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestProviderModelParsesXMLFunctionBlockToolCalls(t *testing.T) {
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("decode tool args: %v", err)
	}
	if args["path"] != "." {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestProviderModelParsesToolCodeAndParametersJSON(t *testing.T) {
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
}

func TestProviderModelMapsShellSnippetsToCoreTools(t *testing.T) {
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
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" || resp.ToolCalls[1].Name != "fs.read" {
		t.Fatalf("unexpected tool names: %q, %q", resp.ToolCalls[0].Name, resp.ToolCalls[1].Name)
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

func TestProviderModelSynthesizesWriteFromCodeBlock(t *testing.T) {
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.write" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("decode tool args: %v", err)
	}
	if args["path"] != "hello.py" {
		t.Fatalf("unexpected write path: %#v", args["path"])
	}
	if !strings.Contains(args["content"].(string), "print(\"hello\")") {
		t.Fatalf("unexpected synthesized content: %#v", args["content"])
	}
}

func TestProviderModelParsesBashFencedBlockToShellExec(t *testing.T) {
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell.exec" {
		t.Fatalf("unexpected tool name: %q", resp.ToolCalls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if args["command"] != "bash" {
		t.Fatalf("expected bash command, got %#v", args["command"])
	}
}

func TestProviderModelCanonicalizesBashExecAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": `{"tool_name":"bash.exec","arguments":{"command":"pwd"}}`,
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
