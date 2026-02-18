# Handoff

Date: 2026-02-18

## What shipped in this batch

1. Improved follow-through handling when the model replies with "I'll do it" style text but does not execute tools.
   - `internal/agent/runner.go`
   - `internal/agent/runner_test.go`
   - Increased follow-through reprompt attempts.
   - Replaced draft-leaking fallback with a clear actionable retry message.

2. Improved tool-parse failure user messaging for blocked/invalid tool payloads.
   - `internal/runtime/model.go`
   - `internal/runtime/model_test.go`
   - When tool-call JSON is rejected (for example `http.request` not allowed), return a clear guidance message instead of raw JSON-like chatter.

3. Additional output redaction hardening on run completion/failure append paths.
   - `internal/runtime/engine.go`

4. Fixed Docs editor typing focus regression in dashboard UI.
   - `internal/channels/dashboard/ui/src/pages/docs.js`
   - Root cause: rerender on every keystroke recreated the textarea and dropped focus.

## Validation run

- `node --check internal/channels/dashboard/ui/src/pages/docs.js`
- `node --check internal/channels/dashboard/ui/src/pages/chat.js`
- `go test ./...`

All checks passed.

## Operator notes

- If users ask for Perplexity/web lookups and get a "tool not enabled" response, enable network in config:
  - `network.enabled=true`
  - maintain `network.allowed_domains` allowlist
- `http.request` remains capability/policy-gated by design.
