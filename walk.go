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

// WalkOpts configures a single Walk invocation. Construct via NewWalkOpts
// for a sensible default set, then customize.
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

	// Allowlist contains the known-tolerated findings. Walk filters
	// findings whose key appears here. Empty allowlist means no findings
	// are tolerated. The key is Coord ("file:line") by default, or
	// Finding.ContentKey() ("file :: snippet") when AllowlistByContent is set.
	Allowlist Allowlist

	// AllowlistByContent makes Walk match the Allowlist by Finding.ContentKey()
	// (file path + guard snippet) instead of by Coord (file:line). RECOMMENDED:
	// a content-keyed allowlist is immune to line shifts (license headers, added
	// imports/comments) and file-internal moves, which otherwise red the gate on
	// every unrelated edit. Coord-keyed (default) is retained for back-compat
	// with existing consumers; new gates should set this true and key their
	// allowlist with ContentKey values. See Finding.ContentKey.
	AllowlistByContent bool

	// SkipTestFiles excludes `*_test.go` from the walk. Defaults to true
	// in NewWalkOpts. Production code is the analysis target; test files
	// are scoped out so test fixtures don't leak into findings.
	SkipTestFiles bool

	// SkipGenerated excludes generated `.pb.go` and `zz_generated*.go`
	// files. Defaults to true in NewWalkOpts.
	SkipGenerated bool

	// ExtraSkipSuffixes adds extra suffixes (like `.gen.go`) to skip.
	ExtraSkipSuffixes []string
}

// NewWalkOpts returns a WalkOpts populated with sensible defaults
// (SkipTestFiles + SkipGenerated). Set ScopeDirs, RepoRoot, Matchers, and
// Allowlist before calling Walk.
func NewWalkOpts() WalkOpts {
	return WalkOpts{
		SkipTestFiles: true,
		SkipGenerated: true,
	}
}

// Walk parses every non-skipped `.go` file under opts.ScopeDirs and
// returns the set of findings whose Coord is NOT in opts.Allowlist. The
// returned findings are sorted by Coord.
//
// Walk validates opts.Allowlist before walking; a malformed allowlist
// produces an error and no findings are returned.
//
// Allowlisted findings are still discovered but not returned; callers
// who want to log the allowlist (the typical pattern in tests) should
// iterate over opts.Allowlist after Walk returns.
func Walk(opts WalkOpts) ([]Finding, error) {
	if err := opts.Allowlist.Validate(); err != nil {
		return nil, err
	}
	if len(opts.Matchers) == 0 {
		return nil, fmt.Errorf("Walk: no matchers configured")
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
			return nil, err
		}
	}

	// Filter against allowlist using repo-relative coords. The lookup key is
	// the content key (file + snippet) when AllowlistByContent is set, else the
	// repo-relative Coord (file:line). Content keying survives line shifts; see
	// Finding.ContentKey and WalkOpts.AllowlistByContent.
	var filtered []Finding
	for _, f := range all {
		f.Coord = relativizeCoord(f.Coord, opts.RepoRoot)
		key := f.Coord
		if opts.AllowlistByContent {
			key = f.ContentKey()
		}
		if _, ok := opts.Allowlist[key]; ok {
			continue
		}
		filtered = append(filtered, f)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Coord < filtered[j].Coord
	})
	return filtered, nil
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
