.PHONY: dev build build-server run test test-coverage test-extensive test-storage test-ingestion test-mcp-e2e test-telemetry generate verify-generated setup

.DEFAULT_GOAL := dev

.PHONY: test-security lint-go
test-security:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/security.test$(EXE) ./internal/security
	./$(BIN_DIR)/security.test$(EXE) -test.count=1 -test.timeout=3m

lint-go:
	go vet ./...

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
# Generators can use an explicitly selected Go launcher without changing the
# workstation's global toolchain (for example a bounded container wrapper).
GO ?= go
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
$(error Spaniel requires MSYS2 UCRT64 GCC 15 at C:/msys64 for DuckDB on Windows. See AGENTS.md for the compatible version; GCC 16 is incompatible with the prebuilt archive)
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

.PHONY: build-assert test-assert
build-assert:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/spaniel-assert$(EXE) ./cmd/spaniel-assert

test-assert:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/traceassert.test$(EXE) ./traceassert
	./$(BIN_DIR)/traceassert.test$(EXE) -test.count=1 -test.timeout=2m
	go test -c -o $(BIN_DIR)/assert-cli.test$(EXE) ./cmd/spaniel-assert
	./$(BIN_DIR)/assert-cli.test$(EXE) -test.count=1 -test.timeout=2m

run: build-server
	./$(BIN) $(ARGS)

verify-generated:
	$(MAKE) generate
	git diff --exit-code -- api/openapi.json api/openapi.yaml frontend/src/generated/liveevent.ts frontend/src/generated/openapi.ts internal/storage/querygen internal/security/querygen internal/api/apigen internal/api/openapi_adapter.gen.go
	test -z "$$(git ls-files --others --exclude-standard -- api/openapi.yaml frontend/src/generated/liveevent.ts frontend/src/generated/openapi.ts internal/storage/querygen internal/security/querygen internal/api/apigen internal/api/openapi_adapter.gen.go)"

.PHONY: test-linter
test-linter:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/linter.test$(EXE) ./internal/ingestion/linter
	./$(BIN_DIR)/linter.test$(EXE) -test.count=1 -test.timeout=2m

test: verify-generated
	@set -e; \
	  mkdir -p $(BIN_DIR); \
	  packages="$$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)"; \
	  for package in $$packages; do \
	    package_dir=$$(go list -f '{{.Dir}}' "$$package"); \
	    test_name=$$(basename "$$package"); \
	    test_binary="$(CURDIR)/$(BIN_DIR)/$$test_name.test$(EXE)"; \
	    go test -c -o "$$test_binary" "$$package"; \
	    ( cd "$$package_dir" && "$$test_binary" -test.timeout=10m ); \
	  done

# Focused coverage-spec contract check. Kept in the Makefile so Windows uses
# the same UCRT64 toolchain as the routine suite without waiting on unrelated
# long-running integration packages.
test-coverage: verify-generated
	@set -e; \
	  mkdir -p $(BIN_DIR); \
	  for package in ./internal/coverage ./internal/api; do \
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

# Retention/reclamation regressions without unrelated code generation. No
# generator inputs change in this storage-only check; use test for the full suite.
.PHONY: test-retention
RETENTION_TEST_RUN ?= TestNativePhysical|TestPhysical|TestPrune|TestRetention|TestCompact|TestMaintenance|TestStorageGuard|TestGetStorage|TestSession|TestReclaim|TestWALDeferral|TestBatcherMaintenance
test-retention:
	go test ./internal/storage ./cmd/spaniel -run '$(RETENTION_TEST_RUN)' -count=1 -timeout=10m -v

.PHONY: test-tracing-gap
test-tracing-gap:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/tracing-gap.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/tracing-gap.test$(EXE) -test.run 'TestTracingGap' -test.count=1 -test.v

.PHONY: test-error-chain
test-error-chain:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/error-chain.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/error-chain.test$(EXE) -test.run 'TestErrorChain' -test.count=1 -test.v

.PHONY: test-ingest-admission
test-ingest-admission:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/ingest-admission.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/ingest-admission.test$(EXE) -test.run 'TestIngestAdmission|TestTracingGap|TestSynchronousIO|TestHTTP|TestDB' -test.count=1 -test.v

