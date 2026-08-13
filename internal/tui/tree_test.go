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

func TestBuildTreeScopeGroupsByEnablementScope(t *testing.T) {
	rows := []row{
		mkRow("audit-logging", withGlobal, withRegistry("sec.git", "security")),
		mkRow("code-review", withProject, withRegistry("team.git", "team")),
		mkRow("my-personal", withLocal, withRegistry("team.git", "team")),
		mkRow("deploy-checklist", withAvailable, withRegistry("team.git", "team")),
	}

	items := buildTree(rows, tabScope, nil)
	paths := sectionPaths(items)

	wantOrder := []string{"scope:global", "scope:project", "scope:local", "scope:available"}
	for i, want := range wantOrder {
		found := false
		for _, p := range paths {
			if p == want {
				found = true
				if i > 0 {
					break
				}
			}
		}
		if !found {
			t.Fatalf("expected section %q in tree, got paths: %v", want, paths)
		}
	}

	if got := skillNamesUnder(items, "scope:global"); len(got) != 1 || got[0] != "audit-logging" {
		t.Fatalf("scope:global should contain audit-logging, got %v", got)
	}
	if got := skillNamesUnder(items, "scope:project"); len(got) != 1 || got[0] != "code-review" {
		t.Fatalf("scope:project should contain code-review, got %v", got)
	}
	if got := skillNamesUnder(items, "scope:local"); len(got) != 1 || got[0] != "my-personal" {
		t.Fatalf("scope:local should contain my-personal, got %v", got)
	}
	if got := skillNamesUnder(items, "scope:available"); len(got) != 1 || got[0] != "deploy-checklist" {
		t.Fatalf("scope:available should contain deploy-checklist, got %v", got)
	}
}

func TestBuildTreeScopePlacesMultiScopeUnderMostSpecific(t *testing.T) {
	// Artifact enabled in all three scopes → appears only under Local.
	r1 := mkRow("code-review", withGlobal, withProject, withLocal, withRegistry("t.git", "t"))
	// Artifact enabled global+project (no local) → appears only under Project.
	r2 := mkRow("audit-logging", withGlobal, withProject, withRegistry("s.git", "s"))

	items := buildTree([]row{r1, r2}, tabScope, nil)

	if got := skillNamesUnder(items, "scope:local"); len(got) != 1 || got[0] != "code-review" {
		t.Fatalf("scope:local should contain code-review once, got %v", got)
	}
	if got := skillNamesUnder(items, "scope:global"); len(got) != 0 {
		t.Fatalf("scope:global should be empty (code-review goes to local, audit-logging goes to project), got %v", got)
	}
	if got := skillNamesUnder(items, "scope:project"); len(got) != 1 || got[0] != "audit-logging" {
		t.Fatalf("scope:project should contain audit-logging only, got %v", got)
	}
}

func TestBuildTreeScopeLocalExcludedGoesToLocalOnly(t *testing.T) {
	// Excluded-locally skill: even if project-enabled, show only under Local.
	r := mkRow("deploy-checklist", withProject, withExcluded, withRegistry("t.git", "t"))

	items := buildTree([]row{r}, tabScope, nil)

	if got := skillNamesUnder(items, "scope:local"); len(got) != 1 || got[0] != "deploy-checklist" {
		t.Fatalf("scope:local should contain deploy-checklist (excluded), got %v", got)
	}
	if got := skillNamesUnder(items, "scope:project"); len(got) != 0 {
		t.Fatalf("scope:project should not duplicate excluded skill, got %v", got)
	}
}

func TestBuildTreeScopeHasRegistrySubsections(t *testing.T) {
	rows := []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("redact", withGlobal, withRegistry("sec.git", "security")),
		mkRow("ci-check", withGlobal, withRegistry("infra.git", "infra")),
	}

	items := buildTree(rows, tabScope, nil)

	if got := skillNamesUnder(items, "scope:global:skills:security"); len(got) != 2 {
		t.Fatalf("scope:global:skills:security should have 2 skills, got %v", got)
	}
	if got := skillNamesUnder(items, "scope:global:skills:infra"); len(got) != 1 {
		t.Fatalf("scope:global:skills:infra should have 1 skill, got %v", got)
	}
}

