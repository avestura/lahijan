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

# OpenAPI codegen tool versions (pinned for reproducibility; bump together).
OAPI_CODEGEN_VERSION := v2.4.1
OPENAPI_TS_VERSION   := 7.13.0
SPEC                 := api/openapi.yaml

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
	@echo "Frontend (web/ dashboard SPA):"
	@echo "  make web-install  Install web/ dependencies"
	@echo "  make web-build    Build the dashboard (tsc + vite build)"
	@echo "  make web-lint     Lint (ESLint + Prettier)"
	@echo "  make web-test     Run Vitest"
	@echo "  make web-typecheck TypeScript strict check"
	@echo "  make web-dev      Start the Vite dev server"
	@echo "  make web-i18n-check   Assert en.json/fa.json key sync"
	@echo "  make web-openapi-check Assert OpenAPI schema sync"
	@echo ""
	@echo "Frontend (website/ marketing SPA):"
	@echo "  make website-install  Install website/ dependencies"
	@echo "  make website-build    Build the marketing site (tsc + vite-react-ssg)"
	@echo "  make website-lint     Lint (ESLint + Prettier)"
	@echo "  make website-test     Run Vitest"
	@echo "  make website-typecheck TypeScript strict check"
	@echo "  make website-dev      Start the Vite dev server"
	@echo "  make website-i18n-check   Assert en.json/fa.json key sync"
	@echo ""
	@echo "Database (WS-03+):"
	@echo "  make db-up         Apply all pending migrations"
	@echo "  make db-down       Roll back the last migration"
	@echo "  make db-new NAME=create_foo    Create a new migration"
	@echo "  make db-version    Show current migration version"
	@echo "  make sqlc          Regenerate sqlc code (needs sqlc on PATH)"
	@echo "  make sqlc-docker   Regenerate sqlc code via the docker image"
	@echo ""
	@echo "OpenAPI (WS-05+):"
	@echo "  make openapi-gen    Regenerate Go + TS code from api/openapi.yaml"
	@echo "  make openapi-verify Fail if generated code has drifted from the spec"
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
# OpenAPI codegen (WS-05)
#
# `api/openapi.yaml` is the source of truth (ADR-0015). We generate:
#   - Go server types + Fiber interface  -> api/gen/go/         (oapi-codegen)
#   - Go client SDK                      -> pkg/lahijan-client/ (oapi-codegen)
#   - TypeScript schema types            -> api/gen/ts/         (openapi-typescript)
# The openapi-fetch client wrapper under api/gen/ts-client/ is hand-written and
# consumes the generated TS schema; regenerate it only if the wrapper API changes.
#
# CI runs `make openapi-verify` and fails if the committed generated code drifts
# from the spec.
# ---------------------------------------------------------------------------

.PHONY: openapi-gen
openapi-gen: ## Regenerate Go + TypeScript code from api/openapi.yaml
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) \
		--config api/oapi-codegen.server.yaml $(SPEC)
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) \
		--config api/oapi-codegen.client.yaml $(SPEC)
	npx -y openapi-typescript@$(OPENAPI_TS_VERSION) $(SPEC) -o api/gen/ts/schema.d.ts
	@echo "openapi: regenerated Go server, Go client SDK, and TS schema"

.PHONY: openapi-verify
openapi-verify: openapi-gen ## Fail if committed generated code has drifted from the spec
	@git --no-pager diff --exit-code -- api/gen pkg/lahijan-client
	@echo "openapi: generated code is up to date"

# ---------------------------------------------------------------------------
# Frontend (web/ dashboard SPA — wired in WS-18)
#
# All npm commands run inside web/. CI mirrors these targets via the
# .github/workflows/frontend.yml workflow.
# ---------------------------------------------------------------------------

WEB_DIR := web
NPM := npm --prefix $(WEB_DIR)

.PHONY: web-install web-build web-lint web-test web-dev web-format web-typecheck
web-install: ## Install frontend dependencies (web/)
	$(NPM) install

web-build: ## Build frontend (web/) — tsc --noEmit + vite build
	$(NPM) run build

web-lint: ## Lint frontend (web/) — ESLint + Prettier
	$(NPM) run lint
	$(NPM) run format:check

web-test: ## Test frontend (web/) — Vitest
	$(NPM) run test

web-typecheck: ## TypeScript check (web/) — strict mode, no emit
	$(NPM) run typecheck

web-dev: ## Start the Vite dev server (web/)
	$(NPM) run dev

web-format: ## Format frontend (web/) — Prettier write
	$(NPM) run format

.PHONY: web-i18n-check web-openapi-check
web-i18n-check: ## Assert en.json and fa.json are key-for-key in sync
	$(NPM) run i18n:check

web-openapi-check: ## Assert openapi-fetch schema is in sync with api/openapi.yaml
	$(NPM) run openapi:check

# ---------------------------------------------------------------------------
# Frontend (website/ marketing SPA — wired in WS-19)
#
# Mirrors the web-* targets. The marketing site prerenders via
# vite-react-ssg at build time (see ADR-0028). CI mirrors these targets
# via the .github/workflows/frontend.yml workflow.
# ---------------------------------------------------------------------------

WEBSITE_DIR := website
NPM_WEBSITE := npm --prefix $(WEBSITE_DIR)

.PHONY: website-install website-build website-lint website-test website-dev website-format website-typecheck website-i18n-check
website-install: ## Install frontend dependencies (website/)
	$(NPM_WEBSITE) install

website-build: ## Build frontend (website/) — tsc --noEmit + vite-react-ssg build + sitemap
	$(NPM_WEBSITE) run build

website-lint: ## Lint frontend (website/) — ESLint + Prettier
	$(NPM_WEBSITE) run lint
	$(NPM_WEBSITE) run format:check

website-test: ## Test frontend (website/) — Vitest
	$(NPM_WEBSITE) run test

website-typecheck: ## TypeScript check (website/) — strict mode, no emit
	$(NPM_WEBSITE) run typecheck

website-dev: ## Start the Vite dev server (website/)
	$(NPM_WEBSITE) run dev

website-format: ## Format frontend (website/) — Prettier write
	$(NPM_WEBSITE) run format

website-i18n-check: ## Assert en.json and fa.json are key-for-key in sync (website/)
	$(NPM_WEBSITE) run i18n:check

# ---------------------------------------------------------------------------
# Database (no-ops until WS-03; commands defined so help text is stable)
# ---------------------------------------------------------------------------

MIGRATE_BIN := migrate
MIGRATIONS_DIR := internal/app/lahijan/database/migrations
SQLC_DIR := internal/app/lahijan/database

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
sqlc: ## Regenerate sqlc code (sqlc must be on PATH, or run via docker — see docs)
	cd $(SQLC_DIR) && sqlc generate

.PHONY: sqlc-docker
sqlc-docker: ## Regenerate sqlc code via the official docker image (no local install needed)
	docker run --rm -v "$(CURDIR)/$(SQLC_DIR):/src" -w /src sqlc/sqlc:1.27.0 generate

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
ci-check: lint test openapi-verify i18n-verify ## Run everything CI runs locally (lint + test + openapi + i18n drift)
	@echo "ci-check passed"

.PHONY: i18n-verify
i18n-verify: ## Fail if en.json and fa.json locale keys have drifted out of sync
	$(GO) test $(GOFLAGS) -run TestLocaleKeys_enAndFaInSync ./internal/app/lahijan/i18n/
	@echo "i18n: en.json and fa.json keys are in sync"

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