.PHONY: test-ingest-backpressure
BACKPRESSURE_TEST_RUN ?= TestIngestMetadata|TestPipelineIngestTraces_Span|TestBatcher|TestMaintenance|TestRetention
test-ingest-backpressure:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/ingest-backpressure.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/ingest-backpressure.test$(EXE) -test.run '$(BACKPRESSURE_TEST_RUN)' -test.count=1 -test.timeout=5m -test.v
	go test -race -c -o $(BIN_DIR)/storage-backpressure.test$(EXE) ./internal/storage
	./$(BIN_DIR)/storage-backpressure.test$(EXE) -test.run '$(BACKPRESSURE_TEST_RUN)' -test.count=1 -test.timeout=5m -test.v

.PHONY: test-ingest-batching
test-ingest-batching:
	$(MAKE) test-ingest-backpressure BACKPRESSURE_TEST_RUN='TestSharedBatch|TestIngestAdmission|TestIngestTraces_Nested|TestIngestMetadata|TestBatcher'
	go test -race -c -o $(BIN_DIR)/batch-receiver.test$(EXE) ./internal/receiver
	./$(BIN_DIR)/batch-receiver.test$(EXE) -test.run 'TestBatchAdmission' -test.count=1 -test.timeout=5m -test.v
	$(MAKE) test-ingest-shutdown

.PHONY: test-ingest-shutdown
test-ingest-shutdown:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/batch-shutdown.test$(EXE) ./cmd/spaniel
	./$(BIN_DIR)/batch-shutdown.test$(EXE) -test.run 'TestRun_ShutsDownCleanlyOnSignal|TestRunSelfMonitoringShutdown' -test.count=1 -test.timeout=1m -test.v

# Explicit representative-size qualification; ordinary batching tests use a
# smaller populated fixture to keep the routine suite inexpensive.
.PHONY: test-ingest-large-db
test-ingest-large-db:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/batch-large-db.test$(EXE) ./internal/ingestion
	SPANIEL_LARGE_DB_REGRESSION=1 ./$(BIN_DIR)/batch-large-db.test$(EXE) -test.run '^TestSharedBatchPopulatedDatabase$$' -test.count=1 -test.timeout=15m -test.v

.PHONY: test-dashboard-reads
test-dashboard-reads:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/dashboard-api.test$(EXE) ./internal/api
	./$(BIN_DIR)/dashboard-api.test$(EXE) -test.count=1 -test.timeout=10m
	go test -race -c -o $(BIN_DIR)/dashboard-storage.test$(EXE) ./internal/storage
	./$(BIN_DIR)/dashboard-storage.test$(EXE) -test.count=1 -test.timeout=10m
	go test -race -c -o $(BIN_DIR)/dashboard-ws.test$(EXE) ./internal/ws
	./$(BIN_DIR)/dashboard-ws.test$(EXE) -test.count=1 -test.timeout=5m
	go test -race -c -o $(BIN_DIR)/dashboard-live.test$(EXE) ./internal/live
	./$(BIN_DIR)/dashboard-live.test$(EXE) -test.count=1 -test.timeout=5m
	$(MAKE) test-dashboard-cli

.PHONY: test-dashboard-cli
test-dashboard-cli:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/dashboard-cli.test$(EXE) ./cmd/spaniel
	(cd cmd/spaniel && "$(CURDIR)/$(BIN_DIR)/dashboard-cli.test$(EXE)" -test.count=1 -test.timeout=10m)

generate:
	$(GO) run ./cmd/genliveevent
	cd frontend && pnpm exec oxfmt --write src/generated/liveevent.ts
	$(GO) run ./cmd/genquery
	$(GO) run ./cmd/genschema
	cd frontend && pnpm exec oxfmt --write src/generated/schemaCatalog.ts
	$(MAKE) generate-openapi

# OpenAPI is the canonical HTTP contract. Regenerate both server and browser
# bindings together so neither side can silently drift from the document.
generate-openapi:
	$(GO) run ./cmd/openapiyaml
	$(GO) tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.json
	$(GO) run ./cmd/genopenapi
	cd frontend && pnpm run generate:openapi

setup:
	git config core.hooksPath git-hooks

# Focused protocol-lint regression check, using the same Windows CGO toolchain.
test-ingestion:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/ingestion.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/ingestion.test$(EXE) -test.run 'TestHTTP|TestDB|TestSerialPromise|TestPartialTrace|TestStorageQueryName|TestChildStartingBeforeParent|TestNegativeDuration|TestPipelineIngestTraces_LintsNegativeTemporalSchedulingDuration|TestRedelivery' -test.v

