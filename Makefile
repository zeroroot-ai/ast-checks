# Per-repo Makefile contract (per zeroroot-ai polyrepo convention).
# Targets: build / test / test-race / check / image (n/a here).

.PHONY: build test test-race check fmt vet lint

build:
	@echo "no binary to build in bootstrap state; implementation lands via the corresponding board #16 slice"

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	@which golangci-lint >/dev/null 2>&1 || (echo "golangci-lint not installed"; exit 1)
	golangci-lint run

check: fmt vet test-race
