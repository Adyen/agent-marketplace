package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

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

	// 1. The code-level defaults are the single source of truth for skeleton
	//    configs; public selectability must not silently change them.
	got := DefaultHarnesses()
	want := []string{"factory", "windsurf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultHarnesses() = %#v, want %#v; a default harness list changed", got, want)
	}

	// 2. The generated global skeleton, so a future change to the skeleton
	//    writer cannot slip an opt-in target into a config the user never typed.
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
	shipped, err := ParseV4Config(data, globalPath, true)
	if err != nil {
		t.Fatalf("parse generated v4 skeleton: %v", err)
	}
	if shipped.APIVersion != APIVersionV4 || !reflect.DeepEqual(shipped.Harnesses, []string{"factory", "windsurf"}) {
		t.Fatalf("generated skeleton version/defaults = %#v", shipped)
	}
	lists := [][]string{shipped.Harnesses}
	for _, group := range shipped.Groups {
		lists = append(lists, group.Harnesses)
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

func TestUnsupportedConfigIsNotRewrittenWithOptInTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	userPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	mustMkdirAll(t, filepath.Dir(userPath))
	const legacy = "apiVersion: agent-marketplace/v2\nskills:\n  include:\n    - legacy-skill\n"
	mustWriteFile(t, userPath, []byte(legacy))
	before, err := os.Stat(userPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadGlobalConfig()
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.api-version" {
		t.Fatalf("unsupported root error = %v, want v4.api-version", err)
	}
	err = EnsureGlobalConfig()
	validation = nil
	if !errors.As(err, &validation) || validation.Code != "v4.api-version" {
		t.Fatalf("initialization error = %v, want v4.api-version without conversion", err)
	}
	after, err := os.Stat(userPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(userPath)
	if err != nil || string(data) != legacy || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		t.Fatalf("refused config was rewritten: bytes=%q, err=%v", data, err)
	}

	// Explicit requests are still honored; selectable does not mean default.
	mustWriteFile(t, userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory, windsurf, devin, codex]\n"))
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("explicit opt-in control: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "windsurf", "devin", "codex"}) {
		t.Fatalf("explicit opt-in harnesses = %#v", cfg.Harnesses)
	}
}
