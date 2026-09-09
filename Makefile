# Halyard - monorepo task runner.
# Every target must work from a clean checkout after `make bootstrap`.
.DEFAULT_GOAL := help
SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

GO_SERVICES := api gitd aigw mcp
GO_MODULES  := $(shell go list -m -f '{{.Dir}}' 2>/dev/null)
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
bootstrap: doctor ## Install all dependencies (JS, Python, Go)
	pnpm install
	uv sync --all-packages
	go work sync

.PHONY: verify
verify: structure lint typecheck test build ## Everything CI runs

.PHONY: structure
structure: ## Assert the repo layout matches SPEC §4
	@./scripts/check-structure.sh

.PHONY: lint
lint: ## Lint every language
	@for d in $(GO_MODULES); do \
		echo "  go   $${d#$(CURDIR)/}"; \
		out=$$(gofmt -l "$$d"); \
		if [ -n "$$out" ]; then echo "$$out"; echo "gofmt: needs formatting (run make fmt)"; exit 1; fi; \
		(cd "$$d" && go vet ./...); \
	done
	uv run ruff check .
	uv run ruff format --check .
	pnpm run lint

.PHONY: typecheck
typecheck: ## Static type checks
	uv run mypy services/sandboxd/src services/workers/src

.PHONY: test
test: ## Run unit tests
	@for d in $(GO_MODULES); do \
		echo "  go test $${d#$(CURDIR)/}"; \
		(cd "$$d" && go test ./...); \
	done
	uv run pytest

.PHONY: build
build: ## Build all Go binaries into bin/
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
gen: ## Regenerate cross-language types from packages/schema (task 0.3)
	@echo "not implemented - see docs/TASKS.md task 0.3"
	@exit 1

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist coverage .turbo
	find . -name __pycache__ -type d -prune -not -path './agent/opencode/*' -exec rm -rf {} +
