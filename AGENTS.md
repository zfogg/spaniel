# Spaniel contributor guidance

## Testing on Windows

Run Go builds and tests through the Makefile (`make build` and `make test`),
not by invoking `go build` or `go test` directly. The Makefile selects the
MSYS2 UCRT64 GCC toolchain required by the prebuilt DuckDB library; the Scoop
MinGW toolchain has an incompatible C++ ABI and fails during linking.

`make build` writes `spaniel.exe` on Windows and `spaniel` on other platforms.
Do not hard-code a different output name in Go code or scripts; use the
Makefile's `BIN` variable for local build artifacts.
