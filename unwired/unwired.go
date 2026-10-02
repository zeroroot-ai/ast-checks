// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

// Package unwired counts, per declaration, how many times production code READS
// it.
//
// # Why this is not deadcode
//
// `golang.org/x/tools/cmd/deadcode` answers whole-program reachability from a
// binary's main. ADR-0094 rules it out for this question in one sentence: "A
// struct field is never a reachability root, so the gate cannot see a field that
// is written and never read. That is the single largest class in the sweep."
//
// Fourteen standing trackers across the estate carry counts produced by an
// ad-hoc scanner that was committed nowhere (ast-checks#13), so every tracker's
// "re-measure and leave the count updated" step was unmeetable. This package is
// that scanner, with the counting rule written down.
//
// # What counts as a read
//
// A declaration's reads are the uses of its object in non-test, non-generated
// files of the analysed packages, MINUS the uses that are writes.
//
// The write/read split is the whole point for fields. These three are writes:
//
//	x.Field = v          assignment, left-hand side
//	T{Field: v}          composite literal key
//	x.Field++            increment or decrement
//
// and these are reads:
//
//	v := x.Field
//	f(x.Field)
//	x.Field == v
//	x.Other = x.Field    the right-hand side is a read
//
// So a field that is populated and never consulted reports zero reads, which is
// the class ADR-0094 names and the reason this package exists.
//
// A compound assignment (`x.Field += v`) reads and writes, and is counted as
// both: the old value is consulted.
//
// # What is not a read
//
// The declaration itself never counts, so a type that only names itself in its
// own methods' receivers does not look alive. A use inside a `_test.go` file
// does not count either: a declaration whose only consumer is its own test is
// exactly what a tracker exists to surface. TestsAsReads turns that off for a
// caller that wants the laxer number.
package unwired

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Kind is what a declaration is.
type Kind string

const (
	KindFunc   Kind = "func"
	KindMethod Kind = "method"
	KindType   Kind = "type"
	KindField  Kind = "field"
	KindConst  Kind = "const"
	KindVar    Kind = "var"
)

// Decl is one declaration and its read count.
type Decl struct {
	// Kind is what was declared.
	Kind Kind

	// Name is the qualified name a human can grep for: "pkg.Name" for a
	// top-level declaration, "pkg.Type.Field" for a field, and
	// "pkg.Type.Method" for a method.
	Name string

	// Coord is "file:line", repo-relative when RepoRoot is set.
	Coord string

	// Exported says whether the name is exported. A published module's
	// exported surface cannot be deleted without a deprecation release, so
	// callers usually report the two sets separately.
	Exported bool

	// Reads is the number of times production code consults this
	// declaration. Zero means nothing reads it.
	Reads int

	// Writes is set for fields and variables: the number of times production
	// code assigns to it. A declaration with writes and no reads is the class
	// `deadcode` structurally cannot see.
	Writes int

	// ReadsViaInterface is the part of Reads a method earned by satisfying an
	// interface that production code calls, rather than by being named
	// directly. It is reported separately because the two mean different things
	// to someone deciding whether a method can go: a direct caller names it, an
	// interface caller names the contract.
	ReadsViaInterface int
}

// Unwired reports whether nothing reads this declaration.
func (d Decl) Unwired() bool { return d.Reads == 0 }

// String renders one declaration for terminal output and for the baseline
// file, which is a diffable text format rather than JSON so a reviewer can
// read what changed.
func (d Decl) String() string {
	if d.ReadsViaInterface > 0 {
		return fmt.Sprintf("%s\t%s\t%s\treads=%d (%d via interface)", d.Kind, d.Name, d.Coord, d.Reads, d.ReadsViaInterface)
	}
	if d.Writes > 0 {
		return fmt.Sprintf("%s\t%s\t%s\treads=%d writes=%d", d.Kind, d.Name, d.Coord, d.Reads, d.Writes)
	}
	return fmt.Sprintf("%s\t%s\t%s\treads=%d", d.Kind, d.Name, d.Coord, d.Reads)
}

// ContentKey is the line-independent key for a baseline or exemption entry,
// matching the convention in astchecks.Finding.ContentKey: a declaration is
// identified by what it is and what it is called, never by where it sits. An
// unrelated edit above it must not need the baseline re-pinned, which is the
// workspace rule that a guard needing a re-pin after an unrelated edit is a
// defect in the guard.
func (d Decl) ContentKey() string {
	return string(d.Kind) + " " + d.Name
}

// Opts configures one Analyze call.
type Opts struct {
	// Dir is the module directory to load. Required.
	Dir string

	// Patterns are the package patterns to load, defaulting to "./...".
	Patterns []string

	// RepoRoot relativizes Coord. Defaults to Dir.
	RepoRoot string

	// IncludeUnexported reports unexported declarations too. A published
	// module's unexported surface can be deleted without ceremony, so it is
	// usually the more actionable half.
	IncludeUnexported bool

	// TestsAsReads counts uses inside _test.go files as reads. Off by
	// default: a declaration whose only consumer is its own test is what a
	// tracker exists to surface.
	TestsAsReads bool

	// IncludeGenerated analyses generated files. Off by default: a generated
	// declaration is regenerated, not deleted, so reporting it is noise.
	IncludeGenerated bool

	// Kinds limits the declaration kinds reported. Empty means all.
	Kinds []Kind
}

// Result is what Analyze found.
type Result struct {
	// Decls is every declaration in scope, sorted by ContentKey, with its
	// counts. Callers filter for Unwired().
	Decls []Decl

	// Packages is the number of packages analysed, and Files the number of
	// files. A caller asserts a floor on these: a scan that loaded nothing
	// reports zero unwired declarations, which reads as success.
	Packages int
	Files    int
}

