# Project Status

_Ussyverse status board for operators and contributors._

## Current state

Openclawssy is a **prototype in development**.

What works now:
- CLI setup and run loop
- tool registry with policy gates
- sandbox-gated `shell.exec`
- scheduler queueing runs
- HTTP API + dashboard
- Discord bridge queueing runs
- encrypted secret ingestion
- persistent chat sessions (`/new`, `/resume`, `/chats`) with session history
- multi-tool parsing and normalized tool call IDs across runs
- repeated identical tool calls reuse successful cached results within a run
- per-tool execution summaries in trace/dashboard/Discord output
- long-context handling with compaction around 80 percent budget
- model response cap (`model.max_tokens`, default 20000)
- dashboard chat layout controls (resizable chat, collapsible panes, focus mode)
- long-running tool defaults (`120` iterations, `900s` per tool call) for heavy shell workflows
- staged failure recovery (after 2 failures, force error-recovery mode; after 3 additional failures, ask user with attempted commands/errors/outputs, including intermittent failure loops)
- dashboard chat auto-progress updates for long runs (elapsed time + completed tool calls + latest summary)
- chat queue API now returns `session_id` for queued runs so clients can stay attached to session context

What is not production-ready:
- compatibility and schema stability
- full authn/authz model for multi-tenant use
- external security review
- complete observability and disaster recovery

Known open issue in test suite:
- Full `go test ./...` currently reports one existing unrelated chat allowlist failure:
  `internal/channels/chat` -> `TestAllowlist_EmptyUsersDenyByDefault`

## Recommendation

Do not deploy this to production.
Use only local/dev environments with test credentials.

Ussyverse maturity label today: **Prototype / Builder Preview**.
