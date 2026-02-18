# Openclawssy Dev Plan (Actionable + Trackable)

Owner: ____________________  
Start date: _______________  
Target milestone: _________  
Repo: mojomast/openclawssy  
Status key: ⬜ Not started · 🟦 In progress · 🟩 Done · 🟥 Blocked

---

## 0) Quick Goals

**Goal A — “Bot can manage itself”**
- The agent can safely modify its workspace, its config, its scheduler, and (optionally) fetch remote info under a strict allowlist.

**Goal B — “Tooling is complete + policy enforced”**
- Every new tool is wired into: tool registry → policy/capability enforcement → tool parsing allowlist → docs → tests.

**Goal C — “Ops confidence”**
- CI/tests cover the critical flows, and there’s a minimal “smoke run” to verify wiring.

---

## 1) Current Observations (What to Fix)

### Missing / incomplete “self-management” tools
- No metadata/append operations yet (`fs.append`, metadata helpers)
- No network request tool even though `network` config exists
- No session / run trace / audit retrieval tools

### Reliability / safety gaps
- No first-class run cancellation / timeout controls
- Tool parsing and capability enforcement exist, but coverage should be validated end-to-end for new tools

### Docs/tests
- Documentation referenced by README may be missing/out of date
- Tests likely thin around policy enforcement, scheduler catch-up, error recovery

---

## 2) Execution Checklist (Do These First)

⬜ **E1: Confirm baseline build & run**
- [ ] `go test ./...`
- [ ] run a minimal local start (engine + dashboard if enabled)
- [ ] verify existing tools: `fs.read/fs.list/fs.write/fs.edit/code.search/time.now` and `shell.exec` gating

⬜ **E2: Map tool wiring**
- [ ] Identify where tools are registered (registry/builtins)
- [ ] Identify capability enforcement points (policy enforcer)
- [ ] Identify tool parsing allowlist usage (toolparse)
- [ ] Document the “add a tool” workflow in CONTRIBUTING

---

## 3) High Priority Features (Self-Management Tool Coverage)

### H1 — File deletion tool: `fs.delete`
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- Delete a file or directory within workspace.
- Must **refuse control-plane paths** (e.g., `.openclawssy`, secrets/master key files, SOUL/RULES if those are treated as control-plane).
- Must enforce workspace boundary checks consistent with other fs tools.

**Tasks**
- [x] Implement tool handler (args: `path`, optional `recursive`, optional `force`)
- [x] Add to tool registry + builtins
- [x] Add capability name mapping (canonicalization / alias if needed)
- [x] Add tests: boundary escape attempts, control-plane refusal, dir recursive, non-existent path behavior
- [x] Add docs + example prompts

**Acceptance**
- [x] Attempt to delete `../` path fails
- [x] Attempt to delete `.openclawssy/*` fails (or your chosen protected set)
- [x] Deletes normal workspace file successfully

---

### H2 — File move/rename tool: `fs.move` (or `fs.rename`)
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- Move/rename files/dirs inside workspace with safe constraints.

**Tasks**
- [x] Implement handler (args: `src`, `dst`, optional `overwrite`)
- [x] Enforce workspace bounds for both src/dst
- [x] Refuse control-plane paths
- [x] Tests: overwrite false/true, dst exists, src missing, path traversal
- [x] Docs

**Acceptance**
- [x] `fs.move a.txt b.txt` works
- [x] `fs.move ../../etc/passwd` is refused

---

### H3 — Config introspection & mutation: `config.get`, `config.set`
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- `config.get` returns a filtered view of config (no secrets).
- `config.set` updates *only safe mutable fields* and persists with existing validation.

**Recommended safe-set table (initial)**
- `output.thinking_mode`, `output.max_thinking_chars`
- `chat.rate_limit_per_min`, `chat.global_rate_limit_per_min`
- `discord.rate_limit_per_min`, `discord.command_prefix` (if enabled)
- `engine.max_concurrent_runs`, `scheduler.max_concurrent_jobs`
- `network.enabled`, `network.allowed_domains`, `network.allow_localhosts`
- `shell.enable_exec` (but only if sandbox.active == true)

**Tasks**
- [x] Add config tool module with whitelist of fields
- [x] Use existing `Validate` and atomic write logic when persisting
- [x] Ensure `config.get` redacts secrets/tokens/API keys
- [x] Tests: invalid set rejected, valid set applied, persisted, and reloaded
- [x] Docs: schema + examples

