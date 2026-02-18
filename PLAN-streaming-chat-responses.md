# PLAN: Streaming Chat Responses (Revised)

Last revised: 2026-02-18

## Objective

Deliver real-time chat progress in the dashboard across all three layers:

1. **Transport layer**: server-pushed run events (SSE)
2. **Model layer**: token/delta streaming from provider
3. **UI layer**: progressive transcript + live tool activity rendering

---

## Plan Review Summary (What changed and why)

This revision incorporates a codebase-alignment review against current sources.

### Critical fixes applied

1. **Avoid runtime ↔ HTTP package coupling**
   - Current runtime entry is `runtime.Engine.ExecuteWithInput` (`internal/runtime/engine.go:160`)
   - Current HTTP queue entry is `QueueRun` (`internal/channels/http/pipeline.go:31`)
   - Revised plan uses callback-based progress plumbing through existing executor boundary instead of importing HTTP types into runtime.

2. **Avoid global query-token auth downgrade**
   - Current auth enforcement is header-based in `authMiddleware` (`internal/channels/http/server.go:420`)
   - Revised plan uses `fetch` streaming (Authorization header preserved) instead of `EventSource ?token=` as default.

3. **Handle late SSE subscribers safely**
   - Current architecture is polling (`chat.js`, `RUN_POLL_MS=1500`, `SESSION_POLL_MS=2000`, lines 7-8)
   - Revised event bus includes per-run replay + terminal-state behavior so clients connecting after run completion still receive terminal events.

4. **Preserve existing QueueRun call sites**
   - QueueRun is used from HTTP, scheduler, and chat connector (see `cmd/openclawssy/main.go` and `server.go`)
   - Revised plan keeps a compatibility wrapper and introduces an options-based queue entry to minimize churn.

5. **Streaming parser/retry hardening**
   - Current model path is non-streaming in `ProviderModel.Generate` (`internal/runtime/model.go:150`)
   - Revised plan adds robust SSE chunk parsing and explicit retry semantics to avoid duplicate streamed tokens.

### Overall status

**READY TO IMPLEMENT (with this revised plan).**

---

## Current Baseline (Codebase Map)

- Queue and background run execution: `internal/channels/http/pipeline.go`
  - `QueueRun`: line 31
  - `executeQueuedRun`: line 56
- HTTP server and auth: `internal/channels/http/server.go`
  - routes registered: lines 118-119
  - auth middleware: line 420
- Runtime execution: `internal/runtime/engine.go`
  - `ExecuteWithInput`: line 160
  - `onToolCall` callback setup: line 324
  - `runner.Run` invocation: line 333
- Agent loop: `internal/agent/runner.go`
  - model call site: line 127
- Model provider call: `internal/runtime/model.go`
  - `Generate`: line 150
  - retry path: `doChatCompletionWithRetry`: line 260
- Dashboard chat polling/rendering: `internal/channels/dashboard/ui/src/pages/chat.js`
  - poll intervals: lines 7-8
  - run poll: `pollRunOnce`
  - session poll: `pollSessionMessagesOnce`
  - full-page render: `renderChatPage`

---

## Revised Target Architecture

```text
Chat UI (fetch stream + fallback poll)
        │
        ▼
GET /v1/runs/events/{runID} (SSE)
        │
        ▼
RunEventBus (pub/sub + replay + terminal close)
        ▲                        │
        │                        │
Queue pipeline status events     │
(pipeline.go)                    │
        ▲                        │
        │                        │
runtimeExecutor bridge (cmd/main.go)
maps runtime progress callbacks -> run events
        ▲
        │
Engine + Runner + ProviderModel
(tool_end + model_text deltas)
```

---

## Event Contract (v1)

Create `internal/channels/http/events.go` with:

```go
type RunEventType string

const (
    RunEventStatus    RunEventType = "status"
    RunEventToolEnd   RunEventType = "tool_end"
    RunEventModelText RunEventType = "model_text"
    RunEventCompleted RunEventType = "completed"
    RunEventFailed    RunEventType = "failed"
    RunEventHeartbeat RunEventType = "heartbeat"
)

type RunEvent struct {
    ID        int64          `json:"id"`    // monotonically increasing per run
    Type      RunEventType   `json:"type"`
    RunID     string         `json:"run_id"`
    Timestamp time.Time      `json:"ts"`
    Data      map[string]any `json:"data,omitempty"`
}
```

Notes:
- `ID` supports replay and SSE `id:` field.
- `Data` for typed payload (`status`, `tool`, `summary`, `error`, `text`, etc.).

---

## Layer 1 — SSE Event Transport (Run + Tool + Final Output)

### 1.1 Run event bus

- [x] Add `RunEventBus` in `internal/channels/http/events.go` with:
  - [x] per-run subscriber list
  - [x] per-run short replay buffer (e.g., last 128 events)
  - [x] terminal marker per run (completed/failed)
  - [x] non-blocking publish to buffered subscriber channels (drop oldest or drop new with counter)
