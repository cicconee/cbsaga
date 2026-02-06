SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

.DEFAULT_GOAL := help

# ==============================================================================
# DOCKER COMPOSE CONFIG
# ==============================================================================

COMPOSE := docker compose
PROJECT ?= cbsaga

INFRA := deployments/docker-compose.yml
SVCS  := deployments/docker-compose.services.yml
OBS   := deployments/docker-compose.observability.yml

DC_INFRA := $(COMPOSE) -f $(INFRA)
DC_SVCS  := $(COMPOSE) -f $(SVCS)
DC_OBS   := $(COMPOSE) -f $(OBS)

# ==============================================================================
# LOCAL DEV CONFIG
# ==============================================================================

SERVICES := orchestrator identity

BIN_DIR := ./bin
RUN_DIR := ./.run
LOG_DIR := $(RUN_DIR)/logs
PID_DIR := $(RUN_DIR)/pids

# ==============================================================================
# TOOLING CONFIG
# ==============================================================================

GOLINES_INSTALL := go install github.com/segmentio/golines@latest
GOLINES_CMD     := golines
GO_FOLDERS      := ./cmd/ ./internal/

# ==============================================================================
# HELP
# ==============================================================================

.PHONY: help
help: ## Show the help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n"} \
	/^##@/ {printf "\n%s\n", substr($$0, 5); next} \
	/^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2} \
	' $(MAKEFILE_LIST)

# ==============================================================================
# TOOLING
# ==============================================================================

##@ Code
.PHONY: lines
lines: ## Run golines
	$(GOLINES_INSTALL)
	$(GOLINES_CMD) -w --shorten-comments $(GO_FOLDERS)

# ==============================================================================
# DEV ENVIRONMENT
# ==============================================================================

##@ Dev (local bins + docker infra)
.PHONY: dev dev-run dev-tail dev-stop dev-clean dev-status
dev: infra dev-run ## Bring up infra, build binaries, run services locally

dev-run: build dirs ## Build + run local services
	./scripts/run.sh "$(SERVICES)"

dev-tail: ## Tail local service logs
	./scripts/tail.sh "$(SERVICES)"

dev-stop: ## Stop all running local services
	./scripts/stop.sh "$(SERVICES)"

dev-clean: dev-stop ## Stop local services and remove bin/.run
	rm -rf $(BIN_DIR) $(RUN_DIR)

dev-status: ## Show service PID + last logs
	./scripts/status.sh "$(SERVICES)"

# ==============================================================================
# INFRASTRUCTURE
# ==============================================================================

##@ Infra (docker)
.PHONY: infra infra-up infra-down infra-nuke infra-ps infra-logs
infra: infra-up dbs migrate connectors verify ## Start infra + bootstrap it
	@echo ""
	@echo "✅ Environment bootstrapped successfully"
	@echo ""

infra-up: ## Start docker compose infra (detached)
	$(DC_INFRA) up -d

infra-down: ## Stop infra (keep volumes)
	$(DC_INFRA) down

infra-nuke: ## Stop infra and delete volumes
	$(DC_INFRA) down -v --remove-orphans

infra-ps: ## Show infra status
	$(DC_INFRA) ps

infra-logs: ## Follow infra logs
	$(DC_INFRA) logs -f --tail=200

# ==============================================================================
# DEMO ENVIRONMENT
# ==============================================================================

##@ Demo (containers)
.PHONY: demo demo-up demo-down demo-nuke demo-logs
demo: infra demo-up ## Bring up demo stack (services in containers + obs if desired)

demo-up: ## Start demo services (and obs if included in DC_DEMO)
	$(DC_OBS) up -d
	$(DC_SVCS) up -d --build

demo-down: ## Stop demo services + infra
	$(DC_SVCS) down
	$(DC_OBS) down

demo-nuke: ## Stop demo stack + delete volumes
	$(DC_SVCS) down -v --remove-orphans
	$(DC_OBS) down -v --remove-orphans

demo-logs: ## Tail logs for demo services
	$(DC_SVCS) logs -f --tail=200

# ==============================================================================
# INFRASTRUCTURE BOOTSTRAP
# ==============================================================================

##@ Bootstrap steps (infra)
.PHONY: dbs migrate connectors verify
dbs: ## Create databases (idempotent)
	./scripts/create-dbs.sh

migrate: ## Run DB migrations
	./scripts/migrate.sh

connectors: ## Register/update Debezium connectors
	./scripts/register-connectors.sh

verify: ## Verify DBs + schemas + connectors
	./scripts/verify.sh

# ==============================================================================
# LOCAL BUILDS
# ==============================================================================

##@ Build
.PHONY: dirs build
dirs: ## Make all run directories (./.run)
	mkdir -p $(BIN_DIR) $(LOG_DIR) $(PID_DIR)

build: dirs ## Build all Go services -> ./bin
	@echo "Building services -> $(BIN_DIR)"
	$(foreach svc,$(SERVICES), \
		echo "➡️  $(svc)"; \
		go build -o $(BIN_DIR)/$(svc) ./cmd/$(svc); \
	)

# ==============================================================================
# NUKE
# ==============================================================================

##@ Cleanup
.PHONY: nuke
nuke: demo-nuke dev-clean infra-nuke ## Remove everything (containers, volumes, local bins)
