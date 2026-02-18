# Openclawssy Dashboard Rewrite Dev Plan (Modular UI + Legacy Fallback)

Owner: ____________________  
Start date: _______________  
Target milestone: _________  
Repo: mojomast/openclawssy  
Status key: ⬜ Not started · 🟦 In progress · 🟩 Done · 🟥 Blocked

---

## Goals (must-hit)
1) **Space used properly**: multi-pane layout (no endless vertical stacking).
2) **Granular visibility**: user can see “everything pertinent” the bot is doing, down to tool call args/output/errors and traces.
3) **All settings available + easy**: settings are discoverable and editable from the UI.
4) **Usability improvements**: the UI actively prevents the failure patterns we saw (schema mismatch, venv confusion, secrets confusion, iteration loops).
5) **Modular**: easy to extend/replace panes, endpoints, and renderers later without editing a single giant string.

> Legacy fallback requirement: keep the old dashboard reachable, and add a **link at the bottom** of the new dashboard to open it.

---

## Non-goals (for this phase)
- Fixing backend behavior (secrets injection, cancellation endpoints, tool catalog endpoints, etc.)  
  We will still **design UI hooks** so backend can be wired later with minimal churn.

---

## Architecture Decision: Static Assets + Tiny Client Router
Current dashboard is embedded HTML (`dashboardHTML`) served from `internal/channels/dashboard/handler.go` at `/dashboard`.  
Rewrite will:
- Serve new UI as static files from `internal/channels/dashboard/ui/`
- Keep legacy UI served at `/dashboard-legacy`
- Provide link in new UI footer: “Open Legacy Dashboard”

Backend APIs to use now (already exist):
- `/api/admin/status` (provider/model)  
- `/api/admin/config` (GET/POST)  
- `/api/admin/secrets` (GET keys / POST set)  
- `/api/admin/scheduler/*` (jobs + control)  
- `/api/admin/chat/sessions` + `/messages`  
- `/api/admin/debug/runs/{id}/trace`  
- `/v1/chat/messages`, `/v1/runs`, `/v1/runs/{id}`

---

## Deliverables
- New dashboard at `/dashboard`
- Legacy dashboard at `/dashboard-legacy`
- New UI footer link: `/dashboard-legacy`
- Modular file layout with clear ownership boundaries
- “MVP parity” with current features + adds missing UI for scheduler/sessions/runs/trace + inspector panes + fix-suggestion UX

---

# Phase 0 — Safe Setup: Legacy fallback + new UI scaffolding

## P0.1 Add legacy route
Status: 🟩  PR: _______
- [x] Serve current monolithic HTML at **`/dashboard-legacy`**
- [x] Keep `/dashboard` temporarily as-is until new UI is ready (or flip immediately if comfortable)

Acceptance:
- [x] `/dashboard-legacy` loads the existing UI exactly as before.

## P0.2 Create static UI directory + asset server
Status: 🟩  PR: _______
Create:
- [x] `internal/channels/dashboard/ui/index.html`
- [x] `internal/channels/dashboard/ui/styles.css`
- [x] `internal/channels/dashboard/ui/app.js`

And in Go:
- [x] Add static serving route for `/dashboard/static/*` → serves files from that folder
- [x] Update `/dashboard` handler to serve the new `index.html`

Acceptance:
- [x] `/dashboard` loads new blank UI shell (header/nav/main panes)
- [x] Footer includes link: **Open Legacy Dashboard** → `/dashboard-legacy`

---

# Phase 1 — Modular Frontend Foundation

## P1.1 Frontend module layout (no framework required)
Status: 🟦  PR: _______
Create `internal/channels/dashboard/ui/src/` modules (bundling optional; can also be plain JS modules):
- [x] `src/api/client.js` (fetch wrapper + auth + JSON parsing + consistent error objects)
- [x] `src/router/router.js` (hash router or simple in-app router)
- [x] `src/state/store.js` (single store + subscriptions; no global spaghetti)
- [x] `src/ui/layout.js` (3-pane grid + resizers)
- [ ] `src/ui/components/` (reusable: tabs, tables, json viewer, toast, modal) - json viewer added, others pending
- [x] `src/pages/` (chat, sessions, runs, scheduler, settings, secrets)
- [ ] `src/inspectors/` (tool-call inspector, trace inspector, run meta, config diff) - tool + trace inspectors added
- [x] `src/ux/fix_suggestions.js` (error classifiers + suggested actions)
- [x] `src/ux/venv_panel.js` (UI-only for now; backend wiring later)
- [x] `src/ux/tool_schema_panel.js` (UI-only for now; backend wiring later)

