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
	cfg.Sandbox.Provider = "local"
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

func TestDefaultConfigSetsThinkingModeNever(t *testing.T) {
	cfg := Default()
	if cfg.Output.ThinkingMode != ThinkingModeNever {
		t.Fatalf("expected default output.thinking_mode=%q, got %q", ThinkingModeNever, cfg.Output.ThinkingMode)
	}
}

func TestApplyDefaultsSetsThinkingModeNever(t *testing.T) {
	cfg := Config{}
	cfg.ApplyDefaults()
	if cfg.Output.ThinkingMode != ThinkingModeNever {
		t.Fatalf("expected thinking_mode default %q, got %q", ThinkingModeNever, cfg.Output.ThinkingMode)
	}
	if cfg.Output.MaxThinkingChars != 4000 {
		t.Fatalf("expected max_thinking_chars default 4000, got %d", cfg.Output.MaxThinkingChars)
	}
}

func TestValidateRejectsInvalidThinkingMode(t *testing.T) {
	cfg := Default()
	cfg.Output.ThinkingMode = "sometimes"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid thinking_mode")
	}
}

func TestValidateRejectsUnsupportedSandboxProvider(t *testing.T) {
	cfg := Default()
	cfg.Sandbox.Provider = "docker"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for unsupported sandbox provider")
	}
}

func TestDefaultConfigSetsConcurrencyAndSchedulerDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Engine.MaxConcurrentRuns != 64 {
		t.Fatalf("expected engine.max_concurrent_runs=64, got %d", cfg.Engine.MaxConcurrentRuns)
	}
	if cfg.Scheduler.MaxConcurrentJobs != 4 {
		t.Fatalf("expected scheduler.max_concurrent_jobs=4, got %d", cfg.Scheduler.MaxConcurrentJobs)
	}
	if !cfg.Scheduler.CatchUp {
		t.Fatal("expected scheduler.catch_up=true by default")
	}
}

func TestValidateRejectsInvalidMaxThinkingChars(t *testing.T) {
	cfg := Default()
	cfg.Output.MaxThinkingChars = 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for output.max_thinking_chars")
	}
}

func TestValidateRejectsEmptyShellAllowedCommand(t *testing.T) {
	cfg := Default()
	cfg.Shell.AllowedCommands = []string{"git", "   "}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for empty shell.allowed_commands entry")
	}
}

func TestRedactedClearsSensitiveFieldsOnly(t *testing.T) {
	cfg := Default()
	cfg.Providers.OpenAI.APIKey = "openai-key"
	cfg.Providers.OpenRouter.APIKey = "openrouter-key"
	cfg.Providers.Requesty.APIKey = "requesty-key"
	cfg.Providers.ZAI.APIKey = "zai-key"
	cfg.Providers.Generic.APIKey = "generic-key"
	cfg.Discord.Token = "discord-token"
	cfg.Model.Name = "kept-model"

	redacted := cfg.Redacted()

	if redacted.Providers.OpenAI.APIKey != "" || redacted.Providers.OpenRouter.APIKey != "" || redacted.Providers.Requesty.APIKey != "" || redacted.Providers.ZAI.APIKey != "" || redacted.Providers.Generic.APIKey != "" {
		t.Fatalf("expected provider api keys redacted, got %+v", redacted.Providers)
	}
	if redacted.Discord.Token != "" {
		t.Fatalf("expected discord token redacted, got %q", redacted.Discord.Token)
	}
	if redacted.Model.Name != "kept-model" {
		t.Fatalf("expected non-sensitive model name preserved, got %q", redacted.Model.Name)
	}
}
