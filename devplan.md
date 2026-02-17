Openclawssy DevPlan v0.4

Focus: Finish remaining gaps after thinking-mode + parser improvements.
Theme: Stability, visibility, scheduling, connectors, tests, and documentation alignment.

How To Use This Plan

Each numbered item = one focused PR

Every PR must:

✅ Add or update tests

✅ Update docs if behavior changes

✅ Append entry to Progress Log

✅ Mark checklist items complete

Keep PRs small and reviewable.

Progress Log

(Append entries here as work lands)

2026-02-17 — PR###: Landed runtime/config/channel hardening pass: per-request `thinking_mode` for HTTP/chat/Discord, bounded thinking output (`output.max_thinking_chars`), parse diagnostics exposure in run results, scheduler catch-up + worker concurrency controls, engine run concurrency cap, and shell allowlist wiring with expanded validation tests.

2026-02-17 — PR###: Updated docs/specs for new config/runtime contracts (`CONFIG.md`, `CONTRACTS.md`, `ACCEPTANCE.md`) and added architecture reference (`ARCHITECTURE.md`).

2026-02-17 — PR###: Added connector rate-limit hardening (global + sender cooldown errors, structured HTTP `chat.rate_limited` response, Discord cooldown messaging) and completed audit logger buffered flush policy (periodic + run-end sync) with tests.

2026-02-17 — PR###: Added HTTP run listing pagination/filtering (`GET /v1/runs`) and dashboard chat-session pagination controls with tests and contracts updates.

2026-02-17 — PR###: Completed shell allowlist wiring in runtime (`shell.allowed_commands` now enforced on live `shell.exec` calls), plus README/spec cleanup (removed Dockerfile examples, documented concurrency/rate-limit behavior).

2026-02-17 — PR###: Expanded runtime+scheduler test coverage with parser/helper/compaction and scheduler lifecycle edge-case tests; coverage now meets target (`runtime=85.0%`, `scheduler=88.6%`).

2026-02-17 — PR###: Aligned acceptance checklist with shipped scheduler admin controls, CLI pause/resume support, and audit buffering completion.

YYYY-MM-DD — PR###: …

YYYY-MM-DD — PR###: …

Milestone 1 — Thinking & Diagnostics Completion (High Priority)

The core thinking feature exists but is not fully surfaced or hardened.

M1.1 Expose thinking-mode in HTTP & Discord

Problem: CLI supports -thinking, but HTTP and Discord do not.

Tasks

 Extend HTTP RunRequest / ChatRequest struct to include:

thinking_mode (optional override)

 Validate and normalize via config.NormalizeThinkingMode

 Pass override into engine RunInput

 Extend Discord command syntax:

/ask thinking=always

 Ensure invalid values return structured error

Acceptance

 HTTP call with thinking_mode=always shows thinking

 Discord command override works

 Tests for HTTP handler validation

M1.2 Add thinking length limit

Problem: Thinking may be unbounded and bloat responses/artifacts.

Tasks

 Add config:

output.max_thinking_chars (default: 4000)

 Truncate thinking before:

Including in visible output

Writing to artifacts

 Preserve ThinkingPresent=true even if truncated

Acceptance

 Unit test verifying truncation

 Long synthetic thinking input is capped

M1.3 Surface parse diagnostics in RunResult

Problem: Parser diagnostics are only in trace files.

Tasks

 Add ParseDiagnostics field to RunResult

 Include:

Rejected snippets (truncated)

Reason

 Only include when:

thinking_mode == always

OR parseFailure == true

 Redact secrets before returning

Acceptance

 Tool malformed JSON shows structured rejection reason

 Tests for diagnostic inclusion

Milestone 2 — Scheduler Completion (High Priority)

Scheduler still does not match spec expectations.

M2.1 Decide: Cron Support vs Spec Update

Choose one:

Option A: Implement Cron (Recommended)

 Add cron parser (minimal 5-field format)

 Support:

@hourly, @daily