func TestBuildTreeScopeGroupsAvailableByResourceKindThenRegistry(t *testing.T) {
	rows := []row{
		mkRow("remember", withAvailable, withKind(ir.KindCommand), withRegistry("configs.git", "ai-configs")),
		mkRow("worker", withAvailable, withKind(ir.KindSubagent), withRegistry("configs.git", "ai-configs")),
		mkRow("memory", withAvailable, withKind(ir.KindMcpServer), withRegistry("configs.git", "ai-configs")),
	}

	items := buildTree(rows, tabScope, nil)

	for path, want := range map[string]string{
		"scope:available:commands:ai-configs":    "remember",
		"scope:available:subagents:ai-configs":   "worker",
		"scope:available:mcp-servers:ai-configs": "memory",
	} {
		got := skillNamesUnder(items, path)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s should contain %s, got %v", path, want, got)
		}
	}
}

func TestBuildTreeRegistryTabGroupsSkillsByRegistry(t *testing.T) {
	rows := []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("infra.git", "infra")),
		mkRow("loose", withAvailable, withRegistry("infra.git", "infra")),
	}

	items := buildTree(rows, tabRegistry, nil)

	if got := skillNamesUnder(items, "registry:skills:security"); len(got) != 1 || got[0] != "audit" {
		t.Fatalf("registry:skills:security should contain audit, got %v", got)
	}
	got := skillNamesUnder(items, "registry:skills:infra")
	if len(got) != 2 {
		t.Fatalf("registry:skills:infra should contain 2 skills, got %v", got)
	}
}

func TestBuildTreeRegistryTabSeparatesSameURLByPath(t *testing.T) {
	// Entries with the same URL but different paths should group separately.
	rows := []row{
		mkRow("pm-data", withGlobal, withRegistry("awesome-ai.git", "awesome-ai"), withRegistryMeta("main", "teams/pm/data")),
		mkRow("pm-exec", withGlobal, withRegistry("awesome-ai.git", "awesome-ai"), withRegistryMeta("main", "teams/pm/exec")),
		mkRow("audit", withGlobal, withRegistry("awesome-ai.git", "awesome-ai"), withRegistryMeta("main", "")),
	}

	items := buildTree(rows, tabRegistry, nil)

	// The three entries should be in different registry groups:
	// "awesome-ai / teams/pm/data", "awesome-ai / teams/pm/exec", and "awesome-ai".
	if got := skillNamesUnder(items, "registry:skills:awesome-ai / teams/pm/data"); len(got) != 1 || got[0] != "pm-data" {
		t.Fatalf("registry:skills:awesome-ai / teams/pm/data should contain pm-data, got %v", got)
	}
	if got := skillNamesUnder(items, "registry:skills:awesome-ai / teams/pm/exec"); len(got) != 1 || got[0] != "pm-exec" {
		t.Fatalf("registry:skills:awesome-ai / teams/pm/exec should contain pm-exec, got %v", got)
	}
	if got := skillNamesUnder(items, "registry:skills:awesome-ai"); len(got) != 1 || got[0] != "audit" {
		t.Fatalf("registry:skills:awesome-ai should contain audit, got %v", got)
	}
}

func TestBuildTreeRegistryTabGroupsByResourceKindThenRegistry(t *testing.T) {
	rows := []row{
		mkRow("audit", withGlobal, withKind(ir.KindSkill), withRegistry("resources.git", "resources")),
		mkRow("continue", withGlobal, withKind(ir.KindCommand), withRegistry("resources.git", "resources")),
		mkRow("worker", withGlobal, withKind(ir.KindSubagent), withRegistry("resources.git", "resources")),
		mkRow("pre-tool-use", withGlobal, withKind(ir.KindHook), withRegistry("resources.git", "resources")),
		mkRow("memory", withGlobal, withKind(ir.KindMcpServer), withRegistry("resources.git", "resources")),
	}

	items := buildTree(rows, tabRegistry, nil)

	for path, want := range map[string]string{
		"registry:skills:resources":      "audit",
		"registry:commands:resources":    "continue",
		"registry:subagents:resources":   "worker",
		"registry:hooks:resources":       "pre-tool-use",
		"registry:mcp-servers:resources": "memory",
	} {
		got := skillNamesUnder(items, path)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s should contain %s, got %v", path, want, got)
		}
	}
}

