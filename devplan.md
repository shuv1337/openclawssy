Openclawssy DevPlan v0.2

Theme: Make chat reliable and transparent (better parsing, better tool calls, optional “thinking” visibility), then harden the rest of the prototype.

How to use this plan

Treat each numbered item as a small PR (ideally one concern per PR).

Every PR must include:

✅ tests (unit/integration as appropriate)

✅ a short entry in Progress Log

✅ updates to this checklist (mark [x])

Prefer contract-first changes: update docs/specs/CONTRACTS.md when interfaces change.

Progress Log

(Append one entry per merged PR)

2026-02-17 — PR###: Completed M1.1 by wiring RunInput metadata/history/allowed-tools/timeout into all Runner model requests (including finalize-from-tool-results) and added unit tests asserting request passthrough.

2026-02-17 — PR###: Completed M1.2 by making OnToolCall callback errors deterministic log+continue, capturing callback failures on tool-call records, and surfacing them in run trace + audit events with tests proving callback failures do not abort runs.

2026-02-17 — PR###: Completed M1.3 by tightening tool allowlist enforcement at parse-time and exec-time (including alias canonicalization like bash.exec -> shell.exec), improving disallowed-tool diagnostics, and adding tests for parser/runtime/policy rejection behavior.

2026-02-17 — PR###: Completed M2.1 by adding a unified `ParseToolCalls` entrypoint with candidate/rejection diagnostics (fenced + inline + array support), routing runtime model parsing through that parser, forwarding diagnostics into trace extraction records, and adding parser/runtime tests for aliasing and max-call capping.

2026-02-17 — PR###: Completed M2.2 by adding minimal safe JSON repair in the unified tool parser (trailing-comma stripping, fence/commentary tolerance), preserving strict allowlist/schema enforcement, and improving invalid-JSON diagnostics with tests for repaired and unrepaired cases.

2026-02-17 — PR###: Completed M2.3 by enforcing strict tool-call object schema (`tool_name` string + `arguments` object), rejecting missing/non-object arguments with explicit reasons, and asserting deterministic generated IDs for calls missing `id`.

2026-02-17 — PR###: Completed M3.1 by replacing blind think-tag stripping with runtime `ExtractThinking` extraction (`<think>`, `<analysis>`, `<!-- THINK -->`), integrating provider fallback tool-call parsing through extracted visible text, and adding graceful ambiguity handling + unit tests.

2026-02-17 — PR###: Completed M3.2/M3.3 by adding `output.thinking_mode` config defaulting to `on_error`, wiring `ask --thinking=` override through runtime output formatting, and persisting redacted/truncated thinking metadata into run trace/bundles/audit with tests for success, parse-failure, always-mode, and persistence behavior.

2026-02-17 — PR###: Completed M4.1 by adopting role=`tool` session replay in model context, normalizing stored tool payloads into truncated context-safe tool messages, enforcing per-message and total-history truncation caps, updating session policy in `docs/specs/CONTRACTS.md`, and adding runtime tests for inclusion/truncation behavior.

2026-02-17 — PR###: Completed M4.2 by adding cross-process chatstore file locking for session/message writes and active-pointer updates, plus a contention test that writes from multiple store instances and validates `messages.jsonl` remains fully valid JSONL.

YYYY-MM-DD — PR###: …

YYYY-MM-DD — PR###: …

Milestone 1 — Chat correctness: wire the runner properly (highest priority)

Goal: The “chat” system must actually use conversation history, allowed tools, tool timeouts, and tool-call hooks (some of these are currently present in structs but not wired through).

- [x] M1.1 Wire RunInput → ModelRequest completely

Problem this fixes: Chat/session history + allowed tools may be loaded but not actually used by the model loop.

Implementation tasks

 Update internal/agent/runner.go so Model.Generate() receives all required fields, not just Prompt/Message/ToolResults.

Pass through (at minimum):

AgentID, RunID

Messages (history)

SystemPrompt / Prompt (whichever your design uses)

AllowedTools

ToolTimeoutMS

 Ensure the model layer (internal/runtime/model.go) actually respects:

AllowedTools (tool parsing and validation)

Messages (conversation)

 Update/confirm tool loop behavior: tool calls should be generated from the model response based on the current prompt + message history, not only the latest user message.

Acceptance

 Add a test using a mock agent.Model that asserts it received:

the history messages

the allowed tools list

run metadata (agent/run IDs)

 Manual: Start a chat session, ask a follow-up question that requires context; verify the model sees prior context.

- [x] M1.2 Wire OnToolCall so chat can stream tool activity (or record it reliably)

Problem this fixes: The runtime prepares an OnToolCall callback, but tool calls may only be appended after the run, meaning chat can’t show incremental progress and some intended behavior is unwired.

Implementation tasks

 In internal/agent/runner.go, invoke input.OnToolCall(record) after each tool execution record is created (or at least after result is known).

 Ensure this callback error is handled deterministically:

If OnToolCall fails, decide: fail the run vs. log and continue.

Recommendation: log + continue, but record the failure in run trace/audit.

Acceptance

 Unit test: OnToolCall is invoked exactly once per tool call.

 Manual: In chat mode, trigger a tool call and confirm the session store shows tool events during the run (or immediately after each tool call).

- [x] M1.3 Make tool-allowlisting real (and test it)

Problem this fixes: “Allowed tools” should be enforced at two layers:

tool parsing (don’t accept calls to tools not allowed)

tool execution (policy enforcer denies execution)

Implementation tasks

 In tool parsing, reject tool calls not in AllowedTools (after alias canonicalization).

 Add a clear “tool not allowed” diagnostic (for trace + user-visible error mode).

Acceptance

 Unit test: model output containing shell.exec is rejected when not allowed.

 Manual: Disable exec; attempt to get model to run exec; verify refusal + helpful message.

Milestone 2 — Tool call parsing that doesn’t break under real chat output (highest priority)

Goal: Robustly parse tool calls from model output and make failures debuggable.

- [x] M2.1 Consolidate tool parsing into one module + add diagnostics

Implementation tasks

 Create (or expand) a single entrypoint in internal/toolparse/:

ParseToolCalls(text string, allowedTools []string) (calls []agent.ToolCall, diag ParseDiagnostics)

 Ensure diag includes:

candidate blocks found

rejected blocks with reasons (invalid JSON, missing fields, tool not allowed, etc.)

 Update internal/runtime/model.go to use this single parser (remove/avoid duplicate parsing implementations).

Acceptance

 Unit tests covering:

