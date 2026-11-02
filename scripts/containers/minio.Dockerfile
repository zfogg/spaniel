# syntax=docker/dockerfile:1.7
# The upstream Docker Hub repositories are no longer anonymously pullable.
# Build the official archived source at immutable revisions instead.
FROM golang:bookworm AS source
ARG MINIO_COMMIT=7aac2a2c5b7c882e68c1ce017d8256be2feea27f
WORKDIR /src/minio
RUN git init && git remote add origin https://github.com/minio/minio.git \
 && git fetch --depth=1 origin ${MINIO_COMMIT} && git checkout --detach FETCH_HEAD
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/minio .

FROM golang:bookworm AS client-source
ARG MC_COMMIT=77f82e18b5401a65958f1619df6ebb994634bd88
WORKDIR /src/mc
RUN git init && git remote add origin https://github.com/minio/mc.git \
 && git fetch --depth=1 origin ${MC_COMMIT} && git checkout --detach FETCH_HEAD
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mc .

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

FROM runtime AS server
COPY --from=source /out/minio /usr/bin/minio
COPY --from=source /src/minio/LICENSE /usr/share/licenses/minio/LICENSE
EXPOSE 9000 9001
VOLUME /data
ENTRYPOINT ["/usr/bin/minio"]
CMD ["server", "/data", "--console-address", ":9001"]

FROM runtime AS client
COPY --from=client-source /out/mc /usr/bin/mc
COPY --from=client-source /src/mc/LICENSE /usr/share/licenses/mc/LICENSE
ENTRYPOINT ["/usr/bin/mc"]
