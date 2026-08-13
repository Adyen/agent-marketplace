package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// TestConcurrentDisableIsLostUpdateProne documents the concurrency behaviour of
// the read-modify-write cycle that `sync --prune-stale` drives: every editor
// entry point reads the whole file, edits a yaml.Node and renames a fresh temp
// file over the original, with no lock and no compare-and-swap. Two agents
// pruning the same repo at the same time therefore race, and the later rename
// wins outright.
//
// The test asserts the outcome is at least never CORRUPT — the file is always
// one of the two coherent results, never a partial write — which is what the
// atomic rename buys. It is documentation of a known limitation, not a claim
// that concurrent pruning is safe.
func TestConcurrentDisableIsLostUpdateProne(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    skills:
      - keeper
      - gone-a
      - gone-b
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var wg sync.WaitGroup
	for _, name := range []string{"gone-a", "gone-b"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_ = DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "main", ir.KindSkill, name)
		}(name)
	}
	wg.Wait()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("config is not loadable after two concurrent edits: %v\nconfig:\n%s", err, data)
	}
	if cfg == nil || len(cfg.Registries) != 1 {
		t.Fatalf("registry entry lost by a concurrent edit:\n%s", data)
	}
	if !strings.Contains(string(data), "keeper") {
		t.Errorf("the untouched selection was lost by a concurrent edit:\n%s", data)
	}
	// At least one of the two prunes must have landed; both landing is the
	// happy interleaving, one landing is the documented lost update.
	got := 0
	for _, name := range []string{"gone-a", "gone-b"} {
		if !stringSliceContains(cfg.Registries[0].SelectorForKind(ir.KindSkill).Include, name) {
			got++
		}
	}
	if got == 0 {
		t.Errorf("neither concurrent prune landed:\n%s", data)
	}
	t.Logf("concurrent prunes landed: %d/2 (1 is the documented lost update)\nconfig:\n%s", got, data)
}
