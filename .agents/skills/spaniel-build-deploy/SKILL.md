---
name: spaniel-build-deploy
description: Build, test, and deploy Spaniel from a Windows or Manjaro workstation to the Pi5 Stonkpile telemetry sidecar. Use for Spaniel implementation, image verification, or Pi5 rollout requests.
---

# Spaniel build and Pi5 deployment

Spaniel's primary development environment is this checkout on the local Windows or Manjaro workstation. Edit and test here first; `make test`, `make build`, `make run`, `make dev`, and a local Docker build/run are appropriate. The local `docker-compose.yml` is only for local Spaniel and uses its normal ports.

The production-like consumer is Stonkpile. On Pi5, its compose file is `/home/alarm/src/github.com/zfogg/stonkpile/docker-compose.yml`: service `spaniel`, container `stonkpile-spaniel`, image `${SPANIEL_IMAGE:-ghcr.io/zfogg/spaniel:latest}`, ports 8345/4345/4346, and volume `stonkpile_spaniel:/data`. Stonkpile may continue sending telemetry while Spaniel is replaced; that is acceptable.

## Local work

- Keep unrelated changes intact. Run the narrowest relevant tests, then build locally. A local Docker image can verify the Dockerfile, but it does not prove an ARM Pi image.
- Do not use the Stonkpile checkout or Pi5 as the ordinary edit/test environment.

## Deploy to Pi5

Deploy only a committed, pushed revision. Record its SHA; push from the workstation, then connect with `ssh alarm@pi5`.

1. In `/home/alarm/src/github.com/zfogg/spaniel`, inspect `git status --porcelain`, preserve any remote work with a named stash including untracked files, fetch/pull fast-forward, and require `HEAD` to equal the intended pushed SHA. Never reset or discard remote work.
2. Build on Pi5 so the image is native `linux/arm64`:
   `docker build --pull -t ghcr.io/zfogg/spaniel:latest .`
   A workstation build is usable only when an explicitly verified multi-platform build produces a runnable `linux/arm64` image on Pi5.
3. From `/home/alarm/src/github.com/zfogg/stonkpile`, replace only Spaniel:
   `docker compose up -d --no-deps --force-recreate spaniel`
   Do not run `docker compose down`, recreate other Stonkpile services, remove `stonkpile_spaniel`, or alter its ports/environment.
4. Verify `stonkpile-spaniel` is running and healthy, inspect recent container logs, and query `http://127.0.0.1:8345/api/health` on Pi5. Confirm its image ID is the newly built image and the retained `/data/spaniel.duckdb` volume remains attached.
5. Restore the exact stash if one was made. If restoration conflicts, leave the stash intact and report it.

Stop and report if the revision cannot be proven, the ARM build fails, the container is unhealthy, or remote work cannot be restored. Do not expose credentials, environment-file contents, or telemetry payloads.
