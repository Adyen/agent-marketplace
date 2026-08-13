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

// TestDefaultHarnessListsExcludeDevin pins the rollout rule of work package D11
// of docs/plans/2026-08-03-devin-first-class-harness.md:
//
//	"Devin remains opt-in."
//	"Existing and new default harness lists remain unchanged."
//	Non-goals: "Adding Devin to default harness lists",
//	           "Automatic migration of existing configurations".
//
// D11 made Devin selectable in the TUI (target registry UI=true). This test is
// the guard that selectable did not become default: it asserts what a user who
// asks for nothing gets, from the three places a harness list can appear
// without the user typing one.
//
// The v3 cutover removed the shipped organization defaults (defaults/org.yaml
// and defaults/org_base.yaml) — an organization now supplies its own external
// config at OrganizationConfigPath(), which this repository does not ship. The
// remaining source of an unrequested harness list is the generated skeleton, so
// that is what part 2 reads.
func TestDefaultHarnessListsExcludeDevin(t *testing.T) {
	devin := string(targetmeta.TargetDevin)

	// 1. The code-level defaults, which are the single source of truth for
	//    skeleton configs and for `migrate --harnesses`.
	got := DefaultHarnesses()
	want := []string{"factory", "windsurf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultHarnesses() = %#v, want %#v; a default harness list changed", got, want)
	}

	// 2. The generated global skeleton, so a future change to the skeleton
	//    writer cannot slip Devin into a config the user never typed.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := EnsureGlobalConfig(); err != nil {
		t.Fatalf("EnsureGlobalConfig: %v", err)
	}
	globalPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
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
		if slices.Contains(list, devin) {
			t.Fatalf("the generated skeleton declares %q in a harness list; Devin must stay opt-in", devin)
		}
	}

	// 3. Devin IS a fully declared, selectable target — the point is that
	//    selectable and default are different things.
	definition, ok := targetmeta.Lookup(devin)
	if !ok {
		t.Fatalf("target registry no longer declares %q", devin)
	}
	if !definition.UI {
		t.Fatalf("%q must be selectable in the TUI after D11", devin)
	}
}

// TestMigrationNeverAddsDevin pins the "no automatic migration" non-goal from
// the other direction: the one code path that WRITES a harness list on a user's
// behalf writes the defaults, and the defaults do not contain Devin.
//
// The v2-to-v3 migration is that path now; v1 input is rejected outright.
func TestMigrationNeverAddsDevin(t *testing.T) {
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
	if strings.Contains(string(migrated), string(targetmeta.TargetDevin)) {
		t.Fatalf("migration added Devin to a config that never asked for it:\n%s", migrated)
	}
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "windsurf"}) {
		t.Fatalf("migrated harnesses = %#v, want the unchanged defaults", cfg.Harnesses)
	}
}
