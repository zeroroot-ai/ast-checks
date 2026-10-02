// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package unwired

import (
	"path/filepath"
	"testing"
)

const fixtureDir = "testdata/sample"

func analyze(t *testing.T, mutate func(*Opts)) Result {
	t.Helper()
	abs, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	opts := Opts{Dir: abs, RepoRoot: abs}
	if mutate != nil {
		mutate(&opts)
	}
	res, err := Analyze(opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if res.Packages == 0 || res.Files == 0 {
		t.Fatalf("Analyze loaded %d packages and %d files; a scan that loads nothing reports nothing unwired, which reads as success",
			res.Packages, res.Files)
	}
	return res
}

func byName(res Result) map[string]Decl {
	out := map[string]Decl{}
	for _, d := range res.Decls {
		out[d.Name] = d
	}
	return out
}

// TestAWrittenFieldThatIsNeverReadIsUnwired is the reason this package exists.
// ADR-0094: "A struct field is never a reachability root, so the gate cannot
// see a field that is written and never read. That is the single largest class
// in the sweep."
func TestAWrittenFieldThatIsNeverReadIsUnwired(t *testing.T) {
	got := byName(analyze(t, nil))

	d, ok := got["sample.Config.WrittenOnlyField"]
	if !ok {
		t.Fatal("the scanner did not report Config.WrittenOnlyField at all")
	}
	if d.Reads != 0 {
		t.Errorf("WrittenOnlyField reads = %d, want 0: it is assigned and never consulted", d.Reads)
	}
	if d.Writes == 0 {
		t.Error("WrittenOnlyField writes = 0, want at least 1: the assignment must be counted, or the report cannot tell a populated field from an untouched one")
	}
	if !d.Unwired() {
		t.Error("WrittenOnlyField is not reported as unwired")
	}
}

// TestAReadFieldIsWired: the other half. Without this the previous test passes
// for a scanner that counts nothing.
func TestAReadFieldIsWired(t *testing.T) {
	got := byName(analyze(t, nil))

	d, ok := got["sample.Config.ReadField"]
	if !ok {
		t.Fatal("the scanner did not report Config.ReadField")
	}
	if d.Reads == 0 {
		t.Error("ReadField reads = 0, but Consume returns it")
	}
	if d.Unwired() {
		t.Error("ReadField is reported as unwired")
	}
	// The composite-literal key `Config{ReadField: …}` is a write, not a read.
	if d.Writes == 0 {
		t.Error("the composite literal key was not counted as a write")
	}
}

// TestAnUntouchedFieldHasNeitherReadsNorWrites separates "populated but never
// consulted" from "not referenced at all". They need different fixes, so the
// report must distinguish them.
func TestAnUntouchedFieldHasNeitherReadsNorWrites(t *testing.T) {
	got := byName(analyze(t, nil))

	d, ok := got["sample.Config.NeverTouchedField"]
	if !ok {
		t.Fatal("the scanner did not report Config.NeverTouchedField")
	}
	if d.Reads != 0 || d.Writes != 0 {
		t.Errorf("NeverTouchedField reads=%d writes=%d, want 0 and 0", d.Reads, d.Writes)
	}
}

// TestCompoundAssignmentCountsAsARead: `x += 1` consults the old value.
// Counting it as a write only would make an accumulator look dead, and
// deleting an accumulator changes behaviour silently.
func TestCompoundAssignmentCountsAsARead(t *testing.T) {
	got := byName(analyze(t, nil))

	d, ok := got["sample.Config.AccumulatedField"]
	if !ok {
		t.Fatal("the scanner did not report Config.AccumulatedField")
	}
	if d.Reads == 0 {
		t.Error("AccumulatedField reads = 0, but `c.AccumulatedField += 1` consults the old value")
	}
}

// TestFuncsTypesConstsAndVars covers the kinds in one table, so a kind that
// stops being collected fails rather than silently reporting nothing.
func TestFuncsTypesConstsAndVars(t *testing.T) {
	got := byName(analyze(t, nil))

	cases := []struct {
		name    string
		kind    Kind
		unwired bool
	}{
		{"sample.UsedFunc", KindFunc, false},
		{"sample.UnusedFunc", KindFunc, true},
		{"sample.UsedType", KindType, false},
		{"sample.UnusedType", KindType, true},
		{"sample.UsedConst", KindConst, false},
		{"sample.UnusedConst", KindConst, true},
		{"sample.UsedVar", KindVar, false},
		{"sample.UnusedVar", KindVar, true},
		{"sample.UsedType.Method", KindMethod, false},
		{"sample.UsedType.UnusedMethod", KindMethod, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, ok := got[c.name]
			if !ok {
				t.Fatalf("the scanner did not report %s", c.name)
			}
			if d.Kind != c.kind {
				t.Errorf("kind = %q, want %q", d.Kind, c.kind)
			}
			if d.Unwired() != c.unwired {
				t.Errorf("unwired = %v (reads=%d), want %v", d.Unwired(), d.Reads, c.unwired)
			}
		})
	}
}

