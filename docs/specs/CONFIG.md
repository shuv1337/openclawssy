# Openclawssy Config Spec (v0.1)

Default config path: `.openclawssy/config.json`

## Setup Flow (Guided)
1. Run `openclawssy init -agent default`.
2. Edit `.openclawssy/config.json` and set `model.provider` + `model.name`.
3. Set the matching API key env var.
4. Verify with `openclawssy doctor -v`.
5. Optional: enable sandbox + shell exec, HTTPS dashboard, Discord bridge.

Default shipping profile:
- `model.provider`: `zai`
- `model.name`: `GLM-4.7`
- `model.max_tokens`: `20000`

## Provider Support
- `openai` (OpenAI endpoint)
- `openrouter`
- `requesty`
- `zai` (ZAI coding-plan compatible OpenAI-style endpoint)
- `generic` (any OpenAI-compatible base URL)

Provider API key env defaults:
- `openai` -> `OPENAI_API_KEY`
- `openrouter` -> `OPENROUTER_API_KEY`
- `requesty` -> `REQUESTY_API_KEY`
- `zai` -> `ZAI_API_KEY`
- `generic` -> `OPENAI_COMPAT_API_KEY`

## Runtime Schema

```json
{
  "network": {
    "enabled": false,
    "allowed_domains": []
  },
  "shell": {
    "enable_exec": false
  },
  "sandbox": {
    "active": false,
    "provider": "none"
  },
  "server": {
    "bind_address": "127.0.0.1",
    "port": 8080,
    "tls_enabled": false,
    "tls_cert_file": ".openclawssy/certs/server.crt",
    "tls_key_file": ".openclawssy/certs/server.key",
    "dashboard_enabled": true
  },
  "output": {
    "thinking_mode": "never"
  },
  "workspace": {
    "root": "./workspace"
  },
  "model": {
    "provider": "openai",
    "name": "gpt-4o-mini",
    "temperature": 0.2,
    "max_tokens": 20000
  },
  "providers": {
    "openai": {
      "base_url": "https://api.openai.com/v1",
      "api_key_env": "OPENAI_API_KEY"
    },
    "openrouter": {
      "base_url": "https://openrouter.ai/api/v1",
      "api_key_env": "OPENROUTER_API_KEY"
    },
    "requesty": {
      "base_url": "https://router.requesty.ai/v1",
      "api_key_env": "REQUESTY_API_KEY"
    },
    "zai": {
      "base_url": "https://api.z.ai/api/coding/paas/v4",
      "api_key_env": "ZAI_API_KEY"
    },
    "generic": {
      "base_url": "",
      "api_key_env": "OPENAI_COMPAT_API_KEY"
    }
  },
  "chat": {
    "enabled": true,
    "default_agent_id": "default",
    "allow_users": [],
    "allow_rooms": [],
    "rate_limit_per_min": 20
  },
  "discord": {
    "enabled": false,
    "token_env": "DISCORD_BOT_TOKEN",
    "default_agent_id": "default",
    "allow_guilds": [],
    "allow_channels": [],
    "allow_users": [],
    "command_prefix": "!ask",
    "rate_limit_per_min": 20
  },
  "secrets": {
    "store_file": ".openclawssy/secrets.enc",
    "master_key_file": ".openclawssy/master.key"
  }
}
```

## Security Invariants
- Config is human-managed; agent tools do not get write access to `.openclawssy/`.
- Workspace write policy stays enforced after path and symlink resolution.
- `shell.exec` is enabled only when sandbox is active and provider is not `none`.
- Supported sandbox providers are `none` and `local`.
- HTTP APIs require bearer token.
- Chat queue accepts allowlisted senders only and enforces rate limits.
- Discord queue accepts allowlisted senders/channels/guilds and enforces rate limits.
- Secret values are write-only at API/UI surface; only key names are listed.
- Tool calls and run lifecycle events are always audited with redaction.

## Model Runtime Notes
- `model.max_tokens` is validated in the range `1..20000`.
- Runtime enforces this cap on provider requests.
- Long chat history is compacted by runtime before context exhaustion.

## Output Notes
- `output.thinking_mode` supports: `never`, `on_error`, `always`.
- Default is `never`.
- CLI `ask` supports per-call override: `openclawssy ask --thinking=always ...`.
