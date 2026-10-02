# Per-repo Makefile contract (per zeroroot-ai polyrepo convention).
# Targets: build / test / test-race / check / image (n/a here).

.PHONY: build test test-race check fmt vet lint lint-unwired lint-unwired-write

build:
	go build ./...

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

# This repo eats its own output. ast-checks#11 is its tracker, and the baseline
# below is what `unwired` measures here.
lint-unwired:
	go run ./cmd/unwired -dir . -baseline .unwired-baseline.txt

lint-unwired-write:
	go run ./cmd/unwired -dir . -baseline .unwired-baseline.txt -write
