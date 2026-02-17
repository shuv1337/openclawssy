package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakePolicy struct {
	deny bool
	ws   string
}

func (p fakePolicy) CheckTool(agentID, tool string) error {
	if p.deny {
		return &ToolError{Code: ErrCodePolicyDenied, Tool: tool, Message: "blocked"}
	}
	return nil
}

func (p fakePolicy) ResolveReadPath(workspace, target string) (string, error) {
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Join(workspace, target), nil
}

func (p fakePolicy) ResolveWritePath(workspace, target string) (string, error) {
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Join(workspace, target), nil
}

type memAudit struct {
	events []string
}

type fakeShell struct{}

func (fakeShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	return command + " " + args[0], "", 0, nil
}

func (m *memAudit) LogEvent(ctx context.Context, eventType string, fields map[string]any) error {
	_ = ctx
	_ = fields
	m.events = append(m.events, eventType)
	return nil
}

func TestRegistryRegisterAndNotFound(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := reg.Register(ToolSpec{Name: "ok"}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "missing", ".", nil); err == nil {
		t.Fatalf("expected not found error")
	}
}

func TestRegistryPolicyDenied(t *testing.T) {
	a := &memAudit{}
	reg := NewRegistry(fakePolicy{deny: true}, a)
	if err := reg.Register(ToolSpec{Name: "t"}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "t", ".", nil); err == nil {
		t.Fatalf("expected denied error")
	}

	foundDenied := false
	for _, e := range a.events {
		if e == "policy.denied" {
			foundDenied = true
		}
	}
	if !foundDenied {
		t.Fatalf("expected policy.denied audit event")
	}
}

func TestCoreFsTools(t *testing.T) {
	ws := t.TempDir()
	audit := &memAudit{}
	reg := NewRegistry(fakePolicy{}, audit)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	writeRes, err := reg.Execute(context.Background(), "agent", "fs.write", ws, map[string]any{
		"path":    "hello.txt",
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("fs.write: %v", err)
	}
	if got := writeRes["lines"]; got != 1 {
		t.Fatalf("expected fs.write lines=1, got %#v", got)
	}
	if got := writeRes["summary"]; got != "wrote 1 line(s) to hello.txt" {
		t.Fatalf("unexpected fs.write summary: %#v", got)
	}

	readRes, err := reg.Execute(context.Background(), "agent", "fs.read", ws, map[string]any{"path": "hello.txt"})
	if err != nil {
		t.Fatalf("fs.read: %v", err)
	}
	if got := readRes["content"]; got != "hello world" {
		t.Fatalf("unexpected read content: %#v", got)
	}

	_, err = reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path": "hello.txt",
		"old":  "world",
		"new":  "there",
	})
	if err != nil {
		t.Fatalf("fs.edit: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(ws, "hello.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "hello there" {
		t.Fatalf("unexpected edited content: %q", string(b))
	}

	if _, err := reg.Execute(context.Background(), "agent", "fs.list", ws, map[string]any{"path": "."}); err != nil {
		t.Fatalf("fs.list: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "code.search", ws, map[string]any{"pattern": "hello"}); err != nil {
		t.Fatalf("code.search: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "time.now", ws, nil); err != nil {
		t.Fatalf("time.now: %v", err)
	}
}

func TestShellExecTool(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(fakeShell{})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"ok"},
	})
	if err != nil {
		t.Fatalf("shell.exec: %v", err)
	}
	if res["stdout"] != "echo ok" {
		t.Fatalf("unexpected stdout: %#v", res["stdout"])
	}
}

func TestFsEditLinePatch(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	_, err := reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path":  "a.txt",
		"edits": []any{map[string]any{"startLine": 2, "endLine": 2, "newText": "TWO"}},
	})
	if err != nil {
		t.Fatalf("fs.edit patch: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(ws, "a.txt"))
	if string(b) != "one\nTWO\nthree\n" {
		t.Fatalf("unexpected patched content: %q", string(b))
	}
}

func TestFsWriteRejectsWorkspaceControlPlaneFilename(t *testing.T) {
	ws := t.TempDir()
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent-123", "fs.write", ws, map[string]any{
		"path":    filepath.Join("notes", "SOUL.md"),
		"content": "new content",
	})
	if err == nil {
		t.Fatalf("expected control-plane filename guard error")
	}
	msg := err.Error()
	for _, needle := range []string{
		"does not control agent behavior",
		".openclawssy/agents/<agent_id>/SOUL.md",
		"dashboard Agent Files",
	} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("expected error to contain %q, got %q", needle, msg)
		}
	}
}

func TestFsEditRejectsWorkspaceControlPlaneFilename(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "DEVPLAN.md"), []byte("draft"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent-123", "fs.edit", ws, map[string]any{
		"path": "DEVPLAN.md",
		"old":  "draft",
		"new":  "updated",
	})
	if err == nil {
		t.Fatalf("expected control-plane filename guard error")
	}
	msg := err.Error()
	for _, needle := range []string{
		"does not control agent behavior",
		".openclawssy/agents/<agent_id>/DEVPLAN.md",
		"dashboard Agent Files",
	} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("expected error to contain %q, got %q", needle, msg)
		}
	}
}
