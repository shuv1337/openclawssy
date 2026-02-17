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
		if !strings.Contains(doc.Content, "chain tool calls until the task is complete") {
			t.Fatalf("tool best practices missing multi-step chaining guidance: %q", doc.Content)
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

func TestExecuteWithInputIncludesHistoricalToolMessagesInModelContext(t *testing.T) {
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

	if len(captured.Messages) < 5 {
		t.Fatalf("expected system + user/tool/assistant history + current user, got %d", len(captured.Messages))
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "list files in ." {
		t.Fatalf("unexpected first history message: %+v", captured.Messages[1])
	}
	if captured.Messages[2].Role != "tool" {
		t.Fatalf("expected tool history message in context, got %+v", captured.Messages[2])
	}
	if !strings.Contains(captured.Messages[2].Content, "tool fs.list result") || !strings.Contains(captured.Messages[2].Content, "README.md") {
		t.Fatalf("unexpected tool history message content: %q", captured.Messages[2].Content)
	}
	if captured.Messages[3].Role != "assistant" || captured.Messages[3].Content != "Found one file." {
		t.Fatalf("unexpected assistant history message: %+v", captured.Messages[3])
	}
	if captured.Messages[len(captured.Messages)-1].Role != "user" || captured.Messages[len(captured.Messages)-1].Content != "create file foo.txt" {
		t.Fatalf("expected current user message to remain final turn, got %+v", captured.Messages[len(captured.Messages)-1])
	}
}

func TestLoadSessionMessagesAppliesToolAndHistoryTruncation(t *testing.T) {
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

	for i := 0; i < 16; i++ {
		content := strings.Repeat("history-window-content-", 60) + " marker-" + strconv.Itoa(i)
		if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "assistant", Content: content}); err != nil {
			t.Fatalf("append history message %d: %v", i, err)
		}
	}

	toolPayload, err := json.Marshal(map[string]any{
		"tool":    "fs.read",
		"id":      "tool-json-1",
		"summary": "read large diagnostics file",
		"output":  strings.Repeat("x", 9000),
	})
	if err != nil {
		t.Fatalf("marshal tool payload: %v", err)
	}
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "tool", Content: string(toolPayload), ToolName: "fs.read", ToolCallID: "tool-json-1"}); err != nil {
		t.Fatalf("append tool message: %v", err)
	}

	messages, err := e.loadSessionMessages(session.SessionID, 200)
	if err != nil {
		t.Fatalf("load session messages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatalf("expected loaded session messages")
	}

	totalChars := 0
	foundTool := false
	for _, msg := range messages {
		totalChars += len([]rune(strings.TrimSpace(msg.Content)))
		if msg.Role != "tool" {
			continue
		}
		foundTool = true
		if !strings.Contains(msg.Content, "tool fs.read result") {
			t.Fatalf("expected normalized tool header in context, got %q", msg.Content)
		}
		if len([]rune(msg.Content)) > maxSessionMessageChars {
			t.Fatalf("expected tool context message to be truncated to %d chars, got %d", maxSessionMessageChars, len([]rune(msg.Content)))
		}
		if strings.Contains(msg.Content, strings.Repeat("x", 1400)) {
			t.Fatalf("expected long tool output to be truncated, got %q", msg.Content)
		}
	}
	if !foundTool {
		t.Fatalf("expected at least one tool message in loaded context: %+v", messages)
	}
	if totalChars > maxSessionContextChars {
		t.Fatalf("expected total session context <= %d chars, got %d", maxSessionContextChars, totalChars)
	}

	combined := make([]string, 0, len(messages))
	for _, msg := range messages {
		combined = append(combined, msg.Content)
	}
	joined := strings.Join(combined, "\n")
	if strings.Contains(joined, "marker-0") {
		t.Fatalf("expected oldest messages to be truncated by history budget")
	}
	if !strings.Contains(joined, "marker-15") {
		t.Fatalf("expected latest messages to remain after truncation")
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
		summary, _ := payload["summary"].(string)
		if strings.TrimSpace(summary) == "" {
			t.Fatalf("expected non-empty tool summary in persisted payload, got %#v", payload)
		}
	}
}

func TestExecuteWithInputTruncatesLongHistoryAndKeepsLatestTurn(t *testing.T) {
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
		content := "marker-" + strconv.Itoa(i) + " " + strings.Repeat("history-window-content-", 180)
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
		t.Fatalf("expected truncated model request with multiple messages, got %d", len(captured.Messages))
	}
	joinedHistory := ""
	for i := 1; i < len(captured.Messages); i++ {
		if captured.Messages[i].Role != "system" && len([]rune(captured.Messages[i].Content)) > maxSessionMessageChars {
			t.Fatalf("expected non-system message content <= %d chars, got %d", maxSessionMessageChars, len([]rune(captured.Messages[i].Content)))
		}
		joinedHistory += captured.Messages[i].Content
	}
	if strings.Contains(joinedHistory, "marker-0") {
		t.Fatalf("expected oldest history to be omitted from model request")
	}
	if !strings.Contains(joinedHistory, "marker-219") {
		t.Fatalf("expected latest history marker to remain in model request")
	}
	last := captured.Messages[len(captured.Messages)-1]
	if last.Role != "user" || last.Content != "latest-user-message-marker" {
		t.Fatalf("expected latest user turn at tail, got %+v", last)
	}
}

