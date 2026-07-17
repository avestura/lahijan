# Lahijan Cloud Platform - Makefile
#
# Conventions:
#   - Every target is idempotent and safe to re-run.
#   - All go commands go through GOFLAGS so CI and local match.
#   - Frontend targets are no-ops until WS-18 lands; they exist now so CI wiring
#     is stable.
#
# Useful variables:
#   VERSION     - version string stamped into the binary (default: dev)
#   BUILD_DIR   - output directory for the binary (default: dist)
#   DB_URL      - PostgreSQL DSN used by migrate targets

VERSION    ?= dev
BUILD_DIR  ?= dist
BIN        := $(BUILD_DIR)/lahijan
MAIN       := ./cmd/lahijan/main.go
LD_FLAGS   := -X 'github.com/avestura/lahijan/internal/app/lahijan/version.LahijanVersion=$(VERSION)'
GO         := go
GOFLAGS    :=
DB_URL     ?= postgres://lahijan:lahijan@localhost:5432/lahijan?sslmode=disable

# Skip powershellisms; this Makefile targets GNU Make on both Linux and Windows
# (via WSL/Git-Bash). PowerShell wrappers live in scripts/.

.PHONY: help
help: ## Show this help
	@echo "Lahijan Makefile targets:"
	@echo ""
	@echo "Development:"
	@echo "  make build         Build the lahijan binary into dist/"
	@echo "  make run           Build + run with --debug"
	@echo "  make test          Run all Go unit tests (race)"
	@echo "  make test-short    Run only short tests"
	@echo "  make cover         Run tests with coverage"
	@echo "  make lint          Run golangci-lint (set LINT_FLAGS=--fast)"
	@echo "  make fmt           Format Go code with gofumpt"
	@echo "  make vet           Run go vet"
	@echo "  make tidy          Run go mod tidy"
	@echo ""
	@echo "Frontend (no-op until WS-18):"
	@echo "  make web-install web-build web-lint web-test"
	@echo ""
	@echo "Database (no-op until WS-03):"
	@echo "  make db-up         Apply all pending migrations"
	@echo "  make db-down       Roll back the last migration"
	@echo "  make db-new NAME=create_foo    Create a new migration"
	@echo "  make db-version    Show current migration version"
	@echo "  make sqlc          Regenerate sqlc code"
	@echo ""
	@echo "Docker / Compose:"
	@echo "  make dev-up        Start dev deps (Postgres etc.) via compose"
	@echo "  make dev-down      Stop dev deps"
	@echo "  make dev-logs      Tail dev dep logs"
	@echo "  make docker-build  Build production image"
	@echo ""
	@echo "Repo hygiene:"
	@echo "  make hooks         Install git hooks locally"
	@echo "  make ci-check      Run lint + test (everything CI runs)"
	@echo "  make clean         Remove build artifacts"
	@echo ""
	@echo "Docs:"
	@echo "  make docs-install  Install Docusaurus deps"
	@echo "  make docs          Serve docs site locally"

# ---------------------------------------------------------------------------
# Development
# ---------------------------------------------------------------------------

.PHONY: build
build: ## Build the lahijan binary into $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LD_FLAGS)" -o $(BIN) $(MAIN)

.PHONY: run
run: build ## Build and run lahijan locally with --debug
	./$(BIN) --debug --http.server.cors.enabled

.PHONY: test
test: ## Run all Go unit tests (no race; works without CGO)
	$(GO) test $(GOFLAGS) -timeout 120s ./...

.PHONY: test-race
test-race: ## Run all Go unit tests with -race (requires CGO_ENABLED=1)
	CGO_ENABLED=1 $(GO) test $(GOFLAGS) -race -timeout 120s ./...

.PHONY: test-short
test-short: ## Run only short tests (skips integration)
	$(GO) test $(GOFLAGS) -short -timeout 60s ./...

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test $(GOFLAGS) -bench=. -benchmem -run=^$$ ./...

