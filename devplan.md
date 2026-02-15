# Openclawssy DevPlan (Codex-ready, Parallel-Agent Build)
Version: v0.1 (2026-02-15)
Goal: Ship a lightweight, secure-by-default, self-hostable “agent gateway + runner” with **no bloat**, designed for easy deployment and easy agent building.

---

## 1) Mission
Openclawssy is a small, auditable, self-hosted **agent runtime + gateway** that:
- Receives messages (CLI first; HTTP + 1 chat channel later)
- Runs an agent loop with **capability-gated tools**
- Writes all state as **portable Markdown artifacts**
- Supports scheduling (cron-like) without becoming a giant platform

**Core vibe constraints**
- Plans before code (spec/plan artifacts drive execution)
- Everything important is an artifact (markdown + jsonl logs)
- Minimal dependencies, minimal tools, minimal attack surface
- Default: small box (restricted capabilities unless explicitly enabled)

---

## 2) Non-goals (Hard “No” List)
These are explicitly out-of-scope for v0.x:
- Any public “skills marketplace” or auto-install from registries
- Any runtime persona swapping hooks (no hidden instruction mutation)
- Any tool that allows the agent to modify its own permissions/config
- Any default binding to 0.0.0.0 without explicit opt-in + auth
- Any UI-first dashboard (CLI + logs first)

---

## 3) Success Criteria (What “Best Possible” Means)
### Functional
- Users can install and run Openclawssy in <10 minutes (single binary preferred).
- Users can define agents with:
  - instructions (read-only)
  - tool capabilities
  - workspace root
  - optional schedule
- Agent can do real work:
  - read/write/edit workspace files
  - run shell commands (only if sandboxed + enabled)
  - search code
- Scheduling can trigger runs and optionally deliver outputs.

### Non-functional
- Secure defaults (deny-by-default capability model).
- Reproducible behavior (deterministic prompt assembly; audit logs).
- Easy upgrades and low config-corruption risk (schema + atomic writes).

---

## 4) Security Invariants (Never Break These)
1) **Config & permissions are human-controlled only**
   - Agent cannot edit config, capability grants, or identity files.
2) **Workspace is the only writable area (by policy)**
   - Writes outside workspace are denied at the policy layer.
3) **Shell exec is OFF unless sandbox is ON**
   - If no sandbox provider is active, `shell.exec` tool must be disabled.
4) **Network is OFF by default**
   - If enabled, must be allowlisted (domains) or proxied.
5) **All tool calls are logged**
   - Append-only JSONL audit trail, with redaction rules for secrets.

Threat model to explicitly mitigate:
- Prompt injection → tool misuse
- Persistence via instruction/config mutation
- Path traversal / symlink escape
- Supply-chain “skills” abuse
- Remote abuse via unauthenticated HTTP endpoints

---

## 5) Product Shape (MVP Surface Area)
### Channels (in order)
1) CLI (`openclawssy run`, `openclawssy ask`)
2) Local HTTP API (loopback-only by default)
3) ONE chat connector (Discord OR Telegram) with allowlists

### Core tools (keep ≤ 8 in MVP)
- fs.read
- fs.list
- fs.write (workspace-only)
- fs.edit (workspace-only; patch-based)
- code.search (ripgrep)
- shell.exec (sandbox-required)
- time.now (utility)
- (optional) http.fetch (off by default; allowlist-only)

---

## 6) Architecture (Modules + Ownership Boundaries)
Language target: Go (single binary, small runtime surface)

### Packages
- `cmd/openclawssy/` — CLI entrypoints
- `internal/config/` — config schema, validation, atomic write
- `internal/agent/` — prompt assembly, run loop, state machine
- `internal/tools/` — tool registry + implementations
- `internal/policy/` — capability checks, path guards, redaction rules
- `internal/sandbox/` — sandbox providers (none/docker/bwrap/etc.)
- `internal/scheduler/` — cron-like scheduler + persistence
- `internal/channels/` — cli/http/(discord|telegram)
- `internal/audit/` — jsonl event log writer, session/run ids, redaction
- `internal/artifacts/` — markdown artifact writers/readers

