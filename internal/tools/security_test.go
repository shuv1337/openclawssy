package tools

import (
	"context"
	"testing"
)

func TestShellExecDefaultDeny(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(fakeShell{})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	// Case 1: Empty allowlist should deny (currently it allows)
	_, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})

	// Current behavior: err is nil
	// Desired behavior: err is not nil and indicates denied
	if err == nil {
		t.Errorf("expected error when allowlist is empty, but command was allowed")
	}

	// Case 2: Explicit "*" should allow
	reg.SetShellAllowedCommands([]string{"*"})
	_, err = reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})
	if err != nil {
		t.Errorf("expected command to be allowed with '*', got error: %v", err)
	}

	// Case 3: Specific allowlist
	reg.SetShellAllowedCommands([]string{"ls"})
	_, err = reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "ls",
		"args":    []any{"-la"},
	})
	if err != nil {
		t.Errorf("expected 'ls' to be allowed, got error: %v", err)
	}

	_, err = reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})
	if err == nil {
		t.Errorf("expected 'echo' to be denied")
	}
}