.PHONY: test-query-diagnostics
.PHONY: test-query-analysis
test-query-analysis:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/query-analysis.test$(EXE) ./internal/querydiagnostics
	(cd internal/querydiagnostics && "$(CURDIR)/$(BIN_DIR)/query-analysis.test$(EXE)" -test.count=1 -test.timeout=2m)
	go test -c -o $(BIN_DIR)/query-counts.test$(EXE) ./internal/storage
	./$(BIN_DIR)/query-counts.test$(EXE) -test.run 'TestSessionQueryOccurrences|TestNamedHelpersPrefixSimpleNames|TestReadOnlyQueryNamesAndSanitizesUserSQL' -test.count=1 -test.timeout=2m
	go test -c -o $(BIN_DIR)/query-analysis-api.test$(EXE) ./internal/api
	./$(BIN_DIR)/query-analysis-api.test$(EXE) -test.run 'TestSQLAnalyzeAPI|TestTraceCaptureIncludes|TestSettingsStorageSharesRequestTrace|TestOpenAPI' -test.count=1 -test.timeout=3m
	go test -c -o $(BIN_DIR)/query-fingerprints.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/query-fingerprints.test$(EXE) -test.run 'TestN1|TestFingerprintSQL' -test.count=1 -test.timeout=3m

# Optional bounded real-DuckDB probe export: SPANIEL_DIAGNOSTICS_PROBE_ENDPOINT
# is a local OTLP gRPC endpoint. No deliberately malformed SQL runs in servers.
test-query-diagnostics:
	@set -e; \
	  mkdir -p $(BIN_DIR); \
	  for package in querydiagnostics ingestion storage api mcp; do \
	    go test -c -o $(BIN_DIR)/diagnostics-$$package.test$(EXE) ./internal/$$package; \
	    ./$(BIN_DIR)/diagnostics-$$package.test$(EXE) -test.run 'TestNormalize|TestQueryDiagnostics' -test.count=1 -test.timeout=3m -test.v; \
	  done

.PHONY: test-query-name-race
.PHONY: test-query-names
test-query-names:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/querynames.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/querynames.test$(EXE) -test.run 'TestStorageQueryName' -test.count=1 -test.v

test-query-name-race:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/querynames.test$(EXE) ./internal/ingestion
	./$(BIN_DIR)/querynames.test$(EXE) -test.run 'TestStorageQueryName' -test.v

# Exercise the real Streamable HTTP boundary with the reference MCP Inspector
# CLI. Inspector requires Node.js 22.19.0 or newer; npx caches the pinned package.
.PHONY: test-mcp
.PHONY: test-trace-context
test-trace-context:
	@set -e; mkdir -p $(BIN_DIR); \
	  for package in ingestion live api; do \
	    go test -race -c -o "$(BIN_DIR)/$$package-context.test$(EXE)" "./internal/$$package"; \
	    "./$(BIN_DIR)/$$package-context.test$(EXE)" -test.run 'TestMetricCatalogPreservesIngestContext|TestPublisher|TestSettingsStorageSharesRequestTrace' -test.timeout=2m; \
	  done

.PHONY: test-telemetry-services
test-telemetry-services:
	@set -e; mkdir -p $(BIN_DIR); \
	  for package in telemetry ingestion; do \
	    go test -race -c -o "$(BIN_DIR)/$$package-services.test$(EXE)" "./internal/$$package"; \
	    "./$(BIN_DIR)/$$package-services.test$(EXE)" -test.run 'TestService|TestMCPMetrics|TestSplitServices|TestSetup|TestMetricCatalog' -test.timeout=2m; \
	  done

MCP_TEST_ARGS ?=
test-mcp:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/mcp.test$(EXE) ./internal/mcp
	./$(BIN_DIR)/mcp.test$(EXE) -test.timeout=5m $(MCP_TEST_ARGS)
	go test -c -o $(BIN_DIR)/telemetry.test$(EXE) ./internal/telemetry
	./$(BIN_DIR)/telemetry.test$(EXE) -test.timeout=2m

test-mcp-e2e:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/mcp-inspector.test$(EXE) ./internal/mcp
	SPANIEL_MCP_INSPECTOR_INTEGRATION=1 ./$(BIN_DIR)/mcp-inspector.test$(EXE) -test.run '^TestMCPInspector$$' -test.timeout=10m -test.v

# Focused alert lifecycle checks; no generator inputs are changed by this target.
.PHONY: test-alerts
test-alerts:
	mkdir -p $(BIN_DIR)
	go test -c -o $(BIN_DIR)/alertconfig.test$(EXE) ./internal/alertconfig
	./$(BIN_DIR)/alertconfig.test$(EXE) -test.timeout=2m
	go test -c -o $(BIN_DIR)/alerts.test$(EXE) ./internal/api
	./$(BIN_DIR)/alerts.test$(EXE) -test.run 'Test.*Alert|TestEvent' -test.timeout=5m
	go test -c -o $(BIN_DIR)/alerts-mcp.test$(EXE) ./internal/mcp
	./$(BIN_DIR)/alerts-mcp.test$(EXE) -test.run 'Test.*Alert' -test.timeout=2m

