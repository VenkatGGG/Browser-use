# Browser-use

Local-first orchestration infrastructure for AI browser automation.

## Current implementation status

### Phase 0 complete
- Go project structure scaffolded: `cmd`, `internal`, `pkg`, `proto`, `deploy`, `docker`.
- Orchestrator HTTP API skeleton:
  - `GET /healthz`
  - `POST /v1/sessions` (alias: `POST /sessions`)
  - `DELETE /v1/sessions/{id}` (alias: `DELETE /sessions/{id}`)
  - `POST /v1/tasks` (alias: `POST /task`)
  - `GET /v1/tasks/{id}` (alias: `GET /tasks/{id}`)
- Node-agent bootstrap binary with health endpoint.
- Node gRPC contract defined in `/proto/node.proto` with generated Go stubs in `/internal/gen`.

### Phase 1 complete (local infra baseline)
- Docker Compose stack with:
  - `orchestrator`
  - `redis`
  - `postgres`
- One-command local startup via `make up` with docker preflight checks and readiness wait (`/healthz`).
- `.env` template in `deploy/compose/.env.example`.

### Phase 2 complete (hardened browser sandbox)
- `browser-node` container now includes:
  - `google-chrome-stable` on `amd64` (falls back to `chromium` on `arm64`)
  - `Xvfb` virtual display
  - `playwright-core` runtime
  - `node-agent` sidecar (HTTP health + gRPC control plane)
- Node phone-home API flow implemented:
  - `POST /v1/nodes/register`
  - `POST /v1/nodes/{id}/heartbeat`
  - `GET /v1/nodes`
- Orchestrator now executes node actions through gRPC (`execute_flow`) instead of direct HTTP calls.
- Runtime hardening applied in compose:
  - non-root node process
  - read-only root filesystem
  - tmpfs for writable runtime paths
  - dropped Linux capabilities + `no-new-privileges`
  - CPU/memory/pid limits
  - Chrome debug port is no longer publicly published.

### Phase 3 started (local provider + warm pool)
- Warm-pool control loop implemented in `internal/pool/manager.go`:
  - maintains target ready count
  - tracks `warming` nodes and times them out
  - reaps stale nodes on heartbeat timeout
  - reaps old nodes by max-age
- Local Docker provider implemented in `internal/pool/local_docker_provider.go`:
  - provisions isolated browser-node containers
  - assigns deterministic node IDs and gRPC addresses
  - destroys managed nodes by ID
- Orchestrator wiring added (feature-flagged):
  - `ORCHESTRATOR_POOL_ENABLED=true`
  - `ORCHESTRATOR_POOL_TARGET_READY=<N>`
  - provider/manager config from `ORCHESTRATOR_POOL_*` env vars

### Phase 4 started (lease safety + idempotency)
- Runner now supports distributed node leasing with fencing tokens:
  - `internal/lease` package (in-memory + Redis implementations)
  - lease TTL via `ORCHESTRATOR_NODE_LEASE_TTL`
  - lease state is reflected in node registry (`leased_until`)
- API idempotency support added for creates:
  - `POST /v1/sessions` and `POST /v1/tasks`
  - send `Idempotency-Key` header to get safe retries without duplicate resources
  - `internal/idempotency` package (in-memory + Redis implementations)
  - retention and lock TTL via:
    - `ORCHESTRATOR_IDEMPOTENCY_TTL`
    - `ORCHESTRATOR_IDEMPOTENCY_LOCK_TTL`

### Phase 5 started (brain execution baseline)
- Orchestrator now enqueues task requests and executes them asynchronously in background workers.
- `node-agent` exposes `POST /v1/execute` and runs CDP actions:
  - open URL
  - deterministic action primitives: `wait_for`, `click`, `type`, `wait`
  - wait render delay
  - capture screenshot
  - extract page title + final URL
- Task lifecycle is tracked:
  - `queued -> running -> completed|failed`
- `POST /v1/tasks` returns immediately (`202 Accepted`) by default; use `GET /v1/tasks/{id}` for progress/result.
- `POST /task` defaults to wait for terminal state and returns final task payload (`200 OK`) unless `wait_for_completion` is explicitly set to `false`.
- `POST /v1/tasks/{id}/replay` clones an existing task and re-queues it (supports optional `session_id` or `create_new_session` + `tenant_id`, `max_retries` overrides, and tracks lineage via `source_task_id`).
- `GET /v1/tasks/{id}/replay_chain` returns replay lineage (task -> parent -> root).
- `GET /v1/tasks/{id}/replays` returns direct replay children for a task.
- Completed tasks store screenshots as artifacts and expose `screenshot_artifact_url`.
- Runner retries transient failures with exponential backoff (`max_retries` per task).
  - Defaults are configurable via:
    - `ORCHESTRATOR_TASK_MAX_RETRIES`
    - `ORCHESTRATOR_TASK_RETRY_BASE_DELAY`
    - `ORCHESTRATOR_TASK_RETRY_MAX_DELAY`
    - `ORCHESTRATOR_TASK_DOMAIN_BLOCK_COOLDOWN`
- When `actions` is omitted, node-agent can auto-plan simple search flows from `goal`
  using a built-in template planner (`NODE_AGENT_PLANNER_MODE=template`).

### Phase 6 started (compact context planner path)
- Node-agent now builds a compact planner state packet from visible interactive elements only:
  - stable element ids (`stable_id`)
  - role/name/text/selector
  - viewport coordinates + dimensions
