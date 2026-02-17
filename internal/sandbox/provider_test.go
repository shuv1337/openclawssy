package sandbox

import (
	"context"
	"errors"
	"testing"
)

func TestNoneProviderExecDenied(t *testing.T) {
	p := &NoneProvider{}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("start none provider: %v", err)
	}
	_, err := p.Exec(Command{Name: "pwd"})
	if !errors.Is(err, ErrExecDenied) {
		t.Fatalf("expected ErrExecDenied, got %v", err)
	}
}

func TestShellExecAllowedGating(t *testing.T) {
	none := &NoneProvider{}
	if ShellExecAllowed(none) {
		t.Fatal("none provider should never allow exec before start")
	}
	if err := none.Start(context.Background()); err != nil {
		t.Fatalf("start none provider: %v", err)
	}
	if ShellExecAllowed(none) {
		t.Fatal("none provider should never allow exec after start")
	}

	local, err := NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	if ShellExecAllowed(local) {
		t.Fatal("local provider should not allow exec before start")
	}
	if err := local.Start(context.Background()); err != nil {
		t.Fatalf("start local provider: %v", err)
	}
	if !ShellExecAllowed(local) {
		t.Fatal("local provider should allow exec after start")
	}
}

func TestNewProviderRejectsUnsupportedProvider(t *testing.T) {
	_, err := NewProvider("docker", t.TempDir())
	if err == nil {
		t.Fatalf("expected unsupported provider error")
	}
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected ErrUnknownProvider, got %v", err)
	}
}
