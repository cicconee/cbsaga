SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

COMPOSE       := docker compose
COMPOSE_FILE  := deployments/docker-compose.yml

.DEFAULT_GOAL := help

SERVICES := orchestrator identity

BIN_DIR := ./bin
RUN_DIR := ./.run
LOG_DIR := $(RUN_DIR)/logs
PID_DIR := $(RUN_DIR)/pids

GOLINES_INSTALL=go install github.com/segmentio/golines@latest
GOLINES_CMD=golines
GO_FOLDERS=./cmd/ ./internal/

.PHONY: help
help: ## Show the help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n"} \
	/^##@/ {printf "\n%s\n", substr($$0, 5); next} \
	/^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2} \
	' $(MAKEFILE_LIST)

##@ Code
shorten-lines: ## Run golines
	${GOLINES_INSTALL}
	${GOLINES_CMD} -w --shorten-comments ${GO_FOLDERS} 

##@ Infra
up: ## Start docker compose infra (detached)
	$(COMPOSE) -f $(COMPOSE_FILE) up -d

down: ## Stop docker compose infra
	$(COMPOSE) -f $(COMPOSE_FILE) down

down-v: ## Stop infra and destroy volumes (DATA LOSS)
	$(COMPOSE) -f $(COMPOSE_FILE) down -v

ps: ## Show docker compose status
	$(COMPOSE) -f $(COMPOSE_FILE) ps

logs: ## Follow infra logs
	$(COMPOSE) -f $(COMPOSE_FILE) logs -f --tail=200

##@ Environment setup
dbs: ## Create databases (idempotent)
	./scripts/create-dbs.sh

migrate: ## Run DB migrations (dockerized)
	./scripts/migrate.sh

connectors: ## Register/update Debezium connectors
	./scripts/register-connectors.sh

verify: ## Verify DBs + schemas + connectors
	./scripts/verify.sh

bootstrap: up dbs migrate connectors verify ## up -> dbs -> migrate -> connectors -> verify
	@echo ""
	@echo "✅ Environment bootstrapped successfully"
	@echo ""

##@ Build / Run local services
dirs: ## Make all run directories (.run/)
	mkdir -p $(BIN_DIR) $(LOG_DIR) $(PID_DIR)

build: dirs ## Build all Go services -> dirs
	@echo "Building services -> $(BIN_DIR)"
	$(foreach svc,$(SERVICES), \
		echo "➡️  $(svc)"; \
		go build -o $(BIN_DIR)/$(svc) ./cmd/$(svc); \
	)

run: build dirs ## build -> dirs -> run all services
	./scripts/run.sh "$(SERVICES)"

stop: ## Stop all running services
	./scripts/stop.sh "$(SERVICES)"

clean: stop ## stop -> remove bin and run directories
	rm -rf $(BIN_DIR) $(RUN_DIR)

restart: stop run ## stop -> run

tail: ## Tail service logs
	./scripts/tail.sh "$(SERVICES)"

status: ## Show service PID + last logs
	./scripts/status.sh "$(SERVICES)"

demo: bootstrap run tail ## bootstrap + run + tail
