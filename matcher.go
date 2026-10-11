// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"go/ast"
	"go/token"
	"strings"
)

// Matcher is the interface every pattern primitive satisfies. A Matcher
// inspects one AST node and returns a Finding when the node matches its
// rule. Matchers are composable — a Walker iterates over a slice of
// Matchers and lets each one inspect every node.
//
// Matchers are stateless. All per-walk configuration is captured at
// construction time (e.g. via the New* factory funcs below).
type Matcher interface {
	// Name returns a short identifier for the matcher used in Finding.Category
	// and in test diagnostics. e.g. "NilGuard".
	Name() string

	// Rule returns the human-readable description used in Finding.Rule.
	// e.g. "no graceful-nil in request paths".
	Rule() string

	// Match inspects a single AST node. If the node matches this matcher's
	// rule, Match returns (true, snippet) where snippet is a rendered
	// single-line view of the construct. Otherwise (false, "").
	Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string)
}

// FileMatcher is a Matcher that needs the file of the node, for example to
// resolve an import alias. Walk calls MatchInFile in place of Match for a
// FileMatcher. The Match method of a FileMatcher may report no match for each
// node, so a caller that walks a file itself must call MatchInFile.
type FileMatcher interface {
	Matcher

	// MatchInFile is Match with the parsed file that holds node.
	MatchInFile(fset *token.FileSet, file *ast.File, node ast.Node, src []byte) (bool, string)
}

// --- NilGuard primitive ------------------------------------------------------

// NilGuard matches the shape `if X == nil { return nil-y }` — the
// graceful-nil anti-pattern from ADR-0003. By default, X may be a
// SelectorExpr (`s.deps.Foo`) or a bare Ident (`cfg`). When ReceiverFieldOnly
// is true, the matcher narrows to method-receiver field shape only
// (`s.foo`) and ignores bare identifiers and deeper selector chains; this
// eliminates the parameter-shape false-positives that bloated gibson's
// initial DEFENSIVE-GUARD allowlist.
type NilGuard struct {
	// ReceiverFieldOnly narrows to `<single-receiver-ident>.<field>`. Bare
	// identifiers (`cfg == nil`) and longer chains (`s.deps.X.Y`) are not
	// flagged.
	ReceiverFieldOnly bool
}

// NewNilGuard constructs a NilGuard matcher with the given narrowing.
func NewNilGuard(receiverFieldOnly bool) *NilGuard {
	return &NilGuard{ReceiverFieldOnly: receiverFieldOnly}
}

func (m *NilGuard) Name() string { return "NilGuard" }
func (m *NilGuard) Rule() string { return "no graceful-nil in request paths" }

func (m *NilGuard) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	ifs, ok := node.(*ast.IfStmt)
	if !ok {
		return false, ""
	}
	if !m.matchCondition(ifs.Cond) {
		return false, ""
	}
	if !isSilentReturnBody(ifs.Body) {
		return false, ""
	}
	return true, renderIfHead(ifs, fset, src)
}

func (m *NilGuard) matchCondition(expr ast.Expr) bool {
	be, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if be.Op == token.LOR || be.Op == token.LAND {
		return m.matchCondition(be.X) || m.matchCondition(be.Y)
	}
	if be.Op != token.EQL {
		return false
	}
	var subject ast.Expr
	switch {
	case isNilIdent(be.Y):
		subject = be.X
	case isNilIdent(be.X):
		subject = be.Y
	default:
		return false
	}
	return m.matchSubject(subject)
}

func (m *NilGuard) matchSubject(subject ast.Expr) bool {
	switch s := subject.(type) {
	case *ast.SelectorExpr:
		if looksLikeError(s.Sel.Name) {
			return false
		}
		if !m.ReceiverFieldOnly {
			return true
		}
		// Receiver-field shape: SelectorExpr.X must be a single Ident
		// (the receiver). Selector chains and parenthesized expressions
		// are excluded.
		_, ok := s.X.(*ast.Ident)
		return ok
	case *ast.Ident:
		if m.ReceiverFieldOnly {
			return false
		}
		return !looksLikeError(s.Name)
	default:
		return false
	}
}

// --- ForbiddenCallsite primitive ---------------------------------------------

