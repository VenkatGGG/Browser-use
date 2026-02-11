# Browser-use

Local-first orchestration infrastructure for AI browser automation.

## Phase 0 status
- Go project structure scaffolded (`cmd`, `internal`, `pkg`, `proto`, `deploy`).
- Orchestrator HTTP API skeleton with basic routes:
  - `GET /healthz`
  - `POST /v1/sessions`
  - `DELETE /v1/sessions/{id}`
  - `POST /v1/tasks`
  - `GET /v1/tasks/{id}`
- Node-agent bootstrap binary with health endpoint.

## Quick start (current)
```bash
go test ./...
go run ./cmd/orchestrator
```

Use `ORCHESTRATOR_HTTP_ADDR` to override bind address (default `:8080`).
