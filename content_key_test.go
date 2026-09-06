// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"os"
	"path/filepath"
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

// TestWalk_AllowlistByContent_SurvivesLineShift is the regression that justifies
// content keying: a content-keyed allowlist entry must keep matching after an
// unrelated edit (here, prepending a license header) shifts the guard's line —
// the exact failure mode that repeatedly reddened gibson's gate (#1025/#1043/
// #1044/#1041). A Coord-keyed allowlist would miss after the shift.
func TestWalk_AllowlistByContent_SurvivesLineShift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	const body = "package p\n\ntype S struct{ dep *int }\n\n" +
		"func (s *S) M() error {\n\tif s.dep == nil {\n\t\treturn nil\n\t}\n\treturn nil\n}\n"
	mustWrite(t, path, body)

	scan := func(byContent bool, al Allowlist) []Finding {
		t.Helper()
		got, err := Walk(WalkOpts{
			ScopeDirs:          []string{dir},
			RepoRoot:           dir,
			Matchers:           []Matcher{NewNilGuard(false)},
			AllowlistByContent: byContent,
			Allowlist:          al,
		})
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		return got
	}

	// 1. Unfiltered walk discovers the guard; capture its content key.
	found := scan(false, nil)
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %v", len(found), found)
	}
	key := found[0].ContentKey()
	al := Allowlist{key: Entry{Category: CategoryDefensiveGuard, Reason: "test"}}

	// 2. Content-keyed allowlist filters it out.
	if got := scan(true, al); len(got) != 0 {
		t.Fatalf("content key %q should filter the guard, got %v", key, got)
	}

	// 3. Shift every line down by prepending a header. The guard is identical;
	//    its line changed but its ContentKey did not — it must stay filtered.
	mustWrite(t, path, "// Copyright 2026 Hack the Planet LLC\n// header line 2\n"+body)
	if got := scan(true, al); len(got) != 0 {
		t.Fatalf("content-keyed allowlist must survive a line shift, got %v", got)
	}

	// 4. Control: the OLD Coord-keyed allowlist (file:line from before the shift)
	//    now MISSES — demonstrating exactly the brittleness content keying fixes.
	stale := Allowlist{found[0].Coord: Entry{Category: CategoryDefensiveGuard, Reason: "test"}}
	if got := scan(false, stale); len(got) != 1 {
		t.Fatalf("Coord-keyed allowlist should have gone stale after the shift (got %d findings)", len(got))
	}
}

func mustWrite(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
