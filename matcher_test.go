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
	got := AssertFindings(t, fixturesRoot(t), matchers, want)
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
	AssertFindings(t, fixturesRoot(t), matchers, want)
}

// TestSilentSubstitution catches `if X == nil { X = default() }`.
func TestSilentSubstitution(t *testing.T) {
	matchers := []Matcher{NewSilentSubstitution()}
	want := []string{
		"internal/illegal_substitution.go.txt:9",
	}
	AssertFindings(t, fixturesRoot(t), matchers, want)
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
	AssertFindings(t, fixturesRoot(t), matchers, want)
}

// TestHostnameLiteral catches hardcoded external hostnames/origins (ADR-0042 /
// deploy#635) while leaving intra-cluster connection addresses alone.
func TestHostnameLiteral(t *testing.T) {
	matchers := []Matcher{
		NewHostnameLiteral(
			"no hardcoded external hostname — derive from global.domain (ADR-0042)",
			`[a-z0-9-]+\.zeroroot\.ai`,
			`zero-day\.(ai|local)`,
		),
	}
	// Positive: the three external-origin literals. Negative: gibson:50051,
	// the *.svc.cluster.local address, and the config-read are NOT flagged.
	want := []string{
		"internal/illegal_hostname_literal.go.txt:9",
		"internal/illegal_hostname_literal.go.txt:11",
		"internal/illegal_hostname_literal.go.txt:13",
	}
	AssertFindings(t, fixturesRoot(t), matchers, want)
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
	AssertFindings(t, fixturesRoot(t), matchers, want)
}

// TestMethodReceiverFieldShape_IsReceiverField unit-tests the predicate
// helper independent of Match.
func TestMethodReceiverFieldShape_IsReceiverField(t *testing.T) {
	src := `package x
func f() {
	_ = s.foo
	_ = pkg.S{}.bar
	_ = a.b.c
	_ = cfg
}`
	exprs := parseExprs(t, src)
	helper := NewMethodReceiverFieldShape()

	cases := []struct {
		name string
		expr int
		want bool
	}{
		{"single-receiver field", 0, true},
		{"composite-literal selector", 1, false}, // selector but X is not Ident
		{"chain selector", 2, false},             // SelectorExpr.X is itself a SelectorExpr
		{"bare ident", 3, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := helper.IsReceiverField(exprs[c.expr])
			if got != c.want {
				t.Errorf("IsReceiverField: got %v, want %v", got, c.want)
			}
		})
	}
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
			"a.go:1": {Category: CategoryLegacyOptional, Reason: "x", IssueURL: "https://example/issue/1"},
		}, false, ""},
		{"LEGACY-OPTIONAL no IssueURL is fine", Allowlist{
			"a.go:1": {Category: CategoryLegacyOptional, Reason: "x"},
		}, false, ""},
		{"IssueURL must look URL-shaped when present", Allowlist{
			"a.go:1": {Category: CategoryLegacyOptional, Reason: "x", IssueURL: "not-a-url"},
		}, true, "does not look like a URL"},
		{"unknown category", Allowlist{
			"a.go:1": {Category: "BOGUS", Reason: "x"},
		}, true, "unknown category"},
		{"empty reason", Allowlist{
			"a.go:1": {Category: CategoryDefensiveGuard, Reason: ""},
		}, true, "empty reason"},
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

// TestWalk_FiltersAllowlist proves Walk filters allowlisted coords.
func TestWalk_FiltersAllowlist(t *testing.T) {
	opts := NewWalkOpts()
	opts.ScopeDirs = []string{fixturesRoot(t)}
	opts.RepoRoot = fixturesRoot(t)
	opts.Matchers = []Matcher{NewNilGuard(true)}
	opts.Allowlist = Allowlist{
		"internal/illegal_nilguard.go.txt:10": {
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
