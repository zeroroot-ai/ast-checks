# ast-checks — CLAUDE.md

> **Workflow rules:** see [`zeroroot-ai/.github` → `AGENTS.md`](https://github.com/zeroroot-ai/.github/blob/main/AGENTS.md) — canonical for branching / commits / PRs / releases / merging. Conventional Commits MANDATORY. Never push to main. Never force-push.

This file is the per-repo addendum. Workspace-wide concerns live in [`~/Code/zeroroot.ai/CLAUDE.md`](https://github.com/zeroroot-ai/.github/blob/main/AGENTS.md); architectural decisions in ``docs/adr/`` (local docs → `adr`).

## TL;DR

Shared Go AST harness for codebase-specific structural invariants. Provides walker primitives (`NilGuard`, `ForbiddenCallsite`, `ImportBoundary`), an allowlist with tagged categories, and fixture-test helpers. Used by every repo that enforces structural Go code rules. Entry point: `make check` (fmt + vet + test-race + lint-deadcode).

## Architecture

A single Go module (`github.com/zeroroot-ai/ast-checks`) with no external service dependencies. The harness wraps `go/ast` + `go/types` walker patterns so per-repo invariant tests do not have to re-implement tree traversal. Each consuming repo imports this module and writes a `*_test.go` file that instantiates walkers with repo-specific allow/deny rules. The rules run as standard `go test` and fail CI like any other test.

No binary to build — this is a library. Allowlist entries are tagged with categories so reports group related violations.

## Regen commands

```bash
make test       # go test ./...
make test-race  # go test -race ./...
make check      # fmt + vet + test-race + lint-deadcode
make lint-deadcode  # ADR-0094 reachability floor against scripts/deadcode-allow.txt
```

## Gotchas

- **Two binaries, one library.** `cmd/unwired` and `cmd/crdfields` are the only `main` packages, and they import only the `unwired` and `crdfields` packages. `deadcode` therefore reports the whole root library as unreachable, so every root symbol sits in `scripts/deadcode-allow.txt` with the consumer that keeps it or the release that deletes it. `DEADCODE_VERSION` in the Makefile is the one pin and CI reads it from there.
- **Consuming repos pin a version.** Each consumer imports a specific tagged version. A change here requires a new tag + consumer bump PRs via the standard fan-out.
- **Walker primitives are the contract.** Any rename or signature change to `NilGuard`, `ForbiddenCallsite` or `ImportBoundary`, is a breaking API change requiring a semver minor bump (pre-1.0 — bump minor, not major).

## Links

- Org-level workflow: [`AGENTS.md`](https://github.com/zeroroot-ai/.github/blob/main/AGENTS.md)
- Workspace map: workspace `CLAUDE.md`
