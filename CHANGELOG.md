# Changelog

## [0.9.0](https://github.com/zeroroot-ai/ast-checks/compare/v0.8.2...v0.9.0) (2026-10-09)


### Features

* **unwired:** count the reads that a marshaller, an outside interface and a test-support package make ([#44](https://github.com/zeroroot-ai/ast-checks/issues/44)) ([331deb8](https://github.com/zeroroot-ai/ast-checks/commit/331deb89aefe65dba7b91a965320a7ca0c91ee43))

## [0.8.2](https://github.com/zeroroot-ai/ast-checks/compare/v0.8.1...v0.8.2) (2026-10-06)


### Bug Fixes

* **unwired:** the baseline names only a consumer that exists ([#34](https://github.com/zeroroot-ai/ast-checks/issues/34)) ([a1bdf6e](https://github.com/zeroroot-ai/ast-checks/commit/a1bdf6e9b46a831ec4d0908e7ab27d242a66bbc8))

## [0.8.1](https://github.com/zeroroot-ai/ast-checks/compare/v0.8.0...v0.8.1) (2026-10-05)


### Bug Fixes

* **unwired:** a literal key that is not a field is a read ([#32](https://github.com/zeroroot-ai/ast-checks/issues/32)) ([ed4790c](https://github.com/zeroroot-ai/ast-checks/commit/ed4790c6276f81d1e842847d635602b611e7a972))

## [0.8.0](https://github.com/zeroroot-ai/ast-checks/compare/v0.7.0...v0.8.0) (2026-10-05)


### ⚠ BREAKING CHANGES

* **api:** SilentSubstitution, MultiStmtSkip, RequiredCallsite, MethodReceiverFieldShape, HostnameLiteral, AssertEmpty, AssertFindings, NewWalkOpts and WalkOpts.AllowlistByContent are removed.

### Features

* **api:** delete the declarations that were marked for deletion in v0.6.0 ([#30](https://github.com/zeroroot-ai/ast-checks/issues/30)) ([e7bcec3](https://github.com/zeroroot-ai/ast-checks/commit/e7bcec3b72616acd125ee1f2994f2aa783fa7801))

## [0.7.0](https://github.com/zeroroot-ai/ast-checks/compare/v0.6.0...v0.7.0) (2026-10-05)


### Features

* **walk:** walkreport returns the allowlist entries that match no finding ([#27](https://github.com/zeroroot-ai/ast-checks/issues/27)) ([c1403d4](https://github.com/zeroroot-ai/ast-checks/commit/c1403d4eca714d220dfd36f057401f0256fe31a1)), closes [#24](https://github.com/zeroroot-ai/ast-checks/issues/24)

## [0.6.0](https://github.com/zeroroot-ai/ast-checks/compare/v0.5.0...v0.6.0) (2026-10-04)


### Features

* **crdfields:** one crd field gate for every repo ([#21](https://github.com/zeroroot-ai/ast-checks/issues/21)) ([499ed02](https://github.com/zeroroot-ai/ast-checks/commit/499ed0267fcf62f7ea2f93131c24d1add933d4ac)), closes [#20](https://github.com/zeroroot-ai/ast-checks/issues/20)

## [0.5.0](https://github.com/zeroroot-ai/ast-checks/compare/v0.4.0...v0.5.0) (2026-10-02)


### ⚠ BREAKING CHANGES

* **walk:** an allowlist entry keyed by file:line fails Walk with a validation error. Key entries by Finding.ContentKey().

### Features

* **walk:** content keying is the only allowlist keying ([#15](https://github.com/zeroroot-ai/ast-checks/issues/15)) ([096d4b4](https://github.com/zeroroot-ai/ast-checks/commit/096d4b4caed493fc3c2e5d9443af45bc99fc886a)), closes [#10](https://github.com/zeroroot-ai/ast-checks/issues/10)


### Bug Fixes

* **unwired:** the baseline keeps its reasons, and the two unread fields get consumers ([#17](https://github.com/zeroroot-ai/ast-checks/issues/17)) ([96f4c79](https://github.com/zeroroot-ai/ast-checks/commit/96f4c790184d24cd9701a71c63c66521c1ee6b2c))

## [0.4.0](https://github.com/zeroroot-ai/ast-checks/compare/v0.3.1...v0.4.0) (2026-10-02)


### Features

* **unwired:** count reads per declaration, so a tracker can be re-measured ([#14](https://github.com/zeroroot-ai/ast-checks/issues/14)) ([6bbbb62](https://github.com/zeroroot-ai/ast-checks/commit/6bbbb62f54a0375b923624c0c2ddd0c870765edf))


### Bug Fixes

* **ci:** link-check checks only the Markdown a PR touched (.github v0.7.2) ([#9](https://github.com/zeroroot-ai/ast-checks/issues/9)) ([b76d10e](https://github.com/zeroroot-ai/ast-checks/commit/b76d10e93b53a1e893aeab72d7eb63b45320a93b))
* **ci:** pin every zeroroot-ai/.github reference to v0.5.1 ([#7](https://github.com/zeroroot-ai/ast-checks/issues/7)) ([ad35c8c](https://github.com/zeroroot-ai/ast-checks/commit/ad35c8c8ac2fecc83d6972b589e6348820ff40ef))
* **ci:** pin the org tree guards to a commit SHA ([#5](https://github.com/zeroroot-ai/ast-checks/issues/5)) ([cd07ca1](https://github.com/zeroroot-ai/ast-checks/commit/cd07ca193c92ff99d3bef66d519004e4e1434af5))
* **finding:** the RenderFindings comment names a CLI that no longer exists ([#12](https://github.com/zeroroot-ai/ast-checks/issues/12)) ([2f0ff12](https://github.com/zeroroot-ai/ast-checks/commit/2f0ff12e63755eaf57ed4dbfd3acc21ef6ea5388))
* **testdata:** the hostname fixture no longer names internal hosts ([#8](https://github.com/zeroroot-ai/ast-checks/issues/8)) ([354b298](https://github.com/zeroroot-ai/ast-checks/commit/354b2988dd69a4b17382b5a5290f7b13b35dd4ec))

## Changelog

This repository restarted from a fresh baseline on 2026-09-06. Release notes before that date are archived offline and do not resolve on GitHub. release-please adds each release below this line.
