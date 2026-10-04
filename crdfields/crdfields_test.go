// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Zero Root AI

package crdfields

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeroroot-ai/ast-checks/unwired"
)

// The fixture is a test of the DECISION, which is the half that holds the
// rules. The analyzer is never run: each case supplies the field declarations
// the analyzer would report, and a crafted schema.
//
// Every rule gets a case that is red for the stated reason, not merely red:
// each red case asserts a substring of the report, so a rule that starts
// failing for a different reason is a test failure.

const fixtureTypes = "package v1alpha1\n" + `
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Shown",type=string,JSONPath=` + "`.status.shown`" + `
// +kubebuilder:printcolumn:name="Store",type=string,JSONPath=` + "`.status.stores[*].state`" + `
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=` + "`.metadata.creationTimestamp`" + `

// Widget is the fixture root object.
type Widget struct {
	Spec   WidgetSpec   ` + "`json:\"spec,omitempty\"`" + `
	Status WidgetStatus ` + "`json:\"status,omitempty\"`" + `
}

type WidgetSpec struct {
	Size     string ` + "`json:\"size,omitempty\"`" + `
	Ignored  string ` + "`json:\"ignored,omitempty\"`" + `
	Untagged string
}

type WidgetStatus struct {
	Shown  string        ` + "`json:\"shown,omitempty\"`" + `
	Stores []WidgetStore ` + "`json:\"stores,omitempty\"`" + `
}

type WidgetStore struct {
	State string ` + "`json:\"state,omitempty\"`" + `
}

// +kubebuilder:object:root=true

// WidgetList carries no print columns, and must not be picked as the root.
type WidgetList struct {
	Items []Widget ` + "`json:\"items\"`" + `
}
`

func fixture(t *testing.T, exempt string) Config {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "types", "fixture_types.go"), fixtureTypes)
	mustWrite(t, filepath.Join(dir, "exempt.txt"), exempt)
	return Config{Dir: dir, TypesDirs: []string{"types"}, ExemptFile: "exempt.txt", MinServed: 1}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// field is one analyzer line for a field of the fixture schema.
func field(name string, reads, writes int) unwired.Decl {
	return unwired.Decl{
		Kind: unwired.KindField, Name: "v1alpha1." + name,
		Coord: "types/fixture_types.go:10", Reads: reads, Writes: writes,
	}
}

func judge(t *testing.T, cfg Config, decls ...unwired.Decl) (Outcome, string) {
	t.Helper()
	out, err := Judge(cfg, decls)
	if err != nil {
		return out, err.Error()
	}
	return out, out.Report()
}

