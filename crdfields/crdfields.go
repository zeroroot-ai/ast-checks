// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

// Package crdfields decides whether every served CRD field has a consumer.
//
// A CRD field with no reader is a promise the API server makes and the operator
// does not keep: `kubectl explain` documents it, the OpenAPI schema validates
// it, the object is admitted, and the cluster does something else (ADR-0094
// layer 6).
//
// # What counts as a consumer
//
//  1. A Go read of the field, resolved by the type checker. The reads come from
//     the unwired analyzer, so a repo has one answer to "is this read".
//  2. A `+kubebuilder:printcolumn` marker whose JSONPath names the field. Such
//     a field exists to be displayed. Every hop on a resolved path is credited:
//     a column on .status.stores[*].state shows the stores container as much as
//     the leaf.
//
// A write is not a read. Generated code is never a consumer, because the
// analyzer skips generated files and this package judges only *_types.go.
//
// # Why this is one package
//
// The gate started as a shell script in setec and was copied into gibson. The
// copies drifted in a week: gibson credited every hop of a print column, read
// bracketed JSONPath segments and skipped List kinds, and setec did none of
// that. A shared fix lives in one place and fans out mechanically.
package crdfields

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/zeroroot-ai/ast-checks/unwired"
)

const rootMarker = "+kubebuilder:object:root=true"

var (
	printColumn  = regexp.MustCompile("\\+kubebuilder:printcolumn:.*?JSONPath=[`\"]([^`\"]+)[`\"]")
	verdictRef   = regexp.MustCompile(`(#\d+|ADR-\d+|https?://)`)
	pathBrackets = regexp.MustCompile(`\[.*$`)
)

// Config is one gate run.
type Config struct {
	// Dir is the module directory. TypesDirs and the analyzer coordinates are
	// relative to it.
	Dir string

	// TypesDirs are the api directories that hold the *_types.go files. A
	// module that serves several CRD api packages names each one.
	TypesDirs []string

	// ExemptFile holds the recorded verdicts, one per line:
	// "<pkg>.<Type>.<Field> | <reference> | <reason>". A missing file is an
	// empty list.
	ExemptFile string

	// MinServed is the plausibility floor. A run that resolves fewer served
	// fields than this measured nothing and must not report ok.
	MinServed int
}

// Blocked is one served field with no consumer and no verdict.
type Blocked struct {
	// Key is "<pkg>.<Type>.<Field>", the analyzer name of the field.
	Key string
	// Coord is "file:line".
	Coord string
	// Reads and Writes are the analyzer counts.
	Reads, Writes int
	// HasJSONTag is false for a field that is not in the served schema at all.
	HasJSONTag bool
}

// Outcome is the verdict of one run.
type Outcome struct {
	// Served is the number of served fields judged.
	Served int
	// ResolvedColumns is the number of print columns that resolved to a field
	// of the schema.
	ResolvedColumns int
	// Verdicts is the number of recorded exemptions.
	Verdicts int

	// Stale are exemptions that name a field that no longer exists.
	Stale []string
	// Satisfied are exemptions for a field that now has a consumer.
	Satisfied []string
	// Blocking are the served fields with no consumer and no verdict.
	Blocking []Blocked
	// BelowFloor is set when Served is under Config.MinServed.
	BelowFloor bool

	exemptFile string
	minServed  int
	typesDirs  []string
}

// OK reports whether the gate passes.
func (o Outcome) OK() bool {
	return len(o.Stale) == 0 && len(o.Satisfied) == 0 && len(o.Blocking) == 0 && !o.BelowFloor
}

type schemaField struct {
	json string // the json name, empty when the field has no json tag
	typ  string // the bare type name
}

type schema struct {
	// fields[Type][Field]
	fields map[string]map[string]schemaField
	// displayed holds "Type.Field" for every hop of a resolved print column.
	displayed       map[string]bool
	resolvedColumns int
}

