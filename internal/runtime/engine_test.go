package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
)

type capturedChatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func TestEngineInitCreatesAgentArtifacts(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	paths := []string{
		filepath.Join(root, "workspace"),
		filepath.Join(root, ".openclawssy", "agents", "default", "SOUL.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "RULES.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "TOOLS.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "SPECPLAN.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "DEVPLAN.md"),
		filepath.Join(root, ".openclawssy", "agents", "default", "HANDOFF.md"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}
}

func TestEngineExecuteWritesRunBundle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZAI_API_KEY", "test-key")
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	res, err := e.Execute(context.Background(), "default", `/tool fs.list {"path":"."}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.RunID == "" {
		t.Fatal("expected run id")
	}
	if res.ArtifactPath == "" {
		t.Fatal("expected artifact path")
	}
	if _, err := os.Stat(filepath.Join(res.ArtifactPath, "output.json")); err != nil {
		t.Fatalf("expected output bundle file: %v", err)
	}
	if res.Trace == nil {
		t.Fatal("expected trace envelope in run result")
	}
	if _, ok := res.Trace["input_message_hash"]; !ok {
		t.Fatalf("expected input_message_hash in trace, got %#v", res.Trace)
	}
}

func TestLoadPromptDocsIncludesRuntimeContext(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	docs, err := e.loadPromptDocs("default")
	if err != nil {
		t.Fatalf("load prompt docs: %v", err)
	}

	found := false
	for _, doc := range docs {
		if doc.Name != "RUNTIME_CONTEXT.md" {
			continue
		}
		found = true
		if !strings.Contains(doc.Content, "Workspace root:") {
			t.Fatalf("runtime context missing workspace root: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "home directory") {
			t.Fatalf("runtime context missing home directory guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "do not mention HANDOFF") {
			t.Fatalf("runtime context missing prompt hygiene guidance: %q", doc.Content)
		}
	}
	if !found {
		t.Fatal("expected RUNTIME_CONTEXT.md prompt doc")
	}

	bestFound := false
	for _, doc := range docs {
		if doc.Name != "TOOL_CALLING_BEST_PRACTICES.md" {
			continue
		}
		bestFound = true
		if !strings.Contains(doc.Content, "shell.exec") {
			t.Fatalf("tool best practices missing shell.exec guidance: %q", doc.Content)
		}
		if !strings.Contains(doc.Content, "Do not invent tool names") {
			t.Fatalf("tool best practices missing invalid tool warning: %q", doc.Content)
		}
	}
	if !bestFound {
		t.Fatal("expected TOOL_CALLING_BEST_PRACTICES.md prompt doc")
	}
}

func TestNormalizeToolArgsDefaultsListPath(t *testing.T) {
	args := normalizeToolArgs("fs.list", map[string]any{})
	if args["path"] != "." {
		t.Fatalf("expected default path '.', got %#v", args["path"])
	}
}

func TestNormalizeToolArgsFixesMalformedWritePathBlob(t *testing.T) {
	args := normalizeToolArgs("fs.write", map[string]any{
		"path": `list_directory.py", """#!/usr/bin/env python3
print("hello")
"""`,
	})
	if args["path"] != "list_directory.py" {
		t.Fatalf("unexpected normalized path: %#v", args["path"])
	}
	content, _ := args["content"].(string)
	if !strings.Contains(content, "#!/usr/bin/env python3") {
		t.Fatalf("expected normalized content to include script, got %#v", args["content"])
	}
}

func TestNormalizeToolArgsSanitizesMarkdownFencePath(t *testing.T) {
	args := normalizeToolArgs("fs.list", map[string]any{"path": "```"})
	if args["path"] != "." {
		t.Fatalf("expected sanitized default path '.', got %#v", args["path"])
	}
}

func TestNormalizeToolArgsShellCommandFallbackToBashLC(t *testing.T) {
	args := normalizeToolArgs("shell.exec", map[string]any{"command": "ls -la"})
	if args["command"] != "bash" {
		t.Fatalf("expected command to normalize to bash, got %#v", args["command"])
	}
	list, ok := args["args"].([]string)
	if !ok || len(list) != 2 || list[0] != "-lc" || list[1] != "ls -la" {
		t.Fatalf("unexpected shell args normalization: %#v", args["args"])
	}
}

func TestExecuteWithInputUsesStructuredHistoryForSession(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "list files in ."}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: "There are two files."}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	var captured struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Providers.Generic.BaseURL = server.URL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "create file foo.txt", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}

	if len(captured.Messages) < 4 {
		t.Fatalf("expected system + 3 chat messages, got %d", len(captured.Messages))
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "list files in ." {
		t.Fatalf("unexpected first history message: %+v", captured.Messages[1])
	}
	if captured.Messages[2].Role != "assistant" || captured.Messages[2].Content != "There are two files." {
		t.Fatalf("unexpected assistant history message: %+v", captured.Messages[2])
	}
	if captured.Messages[len(captured.Messages)-1].Role != "user" || captured.Messages[len(captured.Messages)-1].Content != "create file foo.txt" {
		t.Fatalf("expected current instruction as final user turn, got %+v", captured.Messages[len(captured.Messages)-1])
	}
}

func TestExecuteWithInputSkipsHistoricalToolMessagesInModelContext(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "list files in ."}); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "tool", Content: `{"tool":"fs.list","id":"tool-json-1","output":"{\"entries\":[\"README.md\"]}"}`, ToolCallID: "tool-json-1", ToolName: "fs.list"}); err != nil {
		t.Fatalf("append tool message: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: "Found one file."}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}

	var captured struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Providers.Generic.BaseURL = server.URL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "create file foo.txt", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}

	for _, msg := range captured.Messages {
		if msg.Role == "tool" {
			t.Fatalf("expected tool role to be excluded from model history, got %+v", captured.Messages)
		}
	}
	if len(captured.Messages) < 4 {
		t.Fatalf("expected system + user/assistant/history + current user, got %d", len(captured.Messages))
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "list files in ." {
		t.Fatalf("unexpected first history message: %+v", captured.Messages[1])
	}
	if captured.Messages[2].Role != "assistant" || captured.Messages[2].Content != "Found one file." {
		t.Fatalf("unexpected assistant history message: %+v", captured.Messages[2])
	}
	if captured.Messages[len(captured.Messages)-1].Role != "user" || captured.Messages[len(captured.Messages)-1].Content != "create file foo.txt" {
		t.Fatalf("expected current user message to remain final turn, got %+v", captured.Messages[len(captured.Messages)-1])
	}
}

func TestExecuteWithInputPersistsMultiToolChatFlow(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "workspace", "notes.txt"), []byte("hello from notes\n"), 0o600); err != nil {
		t.Fatalf("seed workspace file: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "show files in workspace"}); err != nil {
		t.Fatalf("append history user: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: "I will inspect the workspace."}); err != nil {
		t.Fatalf("append history assistant: %v", err)
	}

	var (
		mu       sync.Mutex
		requests []capturedChatRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload capturedChatRequest
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
				"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.read\",\"arguments\":{\"path\":\"notes.txt\"}}\n```"}}},
			})
		case 3:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"content": "notes.txt contains hello from notes."}}},
			})
		default:
			t.Fatalf("unexpected extra provider call: %d", callNum)
		}
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Providers.Generic.BaseURL = server.URL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID:   "default",
		Message:   "list files and read notes.txt",
		Source:    "dashboard",
		SessionID: session.SessionID,
	})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}
	if strings.TrimSpace(res.FinalText) != "notes.txt contains hello from notes." {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}
	if res.ToolCalls != 2 {
		t.Fatalf("expected 2 tool calls, got %d", res.ToolCalls)
	}

	mu.Lock()
	if len(requests) != 3 {
		mu.Unlock()
		t.Fatalf("expected 3 provider requests, got %d", len(requests))
	}
	firstReq := requests[0]
	secondReq := requests[1]
	thirdReq := requests[2]
	mu.Unlock()

	if len(firstReq.Messages) < 4 {
		t.Fatalf("expected system + history + user messages, got %d", len(firstReq.Messages))
	}
	if firstReq.Messages[1].Role != "user" || firstReq.Messages[1].Content != "show files in workspace" {
		t.Fatalf("unexpected first history message: %+v", firstReq.Messages[1])
	}
	if firstReq.Messages[2].Role != "assistant" || firstReq.Messages[2].Content != "I will inspect the workspace." {
		t.Fatalf("unexpected second history message: %+v", firstReq.Messages[2])
	}
	if firstReq.Messages[len(firstReq.Messages)-1].Role != "user" || firstReq.Messages[len(firstReq.Messages)-1].Content != "list files and read notes.txt" {
		t.Fatalf("unexpected trailing user message: %+v", firstReq.Messages[len(firstReq.Messages)-1])
	}

	traceItems, ok := res.Trace["tool_execution_results"].([]any)
	if !ok || len(traceItems) != 2 {
		t.Fatalf("expected two trace tool entries, got %#v", res.Trace["tool_execution_results"])
	}
	firstTrace, ok := traceItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first trace entry: %#v", traceItems[0])
	}
	secondTrace, ok := traceItems[1].(map[string]any)
	if !ok {
		t.Fatalf("unexpected second trace entry: %#v", traceItems[1])
	}
	firstCallID := strings.TrimSpace(firstTrace["tool_call_id"].(string))
	secondCallID := strings.TrimSpace(secondTrace["tool_call_id"].(string))
	if firstCallID == "" || secondCallID == "" {
		t.Fatalf("expected non-empty tool call IDs, got %q and %q", firstCallID, secondCallID)
	}
	if firstCallID == secondCallID {
		t.Fatalf("expected distinct tool call IDs, got duplicate %q", firstCallID)
	}

	if !strings.Contains(secondReq.Messages[0].Content, "## Tool Results") || !strings.Contains(secondReq.Messages[0].Content, firstCallID) {
		t.Fatalf("expected second request prompt to include first tool result context, got %q", secondReq.Messages[0].Content)
	}
	if !strings.Contains(thirdReq.Messages[0].Content, firstCallID) || !strings.Contains(thirdReq.Messages[0].Content, secondCallID) {
		t.Fatalf("expected third request prompt to include both tool result IDs, got %q", thirdReq.Messages[0].Content)
	}

	stored, err := chat.ReadRecentMessages(session.SessionID, 20)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	toolMessages := make([]chatstore.Message, 0, 2)
	for _, msg := range stored {
		if msg.Role == "tool" {
			toolMessages = append(toolMessages, msg)
		}
	}
	if len(toolMessages) != 2 {
		t.Fatalf("expected 2 persisted tool messages, got %d (%+v)", len(toolMessages), stored)
	}
	if stored[len(stored)-1].Role != "assistant" || strings.TrimSpace(stored[len(stored)-1].Content) != "notes.txt contains hello from notes." {
		t.Fatalf("expected assistant final message persisted at end, got %+v", stored[len(stored)-1])
	}

	for _, msg := range toolMessages {
		if strings.TrimSpace(msg.ToolName) == "" || strings.TrimSpace(msg.ToolCallID) == "" {
			t.Fatalf("expected persisted tool metadata, got %+v", msg)
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
			t.Fatalf("decode tool payload: %v", err)
		}
		if payload["tool"] != msg.ToolName {
			t.Fatalf("expected payload tool %q to match metadata %q", payload["tool"], msg.ToolName)
		}
		if payload["id"] != msg.ToolCallID {
			t.Fatalf("expected payload id %q to match metadata %q", payload["id"], msg.ToolCallID)
		}
	}
}

func TestExecuteWithInputCompactsLongHistoryAndKeepsLatestTurn(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	chat, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := chat.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 220; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		content := strings.Repeat("history-window-content-", 180) + " marker-" + strconv.Itoa(i)
		if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: role, Content: content}); err != nil {
			t.Fatalf("append history message %d: %v", i, err)
		}
	}

	var captured capturedChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
	}))
	defer server.Close()

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Model.MaxTokens = 20000
	cfg.Providers.Generic.BaseURL = server.URL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err = e.ExecuteWithInput(context.Background(), ExecuteInput{
		AgentID:   "default",
		Source:    "dashboard",
		SessionID: session.SessionID,
		Message:   "latest-user-message-marker",
	})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}

	if len(captured.Messages) < 3 {
		t.Fatalf("expected compacted model request with multiple messages, got %d", len(captured.Messages))
	}
	if captured.Messages[1].Role != "system" || !strings.Contains(captured.Messages[1].Content, "Conversation compaction summary") {
		t.Fatalf("expected compaction summary in request, got %+v", captured.Messages[1])
	}
	last := captured.Messages[len(captured.Messages)-1]
	if last.Role != "user" || last.Content != "latest-user-message-marker" {
		t.Fatalf("expected latest user turn at tail, got %+v", last)
	}
}
