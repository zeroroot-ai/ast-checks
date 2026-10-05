// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package unwired

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// countPackage records every use in the package, split into reads and writes.
//
// The split is computed by first marking the identifier positions that are
// write targets, then treating every remaining use as a read. Marking by
// position rather than by object is what makes it correct for a field used both
// ways in one function: `x.Field = x.Field + 1` is one write and one read.
func (a *analysis) countPackage(p *packages.Package) {
	if p.TypesInfo == nil {
		return
	}
	for _, f := range p.Syntax {
		isTest, skip := a.skipFile(p, f)
		if skip {
			continue
		}
		if isTest && !a.opts.TestsAsReads {
			continue
		}

		writePositions, keyPositions := writeTargets(f)

		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := p.TypesInfo.Uses[id]
			if obj == nil {
				// Not a use: either a definition, or an identifier with no
				// object such as a label or a blank.
				return true
			}
			key := a.objKey(p, obj)
			if key == "" {
				return true
			}
			if writePositions[id.Pos()] || (keyPositions[id.Pos()] && isField(obj)) {
				a.writes[key]++
				return true
			}
			a.reads[key]++
			return true
		})
	}
}

// isField reports whether obj is a struct field.
func isField(obj types.Object) bool {
	v, ok := obj.(*types.Var)
	return ok && v.IsField()
}

// writeTargets returns the positions of identifiers that are assignment
// targets rather than values, in two sets. An identifier in writes is a
// write. An identifier in literalKeys is the bare key of a composite literal,
// and it is a write only when it names a struct field.
//
// Three forms are writes:
//
//	x.Field = v      and every other left-hand side of =, :=, and the
//	                 compound forms
//	T{Field: v}      a composite literal key, which names a field without
//	                 consulting it
//	x.Field++        IncDec, which the spec defines as x = x + 1 but which
//	                 nobody reads the result of
//
// A COMPOUND assignment (`+=`) is deliberately NOT marked: it consults the old
// value, so it is a read as well, and counting it as a write only would make a
// field that is only ever accumulated look unread.
//
// Only the OUTERMOST identifier of a left-hand side is a write. In `x.Field =
// v`, `Field` is written but `x` is read: you cannot assign through x without
// consulting it. In `m[k] = v`, `m` and `k` are both read.
func writeTargets(f *ast.File) (writes, literalKeys map[token.Pos]bool) {
	out := map[token.Pos]bool{}
	keys := map[token.Pos]bool{}

	mark := func(e ast.Expr) {
		switch t := e.(type) {
		case *ast.Ident:
			out[t.Pos()] = true
		case *ast.SelectorExpr:
			// The field is written; the receiver expression is read, so it is
			// left unmarked and the walker counts it normally.
			out[t.Sel.Pos()] = true
		case *ast.StarExpr, *ast.IndexExpr:
			// Writing through a pointer or into a map/slice element reads the
			// container. Nothing is marked.
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			if t.Tok == token.ASSIGN || t.Tok == token.DEFINE {
				for _, lhs := range t.Lhs {
					mark(lhs)
				}
			}
			// Compound assignment (+=, |=, …) reads and writes. Leaving it
			// unmarked counts the read, which is the conservative answer: it
			// cannot make a live field look dead.
		case *ast.IncDecStmt:
			mark(t.X)
		case *ast.CompositeLit:
			for _, elt := range t.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				// In a struct literal the key names a field, and that is a
				// write. In a map or array literal the key is a value: a
				// constant or a variable, and that is a read. The syntax
				// does not tell the two apart, so a bare identifier key goes
				// in its own set and countPackage decides by the object kind.
				if id, ok := kv.Key.(*ast.Ident); ok {
					keys[id.Pos()] = true
				}
			}
		}
		return true
	})
	return out, keys
}

// collectPackage records every declaration in the package.
func (a *analysis) collectPackage(p *packages.Package) {
	if p.TypesInfo == nil || p.Types == nil {
		return
	}
	// Tests: true loads a package several times. Count each distinct package
	// path once so Packages and Files are not inflated.
	if !a.seen[p.PkgPath] {
		a.seen[p.PkgPath] = true
		a.pkgCount++
	}

	for _, f := range p.Syntax {
		isTest, skip := a.skipFile(p, f)
		if skip || isTest {
			// A declaration inside a test file is test scaffolding, not
			// surface, whatever TestsAsReads says.
			continue
		}
		a.fileCount++
		a.collectFile(p, f)
	}
}

func (a *analysis) collectFile(p *packages.Package, f *ast.File) {
	pkgName := p.Name
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			a.addFunc(p, pkgName, decl)
		case *ast.GenDecl:
			a.addGenDecl(p, pkgName, decl)
		}
	}
}

func (a *analysis) addFunc(p *packages.Package, pkgName string, fd *ast.FuncDecl) {
	obj := p.TypesInfo.Defs[fd.Name]
	if obj == nil {
		return
	}
	kind := KindFunc
	name := pkgName + "." + fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		kind = KindMethod
		name = pkgName + "." + receiverTypeName(fd.Recv.List[0].Type) + "." + fd.Name.Name
	}
	// main and init are entry points: the runtime reads them, not the code.
	if fd.Recv == nil && (fd.Name.Name == "main" || fd.Name.Name == "init") {
		return
	}
	a.add(p, obj, kind, name)
}

func receiverTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // a generic receiver, Foo[T]
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		return receiverTypeName(t.X)
	}
	return "?"
}

func (a *analysis) addGenDecl(p *packages.Package, pkgName string, gd *ast.GenDecl) {
	for _, spec := range gd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			obj := p.TypesInfo.Defs[s.Name]
			if obj == nil {
				continue
			}
			a.add(p, obj, KindType, pkgName+"."+s.Name.Name)
			a.addFields(p, pkgName+"."+s.Name.Name, s)
		case *ast.ValueSpec:
			kind := KindVar
			if gd.Tok == token.CONST {
				kind = KindConst
			}
			for _, n := range s.Names {
				if n.Name == "_" {
					continue
				}
				obj := p.TypesInfo.Defs[n]
				if obj == nil {
					continue
				}
				a.add(p, obj, kind, pkgName+"."+n.Name)
			}
		}
	}
}

// addFields records a struct's fields, which is the class deadcode cannot see.
func (a *analysis) addFields(p *packages.Package, typeName string, s *ast.TypeSpec) {
	st, ok := s.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			// An embedded field. Its name is its type's, and uses of the
			// embedded type are already counted against that type.
			continue
		}
		for _, n := range field.Names {
			if n.Name == "_" {
				continue
			}
			obj := p.TypesInfo.Defs[n]
			if obj == nil {
				continue
			}
			a.add(p, obj, KindField, typeName+"."+n.Name)
		}
	}
}

// objKey identifies a declaration by where it is declared, so the several
// package variants that Tests: true produces agree about it. An object with no
// position (a universe builtin) has no key and is not counted.
func (a *analysis) objKey(p *packages.Package, obj types.Object) string {
	if !obj.Pos().IsValid() {
		return ""
	}
	pos := p.Fset.Position(obj.Pos())
	return fmt.Sprintf("%s:%d:%d:%s", pos.Filename, pos.Line, pos.Column, obj.Name())
}

func (a *analysis) add(p *packages.Package, obj types.Object, kind Kind, name string) {
	key := a.objKey(p, obj)
	if key == "" {
		return
	}
	if _, dup := a.decls[key]; dup {
		return
	}
	if !obj.Exported() && !a.opts.IncludeUnexported {
		return
	}
	if len(a.opts.Kinds) > 0 {
		var want bool
		for _, k := range a.opts.Kinds {
			if k == kind {
				want = true
				break
			}
		}
		if !want {
			return
		}
	}
	a.decls[key] = &Decl{
		Kind:     kind,
		Name:     name,
		Coord:    a.coord(p, obj.Pos()),
		Exported: obj.Exported(),
	}
}

func (a *analysis) coord(p *packages.Package, pos token.Pos) string {
	position := p.Fset.Position(pos)
	file := position.Filename
	if rel, err := filepath.Rel(a.opts.RepoRoot, file); err == nil && !strings.HasPrefix(rel, "..") {
		file = rel
	}
	return fmt.Sprintf("%s:%d", file, position.Line)
}

// creditInterfaceMethods attributes reads through an interface to the concrete
// methods that satisfy it.
//
// Without this, every method of every parser, plugin and handler in the estate
// reports zero reads: the call site names the INTERFACE method, and the concrete
// implementation's identifier appears only at its own declaration. Measured on
// gibson-executor, 35 of 61 reported declarations were interface
// implementations, which would have made the whole report untrustworthy.
//
// The rule: a concrete method is read as often as the interface method it
// satisfies, when the receiver type implements an interface declared in the
// analyzed packages that has a method of that name. A type satisfying several
// such interfaces takes the highest count, because one real consumer is enough
// to make it wired.
//
// This can only ADD reads, so it cannot hide a declaration nothing uses: an
// interface nobody calls contributes zero.
func (a *analysis) creditInterfaceMethods(pkgs []*packages.Package) {
	type iface struct {
		typ *types.Interface
		// reads per method name, from the interface's own declaration site.
		methodReads map[string]int
	}
	var ifaces []iface

	for _, p := range pkgs {
		if p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			obj, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			it, ok := obj.Type().Underlying().(*types.Interface)
			if !ok || it.NumMethods() == 0 {
				continue
			}
			mr := map[string]int{}
			for i := 0; i < it.NumMethods(); i++ {
				m := it.Method(i)
				mr[m.Name()] = a.reads[a.objKey(p, m)]
			}
			ifaces = append(ifaces, iface{typ: it, methodReads: mr})
		}
	}
	if len(ifaces) == 0 {
		return
	}

	for _, p := range pkgs {
		if p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			obj, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := obj.Type().(*types.Named)
			if !ok {
				continue
			}
			for _, recv := range []types.Type{named, types.NewPointer(named)} {
				for _, candidate := range ifaces {
					if !types.Implements(recv, candidate.typ) {
						continue
					}
					ms := types.NewMethodSet(recv)
					for i := 0; i < ms.Len(); i++ {
						fn, ok := ms.At(i).Obj().(*types.Func)
						if !ok {
							continue
						}
						viaIface, named := candidate.methodReads[fn.Name()]
						if !named || viaIface == 0 {
							continue
						}
						key := a.objKey(p, fn)
						if key == "" {
							continue
						}
						if viaIface > a.viaInterface[key] {
							a.viaInterface[key] = viaIface
						}
					}
				}
			}
		}
	}
}
