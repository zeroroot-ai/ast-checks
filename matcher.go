// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
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

// --- SilentSubstitution primitive --------------------------------------------

// SilentSubstitution matches the shape `if X == nil { X = default() }` — a
// nil-check whose body silently swaps in a fallback rather than failing
// loud. Less common than the return-nil shape but equally degrading.
type SilentSubstitution struct{}

func NewSilentSubstitution() *SilentSubstitution { return &SilentSubstitution{} }
func (m *SilentSubstitution) Name() string       { return "SilentSubstitution" }
func (m *SilentSubstitution) Rule() string {
	return "no silent fallback substitution in request paths"
}

func (m *SilentSubstitution) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	ifs, ok := node.(*ast.IfStmt)
	if !ok {
		return false, ""
	}
	be, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.EQL {
		return false, ""
	}
	var subjectIdent string
	switch {
	case isNilIdent(be.Y):
		subjectIdent = identName(be.X)
	case isNilIdent(be.X):
		subjectIdent = identName(be.Y)
	default:
		return false, ""
	}
	if subjectIdent == "" {
		return false, ""
	}
	if ifs.Body == nil || len(ifs.Body.List) != 1 {
		return false, ""
	}
	assign, ok := ifs.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN {
		return false, ""
	}
	if len(assign.Lhs) != 1 {
		return false, ""
	}
	if identName(assign.Lhs[0]) != subjectIdent {
		return false, ""
	}
	return true, renderIfHead(ifs, fset, src)
}

// --- MultiStmtSkip primitive -------------------------------------------------

// MultiStmtSkip matches `if X == nil { ...stmts...; return nil-y }` — a
// nil-guard whose body has more than one statement but still returns
// silently. NilGuard only handles single-ReturnStmt bodies; this primitive
// catches log-and-return shapes.
type MultiStmtSkip struct{}

func NewMultiStmtSkip() *MultiStmtSkip { return &MultiStmtSkip{} }
func (m *MultiStmtSkip) Name() string  { return "MultiStmtSkip" }
func (m *MultiStmtSkip) Rule() string  { return "no log-and-return nil-guard in request paths" }

func (m *MultiStmtSkip) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	ifs, ok := node.(*ast.IfStmt)
	if !ok || ifs.Body == nil || len(ifs.Body.List) < 2 {
		return false, ""
	}
	// Same condition shape as NilGuard.
	ng := &NilGuard{}
	if !ng.matchCondition(ifs.Cond) {
		return false, ""
	}
	// Last stmt must be a nil-y ReturnStmt.
	last, ok := ifs.Body.List[len(ifs.Body.List)-1].(*ast.ReturnStmt)
	if !ok || !isNilyReturn(last) {
		return false, ""
	}
	return true, renderIfHead(ifs, fset, src)
}

// --- ForbiddenCallsite primitive ---------------------------------------------

// ForbiddenCallsite matches a call to any symbol in the configured set.
// Used to enforce "no context.Background() in request paths", "no time.Now()
// in production code", "no db.Pool direct access" etc. The forbidden set
// is matched on the rightmost selector (`context.Background`, `time.Now`)
// — package-qualifier-aware.
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

func (m *ForbiddenCallsite) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false, ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, ""
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false, ""
	}
	qualified := pkgIdent.Name + "." + sel.Sel.Name
	if _, found := m.Forbidden[qualified]; !found {
		return false, ""
	}
	return true, renderCall(call, fset, src)
}

// --- RequiredCallsite primitive ----------------------------------------------

// RequiredCallsite is the inverse: it inspects a function body and flags
// the function if a required call is missing on every code path.
//
// This matcher operates at the function-declaration level, not the
// statement level — Walk passes it ast.FuncDecl nodes (not nested
// statements). The walker for slice 3.6's "audit_emit_on_mutation" uses
// this primitive.
//
// v0.1.0 ships a simple body-scan (looks for the required selector
// anywhere in the body, regardless of branching). Full reachability
// analysis lands in v0.2.
type RequiredCallsite struct {
	// Required is the fully-qualified symbol that MUST be called somewhere
	// in a function's body. e.g. "auditEmitter.Emit".
	Required string

	// FunctionFilter restricts the rule to functions whose name matches.
	// nil means apply to every function. Typical use: filter to handler-
	// shaped names (`HandleX`, `CreateX`).
	FunctionFilter func(name string) bool

	RuleDesc string
}

func NewRequiredCallsite(rule, required string, filter func(string) bool) *RequiredCallsite {
	return &RequiredCallsite{Required: required, FunctionFilter: filter, RuleDesc: rule}
}

func (m *RequiredCallsite) Name() string { return "RequiredCallsite" }
func (m *RequiredCallsite) Rule() string { return m.RuleDesc }

