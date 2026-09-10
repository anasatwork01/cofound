# Halyard - monorepo task runner.
# Every target must work from a clean checkout after `make bootstrap`.
.DEFAULT_GOAL := help
SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

GO_SERVICES := api gitd aigw mcp
GO_MODULES  := $(shell go list -m -f '{{.Dir}}' 2>/dev/null)

# Ports match compose.yaml and are deliberately not 5432/6379 — see the note
# there. `?=` so an exported DATABASE_URL/REDIS_URL (as CI sets) takes
# precedence over these local defaults.
DATABASE_URL ?= postgres://halyard:halyard@localhost:55432/halyard
REDIS_URL    ?= redis://localhost:56379/0
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS     := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: doctor
doctor: ## Check the local toolchain against .tool-versions
	@./scripts/doctor.sh

.PHONY: bootstrap
bootstrap: doctor hooks ## Install all dependencies (JS, Python, Go) and git hooks
	pnpm install
	uv sync --all-packages
	go work sync

.PHONY: hooks
hooks: ## Install the repo's git hooks (see .githooks/)
	@git config core.hooksPath .githooks
	@echo "  git hooks -> .githooks (pre-push: protects main, validates branch names)"

.PHONY: check-branch
check-branch: ## Validate the current branch name against CONTRIBUTING.md
	@./scripts/check-branch.sh

.PHONY: verify
verify: structure gen-check lint typecheck test build ## Everything CI runs except integration

.PHONY: structure
structure: ## Assert the repo layout matches SPEC §4
	@./scripts/check-structure.sh

.PHONY: lint
lint: lint-go lint-py lint-js ## Lint every language

.PHONY: lint-go
lint-go: ## gofmt + go vet
	@if [ -z "$(GO_MODULES)" ]; then \
		echo "  no Go modules resolved: 'go list -m' produced nothing."; \
		echo "  Run 'make doctor' — a stale GOROOT export breaks every go command,"; \
		echo "  and this target would otherwise run nothing and report success."; \
		exit 1; \
	fi
	@for d in $(GO_MODULES); do \
		echo "  go   $${d#$(CURDIR)/}"; \
		out=$$(gofmt -l "$$d"); \
		if [ -n "$$out" ]; then echo "$$out"; echo "gofmt: needs formatting (run make fmt)"; exit 1; fi; \
		(cd "$$d" && go vet ./...); \
	done

.PHONY: lint-py
lint-py: ## ruff check + format check
	uv run ruff check .
	uv run ruff format --check .

.PHONY: lint-js
lint-js: ## prettier check
	pnpm run lint

.PHONY: typecheck
typecheck: typecheck-py ## Static type checks (typecheck-js arrives with task 0.11)

.PHONY: typecheck-py
typecheck-py: ## mypy, strict
	uv run mypy services/sandboxd/src services/workers/src

.PHONY: test
test: test-go test-py ## Run unit tests (no services needed)

.PHONY: test-go
test-go: ## Go unit tests
	@if [ -z "$(GO_MODULES)" ]; then \
		echo "  no Go modules resolved: 'go list -m' produced nothing."; \
		echo "  Run 'make doctor' — a stale GOROOT export breaks every go command,"; \
		echo "  and this target would otherwise run nothing and report success."; \
		exit 1; \
	fi
	@for d in $(GO_MODULES); do \
		echo "  go test $${d#$(CURDIR)/}"; \
		(cd "$$d" && go test ./...); \
	done

.PHONY: test-py
test-py: ## Python unit tests
	uv run pytest -m "not integration"

.PHONY: test-integration
test-integration: ## Integration tests against Postgres + Redis (make services-up first)
	DATABASE_URL="$(DATABASE_URL)" REDIS_URL="$(REDIS_URL)" uv run pytest -m integration

.PHONY: services-up
services-up: ## Start Postgres and Redis, waiting for health
	docker compose up --detach --wait
	@echo "  postgres $(DATABASE_URL)"
	@echo "  redis    $(REDIS_URL)"

.PHONY: services-down
services-down: ## Stop them (ARGS=-v also drops the data volume)
	docker compose down $(ARGS)

.PHONY: build
build: build-go ## Build all binaries into bin/

.PHONY: build-go
build-go: ## Build the Go services into bin/
	@mkdir -p bin
	@for s in $(GO_SERVICES); do \
		echo "  building $$s"; \
		go build -ldflags "$(LDFLAGS)" -o bin/$$s ./services/$$s/cmd/$$s; \
	done
	@echo "  building agentd"
	@go build -ldflags "$(LDFLAGS)" -o bin/agentd ./agent/agentd/cmd/agentd

.PHONY: fmt
fmt: ## Auto-format everything
	@for d in $(GO_MODULES); do gofmt -w "$$d"; done
	uv run ruff format .
	uv run ruff check --fix .
	pnpm run format

.PHONY: gen
gen: ## Regenerate TypeScript, Go and Python from packages/schema
	@./scripts/gen.sh

.PHONY: gen-check
gen-check: ## Fail if the checked-in bindings disagree with the schemas
	@./scripts/gen.sh >/dev/null
# Compares regenerated output against what is checked in, which means the
# working tree against the index rather than against HEAD. Using `git status`
# here would reject a schema change staged together with its own regenerated
# output — the correct shape for such a commit.
	@if ! git diff --quiet -- packages/schema/gen || \
	    [ -n "$$(git ls-files --others --exclude-standard -- packages/schema/gen)" ]; then \
		echo "  generated bindings are stale."; \
		echo "  Run 'make gen' and commit the result alongside the schema change."; \
		git diff --stat -- packages/schema/gen; \
		git ls-files --others --exclude-standard -- packages/schema/gen | sed 's/^/  untracked: /'; \
		exit 1; \
	fi
	@echo "  generated bindings match the schemas"

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist coverage .turbo
	find . -name __pycache__ -type d -prune -not -path './agent/opencode/*' -exec rm -rf {} +
