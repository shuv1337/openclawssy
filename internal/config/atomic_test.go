package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomic_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	data := []byte("hello world")
	mode := os.FileMode(0o644)

	if err := WriteAtomic(path, data, mode); err != nil {
		t.Fatalf("WriteAtomic failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("got %q, want %q", got, data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode() != mode {
		t.Errorf("got mode %o, want %o", info.Mode(), mode)
	}
}

func TestWriteAtomic_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	oldData := []byte("old content")
	newData := []byte("new content")
	mode := os.FileMode(0o600)

	if err := os.WriteFile(path, oldData, mode); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := WriteAtomic(path, newData, mode); err != nil {
		t.Fatalf("WriteAtomic failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.Equal(got, newData) {
		t.Errorf("got %q, want %q", got, newData)
	}

	bakPath := path + ".bak"
	gotBak, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("ReadFile backup failed: %v", err)
	}
	if !bytes.Equal(gotBak, oldData) {
		t.Errorf("backup: got %q, want %q", gotBak, oldData)
	}
}

func TestWriteAtomic_CreateDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new/sub/dir/test.txt")
	data := []byte("hello")
	mode := os.FileMode(0o644)

	if err := WriteAtomic(path, data, mode); err != nil {
		t.Fatalf("WriteAtomic failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

func TestWriteAtomic_UnwritableDir(t *testing.T) {
	dir := t.TempDir()
	unwritable := filepath.Join(dir, "unwritable")
	if err := os.Mkdir(unwritable, 0o555); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	defer os.Chmod(unwritable, 0o755) // cleanup so TempDir can be removed

	path := filepath.Join(unwritable, "test.txt")
	if err := WriteAtomic(path, []byte("data"), 0o644); err == nil {
		t.Error("expected error writing to unwritable directory, got nil")
	}
}

func TestWriteAtomic_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")

	// Create a file where we want a directory to be.
	if err := os.WriteFile(blocked, []byte("block"), 0o644); err != nil {
		t.Fatalf("failed to create blocking file: %v", err)
	}

	target := filepath.Join(blocked, "file")
	err := WriteAtomic(target, []byte("data"), 0o644)
	if err == nil {
		t.Error("expected error when directory path is blocked by a file")
	}
}

func TestWriteAtomic_WriteError(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(targetDir, 0o500); err != nil {
		t.Fatalf("failed to create readonly dir: %v", err)
	}
	defer os.Chmod(targetDir, 0o755)

	target := filepath.Join(targetDir, "file")
	err := WriteAtomic(target, []byte("data"), 0o644)
	if err == nil {
		t.Error("expected error when writing to read-only directory")
	}
}

func TestWriteAtomic_BackupError(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "readonly_with_file")

	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	target := filepath.Join(targetDir, "file")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatalf("failed to create original file: %v", err)
	}

	if err := os.Chmod(targetDir, 0o500); err != nil {
		t.Fatalf("failed to make dir readonly: %v", err)
	}
	defer os.Chmod(targetDir, 0o755)

	err := WriteAtomic(target, []byte("new"), 0o644)
	if err == nil {
		t.Error("expected error when writing backup in read-only directory")
	} else if !strings.Contains(err.Error(), "write backup") {
		t.Errorf("expected 'write backup' error, got: %v", err)
	}
}
