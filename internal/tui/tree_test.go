package tui

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// mkRow creates a row populated with the given skill name + scope flags for tests.
func mkRow(name string, opts ...func(*row)) row {
	r := row{
		Artifact: resolver.ResolvedArtifact{Entry: catalog.Entry{Name: name, Path: "/cache/" + name}},
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func withGlobal(r *row)    { r.EnabledGlobal = true }
func withProject(r *row)   { r.EnabledProject = true }
func withLocal(r *row)     { r.EnabledLocal = true }
func withAvailable(r *row) { r.IsAvailable = true }
func withExcluded(r *row)  { r.ExcludedLocally = true; r.IsExcluded = true }
func withRequired(r *row)  { r.Artifact.Required = true }
func withKind(kind ir.ResourceKind) func(*row) {
	return func(r *row) { r.Artifact.Kind = kind }
}
func withHarnesses(harnesses ...string) func(*row) {
	return func(r *row) {
		r.Harnesses = harnesses
		r.Artifact.Harnesses = make([]hcc.Target, 0, len(harnesses))
		for _, h := range harnesses {
			r.Artifact.Harnesses = append(r.Artifact.Harnesses, hcc.Target(h))
		}
	}
}
func withHarnessSource(source string) func(*row) {
	return func(r *row) { r.HarnessSource = source }
}
func withRegistry(url, name string) func(*row) {
	return func(r *row) { r.RegistryURL = url; r.RegistryName = name }
}
func withRegistryMeta(ref, path string) func(*row) {
	return func(r *row) { r.RegistryRef = ref; r.RegistryPath = path }
}
func withMalformed(msg string) func(*row) {
	return func(r *row) { r.IsMalformed = true; r.MalformedMsg = msg }
}
func withDrift(reason materialize.DriftReason) func(*row) {
	return func(r *row) {
		r.DriftItem = &materialize.DriftItem{Name: r.Artifact.Name, Reason: reason}
	}
}

func sectionPaths(items []flatItem) []string {
	var paths []string
	for _, it := range items {
		if it.Kind == flatSection {
			paths = append(paths, it.SectionPath)
		}
	}
	return paths
}

func skillNamesUnder(items []flatItem, sectionPath string) []string {
	var names []string
	inSection := false
	for _, it := range items {
		if it.Kind == flatSection {
			inSection = it.SectionPath == sectionPath ||
				(inSection && isDescendantPath(it.SectionPath, sectionPath))
			continue
		}
		if inSection && it.Kind == flatArtifact {
			names = append(names, it.Row.Artifact.Name)
		}
	}
	return names
}

func isDescendantPath(child, parent string) bool {
	return len(child) > len(parent) &&
		child[:len(parent)] == parent &&
		child[len(parent)] == ':'
}

// spec: tui-navigation/single-status-list-at-minimum-size
func TestBuildStatusTreeGroupsByStatus(t *testing.T) {
	rows := []row{
		mkRow("req-skill", withRequired, withProject),
		mkRow("enabled-skill", withProject),
		mkRow("pending-skill", withProject, withDrift(materialize.DriftMissing)),
		mkRow("broken-skill", withProject, withDrift(materialize.DriftBroken)),
		mkRow("available-skill", withAvailable),
		mkRow("bad-skill", withMalformed("yaml parse error")),
	}

	items := buildStatusTree(rows, nil)

	for path, wantName := range map[string]string{
		"status:required":  "req-skill",
		"status:enabled":   "enabled-skill",
		"status:pending":   "pending-skill",
		"status:broken":    "broken-skill",
		"status:available": "available-skill",
		"status:malformed": "bad-skill",
	} {
		got := skillNamesUnder(items, path)
		if len(got) != 1 || got[0] != wantName {
			t.Fatalf("%s should contain %s, got %v", path, wantName, got)
		}
	}
}

// spec: tui-navigation/single-status-list-at-minimum-size
func TestBuildStatusTreeKeepsFixedSectionOrder(t *testing.T) {
	// Inserted in reverse order; the tree must emit the fixed bucket order.
	rows := []row{
		mkRow("bad", withMalformed("parse")),
		mkRow("avail", withAvailable),
		mkRow("req", withRequired),
		mkRow("on", withGlobal),
	}
	items := buildStatusTree(rows, nil)
	want := []string{"status:required", "status:enabled", "status:available", "status:malformed"}
	got := sectionPaths(items)
	if len(got) != len(want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sections = %v, want %v", got, want)
		}
	}
}

func TestBuildStatusTreeFoldStateHidesChildren(t *testing.T) {
	rows := []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withAvailable, withRegistry("team.git", "team")),
	}
	foldState := map[string]bool{"status:enabled": true}

	items := buildStatusTree(rows, foldState)

	for _, it := range items {
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "audit" {
			t.Fatalf("collapsed status:enabled should hide audit skill")
		}
	}

	// Other top-level sections remain visible.
	if got := skillNamesUnder(items, "status:available"); len(got) != 1 || got[0] != "deploy" {
		t.Fatalf("status:available must stay visible, got %v", got)
	}

	// The collapsed section header itself is still emitted, with Collapsed=true.
	var header *flatItem
	for i := range items {
		if items[i].Kind == flatSection && items[i].SectionPath == "status:enabled" {
			header = &items[i]
			break
		}
	}
	if header == nil {
		t.Fatalf("collapsed status:enabled header should still be present")
	}
	if !header.Collapsed {
		t.Fatalf("status:enabled header should report Collapsed=true")
	}
}

