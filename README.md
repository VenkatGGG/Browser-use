# Browser-use

Local-first orchestration infrastructure for AI browser automation.

## Current implementation status

### Phase 0 complete
- Go project structure scaffolded: `cmd`, `internal`, `pkg`, `proto`, `deploy`, `docker`.
- Orchestrator HTTP API skeleton:
  - `GET /healthz`
  - `POST /v1/sessions`
  - `DELETE /v1/sessions/{id}`
  - `POST /v1/tasks`
  - `GET /v1/tasks/{id}`
- Node-agent bootstrap binary with health endpoint.
- Node gRPC contract defined in `/proto/node.proto`.

### Phase 1 complete (local infra baseline)
- Docker Compose stack with:
  - `orchestrator`
  - `redis`
  - `postgres`
- One-command local startup via `make up`.
- `.env` template in `deploy/compose/.env.example`.

### Phase 2 in progress (agent connectivity baseline)
- `browser-node` container now runs:
  - `google-chrome-stable` on `amd64` (falls back to `chromium` on `arm64`)
  - `Xvfb` virtual display
  - `node-agent` sidecar
- Node phone-home API flow implemented:
  - `POST /v1/nodes/register`
  - `POST /v1/nodes/{id}/heartbeat`
  - `GET /v1/nodes`
- `node-agent` auto-registers with orchestrator and sends periodic heartbeats.

### Phase 3 started (brain execution baseline)
- Orchestrator now executes task requests against a ready node, not just queuing.
- `node-agent` exposes `POST /v1/execute` and runs CDP actions:
  - open URL
  - wait render delay
  - capture screenshot
  - extract page title + final URL
- Task lifecycle is tracked:
  - `queued -> running -> completed|failed`

## Quick start

1. Initialize env and boot the stack:
```bash
make up
```

2. Validate API health:
```bash
curl http://localhost:8080/healthz
```

3. Validate node registration:
```bash
curl http://localhost:8080/v1/nodes
```

4. Create a session:
```bash
curl -sS -X POST http://localhost:8080/v1/sessions \\
  -H 'Content-Type: application/json' \\
  -d '{"tenant_id":"local-dev"}'
```

5. Execute a task (replace `sess_000001` with created session id):
```bash
curl -sS -X POST http://localhost:8080/v1/tasks \\
  -H 'Content-Type: application/json' \\
  -d '{"session_id":"sess_000001","url":"https://example.com","goal":"open page and capture screenshot"}'
```

6. Run tests:
```bash
make test
```

## Development commands

```bash
make up              # build + start local stack
make down            # stop stack
make logs            # stream compose logs
make ps              # list compose services
make test            # run go tests
make fmt             # gofmt all go files
make proto           # generate go protobuf stubs
make run-orchestrator
```

## Notes
- Current session/task services are in-memory stubs for API contract validation.
- Redis/Postgres are wired for next phases (leasing, persistence, and pool manager state).
- Task responses currently include `screenshot_base64` inline for MVP simplicity.
