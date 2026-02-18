# Contributing

Thanks for contributing to Openclawssy.

## Local validation

Run before opening a PR:

```bash
make fmt
make lint
make test
```

At minimum, `go test ./...` must pass.

## How to add a tool

When adding a new tool, wire all layers in one change so behavior and policy stay consistent.

1. Add the tool handler and registration in `internal/tools/`.
   - Define `ToolSpec` with required args and argument types.
   - Enforce safety constraints in handler logic (path bounds, allowlists, limits, redaction).
2. Ensure capability enforcement applies (registry already calls policy checks).
3. Add canonical names/aliases in both:
   - `internal/runtime/model.go`
   - `internal/toolparse/parser.go`
4. Include the tool in runtime allowlist/docs surfaces:
   - `internal/runtime/engine.go` (`allowedTools`, runtime context docs, tool best-practices docs)
5. Add tests:
   - `internal/tools/tools_test.go` for handler behavior and policy denial
   - `internal/runtime/*_test.go` and `internal/toolparse/*_test.go` for wiring/alias parsing
6. Update docs:
   - `README.md` examples
   - `docs/TOOL_CATALOG.md`
   - `devplan.md` status/checklists

## Safety expectations

- Keep workspace boundaries strict for file-like operations.
- Do not leak secret values in logs, audit records, or error messages.
- Prefer explicit limits (timeouts, pagination, response size caps) for potentially unbounded operations.
- Preserve backwards-compatible aliases when renaming tool names.
