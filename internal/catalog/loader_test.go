package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// TestDiscover_VacantLotWithSubBundles verifies that when a directory has no
// standard roots but contains subdirectories that do, Discover recursively
// loads all sub-bundles and returns merged entries.
func TestDiscover_VacantLotWithSubBundles(t *testing.T) {
	dir := t.TempDir()

	// Parent directory has no skills/commands/etc. — just subdirectories.
	pluginA := filepath.Join(dir, "alpha-plugin")
	if err := os.MkdirAll(filepath.Join(pluginA, "skills", "skill-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginA, "skills", "skill-a", "SKILL.md"), `---
name: skill-a
description: Alpha skill
metadata:
  tags: []
---

# Skill A
`)

	pluginB := filepath.Join(dir, "beta-plugin")
	if err := os.MkdirAll(filepath.Join(pluginB, "skills", "skill-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginB, "skills", "skill-b", "SKILL.md"), `---
name: skill-b
description: Beta skill
metadata:
  tags: []
---

# Skill B
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %#v", len(result.Entries), result.Entries)
	}

	names := make(map[string]bool)
	for _, e := range result.Entries {
		names[e.Name] = true
	}
	if !names["skill-a"] {
		t.Error("expected skill-a to be discovered")
	}
	if !names["skill-b"] {
		t.Error("expected skill-b to be discovered")
	}
}

// TestDiscover_NormalPathNoRecursion verifies that when a directory already
// has standard roots, Discover does NOT recurse into subdirectories.
func TestDiscover_NormalPathNoRecursion(t *testing.T) {
	dir := t.TempDir()

	// Root has skills/ — this is a normal bundle, no recursion needed.
	if err := os.MkdirAll(filepath.Join(dir, "skills", "top-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "skills", "top-skill", "SKILL.md"), `---
name: top-skill
description: Top-level skill
metadata:
  tags: []
---

# Top Skill
`)

	// A subdirectory that also has skills/ should NOT be separately loaded.
	if err := os.MkdirAll(filepath.Join(dir, "sub", "skills", "sub-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "sub", "skills", "sub-skill", "SKILL.md"), `---
name: sub-skill
description: Should not be found via recursion
metadata:
  tags: []
---

# Sub Skill
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry (no recursion), got %d: %#v", len(result.Entries), result.Entries)
	}
	if result.Entries[0].Name != "top-skill" {
		t.Errorf("expected top-skill, got %q", result.Entries[0].Name)
	}
}

// TestDiscover_NestedSubBundles verifies that sub-bundles at multiple depths
// are all discovered when the parent has no roots.
func TestDiscover_NestedSubBundles(t *testing.T) {
	// Structure:
	// plugins/                      ← no roots (vacant lot)
	// ├── pm/                       ← no roots
	// │   ├── data-analytics/       ← has skills/ → ingest as bundle
	// │   └── execution/            ← has skills/ → ingest as bundle
	// └── general/                  ← has skills/ → ingest as bundle
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")

	daDir := filepath.Join(pluginsDir, "pm", "data-analytics")
	if err := os.MkdirAll(filepath.Join(daDir, "skills", "analytics-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(daDir, "skills", "analytics-skill", "SKILL.md"), `---
name: analytics-skill
description: Data analytics skill
metadata:
  tags: []
---

# Analytics
`)

	execDir := filepath.Join(pluginsDir, "pm", "execution")
	if err := os.MkdirAll(filepath.Join(execDir, "skills", "execution-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(execDir, "skills", "execution-skill", "SKILL.md"), `---
name: execution-skill
description: Execution skill
metadata:
  tags: []
---

# Execution
`)

	genDir := filepath.Join(pluginsDir, "general")
	if err := os.MkdirAll(filepath.Join(genDir, "skills", "general-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(genDir, "skills", "general-skill", "SKILL.md"), `---
name: general-skill
description: General skill
metadata:
  tags: []
---

# General
`)

	result, err := Discover(pluginsDir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", pluginsDir, err)
	}

	if len(result.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %#v", len(result.Entries), result.Entries)
	}

	names := make(map[string]bool)
	for _, e := range result.Entries {
		names[e.Name] = true
	}
	for _, want := range []string{"analytics-skill", "execution-skill", "general-skill"} {
		if !names[want] {
			t.Errorf("expected %q to be discovered", want)
		}
	}
}

// TestDiscover_NameCollisionFirstWins verifies that when two sub-bundles
// contain resources with the same name, the first one (sorted by directory
// name) wins.
func TestDiscover_NameCollisionFirstWins(t *testing.T) {
	dir := t.TempDir()

	// Two sub-bundles with the same skill name.
	aaaDir := filepath.Join(dir, "aaa-plugin")
	if err := os.MkdirAll(filepath.Join(aaaDir, "skills", "shared-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(aaaDir, "skills", "shared-skill", "SKILL.md"), `---
name: shared-skill
description: From AAA
metadata:
  tags: []
---

# AAA
`)

	zzzDir := filepath.Join(dir, "zzz-plugin")
	if err := os.MkdirAll(filepath.Join(zzzDir, "skills", "shared-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(zzzDir, "skills", "shared-skill", "SKILL.md"), `---
name: shared-skill
description: From ZZZ
metadata:
  tags: []
---

# ZZZ
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry (collision dedup), got %d: %#v", len(result.Entries), result.Entries)
	}
	if result.Entries[0].Description != "From AAA" {
		t.Errorf("expected first-wins (AAA), got description %q", result.Entries[0].Description)
	}
}

// TestDiscover_VacantLotGenuinelyEmpty verifies that a genuinely empty
// directory returns an error (no recognizable layout).
func TestDiscover_VacantLotGenuinelyEmpty(t *testing.T) {
	dir := t.TempDir()

	// A genuinely empty directory with no subdirectories at all.
	_, err := Discover(dir, "claude")
	if err == nil {
		t.Fatal("expected error for genuinely empty directory with no recognizable layout")
	}
}

// TestDiscover_VacantLotWithNonBundleSubdirs verifies that a directory with
// subdirectories that have no roots returns an error (no recognizable layout
// and no sub-bundles to recurse into).
func TestDiscover_VacantLotWithNonBundleSubdirs(t *testing.T) {
	dir := t.TempDir()

	// Subdirectory with no standard roots — just a README.
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docs", "README.md"), "# Documentation")

	_, err := Discover(dir, "claude")
	if err == nil {
		t.Fatal("expected error for directory with no recognizable layout and no sub-bundles")
	}
}

// TestDiscover_SubBundleWithCommandsAndSkills verifies that a sub-bundle
// containing both skills and commands has all resource types discovered.
func TestDiscover_SubBundleWithCommandsAndSkills(t *testing.T) {
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "pm-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "pm-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, "skills", "pm-skill", "SKILL.md"), `---
name: pm-skill
description: PM skill
metadata:
  tags: []
---

# PM Skill
`)
	if err := os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, "commands", "pm-command.md"), `---
name: pm-command
description: PM command
---

# PM Command
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	kinds := make(map[ir.ResourceKind]bool)
	for _, e := range result.Entries {
		kinds[e.Kind] = true
	}
	if !kinds[ir.KindSkill] {
		t.Error("expected skill resource to be discovered")
	}
	if !kinds[ir.KindCommand] {
		t.Error("expected command resource to be discovered")
	}

	// Verify command content fields are populated, not just Kind presence.
	var cmdEntry *Entry
	for i := range result.Entries {
		if result.Entries[i].Kind == ir.KindCommand && result.Entries[i].Name == "pm-command" {
			cmdEntry = &result.Entries[i]
			break
		}
	}
	if cmdEntry == nil {
		t.Fatal("expected pm-command entry to be found")
	}
	if cmdEntry.Description != "PM command" {
		t.Errorf("command Description = %q, want %q", cmdEntry.Description, "PM command")
	}
	if cmdEntry.Resource == nil {
		t.Error("command Resource is nil; expected non-nil ir.Resource")
	}
}

// TestDiscover_SubBundleResourcesPopulated verifies that Resource fields are
// populated for entries discovered via sub-bundle recursion.
func TestDiscover_SubBundleResourcesPopulated(t *testing.T) {
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "my-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "my-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, "skills", "my-skill", "SKILL.md"), `---
name: my-skill
description: My skill
metadata:
  tags: []
---

# My Skill
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	if len(result.Resources) == 0 {
		t.Fatal("expected Resources to be populated")
	}
	key := skill.ResourceKey(ir.KindSkill, "my-skill")
	res, ok := result.Resources[key]
	if !ok {
		t.Fatalf("expected resource key %q in Resources", key)
	}
	if res == nil {
		t.Fatal("expected non-nil ir.Resource")
	}
}