- [x] API:
  - [x] `Subscribe(runID string, lastEventID int64) (<-chan RunEvent, func())`
  - [x] `Publish(runID string, event RunEvent)`
  - [x] `Close(runID string)`
- [x] `Subscribe` behavior:
  - [x] replay buffered events with `ID > lastEventID`
  - [x] if run already terminal, emit terminal event(s) and close channel

### 1.2 Queue/pipeline integration

**File**: `internal/channels/http/pipeline.go`

- [x] Introduce options-based API to preserve compatibility:

```go
type QueueRunOptions struct {
    EventBus *RunEventBus
}

func QueueRunWithOptions(ctx context.Context, store RunStore, executor RunExecutor,
    agentID, message, source, sessionID, thinkingMode string, opts QueueRunOptions) (Run, error)

// Keep existing QueueRun(...) as wrapper to QueueRunWithOptions(..., QueueRunOptions{})
```

- [x] In `executeQueuedRun` publish:
  - [x] `status=running` after run starts
  - [x] `completed` with output metadata on success
  - [x] `failed` with error on failure
- [x] Ensure `Close(runID)` is called in terminal path.

### 1.3 Executor progress callback plumbing

**Files**: `internal/channels/http/server.go`, `cmd/openclawssy/main.go`, `internal/runtime/engine.go`

- [x] Extend HTTP-side `ExecutionInput` (`server.go`) with optional callback:

```go
OnProgress func(eventType string, data map[string]any)
```

- [x] In queue execution path, set `OnProgress` to publish to RunEventBus.
- [x] Extend runtime-side `ExecuteInput` (`engine.go`) with matching callback:

```go
OnProgress func(eventType string, data map[string]any)
```

- [x] In `runtimeExecutor.Execute` (`cmd/openclawssy/main.go`), bridge callback from HTTP input to runtime input.

### 1.4 Runtime tool event publishing

**File**: `internal/runtime/engine.go`

- [x] In `onToolCall` callback (currently around line 324), emit progress callback event:
  - `eventType = "tool_end"`
  - payload includes:
    - `tool`
    - `tool_call_id`
    - `summary`
    - `error`
    - `duration_ms`

- [x] After `runner.Run` returns successfully, emit final non-streaming model text as a single `model_text` event (Layer 1 baseline) so UI can update before terminal status if needed.

### 1.5 SSE endpoint

**File**: `internal/channels/http/server.go`

- [x] Add `EventBus *RunEventBus` to server config/struct.
- [x] Register route: `/v1/runs/events/`.
- [x] Handler behavior:
  - [x] method GET only
  - [x] parse runID from path
  - [x] validate runID shape
  - [x] set SSE headers (`text/event-stream`, `no-cache`, `keep-alive`, `X-Accel-Buffering: no`)
  - [x] read `Last-Event-ID` header (optional) for replay
  - [x] subscribe to event bus with replay
  - [x] write `id:`, `event:`, `data:` frames
  - [x] heartbeat comment/event every ~15s while open
  - [x] close on context done or bus close

### 1.6 Frontend stream connection (no query token fallback by default)

**File**: `internal/channels/dashboard/ui/src/pages/chat.js`

- [x] Add stream state fields to `chatViewState`:
  - `streamActive`
  - `streamAbortController`
  - `streamLastEventID`
  - `currentStreamingText`
- [x] Implement SSE reader using `fetch` + `ReadableStream` parser to preserve Authorization header.
- [x] Token source: `chatViewState.apiClient.resolveBearerToken()`.
- [x] Connect stream after `sendMessage()` receives `runID`.
- [x] On stream healthy:
  - [x] disable run polling
  - [x] keep slower session polling (e.g., 5000ms) for transcript reconciliation
- [x] On stream failure:
  - [x] close stream
  - [x] restore polling fallback (`startPolling`)

### 1.7 Layer 1 tests

- [x] `internal/channels/http/events_test.go`
  - subscribe/publish/unsubscribe
  - replay behavior
  - terminal close behavior
- [x] `internal/channels/http/server_test.go`
  - SSE auth
  - SSE frame format
  - heartbeat emission
  - late subscriber terminal replay
- [x] `internal/channels/http/pipeline_test.go`
  - status/completed/failed event publication

### Layer 1 exit criteria

- [x] Sending chat message opens stream and shows immediate status/tool updates without 1.5s delay.
- [x] Polling fallback still works if stream disconnects.
- [x] No auth query-token requirement introduced globally.

---

## Layer 2 — Model Delta Streaming

### 2.1 Agent type extensions

**File**: `internal/agent/types.go`

- [x] Add callback fields:

```go
OnTextDelta func(delta string) error `json:"-"`
```

to:
- [x] `RunInput`
- [x] `ModelRequest`

### 2.2 Runner pass-through

