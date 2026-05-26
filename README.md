<div align="center">

<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 420 160" width="420" height="160">
  <!-- Dog body -->
  <ellipse cx="210" cy="105" rx="52" ry="34" fill="#c8a97e"/>
  <!-- Head -->
  <circle cx="252" cy="80" r="28" fill="#c8a97e"/>
  <!-- Snout -->
  <ellipse cx="270" cy="88" rx="14" ry="10" fill="#b8926a"/>
  <!-- Nose -->
  <ellipse cx="275" cy="84" rx="5" ry="4" fill="#2d1a0e"/>
  <!-- Eye -->
  <circle cx="258" cy="75" r="5" fill="#1a0a00"/>
  <circle cx="259.5" cy="73.5" r="1.5" fill="white"/>
  <!-- Left floppy ear -->
  <ellipse cx="232" cy="68" rx="12" ry="20" fill="#a07850" transform="rotate(-18 232 68)"/>
  <!-- Right floppy ear -->
  <ellipse cx="270" cy="63" rx="10" ry="18" fill="#a07850" transform="rotate(15 270 63)"/>
  <!-- Tail -->
  <path d="M162 95 Q140 60 155 45" stroke="#c8a97e" stroke-width="10" fill="none" stroke-linecap="round"/>
  <!-- Front legs -->
  <rect x="215" y="130" width="12" height="22" rx="6" fill="#c8a97e"/>
  <rect x="235" y="132" width="12" height="20" rx="6" fill="#c8a97e"/>
  <!-- Back legs -->
  <rect x="175" y="128" width="12" height="22" rx="6" fill="#b8926a"/>
  <rect x="193" y="130" width="12" height="20" rx="6" fill="#b8926a"/>
  <!-- Trace waterfall bars -->
  <rect x="30" y="22" width="90" height="10" rx="5" fill="#6ee7b7" opacity="0.9"/>
  <text x="30" y="19" font-family="monospace" font-size="8" fill="#6ee7b7" opacity="0.85">api</text>
  <rect x="50" y="38" width="55" height="10" rx="5" fill="#67e8f9" opacity="0.9"/>
  <text x="50" y="35" font-family="monospace" font-size="8" fill="#67e8f9" opacity="0.85">postgres</text>
  <rect x="65" y="54" width="28" height="10" rx="5" fill="#a78bfa" opacity="0.9"/>
  <text x="65" y="51" font-family="monospace" font-size="8" fill="#a78bfa" opacity="0.85">redis</text>
  <line x1="45" y1="32" x2="50" y2="38" stroke="#6ee7b7" stroke-width="1" opacity="0.4"/>
  <line x1="60" y1="48" x2="65" y2="54" stroke="#67e8f9" stroke-width="1" opacity="0.4"/>
</svg>

# spaniel