// Judge decides the gate from the analyzer's field declarations. decls is the
// full field list of the module, read or not: the result of an unwired.Analyze
// call restricted to unwired.KindField.
func Judge(cfg Config, decls []unwired.Decl) (Outcome, error) {
	sch, err := loadSchema(cfg)
	if err != nil {
		return Outcome{}, err
	}
	exempt, err := loadExemptions(cfg)
	if err != nil {
		return Outcome{}, err
	}

	typesDirs := map[string]bool{}
	for _, d := range cfg.TypesDirs {
		typesDirs[filepath.Clean(d)] = true
	}

	out := Outcome{
		ResolvedColumns: sch.resolvedColumns,
		Verdicts:        len(exempt),
		exemptFile:      cfg.ExemptFile,
		minServed:       cfg.MinServed,
		typesDirs:       cfg.TypesDirs,
	}
	served := map[string]bool{}
	unread := map[string]Blocked{}
	for _, d := range decls {
		if d.Kind != unwired.KindField {
			continue
		}
		file := d.Coord
		if i := strings.LastIndex(file, ":"); i >= 0 {
			file = file[:i]
		}
		if !strings.HasSuffix(file, "_types.go") || !typesDirs[relDir(cfg.Dir, file)] {
			continue
		}
		pkgless := d.Name
		if i := strings.Index(pkgless, "."); i >= 0 {
			pkgless = pkgless[i+1:]
		}
		typ, field, ok := strings.Cut(pkgless, ".")
		if !ok {
			continue
		}
		// A *List kind is apimachinery plumbing: client.List decodes Items by
		// reflection and no operator sets it. It is not a served promise.
		if strings.HasSuffix(typ, "List") {
			continue
		}
		served[d.Name] = true
		if d.Reads > 0 || sch.displayed[pkgless] {
			continue
		}
		unread[d.Name] = Blocked{
			Key:        d.Name,
			Coord:      d.Coord,
			Reads:      d.Reads,
			Writes:     d.Writes,
			HasJSONTag: sch.fields[typ][field].json != "",
		}
	}
	out.Served = len(served)

	// Two ways an entry rots, and both read as a live decision: the field no
	// longer exists, so the verdict is about nothing, or the field now has a
	// consumer, so the verdict would hide the consumer being removed again.
	for key := range exempt {
		switch _, isUnread := unread[key]; {
		case !served[key]:
			out.Stale = append(out.Stale, key)
		case !isUnread:
			out.Satisfied = append(out.Satisfied, key)
		}
	}
	sort.Strings(out.Stale)
	sort.Strings(out.Satisfied)

	for key, b := range unread {
		if !exempt[key] {
			out.Blocking = append(out.Blocking, b)
		}
	}
	sort.Slice(out.Blocking, func(i, j int) bool { return out.Blocking[i].Key < out.Blocking[j].Key })

	// A floor. A walk that resolves nothing would otherwise read as a clean
	// run, which is the shape of every guard that could not fail.
	out.BelowFloor = out.Served < cfg.MinServed
	return out, nil
}

// relDir returns the directory of file relative to dir, cleaned. The analyzer
// reports a repo-relative coordinate and a test fixture an absolute one.
func relDir(dir, file string) string {
	d := filepath.Dir(file)
	if filepath.IsAbs(d) {
		if abs, err := filepath.Abs(dir); err == nil {
			if rel, err := filepath.Rel(abs, d); err == nil {
				d = rel
			}
		}
	}
	return filepath.Clean(d)
}