func TestExecuteWithInputRepeatedToolCallIsHandledWithoutFailure(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "done"}}}})
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

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "list files", Source: "dashboard"})
	if err != nil {
		t.Fatalf("execute with input: %v", err)
	}
	if strings.TrimSpace(res.FinalText) != "done" {
		t.Fatalf("unexpected final text: %q", res.FinalText)
	}
	if res.ToolCalls != 2 {
		t.Fatalf("expected two recorded tool calls, got %d", res.ToolCalls)
	}
	if calls != 3 {
		t.Fatalf("expected three provider calls, got %d", calls)
	}
}

func TestExecuteWithInputPersistsToolMessageEvenWhenRunFailsLater(t *testing.T) {
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

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
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

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "list files then continue", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("expected graceful recovery from second provider response failure, got %v", err)
	}
	if !strings.Contains(res.FinalText, "model/API error") {
		t.Fatalf("expected degraded final response to mention model error, got %q", res.FinalText)
	}

	msgs, err := chat.ReadRecentMessages(session.SessionID, 20)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(msgs) < 1 {
		t.Fatalf("expected at least one persisted message, got %+v", msgs)
	}
	foundTool := false
	for _, msg := range msgs {
		if msg.Role == "tool" {
			foundTool = true
			payload := map[string]any{}
			if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
				t.Fatalf("decode tool payload: %v", err)
			}
			summary, _ := payload["summary"].(string)
			if strings.TrimSpace(summary) == "" {
				t.Fatalf("expected summary in persisted tool payload, got %#v", payload)
			}
		}
	}
	if !foundTool {
		t.Fatalf("expected at least one persisted tool message after partial failure, got %+v", msgs)
	}
}

func TestExecuteWithInputLogsAndTracesOnToolCallCallbackFailures(t *testing.T) {
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
	if err := chat.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "show files"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}

	messagesPath := filepath.Join(root, ".openclawssy", "agents", "default", "memory", "chats", session.SessionID, "messages.jsonl")
	if err := os.Chmod(messagesPath, 0o400); err != nil {
		t.Fatalf("chmod messages file read-only: %v", err)
	}

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```"}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": ""}}}})
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

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "list files", Source: "dashboard", SessionID: session.SessionID})
	if err != nil {
		t.Fatalf("expected callback failures to be non-fatal, got %v", err)
	}
	if res.ToolCalls != 1 {
		t.Fatalf("expected one tool call, got %d", res.ToolCalls)
	}

	traceItems, ok := res.Trace["tool_execution_results"].([]any)
	if !ok || len(traceItems) != 1 {
		t.Fatalf("expected one tool trace entry, got %#v", res.Trace["tool_execution_results"])
	}
	entry, ok := traceItems[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected trace entry: %#v", traceItems[0])
	}
	callbackError, _ := entry["callback_error"].(string)
	if strings.TrimSpace(callbackError) == "" {
		t.Fatalf("expected callback_error in trace entry, got %#v", entry)
	}

	auditPath := filepath.Join(root, ".openclawssy", "agents", "default", "audit", "events.jsonl")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	found := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("decode audit event: %v", err)
		}
		if evt["type"] == "tool.callback_error" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool.callback_error event in audit log, got %q", string(raw))
	}
}

func TestExecuteDefaultOnErrorDoesNotShowThinkingOnSuccessfulRun(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>private plan</think>visible answer"}}},
		})
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

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(res.FinalText, "Thinking:") {
		t.Fatalf("expected thinking hidden in default successful output, got %q", res.FinalText)
	}
}

func TestExecuteDefaultOnErrorShowsThinkingOnParseFailure(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>parse diagnostics</think>```json\n{invalid}\n```"}}},
		})
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

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.FinalText, "Thinking:\nparse diagnostics") {
		t.Fatalf("expected thinking shown for parse failure, got %q", res.FinalText)
	}
}

func TestExecuteAlwaysThinkingModeAlwaysShowsThinking(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>detailed notes</think>visible answer"}}},
		})
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

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.FinalText, "Thinking:\ndetailed notes") {
		t.Fatalf("expected thinking shown in always mode, got %q", res.FinalText)
	}
}

func TestExecutePersistsRedactedThinkingInTraceArtifactAndAudit(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if err := e.Init("default", false); err != nil {
		t.Fatalf("init: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<think>api_key=super-secret-value-1234567890abcdefghijklmnopqrstuvwxyz</think>ok"}}},
		})
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

	res, err := e.ExecuteWithInput(context.Background(), ExecuteInput{AgentID: "default", Message: "hi", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	traceThinking, _ := res.Trace["thinking"].(string)
	if !strings.Contains(traceThinking, "[REDACTED]") {
		t.Fatalf("expected redacted thinking in trace, got %q", traceThinking)
	}
	if present, ok := res.Trace["thinking_present"].(bool); !ok || !present {
		t.Fatalf("expected thinking_present=true in trace, got %#v", res.Trace["thinking_present"])
	}

	metaRaw, err := os.ReadFile(filepath.Join(res.ArtifactPath, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	meta := map[string]any{}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatalf("unmarshal meta.json: %v", err)
	}
	metaThinking, _ := meta["thinking"].(string)
	if !strings.Contains(metaThinking, "[REDACTED]") {
		t.Fatalf("expected redacted thinking in artifact meta, got %q", metaThinking)
	}
	if present, ok := meta["thinking_present"].(bool); !ok || !present {
		t.Fatalf("expected thinking_present=true in artifact meta, got %#v", meta["thinking_present"])
	}

	auditRaw, err := os.ReadFile(filepath.Join(root, ".openclawssy", "agents", "default", "audit", "events.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(auditRaw), "\"thinking\":\"[REDACTED]\"") {
		t.Fatalf("expected redacted thinking in audit log, got %q", string(auditRaw))
	}
}