**Local OpenTelemetry viewer. Postman for your traces.**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go)](https://go.dev)
[![OpenTelemetry](https://img.shields.io/badge/OTel-native-f5a800?logo=opentelemetry)](https://opentelemetry.io)

</div>

---

You run `docker compose up`. You hit an endpoint. It takes 800ms. You have no idea if it's Postgres, Redis, an N+1 query, or the downstream HTTP call. So you add `print()` statements. There has to be a better way.

**spaniel** is a single binary that receives OpenTelemetry traces, logs, and metrics from your local services and shows them in a beautiful UI — with automatic N+1 detection, a semantic convention linter, and session diffing so you can see exactly what your code change made better or worse.

No Docker required. No cloud account. Nothing leaves your machine.

---

## Install

```bash
# macOS / Linux
brew install zfogg/tap/spaniel

# Go
go install github.com/zfogg/spaniel/cmd/spaniel@latest

# Docker
docker run -p 8080:8080 -p 4317:4317 -p 4318:4318 ghcr.io/zfogg/spaniel:latest
```

## Quickstart

**1. Start spaniel** (browser opens automatically)

```bash
spaniel
```

**2. Point your app at it**

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
```

**3. Hit an endpoint. Traces appear live.**

That's it. No config files, no API keys, no YAML pipelines.

---

## Features

### 🔍 Trace waterfall & flame graph

Click any trace to see a full waterfall view with parent-child span relationships, duration bars, and service color-coding. Toggle to flame graph mode to spot hot paths instantly. Click any span to inspect its attributes as a structured tree.

### ⚡ Automatic N+1 detection

spaniel fingerprints your DB spans and flags when the same query is called an excessive number of times within a single trace. It surfaces the offending parent span, the total wasted time, and the exact statement — without you having to count anything.

```
⚠ N+1 detected in GET /api/projects
  SELECT * FROM builds WHERE project_id = ? — called 47 times (220ms wasted)
  Likely origin: ProjectController.list [span_id: a3f2...]
```

### 🧹 Semantic convention linter

As spans arrive, spaniel validates them against the [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/) spec and flags violations in real time. Missing `db.system` on a database span? Wrong attribute name on an HTTP span? spaniel catches it before you ship.

```
12 spans with warnings this session:
  [error]  3 DB spans missing required attribute: db.system
  [warn]   8 spans with service.name = "unknown_service" — configure OTEL_SERVICE_NAME
  [warn]   1 span with zero duration — likely an instrumentation bug
```

### 🔀 Session diff

Mark any point in time as a baseline. Make your code change. Run again. spaniel diffs the two sessions and shows exactly what changed: new spans, removed spans, duration deltas per operation, attribute changes.

```
Session diff: "before refactor" → "after refactor"
  ✓ GET /api/builds        −18% faster  (820ms → 672ms)
  ✓ SELECT builds          −52% fewer calls  (47 → 3)
  △ POST /api/webhooks     +4% slower  (within noise)
```

```bash
spaniel session new "before refactor"
# ... make your change ...
spaniel session new "after refactor"
# open the diff view in the browser
```

### 🗺 Auto-generated service map

No config. spaniel builds a live dependency graph from your span data — which services are calling which, with call counts, average latency, and error rates on each edge.

### 📋 Log correlation

Full log viewer with severity filtering and free-text search. If a log has a `trace_id`, click it to jump directly to that trace in the waterfall. From any span, see the logs emitted during its execution window.

### 📡 OTLP proxy mode

Already sending traces to Grafana Tempo or Datadog? Run spaniel as a transparent proxy — it stores locally and forwards to your upstream simultaneously. Zero changes to your existing OTel pipeline.

```bash
spaniel --forward http://tempo:4318
```

---

## How it works

spaniel is a single self-contained binary. No external services, no sidecar processes.

- **OTLP receiver** — accepts traces/logs/metrics over gRPC (`:4317`) and HTTP (`:4318`)
- **DuckDB** — stores everything locally in `~/.spaniel/spaniel.duckdb`; persists across restarts
- **Ingestion pipeline** — normalizes, lints, runs detectors, publishes live updates via WebSocket
- **Embedded React UI** — served from the binary itself; opens in your browser at `http://localhost:8080`

```
your app  ──OTLP──►  spaniel :4317/:4318
                         │
                      DuckDB  (~/.spaniel/)
                         │
                    React UI  :8080
```

Data retention defaults to 7 days and 500MB. Configure with `~/.spaniel/config.yaml` or flags.

---

## CI integration

Run spaniel in GitHub Actions to catch regressions before they merge.

```yaml
- name: Start spaniel
  run: spaniel serve --ci &

- name: Run integration tests
  run: pytest tests/integration/

- name: Check for regressions
  run: spaniel ci check --baseline ./spaniel-baseline.json --threshold 20
```

Commit `spaniel-baseline.json` to your repo. spaniel fails the build if p95 latency regresses more than 20%, new N+1s are introduced, or spans with ERROR status appear that weren't in the baseline.

Export a new baseline after intentional changes:

```bash
spaniel ci export --output ./spaniel-baseline.json
```

---

## Configuration

```yaml
# ~/.spaniel/config.yaml
port: 8080
db_path: ~/.spaniel/spaniel.duckdb
retention_days: 7
max_db_size_mb: 500
no_browser: false
forward:
  - url: http://tempo:4318
```

Or use a per-project `.spaniel.yaml` in your repo root. CLI flags always take precedence.

---

## CLI reference

```
spaniel                          start the server (opens browser)
spaniel session new [label]      create and activate a new session
spaniel session list             list all sessions
spaniel session baseline [id]    mark a session as the diff baseline
spaniel import <file>            import a trace from OTLP/Jaeger JSON as a baseline
spaniel ci check                 compare current session against baseline (for CI)
spaniel ci export                export current session as a baseline JSON file
spaniel prune                    delete sessions older than retention period
spaniel reset                    wipe all data
spaniel config                   show current effective config
```

---

## Docker Compose

Drop spaniel into your existing `docker-compose.yml`:

```yaml
services:
  spaniel:
    image: ghcr.io/zfogg/spaniel:latest
    ports:
      - "8080:8080"
      - "4317:4317"
      - "4318:4318"
    volumes:
      - spaniel-data:/data
    environment:
      - SPANIEL_DB_PATH=/data/spaniel.duckdb

  your-api:
    # ... your existing service ...
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=http://spaniel:4318
      - OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
      - OTEL_SERVICE_NAME=your-api

volumes:
  spaniel-data:
```

One engineer adds this. The whole team gets local observability. No individual setup required.

---

## Why not Grafana / Jaeger / Datadog?

| | spaniel | Grafana LGTM | Jaeger | Datadog |
|---|---|---|---|---|
| Single binary | ✓ | ✗ (4+ containers) | ✗ | ✗ |
| Zero config | ✓ | ✗ | ✗ | ✗ |
| N+1 detection | ✓ | ✗ | ✗ | paid |
| Semconv linter | ✓ | ✗ | ✗ | ✗ |
| Session diff | ✓ | ✗ | ✗ | ✗ |
| Local only / private | ✓ | ✓ | ✓ | ✗ |
| Cost | free | free | free | expensive |

spaniel is specifically built for the **local development loop**, not production monitoring. Use it on your laptop. Use Grafana or Datadog in prod.

---

## Roadmap

- [x] OTLP receiver (gRPC + HTTP)
- [x] DuckDB storage
- [x] Trace waterfall + flame graph
- [x] N+1 query detection
- [x] Semantic convention linter
- [x] Session diff
- [x] Service map
- [x] Log correlation
- [x] OTLP proxy mode
- [ ] Production baseline import (Tempo, Jaeger export)
- [ ] Instrumentation coverage heatmap
- [ ] CI regression detection (`spaniel ci`)
- [ ] Cloud baseline sync (team feature)

---

## Contributing

```bash
git clone https://github.com/zfogg/spaniel
cd spaniel
make dev      # starts Go backend + Vite dev server with hot reload
make build    # builds production binary with embedded frontend
make test     # runs Go tests
```

Issues and PRs welcome.

---

## License

MIT © [Zachary Fogg](https://github.com/zfogg)
