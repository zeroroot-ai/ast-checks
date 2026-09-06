// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

// Package astchecks provides the shared AST walker harness used by every
// codebase-specific structural-invariant test in the zeroroot-ai workspace.
//
// See README.md for the high-level model and the slice 3.1 PRD body
// (zeroroot-ai/.github#42) for the design rationale.
package astchecks

import (
	"fmt"
	"go/token"
	"strings"
)

// Finding is one rule violation detected by a Matcher during a Walk.
//
// Findings are file-line keyed, matching the in-code allowlist convention.
// A Finding never carries the raw AST node — once a walker has captured
// position + snippet + category, the AST is no longer needed and downstream
// code (allowlist comparison, rendering) treats Finding as a plain value.
type Finding struct {
	// Coord is the file:line coordinate, repo-relative when produced by Walk.
	// e.g. "internal/daemon/api/server_audit.go:218".
	Coord string

	// Snippet is a rendered single-line view of the matching construct.
	// e.g. "if s.authorizer == nil { ... }".
	Snippet string

	// Category names the matcher that produced this Finding.
	// e.g. "NilGuard", "ImportBoundary".
	Category string

	// Rule is the human-readable name of the rule the matcher enforces.
	// e.g. "no graceful-nil in request paths".
	Rule string
}

// String renders the finding for terminal output. Matches the existing
// gibson `no_graceful_nil_test.go` output shape so failure messages stay
// agent-readable.
func (f Finding) String() string {
	return fmt.Sprintf("%s: [%s] %s", f.Coord, f.Category, f.Snippet)
}

// ContentKey returns a line-INDEPENDENT allowlist key for this finding:
// the file path (Coord with its trailing ":line" removed) joined to the
// rendered guard Snippet by " :: ", e.g.
//
//	"internal/daemon/api/server_audit.go :: if s.authorizer == nil { ... }"
//
// Unlike Coord, ContentKey is stable across line shifts — a license-header
// swap, an added import, or a new comment moves the line but not the key — and
// across file-internal reordering. An allowlist keyed by ContentKey (opt in via
// WalkOpts.AllowlistByContent) therefore needs maintenance ONLY when a
// genuinely new guard appears, not every time an unrelated edit shifts a line.
//
// Trade-off: a key identifies a guard by (file, text), so multiple identical
// guards in one file share one key — one entry tolerates the pattern wherever
// it appears in that file. For a known-tolerated-guards allowlist that is an
// acceptable (often desirable) coarsening.
//
// Callers should pass Coord already repo-relativized (Walk does this before it
// consults the allowlist), so the file segment of the key is repo-relative.
func (f Finding) ContentKey() string {
	file := f.Coord
	if i := strings.LastIndex(file, ":"); i >= 0 {
		file = file[:i]
	}
	return file + " :: " + f.Snippet
}

// CoordFromPos formats a file:line coordinate from a go/token.Position.
// Repo-relativization is the caller's responsibility (it requires the repo
// root, which Walk owns and threads through).
func CoordFromPos(p token.Position) string {
	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}

// RenderFindings prints findings in stable order. Used by tests and by the
// `zda-ast` CLI's structured-output mode.
func RenderFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.String())
	}
	return strings.Join(parts, "\n")
}