# Focused settings-policy regressions. Stable test paths keep Windows firewall
# behavior consistent with the routine suite. Set SETTINGS_TEST_FLAGS=-race.
.PHONY: test-settings
SETTINGS_TEST_FLAGS ?=
test-settings:
	@set -e; \
	  mkdir -p $(BIN_DIR); \
	  for package in ./internal/api ./internal/ingestion ./internal/receiver ./internal/storage ./internal/forwarder ./cmd/spaniel; do \
	    package_dir=$$(go list -f '{{.Dir}}' "$$package"); \
	    test_name=$$(basename "$$package"); \
	    test_binary="$(CURDIR)/$(BIN_DIR)/settings-$$test_name.test$(EXE)"; \
	    go test $(SETTINGS_TEST_FLAGS) -c -o "$$test_binary" "$$package"; \
	    ( cd "$$package_dir" && "$$test_binary" -test.run 'TestSettingsConcurrent|TestExtendedSettings|TestQuietHours|TestPolicyFilters|TestSignalRetention|TestForwardingSecurity|TestSettingsPublicURL|TestSourceLimiter|TestPublicURLFlag' -test.count=1 -test.timeout=5m -test.v ); \
	  done

# SDK lifecycle/whole-trace regressions, using a stable Windows test binary.
test-telemetry:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/telemetry.test$(EXE) ./internal/telemetry
	./$(BIN_DIR)/telemetry.test$(EXE) -test.count=1 -test.timeout=2m

# Watch foundation transport/model tests use stable, loopback-only binaries.
.PHONY: test-watch
test-watch:
	@set -e; mkdir -p $(BIN_DIR); \
	  for package in $$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./internal/liveevent ./internal/watch/...); do \
	    test_binary="$(CURDIR)/$(BIN_DIR)/watch-$$(basename "$$package").test$(EXE)"; \
	    go test -race -c -o "$$test_binary" "$$package"; \
	    "$$test_binary" -test.count=1 -test.timeout=2m; \
	  done

# Canonical scope/filter contracts used by the integrated terminal. Stable
# loopback-only race binaries keep focused API checks under the same toolchain.
.PHONY: test-watch-api
test-watch-api:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/watch-api.test$(EXE) ./internal/api
	./$(BIN_DIR)/watch-api.test$(EXE) -test.run 'TestWatch|TestDashboardCurrentSessionParameter|TestListLogs_|TestGetSpan_|TestListIncomingLinks|TestGetTrace' -test.count=1 -test.timeout=5m

# Shared presentation and full inspector scrolling after table/axis changes.
.PHONY: test-watch-presentation
test-watch-presentation:
	@set -e; mkdir -p $(BIN_DIR); \
	  for package in ./internal/servicecolor ./internal/tui ./internal/watch/tables ./internal/watch/metadata ./internal/watch/attributes ./internal/watch/terminal ./internal/watch/charts ./internal/watch/tabs/traces ./internal/watch/tabs/dashboards; do \
	    test_binary="$(CURDIR)/$(BIN_DIR)/watch-presentation-$$(basename "$$package").test$(EXE)"; \
	    go test -race -c -o "$$test_binary" "$$package"; \
	    "$$test_binary" -test.count=1 -test.timeout=2m; \
	  done

.PHONY: test-watch-footer
test-watch-footer:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/watch-shell.test$(EXE) ./internal/watch/shell
	./$(BIN_DIR)/watch-shell.test$(EXE) -test.count=1 -test.timeout=2m
	go test -race -c -o $(BIN_DIR)/watch-watch.test$(EXE) ./internal/watch
	./$(BIN_DIR)/watch-watch.test$(EXE) -test.run 'TestSeededTerminalSmoke|TestTerminalOutput' -test.count=1 -test.timeout=2m

# Real production navigation/captures without repeating unrelated package suites.
.PHONY: test-watch-integration
test-watch-integration:
	mkdir -p $(BIN_DIR)
	go test -race -c -o $(BIN_DIR)/watch-watch.test$(EXE) ./internal/watch
	./$(BIN_DIR)/watch-watch.test$(EXE) -test.run '^TestAllSevenRealAPINavigationRepairAndContextualFooter$$' -test.count=1 -test.timeout=2m
