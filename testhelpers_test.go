// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"go/ast"
	"go/parser"
	"go/token"
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