func TestBuildStatusTreeSectionCountsMatchChildren(t *testing.T) {
	rows := []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withGlobal, withRegistry("sec.git", "security")),
		mkRow("c", withGlobal, withRegistry("sec.git", "security")),
	}

	items := buildStatusTree(rows, nil)

	for _, it := range items {
		if it.Kind != flatSection {
			continue
		}
		if it.SectionPath == "status:enabled" && it.SectionCount != 3 {
			t.Fatalf("status:enabled count = %d, want 3", it.SectionCount)
		}
	}
}

func TestBuildStatusTreeEmptyInputYieldsNoItems(t *testing.T) {
	items := buildStatusTree(nil, nil)
	if len(items) != 0 {
		t.Fatalf("buildStatusTree(nil) should return empty slice, got %d items", len(items))
	}
}

func TestBuildRowsIncludesMalformedFromCatalog(t *testing.T) {
	// Regression test: malformed skills from catalog must
	// appear in buildRows output so the TUI renders them.
	catalogs := map[string]catalog.LoadedCatalog{
		"test-reg": {
			Entries: []catalog.Entry{
				{Name: "good-skill", Kind: ir.KindSkill, Path: "skills/good-skill"},
			},
			Malformed: []skill.MalformedSkill{
				{DirName: "broken-skill", Path: "skills/broken-skill", Error: fmt.Errorf("ingest.factory.load-skill-failed: yaml parse error")},
			},
			Resources: map[string]ir.Resource{
				"skill\x00good-skill": ir.NewSkill("good-skill", "desc", "body", nil, nil, ir.SourceRef{File: "skills/good-skill/SKILL.md", Line: 1}),
			},
		},
	}

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: "/tmp/test-reg", Default: true, CatalogKey: "test-reg"},
		},
	}

	rows := buildRows(cfg, catalogs, nil, nil, materialize.DriftReport{})

	malformedNames := map[string]bool{}
	for _, r := range rows {
		if r.IsMalformed {
			malformedNames[r.Artifact.Name] = true
		}
	}
	if !malformedNames["broken-skill"] {
		t.Fatalf("expected malformed entry 'broken-skill' in rows; got malformed names: %v", malformedNames)
	}

	// Also verify the good skill appears as available (not resolved).
	foundGood := false
	for _, r := range rows {
		if r.Artifact.Name == "good-skill" && !r.IsMalformed {
			foundGood = true
		}
	}
	if !foundGood {
		t.Fatalf("expected good-skill entry in rows")
	}

	// Verify the malformed entry appears in the rendered tree.
	items := buildStatusTree(rows, nil)
	malformedPaths := sectionPaths(items)
	foundMalformedSection := false
	for _, p := range malformedPaths {
		if p == "status:malformed" {
			foundMalformedSection = true
		}
	}
	if !foundMalformedSection {
		t.Fatalf("expected status:malformed section in tree; got paths: %v", malformedPaths)
	}

	got := skillNamesUnder(items, "status:malformed")
	if len(got) != 1 || got[0] != "broken-skill" {
		t.Fatalf("status:malformed should contain 'broken-skill', got %v", got)
	}
}