.PHONY: cover
cover: ## Run tests with coverage, output to coverage.out
	$(GO) test $(GOFLAGS) -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format Go code with gofumpt (also golines if installed)
	$(GO) run mvdan.cc/gofumpt@latest -w .

.PHONY: lint
lint: ## Run golangci-lint (use LINT_FLAGS=--fast for fast mode)
	golangci-lint run $(LINT_FLAGS) ./...

.PHONY: tidy
tidy: ## Run go mod tidy
	$(GO) mod tidy

# ---------------------------------------------------------------------------
# Frontend (no-ops until WS-18; defined so CI is stable)
# ---------------------------------------------------------------------------

.PHONY: web-install web-build web-lint web-test
web-install: ## Install frontend dependencies (web/)
	@echo "frontend not yet initialized; will be wired in WS-18"

web-build: ## Build frontend (web/)
	@echo "frontend not yet initialized; will be wired in WS-18"

web-lint: ## Lint frontend (web/)
	@echo "frontend not yet initialized; will be wired in WS-18"

web-test: ## Test frontend (web/)
	@echo "frontend not yet initialized; will be wired in WS-18"

# ---------------------------------------------------------------------------
# Database (no-ops until WS-03; commands defined so help text is stable)
# ---------------------------------------------------------------------------

MIGRATE_BIN := migrate
MIGRATIONS_DIR := internal/app/lahijan/database/migrations

.PHONY: db-up db-down db-new db-force db-version
db-up: ## Apply all pending migrations up
	$(MIGRATE_BIN) -path $(MIGRATIONS_DIR) -database "$(DB_URL)" up

db-down: ## Roll back the last migration
	$(MIGRATE_BIN) -path $(MIGRATIONS_DIR) -database "$(DB_URL)" down 1

db-new: ## Create a new migration: make db-new NAME=create_foo
	$(MIGRATE_BIN) create -dir $(MIGRATIONS_DIR) -ext sql -seq $(NAME)

db-force: ## Force a specific migration version: make db-force V=42
	$(MIGRATE_BIN) -path $(MIGRATIONS_DIR) -database "$(DB_URL)" force $(V)

db-version: ## Show current migration version
	$(MIGRATE_BIN) -path $(MIGRATIONS_DIR) -database "$(DB_URL)" version

.PHONY: sqlc
sqlc: ## Regenerate sqlc code
	sqlc generate

# ---------------------------------------------------------------------------
# Docker / Compose
# ---------------------------------------------------------------------------

COMPOSE := docker compose -f deployments/docker-compose.dev.yml

.PHONY: docker-build
docker-build: ## Build the production docker image
	docker build --build-arg LAHIJAN_IMAGE_TAG=$(VERSION) -t lahijan:$(VERSION) .

.PHONY: dev-up
dev-up: ## Start dev dependencies (Postgres, etc.) via docker compose
	$(COMPOSE) up -d

.PHONY: dev-down
dev-down: ## Stop dev dependencies
	$(COMPOSE) down

.PHONY: dev-logs
dev-logs: ## Tail dev dependency logs
	$(COMPOSE) logs -f

# ---------------------------------------------------------------------------
# Repo hygiene
# ---------------------------------------------------------------------------

.PHONY: hooks
hooks: ## Install git hooks locally
	@./githooks/setup-hook.sh 2>/dev/null || powershell -ExecutionPolicy Bypass -File ./githooks/setup-hook.ps1
	@echo "git hooks installed"

.PHONY: ci-check
ci-check: lint test ## Run everything CI runs locally (lint + test)
	@echo "ci-check passed"

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR) coverage.out

# ---------------------------------------------------------------------------
# Docs
# ---------------------------------------------------------------------------

.PHONY: docs-install
docs-install: ## Install Docusaurus deps (in docs-site/)
	cd docs-site && npm install

.PHONY: docs
docs: ## Serve docs site locally
	cd docs-site && npm start
