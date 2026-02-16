package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Network   NetworkConfig   `json:"network"`
	Shell     ShellConfig     `json:"shell"`
	Sandbox   SandboxConfig   `json:"sandbox"`
	Server    ServerConfig    `json:"server"`
	Workspace WorkspaceConfig `json:"workspace"`
	Model     ModelConfig     `json:"model"`
	Providers ProvidersConfig `json:"providers"`
	Chat      ChatConfig      `json:"chat"`
	Discord   DiscordConfig   `json:"discord"`
	Secrets   SecretsConfig   `json:"secrets"`
}

type NetworkConfig struct {
	Enabled         bool     `json:"enabled"`
	AllowedDomains  []string `json:"allowed_domains,omitempty"`
	AllowLocalhosts bool     `json:"allow_localhosts,omitempty"`
}

type ShellConfig struct {
	EnableExec bool `json:"enable_exec"`
}

type SandboxConfig struct {
	Active   bool   `json:"active"`
	Provider string `json:"provider"`
}

type ServerConfig struct {
	BindAddress string `json:"bind_address"`
	Port        int    `json:"port"`
	TLSEnabled  bool   `json:"tls_enabled"`
	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`
	Dashboard   bool   `json:"dashboard_enabled"`
}

type WorkspaceConfig struct {
	Root string `json:"root"`
}

type ModelConfig struct {
	Provider    string  `json:"provider"`
	Name        string  `json:"name"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

type ProviderEndpointConfig struct {
	BaseURL   string            `json:"base_url"`
	APIKey    string            `json:"api_key,omitempty"`
	APIKeyEnv string            `json:"api_key_env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type ProvidersConfig struct {
	OpenAI     ProviderEndpointConfig `json:"openai"`
	OpenRouter ProviderEndpointConfig `json:"openrouter"`
	Requesty   ProviderEndpointConfig `json:"requesty"`
	ZAI        ProviderEndpointConfig `json:"zai"`
	Generic    ProviderEndpointConfig `json:"generic"`
}

type ChatConfig struct {
	Enabled         bool     `json:"enabled"`
	DefaultAgentID  string   `json:"default_agent_id"`
	AllowUsers      []string `json:"allow_users,omitempty"`
	AllowRooms      []string `json:"allow_rooms,omitempty"`
	RateLimitPerMin int      `json:"rate_limit_per_min,omitempty"`
}

type DiscordConfig struct {
	Enabled         bool     `json:"enabled"`
	Token           string   `json:"token,omitempty"`
	TokenEnv        string   `json:"token_env,omitempty"`
	DefaultAgentID  string   `json:"default_agent_id"`
	AllowGuilds     []string `json:"allow_guilds,omitempty"`
	AllowChannels   []string `json:"allow_channels,omitempty"`
	AllowUsers      []string `json:"allow_users,omitempty"`
	CommandPrefix   string   `json:"command_prefix,omitempty"`
	RateLimitPerMin int      `json:"rate_limit_per_min,omitempty"`
}

type SecretsConfig struct {
	StoreFile     string `json:"store_file"`
	MasterKeyFile string `json:"master_key_file"`
}

func Default() Config {
	return Config{
		Network: NetworkConfig{
			Enabled: false,
		},
		Shell: ShellConfig{
			EnableExec: false,
		},
		Sandbox: SandboxConfig{
			Active:   false,
			Provider: "none",
		},
		Server: ServerConfig{
			BindAddress: "0.0.0.0",
			Port:        8080,
			TLSEnabled:  false,
			TLSCertFile: ".openclawssy/certs/server.crt",
			TLSKeyFile:  ".openclawssy/certs/server.key",
			Dashboard:   true,
		},
		Workspace: WorkspaceConfig{
			Root: "./workspace",
		},
		Model: ModelConfig{
			Provider:    "zai",
			Name:        "GLM-4.7",
			Temperature: 0.2,
		},
		Providers: ProvidersConfig{
			OpenAI: ProviderEndpointConfig{
				BaseURL:   "https://api.openai.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
			},
			OpenRouter: ProviderEndpointConfig{
				BaseURL:   "https://openrouter.ai/api/v1",
				APIKeyEnv: "OPENROUTER_API_KEY",
			},
			Requesty: ProviderEndpointConfig{
				BaseURL:   "https://router.requesty.ai/v1",
				APIKeyEnv: "REQUESTY_API_KEY",
			},
			ZAI: ProviderEndpointConfig{
				BaseURL:   "https://api.z.ai/api/coding/paas/v4",
				APIKeyEnv: "ZAI_API_KEY",
			},
			Generic: ProviderEndpointConfig{
				BaseURL:   "",
				APIKeyEnv: "OPENAI_COMPAT_API_KEY",
			},
		},
		Chat: ChatConfig{
			Enabled:         true,
			DefaultAgentID:  "default",
			RateLimitPerMin: 20,
		},
		Discord: DiscordConfig{
			Enabled:         false,
			TokenEnv:        "DISCORD_BOT_TOKEN",
			DefaultAgentID:  "default",
			CommandPrefix:   "!ask",
			RateLimitPerMin: 20,
		},
		Secrets: SecretsConfig{
			StoreFile:     ".openclawssy/secrets.enc",
			MasterKeyFile: ".openclawssy/master.key",
		},
	}
}

func (c *Config) ApplyDefaults() {
	d := Default()
	if c.Sandbox.Provider == "" {
		c.Sandbox.Provider = d.Sandbox.Provider
	}
	if c.Server.BindAddress == "" {
		c.Server.BindAddress = d.Server.BindAddress
	}
	if c.Server.Port == 0 {
		c.Server.Port = d.Server.Port
	}
	if c.Workspace.Root == "" {
		c.Workspace.Root = d.Workspace.Root
	}
	if c.Server.TLSCertFile == "" {
		c.Server.TLSCertFile = d.Server.TLSCertFile
	}
	if c.Server.TLSKeyFile == "" {
		c.Server.TLSKeyFile = d.Server.TLSKeyFile
	}
	if c.Model.Provider == "" {
		c.Model.Provider = d.Model.Provider
	}
	if c.Model.Name == "" {
		c.Model.Name = d.Model.Name
	}
	if c.Chat.DefaultAgentID == "" {
		c.Chat.DefaultAgentID = d.Chat.DefaultAgentID
	}
	if c.Chat.RateLimitPerMin == 0 {
		c.Chat.RateLimitPerMin = d.Chat.RateLimitPerMin
	}
	if c.Discord.TokenEnv == "" {
		c.Discord.TokenEnv = d.Discord.TokenEnv
	}
	if c.Discord.DefaultAgentID == "" {
		c.Discord.DefaultAgentID = d.Discord.DefaultAgentID
	}
	if c.Discord.CommandPrefix == "" {
		c.Discord.CommandPrefix = d.Discord.CommandPrefix
	}
	if c.Discord.RateLimitPerMin == 0 {
		c.Discord.RateLimitPerMin = d.Discord.RateLimitPerMin
	}
	if c.Secrets.StoreFile == "" {
		c.Secrets.StoreFile = d.Secrets.StoreFile
	}
	if c.Secrets.MasterKeyFile == "" {
		c.Secrets.MasterKeyFile = d.Secrets.MasterKeyFile
	}

	if c.Providers.OpenAI.BaseURL == "" {
		c.Providers.OpenAI = d.Providers.OpenAI
	}
	if c.Providers.OpenRouter.BaseURL == "" {
		c.Providers.OpenRouter = d.Providers.OpenRouter
	}
	if c.Providers.Requesty.BaseURL == "" {
		c.Providers.Requesty = d.Providers.Requesty
	}
	if c.Providers.ZAI.BaseURL == "" {
		c.Providers.ZAI = d.Providers.ZAI
	}
	if c.Providers.Generic.APIKeyEnv == "" && c.Providers.Generic.APIKey == "" {
		c.Providers.Generic.APIKeyEnv = d.Providers.Generic.APIKeyEnv
	}
}

func (c Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port out of range: %d", c.Server.Port)
	}

	host := c.Server.BindAddress
	if host == "" {
		return errors.New("server.bind_address is required")
	}
	if ip := net.ParseIP(host); ip == nil {
		return fmt.Errorf("server.bind_address must be an IP address: %q", host)
	}

	if c.Shell.EnableExec && !c.Sandbox.Active {
		return errors.New("shell.enable_exec cannot be true when sandbox.active is false")
	}
	if c.Sandbox.Active && c.Sandbox.Provider == "none" {
		return errors.New("sandbox.provider must not be 'none' when sandbox.active is true")
	}
	if !c.Sandbox.Active && c.Shell.EnableExec {
		return errors.New("shell execution requires active sandbox")
	}

	if strings.TrimSpace(c.Workspace.Root) == "" {
		return errors.New("workspace.root cannot be empty")
	}

	for _, d := range c.Network.AllowedDomains {
		d = strings.TrimSpace(d)
		if d == "" {
			return errors.New("network.allowed_domains cannot contain empty entries")
		}
		if strings.Contains(d, " ") {
			return fmt.Errorf("invalid allowed domain: %q", d)
		}
	}

	provider := strings.ToLower(strings.TrimSpace(c.Model.Provider))
	supported := map[string]bool{
		"openai": true, "openrouter": true, "requesty": true, "zai": true, "generic": true,
	}
	if !supported[provider] {
		return fmt.Errorf("unsupported model provider: %q", c.Model.Provider)
	}
	if strings.TrimSpace(c.Model.Name) == "" {
		return errors.New("model.name is required")
	}
	if c.Chat.RateLimitPerMin < 1 {
		return errors.New("chat.rate_limit_per_min must be >= 1")
	}
	if c.Discord.RateLimitPerMin < 1 {
		return errors.New("discord.rate_limit_per_min must be >= 1")
	}
	if c.Server.TLSEnabled {
		if strings.TrimSpace(c.Server.TLSCertFile) == "" || strings.TrimSpace(c.Server.TLSKeyFile) == "" {
			return errors.New("tls requires server.tls_cert_file and server.tls_key_file")
		}
	}
	if strings.TrimSpace(c.Secrets.StoreFile) == "" || strings.TrimSpace(c.Secrets.MasterKeyFile) == "" {
		return errors.New("secrets.store_file and secrets.master_key_file are required")
	}

	return nil
}

func LoadOrDefault(path string) (Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			return cfg, nil
		}
		return Config{}, err
	}
	return Load(path)
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	buf = append(buf, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return WriteAtomic(path, buf, 0o600)
}
