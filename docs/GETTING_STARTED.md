# Getting Started

Welcome. This is the fastest safe path to try Openclawssy locally.

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

## 6) Send a run

```bash
curl -s -X POST http://127.0.0.1:8080/v1/runs \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"default","message":"/tool fs.list {""path"":"".""}"}'
```

## Important warning

This project is still a prototype. Use it only in disposable dev environments.