// TestScanForSubBundles verifies the scanForSubBundles helper directly.
func TestScanForSubBundles(t *testing.T) {
	dir := t.TempDir()

	// Create structure:
	// dir/
	// ├── alpha/          ← has skills/
	// ├── docs/           ← no roots
	// │   └── nested/     ← no roots
	// ├── beta/           ← has skills/
	// └── gamma/          ← no roots
	//     └── delta/      ← has skills/ (nested sub-bundle)

	if err := os.MkdirAll(filepath.Join(dir, "alpha", "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "alpha", "skills", "x", "SKILL.md"), "---\nname: x\n---\nX\n")

	if err := os.MkdirAll(filepath.Join(dir, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "beta", "skills", "y"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "beta", "skills", "y", "SKILL.md"), "---\nname: y\n---\nY\n")

	if err := os.MkdirAll(filepath.Join(dir, "gamma", "delta", "skills", "z"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "gamma", "delta", "skills", "z", "SKILL.md"), "---\nname: z\n---\nZ\n")

	bundles := scanForSubBundles(dir)

	if len(bundles) != 3 {
		t.Fatalf("expected 3 sub-bundles, got %d: %v", len(bundles), bundles)
	}

	expected := []string{
		filepath.Join(dir, "alpha"),
		filepath.Join(dir, "beta"),
		filepath.Join(dir, "gamma", "delta"),
	}
	for i, want := range expected {
		if bundles[i] != want {
			t.Errorf("bundles[%d] = %q, want %q", i, bundles[i], want)
		}
	}
}

// TestScanForSubBundles_StopsAtBundleRoot verifies that scanForSubBundles
// does not descend into a directory that already is a bundle root.
func TestScanForSubBundles_StopsAtBundleRoot(t *testing.T) {
	dir := t.TempDir()

	// alpha/ has skills/ and a nested sub-skill/ — we should NOT descend
	// into alpha/ to find sub-skill, because alpha itself is a bundle.
	if err := os.MkdirAll(filepath.Join(dir, "alpha", "skills", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "alpha", "skills", "main", "SKILL.md"), "---\nname: main\n---\nM\n")

	if err := os.MkdirAll(filepath.Join(dir, "alpha", "sub-skill", "skills", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "alpha", "sub-skill", "skills", "inner", "SKILL.md"), "---\nname: inner\n---\nI\n")

	bundles := scanForSubBundles(dir)

	if len(bundles) != 1 {
		t.Fatalf("expected 1 sub-bundle (alpha only, not descending further), got %d: %v", len(bundles), bundles)
	}
	if bundles[0] != filepath.Join(dir, "alpha") {
		t.Errorf("bundle = %q, want %q", bundles[0], filepath.Join(dir, "alpha"))
	}
}

// TestHasWarningOrHigher verifies the diagnostic severity check.
func TestHasWarningOrHigher(t *testing.T) {
	tests := []struct {
		name        string
		diagnostics []diag.Diagnostic
		want        bool
	}{
		{
			name:        "empty diagnostics",
			diagnostics: nil,
			want:        false,
		},
		{
			name: "info only",
			diagnostics: []diag.Diagnostic{
				{Code: "test.info", Severity: diag.SeverityInfo},
			},
			want: false,
		},
		{
			name: "warning present",
			diagnostics: []diag.Diagnostic{
				{Code: "test.info", Severity: diag.SeverityInfo},
				{Code: "test.warn", Severity: diag.SeverityWarning},
			},
			want: true,
		},
		{
			name: "error present",
			diagnostics: []diag.Diagnostic{
				{Code: "test.err", Severity: diag.SeverityError},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasWarningOrHigher(tt.diagnostics)
			if got != tt.want {
				t.Errorf("hasWarningOrHigher() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDiscover_DoesNotRecurseWhenWarningDiagnostics verifies that the vacant-lot
// recursion does NOT happen when there are warning-level diagnostics, even if
// there are zero entries. This prevents recursing into directories that HCC
// already partially processed.
func TestDiscover_DoesNotRecurseWhenWarningDiagnostics(t *testing.T) {
	// We test this indirectly: if Load returns 0 entries but has warning
	// diagnostics, the recursion should not trigger. We can't easily force
	// Load to produce warnings without errors, so we test hasWarningOrHigher
	// directly and trust the integration.
	t.Run("hasWarningOrHigher_guards_recursion", func(t *testing.T) {
		diagnostics := []diag.Diagnostic{
			{Code: "some.warning", Severity: diag.SeverityWarning},
		}
		if !hasWarningOrHigher(diagnostics) {
			t.Error("expected hasWarningOrHigher to return true for warning diagnostics")
		}
		// If there are warnings, the vacant-lot condition is NOT met,
		// so no recursion happens.
	})
}

// TestDiscover_MixedRootEmitsSubBundlesIgnoredWarning verifies that when a
// directory has root-level resources (making it a bundle) AND subdirectories
// that are also bundles, a warning is emitted telling the user to restructure
// so the parent is a pure container. Sub-bundles are NOT discovered in this
// case — only root-level resources are returned.
func TestDiscover_MixedRootEmitsSubBundlesIgnoredWarning(t *testing.T) {
	dir := t.TempDir()

	// Root-level commands/ makes Autodetect detect this as a bundle.
	if err := os.MkdirAll(filepath.Join(dir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "commands", "root-cmd.md"), `---
name: root-cmd
description: Root command
---

# Root Command
`)

	// Sub-directory that is also a bundle — should NOT be discovered,
	// but the user should be warned.
	pluginA := filepath.Join(dir, "alpha-plugin")
	if err := os.MkdirAll(filepath.Join(pluginA, "skills", "skill-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginA, "skills", "skill-a", "SKILL.md"), `---
name: skill-a
description: Alpha skill
metadata:
  tags: []
---

# Skill A
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	// Only root-level resources should be returned.
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry (root-cmd only, no sub-bundle), got %d: %#v", len(result.Entries), result.Entries)
	}
	if result.Entries[0].Name != "root-cmd" {
		t.Errorf("expected root-cmd, got %q", result.Entries[0].Name)
	}

	// A warning must be emitted about ignored sub-bundles.
	if len(result.Malformed) == 0 {
		t.Fatal("expected at least one MalformedSkill warning about ignored sub-bundles, got none")
	}
	found := false
	for _, m := range result.Malformed {
		if m.DirName == "catalog.discover.sub-bundles-ignored" {
			found = true
			if !strings.Contains(m.Error.Error(), "sub-bundle") {
				t.Errorf("warning message should mention sub-bundles, got: %v", m.Error)
			}
			if !strings.Contains(m.Error.Error(), "common/") {
				t.Errorf("warning message should suggest common/ sub-bundle, got: %v", m.Error)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected MalformedSkill with DirName %q, got: %#v", "catalog.discover.sub-bundles-ignored", result.Malformed)
	}
}

// TestDiscover_SubBundlesIgnoredSuppressedWhenCatalogCoversThem verifies that
// the sub-bundles-ignored warning is NOT emitted when the loaded catalog
// already contains entries from the sub-bundle directories. This happens
// when a recognized harness workspace (e.g. .factory/skills/) has
// organizational groupings (e.g. go/, python/) that contain skill
// subdirectories — HCC discovers all skills within the workspace, so the
// "sub-bundles" don't represent missed resources.
func TestDiscover_SubBundlesIgnoredSuppressedWhenCatalogCoversThem(t *testing.T) {
	dir := t.TempDir()

	// Create a bare skills directory container with skills organized in
	// sub-directories (like awesome-ai's .factory/skills/ layout).
	// The "go" and "python" directories contain multiple skills each,
	// which makes hasMultiSkillSubdirs detect them as sub-bundles.
	if err := os.MkdirAll(filepath.Join(dir, "go", "go-conventions"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go", "go-conventions", "SKILL.md"), "---\nname: go-conventions\ndescription: Go conventions\nmetadata:\n  tags: []\n---\n\n# Go Conventions\n")

	if err := os.MkdirAll(filepath.Join(dir, "go", "go-idioms"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go", "go-idioms", "SKILL.md"), "---\nname: go-idioms\ndescription: Go idioms\nmetadata:\n  tags: []\n---\n\n# Go Idioms\n")

	if err := os.MkdirAll(filepath.Join(dir, "python", "python-testing"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "python", "python-testing", "SKILL.md"), "---\nname: python-testing\ndescription: Python testing\nmetadata:\n  tags: []\n---\n\n# Python Testing\n")

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	// All skills should be discovered.
	if len(result.Entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d: %#v", len(result.Entries), result.Entries)
	}

	// The sub-bundles-ignored warning should NOT be emitted because the
	// loaded catalog already covers the "go" and "python" sub-directories.
	for _, m := range result.Malformed {
		if m.DirName == "catalog.discover.sub-bundles-ignored" {
			t.Fatalf("unexpected sub-bundles-ignored warning (catalog covers the sub-bundles): %v", m.Error)
		}
	}
}

// TestCatalogCoversSubBundles verifies the catalogCoversSubBundles helper.
func TestCatalogCoversSubBundles(t *testing.T) {
	dir := t.TempDir()

	// Create sub-bundle directories.
	goDir := filepath.Join(dir, "go")
	pythonDir := filepath.Join(dir, "python")

	entries := []Entry{
		{Kind: ir.KindSkill, Name: "go-conventions", Path: "go/go-conventions/SKILL.md"},
		{Kind: ir.KindSkill, Name: "go-idioms", Path: "go/go-idioms/SKILL.md"},
		{Kind: ir.KindSkill, Name: "python-testing", Path: "python/python-testing/SKILL.md"},
	}

	// All sub-bundles covered.
	if !catalogCoversSubBundles(entries, dir, []string{goDir, pythonDir}) {
		t.Fatal("expected catalogCoversSubBundles to return true when all sub-bundles are covered")
	}

	// Only one sub-bundle covered.
	uncoveredDir := filepath.Join(dir, "uncovered")
	if catalogCoversSubBundles(entries, dir, []string{goDir, uncoveredDir}) {
		t.Fatal("expected catalogCoversSubBundles to return false when a sub-bundle has no entries")
	}

	// Empty entries should return false for any sub-bundles.
	if catalogCoversSubBundles(nil, dir, []string{goDir}) {
		t.Fatal("expected catalogCoversSubBundles to return false for empty entries")
	}

	// No sub-bundles should return true.
	if !catalogCoversSubBundles(entries, dir, nil) {
		t.Fatal("expected catalogCoversSubBundles to return true for no sub-bundles")
	}
}

// TestSubPathRel verifies the subPathRel helper.
func TestSubPathRel(t *testing.T) {
	tests := []struct {
		path, prefix string
		want         bool
	}{
		{"go/go-conventions/SKILL.md", "go", true},
		{"go/go-idioms/SKILL.md", "go", true},
		{"golang/go-idioms/SKILL.md", "go", false},
		{"python/python-testing/SKILL.md", "go", false},
		{"python/python-testing/SKILL.md", "python", true},
		{"go/SKILL.md", "go", true}, // file directly inside "go/" is within the sub-bundle
		{"skills/my-skill/SKILL.md", "skills", true},
		{"skills/my-skill/SKILL.md", "", true},
		{"same", "same", true},
		{"other/SKILL.md", "same", false},
	}
	for _, tc := range tests {
		got := subPathRel(tc.path, tc.prefix)
		if got != tc.want {
			t.Errorf("subPathRel(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.want)
		}
	}
}

// TestScanForSubBundles_SkipsHarnessWorkspaceDirs verifies that harness
// workspace directories (.factory/, .claude/, .cursor/, etc.) are not
// treated as sub-bundles even though they contain skills/ or other roots.
// These are internal bundle structure, not independent sub-bundles.
func TestScanForSubBundles_SkipsHarnessWorkspaceDirs(t *testing.T) {
	dir := t.TempDir()

	// .factory/ with skills inside — this is the workspace form of a
	// bundle, NOT an independent sub-bundle.
	if err := os.MkdirAll(filepath.Join(dir, ".factory", "skills", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".factory", "skills", "main", "SKILL.md"), "---\nname: main\n---\nM\n")

	// .claude/ with skills inside — also workspace form.
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "skills", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".claude", "skills", "other", "SKILL.md"), "---\nname: other\n---\nO\n")

	// A real sub-bundle should still be found.
	if err := os.MkdirAll(filepath.Join(dir, "my-plugin", "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "my-plugin", "skills", "x", "SKILL.md"), "---\nname: x\n---\nX\n")

	bundles := scanForSubBundles(dir)

	if len(bundles) != 1 {
		t.Fatalf("expected 1 sub-bundle (my-plugin only), got %d: %v", len(bundles), bundles)
	}
	if bundles[0] != filepath.Join(dir, "my-plugin") {
		t.Errorf("bundle = %q, want %q", bundles[0], filepath.Join(dir, "my-plugin"))
	}
}

// TestDiscover_SubBundlePathsAbsolute verifies that entry paths from
// sub-bundles are absolute and point to the correct on-disk location.
func TestDiscover_SubBundlePathsAbsolute(t *testing.T) {
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "my-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "my-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, "skills", "my-skill", "SKILL.md"), `---
name: my-skill
description: My skill
metadata:
  tags: []
---

# My Skill
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	if len(result.Entries) == 0 {
		t.Fatal("expected at least one entry")
	}

	for _, e := range result.Entries {
		if e.Path == "" {
			t.Errorf("entry %q has empty Path", e.Name)
		}
		if !filepath.IsAbs(e.Path) {
			t.Errorf("entry %q Path %q is not absolute", e.Name, e.Path)
		}
	}
}

// TestDiscover_FactorySubBundle verifies recursive discovery works with
// the Factory harness form (.factory/skills/).
func TestDiscover_FactorySubBundle(t *testing.T) {
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "my-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, ".factory", "skills", "factory-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, ".factory", "skills", "factory-skill", "SKILL.md"), "Do the factory thing\n")

	result, err := Discover(dir, "factory")
	if err != nil {
		t.Fatalf("Discover(%q, \"factory\") = _, %v", dir, err)
	}

	found := false
	for _, e := range result.Entries {
		if e.Kind == ir.KindSkill && e.Name == "factory-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected factory-skill to be discovered, got %#v", result.Entries)
	}
}

// TestScanForSubBundles_SkipsGitDir verifies that .git directories are not
// descended into during sub-bundle scanning, preventing false positives from
// .git/hooks/ being detected as a Claude bare-plugin form.
func TestScanForSubBundles_SkipsGitDir(t *testing.T) {
	dir := t.TempDir()

	// Create a .git/hooks structure like a real git repo
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\n")

	// Create a real sub-bundle
	if err := os.MkdirAll(filepath.Join(dir, "my-plugin", "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "my-plugin", "skills", "x", "SKILL.md"), "---\nname: x\n---\nX\n")

	bundles := scanForSubBundles(dir)

	if len(bundles) != 1 {
		t.Fatalf("expected 1 sub-bundle (my-plugin, NOT .git), got %d: %v", len(bundles), bundles)
	}
	if bundles[0] != filepath.Join(dir, "my-plugin") {
		t.Errorf("bundle = %q, want %q", bundles[0], filepath.Join(dir, "my-plugin"))
	}
}

// TestDiscover_BundleLayoutWithGitDir verifies that a registry with
// a proper bundle layout (skills under skills/) and a .git directory
// works correctly — .git is ignored during discovery.
func TestDiscover_BundleLayoutWithGitDir(t *testing.T) {
	dir := t.TempDir()

	// Create .git directory (like a cloned repo)
	if err := os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")

	// Create skill under skills/ (proper HCC-recognizable layout)
	if err := os.MkdirAll(filepath.Join(dir, "skills", "vm-lifecycle"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "skills", "vm-lifecycle", "SKILL.md"), `---
name: vm-lifecycle
description: VM lifecycle management
metadata:
  tags:
    - openstack
---

# VM Lifecycle
`)

	result, err := Discover(dir, "")
	if err != nil {
		t.Fatalf("Discover(%q, \"\") = _, %v", dir, err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %#v", len(result.Entries), result.Entries)
	}
	if result.Entries[0].Name != "vm-lifecycle" {
		t.Errorf("entry name = %q, want %q", result.Entries[0].Name, "vm-lifecycle")
	}
}

// TestDiscover_SubBundlesPopulated verifies that Discover populates the
// SubBundles field when recursive discovery is triggered.
func TestDiscover_SubBundlesPopulated(t *testing.T) {
	dir := t.TempDir()

	pluginA := filepath.Join(dir, "alpha-plugin")
	if err := os.MkdirAll(filepath.Join(pluginA, "skills", "skill-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginA, "skills", "skill-a", "SKILL.md"), `---
name: skill-a
description: Alpha skill
metadata:
  tags: []
---

# Skill A
`)

	pluginB := filepath.Join(dir, "beta-plugin")
	if err := os.MkdirAll(filepath.Join(pluginB, "skills", "skill-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginB, "skills", "skill-b", "SKILL.md"), `---
name: skill-b
description: Beta skill
metadata:
  tags: []
---

# Skill B
`)

	result, err := Discover(dir, "claude")
	if err != nil {
		t.Fatalf("Discover(%q, \"claude\") = _, %v", dir, err)
	}

	if len(result.SubBundles) != 2 {
		t.Fatalf("expected 2 SubBundles, got %d", len(result.SubBundles))
	}

	if _, ok := result.SubBundles[pluginA]; !ok {
		t.Errorf("expected SubBundles to contain %q", pluginA)
	}
	if _, ok := result.SubBundles[pluginB]; !ok {
		t.Errorf("expected SubBundles to contain %q", pluginB)
	}

	// Each SubBundle should have its own entries
	if len(result.SubBundles[pluginA].Entries) != 1 || result.SubBundles[pluginA].Entries[0].Name != "skill-a" {
		t.Errorf("alpha SubBundle entries: %#v", result.SubBundles[pluginA].Entries)
	}
	if len(result.SubBundles[pluginB].Entries) != 1 || result.SubBundles[pluginB].Entries[0].Name != "skill-b" {
		t.Errorf("beta SubBundle entries: %#v", result.SubBundles[pluginB].Entries)
	}
}

// TestLoadRegistries_SplitsSubBundlesIntoSeparateCatalogs verifies that
// LoadRegistries creates a separate catalog per sub-bundle when recursive
// discovery is triggered, so the TUI groups each sub-bundle under its own
// registry label.
func TestLoadRegistries_SplitsSubBundlesIntoSeparateCatalogs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Create a local registry that is a meta-directory with sub-bundles.
	regDir := t.TempDir()
	pluginA := filepath.Join(regDir, "alpha-plugin")
	if err := os.MkdirAll(filepath.Join(pluginA, "skills", "skill-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginA, "skills", "skill-a", "SKILL.md"), `---
name: skill-a
description: Alpha skill
metadata:
  tags: []
---

# Skill A
`)

	pluginB := filepath.Join(regDir, "beta-plugin")
	if err := os.MkdirAll(filepath.Join(pluginB, "skills", "skill-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginB, "skills", "skill-b", "SKILL.md"), `---
name: skill-b
description: Beta skill
metadata:
  tags: []
---

# Skill B
`)

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:           regDir,
			Ref:           "",
			Source:        config.RepoCommitted,
			SourceHarness: "claude",
		}},
	}

	// Use WithCloneOrPull to override so we don't need git.
	result := LoadRegistries(cfg, "", LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			return regDir, nil
		}),
	)

	if len(result.Errors) > 0 {
		t.Fatalf("LoadRegistries returned errors: %v", result.Errors)
	}

	// Should have 3 catalogs: one per sub-bundle plus the merged parent catalog.
	if len(result.Catalogs) != 3 {
		t.Fatalf("expected 3 catalogs (two sub-bundles + parent), got %d: %v", len(result.Catalogs), catalogKeyList(result.Catalogs))
	}

	// Each sub-bundle catalog should have exactly one entry; the parent
	// catalog has the merged entries from both sub-bundles.
	subBundleCatalogCount := 0
	parentCatalogCount := 0
	totalEntries := 0
	for key, cat := range result.Catalogs {
		if len(cat.Entries) == 1 {
			subBundleCatalogCount++
		} else if len(cat.Entries) == 2 {
			parentCatalogCount++
		} else {
			t.Errorf("catalog %q has %d entries, want 1 or 2", key, len(cat.Entries))
		}
		totalEntries += len(cat.Entries)
	}
	if subBundleCatalogCount != 2 {
		t.Errorf("expected 2 sub-bundle catalogs with 1 entry each, got %d", subBundleCatalogCount)
	}
	if parentCatalogCount != 1 {
		t.Errorf("expected 1 parent catalog with 2 merged entries, got %d", parentCatalogCount)
	}
	// 2 sub-bundle entries + 2 parent merged entries = 4 total
	if totalEntries != 4 {
		t.Errorf("total entries across all catalogs = %d, want 4", totalEntries)
	}

	// Config should have 2 registry entries (one per sub-bundle).
	if len(result.Config.Registries) != 2 {
		t.Fatalf("expected 2 registry entries, got %d", len(result.Config.Registries))
	}

	// Each registry entry should have a distinct path.
	paths := make(map[string]bool)
	for _, e := range result.Config.Registries {
		if paths[e.Path] {
			t.Errorf("duplicate path %q in registry entries", e.Path)
		}
		paths[e.Path] = true
	}

	// Verify paths include the sub-bundle directory names.
	if !paths["alpha-plugin"] && !paths["beta-plugin"] {
		t.Errorf("expected registry paths to include sub-bundle names, got %v", paths)
	}
}

// TestLoadRegistries_ParentCatalogKeyPopulatedForSubBundles verifies that
// when recursive discovery splits a meta-directory into per-sub-bundle
// catalogs, the parent entry's catalog key also exists in the catalogs map
// with the merged catalog. This allows callers that only know the parent
// entry (e.g. install --from) to look up resources without needing to know
// about the sub-bundle decomposition.
func TestLoadRegistries_ParentCatalogKeyPopulatedForSubBundles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regDir := t.TempDir()
	pluginA := filepath.Join(regDir, "alpha-plugin")
	if err := os.MkdirAll(filepath.Join(pluginA, "skills", "skill-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginA, "skills", "skill-a", "SKILL.md"), `---
name: skill-a
description: Alpha skill
metadata:
  tags: []
---

# Skill A
`)

	pluginB := filepath.Join(regDir, "beta-plugin")
	if err := os.MkdirAll(filepath.Join(pluginB, "skills", "skill-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginB, "skills", "skill-b", "SKILL.md"), `---
name: skill-b
description: Beta skill
metadata:
  tags: []
---

# Skill B
`)

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:           regDir,
			Ref:           "",
			Source:        config.RepoCommitted,
			SourceHarness: "claude",
		}},
	}

	result := LoadRegistries(cfg, "", LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			return regDir, nil
		}),
	)

	if len(result.Errors) > 0 {
		t.Fatalf("LoadRegistries returned errors: %v", result.Errors)
	}

	// The parent entry's CatalogKey should also exist in the catalogs map,
	// containing the merged catalog with all sub-bundle resources.
	parentKey := cfg.Registries[0].CatalogKey
	if parentKey == "" {
		t.Fatal("parent entry CatalogKey was not set")
	}

	parentCat, ok := result.Catalogs[parentKey]
	if !ok {
		t.Fatalf("parent entry catalog key %q not found in catalogs map; keys: %v", parentKey, catalogKeyList(result.Catalogs))
	}

	// The merged catalog should contain entries from both sub-bundles.
	names := make(map[string]bool)
	for _, e := range parentCat.Entries {
		names[e.Name] = true
	}
	if !names["skill-a"] {
		t.Error("parent catalog missing skill-a")
	}
	if !names["skill-b"] {
		t.Error("parent catalog missing skill-b")
	}
}

func catalogKeyList(catalogs map[string]LoadedCatalog) []string {
	keys := make([]string, 0, len(catalogs))
	for k := range catalogs {
		keys = append(keys, k)
	}
	return keys
}

// TestDiscoverSubBundles_ReportsErrorsWhenSubBundleFails verifies that
// discoverSubBundles surfaces errors from failed sub-bundle loads rather than
// silently swallowing them. A partial success (some sub-bundles load, some
// fail) should still return the successfully loaded entries with errors in
// the Errors field.
func TestDiscoverSubBundles_ReportsErrorsWhenSubBundleFails(t *testing.T) {
	dir := t.TempDir()

	// One good sub-bundle
	goodDir := filepath.Join(dir, "good-plugin")
	if err := os.MkdirAll(filepath.Join(goodDir, "skills", "good-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(goodDir, "skills", "good-skill", "SKILL.md"), `---
name: good-skill
description: Good skill
metadata:
  tags: []
---

# Good
`)

	// Test discoverSubBundles directly by passing a mix of valid and
	// non-existent paths. Load will fail on the non-existent directory.
	badDir := filepath.Join(dir, "nonexistent-plugin")

	result, err := discoverSubBundles([]string{goodDir, badDir}, "claude")
	if err != nil {
		t.Fatalf("discoverSubBundles returned error: %v (expected partial success)", err)
	}

	// The good skill must be present.
	names := make(map[string]bool)
	for _, e := range result.Entries {
		names[e.Name] = true
	}
	if !names["good-skill"] {
		t.Errorf("expected good-skill to be discovered, got entries: %v", names)
	}

	// Errors from the bad sub-bundle must be reported.
	if len(result.Errors) == 0 {
		t.Error("expected Errors to contain at least one sub-bundle error, got none")
	}

	// SubBundles should contain the good one but not the bad one.
	if _, ok := result.SubBundles[goodDir]; !ok {
		t.Errorf("expected SubBundles to contain %q", goodDir)
	}
	if _, ok := result.SubBundles[badDir]; ok {
		t.Errorf("expected SubBundles to NOT contain bad dir %q", badDir)
	}
}

// TestDiscoverSubBundles_AllFailReturnsError verifies that when ALL
// sub-bundles fail to load, discoverSubBundles returns an error (not an
// empty catalog with no explanation).
func TestDiscoverSubBundles_AllFailReturnsError(t *testing.T) {
	dir := t.TempDir()

	// Two non-existent directories
	aaaDir := filepath.Join(dir, "aaa-plugin")
	bbbDir := filepath.Join(dir, "bbb-plugin")

	_, err := discoverSubBundles([]string{aaaDir, bbbDir}, "claude")
	if err == nil {
		t.Fatal("expected error when all sub-bundles fail to load, got nil")
	}
	if !strings.Contains(err.Error(), "sub-bundle") {
		t.Errorf("expected error to mention sub-bundle, got: %v", err)
	}
}

// TestLoadRegistries_SubBundleErrorsPropagated verifies that sub-bundle
// errors from discoverSubBundles are propagated into the LoadResult.Errors
// slice so callers (CLI, TUI) can surface them to the user.
func TestLoadRegistries_SubBundleErrorsPropagated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regDir := t.TempDir()
	// One valid sub-bundle
	goodDir := filepath.Join(regDir, "good-plugin")
	if err := os.MkdirAll(filepath.Join(goodDir, "skills", "good-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(goodDir, "skills", "good-skill", "SKILL.md"), `---
name: good-skill
description: Good skill
metadata:
  tags: []
---

# Good
`)

	cfg := &config.Config{
		Registries: []config.RegistryEntry{{
			URL:           regDir,
			Ref:           "",
			Source:        config.RepoCommitted,
			SourceHarness: "claude",
		}},
	}

	// Inject a discover function that returns SubBundles with a partial error.
	badDir := filepath.Join(regDir, "nonexistent-plugin")
	result := LoadRegistries(cfg, "", LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			return regDir, nil
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			if dir == regDir {
				// Return a catalog with SubBundles: one good, one failed.
				goodCat, _ := Discover(goodDir, sourceHarness)
				return LoadedCatalog{
					SubBundles: map[string]LoadedCatalog{
						goodDir: goodCat,
					},
					Errors: []error{fmt.Errorf("sub-bundle %q: directory does not exist", badDir)},
				}, nil
			}
			return Discover(dir, sourceHarness)
		}),
	)

	// The good sub-bundle should produce a catalog.
	if len(result.Catalogs) == 0 {
		t.Fatalf("expected at least one catalog from the good sub-bundle, got none")
	}

	// Sub-bundle errors must be in result.Errors.
	hasSubBundleErr := false
	for _, e := range result.Errors {
		if strings.Contains(e.Error(), "sub-bundle") {
			hasSubBundleErr = true
			break
		}
	}
	if !hasSubBundleErr {
		t.Errorf("expected LoadResult.Errors to contain sub-bundle error, got: %v", result.Errors)
	}
}

// TestLoadRegistries_TopLevelAndSubBundleOverlap verifies the behavior when
// a config explicitly defines a registry entry for a subdirectory that would
// also be discovered as a sub-bundle via recursive discovery. The explicit
// entry should take precedence and the sub-bundle should not be duplicated.
func TestLoadRegistries_TopLevelAndSubBundleOverlap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regDir := t.TempDir()

	// Sub-bundle that will be discovered via recursion AND explicit entry
	pluginDir := filepath.Join(regDir, "pm-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "pm-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, "skills", "pm-skill", "SKILL.md"), `---
name: pm-skill
description: PM skill
metadata:
  tags: []
---

# PM Skill
`)

	// Another sub-bundle that is NOT explicitly referenced
	otherDir := filepath.Join(regDir, "other-plugin")
	if err := os.MkdirAll(filepath.Join(otherDir, "skills", "other-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(otherDir, "skills", "other-skill", "SKILL.md"), `---
name: other-skill
description: Other skill
metadata:
  tags: []
---

# Other Skill
`)

	// Config has two entries for the same git repo: one pointing at the root
	// (will trigger recursive discovery) and one explicitly pointing at
	// pm-plugin with a specific skill selection.
	// Using a git URL to ensure CatalogKeyForEntry produces consistent keys
	// for both the explicit entry and the processSubBundles-derived entry.
	gitURL := "git@example.com:repo.git"
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:           gitURL,
				Ref:           "main",
				Source:        config.UserPersonal,
				SourceHarness: "claude",
			},
			{
				URL:           gitURL,
				Ref:           "main",
				Path:          "pm-plugin",
				Source:        config.UserPersonal,
				SourceHarness: "claude",
				Skills:        config.KindSelector{Include: []string{"pm-skill"}},
			},
		},
	}

	pluginCat, _ := Discover(pluginDir, "claude")
	otherCat, _ := Discover(otherDir, "claude")

	result := LoadRegistries(cfg, "", LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			return regDir, nil
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			if dir == regDir {
				// Root: return SubBundles so recursive discovery triggers
				return LoadedCatalog{
					SubBundles: map[string]LoadedCatalog{
						pluginDir: pluginCat,
						otherDir:  otherCat,
					},
				}, nil
			}
			if dir == pluginDir {
				return pluginCat, nil
			}
			if dir == otherDir {
				return otherCat, nil
			}
			return LoadedCatalog{}, nil
		}),
	)

	if len(result.Errors) > 0 {
		// Overlap warnings are acceptable; hard errors are not.
		for _, err := range result.Errors {
			if !IsOverlapWarning(err) {
				t.Fatalf("LoadRegistries returned unexpected error: %v", err)
			}
		}
	}

	// Should have 3 catalogs: pm-plugin, other-plugin, and the merged parent.
	if len(result.Catalogs) != 3 {
		t.Fatalf("expected 3 catalogs (pm-plugin + other-plugin + parent), got %d: %v",
			len(result.Catalogs), catalogKeyList(result.Catalogs))
	}

	// Verify pm-skill is not duplicated across catalogs.
	totalPmSkill := 0
	for _, cat := range result.Catalogs {
		for _, e := range cat.Entries {
			if e.Name == "pm-skill" {
				totalPmSkill++
			}
		}
	}
	if totalPmSkill != 1 {
		t.Errorf("expected pm-skill to appear exactly once across all catalogs, got %d", totalPmSkill)
	}
}

// TestLoadRegistries_OverlappingPathPreservesPolicy verifies that when
// a registry entry's skill directory overlaps with a sub-bundle discovered
// via recursive discovery, the overlap skip path preserves the explicit
// entry's policy (default, required, selectors) and provides valid catalog
// data instead of silently dropping it.
func TestLoadRegistries_OverlappingPathPreservesPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regDir := t.TempDir()

	// Sub-bundle that will be discovered via recursion AND explicit entry
	pluginDir := filepath.Join(regDir, "pm-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "pm-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, "skills", "pm-skill", "SKILL.md"), `---
name: pm-skill
description: PM skill
metadata:
  tags: []
---

# PM Skill
`)

	// Another sub-bundle that is NOT explicitly referenced
	otherDir := filepath.Join(regDir, "other-plugin")
	if err := os.MkdirAll(filepath.Join(otherDir, "skills", "other-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(otherDir, "skills", "other-skill", "SKILL.md"), `---
name: other-skill
description: Other skill
metadata:
  tags: []
---

# Other Skill
`)

	gitURL := "git@example.com:repo.git"
	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:           gitURL,
				Ref:           "main",
				Source:        config.UserPersonal,
				SourceHarness: "claude",
			},
			{
				URL:           gitURL,
				Ref:           "main",
				Path:          "pm-plugin",
				Source:        config.UserPersonal,
				SourceHarness: "claude",
				Default:       true, // policy that must reach the resolver
			},
		},
	}

	pluginCat, _ := Discover(pluginDir, "claude")
	otherCat, _ := Discover(otherDir, "claude")

	// Realistic mock: parent catalog contains merged entries from all
	// sub-bundles, plus SubBundles for decomposition.
	mergedParentEntries := append(append([]Entry{}, pluginCat.Entries...), otherCat.Entries...)
	parentCatWithMerge := LoadedCatalog{
		Entries: mergedParentEntries,
		SubBundles: map[string]LoadedCatalog{
			pluginDir: pluginCat,
			otherDir:  otherCat,
		},
	}

	result := LoadRegistries(cfg, "", LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			return regDir, nil
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			if dir == regDir {
				return parentCatWithMerge, nil
			}
			if dir == pluginDir {
				return pluginCat, nil
			}
			if dir == otherDir {
				return otherCat, nil
			}
			return LoadedCatalog{}, nil
		}),
	)

	// Overlap warnings are acceptable.
	for _, err := range result.Errors {
		if !IsOverlapWarning(err) {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Verify that the explicit pm-plugin entry's policy (default:true)
	// reaches the resolver by checking its registry entry has Default=true.
	foundExplicitEntry := false
	for _, entry := range result.Config.Registries {
		if entry.Path == "pm-plugin" && entry.Default {
			foundExplicitEntry = true
			break
		}
	}
	if !foundExplicitEntry {
		t.Errorf("explicit pm-plugin entry with default:true should be in Config.Registries")
	}

	// The explicit pm-plugin entry should have a valid catalog (not empty).
	pluginKey := cfg.Registries[1].CatalogKey
	if cat, ok := result.Catalogs[pluginKey]; !ok || len(cat.Entries) == 0 {
		t.Errorf("pm-plugin catalog should contain pm-skill, got entries: %v", cat.Entries)
	}
}

// TestLoadRegistries_BundleRootSingleDiscover verifies that discoverAll
// calls discover exactly once per registry entry. There is no separate
// root-scan pass (removed with bundle-root semantics).
func TestLoadRegistries_BundleRootSingleDiscover(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regDir := t.TempDir()
	bundleDir := filepath.Join(regDir, ".factory")
	skillDir := filepath.Join(bundleDir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), `---
name: review
description: Review skill
metadata:
  tags: []
---

# Review
`)

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:           "git@example.com:repo.git",
				Ref:           "main",
				Path:          ".factory",
				Source:        config.UserPersonal,
				SourceHarness: "factory",
			},
		},
	}

	discoverCalls := map[string]int{}
	result := LoadRegistries(cfg, "", LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			return regDir, nil
		}),
		WithDiscoverFunc(func(dir, sourceHarness string) (LoadedCatalog, error) {
			discoverCalls[dir]++
			return Discover(dir, sourceHarness)
		}),
	)

	for _, err := range result.Errors {
		t.Fatalf("unexpected error: %v", err)
	}

	// discover should be called exactly once for the bundle dir.
	if discoverCalls[bundleDir] != 1 {
		t.Errorf("expected exactly 1 discover call for %q, got %d", bundleDir, discoverCalls[bundleDir])
	}

	// No separate root scan for regDir.
	if discoverCalls[regDir] > 0 {
		t.Errorf("expected no discover call for root %q, got %d", regDir, discoverCalls[regDir])
	}

	// The review skill should be found.
	found := false
	for _, cat := range result.Catalogs {
		for _, e := range cat.Entries {
			if e.Name == "review" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected review skill in catalog")
	}
}

// TestLoadRegistries_CatalogKeyNormalization verifies that entries with
// equivalent paths (e.g. "./bundle" vs "bundle") produce the same catalog key.
func TestLoadRegistries_CatalogKeyNormalization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regDir := t.TempDir()
	bundleDir := filepath.Join(regDir, "bundle")
	skillDir := filepath.Join(bundleDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), `---
name: test-skill
description: Test skill
metadata:
  tags: []
---

# Test
`)

	gitURL := "git@example.com:repo.git"
	cfg1 := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: gitURL, Ref: "main", Path: "./bundle", Source: config.UserPersonal, SourceHarness: "factory"},
		},
	}
	cfg2 := &config.Config{
		Registries: []config.RegistryEntry{
			{URL: gitURL, Ref: "main", Path: "bundle", Source: config.UserPersonal, SourceHarness: "factory"},
		},
	}

	mockClone := func(url, ref string) (string, error) { return regDir, nil }

	r1 := LoadRegistries(cfg1, "", LoadModeRefresh, WithCloneOrPull(mockClone))
	r2 := LoadRegistries(cfg2, "", LoadModeRefresh, WithCloneOrPull(mockClone))

	// Both should produce the same catalog key for the registry entry.
	if cfg1.Registries[0].CatalogKey != cfg2.Registries[0].CatalogKey {
		t.Errorf("catalog keys should match for equivalent paths: %q vs %q",
			cfg1.Registries[0].CatalogKey, cfg2.Registries[0].CatalogKey)
	}

	// Both should find the same skill.
	for _, r := range []LoadResult{r1, r2} {
		found := false
		for _, cat := range r.Catalogs {
			for _, e := range cat.Entries {
				if e.Name == "test-skill" {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("expected test-skill in catalog")
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkLoadRegistries_BundleRootDiscover(b *testing.B) {
	regDir := b.TempDir()
	bundleDir := filepath.Join(regDir, ".factory")
	for i := 0; i < 20; i++ {
		skillDir := filepath.Join(bundleDir, "skills", fmt.Sprintf("skill-%02d", i))
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			b.Fatal(err)
		}
		content := fmt.Sprintf("---\nname: skill-%02d\ndescription: Skill %d\nmetadata:\n  tags: []\n---\n\n# Skill %d\n", i, i, i)
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	cfg := &config.Config{
		Registries: []config.RegistryEntry{
			{
				URL:           "git@example.com:repo.git",
				Ref:           "main",
				Path:          ".factory",
				Source:        config.UserPersonal,
				SourceHarness: "factory",
			},
		},
	}
	mockClone := func(url, ref string) (string, error) { return regDir, nil }

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfgCopy := *cfg
		cfgCopy.Registries = append([]config.RegistryEntry{}, cfg.Registries...)
		LoadRegistries(&cfgCopy, "", LoadModeRefresh, WithCloneOrPull(mockClone))
	}
}

func BenchmarkDiscoverMalformedPathNormalization(b *testing.B) {
	dir := b.TempDir()
	skillDir := filepath.Join(dir, "skills", "broken-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: broken-skill\ndescription: \"unmatched quote\n---\nBody\n"),
		0o644,
	); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Discover(dir, "factory"); err != nil {
			b.Fatal(err)
		}
	}
}