// ForbiddenCallsite matches a call to any symbol in the configured set.
// Used to enforce "no context.Background() in request paths", "no time.Now()
// in production code", "no db.Pool direct access" etc.
//
// Each entry is "<package name>.<symbol>", for example `context.Background`
// or `pgxpool.New`. The matcher reads the imports of the file:
// `import ctx "context"` and then `ctx.Background()` is a match, a dot import
// of "time" and then `Now()` is a match, and a local variable named `time`
// with a `Now` method is not a match.
//
// Limit: for an aliased or dot import, the package name comes from the path:
// its last element, with a major version element such as `v5` skipped. An
// aliased import of a package whose name the path does not show (for example
// github.com/openfga/go-sdk, package openfga) is not matched.
type ForbiddenCallsite struct {
	// Forbidden is the set of fully-qualified symbols to flag.
	// e.g. {"context.Background", "time.Now"}.
	Forbidden map[string]struct{}

	// RuleDesc is the rule description (since ForbiddenCallsite is generic;
	// each instance has its own rationale).
	RuleDesc string
}

func NewForbiddenCallsite(rule string, forbidden ...string) *ForbiddenCallsite {
	set := make(map[string]struct{}, len(forbidden))
	for _, f := range forbidden {
		set[f] = struct{}{}
	}
	return &ForbiddenCallsite{Forbidden: set, RuleDesc: rule}
}

func (m *ForbiddenCallsite) Name() string { return "ForbiddenCallsite" }
func (m *ForbiddenCallsite) Rule() string { return m.RuleDesc }

// Match always reports no match. ForbiddenCallsite needs the imports of the
// file to tell a package from a local name, so it is a FileMatcher, and Walk
// calls MatchInFile for it. Call MatchInFile, or use Walk; never call Match.
func (m *ForbiddenCallsite) Match(_ *token.FileSet, _ ast.Node, _ []byte) (bool, string) {
	return false, ""
}

// MatchInFile resolves the qualifier of a call against the imports of file
// and matches the call when "<package name>.<symbol>" is in the forbidden set.
func (m *ForbiddenCallsite) MatchInFile(fset *token.FileSet, file *ast.File, node ast.Node, src []byte) (bool, string) {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false, ""
	}
	var qualified string
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		qual, ok := fun.X.(*ast.Ident)
		if !ok {
			return false, ""
		}
		pkg, imported := importedPackageName(file, qual.Name)
		if !imported {
			return false, ""
		}
		qualified = pkg + "." + fun.Sel.Name
	case *ast.Ident:
		for _, pkg := range dotImportedPackageNames(file) {
			if _, found := m.Forbidden[pkg+"."+fun.Name]; found {
				return true, renderCall(call, fset, src)
			}
		}
		return false, ""
	default:
		return false, ""
	}
	if _, found := m.Forbidden[qualified]; !found {
		return false, ""
	}
	return true, renderCall(call, fset, src)
}

// importedPackageName returns the package name that the qualifier local
// stands for in file, and whether local names a package at all.
//
//  1. An import with the alias local names the package of its path.
//  2. An import with no alias whose path shows the name local names it.
//  3. A name that the file declares (a variable, a parameter, a type, a
//     function) is not a package.
//  4. Otherwise, when the file has an import with no alias, local is that
//     package under its real name, which the path does not always show
//     (github.com/stripe/stripe-go/v76 is package stripe).
func importedPackageName(file *ast.File, local string) (string, bool) {
	unaliased := false
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if imp.Name != nil {
			if imp.Name.Name == local {
				return packageNameOfPath(path), true
			}
			continue
		}
		unaliased = true
		if packageNameOfPath(path) == local {
			return local, true
		}
	}
	if !unaliased || declaredInFile(file, local) {
		return "", false
	}
	return local, true
}

