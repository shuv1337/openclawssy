# Getting Started

Welcome. This is the fastest safe path to try Openclawssy locally.

Think of this as your first walk through the Ussyverse: small scope, clean controls, and auditable outcomes.

## 1) Build

```bash
make fmt
make lint
make test
make build
```

## 2) Guided setup

```bash
./bin/openclawssy setup
```

During setup you can:
- pick provider and model
- ingest API key into encrypted secret store
- enable HTTPS dashboard
- enable Discord bot

## 3) Verify

```bash
./bin/openclawssy doctor -v
```

## 4) Start server

```bash
./bin/openclawssy serve --token change-me
```

## 5) Open dashboard

- HTTPS mode: `https://127.0.0.1:8080/dashboard`
- HTTP mode: `http://127.0.0.1:8080/dashboard`

Dashboard tips:
- Chat is session-aware (`/new`, `/resume <session_id>`, `/chats`).
- Tool activity is summarized per step (for example file writes show line counts).
- You can resize the chat panel and collapse tool/session/status/admin panes.

## 6) Send a run

```bash
curl -s -X POST http://127.0.0.1:8080/v1/runs \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"default","message":"/tool fs.list {""path"":"".""}"}'
```

Or use chat mode through the same API:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/chat/messages \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"dashboard_user","room_id":"dashboard","agent_id":"default","message":"list files and read README.md"}'
```

## Important warning

This project is still a prototype. Use it only in disposable dev environments.

Ussyverse rule #1: if it matters, isolate it.
