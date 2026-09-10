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

# The role services connect as. It is NOT the role migrations run as, and that
# distinction is the whole tenant isolation control: the owner and any superuser
# bypass row-level security silently (see db/migrations/00013). This password is
# local-only, exactly like the halyard:halyard above, and the migration ships
# the role NOLOGIN so nothing is granted a login by deploying.
APP_DATABASE_URL ?= postgres://halyard_app:halyard_app@localhost:55432/halyard
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
	DATABASE_URL="$(DATABASE_URL)" APP_DATABASE_URL="$(APP_DATABASE_URL)" \
		REDIS_URL="$(REDIS_URL)" uv run pytest -m integration

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
#
# The paths come from gen_obs.py rather than being listed here: the observability
# vocabulary generates into packages/chassis and packages/pychassis, and a
# generated file this check does not cover is one that can silently go stale,
# which is the entire failure this target exists to prevent.
	@paths="packages/schema/gen $$(./scripts/gen_obs.py --outputs)"; \
	if ! git diff --quiet -- $$paths || \
	    [ -n "$$(git ls-files --others --exclude-standard -- $$paths)" ]; then \
		echo "  generated bindings are stale."; \
		echo "  Run 'make gen' and commit the result alongside the schema change."; \
		git diff --stat -- $$paths; \
		git ls-files --others --exclude-standard -- $$paths | sed 's/^/  untracked: /'; \
		exit 1; \
	fi
	@echo "  generated bindings match the schemas"

# --------------------------------------------------------------- database

.PHONY: db-migrate
db-migrate: ## Apply pending migrations to $(DATABASE_URL)
	@cd packages/db && DATABASE_URL="$(DATABASE_URL)" \
		go run ./cmd/migrate -dir ../../db/migrations -command up

.PHONY: db-status
db-status: ## Show which migrations have been applied
	@cd packages/db && DATABASE_URL="$(DATABASE_URL)" \
		go run ./cmd/migrate -dir ../../db/migrations -command status

.PHONY: db-validate
db-validate: ## Check the migration files parse, without a database
	@cd packages/db && go run ./cmd/migrate -dir ../../db/migrations -command validate

.PHONY: db-setup
db-setup: db-migrate ## Migrate, then give halyard_app a local login
# The migration creates the role NOLOGIN and with no password, because a
# credential in a migration is a credential in git. Granting login is an
# operator action; locally it is this.
	@psql "$(DATABASE_URL)" -q -c \
		"alter role halyard_app with login password 'halyard_app'" \
	  && echo "  halyard_app can log in locally: $(APP_DATABASE_URL)"

.PHONY: db-reset
db-reset: ## Drop and recreate the local database, then migrate
# The counterpart to there being no `-- +goose Down` anywhere: migrations are
# forward-only (SPEC §0 rule 6), so local iteration resets rather than rolls
# back. Refuses to touch anything that is not the local compose database.
	@case "$(DATABASE_URL)" in \
	  *@localhost:55432/*|*@127.0.0.1:55432/*) ;; \
	  *) echo "  refusing: DATABASE_URL is not the local compose database"; exit 1 ;; \
	esac
	@psql "$(DATABASE_URL)" -q -c \
		"drop schema public cascade; create schema public; grant all on schema public to halyard" \
	  && echo "  schema dropped"
	@$(MAKE) --no-print-directory db-setup

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist coverage .turbo
	find . -name __pycache__ -type d -prune -not -path './agent/opencode/*' -exec rm -rf {} +
