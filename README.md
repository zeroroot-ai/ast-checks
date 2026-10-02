# ast-checks

Shared Go AST harness for codebase-specific structural invariants. Walker primitives (NilGuard, SilentSubstitution, ForbiddenCallsite, ImportBoundary, MethodReceiverFieldShape), allowlist with tagged categories, fixture-test helpers.

Internal Go module under the zeroroot-ai workspace. See [`zeroroot-ai/.github` → `AGENTS.md`](https://github.com/zeroroot-ai/.github/blob/main/AGENTS.md) for workflow conventions (branching, PRs, releases, agent merge autonomy).

## `unwired` — who reads this declaration?

`unwired` counts, per declaration, how many times production code READS it, and
fails when the set of unread declarations grows past a committed baseline.

It is not `deadcode`. ADR-0094 rules that tool out for this question in one
sentence: *"A struct field is never a reachability root, so the gate cannot see a
field that is written and never read. That is the single largest class in the
sweep."*

```bash
go run github.com/zeroroot-ai/ast-checks/cmd/unwired -dir . -baseline .unwired-baseline.txt
go run github.com/zeroroot-ai/ast-checks/cmd/unwired -dir . -baseline .unwired-baseline.txt -write
```

Each consuming repo wires a thin `make lint-unwired` of its own and keeps its own
baseline, per ADR-0094 and the workspace rule that a shared fix lives in one
place and fans out mechanically.

### The counting rule

A read is a use of the declaration's object in a non-test, non-generated file,
minus the uses that are writes.

| Form | Counted as |
|---|---|
| `v := x.Field`, `f(x.Field)`, `x.Field == v` | read |
| `x.Field = v` | write |
| `T{Field: v}` | write (a literal key names a field without consulting it) |
| `x.Field++` | write |
| `x.Field += v` | **both** — the old value is consulted |

So a field that is populated and never consulted reports `reads=0 writes=N`,
which is the class ADR-0094 names.

Two things are deliberately **not** reads: the declaration itself, and a use
inside a `_test.go` file. A declaration whose only consumer is its own test is
what a tracker exists to surface. `-tests-as-reads` turns that off.

### Methods reached through an interface

A concrete method is credited with the reads of the interface method it
satisfies, when the receiver implements an interface declared in the analysed
packages. Without this every parser, plugin and handler in the estate reports
zero reads, because the call site names the interface method: measured on
`gibson-executor`, 35 of 61 findings were this false positive.

Crediting only flows from an interface method that is **actually called**, so an
interface nobody calls does not make its implementations look live. That case is
a real finding and stays one.

### Keys

Baseline and exemption entries are `kind name` pairs — never file or line. A
guard that needs re-pinning after an unrelated edit is a defect in the guard.

## Install

```bash
go get github.com/zeroroot-ai/ast-checks@latest
```

## License and history

Apache License 2.0. See [LICENSE](LICENSE). Copyright Zero Root AI.

Issue and pull request numbers cited in comments and documents dated before 2026-09-05 refer to the tracker before the history reset, archived offline. They do not resolve on GitHub.