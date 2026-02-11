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
- Orchestrator now enqueues task requests and executes them asynchronously in background workers.
- `node-agent` exposes `POST /v1/execute` and runs CDP actions:
  - open URL
  - deterministic action primitives: `wait_for`, `click`, `type`, `wait`
  - wait render delay
  - capture screenshot
  - extract page title + final URL
- Task lifecycle is tracked:
  - `queued -> running -> completed|failed`
- `POST /v1/tasks` returns immediately (`202 Accepted`); use `GET /v1/tasks/{id}` for progress/result.
- Completed tasks store screenshots as artifacts and expose `screenshot_artifact_url`.
- Runner retries transient failures with exponential backoff (`max_retries` per task).
  - Defaults are configurable via:
    - `ORCHESTRATOR_TASK_MAX_RETRIES`
    - `ORCHESTRATOR_TASK_RETRY_BASE_DELAY`
    - `ORCHESTRATOR_TASK_RETRY_MAX_DELAY`
- When `actions` is omitted, node-agent can auto-plan simple search flows from `goal`
  using a lightweight page snapshot heuristic planner (`NODE_AGENT_PLANNER_MODE=heuristic`).

### Phase 4 started (dashboard)
- Orchestrator now serves a live dashboard at `GET /dashboard`.
- Dashboard includes:
  - node fleet cards with heartbeat + version metadata
  - live polling controls (pause/resume, refresh interval, fetch limit)
  - filtered task feed (status/search/artifact/failure/sort)
  - selectable task detail panel with metadata, action JSON, and replay
  - screenshot artifact preview modal and failure triage list
  - task submission form with reusable presets (session creation + queue task)
  - embedded HTML asset at `internal/api/assets/dashboard.html`

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
  -d '{"session_id":"sess_000001","url":"https://example.com","goal":"open page and capture screenshot","max_retries":2}'
```

6. Execute a deterministic action flow:
```bash
curl -sS -X POST http://localhost:8080/v1/tasks \\
  -H 'Content-Type: application/json' \\
  -d '{
    "session_id":"sess_000001",
    "url":"https://duckduckgo.com",
    "goal":"search for browser use",
    "actions":[
      {"type":"wait_for","selector":"input[name=\"q\"]","timeout_ms":8000},
      {"type":"type","selector":"input[name=\"q\"]","text":"browser use"},
      {"type":"click","selector":"button[type=\"submit\"]"},
      {"type":"wait","delay_ms":1200}
    ]
  }'
```

7. Poll task status:
```bash
curl -sS http://localhost:8080/v1/tasks/<task-id>
```

8. Fetch stored screenshot artifact:
```bash
curl -sS http://localhost:8080/artifacts/screenshots/<artifact-file>.png --output screenshot.png
```

9. Run tests:
```bash
make test
```

10. Open the dashboard:
```bash
open http://localhost:8080/dashboard
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
- Sessions are still in-memory; task state is persisted in Postgres.
- Queued tasks are reconciled from Postgres on runner startup/restart.
- Task responses prefer `screenshot_artifact_url`; `screenshot_base64` is used only as fallback when artifact storage fails.
- Task status payload includes `attempt`, `max_retries`, and `next_retry_at` for retry visibility.
- Supported deterministic action types include `wait_for`, `click`, `type`, `wait`, `press_enter`, and `wait_for_url_contains`.
- `GET /v1/tasks?limit=N` returns recent tasks (newest first) for dashboard polling.
