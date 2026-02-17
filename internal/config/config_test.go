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

func TestDefaultConfigSetsMaxTokens(t *testing.T) {
	cfg := Default()
	if cfg.Model.MaxTokens != 20000 {
		t.Fatalf("expected default model.max_tokens=20000, got %d", cfg.Model.MaxTokens)
	}
}

func TestDefaultConfigBindsServerToLoopback(t *testing.T) {
	cfg := Default()
	if cfg.Server.BindAddress != "127.0.0.1" {
		t.Fatalf("expected default server.bind_address=127.0.0.1, got %q", cfg.Server.BindAddress)
	}
}

func TestValidateRejectsOutOfRangeMaxTokens(t *testing.T) {
	cfg := Default()
	cfg.Model.MaxTokens = 25000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for max_tokens > 20000")
	}
}

func TestDefaultConfigSetsThinkingModeOnError(t *testing.T) {
	cfg := Default()
	if cfg.Output.ThinkingMode != ThinkingModeOnError {
		t.Fatalf("expected default output.thinking_mode=%q, got %q", ThinkingModeOnError, cfg.Output.ThinkingMode)
	}
}

func TestApplyDefaultsSetsThinkingModeOnError(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Output.ThinkingMode != ThinkingModeOnError {
		t.Fatalf("expected thinking_mode default %q, got %q", ThinkingModeOnError, cfg.Output.ThinkingMode)
	}
}

func TestValidateRejectsInvalidThinkingMode(t *testing.T) {
	cfg := Default()
	cfg.Output.ThinkingMode = "sometimes"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid thinking_mode")
	}
}
