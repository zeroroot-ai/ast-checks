// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package unwired

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// workspaceFor writes a temporary go.work that uses the scanned module and
// each consumer module, and returns its path, one package pattern for each
// consumer, the module path of the scanned module, and a cleanup function.
// The file lives in a temporary directory and is never part of any
// repository.
func workspaceFor(mainDir string, consumers []string) (work string, patterns []string, mainModule string, cleanup func(), err error) {
	mainModule, goVersion, err := readModule(mainDir)
	if err != nil {
		return "", nil, "", nil, err
	}
	uses := []string{mainDir}
	for _, c := range consumers {
		abs, aerr := filepath.Abs(c)
		if aerr != nil {
			return "", nil, "", nil, fmt.Errorf("unwired: consumer %s: %w", c, aerr)
		}
		mod, v, rerr := readModule(abs)
		if rerr != nil {
			return "", nil, "", nil, rerr
		}
		if mod == mainModule {
			return "", nil, "", nil, fmt.Errorf("unwired: consumer %s is the scanned module %s", c, mod)
		}
		if semver.Compare("v"+v, "v"+goVersion) > 0 {
			goVersion = v
		}
		uses = append(uses, abs)
		patterns = append(patterns, mod+"/...")
	}

	dir, err := os.MkdirTemp("", "unwired-work-")
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("unwired: workspace: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "go %s\n\nuse (\n", goVersion)
	for _, u := range uses {
		fmt.Fprintf(&b, "\t%s\n", u)
	}
	b.WriteString(")\n")
	work = filepath.Join(dir, "go.work")
	if err := os.WriteFile(work, []byte(b.String()), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, "", nil, fmt.Errorf("unwired: workspace: %w", err)
	}
	return work, patterns, mainModule, func() { _ = os.RemoveAll(dir) }, nil
}

// readModule returns the module path and the go version of the module in dir.
func readModule(dir string) (path, goVersion string, err error) {
	name := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(name) // #nosec G304 -- an operator-supplied module directory.
	if err != nil {
		return "", "", fmt.Errorf("unwired: %w", err)
	}
	f, err := modfile.ParseLax(name, data, nil)
	if err != nil {
		return "", "", fmt.Errorf("unwired: %s: %w", name, err)
	}
	if f.Module == nil || f.Go == nil {
		return "", "", fmt.Errorf("unwired: %s has no module or go line", name)
	}
	return f.Module.Mod.Path, f.Go.Version, nil
}