func TestBuildTreeStatusTabGroupsByStatus(t *testing.T) {
	rows := []row{
		mkRow("req-skill", withRequired, withProject),
		mkRow("enabled-skill", withProject),
		mkRow("pending-skill", withProject, withDrift(materialize.DriftMissing)),
		mkRow("broken-skill", withProject, withDrift(materialize.DriftBroken)),
		mkRow("available-skill", withAvailable),
	}

	items := buildTree(rows, tabStatus, nil)

	for path, wantName := range map[string]string{
		"status:required":  "req-skill",
		"status:enabled":   "enabled-skill",
		"status:pending":   "pending-skill",
		"status:broken":    "broken-skill",
		"status:available": "available-skill",
	} {
		got := skillNamesUnder(items, path)
		if len(got) != 1 || got[0] != wantName {
			t.Fatalf("%s should contain %s, got %v", path, wantName, got)
		}
	}
}

func TestBuildTreeFoldStateHidesChildren(t *testing.T) {
	rows := []row{
		mkRow("audit", withGlobal, withRegistry("sec.git", "security")),
		mkRow("deploy", withProject, withRegistry("team.git", "team")),
	}
	foldState := map[string]bool{"scope:global": true}

	items := buildTree(rows, tabScope, foldState)

	// When scope:global is collapsed, its sub-sections and skills must not appear.
	for _, it := range items {
		if it.Kind == flatSection && it.SectionPath == "scope:global:skills:security" {
			t.Fatalf("collapsed scope:global should not emit child section scope:global:skills:security")
		}
		if it.Kind == flatArtifact && it.Row.Artifact.Name == "audit" {
			t.Fatalf("collapsed scope:global should hide audit skill")
		}
	}

	// Other top-level sections remain visible.
	if got := skillNamesUnder(items, "scope:project"); len(got) != 1 || got[0] != "deploy" {
		t.Fatalf("scope:project must stay visible, got %v", got)
	}

	// The collapsed section header itself is still emitted, with Collapsed=true.
	var globalHeader *flatItem
	for i := range items {
		if items[i].Kind == flatSection && items[i].SectionPath == "scope:global" {
			globalHeader = &items[i]
			break
		}
	}
	if globalHeader == nil {
		t.Fatalf("collapsed scope:global header should still be present")
	}
	if !globalHeader.Collapsed {
		t.Fatalf("scope:global header should report Collapsed=true")
	}
}

func TestBuildTreeSectionCountsMatchChildren(t *testing.T) {
	rows := []row{
		mkRow("a", withGlobal, withRegistry("sec.git", "security")),
		mkRow("b", withGlobal, withRegistry("sec.git", "security")),
		mkRow("c", withGlobal, withRegistry("sec.git", "security")),
	}

	items := buildTree(rows, tabScope, nil)

	for _, it := range items {
		if it.Kind != flatSection {
			continue
		}
		switch it.SectionPath {
		case "scope:global":
			if it.SectionCount != 3 {
				t.Fatalf("scope:global count = %d, want 3", it.SectionCount)
			}
		case "scope:global:skills", "scope:global:skills:security":
			if it.SectionCount != 3 {
				t.Fatalf("%s count = %d, want 3", it.SectionPath, it.SectionCount)
			}
		}
	}
}

func TestBuildTreeEmptyInputYieldsNoItems(t *testing.T) {
	items := buildTree(nil, tabScope, nil)
	if len(items) != 0 {
		t.Fatalf("buildTree(nil) should return empty slice, got %d items", len(items))
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
	items := buildTree(rows, tabScope, nil)
	malformedPaths := sectionPaths(items)
	foundMalformedSection := false
	for _, p := range malformedPaths {
		if p == "scope:malformed" {
			foundMalformedSection = true
		}
	}
	if !foundMalformedSection {
		t.Fatalf("expected scope:malformed section in tree; got paths: %v", malformedPaths)
	}

	got := skillNamesUnder(items, "scope:malformed")
	if len(got) != 1 || got[0] != "broken-skill" {
		t.Fatalf("scope:malformed should contain 'broken-skill', got %v", got)
	}
}
