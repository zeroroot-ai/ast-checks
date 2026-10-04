// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

// Command crdfields fails when a served CRD field has no consumer and no
// recorded verdict (ADR-0094 layer 6).
//
// It is the one implementation of a gate that setec and gibson each carried as
// a shell script. The two copies drifted, so the decision moved here beside the
// analyzer that counts the reads. Each consuming repo wires a thin make target
// and keeps its own exemption file.
//
// Usage:
//
//	crdfields -dir . -types api/v1alpha1 -exempt scripts/crd-field-consumers-exempt.txt
//	crdfields -dir . -types a/api/v1alpha1,b/api/v1alpha1 -tags setec_integration
//
// Exit codes:
//
//	0  every served field has a consumer or a recorded verdict
//	1  at least one does not, an exemption is stale, or the floor was missed
//	2  the gate could not run (the module does not type-check, say)
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/zeroroot-ai/ast-checks/crdfields"
	"github.com/zeroroot-ai/ast-checks/unwired"
)

func main() {
	var (
		dir       = flag.String("dir", ".", "module directory to analyze")
		types     = flag.String("types", "api/v1alpha1", "comma-separated api directories that hold the *_types.go files")
		exempt    = flag.String("exempt", "scripts/crd-field-consumers-exempt.txt", "file of recorded verdicts")
		minServed = flag.Int("min-served", 50, "plausibility floor: fewer served fields than this is a run that measured nothing")
		tags      = flag.String("tags", "", "comma-separated build tags the shipped image is built with")
	)
	flag.Parse()

	res, err := unwired.Analyze(unwired.Opts{
		Dir:       *dir,
		Kinds:     []unwired.Kind{unwired.KindField},
		BuildTags: splitComma(*tags),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	out, err := crdfields.Judge(crdfields.Config{
		Dir:        *dir,
		TypesDirs:  splitComma(*types),
		ExemptFile: *exempt,
		MinServed:  *minServed,
	}, res.Decls)
	if err != nil {
		fmt.Fprintln(os.Stderr, "::error::"+err.Error())
		os.Exit(1)
	}
	fmt.Print(out.Report())
	if !out.OK() {
		os.Exit(1)
	}
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
