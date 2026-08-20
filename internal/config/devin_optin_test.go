package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// TestDefaultHarnessListsExcludeOptInTargets guards the distinction between a
// publicly selectable target and a default target for both Devin and Codex.
//
// The v3 cutover removed the shipped organization defaults (defaults/org.yaml
// and defaults/org_base.yaml) — an organization now supplies its own external
// config at OrganizationConfigPath(), which this repository does not ship. The
// remaining source of an unrequested harness list is the generated skeleton, so
// that is what part 2 reads.
func TestDefaultHarnessListsExcludeOptInTargets(t *testing.T) {
	optIn := []string{string(targetmeta.TargetDevin), string(targetmeta.TargetCodex)}

	// 1. The code-level defaults, which are the single source of truth for
	//    skeleton configs and for `migrate --harnesses`.
	got := DefaultHarnesses()
	want := []string{"factory", "windsurf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultHarnesses() = %#v, want %#v; a default harness list changed", got, want)
	}

	// 2. The generated global skeleton, so a future change to the skeleton
	//    writer cannot slip an opt-in target into a config the user never typed.
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	doc, mode, err := readOrInitConfig(globalPath)
	if err != nil {
		t.Fatalf("readOrInitConfig: %v", err)
	}
	if err := writeConfigDocument(globalPath, mode, doc); err != nil {
		t.Fatalf("write generated skeleton: %v", err)
	}
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read generated skeleton: %v", err)
	}
	var shipped Config
	if err := yaml.Unmarshal(data, &shipped); err != nil {
		t.Fatalf("parse generated skeleton: %v", err)
	}
	lists := [][]string{shipped.Harnesses}
	for _, r := range shipped.Registries {
		lists = append(lists, r.Harnesses)
	}
	for _, list := range lists {
		for _, target := range optIn {
			if slices.Contains(list, target) {
				t.Fatalf("the generated skeleton declares %q in a harness list; it must stay opt-in", target)
			}
		}
	}

	// 3. Both are fully declared, selectable targets — the point is that
	//    selectable and default are different things.
	for _, target := range optIn {
		definition, ok := targetmeta.Lookup(target)
		if !ok {
			t.Fatalf("target registry no longer declares %q", target)
		}
		if !definition.UI {
			t.Fatalf("%q must be selectable in the TUI", target)
		}
	}
}

// TestMigrationNeverAddsOptInTargets pins the "no automatic migration" rule:
// generated harness lists use defaults and do not add Devin or Codex.
//
// The v2-to-v3 migration is that path now; v1 input is rejected outright.
func TestMigrationNeverAddsOptInTargets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// A v2 config with no harnesses: the only shape that gets a harness list
	// written for it automatically, from the explicit --harnesses argument.
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v2\nskills:\n  include:\n    - legacy-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateConfigFiles(MigratePaths{User: userPath}, DefaultHarnesses()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	migrated, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []targetmeta.Target{targetmeta.TargetDevin, targetmeta.TargetCodex} {
		if strings.Contains(string(migrated), string(target)) {
			t.Fatalf("migration added %s to a config that never asked for it:\n%s", target, migrated)
		}
	}
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "windsurf"}) {
		t.Fatalf("migrated harnesses = %#v, want the unchanged defaults", cfg.Harnesses)
	}
}
