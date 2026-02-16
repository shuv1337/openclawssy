package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
