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

## Quick start

1. Initialize env and boot the stack:
```bash
make up
```

2. Validate API health:
```bash
curl http://localhost:8080/healthz
```

3. Run tests:
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
