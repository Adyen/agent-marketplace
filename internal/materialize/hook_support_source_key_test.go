package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func localHookSupportTarget(t *testing.T, registry, registryPath string, machine bool) skillTarget {
	t.Helper()
	return skillTarget{
		skill: resolver.ResolvedArtifact{
			Entry:        catalog.Entry{Name: "guard"},
			RegistryURL:  registry,
			RegistryPath: registryPath,
			V4Kind:       config.V4SourceDirectory,
		},
		target:       hcc.TargetClaude,
		machineScope: machine,
	}
}

func mustHookSupportSourceKeyForScope(t *testing.T, w skillTarget, repoRoot string) string {
	t.Helper()
	key, err := hookSupportSourceKeyForScope(w, repoRoot)
	if err != nil {
		t.Fatalf("hookSupportSourceKeyForScope: %v", err)
	}
	return key
}

func keyDigest(key string) string {
	return key[strings.LastIndex(key, "-")+1:]
}

func TestHookSupportSourceKeyForScopeRepositoryLocalPlacement(t *testing.T) {
	root := t.TempDir()
	repoA := filepath.Join(root, "a")
	repoB := filepath.Join(root, "b")
	for _, dir := range []string{
		filepath.Join(repoA, "vendor", "reg"),
		filepath.Join(repoA, "vendor", "other"),
		filepath.Join(repoB, "vendor", "reg"),
		filepath.Join(root, "outside", "reg"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	sameRelativeA := mustHookSupportSourceKeyForScope(t, localHookSupportTarget(t, filepath.Join(repoA, "vendor", "reg"), "", false), repoA)
	sameRelativeB := mustHookSupportSourceKeyForScope(t, localHookSupportTarget(t, filepath.Join(repoB, "vendor", "reg"), "", false), repoB)
	if sameRelativeA != sameRelativeB {
		t.Fatalf("same relative placement keys differ across checkouts: %q vs %q", sameRelativeA, sameRelativeB)
	}

	machineA := mustHookSupportSourceKeyForScope(t, localHookSupportTarget(t, filepath.Join(repoA, "vendor", "reg"), "", true), repoA)
	if machineA == sameRelativeA {
		t.Fatalf("machine-scope key collapsed onto repository-local key %q", machineA)
	}

	other := mustHookSupportSourceKeyForScope(t, localHookSupportTarget(t, filepath.Join(repoA, "vendor", "other"), "", false), repoA)
	if other == sameRelativeA {
		t.Fatalf("different relative sources collapsed onto key %q", other)
	}

	subpathReg := mustHookSupportSourceKeyForScope(t, localHookSupportTarget(t, filepath.Join(repoA, "vendor"), "reg", false), repoA)
	if keyDigest(subpathReg) != keyDigest(sameRelativeA) {
		t.Fatalf("registry subpath reg and directory vendor/reg differ: %q vs %q", subpathReg, sameRelativeA)
	}
	subpathOther := mustHookSupportSourceKeyForScope(t, localHookSupportTarget(t, filepath.Join(repoA, "vendor"), "other", false), repoA)
	if keyDigest(subpathOther) == keyDigest(subpathReg) {
		t.Fatalf("different registry subpaths collapsed onto key %q", subpathOther)
	}

	outside := localHookSupportTarget(t, filepath.Join(root, "outside", "reg"), "", false)
	outsideKey := mustHookSupportSourceKeyForScope(t, outside, repoA)
	absolute, err := hookSupportSourceKey(outside.skill)
	if err != nil {
		t.Fatal(err)
	}
	if outsideKey != absolute {
		t.Fatalf("escaping source key = %q, want absolute fallback %q", outsideKey, absolute)
	}

	alias := filepath.Join(repoA, "alias")
	if err := os.Symlink(filepath.Join(root, "outside", "reg"), alias); err != nil {
		t.Fatal(err)
	}
	aliasTarget := localHookSupportTarget(t, alias, "", false)
	aliasKey := mustHookSupportSourceKeyForScope(t, aliasTarget, repoA)
	aliasAbsolute, err := hookSupportSourceKey(aliasTarget.skill)
	if err != nil {
		t.Fatal(err)
	}
	if aliasKey != aliasAbsolute {
		t.Fatalf("external alias key = %q, want absolute fallback %q", aliasKey, aliasAbsolute)
	}
}