// TestATestOnlyConsumerIsNotAReadByDefault: a declaration whose only consumer
// is its own test is exactly what a tracker exists to surface, so the default
// must not count it.
func TestATestOnlyConsumerIsNotAReadByDefault(t *testing.T) {
	got := byName(analyze(t, nil))

	d, ok := got["sample.OnlyTestUsesThis"]
	if !ok {
		t.Fatal("the scanner did not report OnlyTestUsesThis")
	}
	if d.Reads != 0 {
		t.Errorf("reads = %d, want 0: its only caller is sample_test.go", d.Reads)
	}

	withTests := byName(analyze(t, func(o *Opts) { o.TestsAsReads = true }))
	if withTests["sample.OnlyTestUsesThis"].Reads == 0 {
		t.Error("TestsAsReads did not count the test's call, so the option does nothing")
	}
}

// TestMainAndInitAreNotReported: the runtime calls them, so reporting them as
// unwired is a false positive in every binary.
func TestMainAndInitAreNotReported(t *testing.T) {
	for _, d := range analyze(t, nil).Decls {
		if d.Kind == KindFunc && (d.Name == "sample.main" || d.Name == "sample.init") {
			t.Errorf("%s was reported; the runtime reads it, not the code", d.Name)
		}
	}
}

// TestNoDeclarationComesFromATestFile: test scaffolding is not surface.
func TestNoDeclarationComesFromATestFile(t *testing.T) {
	for _, d := range analyze(t, nil).Decls {
		if filepath.Ext(d.Coord) != "" && len(d.Coord) > 8 {
			if strings_HasSuffixBefore(d.Coord, "_test.go") {
				t.Errorf("%s is declared in a test file (%s)", d.Name, d.Coord)
			}
		}
	}
}

// TestUnexportedIsOptIn: a published module's unexported surface can be
// deleted without ceremony, so it is reported separately, not by default.
func TestUnexportedIsOptIn(t *testing.T) {
	for _, d := range analyze(t, nil).Decls {
		if !d.Exported {
			t.Errorf("%s is unexported and was reported without IncludeUnexported", d.Name)
		}
	}
	var sawUnexported bool
	for _, d := range analyze(t, func(o *Opts) { o.IncludeUnexported = true }).Decls {
		if !d.Exported {
			sawUnexported = true
		}
	}
	if !sawUnexported {
		t.Skip("the fixture declares nothing unexported, so this half cannot be measured")
	}
}

// TestKindsFilter: a caller burning down one class at a time asks for one kind.
func TestKindsFilter(t *testing.T) {
	res := analyze(t, func(o *Opts) { o.Kinds = []Kind{KindField} })
	if len(res.Decls) == 0 {
		t.Fatal("filtering to fields reported nothing")
	}
	for _, d := range res.Decls {
		if d.Kind != KindField {
			t.Errorf("%s has kind %q, want only fields", d.Name, d.Kind)
		}
	}
}

// TestContentKeyIsLineIndependent: the workspace rule is that a guard needing a
// re-pin after an unrelated edit is a defect in the guard.
func TestContentKeyIsLineIndependent(t *testing.T) {
	d := Decl{Kind: KindField, Name: "pkg.T.F", Coord: "a/b.go:42"}
	moved := Decl{Kind: KindField, Name: "pkg.T.F", Coord: "a/b.go:99"}
	if d.ContentKey() != moved.ContentKey() {
		t.Errorf("a line shift changed the key: %q vs %q", d.ContentKey(), moved.ContentKey())
	}
	if d.ContentKey() == (Decl{Kind: KindFunc, Name: "pkg.T.F"}).ContentKey() {
		t.Error("two different kinds share one key")
	}
}

// TestADirtyModuleIsRefused: a package that does not type-check would report
// zero reads for the wrong reason, and a silently partial scan is how a count
// becomes fiction.
func TestADirtyModuleIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module broken\n\ngo 1.27.1\n")
	write(t, dir, "broken.go", "package broken\n\nfunc F() { return undefinedThing }\n")

	_, err := Analyze(Opts{Dir: dir, RepoRoot: dir})
	if err == nil {
		t.Fatal("a module that does not type-check was analysed anyway")
	}
	if !contains(err.Error(), "does not type-check") {
		t.Errorf("the error does not say why: %v", err)
	}
}

