**Purpose:** Address all remaining gaps identified in the latest code review.  This plan focuses on items not yet resolved after v0.2, prioritising reliability and security.  Use this checklist to guide incremental pull requests (PRs) and track progress.

## How to use this plan

- Every numbered item should be delivered as a **self‑contained PR**.  Keep PRs small and focused.  Each PR must update the **Progress Log** (append a dated entry), mark relevant tasks as complete (`[x]`), and include new/updated tests.  
- Always update docs/specs when interfaces or behaviours change.  
- After each milestone, increment the version (e.g., v0.3 → v0.4).

## Progress Log
*(append a new entry for each merged PR)*

- 2026-02-17 — PR###: Completed M1.1 by confirming/fixing trace condition regressions in `internal/runtime/trace.go` and adding focused unit coverage for shell fallback summarization and nil/empty `intValue()` parsing behavior.

- 2026-02-17 — PR###: Completed M1.2 by hardening `internal/policy/pathguard.go` for cross-platform Windows path forms (drive letters, UNC, mixed separators), adding explicit absolute-path detection, and expanding policy tests with POSIX + Windows-style traversal and absolute-path cases.

- 2026-02-17 — PR###: Completed M1.3 via the “remove Docker provider” path by restricting sandbox providers to `none|local` in config validation, removing docker support from sandbox provider selection and docs, and updating runtime/config/sandbox tests accordingly.

- 2026-02-17 — PR###: Completed M1.4 by extending chatstore file locking to reads (`messages.jsonl` and `_active` pointers), and adding contention tests that prove reads wait for lock release while concurrent writers/lock-holders are active.

- 2026-02-17 — PR###: Completed M2.1 using the spec-alignment path by documenting that scheduler supports only `@every <duration>` and one-shot RFC3339 schedules, and that cron expressions are rejected.

- 2026-02-17 — PR###: Completed M2.2 by documenting missed-job behavior explicitly, adding bounded concurrent scheduler execution with a worker pool, and adding restart/resume tests covering no-replay `@every` and one-shot disable-after-run behavior.

- 2026-02-17 — PR###: Completed M3.1 by validating full thinking extraction wiring end-to-end and aligning `output.thinking_mode` defaults/behavior to `never` by default with `on_error` and `always` overrides via config/CLI/runtime.

- 2026-02-17 — PR###: Completed M3.2 by confirming unified tool parsing + diagnostics entrypoints are used by runtime, surfaced in trace, and covered by malformed-call parser tests.

- 2026-02-17 — PR###: Completed M4.2 by expanding coverage for scheduler concurrency/restart behavior, chatstore lock-respecting reads, provider network failure handling, path/protected-write enforcement, and dashboard admin API endpoints (`status`, `config`, `secrets`, debug/chat).

- 2026-02-17 — PR###: Progressed M4.3 by aligning `CONFIG.md` and `CONTRACTS.md` with current runtime behavior (sandbox providers, scheduler schema/capabilities, thinking defaults) and rewriting `ACCEPTANCE.md` to a v0.2-aligned acceptance checklist.

- 2026-02-17 — PR###: Partially completed M4.4 by adding registry-level required argument type enforcement (`ToolSpec.ArgTypes`) so invalid types are rejected before handler execution, plus tests for typed validation failures.

- 2026-02-17 — PR###: Further completed M4.4 by adding a bounded global run-queue guard (default max in-flight runs), returning queue-full errors/HTTP 429 under overload, and adding pipeline/server tests for queue saturation behavior.

- 2026-02-17 — PR###: Completed the remaining M4.4 scheduler-cleanup item by removing unused `mode` and `notifyTarget` fields from scheduler job models/CLI wiring and aligning scheduler contracts/docs with the simplified job schema.

- 2026-02-17 — PR###: Completed M4.1 by adding authenticated scheduler admin HTTP endpoints (list/add/delete jobs plus global/per-job pause/resume control) and extending CLI cron commands with `delete` alias and `pause`/`resume` controls backed by persisted scheduler state.

---

## Milestone M1 — Core Correctness Fixes (highest priority)

### M1.1 Fix trace‑collector bugs

- [x] **Deduplicate conditions** – Correct duplicate conditions in `runtime/trace.go`:
  - `fallback != "" && fallback != ""` should check for actual fallback presence【268606176511821†L70-L73】.
  - `if s == "" || s == ""` in `intValue()` should properly detect empty string【268606176511821†L77-L79】.
- [x] Add unit tests for `summarizeToolExecution()` and `intValue()` to ensure correct fallback messages and integer parsing.

### M1.2 Robust path guard & cross‑platform support

- [x] Update `policy/pathguard.go` to correctly handle Windows drive letters and UNC paths; ensure path traversal detection works on Windows【222064012191688†L17-L29】.
- [x] Add tests covering POSIX and Windows path variants (simulate in tests via path strings; no need for actual Windows runtime).

### M1.3 Complete sandbox support

