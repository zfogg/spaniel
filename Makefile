.PHONY: dev build run test test-storage generate setup

# Version string baked into the binary: the latest git tag (e.g. v0.2.1), with
# a -N-gSHA suffix for commits past the tag and -dirty for uncommitted changes.
# Falls back to the short commit hash when no tags exist, then to "dev".
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BIN := spaniel

# DuckDB's prebuilt Windows archive is compiled for the MSYS2 UCRT ABI.  The
# Scoop MinGW compiler has a different libstdc++ ABI and fails at link time
# (for example, resolving basic_streambuf::seekpos).  Put UCRT64 first so
# every Go/CGo command launched by Make uses the compatible compiler and its
# runtime DLLs.
ifeq ($(OS),Windows_NT)
UCRT64_BIN := C:/msys64/ucrt64/bin
ifeq ($(wildcard $(UCRT64_BIN)/gcc.exe),)
$(error Spaniel requires MSYS2 UCRT64 GCC for DuckDB on Windows. Install it with: C:/msys64/usr/bin/bash.exe -lc 'pacman -S mingw-w64-ucrt-x86_64-gcc')
endif
export PATH := $(UCRT64_BIN);$(PATH)
export CC := $(UCRT64_BIN)/gcc.exe
BIN := spaniel.exe
endif

dev:
	@# Run vite in the background, wait until it answers, then start spaniel
	@# in --dev mode (which reverse-proxies UI requests to vite:5173 for live
	@# reload). `trap 'kill 0'` makes the vite child die with the make
	@# process instead of orphaning when you Ctrl-C spaniel.
	@trap 'kill 0' EXIT INT TERM; \
	  ( cd frontend && SPANIEL_API_URL=http://localhost:8080 pnpm dev --host 127.0.0.1 ) & \
	  printf "waiting for vite…"; \
	  until curl -sf http://localhost:5173 >/dev/null 2>&1; do printf '.'; sleep 0.3; done; \
	  echo " ready"; \
	  ENV=dev go run -ldflags "-X main.version=$(VERSION)" ./cmd/spaniel --dev

build:
	cd frontend && pnpm build
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/spaniel

run:
	go run -ldflags "-X main.version=$(VERSION)" ./cmd/spaniel

test:
	go test ./...

# Focused backend verification for storage work when the embedded frontend
# artifact has not been built in an isolated worktree.
test-storage:
	go test ./internal/storage

generate:
	go run ./cmd/genquery

setup:
	git config core.hooksPath git-hooks
