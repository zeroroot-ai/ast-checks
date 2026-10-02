// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFinding_ContentKey(t *testing.T) {
	cases := []struct {
		name string
		f    Finding
		want string
	}{
		{"file:line", Finding{Coord: "internal/a/b.go:218", Snippet: "if s.dep == nil { ... }"},
			"internal/a/b.go :: if s.dep == nil { ... }"},
		{"no line segment", Finding{Coord: "internal/a/b.go", Snippet: "x"},
			"internal/a/b.go :: x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.ContentKey(); got != tc.want {
				t.Fatalf("ContentKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

// walkShiftFixture walks one half of the testdata/shift pair with the given
// allowlist and returns the findings.
func walkShiftFixture(t *testing.T, half string, al Allowlist) []Finding {
	t.Helper()
	root := filepath.Join(fixturesRoot(t), "..", "shift", half)
	opts := NewWalkOpts()
	opts.ScopeDirs = []string{filepath.Join(root, "internal")}
	opts.RepoRoot = root
	opts.Matchers = []Matcher{NewNilGuard(true)}
	opts.Allowlist = al
	got, err := Walk(opts)
	if err != nil {
		t.Fatalf("Walk %s: %v", half, err)
	}
	return got
}

// TestWalk_AllowlistSurvivesUnrelatedEditAbove is the fixture behind the
// workspace rule that a guard which needs re-pinning after an unrelated edit is
// a defect in the guard. testdata/shift/before and testdata/shift/after hold
// the same guard; "after" has a header, a package comment, an import and a new
// function above it. One allowlist entry must tolerate the guard in both.
func TestWalk_AllowlistSurvivesUnrelatedEditAbove(t *testing.T) {
	before := walkShiftFixture(t, "before", nil)
	after := walkShiftFixture(t, "after", nil)
	if len(before) != 1 || len(after) != 1 {
		t.Fatalf("each half must hold exactly one guard, got before=%v after=%v", before, after)
	}

	// The edit moved the guard. If it did not, the test proves nothing.
	if before[0].Coord == after[0].Coord {
		t.Fatalf("the unrelated edit did not shift the guard's line: both at %s", before[0].Coord)
	}
	if before[0].ContentKey() != after[0].ContentKey() {
		t.Fatalf("content key changed across an unrelated edit: %q vs %q",
			before[0].ContentKey(), after[0].ContentKey())
	}

	al := Allowlist{before[0].ContentKey(): {Category: CategoryDefensiveGuard, Reason: "fixture"}}
	if got := walkShiftFixture(t, "before", al); len(got) != 0 {
		t.Errorf("allowlist entry %q does not tolerate the guard before the edit: %v", before[0].ContentKey(), got)
	}
	if got := walkShiftFixture(t, "after", al); len(got) != 0 {
		t.Errorf("allowlist entry %q stopped tolerating the guard after an unrelated edit above it: %v",
			before[0].ContentKey(), got)
	}
}

// TestWalk_RejectsCoordinateKeyedAllowlist is the failing fixture for the
// guard above: a "file:line" key can never match a finding, so Walk refuses it
// with the migration named instead of letting the entry go silently inert.
func TestWalk_RejectsCoordinateKeyedAllowlist(t *testing.T) {
	before := walkShiftFixture(t, "before", nil)
	if len(before) != 1 {
		t.Fatalf("expected one guard, got %v", before)
	}
	stale := Allowlist{before[0].Coord: {Category: CategoryDefensiveGuard, Reason: "fixture"}}

	root := filepath.Join(fixturesRoot(t), "..", "shift", "before")
	opts := NewWalkOpts()
	opts.ScopeDirs = []string{filepath.Join(root, "internal")}
	opts.RepoRoot = root
	opts.Matchers = []Matcher{NewNilGuard(true)}
	opts.Allowlist = stale
	_, err := Walk(opts)
	if err == nil {
		t.Fatalf("Walk accepted the coordinate-keyed entry %q; it can never match a finding", before[0].Coord)
	}
	if !strings.Contains(err.Error(), "not a content key") || !strings.Contains(err.Error(), before[0].Coord) {
		t.Fatalf("error must name the key and the migration, got: %v", err)
	}
}

// TestRenderFindings_NamesTheRule: the rule text a consumer passes to a matcher
// is the one line that says what the gate enforces. Before this, Finding.Rule
// was filled on every finding and rendered nowhere.
func TestRenderFindings_NamesTheRule(t *testing.T) {
	got := RenderFindings([]Finding{
		{Coord: "a.go:1", Snippet: "x", Category: "C", Rule: "rule one"},
		{Coord: "a.go:2", Snippet: "y", Category: "C", Rule: "rule one"},
		{Coord: "b.go:3", Snippet: "z", Category: "D", Rule: "rule two"},
	})
	want := "rule: rule one\na.go:1: [C] x\na.go:2: [C] y\nrule: rule two\nb.go:3: [D] z"
	if got != want {
		t.Fatalf("RenderFindings:\n got %q\nwant %q", got, want)
	}
	if RenderFindings(nil) != "" {
		t.Fatal("no findings must render as the empty string")
	}
}
