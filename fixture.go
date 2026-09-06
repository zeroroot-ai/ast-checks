// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// AssertFindings is the canonical fixture-test helper. Walks fixturesRoot
// with the given matchers and asserts the returned findings match wantCoords
// exactly (after repo-relativization).
//
// Used by every walker's fixture sub-test to prove the walker fires on
// known-bad fixtures and doesn't over-flag known-good fixtures. The
// returned findings are the actual ones from the walker — callers can
// inspect them for additional assertions.
//
// wantCoords contains the file:line coordinates (repo-relative to
// fixturesRoot) the walker MUST find. Extra findings fail the test;
// missing findings fail the test; identical sets pass.
func AssertFindings(t *testing.T, fixturesRoot string, matchers []Matcher, wantCoords []string) []Finding {
	t.Helper()
	opts := NewWalkOpts()
	opts.ScopeDirs = []string{fixturesRoot}
	opts.RepoRoot = fixturesRoot
	opts.Matchers = matchers

	got, err := Walk(opts)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	gotCoords := make([]string, 0, len(got))
	for _, f := range got {
		gotCoords = append(gotCoords, f.Coord)
	}
	sort.Strings(gotCoords)

	sortedWant := append([]string(nil), wantCoords...)
	sort.Strings(sortedWant)

	if !equalStringSlices(gotCoords, sortedWant) {
		t.Errorf("fixture findings mismatch:\n  want: %s\n  got:  %s",
			strings.Join(sortedWant, ", "),
			strings.Join(gotCoords, ", "))
	}
	return got
}

// AssertEmpty walks a single fixture file with the given matchers and
// asserts NO findings are produced. Used to verify legal_*.go fixtures
// don't trip the walker.
func AssertEmpty(t *testing.T, fixturesRoot string, matchers []Matcher) {
	t.Helper()
	AssertFindings(t, fixturesRoot, matchers, nil)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// FormatAllowlistLog returns a stable string suitable for `t.Logf` so the
// allowlist stays visible in test output even when no findings are produced.
// Matches the format used by the existing gibson `no_graceful_nil_test.go`.
func FormatAllowlistLog(a Allowlist) string {
	if len(a) == 0 {
		return ""
	}
	coords := make([]string, 0, len(a))
	for c := range a {
		coords = append(coords, c)
	}
	sort.Strings(coords)
	var b strings.Builder
	for _, c := range coords {
		e := a[c]
		fmt.Fprintf(&b, "allowlisted: %s — [%s] %s\n", c, e.Category, e.Reason)
	}
	return b.String()
}
