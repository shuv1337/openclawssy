# Openclawssy

Openclawssy is a minimal, secure-by-default AI agent runtime and gateway for builders who prefer discipline over chaos.

**The pitch:** an alternative to OpenClaw for the sensible, thinking gentleman `🎩🚬`.

Welcome to the **Ussyverse**: the tiny but principled corner of agent tooling where we ship calm systems, clear logs, and fewer regrettable surprises.

- Small surface area, explicit contracts, auditable artifacts.
- Security-first defaults (deny by default, loopback by default, network off by default).
- Real operational controls (scheduler, dashboard, Discord bridge, secret ingestion).

## Why Openclawssy

- Predictable architecture: plans, contracts, and artifacts before platform bloat.
- Operator control: agent cannot mutate core permissions, config, or secret store.
- Easy to self-host: one Go binary, simple config, practical defaults.
- Built for iteration: run bundles, audit trails, and strict capability gates.

## Ussyverse Principles

- No mystical auto-magic: contracts and artifacts first.
- No permission shape-shifting: control-plane files are protected.
- No bloat by default: only the tools required to get real work done.
- No vibes-only operations: logs, bundles, and reproducible runs.

## Highlights

- CLI: `init`, `setup`, `ask`, `run`, `serve`, `cron`, `doctor`
- Channels: local HTTP API + Discord bot bridge
- Dashboard: HTTPS admin panel for status/config/secrets ingestion
- Tooling: fs/code tools, `time.now`, sandbox-gated `shell.exec`
- Security: encrypted secret store, append-only audit logs, path/symlink guards
- Providers: OpenAI, OpenRouter, Requesty, ZAI, generic OpenAI-compatible endpoints

Ussyverse flavor, practical core.

## Prototype Warning

This project is a **PROTOTYPE IN ACTIVE DEVELOPMENT**.

- Expect breaking changes, rough edges, and incomplete hardening.
- Do not trust this with production data, regulated workloads, or high-value secrets.
- If you are not prepared to read code and debug behavior, you probably should not run this.
- Real talk: you probably should not even use OpenClaw in production yet either.

## Security Defaults
- Deny-by-default capabilities.
- Workspace-only writes.
- `shell.exec` disabled unless sandbox is active.
- Network disabled by default.
- HTTP API disabled by default; when enabled it should stay on loopback with token auth.
- Append-only JSONL audit logs with redaction.

## Quickstart

Prerequisites:
- Go 1.24+

Build and validate:

```bash
make fmt
make lint
make test
make build
```

Install globally if you prefer:

```bash
go install ./cmd/openclawssy
```

Minimal first run:

```bash
openclawssy setup
openclawssy doctor -v
openclawssy serve --token change-me
```

Then open:

- `https://127.0.0.1:8080/dashboard` (if TLS enabled)
- `http://127.0.0.1:8080/dashboard` (if TLS disabled)

## 5-Minute Tour

1. `openclawssy setup` to pick model/provider and optional Discord/TLS.
2. `openclawssy doctor -v` to verify readiness.
3. `openclawssy ask -agent default -message "hello"` for a synchronous run.
4. `openclawssy run -agent default -message '/tool time.now {}'` for tool flow.
5. `openclawssy serve --token change-me` and monitor in `/dashboard`.

Expected CLI commands for the `openclawssy` binary:

```bash
openclawssy init
openclawssy setup
openclawssy ask "hello"
openclawssy run --agent default --message "summarize changes"
openclawssy run --agent default --message-file ./prompt.txt
openclawssy serve --addr 127.0.0.1:8787 --token local-dev-token
openclawssy cron add --agent default --schedule "@every 1h" --message "status report"
openclawssy doctor
```

## Guided Setup (Openclaw-style)

Run the wizard:

```bash
openclawssy setup
```

The wizard can:
- choose provider/model
- store provider API key in encrypted secret store
- enable HTTPS dashboard (self-signed cert generation)
- enable Discord bot bridge and store Discord token

Wizard tip: use `openclawssy setup --force` to regenerate defaults.

## Provider and Model Setup

1) Initialize files and default config:

```bash
openclawssy init -agent default
```

2) Select provider/model in `.openclawssy/config.json`:

