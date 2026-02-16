package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
)

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
