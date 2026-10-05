// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WalkOpts configures a single Walk invocation. The zero value walks test
// files and generated files too; a gate over production code sets
// SkipTestFiles and SkipGenerated.
type WalkOpts struct {
	// ScopeDirs is the set of directories to walk. Files outside this set
	// are not parsed. Typically `internal/` subdirs of a Go module.
	ScopeDirs []string

	// RepoRoot is used to relativize file paths in Finding.Coord. Set this
	// to the module root (parent of `go.mod`) so coords look like
	// "internal/daemon/api/server_audit.go:218".
	RepoRoot string

	// Matchers is the set of Matcher instances Walk evaluates against
	// every AST node. Composable per call site.
	Matchers []Matcher

	// Allowlist contains the known-tolerated findings. Walk filters a
	// finding whose Finding.ContentKey() ("file :: snippet") appears here.
	// An empty allowlist tolerates nothing. Keys are content keys, never
	// "file:line": a coordinate-keyed allowlist needs a re-pin after every
	// unrelated edit above the guard, and the workspace rule names that as a
	// defect in the guard. Allowlist.Validate rejects a coordinate-shaped key.
	Allowlist Allowlist

	// SkipTestFiles excludes `*_test.go` from the walk. Production code is
	// the usual analysis target, and test fixtures then do not leak into
	// findings.
	SkipTestFiles bool

	// SkipGenerated excludes generated `.pb.go` and `zz_generated*.go`
	// files.
	SkipGenerated bool

	// ExtraSkipSuffixes adds extra suffixes (like `.gen.go`) to skip.
	ExtraSkipSuffixes []string
}

// Walk parses every non-skipped `.go` file under opts.ScopeDirs and
// returns the set of findings whose Finding.ContentKey() is NOT in
// opts.Allowlist. The returned findings are sorted by Coord.
//
// Walk validates opts.Allowlist before walking; a malformed allowlist
// produces an error and no findings are returned.
//
// Allowlisted findings are still discovered but not returned; callers
// who want to log the allowlist (the typical pattern in tests) should
// iterate over opts.Allowlist after Walk returns.
//
// Walk does not say which allowlist entries matched nothing. WalkReport does.
func Walk(opts WalkOpts) ([]Finding, error) {
	report, err := WalkReport(opts)
	return report.Findings, err
}

// Report is what one walk found: the findings the allowlist does not
// tolerate, and the allowlist entries that tolerate nothing.
type Report struct {
	// Findings are the findings whose content key is not in the allowlist,
	// sorted by Coord. This is what Walk returns.
	Findings []Finding

	// StaleAllowlist holds each allowlist key that matched no finding in
	// this walk, sorted. The guard it tolerated is gone, or its file or text
	// changed. Such an entry records a decision about nothing, and it is a
	// ready exemption for the next guard with the same text (ADR-0094).
	StaleAllowlist []string
}

// WalkReport is Walk plus the allowlist entries that matched no finding.
//
// An entry is stale only against the ScopeDirs and Matchers of this call. A
// caller that splits one allowlist over several walks must union the used
// entries itself; one walk over one allowlist is the shape this serves.
func WalkReport(opts WalkOpts) (Report, error) {
	if err := opts.Allowlist.Validate(); err != nil {
		return Report{}, err
	}
	if len(opts.Matchers) == 0 {
		return Report{}, fmt.Errorf("Walk: no matchers configured")
	}

	var all []Finding
	for _, dir := range opts.ScopeDirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "testdata" || info.Name() == "vendor" || info.Name() == ".worktrees" {
					return filepath.SkipDir
				}
				return nil
			}
			if !opts.shouldParse(path) {
				return nil
			}
			fileFindings, err := analyzeFile(path, opts)
			if err != nil {
				return err
			}
			all = append(all, fileFindings...)
			return nil
		})
		if err != nil {
			return Report{}, err
		}
	}

	// Filter against the allowlist by content key (repo-relative file +
	// snippet). The key carries no line number, so an unrelated edit above a
	// guard never changes whether the guard is tolerated. See Finding.ContentKey.
	var filtered []Finding
	used := map[string]bool{}
	for _, f := range all {
		f.Coord = relativizeCoord(f.Coord, opts.RepoRoot)
		if _, ok := opts.Allowlist[f.ContentKey()]; ok {
			used[f.ContentKey()] = true
			continue
		}
		filtered = append(filtered, f)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Coord < filtered[j].Coord
	})

	var stale []string
	for key := range opts.Allowlist {
		if !used[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	return Report{Findings: filtered, StaleAllowlist: stale}, nil
}

func (o WalkOpts) shouldParse(path string) bool {
	// Accept both `.go` (production) and `.go.txt` (fixture convention —
	// keeps fixtures from being picked up by `go build` / `go test`).
	if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".go.txt") {
		return false
	}
	base := filepath.Base(path)
	if o.SkipTestFiles && strings.HasSuffix(base, "_test.go") {
		return false
	}
	if o.SkipGenerated {
		if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_grpc.pb.go") {
			return false
		}
		if strings.HasPrefix(base, "zz_generated") {
			return false
		}
	}
	for _, suf := range o.ExtraSkipSuffixes {
		if strings.HasSuffix(base, suf) {
			return false
		}
	}
	return true
}

func analyzeFile(path string, opts WalkOpts) ([]Finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var findings []Finding
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		for _, m := range opts.Matchers {
			matched, snippet := m.Match(fset, n, src)
			if matched {
				pos := fset.Position(n.Pos())
				findings = append(findings, Finding{
					Coord:    CoordFromPos(pos),
					Snippet:  snippet,
					Category: m.Name(),
					Rule:     m.Rule(),
				})
			}
		}
		return true
	})
	return findings, nil
}

func relativizeCoord(coord, repoRoot string) string {
	if repoRoot == "" {
		return coord
	}
	idx := strings.LastIndex(coord, ":")
	if idx < 0 {
		return coord
	}
	abs := coord[:idx]
	line := coord[idx+1:]
	if rel, err := filepath.Rel(repoRoot, abs); err == nil {
		return rel + ":" + line
	}
	return coord
}