**Acceptance**
- [x] `config.set` rejects out-of-range values
- [x] `config.get` never returns API keys / master key / secret store plaintext

---

### H4 — Secrets lifecycle: `secrets.get`, `secrets.set` (and optionally `secrets.list`)
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- Enable agents to read/write secrets by key.
- Enforce access control: at minimum, allow only specific agents or require capability grant.
- Redact secrets from logs/tool results unless explicitly requested.

**Tasks**
- [x] Identify existing encrypted secret store interface (dashboard uses it)
- [x] Implement tools:
  - `secrets.set { key, value }`
  - `secrets.get { key }` (returns value)
  - `secrets.list` (returns keys only)
- [x] Ensure audit logging does not leak secret values
- [x] Tests: set/get roundtrip, missing key, permission denied, redaction behavior
- [x] Docs: secure usage patterns

**Acceptance**
- [x] Tool events do not store secret plaintext
- [x] Capability enforcement works (unauthorized agent denied)

---

### H5 — Scheduler management tools
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- Let agent manage jobs programmatically.
- Minimum set:
  - `scheduler.list`
  - `scheduler.add`
  - `scheduler.remove`
  - `scheduler.pause` / `scheduler.resume` (or `enable/disable`)

**Tasks**
- [x] Identify scheduler store/struct model
- [x] Define job schema (cron, agent_id, prompt/payload, enabled, metadata)
- [x] Implement tools backed by scheduler store
- [x] Tests: add/list/remove, invalid cron, concurrency, persistence
- [x] Docs: examples and safe defaults

**Acceptance**
- [x] Jobs persist restart-to-restart
- [x] Invalid cron rejected with clear error

---

### H6 — Network request tool: `http.request` (or `net.fetch`)
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- Only available when `network.enabled == true`
- Strict domain allowlist (`network.allowed_domains`)
- Optional localhost if `network.allow_localhosts == true`
- Safe limits: timeouts, max response size, content-type controls

**Minimal API**
- args: `method`, `url`, optional `headers`, optional `body`, optional `timeout_ms`
- returns: `status`, `headers` (maybe subset), `body` truncated, `bytes_read`

**Tasks**
- [x] Implement allowlist hostname matching (exact + optionally subdomains)
- [x] Enforce scheme (`http/https` only)
- [x] Add response size cap (e.g. 1–5MB configurable)
- [x] Add to policy/capabilities
- [x] Tests: denied domain, allowed domain, redirect handling (suggest: re-check on redirect)
- [x] Docs: security warnings & configuration

**Acceptance**
- [x] Requests to non-allowed domains are denied
- [x] Redirects do not bypass allowlist

---

### H7 — Session management tools
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- `session.list`: enumerate active sessions (IDs, user/channel, last active)
- `session.close`: close a session by ID
- (Optional) `session.export`: export transcript to file in workspace

**Tasks**
- [x] Identify session store in engine/channel
- [x] Implement tools + tests
- [x] Docs

**Acceptance**
- [x] Closing session stops further messages routed to it

---

### H8 — Run trace / audit tools
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- `run.list` / `run.get` to retrieve recent run summaries and full traces
- Pagination and size limits required

**Tasks**
- [x] Locate trace storage (dashboard already fetches these)
- [x] Implement tools that return structured summaries
- [x] Tests: pagination, missing run id
- [x] Docs

**Acceptance**
- [x] Large traces are truncated/paginated without crashing

---

### H9 — Cancellation & timeouts
Status: 🟩  
Owner: _______  
PR: _______  

**Definition**
- Add a way to cancel a run or tool invocation
- Add per-tool and global execution timeout config

**Tasks**
- [x] Add context cancellation wiring through runner → tool execution
- [x] Add `run.cancel` tool or channel command
- [x] Enforce timeouts on `shell.exec` and `http.request`
- [x] Tests: cancellation stops long-running tool
- [x] Docs: how to cancel

**Acceptance**
- [x] Long shell command is terminated on cancel

---

## 4) Medium Priority Enhancements

### M1 — Multi-agent management tools
Status: 🟩  
Tasks
- [x] `agent.list`, `agent.create`, `agent.switch`
- [x] Per-agent capability grants + separate rules/soul docs (or equivalent)
- [x] Tests

