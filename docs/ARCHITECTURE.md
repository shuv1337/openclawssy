# Openclawssy Architecture (v0.4)

## Runtime Flow
- Channel adapters (CLI, HTTP, chat, Discord, scheduler) normalize requests into `runtime.ExecuteInput`.
- Engine acquires a global run slot (`engine.max_concurrent_runs`) before execution.
- Prompt assembly merges: system policy, agent files, optional chat/session context, and user input.
- Model response is parsed for tool calls and visible text in a bounded loop.
- Tool invocations pass through registry validation and policy checks before execution.
- Run bundle artifacts, trace, and audit events are persisted at completion.

## Runner Loop
```text
Input -> ExecuteWithInput
      -> acquire run slot
      -> build prompt + session context
      -> model turn
      -> parse output (text + tool calls)
      -> execute tools (0..n)
      -> repeat until terminal assistant output
      -> write run bundle + audit + release slot
```

## Parser and Thinking Extraction
- Parsing captures malformed tool snippets and normalized rejection reasons.
- `ParseDiagnostics` is returned when `thinking_mode=always` or parse failure occurred.
- Thinking text extraction is controlled by `output.thinking_mode` (or per-request override).
- Thinking text is truncated to `output.max_thinking_chars` before persistence/return.
- Redaction runs before diagnostics/thinking data is emitted to user-visible outputs.

## Scheduler Execution Path
- Scheduler store persists jobs and pause state on disk.
- Executor ticks at a fixed interval and computes due jobs (`@every` or RFC3339 one-shot).
- Startup behavior is controlled by `scheduler.catch_up`.
- Due jobs are dispatched through a bounded worker pool (`scheduler.max_concurrent_jobs`).
- Each scheduled execution enqueues a normal runtime run via channel/runtime integration.

## Key Persistence Surfaces
- Config: `.openclawssy/config.json` (atomic write + validation).
- Runs: `.openclawssy/agents/<agent>/runs/<run_id>/`.
- Audit: `.openclawssy/agents/<agent>/audit/YYYY-MM-DD.jsonl` (buffered writes, periodic flush, run-end sync).
- Chat sessions: persisted chat store files (session metadata + messages).
- Scheduler: persisted jobs/state file with backup/restore safeguards.