func loadSchema(cfg Config) (schema, error) {
	sch := schema{fields: map[string]map[string]schemaField{}, displayed: map[string]bool{}}
	type column struct{ root, path string }
	var columns []column
	// Several api packages share the package name v1alpha1, and the analyzer
	// keys a field as <package>.<Type>.<Field>, so a struct name repeated
	// across the directories would make one exemption silence two fields.
	declaredIn := map[string]string{}

	for _, typesDir := range cfg.TypesDirs {
		dir := filepath.Join(cfg.Dir, typesDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return schema{}, fmt.Errorf("crdfields: read types dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_types.go") {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ParseComments)
			if err != nil {
				return schema{}, fmt.Errorf("crdfields: parse %s: %w", e.Name(), err)
			}

			// Markers are collected per file and attached to the file's root
			// object, not tracked by adjacency to the next type. A gofmt'd
			// types file puts a blank line between the marker block and the
			// doc comment, so adjacency loses every marker. Each *_types.go
			// declares one root object plus its List, and the List carries no
			// print columns.
			var structs []*ast.TypeSpec
			ast.Inspect(file, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				if _, isStruct := ts.Type.(*ast.StructType); isStruct {
					structs = append(structs, ts)
				}
				return false
			})
			var roots, paths []string
			for _, cg := range file.Comments {
				for _, c := range cg.List {
					if strings.Contains(c.Text, rootMarker) {
						for _, ts := range structs {
							if ts.Pos() > c.Pos() {
								roots = append(roots, ts.Name.Name)
								break
							}
						}
					}
					if m := printColumn.FindStringSubmatch(c.Text); m != nil {
						paths = append(paths, m[1])
					}
				}
			}

			for _, ts := range structs {
				name := ts.Name.Name
				if prev, seen := declaredIn[name]; seen && prev != typesDir {
					return schema{}, fmt.Errorf("crdfields: struct %s is declared in both %s and %s; "+
						"the analyzer key cannot tell them apart, so rename one", name, prev, typesDir)
				}
				declaredIn[name] = typesDir
				if sch.fields[name] == nil {
					sch.fields[name] = map[string]schemaField{}
				}
				for _, f := range ts.Type.(*ast.StructType).Fields.List {
					sf := schemaField{typ: bareType(f.Type)}
					if f.Tag != nil {
						tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
						sf.json, _, _ = strings.Cut(tag.Get("json"), ",")
					}
					for _, n := range f.Names {
						if n.IsExported() {
							sch.fields[name][n.Name] = sf
						}
					}
				}
			}

			var object string
			for _, r := range roots {
				if !strings.HasSuffix(r, "List") {
					object = r
					break
				}
			}
			if len(paths) > 0 && object == "" {
				return schema{}, fmt.Errorf("crdfields: %s declares print columns but no +kubebuilder:object:root type; "+
					"the columns cannot be resolved to a field", e.Name())
			}
			for _, p := range paths {
				columns = append(columns, column{root: object, path: p})
			}
		}
	}

	// A path that leaves the package (.metadata.creationTimestamp) resolves to
	// nothing, which is correct: there is no field of ours to credit. A segment
	// may carry a filter or a wildcard (`stores[*]`,
	// `conditions[?(@.type=="Ready")]`); the json name precedes the bracket.
	for _, c := range columns {
		cur, hops, ok := c.root, []string(nil), true
		for _, seg := range strings.Split(c.path, ".") {
			seg = pathBrackets.ReplaceAllString(seg, "")
			if seg == "" {
				continue
			}
			hit := ""
			for name, f := range sch.fields[cur] {
				if f.json == seg {
					hit = name
					break
				}
			}
			if hit == "" {
				ok = false
				break
			}
			hops = append(hops, cur+"."+hit)
			cur = sch.fields[cur][hit].typ
		}
		if ok && len(hops) > 0 {
			for _, h := range hops {
				sch.displayed[h] = true
			}
			sch.resolvedColumns++
		}
	}
	return sch, nil
}

// bareType strips pointer, slice and map decoration down to a type name.
func bareType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return bareType(t.X)
	case *ast.ArrayType:
		return bareType(t.Elt)
	case *ast.MapType:
		return bareType(t.Value)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return bareType(t.X) + "." + t.Sel.Name
	}
	return ""
}

