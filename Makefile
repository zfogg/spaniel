.PHONY: dev build run test setup

# Version string baked into the binary: the latest git tag (e.g. v0.2.1), with
# a -N-gSHA suffix for commits past the tag and -dirty for uncommitted changes.
# Falls back to the short commit hash when no tags exist, then to "dev".
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

dev:
	@# Run vite in the background, wait until it answers, then start spaniel
	@# in --dev mode (which reverse-proxies UI requests to vite:5173 for live
	@# reload). `trap 'kill 0'` makes the vite child die with the make
	@# process instead of orphaning when you Ctrl-C spaniel.
	@trap 'kill 0' EXIT INT TERM; \
	  ( cd frontend && pnpm dev --host 127.0.0.1 ) & \
	  printf "waiting for vite…"; \
	  until curl -sf http://localhost:5173 >/dev/null 2>&1; do printf '.'; sleep 0.3; done; \
	  echo " ready"; \
	  ENV=dev go run -ldflags "-X main.version=$(VERSION)" ./cmd/spaniel --dev

build:
	cd frontend && pnpm build
	go build -ldflags "-X main.version=$(VERSION)" -o spaniel ./cmd/spaniel

run:
	go run -ldflags "-X main.version=$(VERSION)" ./cmd/spaniel

test:
	go test ./...

setup:
	git config core.hooksPath git-hooks
