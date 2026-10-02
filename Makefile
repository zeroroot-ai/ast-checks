# Per-repo Makefile contract (per zeroroot-ai polyrepo convention).
# Targets: build / test / test-race / check / image (n/a here).

# The golang.org/x/tools release that cmd/deadcode is installed from (ADR-0094,
# .github#160). ONE pin: CI reads it with `make deadcode-version` and hands it to
# the org gate, so a developer's gate and CI's gate cannot run different tools.
# The gate asserts the binary it ran carries exactly this x/tools. A newer one
# fails too, because the pin is the pin and not a floor.
DEADCODE_VERSION ?= v0.50.0
DEADCODE_ALLOWLIST ?= scripts/deadcode-allow.txt
DEADCODE_BIN := $(CURDIR)/bin/tools/deadcode

# The Go that builds deadcode. `go install pkg@version` ignores this module's
# go.mod and builds with the oldest Go that x/tools accepts. deadcode type-checks
# with the go/types of the Go that built it, so a binary built by go1.26 refuses
# this go1.27 module with "package requires newer Go version", and the gate then
# fails for a reason that has nothing to do with dead code. The module's own go
# directive is the Go that builds it. CI installs the same Go through setup-go.
#
# An EXACT pin, not `+auto`. Measured 2026-10-02 under a go1.23.2 shim:
# `GOTOOLCHAIN=go1.27.1+auto go install ...@v0.50.0` still switched to go1.26.8,
# the oldest Go x/tools accepts, and the binary then refused the module. With
# the exact pin the go command runs the named toolchain and nothing else.
GO_TOOLCHAIN := go$(shell awk '$$1=="go"{print $$2; exit}' go.mod)

.PHONY: build test test-race check fmt vet lint lint-unwired lint-unwired-write \
	deadcode-version deadcode-install lint-deadcode lint-deadcode-selftest

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

check: fmt vet test-race lint-deadcode

# This repo eats its own output. ast-checks#11 is its tracker, and the baseline
# below is what `unwired` measures here.
lint-unwired:
	go run ./cmd/unwired -dir . -baseline .unwired-baseline.txt

lint-unwired-write:
	go run ./cmd/unwired -dir . -baseline .unwired-baseline.txt -write

# Print the pin for CI. The workflow passes it to zeroroot-ai/.github's
# actions/deadcode-gate, so the Makefile stays the only place the version lives.
deadcode-version:
	@echo $(DEADCODE_VERSION)

# Install into bin/tools, never GOPATH/bin. PATH decides which deadcode answers
# `command -v` on a developer box, and a stale one from an older toolchain reads
# no code. The gate resolves the binary from where go install wrote it.
deadcode-install:
	GOBIN=$(dir $(DEADCODE_BIN)) GOFLAGS= GOTOOLCHAIN=$(GO_TOOLCHAIN) \
		go install golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION)

# Whole-program reachability floor (ADR-0094). The one main is cmd/unwired, and
# it imports only the unwired package, so every function in the root library is
# unreachable from it. The allowlist names each one with the consumer that keeps
# it, or the release that deletes it. A finding outside the allowlist fails, and
# an allowlist entry that names nothing fails too, so the baseline ratchets both
# ways. CI runs the org action with the same pin and the same allowlist. This
# target gives the same verdict before a push. Keys are the bare symbol deadcode
# prints, never a line.
lint-deadcode: deadcode-install
	@have="$$(go version -m $(DEADCODE_BIN) 2>/dev/null | awk '$$1=="mod" && $$2=="golang.org/x/tools"{print $$3; exit}')"; \
	if [ "$$have" != "$(DEADCODE_VERSION)" ]; then \
		echo "ERROR: $(DEADCODE_BIN) is x/tools $${have:-unreadable}, not $(DEADCODE_VERSION). The pin is the pin." >&2; \
		exit 1; \
	fi; \
	if [ ! -f "$(DEADCODE_ALLOWLIST)" ]; then \
		echo "ERROR: $(DEADCODE_ALLOWLIST) does not exist. A missing allowlist is a wiring mistake, not an empty baseline." >&2; \
		exit 1; \
	fi; \
	tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
	out="$$($(DEADCODE_BIN) ./... 2>"$$tmp/err")" && rc=0 || rc=$$?; \
	if [ "$$rc" -ne 0 ]; then \
		echo "ERROR: deadcode could not analyse the module (exit $$rc). It read no code, so this is not a pass." >&2; \
		cat "$$tmp/err" >&2; \
		exit 1; \
	fi; \
	printf '%s\n' "$$out" | sed -n 's/.*unreachable func: //p' | LC_ALL=C sort -u > "$$tmp/found"; \
	{ grep -vE '^[[:space:]]*(#|$$)' "$(DEADCODE_ALLOWLIST)" || true; } | sed 's/[[:space:]]*#.*$$//; s/[[:space:]]*$$//' | LC_ALL=C sort -u > "$$tmp/allowed"; \
	new="$$(LC_ALL=C comm -23 "$$tmp/found" "$$tmp/allowed")"; \
	stale="$$(LC_ALL=C comm -13 "$$tmp/found" "$$tmp/allowed")"; \
	found=$$(wc -l < "$$tmp/found" | tr -d ' '); allowed=$$(wc -l < "$$tmp/allowed" | tr -d ' '); \
	status=0; \
	if [ -n "$$stale" ]; then \
		echo "ERROR: $(DEADCODE_ALLOWLIST) names symbols deadcode no longer reports. Remove the entries:" >&2; \
		printf '%s\n' "$$stale" | sed 's/^/  /' >&2; \
		status=1; \
	fi; \
	if [ -n "$$new" ]; then \
		echo "ERROR: unreachable code that is not in $(DEADCODE_ALLOWLIST):" >&2; \
		printf '%s\n' "$$new" > "$$tmp/new"; \
		printf '%s\n' "$$out" | while IFS= read -r line; do \
			sym="$${line##*unreachable func: }"; \
			grep -qxF -- "$$sym" "$$tmp/new" && echo "  $$line" >&2; \
		done; \
		echo "Wire it to a consumer or delete it. ADR-0094: never default to deletion." >&2; \
		status=1; \
	fi; \
	[ "$$status" -eq 0 ] || exit "$$status"; \
	echo "deadcode: $$found unreachable func(s) found, $$allowed allowlisted, 0 new"

# Prove lint-deadcode can fail: a copy of the module with one function nothing
# calls, and one with a stale allowlist entry, must each make the gate exit
# non-zero and name the symbol.
lint-deadcode-selftest:
	bash scripts/check-deadcode-can-fail.sh