// declaredInFile reports whether file declares the name anywhere: a variable,
// a constant, a parameter, a result, a receiver, a type or a function.
func declaredInFile(file *ast.File, name string) bool {
	found := false
	names := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				if n.Name == name {
					found = true
				}
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		switch d := n.(type) {
		case *ast.AssignStmt:
			if d.Tok == token.DEFINE {
				for _, l := range d.Lhs {
					if id, ok := l.(*ast.Ident); ok && id.Name == name {
						found = true
					}
				}
			}
		case *ast.RangeStmt:
			if d.Tok == token.DEFINE {
				for _, e := range []ast.Expr{d.Key, d.Value} {
					if id, ok := e.(*ast.Ident); ok && id.Name == name {
						found = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range d.Names {
				if id.Name == name {
					found = true
				}
			}
		case *ast.TypeSpec:
			found = found || d.Name.Name == name
		case *ast.FuncDecl:
			found = found || d.Name.Name == name
			names(d.Recv)
		case *ast.FuncType:
			names(d.Params)
			names(d.Results)
		}
		return !found
	})
	return found
}

// dotImportedPackageNames returns the package name of each dot import of file.
func dotImportedPackageNames(file *ast.File) []string {
	var names []string
	for _, imp := range file.Imports {
		if imp.Name != nil && imp.Name.Name == "." {
			names = append(names, packageNameOfPath(strings.Trim(imp.Path.Value, `"`)))
		}
	}
	return names
}

// packageNameOfPath is the package name that Go gives an import path in
// nearly every module: the last path element, with a major version element
// such as "v5" skipped and a ".vN" suffix such as "yaml.v3" removed.
func packageNameOfPath(path string) string {
	elems := strings.Split(path, "/")
	name := elems[len(elems)-1]
	if len(elems) > 1 && isMajorVersion(name) {
		name = elems[len(elems)-2]
	}
	if i := strings.Index(name, ".v"); i > 0 && isMajorVersion(name[i+1:]) {
		name = name[:i]
	}
	return name
}

func isMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// --- ImportBoundary primitive ------------------------------------------------

// ImportBoundary matches an import statement whose path matches a
// forbidden prefix. Replaces the shell-scripted `check-no-gibson.sh`
// pattern with typed, AST-grounded enforcement that catches aliased
// imports and indirect references.
type ImportBoundary struct {
	// ForbiddenPrefixes is the set of import path prefixes that may not
	// appear in any file under the walked scope. e.g.
	// {"github.com/zeroroot-ai/gibson"} for SDK + ext-authz.
	ForbiddenPrefixes []string

	RuleDesc string
}

func NewImportBoundary(rule string, prefixes ...string) *ImportBoundary {
	return &ImportBoundary{ForbiddenPrefixes: prefixes, RuleDesc: rule}
}

func (m *ImportBoundary) Name() string { return "ImportBoundary" }
func (m *ImportBoundary) Rule() string { return m.RuleDesc }

func (m *ImportBoundary) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	imp, ok := node.(*ast.ImportSpec)
	if !ok || imp.Path == nil {
		return false, ""
	}
	path := strings.Trim(imp.Path.Value, `"`)
	for _, prefix := range m.ForbiddenPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true, `import "` + path + `"`
		}
	}
	return false, ""
}

// --- shared helpers ----------------------------------------------------------

func isSilentReturnBody(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	r, ok := body.List[0].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	return isNilyReturn(r)
}

func isNilyReturn(r *ast.ReturnStmt) bool {
	if len(r.Results) == 0 {
		return true
	}
	for _, res := range r.Results {
		if !isNilOrBoolLiteral(res) {
			return false
		}
	}
	return true
}

func isNilOrBoolLiteral(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	switch id.Name {
	case "nil", "true", "false":
		return true
	}
	return false
}

func isNilIdent(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "nil"
}

func looksLikeError(name string) bool {
	lower := strings.ToLower(name)
	return lower == "err" || strings.HasSuffix(lower, "err") || strings.HasSuffix(lower, "error")
}

// --- rendering helpers ------------------------------------------------------

func renderIfHead(ifs *ast.IfStmt, fset *token.FileSet, src []byte) string {
	start := fset.Position(ifs.Pos()).Offset
	end := fset.Position(ifs.Body.Lbrace).Offset
	if start < 0 || end < start || end > len(src) {
		return "<unrenderable>"
	}
	line := strings.TrimSpace(string(src[start:end]))
	line = strings.ReplaceAll(line, "\n", " ")
	line = strings.ReplaceAll(line, "\t", " ")
	for strings.Contains(line, "  ") {
		line = strings.ReplaceAll(line, "  ", " ")
	}
	return line + " { ... }"
}

func renderCall(call *ast.CallExpr, fset *token.FileSet, src []byte) string {
	start := fset.Position(call.Pos()).Offset
	end := fset.Position(call.End()).Offset
	if start < 0 || end < start || end > len(src) {
		return "<unrenderable>"
	}
	return strings.TrimSpace(string(src[start:end]))
}
