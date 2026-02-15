# Openclawssy Acceptance Checklist (v0.1)

Source of truth: `devplan.md` phases 1-9.

## Phase 1 - Repo Bootstrap + Contracts
- [ ] `go.mod` exists with module `openclawssy` and Go 1.24.
- [ ] `Makefile` has `fmt`, `lint`, `test`, `build` targets.
- [ ] Contracts exist for tools, runs, audit events, scheduler jobs, and HTTP shapes.
- [ ] Security and config specs exist.

## Phase 2 - Config + Atomic Persistence
- [ ] Config parse + validation implemented with secure defaults.
- [ ] Atomic write flow implemented (`temp -> fsync -> rename`).
- [ ] Last-known-good recovery path implemented.
- [ ] Corruption and invalid-config tests pass.

## Phase 3 - Core Runner
- [ ] Run state machine transitions are implemented and tested.
- [ ] Prompt assembly is deterministic with bounded file sizes.
- [ ] Run bundle persists input, tool calls, output, timing, and usage metadata.
- [ ] `openclawssy ask "hello"` works with mock or real provider.

## Phase 4 - Tool System + Policy Layer
- [ ] Tool registry validates input/output contracts.
- [ ] Capability policy enforced per agent.
- [ ] Workspace-only write guard enforced.
- [ ] Path traversal and symlink escape attempts are denied.
- [ ] Core tools (`fs.read`, `fs.list`, `fs.write`, `fs.edit`, `code.search`) work.

## Phase 5 - Audit Logging + Redaction
- [ ] Required audit events are emitted (`run.*`, `tool.*`, `policy.denied`).
- [ ] Audit logs are append-only JSONL.
- [ ] Secret redaction rules remove sensitive material from logs.
- [ ] Abuse tests verify denial and redaction behavior.

## Phase 6 - Sandbox Providers + Exec Gating
- [ ] Sandbox interface implemented with lifecycle methods.
- [ ] `none` mode blocks `shell.exec`.
- [ ] `docker` (or equivalent) confines execution to workspace.
- [ ] Invariant enforced: no sandbox, no shell execution.

## Phase 7 - Scheduler (Cron-lite)
- [ ] Job create/list/remove works.
- [ ] Scheduler state persists across restarts.
- [ ] Triggered jobs generate run artifacts and audit events.
- [ ] Missed-job behavior is documented and tested.

## Phase 8 - Channels
- [ ] CLI commands exist: `init`, `ask`, `run`, `serve`, `cron`, `doctor`.
- [ ] HTTP API supports run create and run status endpoints.
- [ ] HTTP requires auth token and defaults to loopback binding.
- [ ] One chat connector enforces allowlist and rate limiting.

## Phase 9 - Hardening + Packaging + Docs
- [ ] CI runs fmt-check, vet, test, and build.
- [ ] Quickstart docs enable first successful run in under 10 minutes.
- [ ] Security deployment guidance is documented.
- [ ] Release artifacts and changelog workflow are documented.

## Definition of Done (v0.1)
- [ ] CLI workflow operational end-to-end.
- [ ] Policy + audit boundaries enforced and tested.
- [ ] Scheduler and sandbox invariants hold under abuse tests.
- [ ] At least one non-CLI channel works end-to-end.
