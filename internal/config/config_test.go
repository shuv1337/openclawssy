package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	bad := `{"shell":{"enable_exec":true},"sandbox":{"active":false},"server":{"bind_address":"127.0.0.1","port":8080},"workspace":{"root":"./workspace"}}`
	if err := WriteAtomic(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigRoundtripAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Sandbox.Active = true
	cfg.Sandbox.Provider = "docker"
	cfg.Shell.EnableExec = true

	if err := Save(path, cfg); err != nil {
		t.Fatalf("first save: %v", err)
	}

	cfg.Server.Port = 9090
	if err := Save(path, cfg); err != nil {
		t.Fatalf("second save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Server.Port != 9090 {
		t.Fatalf("expected server port 9090, got %d", loaded.Server.Port)
	}

	bak := filepath.Join(dir, "config.json.bak")
	if _, err := Load(bak); err != nil {
		t.Fatalf("expected readable backup config, got: %v", err)
	}
}