### M2 — Better editing ergonomics
Status: 🟩  
Tasks
- [x] `fs.append` tool
- [x] Diff/patch support in `fs.edit` (unified diff)
- [x] Tests + docs

### M3 — Policy management tools
Status: 🟩  
Tasks
- [x] `policy.list` / `policy.grant` / `policy.revoke`
- [x] Enforce audit + require elevated capability

### M4 — Metrics
Status: 🟩  
Tasks
- [x] Track tool durations/errors
- [x] `metrics.get` tool and/or dashboard view

---

## 5) Documentation & Quality

### D1 — Documentation audit
Status: 🟩  
Tasks
- [x] Ensure all referenced docs exist (SOUL/RULES/README references)
- [x] Add “Tool catalog” page: names, args schema, examples, safety rules
- [x] Add “How to add a tool” contributor guide

### D2 — Test coverage & CI
Status: ⬜  
Tasks
- [ ] Add integration tests for: policy enforcement, scheduler persistence, toolparse parsing, sandbox gating
- [ ] Add a “smoke workflow” GitHub Action: `go test ./...` + basic lint
- [ ] Add fuzz tests for toolparse JSON repair/parsing

---

## 6) Definition of Done (for each PR)

- [ ] Tool is registered and discoverable in registry
- [ ] Capability enforcement blocks unauthorized usage
- [ ] Toolparse accepts canonical name + aliases (if any)
- [ ] Unit tests added (happy path + abuse cases)
- [ ] Docs updated (tool schema + examples)
- [ ] No sensitive data leaked in logs/tool results
- [ ] Verified via local smoke run

---

## 7) Progress Tracker (Rolling)

| Week | Focus | Owner | Planned | Done | Notes |
|------|-------|-------|---------|------|------|
| W__  | H1–H2 |       | H1,H2   | H1,H2| Added `fs.delete` + `fs.move` with policy/control-plane enforcement, parser wiring, tests, and docs update |
| W__  | H3–H4 |       | H3,H4   | H3,H4| Added `config.get`/`config.set` plus `secrets.get`/`secrets.set`/`secrets.list`, with audit-safe redaction and capability tests |
| W__  | H5–H6 |       | H5,H6   | H5,H6| Added scheduler lifecycle tools and `http.request` (`net.fetch` alias) with network allowlist/redirect enforcement, response caps, parser/runtime wiring, tests, and docs updates |
| W__  | H7–H8 |       | H7,H8   | H7,H8| Added `session.list`/`session.close`, close-state metadata, routing guard to avoid reusing closed sessions, parser/runtime wiring, tests, and docs updates; Added `run.list`/`run.get` run trace tools with filtering, pagination, and capability gating |
| W__  | H9    |       | H9      | H9   | Added `run.cancel` tool with RunTracker for context-based cancellation; Added shell.exec timeout_ms support; Added timeout config fields to ShellConfig and EngineConfig; Full test coverage and documentation updates |
| W__  | M1    |       | M1      | M1   | Added `agent.list`/`agent.create`/`agent.switch` with strict `agent_id` validation, config default switching for chat/discord, runtime/parser/model wiring, tests, and docs updates |
| W__  | M2    |       | M2      | M2   | Added `fs.append` plus `fs.edit` unified-diff patch mode, runtime arg normalization (`unified_diff` alias), guardrail tests, and docs updates |
| W__  | M3    |       | M3      | M3   | Added `policy.list`/`policy.grant`/`policy.revoke` with persisted grants file support, live enforcer updates, admin capability gating, runtime/parser/model wiring, tests, and docs updates |
| W__  | M4    |       | M4      | M4   | Added per-tool duration capture in run traces and `metrics.get` aggregation (calls/errors/latency by tool with filters/pagination), plus capability tests and docs updates |
| W__  | D1    |       | D1      | D1   | Added `docs/TOOL_CATALOG.md`, `CONTRIBUTING.md` tool workflow guidance, and refreshed README docs index |

---

## 8) Notes / Open Questions

- Protected paths: confirm exact “control-plane” set (e.g., `.openclawssy/*`, config file, secrets store, master key, agent system prompts)
- Decide whether secrets should ever be returned as plaintext (default: yes for `secrets.get`, but never logged)
- Decide whether network tool should support redirects (recommended: yes, but re-check allowlist on every hop)

---
