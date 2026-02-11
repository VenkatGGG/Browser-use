COMPOSE := docker compose -f deploy/compose/docker-compose.yml --env-file deploy/compose/.env

.PHONY: up down logs ps test fmt proto run-orchestrator init-env preflight health

init-env:
	@if [ ! -f deploy/compose/.env ]; then cp deploy/compose/.env.example deploy/compose/.env; fi

preflight:
	@docker info >/dev/null 2>&1 || (echo "docker daemon is not running"; exit 1)
	@docker compose version >/dev/null 2>&1 || (echo "docker compose plugin is not available"; exit 1)
	@command -v curl >/dev/null 2>&1 || (echo "curl is required"; exit 1)

up: preflight init-env
	$(COMPOSE) up --build -d
	$(MAKE) health

health:
	@echo "waiting for orchestrator healthz..."
	@for i in $$(seq 1 60); do \
		if curl -fsS "http://localhost:8080/healthz" >/dev/null 2>&1; then \
			echo "orchestrator is healthy"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "orchestrator failed health check"; \
	$(COMPOSE) ps; \
	exit 1

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=200

ps:
	$(COMPOSE) ps

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -type f)

proto:
	./scripts/generate_proto.sh

run-orchestrator:
	go run ./cmd/orchestrator