### Artifact-first workspace layout
In repo root (or user-specified workspace):
- `.openclawssy/agents/<agentId>/`
  - `SOUL.md` (read-only instructions)
  - `RULES.md` (read-only guardrails)
  - `TOOLS.md` (tool notes)
  - `SPECPLAN.md` (read-only plan)
  - `DEVPLAN.md` (editable checklist for execution loop)
  - `HANDOFF.md` (agent-updated status + next steps)
  - `memory/` (optional, untrusted)
  - `audit/` (jsonl logs)
  - `runs/` (per-run bundles: inputs/outputs)
- `workspace/` (project files; writable root)

---

## 7) Parallel Agent Operating Model (How Codex Agents Collaborate)
You are building this with **multiple parallel agents**. Use this operating model:

### 7.1 Roles (Create one Codex agent per role)
- **A0 Architect**: writes specs, module boundaries, interfaces, acceptance tests
- **A1 Core Runner**: run loop + prompt assembly + run state machine
- **A2 Tools/Policy**: tool registry + fs tools + path/symlink guards + redaction
- **A3 Sandbox**: sandbox providers + “exec only if sandbox” enforcement
- **A4 Scheduler**: cron-like scheduler + persistence + tests
- **A5 Channels**: CLI UX + local HTTP API + one chat connector
- **A6 Release/CI**: build, lint, test, release packaging, docs
- **A7 Security**: threat model checks, secure defaults, abuse test cases

### 7.2 Workstream boundaries (avoid merge hell)
Each agent **owns** specific folders and must not touch others unless required:
- A1 owns `internal/agent/`, `internal/artifacts/`
- A2 owns `internal/tools/`, `internal/policy/`, `internal/audit/`
- A3 owns `internal/sandbox/`
- A4 owns `internal/scheduler/`
- A5 owns `internal/channels/`, CLI UX in `cmd/`
- A6 owns `.github/`, `Makefile`, `docs/`, release scripts
- A0 owns `docs/specs/`, contracts, and integration checklists
- A7 owns `docs/security/`, abuse tests, config hardening checks

### 7.3 Contracts first
Before implementing, A0 must create:
- `docs/specs/CONTRACTS.md`:
  - tool interface types
  - run lifecycle events
  - audit event schema
  - scheduler job schema
  - HTTP endpoint shapes (if any)
- `docs/specs/ACCEPTANCE.md`:
  - phase-by-phase acceptance tests

Workers implement against contracts; integrator merges only when contract tests pass.

### 7.4 Branching + PR rules
- Each agent works on a branch: `agent/<role>/<feature>`
- Every PR must include:
  - tests for new behavior
  - updated docs/spec if interface changes
  - “security impact” note (yes/no)
- Small PRs preferred (≤ ~500 LOC net when possible)

### 7.5 Integration cadence
- A0 (or A6) runs daily integration:
  - merges green PRs
  - resolves conflicts
  - updates `HANDOFF.md` with repo status
- No two agents implement the same interface at once.
  - If conflict: A0 updates contract; all agents rebase.

---

## 8) Development Phases (7–10 phases; 3–5 tasks each)
Each phase ends with:
- tests passing (`make test`)
- acceptance checklist updated
- artifacts updated (`SPECPLAN/DEVPLAN/HANDOFF`)

### Phase 1 — Repo Bootstrap + Contracts (Owner: A0 + A6)
1. Create repo skeleton + go module + base Makefile targets:
   - `make fmt`, `make lint`, `make test`, `make build`
2. Add initial docs:
   - `docs/specs/CONTRACTS.md`
   - `docs/specs/ACCEPTANCE.md`
   - `docs/security/THREAT_MODEL.md`
3. Add config schema draft:
   - `docs/specs/CONFIG.md` (fields, defaults, safety invariants)

**Acceptance**
- `make test` runs (even if minimal)
- Contracts exist for tools, runs, audit events, scheduler jobs

---

### Phase 2 — Config + Atomic Persistence (Owner: A6 + A2)
1. Implement config loader:
   - parse + validate schema
   - explicit defaults (secure-by-default)
2. Implement atomic writes:
   - write temp → fsync → rename
   - keep last-known-good backup
3. Add corruption tests:
   - interrupted write simulation
   - invalid config error messaging

**Acceptance**
- Config round-trip is stable
- Corruption tests prove recoverability

---

### Phase 3 — Core Runner (Owner: A1)
1. Implement run state machine:
   - input → assemble prompt → model call → tool calls → finalize
2. Implement prompt assembly:
   - deterministic ordering of bootstrap files
   - size limits per file