fenced ```json blocks

inline JSON objects

arrays of tool calls

“almost JSON” cases (see M2.2)

tool aliasing (e.g., bash.exec → shell.exec)

max tool calls per reply cap

- [x] M2.2 Add a “JSON repair” strategy (minimal + safe)

Goal: Recover from common LLM formatting mistakes without accepting dangerous garbage.

Implementation tasks

 Implement small repairs only (do not build a permissive parser that can misinterpret):

strip trailing commas

tolerate code-fence wrappers

trim leading/trailing commentary around a JSON object

 Never “guess” tool names or invent arguments.

 If repair fails, surface a debuggable parse failure:

store diagnostics in trace/artifacts

optionally show a user-facing message: “Tool call malformed; please retry.”

Acceptance

 Tests: trailing comma JSON gets repaired; truly invalid JSON fails with good diagnostics.

- [x] M2.3 Validate tool call schema strictly before execution

Implementation tasks

 Require shape: {"tool_name": "...", "arguments": {...}}

 Ensure arguments is an object (not string).

 If tool call id is missing, generate one deterministically (e.g., call_<runSeq>_<n>).

Acceptance

 Tests: missing args / wrong types are rejected with clear error.

Milestone 3 — “Thinking” visibility when pertinent (highest priority)

Goal: Don’t always strip thinking. Capture it reliably and optionally show it when it helps (e.g., on errors), without cluttering normal output.

- [x] M3.1 Extract thinking instead of blindly stripping it

Implementation tasks

 Replace stripThinkingTags() with an extractor:

ExtractThinking(text) -> { visibleText, thinkingText }

 Support common patterns:

<think>...</think>

<analysis>...</analysis>

(Optional) <!-- THINK --> ... <!-- /THINK -->

 Never delete content you can’t confidently classify; fall back to leaving text intact if parsing is ambiguous.

Acceptance

 Unit tests with:

nested tags (should not crash)

missing closing tags (should degrade gracefully)

mixed content where visible text must remain correct

- [x] M3.2 Add thinking_mode configuration and CLI flags

Recommended behavior: default to on_error.

Implementation tasks

 Add config setting (choose location consistent with your config schema), e.g.:

output.thinking_mode: "never" | "on_error" | "always"

 Add CLI override flag(s):

openclawssy ask --thinking=always

openclawssy serve respects config default

 Update HTTP/Discord/channel formatting to include thinking when enabled.

Acceptance

 Manual:

normal successful run: no thinking shown (default)

tool parse failure: thinking shown (default on_error)

always: thinking always shown

- [x] M3.3 Persist thinking in artifacts + trace

Implementation tasks

 Add fields to run trace snapshot and bundle metadata:

thinking (may be truncated)

thinking_present: true/false

 Ensure secrets redaction is applied to thinking before writing audit/log artifacts.

Acceptance

 Verify run bundles include thinking in the configured mode.

 Verify thinking is redacted.

Milestone 4 — Chat session quality improvements (still high priority)

Goal: Sessions feel coherent, tool results are represented correctly, and storage is resilient.

- [x] M4.1 Decide how tool results appear in conversation history

You have two good options:

Option A (recommended): Store tool results as role="tool" messages and include them in model context (with truncation).

Pros: structured; closer to modern tool-calling semantics

Cons: providers vary in “tool role” support

Option B: Convert tool events into short assistant summaries.

Pros: universal

Cons: less structured

Implementation tasks

 Pick A or B and document it in docs/specs/CONTRACTS.md.

 Ensure Engine.loadSessionMessages() includes the right message types for your choice.

 Add truncation limits (per-message and per-history) so tool output doesn’t explode context.

Acceptance

 Manual: Ask the agent to do a multi-step task; confirm it remembers tool outputs appropriately.

- [x] M4.2 Make chatstore safe across processes

Problem: Current chatstore uses a process-local mutex; multi-process writes can corrupt JSONL.

Implementation tasks

 Add a cross-process lock (directory/file lock) around:

appending to messages.jsonl

writing meta.json

writing _active pointers

 Keep dependencies minimal:

either implement flock with build tags

or use a tiny lock dependency (if acceptable)

Acceptance

 Add an integration test that spawns concurrent writers (or simulates contention) and verifies JSONL remains valid.

Milestone 5 — Tool execution reliability & UX (important, after chat correctness)

Goal: When tools fail, users get actionable output, and the system avoids runaway resource usage.

M5.1 Respect ToolTimeoutMS

Implementation tasks

 Wrap tool execution in context.WithTimeout using ToolTimeoutMS.

 Ensure timeout becomes a structured tool error (and is audited).

Acceptance

 Test: a tool that sleeps past timeout is cancelled and returns a timeout error.

M5.2 Improve tool error reporting + schema consistency

Implementation tasks

 Align tool errors with your documented canonical codes (and keep mapping stable).

 Ensure tool result payloads are machine-readable for chat rendering (especially the tool summaries).

Acceptance

 Tests: invalid input → tool.input_invalid (or your canonical equivalent)

 Manual: tool error shows a short summary + details in debug mode.

M5.3 Fix small correctness bugs in trace summarization

Implementation tasks

 Fix duplicated conditions in summarizeToolExecution() / intValue() logic.

 Add tests for summarization output format.

Acceptance

 Unit test: shell.exec fallback summarization works.

 Unit test: intValue() parses numbers correctly.

Milestone 6 — Security & sandboxing (important, but after chat/tool stability)

Goal: Don’t claim sandboxing that isn’t real; make “safe defaults” actually safe.

M6.1 Resolve Docker sandbox stub (implement or remove)

Implementation tasks

 Choose one:

Implement docker provider minimally (mount workspace, run command, capture stdout/stderr, apply resource limits)

Or remove docker from config defaults/docs until implemented

 Ensure shell.exec behavior is consistent:

if sandbox provider requested but unavailable → error clearly (sandbox.unavailable)

never silently fall back to unsafe execution

Acceptance

 Manual: sandbox=docker with docker missing yields a clear error and shell.exec remains disabled.

M6.2 Align server bind defaults with secure posture

Implementation tasks

 Ensure default bind is loopback unless explicitly overridden.

 Ensure dashboard/admin endpoints are clearly protected by token and disabled if desired.

Acceptance

 Manual: default config binds to 127.0.0.1 only.

Milestone 7 — Scheduler correctness vs. spec (later)

Goal: Match spec expectations (cron-like scheduling, persistence, restart behavior).

M7.1 Add cron expression support (or update spec to match reality)

Implementation tasks

 Either:

implement cron parsing (library or minimal parser)

or update docs/specs to explicitly state only @every + one-shot are supported

 Add persistence and restart correctness tests.

Milestone 8 — Performance & observability (later)

Goal: Make it easier to debug and cheaper to run.

M8.1 Improve audit logger performance safely

Implementation tasks

 Avoid open/close/fsync on every single event (buffer with periodic flush).

 Preserve durability requirements (flush on run end; bounded buffering).

“Done” Definition for v0.2

Mark v0.2 complete when all are true:

 Chat sessions actually influence model output (history is wired end-to-end).

 Tool call parsing is robust, tested, and diagnostics are stored in trace/artifacts.

 Tool allowlist is enforced at parse-time and exec-time.

 “Thinking” can be captured and shown in on_error mode (default) + optionally always.

 Tool calls can be streamed/recorded via OnToolCall.

 Chatstore is safe against cross-process corruption (locking + tests).

 Docker sandbox situation is resolved (implemented or removed), no misleading config.

Suggested PR Order (so progress stays visible)

PR1: Runner wiring (M1.1) + tests

PR2: OnToolCall wiring (M1.2) + tests

PR3: Consolidated tool parser + diagnostics scaffold (M2.1) + tests

PR4: Strict schema validation + allowlist enforcement (M2.3/M1.3)

PR5: Thinking extraction + thinking_mode (M3.1/M3.2)

PR6: Persist thinking in artifacts/trace + redaction (M3.3)

PR7: Session history tool message policy + truncation (M4.1)

PR8: Chatstore cross-process locking + tests (M4.2)

PR9+: Tool timeout enforcement + tool error shaping (M5.1/M5.2)

PR10+: Docker sandbox decision (M6.1) + secure defaults (M6.2)
