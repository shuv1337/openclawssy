package tools

import (
	"context"
	"os"
	"path/filepath"
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

	_, err := reg.Execute(context.Background(), "agent", "fs.write", ws, map[string]any{
		"path":    "hello.txt",
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("fs.write: %v", err)
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
