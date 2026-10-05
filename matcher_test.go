// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixturesRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "scope")
}

// TestNilGuard_ReceiverFieldOnly proves NilGuard in narrowed mode catches
// `s.foo == nil { return nil }` but not bare-ident or err checks.
func TestNilGuard_ReceiverFieldOnly(t *testing.T) {
	matchers := []Matcher{NewNilGuard(true)}
	want := []string{
		"internal/illegal_nilguard.go.txt:10",
	}
	got := assertFindings(t, fixturesRoot(t), matchers, want)
	if len(got) > 0 && !strings.Contains(got[0].Snippet, "s.authorizer") {
		t.Errorf("snippet should contain s.authorizer; got %q", got[0].Snippet)
	}
}

// TestNilGuard_BroadMode catches both receiver-field and bare-ident shapes.
func TestNilGuard_BroadMode(t *testing.T) {
	matchers := []Matcher{NewNilGuard(false)}
	// broad mode also catches the bare-ident `if cfg == nil` in legal_err_check.go.txt.
	want := []string{
		"internal/illegal_nilguard.go.txt:10",
		"internal/legal_err_check.go.txt:17",
	}
	assertFindings(t, fixturesRoot(t), matchers, want)
}

// TestForbiddenCallsite catches forbidden symbol invocations.
func TestForbiddenCallsite(t *testing.T) {
	matchers := []Matcher{
		NewForbiddenCallsite(
			"no context.Background() in request paths",
			"context.Background",
		),
		NewForbiddenCallsite(
			"no time.Now() in production code",
			"time.Now",
		),
	}
	want := []string{
		"internal/illegal_forbidden_call.go.txt:10",
		"internal/illegal_forbidden_call.go.txt:16",
	}
	assertFindings(t, fixturesRoot(t), matchers, want)
}

// TestImportBoundary catches forbidden imports.
func TestImportBoundary(t *testing.T) {
	matchers := []Matcher{
		NewImportBoundary(
			"sdk and ext-authz must not import gibson",
			"github.com/zeroroot-ai/gibson",
		),
	}
	want := []string{
		"internal/illegal_forbidden_import.go.txt:4",
	}
	assertFindings(t, fixturesRoot(t), matchers, want)
}

// TestAllowlist_Validate rejects malformed allowlists.
func TestAllowlist_Validate(t *testing.T) {
	cases := []struct {
		name     string
		al       Allowlist
		wantErr  bool
		errMatch string
	}{
		{"empty is valid", Allowlist{}, false, ""},
		{"valid LEGACY-OPTIONAL", Allowlist{
			"a.go :: if s.x == nil { ... }": {Category: CategoryLegacyOptional, Reason: "x", IssueURL: "https://example/issue/1"},
		}, false, ""},
		{"LEGACY-OPTIONAL no IssueURL is fine", Allowlist{
			"a.go :: if s.x == nil { ... }": {Category: CategoryLegacyOptional, Reason: "x"},
		}, false, ""},
		{"IssueURL must look URL-shaped when present", Allowlist{
			"a.go :: if s.x == nil { ... }": {Category: CategoryLegacyOptional, Reason: "x", IssueURL: "not-a-url"},
		}, true, "does not look like a URL"},
		{"unknown category", Allowlist{
			"a.go :: if s.x == nil { ... }": {Category: "BOGUS", Reason: "x"},
		}, true, "unknown category"},
		{"empty reason", Allowlist{
			"a.go :: if s.x == nil { ... }": {Category: CategoryDefensiveGuard, Reason: ""},
		}, true, "empty reason"},
		{"coordinate-shaped key is rejected", Allowlist{
			"a.go:1": {Category: CategoryDefensiveGuard, Reason: "x"},
		}, true, "not a content key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.al.Validate()
			if (err != nil) != c.wantErr {
				t.Errorf("Validate: got err=%v, want err? %v", err, c.wantErr)
			}
			if c.errMatch != "" && (err == nil || !strings.Contains(err.Error(), c.errMatch)) {
				t.Errorf("Validate: expected error containing %q, got %v", c.errMatch, err)
			}
		})
	}
}

// TestWalk_FiltersAllowlist proves Walk filters an allowlisted content key.
func TestWalk_FiltersAllowlist(t *testing.T) {
	opts := newWalkOpts()
	opts.ScopeDirs = []string{fixturesRoot(t)}
	opts.RepoRoot = fixturesRoot(t)
	opts.Matchers = []Matcher{NewNilGuard(true)}
	opts.Allowlist = Allowlist{
		"internal/illegal_nilguard.go.txt :: if s.authorizer == nil { ... }": {
			Category: CategoryLegacyOptional,
			Reason:   "test fixture only",
			IssueURL: "https://example/issue/1",
		},
	}

	got, err := Walk(opts)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Walk: expected 0 findings after allowlist, got %d: %v", len(got), got)
	}
}