func TestJudge(t *testing.T) {
	cases := []struct {
		name   string
		exempt string
		decls  []unwired.Decl
		// want is a substring of the report. Empty means the gate passes.
		want string
	}{
		// The case this gate exists for.
		{name: "an unread spec field is blocked",
			decls: []unwired.Decl{field("WidgetSpec.Ignored", 0, 0)},
			want:  "v1alpha1.WidgetSpec.Ignored"},
		// A write is not a read.
		{name: "a field written many times and never read is blocked",
			decls: []unwired.Decl{field("WidgetSpec.Ignored", 0, 9)},
			want:  "reads=0 writes=9"},
		{name: "a field with a Go read passes",
			decls: []unwired.Decl{field("WidgetSpec.Ignored", 3, 0)}},
		// The field exists to be displayed and no Go code will ever read it.
		{name: "an unread field named by a print column passes",
			decls: []unwired.Decl{field("WidgetStatus.Shown", 0, 4)}},
		// A bracketed segment names the field before the bracket, and every
		// hop of the path is displayed. The setec copy of the script credited
		// neither.
		{name: "the leaf of a wildcard print column passes",
			decls: []unwired.Decl{field("WidgetStore.State", 0, 2)}},
		{name: "the container of a wildcard print column passes",
			decls: []unwired.Decl{field("WidgetStatus.Stores", 0, 2)}},
		{name: "the first hop of a print column passes",
			decls: []unwired.Decl{field("Widget.Status", 0, 0)}},
		// A field with no json tag is not in the served schema at all, and
		// says so instead of reading as an ordinary review item.
		{name: "an unread field with no json tag is a failure",
			decls: []unwired.Decl{field("WidgetSpec.Untagged", 0, 0)},
			want:  "no json tag"},
		// Generated code is never a candidate: the gate judges only
		// *_types.go. A real line travels with it, or the floor would fire and
		// the case would pass for the wrong reason.
		{name: "an unread field reported from generated code is not judged",
			decls: []unwired.Decl{
				{Kind: unwired.KindField, Name: "v1alpha1.WidgetSpec.Ignored", Coord: "types/zz_generated.deepcopy.go:88"},
				field("WidgetSpec.Size", 2, 0),
			}},
		// A List kind is apimachinery plumbing, not a served promise. A real
		// line travels with it for the same reason as above.
		{name: "an unread field of a List kind is not judged",
			decls: []unwired.Decl{field("WidgetList.Items", 0, 0), field("WidgetSpec.Size", 2, 0)}},
		{name: "a verdict with a reference and a reason passes",
			exempt: "v1alpha1.WidgetSpec.Ignored | #121 | build the consumer, tracked",
			decls:  []unwired.Decl{field("WidgetSpec.Ignored", 0, 0)}},
		{name: "a comment line in the verdict file is not an entry",
			exempt: "# v1alpha1.WidgetSpec.Ignored | #121 | commented out",
			decls:  []unwired.Decl{field("WidgetSpec.Ignored", 0, 0)},
			want:   "BLOCKED: 1 served CRD field"},
		// A verdict with no issue or ADR reference is an opinion.
		{name: "a verdict with no reference is refused",
			exempt: "v1alpha1.WidgetSpec.Ignored | later | build the consumer",
			decls:  []unwired.Decl{field("WidgetSpec.Ignored", 0, 0)},
			want:   "has no issue or ADR reference"},
		// A malformed entry must not silently exempt anything.
		{name: "a verdict with two columns is refused",
			exempt: "v1alpha1.WidgetSpec.Ignored | #121",
			decls:  []unwired.Decl{field("WidgetSpec.Ignored", 0, 0)},
			want:   `expected "<pkg>.<Type>.<Field>`},
		// An exemption outliving its field records a decision about nothing.
		{name: "a verdict naming a field that no longer exists is refused",
			exempt: "v1alpha1.WidgetSpec.Removed | #121 | kept after the field was deleted",
			decls:  []unwired.Decl{field("WidgetSpec.Ignored", 3, 0)},
			want:   "no longer exist"},
		// It would hide the consumer being removed again later.
		{name: "a verdict for a field that now has a consumer is refused",
			exempt: "v1alpha1.WidgetSpec.Ignored | #121 | kept after the consumer was built",
			decls:  []unwired.Decl{field("WidgetSpec.Ignored", 4, 0)},
			want:   "now HAVE a consumer"},
	}

	ran := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran++
			out, report := judge(t, fixture(t, tc.exempt), tc.decls...)
			if tc.want == "" {
				if !out.OK() || !strings.HasPrefix(report, "ok  ") {
					t.Fatalf("was blocked:\n%s", report)
				}
				return
			}
			if out.OK() && strings.HasPrefix(report, "ok  ") {
				t.Fatalf("was allowed:\n%s", report)
			}
			if !strings.Contains(report, tc.want) {
				t.Fatalf("failed for the wrong reason; want %q in:\n%s", tc.want, report)
			}
		})
	}
	// A count floor. A table that stops running its cases reads as a pass.
	if ran != len(cases) || ran < 16 {
		t.Fatalf("%d of %d cases ran, want at least 16", ran, len(cases))
	}
}