- Added optional external planner mode:
  - `NODE_AGENT_PLANNER_MODE=endpoint`
  - `NODE_AGENT_PLANNER_ENDPOINT_URL=<planner-api>`
  - optional auth/model controls:
    - `NODE_AGENT_PLANNER_AUTH_TOKEN`
    - `NODE_AGENT_PLANNER_MODEL`
    - `NODE_AGENT_PLANNER_TIMEOUT`
    - `NODE_AGENT_PLANNER_MAX_ELEMENTS`
- Endpoint planner has safe fallback to deterministic heuristic planning on endpoint failures/invalid output.
- Built-in template planner now handles common commerce extraction goals (for example: search + extract price) without external planner services.
- Task records now persist execution trace steps (`trace`) including action payload, step status, timing, and failure reason when available.
- Optional trace step screenshots can be enabled with `NODE_AGENT_TRACE_SCREENSHOTS=true` (or `ORCHESTRATOR_POOL_NODE_TRACE_SCREENSHOTS=true` for managed warm-pool nodes).

### Phase 4 started (dashboard)
- Orchestrator now serves a live dashboard at `GET /dashboard`.
- Dashboard includes:
  - node fleet cards with heartbeat + version metadata
  - live polling controls (pause/resume, refresh interval, fetch limit)
  - filtered task feed (status/search/artifact/failure/blocker/sort)
  - blocker-aware task table column + blocker KPIs (count and rate)
  - selectable task detail panel with metadata, lineage jump-to-source, action JSON, and replay
  - screenshot artifact preview modal and failure triage list
  - task submission form with reusable presets (session creation + queue task)
  - embedded HTML asset at `internal/api/assets/dashboard.html`

## Quick start

1. Initialize env and boot the stack:
```bash
make up
```

Alternative local warm-pool mode (host-run orchestrator + dynamic local sandboxes):
```bash
make dev-pool
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

5b. Execute a one-shot task without pre-creating a session (`session_id` auto-created):
```bash
curl -sS -X POST http://localhost:8080/task \\
  -H 'Content-Type: application/json' \\
  -d '{"url":"https://example.com","goal":"open page and capture screenshot"}'
```

Optional (safe retries): add idempotency key for create operations:
```bash
curl -sS -X POST http://localhost:8080/v1/tasks \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: task-demo-001' \
  -d '{"session_id":"sess_000001","url":"https://example.com","goal":"open page and capture screenshot"}'
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

Optional async mode for `/task` (return `202 Accepted` immediately):
```bash
curl -sS -X POST http://localhost:8080/task \\
  -H 'Content-Type: application/json' \\
  -d '{
    "session_id":"sess_000001",
    "url":"https://duckduckgo.com",
    "goal":"search for browser use",
    "wait_for_completion": false
  }'
```

7. Poll task status:
```bash
curl -sS http://localhost:8080/v1/tasks/<task-id>
```

8. Replay an existing task (override session/retries):
```bash
curl -sS -X POST http://localhost:8080/v1/tasks/<task-id>/replay \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"sess_000001","max_retries":2}'
```

9. Replay an existing task in a fresh session:
```bash
curl -sS -X POST http://localhost:8080/v1/tasks/<task-id>/replay \
  -H 'Content-Type: application/json' \
  -d '{"create_new_session":true,"tenant_id":"local-dev","max_retries":2}'
```

10. Fetch stored screenshot artifact:
```bash
curl -sS http://localhost:8080/artifacts/screenshots/<artifact-file>.png --output screenshot.png
```

11. Run tests:
```bash
make test
```

12. Open the dashboard:
```bash
open http://localhost:8080/dashboard
```

## Development commands

```bash
make up              # build + start local stack
make down            # stop stack
make logs            # stream compose logs
make ps              # list compose services
make health          # wait for orchestrator readiness
make test            # run go tests
make fmt             # gofmt all go files
make proto           # generate go protobuf stubs
make run-orchestrator
make infra-up        # start only redis+postgres for host-run orchestrator
make run-orchestrator-pool
make dev-pool        # infra + browser image + host-run orchestrator with warm pool
make clean-pool-nodes
```

## Notes
- Sessions are still in-memory; task state is persisted in Postgres.
- Queued tasks are reconciled from Postgres on runner startup/restart.
- Task responses prefer `screenshot_artifact_url`; `screenshot_base64` is used only as fallback when artifact storage fails.
- Task status payload includes `attempt`, `max_retries`, and `next_retry_at` for retry visibility.
- Task status payload now also includes `trace` for step-by-step execution visibility.
- Supported deterministic action types include `wait_for`, `click`, `type`, `extract_text`, `scroll`, `wait`, `press_enter`, and `wait_for_url_contains`.
- Planner mode defaults to `template`; set `NODE_AGENT_PLANNER_MODE=endpoint` to call an external planner API using compact page state.
- `GET /v1/tasks?limit=N` returns recent tasks (newest first) for dashboard polling.
- `GET /v1/tasks/stats?limit=N` returns aggregated status/blocker metrics over recent tasks.
- `Idempotency-Key` header is supported on `POST /v1/sessions` and `POST /v1/tasks`.
- Node-agent now detects blocker pages (captcha/human verification/form validation), returns structured blocker metadata, and runner persists blocker evidence on failed tasks without retry loops.
- Runner applies per-domain cooldowns after challenge blockers (`human_verification_required` / `bot_blocked`) to fail subsequent tasks fast until cooldown expires.
- Warm-pool manager is currently feature-flagged and intended for host-run orchestrator mode (`make run-orchestrator`) where `docker` CLI is available; compose mode keeps static node service by default.
