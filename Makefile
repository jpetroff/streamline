.DEFAULT_GOAL := help
SHELL := /bin/sh

# HOMEBREW_PREFIX ?= /home/linuxbrew/.linuxbrew
# BUN_INSTALL ?= $(HOME)/.bun
GO ?= go
GOFMT ?= gofmt
BUN ?= bun
PORT ?= 8080

# export PATH := $(HOMEBREW_PREFIX)/bin:$(BUN_INSTALL)/bin:$(PATH)
export GOCACHE := $(CURDIR)/.cache/go-build
export GOMODCACHE := $(CURDIR)/.cache/go-mod
export GOTOOLCHAIN := local
export CGO_ENABLED := 0
export STREAMLINE_API_PORT := $(PORT)

.PHONY: help setup dev dev-go dev-web build run check

help:
	@printf '%s\n' \
	  'make setup    Check global Go/Bun and install frontend dependencies' \
	  'make dev      Start Go and Vite together (Ctrl+C stops both)' \
	  'make dev-go   Build and start the development API' \
	  'make dev-web  Start Vite on localhost:5173' \
	  'make build    Build bin/streamline with the frontend embedded' \
	  'make run      Build and run the standalone binary' \
	  'make check    Run frontend type checks and Go static checks' \
	  'make serena   Start Serena MCP server'

setup:
	$(BUN) scripts/setup.mjs "$(GO)"
	$(BUN) install --frozen-lockfile

dev:
	$(BUN) run --bun dev

dev-go:
	@mkdir -p .cache/bin
	$(GO) build -tags=dev -o .cache/bin/streamline-dev ./cmd/streamline
	@exec .cache/bin/streamline-dev -port "$(PORT)"

dev-web:
	$(BUN) run --bun --filter @streamline/web dev

build:
	$(BUN) run --bun --filter @streamline/web build
	@mkdir -p bin
	$(GO) build -trimpath -o bin/streamline ./cmd/streamline

run: build
	@exec bin/streamline -port "$(PORT)"

check:
	$(BUN) run --bun --filter @streamline/web check
	$(GO) vet -tags=dev ./...
	@unformatted="$$( $(GOFMT) -l cmd internal )" && test -z "$$unformatted" || \
	  { printf '%s\n' 'Go formatting check failed; run $(GOFMT) -w cmd internal'; exit 1; }

serena:
	uvx --from git+https://github.com/oraios/serena serena start-mcp-server --transport streamable-http --port 9121 --project-from-cwd --context codex