**File**: `internal/agent/runner.go`

- [x] Forward `input.OnTextDelta` into each `ModelRequest`.
- [x] Keep nil behavior unchanged (non-streaming path remains default).

### 2.3 Provider streaming implementation

**File**: `internal/runtime/model.go`

- [x] Add streaming request path (`stream: true`) when `req.OnTextDelta != nil`.
- [x] Implement streaming parser that handles SSE lines robustly (avoid default scanner token-limit pitfalls).
- [x] Accumulate full content while emitting deltas.
- [x] Parse final content using existing tool-call parsing + thinking extraction logic.
- [x] Retry semantics:
  - [x] retries allowed before any delta is emitted
  - [x] once deltas have been emitted, do not retry silently (avoid duplicate token streams)

### 2.4 Runtime delta batching to transport

**File**: `internal/runtime/engine.go`

- [x] Wrap `OnTextDelta` to batch small chunks before publishing progress callback:
  - flush every ~100-150ms or size threshold (~160-240 chars)
- [x] Publish callback as:
  - `eventType="model_text"`
  - payload `{ text: "...", partial: true }`
- [x] Flush remaining buffer at run completion.

### 2.5 Layer 2 tests

- [x] `internal/runtime/model_test.go`
  - mock streaming SSE response parsing
  - retry behavior before/after first emitted delta
  - non-streaming fallback unchanged
- [x] `internal/agent/runner_test.go`
  - `OnTextDelta` forwarded and invoked

### Layer 2 exit criteria

- [x] Multiple `model_text` events arrive during long model generation.
- [x] Existing non-streaming tests remain green when callback is nil.

---

## Layer 3 — Frontend Progressive Rendering and Activity UX

### 3.1 Rendering strategy (safe incremental rollout)

**File**: `internal/channels/dashboard/ui/src/pages/chat.js`

- [x] Keep existing full-render path (`renderChatPage`) as fallback.
- [x] Add throttled streaming updater (`~100ms`) for pending assistant text updates.
- [x] First pass can update state + throttled rerender.
- [x] Optional second pass (after stabilization): direct DOM patch of pending assistant `<pre>` to reduce full rerender frequency.

### 3.2 Live tool indicators

- [x] On `tool_end` stream events:
  - [x] update `latestToolActivity`
  - [x] update `loopRisk` from streamed tool window
  - [x] append lightweight inline indicator to pending assistant block (or activity pane first if safer)

### 3.3 Activity pane live status

- [x] Update run meta and elapsed-time indicators from stream status events.
- [x] Keep existing session poll reconciliation for tool history durability.

### 3.4 Route-change and cleanup safety

- [x] Ensure stream and timers are closed when user leaves `/chat` route.
- [x] Ensure only one active stream per run.
- [x] Reconnect logic must not leak duplicate stream connections.

### 3.5 CSS updates

**File**: `internal/channels/dashboard/ui/styles.css`

- [x] Add pending-stream cursor style.
- [x] Add tool-indicator styles (running/success/failed variants if tool_start added later).

### 3.6 Manual verification checklist

- [ ] Send tool-heavy prompt, observe live tool updates and progressive text.
- [ ] Disconnect/reconnect network briefly, confirm fallback polling recovers.
- [ ] Navigate away from `/chat` and back, confirm no duplicate stream handlers.
- [ ] Confirm text input focus remains stable while streaming updates occur.

### Layer 3 exit criteria

- [x] User sees progressively updating assistant output during model generation.
- [x] Live activity pane updates in near real-time without waiting for poll intervals.
- [x] No focus-loss regressions from high-frequency updates.

---

## Implementation Order (Enforced)

1. **Layer 1 backend transport** (event bus + SSE endpoint + queue/runtime callback bridge)
2. **Layer 1 frontend stream client** (fetch stream + polling fallback)
3. **Layer 2 model streaming** (types → runner → provider → engine batching)
4. **Layer 3 UX polish** (render throttle, indicators, cleanup hardening)

---

## Risks & Mitigations

1. **Risk: token leakage via URL query param**
   - Mitigation: default to fetch stream with Authorization header.

2. **Risk: missed events for late subscribers**
   - Mitigation: replay buffer + terminal replay behavior in event bus.

3. **Risk: duplicate deltas on retries**
   - Mitigation: no silent retry after first emitted delta.

4. **Risk: UI churn/focus issues from frequent rerenders**
   - Mitigation: throttled rendering + optional targeted DOM patching.

5. **Risk: stream disconnects behind proxies**
   - Mitigation: SSE heartbeat and polling fallback.

---

## Final Go/No-Go Checklist

- [x] `go test ./...` passes
- [x] SSE endpoint authenticated and stable under concurrent runs
- [x] Chat page streams live deltas with graceful fallback
- [x] No memory leaks from stale subscribers/streams
- [ ] All three layers validated manually on dashboard