Acceptance:
- [x] Adding a new page = one new file in `pages/` + one route entry
- [x] No giant monolithic HTML/JS strings

## P1.2 3-pane layout + resizable panes + persistence
Status: 🟦  PR: _______
Layout:
- Left: nav
- Center: active view (chat/timeline)
- Right: inspector tabs

Add:
 - [x] Draggable resizers between panes
 - [x] LocalStorage persistence of pane sizes + collapsed state
 - [x] Responsive behavior: right pane becomes drawer on narrow screens

Acceptance:
 - [x] On wide screens, chat + timeline + inspector are visible simultaneously
 - [x] No “scroll forever to find tools” experience

---

# Phase 2 — “Everything the bot is doing” (granular visibility)

## P2.1 Runs page (list/filter/open)
Status: 🟩  PR: _______
- [x] Runs list uses `/v1/runs?status&limit&offset`
- [x] Filters: status; pagination controls
- [x] Clicking a run loads `/v1/runs/{id}` and optionally `/api/admin/debug/runs/{id}/trace`

Acceptance:
- [x] User can browse prior runs without copying IDs manually

## P2.2 Run Timeline (trace-first experience)
Status: 🟦  PR: _______
- [x] Timeline view for a selected run:
  - model step(s) (if in trace)
  - tool calls (name, args preview, duration, status)
  - error blocks grouped (same tool+same error collapses)
- [x] Selecting a tool call populates Inspector with:
  - args JSON (pretty)
  - output (truncate/expand)
  - error (if any)
  - copy buttons
- [x] Trace inspector shows source metadata (`debug`/`run`/`none`) and trace payload context

Acceptance (must address the earlier failures):
- [ ] If `fs.edit` fails with “missing edits”, UI highlights tool call payload and error clearly. (pending manual UI walkthrough)
- [ ] If tool repeats failures, UI groups them so iteration loops are obvious. (pending manual UI walkthrough)

## P2.3 Sessions page (browse + open + tool events)
Status: 🟦  PR: _______
- [x] Sessions list uses `/api/admin/chat/sessions` (limit/offset)
- [x] Search box + sort modes
- [x] Open session → `/api/admin/chat/sessions/{id}/messages?limit=...`
- [x] Render:
  - user/assistant messages
  - tool messages as structured events (not just raw blobs)

Acceptance:
- [ ] You can inspect a past troubleshooting session and see tool outputs/errors inline. (pending manual UI walkthrough)

## P2.4 “Live Activity” pane (for current chat)
Status: 🟦  PR: _______
- [x] When a chat run is in-flight, show:
  - “current run id”
  - latest tool activity (stream/poll via session messages)
  - last error summary
- [x] Surface iteration count / “loop risk” indicator (client-only heuristic)
- [x] Show explicit in-flight heartbeat text in chat transcript and queued/run status handoff messaging

Acceptance:
- [ ] User never wonders “what is it doing right now?” (pending manual UX walkthrough)

---

# Phase 3 — Settings: Everything accessible + easy to get to

> Backend config endpoint exists already; we’ll reorganize the UI to make it discoverable.

## P3.1 Settings Home + categories
Status: 🟦  PR: _______
Pages:
- [x] General
- [x] Model Provider
- [x] Chat/Discord
- [x] Sandbox/Shell
- [x] Network
- [x] Scheduler
- [x] Capabilities (UI-first; backend wiring later if needed)
- [x] Advanced (raw JSON editor + diff)

UX:
- [x] Always show breadcrumbs + search within settings
- [x] Inline validation messages (client-side basic + show server error on save)
- [x] “Diff before save” (compare loaded config vs edited config)

Acceptance:
- [ ] A new user can find “model provider” in 1 click
- [ ] A power user can still edit raw config safely and see diff

## P3.2 Secrets page improvements
Status: 🟦  PR: _______
- [x] Keys list from `/api/admin/secrets` (GET)
- [x] Store secret via POST (existing)
- [x] Quality-of-life:
  - searchable keys list
  - copy key name
  - “conventions” helper block (provider keys, PERPLEXITY_API_KEY, etc.)
  - never display stored values (only accept new input)

Acceptance:
- [ ] User can confirm PERPLEXITY_API_KEY exists (key visible)
- [ ] No confusing “env | grep” debugging needed from the UI side