// An analyzer output that reaches nothing must not read as clean. Every guard
// that could not fail looked exactly like this.
func TestJudge_TheFloor(t *testing.T) {
	cfg := fixture(t, "")
	cfg.MinServed = 5
	out, report := judge(t, cfg, field("WidgetSpec.Size", 1, 0))
	if out.OK() {
		t.Fatalf("a run that resolved 1 field below a floor of 5 reported ok:\n%s", report)
	}
	if !strings.Contains(report, "below the floor of 5") {
		t.Fatalf("the floor failed for the wrong reason:\n%s", report)
	}
}

// The List type must not be chosen as the root. If it were, no JSONPath would
// resolve and the print-column rule would stop crediting anything in silence.
// .metadata.creationTimestamp leaves the package and resolves to nothing.
func TestJudge_ResolvedColumnsAreCounted(t *testing.T) {
	out, report := judge(t, fixture(t, ""), field("WidgetStatus.Shown", 0, 0))
	if out.ResolvedColumns != 2 {
		t.Fatalf("resolved %d print column(s), want 2 of 3:\n%s", out.ResolvedColumns, report)
	}
	if !strings.Contains(report, "2 print column(s) resolved") {
		t.Fatalf("the ok line does not report the resolved print columns:\n%s", report)
	}
}

// A schema with print columns and no root object cannot resolve them, and must
// say so instead of crediting nothing in silence.
func TestJudge_PrintColumnsNeedARootObject(t *testing.T) {
	cfg := fixture(t, "")
	rootless := strings.ReplaceAll(fixtureTypes, "// +kubebuilder:object:root=true\n", "")
	mustWrite(t, filepath.Join(cfg.Dir, "types", "fixture_types.go"), rootless)
	_, report := judge(t, cfg, field("WidgetSpec.Size", 1, 0))
	if !strings.Contains(report, "no +kubebuilder:object:root type") {
		t.Fatalf("wrong reason for a rootless schema:\n%s", report)
	}
}

// Several api packages share the package name v1alpha1, so a struct name
// repeated across two directories would let one exemption silence two fields.
func TestJudge_AStructDeclaredInTwoTypesDirsIsRefused(t *testing.T) {
	cfg := fixture(t, "")
	mustWrite(t, filepath.Join(cfg.Dir, "other", "clash_types.go"),
		"package v1alpha1\n\ntype WidgetSpec struct {\n\tOther string `json:\"other\"`\n}\n")
	cfg.TypesDirs = []string{"types", "other"}
	_, report := judge(t, cfg, field("WidgetSpec.Size", 1, 0))
	if !strings.Contains(report, "struct WidgetSpec is declared in both") {
		t.Fatalf("wrong reason for a repeated struct:\n%s", report)
	}
}

// A field in a second types directory is judged like one in the first.
func TestJudge_EveryTypesDirIsJudged(t *testing.T) {
	cfg := fixture(t, "")
	mustWrite(t, filepath.Join(cfg.Dir, "other", "gadget_types.go"),
		"package v1alpha1\n\ntype GadgetSpec struct {\n\tKnob string `json:\"knob\"`\n}\n")
	cfg.TypesDirs = []string{"types", "other"}
	out, report := judge(t, cfg, unwired.Decl{
		Kind: unwired.KindField, Name: "v1alpha1.GadgetSpec.Knob", Coord: "other/gadget_types.go:4",
	})
	if out.OK() || !strings.Contains(report, "v1alpha1.GadgetSpec.Knob") {
		t.Fatalf("an unread field in the second types dir was not blocked:\n%s", report)
	}
}

// The analyzer reports a repo-relative coordinate. A caller that passes an
// absolute one must reach the same fields.
func TestJudge_AnAbsoluteCoordinateReachesTheSchema(t *testing.T) {
	cfg := fixture(t, "")
	d := field("WidgetSpec.Ignored", 0, 0)
	d.Coord = filepath.Join(cfg.Dir, "types", "fixture_types.go") + ":10"
	out, report := judge(t, cfg, d)
	if out.OK() || out.Served != 1 {
		t.Fatalf("served %d field(s), want 1 blocked:\n%s", out.Served, report)
	}
}