func (m *RequiredCallsite) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	fn, ok := node.(*ast.FuncDecl)
	if !ok || fn.Body == nil {
		return false, ""
	}
	if m.FunctionFilter != nil && !m.FunctionFilter(fn.Name.Name) {
		return false, ""
	}
	if m.containsRequired(fn.Body) {
		return false, ""
	}
	return true, fn.Name.Name + "(...)"
}

func (m *RequiredCallsite) containsRequired(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkgIdent.Name+"."+sel.Sel.Name == m.Required {
			found = true
			return false
		}
		return true
	})
	return found
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

// --- MethodReceiverFieldShape primitive --------------------------------------

// MethodReceiverFieldShape is a helper that other matchers can compose
// with to narrow their subject-recognition to method-receiver fields.
// Standalone it doesn't produce findings on its own — it's a predicate
// other primitives layer on top of.
//
// Exposed publicly because slice 3.6+ walkers will compose it with
// ForbiddenCallsite (to flag `s.db.Pool` direct access where `s` is the
// method receiver and `db` is a struct field).
type MethodReceiverFieldShape struct{}

func NewMethodReceiverFieldShape() *MethodReceiverFieldShape {
	return &MethodReceiverFieldShape{}
}

func (m *MethodReceiverFieldShape) Name() string { return "MethodReceiverFieldShape" }
func (m *MethodReceiverFieldShape) Rule() string {
	return "(predicate helper: subject is a method-receiver field)"
}

// Match implements the Matcher interface but always returns false — this
// primitive is composed with others via IsReceiverField, not used standalone.
func (m *MethodReceiverFieldShape) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	return false, ""
}

// IsReceiverField returns true when expr is shaped `<ident>.<field>` —
// a single-level selector rooted at a bare identifier. The classic
// receiver-field shape (`s.foo`, `m.db`).
func (m *MethodReceiverFieldShape) IsReceiverField(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	_, isIdent := sel.X.(*ast.Ident)
	return isIdent
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

func identName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		left := identName(e.X)
		if left == "" {
			return ""
		}
		return left + "." + e.Sel.Name
	}
	return ""
}

// --- HostnameLiteral primitive -----------------------------------------------

// HostnameLiteral flags string literals that hardcode an EXTERNAL hostname or
// public origin. Per ADR-0042 (two-plane addressing), external-identity values
// — the public domain, the OIDC issuer, redirect URIs, the Host header on
// domain-routed upstreams — must derive from a single config source
// (`global.domain` and the gibson-common helpers), never be typed as a literal
// in source. This is the Go-source half of the no-hardcoding CI guard
// (deploy#630 / deploy#635); non-Go config (helm/yaml/ts) is guarded by a
// text-based companion in the consuming repos.
//
// Patterns are caller-supplied regexps so each consumer denies the hostnames
// relevant to its environment (e.g. `[a-z0-9-]+\.zeroroot\.ai`,
// `zero-day\.(ai|local)`). Intra-cluster connection addresses
// (`*.svc.cluster.local`, `*.svc`, `localhost`) are the OTHER plane and are
// intentionally NOT denied here — a connection target is not a claimed
// identity. Genuine exceptions are handled by the Allowlist (tagged category),
// not by loosening the pattern.
type HostnameLiteral struct {
	// Patterns is the set of compiled deny-regexps. A literal matching ANY
	// pattern is flagged.
	Patterns []*regexp.Regexp

	// RuleDesc is the rule description (HostnameLiteral is generic; each
	// instance carries its own rationale).
	RuleDesc string
}

// NewHostnameLiteral compiles the supplied patterns. A pattern that fails to
// compile panics at construction time (these are test-author-supplied
// constants, so a bad regexp is a programming error, surfaced loudly).
func NewHostnameLiteral(rule string, patterns ...string) *HostnameLiteral {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		compiled = append(compiled, regexp.MustCompile(p))
	}
	return &HostnameLiteral{Patterns: compiled, RuleDesc: rule}
}

func (m *HostnameLiteral) Name() string { return "HostnameLiteral" }
func (m *HostnameLiteral) Rule() string { return m.RuleDesc }

func (m *HostnameLiteral) Match(fset *token.FileSet, node ast.Node, src []byte) (bool, string) {
	lit, ok := node.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false, ""
	}
	// lit.Value retains the surrounding quotes/backticks; unquote so the
	// regexps match the actual string content, not the Go quoting.
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		// Unparseable literal (shouldn't happen for valid STRING tokens) —
		// fall back to the raw token so we never silently skip.
		val = lit.Value
	}
	for _, re := range m.Patterns {
		if re.MatchString(val) {
			return true, strings.TrimSpace(lit.Value)
		}
	}
	return false, ""
}

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