## P3.3 Scheduler page (already supported server-side)
Status: 🟦  PR: _______
- [x] List jobs (paused + jobs)
- [x] Add job form (agent_id, schedule, message, enabled)
- [x] Delete job
- [x] Pause/resume scheduler
- [x] Enable/disable specific job

Acceptance:
- [ ] All scheduler functions usable without leaving UI (pending manual UI walkthrough)

---

# Phase 4 — Usability: Prevent the tool failures you saw

This phase MUST cover all “problems the first devplan addressed”.

## P4.1 Tool Schema Viewer (UI now, backend later)
Status: 🟦  PR: _______
UI now:
- [x] Add “Tool Schema” inspector tab with placeholder data model:
   - show required fields
   - show example payload
- [x] For now: hardcode schemas for built-in tools in a JSON file:
   - `ui/src/data/tool_schemas.json`

Later (backend wiring):
- Replace hardcoded schemas with a `/api/admin/tools` endpoint.

Acceptance:
- [x] When user clicks `fs.edit`, UI shows “requires edits[]”
- [x] Prevents the exact schema misuse that caused `missing argument: edits`

## P4.2 Fix Suggestions Panel (error classifier)
Status: 🟦  PR: _______
Detect and display “suggested next step” actions when errors appear in:
- tool outputs
- run trace errors
- API errors

Rules (initial):
- [x] `tool.input_invalid` → show schema + highlight missing fields
- [x] `externally-managed-environment` → suggest venv creation and always use `.venv/bin/python`
- [x] `ModuleNotFoundError` → suggest “install requirements in venv”
- [x] “env var not set” → suggest check Secrets page and later “inject/export to run env”
- [x] `context deadline exceeded` → show provider request info and suggest retry/backoff (UI only)

Acceptance:
- [x] When pip fails with externally-managed-environment, UI suggests venv workflow immediately
- [x] When `requests` missing, UI suggests installing into venv and running with venv python

## P4.3 Venv Manager Pane (UI-only for now)
Status: 🟦  PR: _______
- [x] Add an inspector tab “Python Env”
- [x] Provide UI fields/buttons (no backend required yet):
   - venv path input (default: `./.venv`)
   - “suggested commands” generator:
     - create venv
     - install requirements
     - run script using `.venv/bin/python`
- [x] Copy-to-clipboard buttons for commands

Acceptance:
- [x] User can copy the correct venv commands without the bot guessing wrong

## P4.4 Run controls: Stop polling + “soft cancel”
Status: 🟦  PR: _______
Frontend-only:
- [x] “Stop polling” button (halts UI polling and clears “Thinking…”)
- [x] “Retry” button (re-send last prompt)
- [x] “Copy debug bundle” (selected run/session ids + errors)

Later (backend wiring):
- Add true cancel endpoint.

Acceptance:
- [ ] User can stop the UI from spinning forever when iteration caps happen

## P4.5 Provider/model identity stamp everywhere
Status: 🟦  PR: _______
- [x] Display provider/model from `/api/admin/status` in header
- [x] When rendering run/session, display provider/model for that run if available in trace; otherwise label as “current config”

Acceptance:
- [x] If bot claims “I’m Claude”, user can see the actual configured model/provider at all times

---

# Phase 5 — Polish (space, speed, and maintainability)

## P5.1 JSON viewer + truncation + streaming-friendly UI
Status: 🟦  PR: _______
- [x] Reusable JSON viewer component (collapse/expand)
- [x] Truncate huge outputs with “expand”
- [x] Search within JSON text

## P5.2 Theming + accessibility + keyboard shortcuts
Status: 🟦  PR: _______
- [ ] Improve contrast and spacing
- [ ] Shortcuts:
  - [x] `g c` chat, `g r` runs, `g s` scheduler
  - [x] `/` focus search
  - [x] `Esc` close drawer

---

# Legacy Fallback UX Requirement (must-have)
- [x] New UI footer contains: **“Open Legacy Dashboard”** linking to `/dashboard-legacy`
- [x] Optional: “Report bug” link to prefill run/session id in issue template

---

# QA Checklist (manual scripts)

Automation note:
- Added handler-level automated checks for `/api/admin/status` model stamp payload and static tool schema catalog serving/missing-file behavior in `internal/channels/dashboard/handler_test.go`.

## Script 1: Tool failure visibility
- [ ] Trigger a known tool input error (e.g., bad payload)
- [ ] Confirm: timeline shows error, inspector shows payload, Tool Schema tab shows required args, Fix Suggestions appear

## Script 2: Python dependency failure guidance
- [ ] Confirm: Fix Suggestions show venv workflow; Python Env tab generates correct commands

