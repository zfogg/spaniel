# Spaniel contributor guidance

## Testing on Windows

Run Go builds and tests through the Makefile (`make build` and `make test`),
not by invoking `go build` or `go test` directly. The Makefile selects the
MSYS2 UCRT64 GCC toolchain required by the prebuilt DuckDB library; the Scoop
MinGW toolchain has an incompatible C++ ABI and fails during linking.

`make build` writes `bin/spaniel.exe` on Windows and `bin/spaniel` on other platforms.
Do not hard-code a different output name in Go code or scripts; use the
Makefile's `BIN` variable for local build artifacts.

On Windows, use `make run` or `make dev` for any Spaniel process that listens
on a network port. They run the stable `bin/spaniel.exe` build artifact, so a
single Windows Defender Firewall approval applies to future development runs.
Do not use `go run` for the server: it produces a fresh temporary executable
and can cause repeated firewall prompts. Tests must bind helper listeners to
`127.0.0.1`, never `:0` or another wildcard address, unless the test explicitly
requires externally reachable behavior. `make test` builds test packages under
`bin`; loopback-only tests do not need a firewall rule. If a test is
intentionally externally reachable, approve only its corresponding stable
`bin/*.test.exe` path and only on the required network profiles.

## Storage query boundary

Use GORM Gen for Spaniel's typed reads/writes and named SQL (including DML).
Raw GORM is reserved for migrations/schema repair, dynamic retention cleanup,
and DuckDB maintenance; user SQL remains read-only and is separately traced.
Run `make generate` after changing storage models or named query interfaces.
