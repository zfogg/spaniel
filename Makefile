.PHONY: dev build build-server run test test-extensive test-storage generate verify-generated setup

# Use MSYS2 Bash when available; otherwise use Bash from PATH. Scoop's sh.exe
# shim may select a broken Zsh process on Windows.
ifeq ($(OS),Windows_NT)
ifeq ($(wildcard C:/msys64/usr/bin/bash.exe),)
SHELL := bash
else
SHELL := C:/msys64/usr/bin/bash.exe
endif
endif

# Keep the development server at a stable path.  Windows Defender Firewall
# associates an allow rule with the executable path; `go run` instead creates a
# fresh temporary executable on every invocation and repeatedly prompts.
BIN_DIR := bin
EXE :=
BIN := $(BIN_DIR)/spaniel$(EXE)

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
EXE := .exe
BIN := $(BIN_DIR)/spaniel$(EXE)
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
	  $(MAKE) build-server; \
	  ENV=dev ./$(BIN) --dev

build:
	cd frontend && pnpm build
	$(MAKE) build-server

build-server:
	mkdir -p $(BIN_DIR)
	go build -ldflags "-X main.version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o $(BIN) ./cmd/spaniel

run: build-server
	./$(BIN) $(ARGS)

verify-generated:
	$(MAKE) generate
	git diff --exit-code -- internal/storage/querygen
	test -z "$$(git ls-files --others --exclude-standard -- internal/storage/querygen)"

test: verify-generated
	@set -e; \
	  mkdir -p $(BIN_DIR); \
	  for package in $$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...); do \
	    package_dir=$$(go list -f '{{.Dir}}' "$$package"); \
	    test_name=$$(basename "$$package"); \
	    test_binary="$(CURDIR)/$(BIN_DIR)/$$test_name.test$(EXE)"; \
	    go test -c -o "$$test_binary" "$$package"; \
	    ( cd "$$package_dir" && "$$test_binary" -test.timeout=10m ); \
	  done

# Full local confidence suite. This mirrors the test coverage in CI, while
# keeping all Go invocations behind Make so Windows uses MSYS2 UCRT64 GCC.
# Playwright must already have its Chromium browser installed.
test-extensive: test
	go test ./internal/... ./cmd/... -race -count=1
	cd frontend && pnpm test
	cd frontend && pnpm e2e

# Focused backend verification for storage work when the embedded frontend
# artifact has not been built in an isolated worktree.
test-storage: verify-generated
	go test ./internal/storage

generate:
	go run ./cmd/genquery
	go run ./cmd/genschema

setup:
	git config core.hooksPath git-hooks
