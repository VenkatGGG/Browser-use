COMPOSE := docker compose -f deploy/compose/docker-compose.yml --env-file deploy/compose/.env

.PHONY: up down logs ps test fmt proto run-orchestrator run-orchestrator-pool init-env preflight health infra-up infra-down build-browser-node-image dev-pool clean-pool-nodes

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

infra-up: preflight init-env
	$(COMPOSE) up -d redis postgres

infra-down:
	$(COMPOSE) stop redis postgres

build-browser-node-image: preflight
	docker build -f docker/browser-node/Dockerfile -t browseruse-browser-node:latest .

run-orchestrator-pool: init-env
	@set -a; . deploy/compose/.env; set +a; \
	REDIS_ADDR=$${REDIS_ADDR_LOCAL:-localhost:6379} \
	POSTGRES_DSN=$${POSTGRES_DSN_LOCAL:-postgres://browseruse:browseruse@localhost:5432/browseruse?sslmode=disable} \
	ORCHESTRATOR_POOL_ENABLED=true \
	ORCHESTRATOR_POOL_TARGET_READY=$${ORCHESTRATOR_POOL_TARGET_READY:-2} \
	ORCHESTRATOR_POOL_DOCKER_IMAGE=$${ORCHESTRATOR_POOL_DOCKER_IMAGE:-browseruse-browser-node:latest} \
	ORCHESTRATOR_POOL_DOCKER_NETWORK=$${ORCHESTRATOR_POOL_DOCKER_NETWORK:-bridge} \
	ORCHESTRATOR_POOL_ORCHESTRATOR_URL=$${ORCHESTRATOR_POOL_ORCHESTRATOR_URL:-http://host.docker.internal:8080} \
	go run ./cmd/orchestrator

dev-pool: infra-up build-browser-node-image
	$(MAKE) run-orchestrator-pool

clean-pool-nodes: preflight
	@ids=$$(docker ps -aq --filter label=browseruse.managed=true); \
	if [ -n "$$ids" ]; then \
		docker rm -f $$ids >/dev/null; \
		echo "removed managed pool containers"; \
	else \
		echo "no managed pool containers to remove"; \
	fi