Standard * * * * *

 Add NextDue() calculation

 Tests for cron schedule correctness

Option B: Update Spec

 Explicitly document only:

@every

RFC3339 one-shot

 Remove cron mentions from docs

M2.2 Missed Job Policy

Define and implement one:

 On startup:

Either execute missed runs immediately

OR mark skipped

 Add config:

scheduler.catch_up = true|false

 Add persistence test:

Simulate restart with past-due job

M2.3 Concurrency Control

 Add worker pool:

Config: scheduler.max_concurrent_jobs

 Prevent unbounded goroutine growth

 Add stress test with multiple jobs

Milestone 3 — Tool & Execution Hardening (Medium Priority)
M3.1 Global Run Concurrency Limit

Problem: Many runs could overwhelm system.

Tasks

 Add engine-level semaphore:

engine.max_concurrent_runs

 Reject new runs with structured error if exceeded

 Expose metric/log entry

M3.2 Improve Tool Parse Feedback

Current error: “Tool call malformed; please retry.”

Improve to:

 Include:

Snippet excerpt (truncated)

Reason

 Avoid leaking sensitive data

 Add test for helpful error message

M3.3 Strengthen Tool Argument Validation

Currently only checks object shape.

 Enforce required fields per tool

 Enforce argument type validation

 Return canonical error codes

 Add per-tool validation tests

Milestone 4 — Security & Isolation (Medium Priority)
M4.1 Improve Shell Safety

Current: only none and local providers.

Tasks

 Add config:

shell.allowed_commands (allowlist)

 Reject commands not matching prefix list

 Optional:

Disable shell entirely unless explicitly enabled

M4.2 Add Audit Logger Buffering

Current: file reopened frequently.

Tasks

 Maintain buffered writer

 Flush:

On run completion

Every N seconds

 Ensure crash-safe semantics

Milestone 5 — Connector Improvements (Medium Priority)
M5.1 Rate Limiter Enhancements

 Add global rate limit

 Add structured response when limited

 Add cooldown message

M5.2 HTTP Improvements

 Add pagination to:

Run listing

Chat listing

 Add filtering by status

M5.3 Discord Improvements

 Add:

/resume

/sessions

 Add better error display formatting

Milestone 6 — Test Coverage Expansion (High Priority)

Add missing test coverage for:

 Thinking-mode gating logic

 Thinking truncation

 Parse diagnostics

 Scheduler cron/missed jobs

 Cross-process chat locking

 Concurrency limits

 Shell allowlist enforcement

 HTTP validation paths

Target:

85% coverage on runtime + scheduler

Integration tests for:

Chat session multi-step flow

Tool failure recovery

Milestone 7 — Documentation Alignment (High Priority)
M7.1 Update All Specs

 Remove Docker references

 Clarify scheduler behavior

 Document thinking modes

 Document concurrency limits

 Update README security posture

M7.2 Add Architecture Diagram

 Update ARCHITECTURE.md

 Include:

Runner loop

Parser flow

Thinking extraction

Scheduler execution path

Suggested PR Order

PR1 — Thinking truncation (M1.2)

PR2 — Parse diagnostics surfaced (M1.3)

PR3 — HTTP thinking override (M1.1)

PR4 — Scheduler decision (M2.1)

PR5 — Scheduler missed-job handling (M2.2)

PR6 — Scheduler concurrency (M2.3)

PR7 — Tool argument validation (M3.3)

PR8 — Global concurrency limits (M3.1)

PR9 — Shell allowlist (M4.1)

PR10 — Audit buffering (M4.2)

PR11+ — Connector polish + docs alignment

v0.4 Completion Criteria

Mark v0.4 complete when:

 Thinking behavior is fully configurable across CLI + HTTP + Discord

 Thinking is safely truncated

 Parse diagnostics are visible to users

 Scheduler behavior is consistent with spec

 Concurrency limits exist at scheduler + engine level

 Shell execution is constrained

 Test coverage expanded

 Docs reflect real behavior