// Unwired returns only the declarations nothing reads.
func (r Result) Unwired() []Decl {
	out := make([]Decl, 0, len(r.Decls))
	for _, d := range r.Decls {
		if d.Unwired() {
			out = append(out, d)
		}
	}
	return out
}

// Analyze loads the module and counts reads per declaration.
func Analyze(opts Opts) (Result, error) {
	if opts.Dir == "" {
		return Result{}, fmt.Errorf("unwired: Opts.Dir is required")
	}
	if len(opts.Patterns) == 0 {
		opts.Patterns = []string{"./..."}
	}
	// Both are made absolute, so Coord is repo-relative whatever the caller
	// passed. `-dir .` used to leave RepoRoot relative, filepath.Rel then
	// failed, and every coordinate came out absolute and machine-specific,
	// which makes a committed baseline unportable.
	absDir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return Result{}, fmt.Errorf("unwired: resolve %s: %w", opts.Dir, err)
	}
	opts.Dir = absDir
	if opts.RepoRoot == "" {
		opts.RepoRoot = absDir
	} else if abs, err := filepath.Abs(opts.RepoRoot); err == nil {
		opts.RepoRoot = abs
	}

	cfg := &packages.Config{
		// Syntax and type info for the loaded packages, and types for their
		// dependencies so a selector on an imported type resolves.
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports | packages.NeedModule,
		Dir: opts.Dir,
		// Test files are loaded so their uses can be identified and excluded
		// or counted, per TestsAsReads. Loading without them would make the
		// option unimplementable.
		Tests: true,
	}
	pkgs, loadErr := packages.Load(cfg, opts.Patterns...)
	err = loadErr
	if err != nil {
		return Result{}, fmt.Errorf("unwired: load %v: %w", opts.Patterns, err)
	}
	if len(pkgs) == 0 {
		return Result{}, fmt.Errorf("unwired: %v matched no packages in %s", opts.Patterns, opts.Dir)
	}
	// A load error means some file did not type-check, and a declaration in it
	// would report zero reads for the wrong reason. Refusing is the only safe
	// answer: a silently partial scan is how a count becomes fiction.
	var loadErrs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			loadErrs = append(loadErrs, p.PkgPath+": "+e.Error())
		}
	})
	if len(loadErrs) > 0 {
		sort.Strings(loadErrs)
		if len(loadErrs) > 10 {
			loadErrs = append(loadErrs[:10], fmt.Sprintf("... and %d more", len(loadErrs)-10))
		}
		return Result{}, fmt.Errorf("unwired: the module does not type-check, so read counts would be wrong:\n  %s",
			strings.Join(loadErrs, "\n  "))
	}

	a := &analysis{
		opts:         opts,
		reads:        map[string]int{},
		writes:       map[string]int{},
		viaInterface: map[string]int{},
		decls:        map[string]*Decl{},
		seen:         map[string]bool{},
	}

	for _, p := range pkgs {
		a.countPackage(p)
	}
	// Interface credit runs after counting and before collection: it needs the
	// interface methods' own read counts, and the concrete methods need the
	// credit before their Decl is filled in.
	a.creditInterfaceMethods(pkgs)
	for _, p := range pkgs {
		a.collectPackage(p)
	}

	out := make([]Decl, 0, len(a.decls))
	for key, d := range a.decls {
		d.Reads = a.reads[key]
		d.Writes = a.writes[key]
		if via := a.viaInterface[key]; via > 0 {
			d.ReadsViaInterface = via
			d.Reads += via
		}
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ContentKey() != out[j].ContentKey() {
			return out[i].ContentKey() < out[j].ContentKey()
		}
		return out[i].Coord < out[j].Coord
	})
	return Result{Decls: out, Packages: a.pkgCount, Files: a.fileCount}, nil
}

type analysis struct {
	opts   Opts
	reads  map[string]int
	writes map[string]int

	// viaInterface holds reads a concrete method earns through an interface it
	// satisfies. Kept apart from reads so the report can say which it was: a
	// method read only through an interface is wired, but differently.
	viaInterface map[string]int

	decls map[string]*Decl

	// seen dedupes packages, because Tests: true loads a package up to three
	// times (the package, its internal test variant, and its external test
	// package) and the same file would otherwise be counted twice.
	//
	// The same reason is why reads, writes and decls are keyed by DECLARATION
	// SITE rather than by types.Object. Each variant type-checks the sources
	// again and produces its own Object for the same declaration, so a use in
	// the test variant points at an Object the base package's map has never
	// seen. Keying by site makes the variants agree.
	seen      map[string]bool
	pkgCount  int
	fileCount int
}

// skipFile decides whether a file's contents are analysed at all.
func (a *analysis) skipFile(p *packages.Package, f *ast.File) (isTest bool, skip bool) {
	pos := p.Fset.Position(f.Pos())
	base := filepath.Base(pos.Filename)
	isTest = strings.HasSuffix(base, "_test.go")
	if !a.opts.IncludeGenerated && isGenerated(base, f) {
		return isTest, true
	}
	return isTest, false
}

// isGenerated matches the two conventions this workspace uses plus the
// cmd/go "Code generated by" line.
func isGenerated(base string, f *ast.File) bool {
	if strings.HasSuffix(base, ".pb.go") ||
		strings.HasSuffix(base, "_generated.go") ||
		strings.HasPrefix(base, "zz_generated") ||
		strings.HasSuffix(base, ".gen.go") {
		return true
	}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "// Code generated ") && strings.Contains(c.Text, "DO NOT EDIT") {
				return true
			}
		}
	}
	return false
}
