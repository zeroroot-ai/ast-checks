// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Category is the tag carried on every allowlist entry. The categories form
// a closed set so the allowlist's intent is grep-able and per-category
// follow-ups are filterable.
//
// New categories require an ADR change (the cross-language convention is
// codified workspace-wide). Do not invent ad-hoc tags.
type Category string

const (
	// CategoryLegacyOptional marks a dependency that is wired conditionally
	// today and should be reasserted as always-on by a follow-up slice.
	// Each entry names its follow-up.
	CategoryLegacyOptional Category = "LEGACY-OPTIONAL"

	// CategoryDefensiveGuard marks a nil-check on a value-shape input
	// (typically a converter helper or an observability span shim) that
	// the walker over-flags. The anti-pattern only applies to injected
	// dependencies; defensive-guard entries are legitimate.
	CategoryDefensiveGuard Category = "DEFENSIVE-GUARD"

	// CategoryReceiverNilGuard marks a nil-receiver shim — `if s == nil`
	// at the top of a method body, used as a defensive convenience for
	// tests that pass a zero-value server. Not the dependency anti-pattern.
	CategoryReceiverNilGuard Category = "RECEIVER-NIL-GUARD"

	// CategoryTestOnly marks a guard that only fires in test fixtures.
	// Production code paths cannot reach the guard.
	CategoryTestOnly Category = "TEST-ONLY"
)

// Entry is one allowlist row. Each entry carries a tagged category and a
// human-readable reason. IssueURL is optional but validated as URL-shape
// when present; the freshness gate (PRD 6.8) asserts every IssueURL
// resolves to an existing issue.
//
// Reason itself encodes the follow-up plan (e.g. "remove with budget-required
// slice", "predates noopAuthorizer deletion"). When a slice that closes
// the follow-up is filed, prefer adding IssueURL too so the trail is
// machine-checkable.
type Entry struct {
	// Category is one of the closed-set tags above.
	Category Category

	// Reason is a one-line explanation that encodes the follow-up plan.
	// Required. Read by reviewers; written for an audience who hasn't read
	// the PR that introduced the entry.
	Reason string

	// IssueURL points at the tracked follow-up. Optional but recommended
	// for LEGACY-OPTIONAL entries. URL-shape validated when present.
	IssueURL string
}

// Allowlist maps Finding.ContentKey() ("file :: snippet") to an Entry. The
// walker passes the allowlist to Walk; findings whose content key is present
// in the allowlist are filtered out of the failing-findings list, but a Logf
// line is still emitted so the allowlist stays visible in test output.
//
// A key never carries a line number. "internal/a/b.go:218" is rejected by
// Validate; "internal/a/b.go :: if s.dep == nil { ... }" is the shape.
type Allowlist map[string]Entry

// contentKeySeparator joins the file and the snippet in Finding.ContentKey.
const contentKeySeparator = " :: "

// Validate asserts every key is a content key, every entry has a known
// Category and a non-empty Reason, and every IssueURL is URL-shaped.
// Returns nil on success.
//
// Called by Walk before the walk begins so a malformed allowlist fails the
// test loudly rather than silently passing through. A coordinate-shaped key
// ("file:line") can never match a finding, so it fails here with the
// migration named instead of going silently inert.
func (a Allowlist) Validate() error {
	var errs []string
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, c := range keys {
		e := a[c]
		if !strings.Contains(c, contentKeySeparator) {
			errs = append(errs, fmt.Sprintf("%s: not a content key; key the entry by Finding.ContentKey() (\"file :: snippet\"), never by file:line", c))
		}
		switch e.Category {
		case CategoryLegacyOptional, CategoryDefensiveGuard, CategoryReceiverNilGuard, CategoryTestOnly:
		default:
			errs = append(errs, fmt.Sprintf("%s: unknown category %q", c, e.Category))
		}
		if e.Reason == "" {
			errs = append(errs, fmt.Sprintf("%s: empty reason", c))
		}
		if e.IssueURL != "" && !isURLLike(e.IssueURL) {
			errs = append(errs, fmt.Sprintf("%s: IssueURL %q does not look like a URL", c, e.IssueURL))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New("allowlist validation:\n  " + joinLines(errs))
}

func isURLLike(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func joinLines(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += "\n  "
		}
		out += x
	}
	return out
}
