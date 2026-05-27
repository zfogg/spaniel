.PHONY: dev build run test setup

dev:
	cd frontend && npm run dev &
	ENV=dev go run ./cmd/spaniel --dev

build:
	cd frontend && npm run build
	go build -o spaniel ./cmd/spaniel

run:
	go run ./cmd/spaniel

test:
	go test ./...

setup:
	git config core.hooksPath git-hooks
