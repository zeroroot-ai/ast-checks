// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

// Command unwired counts, per declaration, how many times production code reads
// it, and fails when the set of unread declarations grows past a committed
// baseline.
//
// It exists because fourteen standing trackers across the estate carry counts
// produced by a scanner that was committed nowhere (ast-checks#13), so every
// tracker's "re-measure and leave the count updated" step was unmeetable.
//
// Each consuming repo wires a thin `make lint-unwired` of its own and keeps its
// own baseline and exemption file, per ADR-0094 and the workspace rule that a
// shared fix lives in one place and fans out mechanically.
//
// Usage:
//
//	unwired -dir . -baseline .unwired-baseline.txt
//	unwired -dir . -baseline .unwired-baseline.txt -write    # re-measure
//	unwired -dir . -kinds field -unexported                  # one class
//
// Exit codes:
//
//	0  the unread set is the baseline, or smaller
//	1  it grew, or the baseline is missing
//	2  the scan could not run (the module does not type-check, say)
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zeroroot-ai/ast-checks/unwired"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitCode(err))
	}
}

// grewError means the unread set grew. It is the only failure that is a verdict
// about the code rather than about the run, so it gets exit 1 and everything
// else gets exit 2.
type grewError struct{ msg string }

func (e *grewError) Error() string { return e.msg }

func exitCode(err error) int {
	var g *grewError
	if ok := asGrew(err, &g); ok {
		return 1
	}
	return 2
}

func asGrew(err error, target **grewError) bool {
	g, ok := err.(*grewError)
	if ok {
		*target = g
	}
	return ok
}

func run() error {
	var (
		dir        = flag.String("dir", ".", "module directory to analyze")
		patterns   = flag.String("patterns", "./...", "comma-separated package patterns")
		baseline   = flag.String("baseline", "", "baseline file of tolerated unread declarations")
		write      = flag.Bool("write", false, "rewrite the baseline from this run")
		kinds      = flag.String("kinds", "", "comma-separated kinds to report (func,method,type,field,const,var); empty means all")
		unexported = flag.Bool("unexported", false, "report unexported declarations too")
		testReads  = flag.Bool("tests-as-reads", false, "count uses inside _test.go files as reads")
		generated  = flag.Bool("generated", false, "analyze generated files too")
		listAll    = flag.Bool("all", false, "print every declaration with its counts, not only the unread ones")
	)
	flag.Parse()

	opts := unwired.Opts{
		Dir:               *dir,
		Patterns:          splitComma(*patterns),
		IncludeUnexported: *unexported,
		TestsAsReads:      *testReads,
		IncludeGenerated:  *generated,
	}
	for _, k := range splitComma(*kinds) {
		opts.Kinds = append(opts.Kinds, unwired.Kind(k))
	}

	res, err := unwired.Analyze(opts)
	if err != nil {
		return err
	}

	if *listAll {
		for _, d := range res.Decls {
			fmt.Println(d.String())
		}
		return nil
	}

	found := res.Unwired()
	fmt.Fprintf(os.Stderr, "scanned %d packages, %d files: %d of %d declarations are read nowhere\n",
		res.Packages, res.Files, len(found), len(res.Decls))

	if *baseline == "" {
		for _, d := range found {
			fmt.Println(d.String())
		}
		return nil
	}

	if *write {
		if err := unwired.WriteBaseline(*baseline, found, res); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s with %d entries\n", *baseline, len(found))
		return nil
	}

	tolerated, err := unwired.ReadBaseline(*baseline)
	if err != nil {
		return err
	}

	var added []unwired.Decl
	for _, d := range found {
		if !tolerated[d.ContentKey()] {
			added = append(added, d)
		}
	}
	var fixed []string
	current := map[string]bool{}
	for _, d := range found {
		current[d.ContentKey()] = true
	}
	for key := range tolerated {
		if !current[key] {
			fixed = append(fixed, key)
		}
	}
	sort.Strings(fixed)

	for _, key := range fixed {
		fmt.Fprintf(os.Stderr, "resolved: %s\n", key)
	}
	if len(fixed) > 0 {
		fmt.Fprintf(os.Stderr, "%d entries are no longer unread. Run with -write to shrink the baseline.\n", len(fixed))
	}

	if len(added) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d declaration(s) are read nowhere and are not in %s:\n\n", len(added), *baseline)
		for _, d := range added {
			fmt.Fprintf(&b, "  %s\n", d.String())
		}
		b.WriteString("\nEither wire it to a consumer or delete it (ADR-0094: never default to deletion).\n")
		b.WriteString("If it is a published surface that cannot go yet, add it to the baseline with a reason.\n")
		return &grewError{msg: b.String()}
	}

	fmt.Fprintln(os.Stderr, "no new unread declarations")
	return nil
}

func splitComma(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
