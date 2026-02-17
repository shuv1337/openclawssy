package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	recs   []auditRecord
}

type auditRecord struct {
	eventType string
	fields    map[string]any
}

type fakeShell struct{}

func (fakeShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	return command + " " + args[0], "", 0, nil
}

type fallbackShell struct {
	calls []string
}

func (s *fallbackShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	s.calls = append(s.calls, command)
	if command == "bash" {
		return "", "", -1, errors.New(`exec: "bash": executable file not found in $PATH`)
	}
	if command == "/bin/bash" {
		return "venv ready", "", 0, nil
	}
	if command == "/usr/bin/bash" {
		return "", "", -1, errors.New(`fork/exec /usr/bin/bash: no such file or directory`)
	}
	if command == "sh" {
		return "venv ready", "", 0, nil
	}
	return "", "", 127, errors.New("unexpected command")
}

type shOnlyFallbackShell struct {
	calls []string
}

func (s *shOnlyFallbackShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	s.calls = append(s.calls, command)
	if command == "bash" {
		return "", "", -1, errors.New(`exec: "bash": executable file not found in $PATH`)
	}
	if command == "/bin/bash" {
		return "", "", -1, errors.New(`fork/exec /bin/bash: no such file or directory`)
	}
	if command == "/usr/bin/bash" {
		return "", "", -1, errors.New(`fork/exec /usr/bin/bash: no such file or directory`)
	}
	if command == "sh" {
		return "venv ready via sh", "", 0, nil
	}
	return "", "", 127, errors.New("unexpected command")
}

type exitStatusShell struct{}

func (exitStatusShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	_ = command
	_ = args
	return "scan partial output", "permission denied", 1, errors.New("exit status 1")
}

func (m *memAudit) LogEvent(ctx context.Context, eventType string, fields map[string]any) error {
	_ = ctx
	copyFields := map[string]any{}
	for k, v := range fields {
		copyFields[k] = v
	}
	m.events = append(m.events, eventType)
	m.recs = append(m.recs, auditRecord{eventType: eventType, fields: copyFields})
	return nil
}

func TestRegistryRegisterAndNotFound(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := reg.Register(ToolSpec{Name: "ok"}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "missing", ".", nil)
	if err == nil {
		t.Fatalf("expected not found error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodeNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeNotFound, toolErr.Code)
	}
}

func TestRegistryMissingRequiredFieldUsesCanonicalInputInvalidCode(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := reg.Register(ToolSpec{Name: "needs_path", Required: []string{"path"}}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "needs_path", ".", map[string]any{})
	if err == nil {
		t.Fatalf("expected input invalid error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodeInvalidInput {
		t.Fatalf("expected %s, got %s", ErrCodeInvalidInput, toolErr.Code)
	}
}

func TestRegistryRejectsWrongTypeForRequiredField(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := reg.Register(ToolSpec{Name: "needs_path", Required: []string{"path"}, ArgTypes: map[string]ArgType{"path": ArgTypeString}}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "needs_path", ".", map[string]any{"path": 42})
	if err == nil {
		t.Fatalf("expected invalid-input type error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodeInvalidInput {
		t.Fatalf("expected %s, got %s", ErrCodeInvalidInput, toolErr.Code)
	}
	if !strings.Contains(toolErr.Message, "invalid type") {
		t.Fatalf("expected invalid type message, got %q", toolErr.Message)
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

func TestRegistryTimeoutIsStructuredAndAudited(t *testing.T) {
	a := &memAudit{}
	reg := NewRegistry(fakePolicy{}, a)
	if err := reg.Register(ToolSpec{Name: "slow"}, func(ctx context.Context, req Request) (map[string]any, error) {
		_ = req
		<-ctx.Done()
		return nil, ctx.Err()
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := reg.Execute(ctx, "agent", "slow", ".", nil)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected tool error, got %T", err)
	}
	if toolErr.Code != ErrCodeTimeout {
		t.Fatalf("expected timeout code, got %s", toolErr.Code)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("expected structured timeout error text, got %q", err.Error())
	}

	foundAuditedTimeout := false
	for _, rec := range a.recs {
		if rec.eventType != "tool.result" {
			continue
		}
		errText, _ := rec.fields["error"].(string)
		if strings.Contains(strings.ToLower(errText), "timeout") {
			foundAuditedTimeout = true
			break
		}
	}
	if !foundAuditedTimeout {
		t.Fatalf("expected timeout to be present in tool.result audit event, records=%#v", a.recs)
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

func TestShellExecFallsBackToShWhenBashUnavailable(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	shell := &fallbackShell{}
	reg.SetShellExecutor(shell)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "bash",
		"args":    []any{"-lc", "python -m venv .venv"},
	})
	if err != nil {
		t.Fatalf("shell.exec expected fallback success, got error: %v", err)
	}
	if len(shell.calls) != 2 || shell.calls[0] != "bash" || shell.calls[1] != "/bin/bash" {
		t.Fatalf("expected bash then /bin/bash fallback, got %#v", shell.calls)
	}
	if res["stdout"] != "venv ready" {
		t.Fatalf("unexpected stdout after fallback: %#v", res["stdout"])
	}
	if res["shell_fallback"] != "/bin/bash" {
		t.Fatalf("expected shell_fallback=/bin/bash, got %#v", res["shell_fallback"])
	}
}

func TestShellExecFallsBackToShWhenBashBinaryMissing(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	shell := &shOnlyFallbackShell{}
	reg.SetShellExecutor(shell)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "bash",
		"args":    []any{"-lc", "python -m venv .venv"},
	})
	if err != nil {
		t.Fatalf("shell.exec expected sh fallback success, got error: %v", err)
	}
	if len(shell.calls) != 4 || shell.calls[0] != "bash" || shell.calls[1] != "/bin/bash" || shell.calls[2] != "/usr/bin/bash" || shell.calls[3] != "sh" {
		t.Fatalf("expected bash -> /bin/bash -> /usr/bin/bash -> sh order, got %#v", shell.calls)
	}
	if res["stdout"] != "venv ready via sh" {
		t.Fatalf("unexpected stdout after sh fallback: %#v", res["stdout"])
	}
	if res["shell_fallback"] != "sh" {
		t.Fatalf("expected shell_fallback=sh, got %#v", res["shell_fallback"])
	}
}

func TestShellExecTreatsExitStatusAsResultNotToolFailure(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(exitStatusShell{})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "bash",
		"args":    []any{"-lc", "nmap -sS 127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("expected non-zero exit status to return structured result without tool failure, got: %v", err)
	}
	if res["exit_code"] != 1 {
		t.Fatalf("expected exit_code=1, got %#v", res["exit_code"])
	}
	if strings.TrimSpace(res["error"].(string)) != "exit status 1" {
		t.Fatalf("expected structured error field with exit status, got %#v", res["error"])
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
