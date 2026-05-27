# syntax=docker/dockerfile:1.7

# ── Stage 1: build the React frontend ────────────────────────────────────────
FROM node:20-alpine AS frontend
WORKDIR /app/frontend
RUN corepack enable && corepack prepare pnpm@10.33.0 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

# ── Stage 2: build the Go binary (CGO required by go-duckdb) ─────────────────
# go-duckdb ships a prebuilt libduckdb.a linked against glibc with
# FORTIFY_SOURCE, so we must use a glibc toolchain (Debian) here — Alpine/musl
# fails to resolve __memcpy_chk and friends.
FROM golang:bookworm AS gobuild
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src

# Cache modules first.
COPY go.mod go.sum ./
RUN go mod download

# Then bring in the source and the freshly-built frontend dist.
COPY . .
COPY --from=frontend /app/frontend/dist ./frontend/dist

ENV CGO_ENABLED=1 GOOS=linux
ARG VERSION=dev
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/spaniel ./cmd/spaniel

# ── Stage 3: minimal runtime ─────────────────────────────────────────────────
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates tzdata wget \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -r spaniel \
 && useradd -r -g spaniel -d /data -s /usr/sbin/nologin spaniel \
 && mkdir -p /data \
 && chown -R spaniel:spaniel /data

COPY --from=gobuild /out/spaniel /usr/local/bin/spaniel

USER spaniel
WORKDIR /data
VOLUME ["/data"]

# UI / OTLP gRPC / OTLP HTTP
EXPOSE 8080 4317 4318

ENTRYPOINT ["/usr/local/bin/spaniel"]
CMD ["--db-path", "/data/spaniel.duckdb", "--no-browser"]
