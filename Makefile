COMPOSE := docker compose -f deploy/compose/docker-compose.yml --env-file deploy/compose/.env

.PHONY: up down logs ps test fmt proto run-orchestrator init-env

init-env:
	@if [ ! -f deploy/compose/.env ]; then cp deploy/compose/.env.example deploy/compose/.env; fi

up: init-env
	$(COMPOSE) up --build -d

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
