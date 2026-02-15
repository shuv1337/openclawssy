package policy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTraversalDenied(t *testing.T) {
	ws := t.TempDir()
	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.read"}})

	_, err := enf.ResolveReadPath(ws, "../secret.txt")
	if err == nil {
		t.Fatalf("expected traversal denial")
	}
}

func TestSymlinkEscapeDeniedOnRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires elevated privileges on many windows setups")
	}

	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	link := filepath.Join(ws, "escape.txt")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.read"}})
	_, err := enf.ResolveReadPath(ws, "escape.txt")
	if err == nil {
		t.Fatalf("expected symlink escape denial")
	}
}

func TestSymlinkEscapeDeniedOnWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires elevated privileges on many windows setups")
	}

	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	linkDir := filepath.Join(ws, "out")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Fatalf("create symlink dir: %v", err)
	}

	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.write"}})
	_, err := enf.ResolveWritePath(ws, filepath.Join("out", "new.txt"))
	if err == nil {
		t.Fatalf("expected symlink write escape denial")
	}
}
