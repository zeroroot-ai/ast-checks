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
// A Finding carries its file:line Coord for the human reading the report, and
// an allowlist matches it by ContentKey, which carries no line number. A
// Finding never carries the raw AST node — once a walker has captured
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

// ContentKey returns the allowlist key for this finding: the file path
// (Coord with its trailing ":line" removed) joined to the rendered guard
// Snippet by " :: ", e.g.
//
//	"internal/daemon/api/server_audit.go :: if s.authorizer == nil { ... }"
//
// Unlike Coord, ContentKey is stable across line shifts (a license-header
// swap, an added import, a new comment) and across file-internal reordering.
// An allowlist therefore needs maintenance only when a new guard appears,
// never when an unrelated edit shifts a line. Walk matches the allowlist by
// this key and by nothing else.
//
// Trade-off: a key identifies a guard by (file, text), so multiple identical
// guards in one file share one key, and one entry tolerates the pattern
// wherever it appears in that file. For a known-tolerated-guards allowlist
// that is an acceptable coarsening. A consumer that wants the other
// direction, an entry that no longer matches any finding, re-walks with an
// empty allowlist and diffs the keys.
//
// Callers should pass Coord already repo-relativized (Walk does this before it
// consults the allowlist), so the file segment of the key is repo-relative.
func (f Finding) ContentKey() string {
	file := f.Coord
	if i := strings.LastIndex(file, ":"); i >= 0 {
		file = file[:i]
	}
	return file + contentKeySeparator + f.Snippet
}

// CoordFromPos formats a file:line coordinate from a go/token.Position.
// Repo-relativization is the caller's responsibility (it requires the repo
// root, which Walk owns and threads through).
func CoordFromPos(p token.Position) string {
	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}

// RenderFindings prints findings in stable order, grouped under the rule each
// one violates. Used by fixture tests and by any repo-side guard that needs a
// stable text block to put in a failure message. The rule line is the text a
// consumer passed to the matcher's constructor, so the message says what the
// rule is and not only where it fired:
//
//	rule: no graceful-nil in request paths
//	internal/daemon/api/server_audit.go:218: [NilGuard] if s.authorizer == nil { ... }
func RenderFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	var rules []string
	byRule := map[string][]Finding{}
	for _, f := range findings {
		if _, seen := byRule[f.Rule]; !seen {
			rules = append(rules, f.Rule)
		}
		byRule[f.Rule] = append(byRule[f.Rule], f)
	}
	var parts []string
	for _, r := range rules {
		parts = append(parts, "rule: "+r)
		for _, f := range byRule[r] {
			parts = append(parts, f.String())
		}
	}
	return strings.Join(parts, "\n")
}