// TestNoPatternsMatchedIsAnError: "no packages" must not read as "nothing
// unwired".
func TestNoPatternsMatchedIsAnError(t *testing.T) {
	abs, _ := filepath.Abs(fixtureDir)
	if _, err := Analyze(Opts{Dir: abs, Patterns: []string{"./nope/..."}}); err == nil {
		t.Fatal("a pattern that matched no package was accepted")
	}
}

func TestDirIsRequired(t *testing.T) {
	if _, err := Analyze(Opts{}); err == nil {
		t.Fatal("Analyze ran with no Dir")
	}
}

// TestAMethodReachedThroughAnInterfaceIsWired is the false-positive class that
// would have made the whole report untrustworthy. Measured on gibson-executor
// before this rule: 35 of 61 reported declarations were interface
// implementations, because the call site names the interface method and the
// concrete method's identifier appears only at its own declaration.
func TestAMethodReachedThroughAnInterfaceIsWired(t *testing.T) {
	got := byName(analyze(t, nil))

	d, ok := got["sample.Impl.Handle"]
	if !ok {
		t.Fatal("the scanner did not report Impl.Handle at all")
	}
	if d.Unwired() {
		t.Error("Impl.Handle is reported as unwired, but Dispatch calls it through Handler")
	}
	if d.ReadsViaInterface == 0 {
		t.Error("the read was not attributed to the interface, so the report cannot say how it is reached")
	}
}

// TestAnInterfaceMethodNobodyCallsDoesNotMakeItsImplementationsLive is the
// other half, and the one that keeps the rule honest. Crediting through any
// interface a type happens to satisfy would mark every implementation live and
// the rule would hide the findings it exists to keep.
func TestAnInterfaceMethodNobodyCallsDoesNotMakeItsImplementationsLive(t *testing.T) {
	got := byName(analyze(t, nil))

	d, ok := got["sample.Impl.Describe"]
	if !ok {
		t.Fatal("the scanner did not report Impl.Describe")
	}
	if !d.Unwired() {
		t.Errorf("Impl.Describe reads = %d (%d via interface), want 0: nothing calls Describe through Handler or by name",
			d.Reads, d.ReadsViaInterface)
	}

	// And the interface's own method is reported too, so the fix is visible as
	// one declaration rather than as N implementations.
	iface, ok := got["sample.Handler.Describe"]
	if ok && !iface.Unwired() {
		t.Errorf("Handler.Describe reads = %d, want 0", iface.Reads)
	}
}

// TestBaselineRoundTrips is the test that was missing. WriteBaseline appends a
// "\t# file:line" coordinate for a reader, and ReadBaseline kept it as part of
// the key, so a freshly written baseline matched none of its own entries: the
// very next run reported all fifteen as resolved AND as new. A gate that
// contradicts itself one second after being written is worse than no gate.
func TestBaselineRoundTrips(t *testing.T) {
	res := analyze(t, nil)
	found := res.Unwired()
	if len(found) == 0 {
		t.Fatal("the fixture has no unread declarations, so this test would measure nothing")
	}

	path := filepath.Join(t.TempDir(), "baseline.txt")
	if err := WriteBaseline(path, found, res); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	tolerated, err := ReadBaseline(path)
	if err != nil {
		t.Fatalf("ReadBaseline: %v", err)
	}

	if len(tolerated) != len(found) {
		t.Errorf("wrote %d entries and read back %d", len(found), len(tolerated))
	}
	for _, d := range found {
		if !tolerated[d.ContentKey()] {
			t.Errorf("entry %q did not survive the round trip", d.ContentKey())
		}
	}
}

// TestBaselineIgnoresCommentsAndBlanks: the reason for an entry goes on a #
// line above it, so those must not become keys.
func TestBaselineIgnoresCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.txt")
	write(t, dir, "b.txt", "# a reason\n\nfunc pkg.Thing\t# a/b.go:1\nfield pkg.T.F\n   \n# trailing note\n")

	got, err := ReadBaseline(path)
	if err != nil {
		t.Fatalf("ReadBaseline: %v", err)
	}
	want := map[string]bool{"func pkg.Thing": true, "field pkg.T.F": true}
	if len(got) != len(want) {
		t.Errorf("read %d entries, want %d: %v", len(got), len(want), got)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing %q", k)
		}
	}
}

// TestAMissingBaselineIsAnError: an empty baseline tolerates nothing and would
// fail the whole repo at once, which reads as a broken gate. A missing file is
// a wiring mistake and must say so.
func TestAMissingBaselineIsAnError(t *testing.T) {
	_, err := ReadBaseline(filepath.Join(t.TempDir(), "absent.txt"))
	if err == nil {
		t.Fatal("a missing baseline was read as an empty one")
	}
	if !contains(err.Error(), "-write") {
		t.Errorf("the error does not say how to create it: %v", err)
	}
}