```json
{
  "model": {
    "provider": "openrouter",
    "name": "openai/gpt-4o-mini"
  }
}
```

3) Set API key env var for the selected provider:

```bash
export OPENROUTER_API_KEY=...
```

Supported providers:
- `openai` (`OPENAI_API_KEY`)
- `openrouter` (`OPENROUTER_API_KEY`)
- `requesty` (`REQUESTY_API_KEY`)
- `zai` (`ZAI_API_KEY`)
- `generic` (`OPENAI_COMPAT_API_KEY`, set `providers.generic.base_url`)

Secret-store key names (write-only ingestion):
- `provider/<provider>/api_key`
- `discord/bot_token`

Check setup:

```bash
openclawssy doctor -v
```

Provider key precedence:
1. encrypted secret store key `provider/<provider>/api_key`
2. provider env var (`OPENAI_API_KEY`, `OPENROUTER_API_KEY`, etc.)
3. `providers.<provider>.api_key` in config (discouraged)

## Shell Exec and Sandbox

`shell.exec` stays disabled unless all are true:
- `sandbox.active=true`
- `sandbox.provider=local|docker`
- `shell.enable_exec=true`

Example tool call through runner:

```bash
openclawssy run -agent default -message '/tool shell.exec {"command":"pwd"}'
```

Time utility example:

```bash
openclawssy run -agent default -message '/tool time.now {}'
```

## Run Bundle Format (v1)

Every successful run writes:
- `runs/<run-id>/input.json`
- `runs/<run-id>/prompt.md`
- `runs/<run-id>/toolcalls.jsonl`
- `runs/<run-id>/output.md`
- `runs/<run-id>/meta.json` (`bundle_version: 1`)

This artifact-first layout is intentional: every run should be reproducible, inspectable, and debuggable.

## HTTP and Chat Queue

- HTTP run API: `POST /v1/runs`, `GET /v1/runs/{id}`
- Chat bridge API: `POST /v1/chat/messages`
- Both require bearer token.
- Chat queue uses allowlist + rate limit from `chat.*` config.

## Discord Bot

- Enable `discord.enabled=true` in config.
- Provide token via `DISCORD_BOT_TOKEN`, `discord.token`, or secret store `discord/bot_token`.
- Set channel/user/guild allowlists in `discord.*` config fields.
- Messages with prefix `!ask` (configurable) are queued as runs.

Example:

```text
!ask summarize latest run failures
```

Discord token precedence:
1. secret store key `discord/bot_token`
2. `DISCORD_BOT_TOKEN`
3. `discord.token` in config (discouraged)

## Dashboard over HTTPS

- Set `server.tls_enabled=true` and provide `server.tls_cert_file` + `server.tls_key_file`.
- Start server with token and open `https://<bind>:<port>/dashboard`.
- Dashboard includes:
  - run monitoring
  - config management (providers/models)
  - one-way secret ingestion + secret key listing

Admin API endpoints behind bearer auth:
- `GET /api/admin/status`
- `GET /api/admin/config`
- `POST /api/admin/config`
- `GET /api/admin/secrets`
- `POST /api/admin/secrets`

Security note: secret values are write-only at API/UI layer; only secret keys are listed.

## Production Disclaimer

If you are evaluating this project for production, please don't yet.

- Use an isolated host.
- Use test credentials only.
- Assume data loss is possible.
- Track changes between commits before every upgrade.

If you need battle-tested enterprise controls today, this is not that product yet.

## Project Layout
- `cmd/openclawssy/` - CLI entrypoint and command wiring.
- `internal/` - runtime modules (agent, tools, policy, scheduler, channels, sandbox).
- `docs/specs/` - contracts, acceptance checklist, and config invariants.
- `docs/security/` - threat model and abuse-case expectations.
- `.github/workflows/ci.yml` - fmt, vet, test, build CI checks.

## Docs
- `docs/GETTING_STARTED.md`
- `docs/PROJECT_STATUS.md`
- `docs/specs/CONTRACTS.md`
- `docs/specs/ACCEPTANCE.md`
- `docs/specs/CONFIG.md`
- `docs/security/THREAT_MODEL.md`
