# ast-checks

Shared Go AST harness for codebase-specific structural invariants. Walker primitives (NilGuard, SilentSubstitution, ForbiddenCallsite, ImportBoundary, MethodReceiverFieldShape), allowlist with tagged categories, fixture-test helpers.

Internal Go module under the zeroroot-ai workspace. See [`zeroroot-ai/.github` → `AGENTS.md`](https://github.com/zeroroot-ai/.github/blob/main/AGENTS.md) for workflow conventions (branching, PRs, releases, agent merge autonomy).

## Status

Bootstrap repo. Initial implementation lands via the corresponding production-readiness slice on board #16. Until then, this README + LICENSE + Makefile contract are the only contents.

## Install

```bash
go get github.com/zeroroot-ai/ast-checks@latest
```

## License

[BUSL-1.1](./LICENSE).

## License and history

Apache License 2.0. See [LICENSE](LICENSE). Copyright Zero Root AI.

Issue and pull request numbers cited in comments and documents dated before 2026-09-05 refer to the tracker before the history reset, archived offline. They do not resolve on GitHub.