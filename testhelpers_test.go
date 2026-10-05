// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// parseExprs parses src as a Go file and returns the expressions that
// appear on the RHS of `_ = expr` AssignStmts inside the file's first
// function declaration. Used by tests that need to construct specific
// AST shapes inline.
func parseExprs(t *testing.T, src string) []ast.Expr {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "<test>", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var exprs []ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		if ident, ok := assign.Lhs[0].(*ast.Ident); !ok || ident.Name != "_" {
			return true
		}
		exprs = append(exprs, assign.Rhs[0])
		return true
	})
	return exprs
}

// assertFindings is the fixture-test helper of this module. Walks fixturesRoot
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
func assertFindings(t *testing.T, fixturesRoot string, matchers []Matcher, wantCoords []string) []Finding {
	t.Helper()
	opts := newWalkOpts()
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

// newWalkOpts is the options every fixture walk in this module starts from:
// production files only, generated files skipped.
func newWalkOpts() WalkOpts {
	return WalkOpts{
		SkipTestFiles: true,
		SkipGenerated: true,
	}
}
