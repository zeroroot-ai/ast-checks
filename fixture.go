// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package astchecks

import (
	"fmt"
	"sort"
	"strings"
)

// ErrorReporter is the part of *testing.T that AssertNoStaleAllowlist uses.
type ErrorReporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// AssertNoStaleAllowlist fails the test once for each allowlist entry that
// matched no finding in the walk that produced report. A consumer's gate test
// calls it after WalkReport, so an entry cannot outlive the guard it tolerated.
//
// t is a *testing.T in a consumer. The parameter is the two methods the helper
// calls, so this module's own test can prove that the helper fails.
func AssertNoStaleAllowlist(t ErrorReporter, report Report) {
	t.Helper()
	for _, key := range report.StaleAllowlist {
		t.Errorf("allowlist entry matches no finding, delete it: %q", key)
	}
}

// FormatAllowlistLog returns a stable string suitable for `t.Logf` so the
// allowlist stays visible in test output even when no findings are produced.
// Matches the format used by the existing gibson `no_graceful_nil_test.go`.
func FormatAllowlistLog(a Allowlist) string {
	if len(a) == 0 {
		return ""
	}
	coords := make([]string, 0, len(a))
	for c := range a {
		coords = append(coords, c)
	}
	sort.Strings(coords)
	var b strings.Builder
	for _, c := range coords {
		e := a[c]
		fmt.Fprintf(&b, "allowlisted: %s — [%s] %s\n", c, e.Category, e.Reason)
	}
	return b.String()
}
