.DEFAULT_GOAL := help
SHELL := /bin/sh

# HOMEBREW_PREFIX ?= /home/linuxbrew/.linuxbrew
# BUN_INSTALL ?= $(HOME)/.bun
GO ?= go
GOFMT ?= gofmt
BUN ?= bun
# Use Go target names (for example, GOOS=darwin GOARCH=arm64).
GOOS ?= $(shell $(GO) env GOHOSTOS)
GOARCH ?= $(shell $(GO) env GOHOSTARCH)
BUILD_ARCHIVE = bin/streamline.$(GOOS).$(GOARCH).tar.gz
VERSION = $(shell cat VERSION)
BUILD_TIME = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT_HASH = $(shell git rev-parse HEAD 2>/dev/null || printf unknown)
VERSION_LDFLAGS = -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.commitHash=$(COMMIT_HASH)
PORT ?= 8080

# export PATH := $(HOMEBREW_PREFIX)/bin:$(BUN_INSTALL)/bin:$(PATH)
export GOCACHE := $(CURDIR)/.cache/go-build
export GOMODCACHE := $(CURDIR)/.cache/go-mod
export GOTOOLCHAIN := local
export CGO_ENABLED := 0
export STREAMLINE_API_PORT := $(PORT)

.PHONY: help setup dev dev-go dev-web build run check version-bump

help:
	@printf '%s\n' \
	  'make setup    Check global Go/Bun and install frontend dependencies' \
	  'make dev      Start Go and Vite together (Ctrl+C stops both)' \
	  'make dev-go   Build and start the development API' \
	  'make dev-web  Start Vite on localhost:5173' \
	  'make build    Build bin/streamline and bundle bin/streamline.<GOOS>.<GOARCH>.tar.gz' \
	  '              Optional target: make build GOOS=darwin GOARCH=arm64' \
	  'make run      Build and run the standalone binary' \
	  'make check    Run frontend type checks and Go static checks' \
	  'make version-bump  Increment the minor version in VERSION (0.1.0 -> 0.2.0)' \
	  'make serena   Start Serena MCP server'

version-bump:
	@awk -F. 'NF != 3 || $$0 !~ /^[0-9]+\.[0-9]+\.[0-9]+$$/ { exit 1 } \
	  { printf "%d.%d.0\n", $$1, $$2 + 1 } END { if (NR != 1) exit 1 }' VERSION > VERSION.tmp && \
	  mv VERSION.tmp VERSION || { rm -f VERSION.tmp; printf '%s\n' 'Failed to bump VERSION; expected major.minor.patch' >&2; exit 1; }
	@cat VERSION

setup:
	$(BUN) scripts/setup.mjs "$(GO)"
	$(BUN) install --frozen-lockfile

dev:
	$(BUN) run --bun dev

dev-go:
	@mkdir -p .cache/bin
	$(GO) build -tags=dev -ldflags "$(VERSION_LDFLAGS)" -o .cache/bin/streamline-dev ./cmd/streamline
	@exec .cache/bin/streamline-dev -port "$(PORT)" -config-dir "$(CURDIR)/.local/streamline"

dev-web:
	$(BUN) run --bun --filter @streamline/web dev

build:
	@printf 'Building Streamline for %s/%s\n' "$(GOOS)" "$(GOARCH)"
	$(BUN) run --bun --filter @streamline/web build
	@mkdir -p bin
	env GOOS="$(GOOS)" GOARCH="$(GOARCH)" $(GO) build -trimpath -ldflags "$(VERSION_LDFLAGS)" -o bin/streamline ./cmd/streamline
	tar -czf "$(BUILD_ARCHIVE)" -C bin streamline -C "$(CURDIR)" README.md
	@printf 'Build archive: %s\n' "$(BUILD_ARCHIVE)"

run: build
	@exec bin/streamline -port "$(PORT)"

check:
	$(BUN) run --bun --filter @streamline/web check
	$(BUN) run --bun --filter @streamline/web test
	$(GO) test -tags=dev ./...
	$(GO) vet -tags=dev ./...
	@unformatted="$$( $(GOFMT) -l cmd internal )" && test -z "$$unformatted" || \
	  { printf '%s\n' 'Go formatting check failed; run $(GOFMT) -w cmd internal'; exit 1; }

serena:
	uvx --from git+https://github.com/oraios/serena serena start-mcp-server --transport streamable-http --port 9121 --project-from-cwd --context codex