## Script 3: Secrets workflow confidence
- [ ] Store PERPLEXITY_API_KEY
- [ ] Confirm key appears in list
- [ ] UI shows “env var missing” guidance that points to Secrets page (until backend injection is wired)

## Script 4: Scheduler usability
- [ ] Create job
- [ ] Disable job
- [ ] Pause scheduler
- [ ] Delete job

---

# Progress Tracker

| PR | Theme | Status | Notes |
|----|-------|--------|------|
| P0.1 | /dashboard-legacy route | 🟩 | |
| P0.2 | static UI serve + /dashboard new shell | 🟩 | |
| P1.1 | modular JS layout | 🟦 | Foundation wired; modular pages now include a dashboard Docs editor for core agent markdown files, with remaining component/inspector depth in follow-up slices. |
| P1.2 | 3-pane resizable layout | 🟦 | Resizers, localStorage pane state, and mobile inspector drawer added in modular shell; verify with manual UX pass. |
| P2.1 | runs page | 🟩 | `/runs` now supports status filter + pagination, run open, optional debug trace fetch, and inspector state wiring (`selectedTrace`, `selectedTool`, `lastError`). |
| P2.2 | run timeline + inspector | 🟦 | Runs page now renders trace-first timeline with model-step notes, tool-call blocks, repeated-failure grouping, tool-click inspector wiring, and trace source metadata; acceptance scenarios still need manual walkthrough. |
| P2.3 | sessions page | 🟦 | `/sessions` now uses paged sessions endpoint, includes client-side search/sort controls, opens session messages with configurable `limit`, renders user/assistant chat items plus structured tool-event cards, and supports optional inspector wiring via `selectedTool`; acceptance still needs manual UX pass. |
| P2.4 | live activity pane | 🟦 | `/chat` now sends prompts via `/v1/chat/messages` using dashboard defaults, renders user/assistant transcript with sticky-bottom behavior unless user scrolls up, polls `/v1/runs/{id}` and session messages for live tool activity/error summaries, and shows client-side loop-risk heuristics plus explicit in-flight heartbeat text; runtime now also enforces default run timeouts, appends assistant "need your attention" failure messages on run error/timeout, and reprompts deferral-only model replies to reduce "I'll do it" without action outcomes; acceptance needs manual UX pass. |
| P3.1 | settings pages + diff | 🟦 | `settings.js` now provides category workspace (General/Model/Chat/Sandbox/Network/Scheduler/Capabilities/Advanced), config draft vs baseline handling, inline validation, clear server save errors, search + breadcrumbs, and diff-before-save with changed path table plus raw JSON editor; acceptance still needs manual UX verification. |
| P3.2 | secrets page | 🟦 | `secrets.js` now uses a modular UI with GET key loading, search + copy-name controls, write-only POST set/rotate form, and a naming-conventions helper block (including PERPLEXITY_API_KEY and Discord token patterns); acceptance still needs manual UX verification. |
| P3.3 | scheduler page | 🟦 | `/scheduler` now has modular controls for global paused/running state, refresh, add-job form (`agent_id`, `schedule`, `message`, optional `id`, optional explicit `enabled`), per-job enable/disable via scheduler control, delete actions, and clear success/error banners; acceptance still needs manual UI walkthrough. |
| P4.1 | tool schema viewer (hardcoded first) | 🟦 | Added dedicated Schema inspector tab, loaded hardcoded schema catalog from `ui/src/data/tool_schemas.json`, and highlighted missing required fields with targeted `fs.edit` guidance. |
| P4.2 | fix suggestions | 🟦 | Expanded classifier rules (`tool.input_invalid`, externally managed env, missing module, env/secret missing, timeout) and added one-click actions to open Schema/Tool/Python/Secrets/Settings views. |
| P4.3 | python env pane | 🟦 | Added dedicated Python Env inspector tab with editable venv path, generated commands, and copy buttons. |
| P4.4 | stop polling + soft controls | 🟦 | `/chat` now includes Stop polling, Retry (re-send last prompt), and Copy debug bundle actions; backend true-cancel endpoint remains future work. |
| P4.5 | provider/model stamp everywhere | 🟦 | Header now fetches `/api/admin/status` and shows runtime provider/model; runs/sessions render model identity using run metadata when present or fallback to current config. |
| P5.* | polish | 🟦 | Added global keyboard shortcuts (`g c`, `g r`, `g s`, `/`, `Esc`), JSON viewer search, and footer bug-report link with prefilled context; contrast/spacing polish remains. |

---
