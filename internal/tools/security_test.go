package tools

import (
	"context"
	"strings"
	"testing"
)

func TestShellExecDeniesEmptyAllowedList(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(fakeShell{})
	// No allowed commands set explicitly, so it's empty by default.

	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"should be denied if list is empty"},
	})

	if err == nil {
		t.Fatal("expected policy denied error for empty allowed list, got success")
	}
	if !strings.Contains(err.Error(), "command is not allowed") {
		t.Fatalf("expected 'command is not allowed' error, got %v", err)
	}
}

func TestShellExecWildcardAndSpecificAllowlist(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(fakeShell{})

	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	// Explicit "*" should allow.
	reg.SetShellAllowedCommands([]string{"*"})
	_, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})
	if err != nil {
		t.Errorf("expected command to be allowed with '*', got error: %v", err)
	}

	// Specific allowlist should only allow listed commands.
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
