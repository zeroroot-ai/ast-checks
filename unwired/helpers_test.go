// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package unwired

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

func strings_HasSuffixBefore(coord, suffix string) bool {
	if i := strings.LastIndex(coord, ":"); i >= 0 {
		coord = coord[:i]
	}
	return strings.HasSuffix(coord, suffix)
}