- [x] Decide: implement Docker provider or remove it.  If implementing:
  - Create a minimal `DockerProvider` that runs commands in a container with workspace mounted read‑write, capturing stdout/stderr/exit code.
  - Add basic resource limits (CPU/memory/time).  
  - Ensure `shell.exec` fails gracefully if Docker is unavailable.
- [x] If removing: remove `docker` as a valid provider in config and docs; update defaults accordingly.
- [x] Add tests verifying sandbox provider behaviour (exec allowed vs. denied).

### M1.4 Chat store concurrency safety

- [x] Implement cross‑process file locking for `messages.jsonl` and `_active` pointers (e.g., using OS‑level advisory locks or a lock file).  Use minimal dependencies.
- [x] Ensure reads respect locks to avoid reading partial writes.
- [x] Add integration tests simulating concurrent writes (could spawn goroutines writing concurrently).

## Milestone M2 — Scheduler & Cron (important)

### M2.1 Cron expression support or spec update

- [x] Either implement cron‑string parsing (use a minimal library or write a parser) or explicitly update `docs/specs/CONTRACTS.md` to state that only `@every` and one‑shot timestamps are supported.
- [x] If implementing, support at least standard 5‑field cron syntax and `@hourly`, `@daily`, etc. *(N/A — chose spec-update path instead of cron implementation.)*
- [x] Add `nextDue` logic for cron expressions and tests covering daily/hourly schedules, misfires, and time zones. *(N/A — chose spec-update path instead of cron implementation.)*

### M2.2 Missed‑job handling and concurrency

- [x] Decide on policy for missed jobs (e.g., run immediately on startup or skip).  Document this in the spec.
- [x] Allow multiple jobs to run concurrently with a worker pool (limit concurrency to avoid overload).
- [x] Add tests ensuring the scheduler persists and resumes jobs correctly after restart.

## Milestone M3 — Thinking & Diagnostics (important)

### M3.1 Replace `stripThinkingTags` with extraction

- [x] Implement `ExtractThinking` that returns `(visibleText, thinkingText)` instead of deleting thinking.  If extraction fails, leave original text intact.
- [x] Replace all calls to `stripThinkingTags` with the new extractor.  Store `thinkingText` in the trace and bundle (with redaction).  
- [x] Add config & CLI flag `output.thinking_mode` with values: `never` (default), `on_error`, `always`.  Use this to decide when to display thinking to users.

### M3.2 Unified tool‑parse diagnostics

- [x] Consolidate tool parsing into a single entrypoint that returns both accepted calls and diagnostic data (parsed name, arguments, acceptance reason).  Replace ad‑hoc parsing scattered across `model.go` and `toolparse`.
- [x] Expose diagnostics in the run trace so users can see why a tool call was rejected (e.g., invalid JSON, unsupported tool name, not allowed).
- [x] Add tests for various malformed tool call patterns.

## Milestone M4 — Remaining Gaps & Hardening (medium priority)

### M4.1 Cron & admin features in HTTP/CLI

- [x] Expose scheduler CRUD via HTTP API and CLI (add, list, delete jobs).  Require proper auth.
- [x] Add endpoints/commands to pause/resume scheduler globally and per job.

### M4.2 Test coverage expansion

- [x] Add tests for:
  - `scheduler` concurrency and persistence.
  - `chatstore` cross‑process locks.
  - Path traversal & protected file editing.
  - Provider error handling (simulate network failures/timeouts).
  - Dashboard actions (basic admin API functions).

### M4.3 Documentation alignment

- [x] Update `docs/specs/CONFIG.md`, `CONTRACTS.md`, `ACCEPTANCE.md` to reflect implemented features and removed/stubbed ones (e.g., update sandbox provider list, scheduler capabilities, thinking mode semantics).
- [x] Ensure README highlights **prototype** status, improved iteration limits, and new safety controls.

### M4.4 Miscellaneous fixes

- [x] Clean up unused fields like `Mode` and `NotifyTarget` in scheduler `Job` struct if not implemented.  Or implement their intended functions (e.g., different run contexts, notification channels).
- [x] Ensure tool registry enforces required argument types (not just presence).
- [x] Add per‑tool concurrency limit or global run queue size limit to avoid over‑loading the engine.

---

## Recommended PR Sequence

1. **PR‑1:** Fix trace bugs (M1.1) + tests.
2. **PR‑2:** Path guard cross‑platform + tests (M1.2).
3. **PR‑3:** Implement or remove Docker provider (M1.3).
4. **PR‑4:** Add file locks in chatstore (M1.4).
5. **PR‑5:** Cron support or spec update (M2.1) + tests.
6. **PR‑6:** Missed‑job handling & concurrency (M2.2).
7. **PR‑7:** Thinking extraction & config (M3.1).
8. **PR‑8:** Unified tool parse diagnostics (M3.2).
9. **PR‑9+:** Scheduler admin endpoints & CLI (M4.1), followed by expanded tests (M4.2) and docs alignment (M4.3).

If you need more granular guidance (e.g., specific file names and functions), feel free to ask.
EOF