3. Implement run output bundle:
   - store input, tool calls, final output, timing, token usage (if available)

**Acceptance**
- `openclawssy ask "hello"` works (mock provider ok)
- Run artifacts written to `runs/<runId>/`

---

### Phase 4 — Tool System + Policy Layer (Owner: A2)
1. Implement tool registry:
   - tool schema, validation, standardized errors
2. Implement policy gates:
   - capabilities per agent
   - workspace-only write rules
   - absolute path rules
   - symlink escape detection
3. Implement core tools:
   - fs.read, fs.list, fs.write, fs.edit, code.search

**Acceptance**
- Path traversal attempts fail
- Symlink escape attempts fail
- Tool calls are audited (jsonl)

---

### Phase 5 — Audit Logging + Redaction (Owner: A2 + A7)
1. Implement audit event schema:
   - run.start, run.end, tool.call, tool.result, policy.denied
2. Implement secret redaction rules:
   - env var patterns, token-like strings (configurable)
3. Add “abuse tests”:
   - prompt injection tries to exfiltrate → logs show denial and redaction

**Acceptance**
- Audit logs are append-only JSONL
- Secrets not printed in cleartext in logs

---

### Phase 6 — Sandbox Providers + Exec Gating (Owner: A3 + A7)
1. Implement sandbox interface:
   - `Start(runCtx)`, `Exec(cmd)`, `Stop()`
2. Implement at least two modes:
   - `none` (no exec allowed)
   - `docker` (or other container-based provider)
3. Enforce invariant:
   - if sandbox is not active, `shell.exec` tool is disabled

**Acceptance**
- With sandbox=none → exec tool unavailable
- With sandbox=docker → exec works, confined to workspace mount

---

### Phase 7 — Scheduler (Cron-lite) (Owner: A4)
1. Implement job schema:
   - schedule, agentId, message, mode (isolated/main-like), notify target
2. Implement persistence:
   - stable storage (json + atomic writes OR sqlite)
3. Implement executor:
   - triggers run at times
   - writes job-run artifacts + audit events

**Acceptance**
- Add/list/remove jobs works
- Scheduler survives restart
- Missed-job behavior documented and tested

---

### Phase 8 — Channels (Owner: A5)
1. CLI UX polish:
   - `init`, `ask`, `run`, `serve`, `cron`, `doctor`
2. HTTP API (minimal):
   - POST message → starts run
   - GET run status → returns output + artifacts pointers
   - auth token required
   - bind loopback by default
3. Add ONE chat connector:
   - allowlist user/room ids
   - map to agentId
   - safe rate limiting

**Acceptance**
- Local HTTP requires token
- Chat connector ignores non-allowlisted senders
- End-to-end: message → run → response

---

### Phase 9 — Hardening + Packaging + Docs (Owner: A6 + A7)
1. Add CI:
   - lint, test, build
   - minimal SAST checks if available
2. Produce quickstart:
   - install, init, first agent, first run, first schedule
3. Add security docs:
   - safe deployment notes
   - “how to run in a small box”
4. Release artifacts:
   - binaries (or container), versioning, changelog

**Acceptance**
- New user can install + run in <10 minutes
- Security defaults clearly documented
- Reproducible builds (at least via CI)

---

## 9) Definition of Done (v0.1)
- CLI works: init/run/ask/serve/cron/doctor
- Tool system works with policy + audit
- Sandbox-gated exec works (docker provider ok)
- Scheduler works and survives restart
- One additional channel works (HTTP or Discord)
- No marketplace; no agent-controlled config mutation
- Tests cover policy boundaries and basic workflows

---

## 10) “Do Not Bloat” Guardrails (Enforced During Build)
- Max MVP tools: 8 (new tools require an ADR in `docs/adr/`)
- Max direct dependencies: keep small; justify each new dep
- No framework creep: prefer stdlib unless a real need is proven
- Every feature must ship with:
  - tests
  - audit behavior
  - security consideration note

---

## 11) Immediate Next Actions (Kickoff Checklist)
1) A0: write `docs/specs/CONTRACTS.md` + `docs/specs/ACCEPTANCE.md`
2) A6: bootstrap repo + Makefile + CI scaffold
3) A1/A2/A3/A4/A5 start implementation ONLY after contracts land
4) A7 writes abuse tests in parallel as contracts stabilize
