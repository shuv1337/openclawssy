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
- Tooling: fs/code tools (`fs.read`, `fs.list`, `fs.write`, `fs.append`, `fs.delete`, `fs.move`, `fs.edit`), config tools (`config.get`, `config.set`), secret tools (`secrets.get`, `secrets.set`, `secrets.list`), scheduler tools (`scheduler.list`, `scheduler.add`, `scheduler.remove`, `scheduler.pause`, `scheduler.resume`), session tools (`session.list`, `session.close`), agent management tools (`agent.list`, `agent.create`, `agent.switch`), policy tools (`policy.list`, `policy.grant`, `policy.revoke`), run management tools (`run.list`, `run.get`, `run.cancel`), metrics tool (`metrics.get`), network tool (`http.request`), `time.now`, sandbox-gated `shell.exec`
- Security: encrypted secret store, append-only audit logs, path/symlink guards
- Providers: OpenAI, OpenRouter, Requesty, ZAI, generic OpenAI-compatible endpoints

Recent runtime hardening and UX upgrades:
- Chat sessions with `/new`, `/resume <session>`, `/chats`, persisted message history, and session listing.
- Multi-tool runs with normalized unique tool call IDs and repeated-call result reuse (no hard stop on benign duplicates).
- Long-running run defaults raised to support real workloads (`120` tool iterations, `900s` per-tool timeout) with no-progress loop guarding.
- Staged failure handling: after 2 consecutive tool failures, the model is forced into recovery mode; after 3 more failures (including intermittent failure patterns), the run asks the user for guidance with attempted commands, errors, and outputs.
- Structured canonical tool errors (`tool.not_found`, `tool.input_invalid`, `policy.denied`, `timeout`, `internal.error`) are emitted and persisted with machine-readable fields.
- Session context safety improvements: historical `tool` messages are replayed as normalized tool-result summaries with bounded per-message and total history budgets.
- Model response cap is enforced via `model.max_tokens` (1..20000, default 20000).
- Tool activity now includes concise summaries (for example `wrote N line(s) to file`) in trace, dashboard, and Discord updates.
- Thinking extraction is first-class (`ExtractThinking`) with `output.thinking_mode` controls (`never` default, `on_error`, `always`) and redacted thinking persistence in trace/artifacts/audit.
- Thinking output is bounded by `output.max_thinking_chars` and per-request overrides are supported in HTTP and Discord chat flows.
- Chatstore now uses cross-process locking for session writes and lock-respecting reads for `messages.jsonl` and active-session pointers.
- Scheduler now supports bounded concurrent execution (`scheduler.max_concurrent_jobs`), startup catch-up policy (`scheduler.catch_up`), and persisted global pause/resume plus per-job enable/disable controls.
- Run queue saturation is guarded by a global in-flight cap; overload returns HTTP `429` instead of unbounded queuing.
- Runtime execution is guarded by `engine.max_concurrent_runs` to reject excess concurrent runs early.
- Dashboard chat layout improvements: resizable chat panel, collapsible panes, focus-chat mode, persisted layout preferences, and continuous in-chat progress updates for long runs.
- Chat API queue responses now include `session_id` so clients can keep progress/tool timelines tied to the active session.
- Chat rate limiting supports sender + global policies with cooldown hints (`chat.rate_limited`).
- Run cancellation support via `run.cancel` tool and internal RunTracker for graceful termination.
- Shell execution timeout support via `timeout_ms` argument in `shell.exec`.

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
- Chat user allowlist is deny-by-default when empty.
- `shell.exec` disabled unless sandbox is active.
- `shell.allowed_commands` can enforce explicit command-prefix allowlists when shell exec is enabled.
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

Safe delete example:

```bash
openclawssy run --agent default --message '/tool fs.delete {"path":"scratch.txt","force":true}'

# safe rename/move example
openclawssy run --agent default --message '/tool fs.move {"src":"draft.txt","dst":"final.txt"}'

# append content without overwriting file
openclawssy run --agent default --message '/tool fs.append {"path":"notes.txt","content":"\nnew line"}'

# apply unified-diff patch to a file
openclawssy run --agent default --message '/tool fs.edit {"path":"notes.txt","patch":"@@ -1,1 +1,1 @@\n-old\n+new\n"}'

# safe config mutation example (allowlisted fields only)
openclawssy run --agent default --message '/tool config.set {"updates":{"output.thinking_mode":"on_error","engine.max_concurrent_runs":32}}'

# read redacted config
openclawssy run --agent default --message '/tool config.get {"field":"output.thinking_mode"}'

# secret lifecycle examples
openclawssy run --agent default --message '/tool secrets.set {"key":"provider/openrouter/api_key","value":"<token>"}'
openclawssy run --agent default --message '/tool secrets.get {"key":"provider/openrouter/api_key"}'
openclawssy run --agent default --message '/tool secrets.list {}'

# scheduler lifecycle examples
openclawssy run --agent default --message '/tool scheduler.add {"id":"job_1","schedule":"@every 1h","message":"status report"}'
openclawssy run --agent default --message '/tool scheduler.list {}'
openclawssy run --agent default --message '/tool scheduler.pause {"id":"job_1"}'

# session lifecycle examples
openclawssy run --agent default --message '/tool session.list {"agent_id":"default","limit":10}'
openclawssy run --agent default --message '/tool session.close {"session_id":"chat_123"}'

# agent management examples
openclawssy run --agent default --message '/tool agent.list {"limit":20,"offset":0}'
openclawssy run --agent default --message '/tool agent.create {"agent_id":"research"}'
openclawssy run --agent default --message '/tool agent.switch {"agent_id":"research","scope":"both","create_if_missing":true}'

# run management examples
openclawssy run --agent default --message '/tool run.list {"agent_id":"default","limit":20}'
openclawssy run --agent default --message '/tool run.get {"run_id":"run_1234567890"}'
openclawssy run --agent default --message '/tool run.cancel {"run_id":"run_1234567890"}'

# policy management examples (requires policy.admin capability)
openclawssy run --agent default --message '/tool policy.list {"agent_id":"default"}'
openclawssy run --agent default --message '/tool policy.grant {"agent_id":"default","capability":"metrics.get"}'

# tool metrics summary example
openclawssy run --agent default --message '/tool metrics.get {"agent_id":"default","limit":100}'

# shell execution with timeout (ms)
openclawssy run --agent default --message '/tool shell.exec {"command":"bash","args":["-lc","sleep 60"],"timeout_ms":10000}'

# allowlisted network request example (requires network.enabled + allowlist/localhost policy)
openclawssy run --agent default --message '/tool http.request {"method":"GET","url":"https://example.com"}'
```

`config.set` currently allows a safe subset of fields only:
- `output.thinking_mode`, `output.max_thinking_chars`
- `chat.rate_limit_per_min`, `chat.global_rate_limit_per_min`
- `discord.rate_limit_per_min`, `discord.command_prefix` (only when `discord.enabled=true`)
- `engine.max_concurrent_runs`, `scheduler.max_concurrent_jobs`
- `network.enabled`, `network.allowed_domains`, `network.allow_localhosts`
- `shell.enable_exec` (only when `sandbox.active=true`)

Dashboard chat quick controls:
- Use `New chat`, `Resume`, and `List chats` in the chat controls row.
- Use `Focus chat` to collapse non-chat sections temporarily.
- Drag the bottom edge of the chat history panel or use the height slider to resize.

Expected CLI commands for the `openclawssy` binary:

```bash
openclawssy init
openclawssy setup
openclawssy ask "hello"
openclawssy run --agent default --message "summarize changes"
openclawssy run --agent default --message-file ./prompt.txt
openclawssy serve --addr 127.0.0.1:8787 --token local-dev-token
openclawssy cron add --agent default --schedule "@every 1h" --message "status report"
openclawssy cron delete --id job_123
openclawssy cron pause
openclawssy cron resume --id job_123
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
- `sandbox.provider=local`
- `shell.enable_exec=true`

Supported sandbox providers: `none`, `local`.

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

- HTTP run API: `POST /v1/runs`, `GET /v1/runs`, `GET /v1/runs/{id}`
- Chat bridge API: `POST /v1/chat/messages`
- Both require bearer token.
- Global queue saturation returns `429` when in-flight run capacity is exhausted.
- Chat queue uses allowlist + rate limit from `chat.*` config.
- Run listing supports pagination/filtering (`limit`, `offset`, `status`).
- Chat rate-limit responses are structured (`chat.rate_limited`) and include `retry_after_seconds`.
- Chat queue response includes `session_id` for queued runs so clients can reattach to the same session timeline.
- Chat runs persist tool-call messages with metadata (`tool_name`, `tool_call_id`, `run_id`) so multi-step tool activity is visible in UI/channel outputs.

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
  - session-aware chat console and tool activity timeline
  - scheduler job management and pause/resume controls
  - config management (providers/models)
  - one-way secret ingestion + secret key listing

Admin API endpoints behind bearer auth:
- `GET /api/admin/status`
- `GET /api/admin/config`
- `POST /api/admin/config`
- `GET /api/admin/secrets`
- `POST /api/admin/secrets`
- `GET /api/admin/scheduler/jobs`
- `POST /api/admin/scheduler/jobs`
- `DELETE /api/admin/scheduler/jobs/{id}`
- `POST /api/admin/scheduler/control` (`pause|resume`, optional `job_id`)

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
- `CONTRIBUTING.md`
- `docs/GETTING_STARTED.md`
- `docs/PROJECT_STATUS.md`
- `docs/TOOL_CATALOG.md`
- `docs/specs/CONTRACTS.md`
- `docs/specs/ACCEPTANCE.md`
- `docs/specs/CONFIG.md`
- `docs/security/THREAT_MODEL.md`
- `mondaynight.md` (latest implementation handoff)