func loadExemptions(cfg Config) (map[string]bool, error) {
	exempt := map[string]bool{}
	if cfg.ExemptFile == "" {
		return exempt, nil
	}
	path := cfg.ExemptFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(cfg.Dir, path)
	}
	f, err := os.Open(path) //nolint:gosec // G304: the repo's own exemption file
	if os.IsNotExist(err) {
		return exempt, nil
	}
	if err != nil {
		return nil, fmt.Errorf("crdfields: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only

	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		// A comment is a line that STARTS with #. There are no inline
		// comments, because an issue reference is "#121" and stripping from
		// the first # eats every reference column.
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return nil, fmt.Errorf(`crdfields: %s:%d: expected "<pkg>.<Type>.<Field> | <reference> | <reason>"`, cfg.ExemptFile, n)
		}
		if !verdictRef.MatchString(parts[1]) {
			return nil, fmt.Errorf("crdfields: %s:%d: %s has no issue or ADR reference; "+
				"a verdict with no place to read it is an opinion", cfg.ExemptFile, n, parts[0])
		}
		exempt[parts[0]] = true
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("crdfields: read %s: %w", cfg.ExemptFile, err)
	}
	return exempt, nil
}

// Report renders the outcome for a terminal and for a CI log.
func (o Outcome) Report() string {
	var b strings.Builder
	if len(o.Stale) > 0 {
		fmt.Fprintf(&b, "::error::%s names %d field(s) that no longer exist:\n", o.exemptFile, len(o.Stale))
		for _, k := range o.Stale {
			fmt.Fprintf(&b, "::error::  %s\n", k)
		}
		b.WriteString("::error::Delete the entry. An exemption outliving its field records a decision about nothing.\n")
	}
	if len(o.Satisfied) > 0 {
		fmt.Fprintf(&b, "::error::%s records a verdict for %d field(s) that now HAVE a consumer:\n", o.exemptFile, len(o.Satisfied))
		for _, k := range o.Satisfied {
			fmt.Fprintf(&b, "::error::  %s\n", k)
		}
		b.WriteString("::error::Delete the entry. The promise is kept, and an exemption left in place " +
			"would hide the consumer being removed again.\n")
	}
	if len(o.Blocking) > 0 {
		fmt.Fprintf(&b, "\nBLOCKED: %d served CRD field(s) with no consumer.\n\n", len(o.Blocking))
		for _, bl := range o.Blocking {
			counts := fmt.Sprintf("reads=%d", bl.Reads)
			if bl.Writes > 0 {
				counts += fmt.Sprintf(" writes=%d", bl.Writes)
			}
			kind := "review"
			if !bl.HasJSONTag {
				kind = "FAILURE: no json tag, so it is not in the served schema at all"
			}
			fmt.Fprintf(&b, "  %-52s %s  %s\n       %s\n", bl.Key, counts, bl.Coord, kind)
		}
		b.WriteString("\nEach one is a promise kubectl explain prints and the cluster does not keep.\n")
		b.WriteString("ADR-0094 rule 5: build the consumer, or delete the producer. Never default\n")
		b.WriteString("to deletion. A print column counts as a consumer when the field exists only\n")
		b.WriteString("to be displayed.\n\n")
		fmt.Fprintf(&b, "To record a verdict instead, add a line to %s:\n\n", o.exemptFile)
		b.WriteString("    <pkg>.<Type>.<Field> | <#issue or ADR> | <why this is the right state>\n\n")
	}
	if len(o.Stale) == 0 && len(o.Satisfied) == 0 && len(o.Blocking) == 0 {
		fmt.Fprintf(&b, "ok  %d served field(s) checked, %d print column(s) resolved, %d verdict(s) recorded\n",
			o.Served, o.ResolvedColumns, o.Verdicts)
		if o.BelowFloor {
			fmt.Fprintf(&b, "::error::only %d served field(s) were found, below the floor of %d; "+
				"the analyzer output is not reaching %s\n", o.Served, o.minServed, strings.Join(o.typesDirs, " "))
		}
	}
	return b.String()
}
