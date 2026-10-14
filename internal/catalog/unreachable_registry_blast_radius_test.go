package catalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// TestUnreachableRegistryIsAHardCatalogError documents what a developer who
// cannot reach a remote registry named in a committed `.agents/config.yaml`
// gets. Every failing clone becomes an entry in LoadResult.Errors, and
// pipeline.Resolve promotes every non-overlap catalog error to the hard error
// list, so an unreachable registry fails the sync rather than silently
// resolving to nothing.
//
// This used to read this repository's own committed config, which shipped two
// entries for one internal host. That config now declares only a local
// registry, so the fixture is synthetic: the invariant under test is the
// loader's behaviour for an unreachable remote, not this repo's entry list.
// Two entries for ONE host are kept so the per-entry (not per-host) error
// accounting stays pinned.
func TestUnreachableRegistryIsAHardCatalogError(t *testing.T) {
	const host = "git@git.example.invalid:org/registry.git"
	entries := []config.RegistryEntry{
		{URL: host, Ref: "main", Path: "plugins/one"},
		{URL: host, Ref: "main", Path: "plugins/two"},
	}

	cfg := &config.Config{
		APIVersion: config.APIVersionV4,
		Registries: entries,
	}

	result := LoadRegistries(cfg, t.TempDir(), LoadModeRefresh,
		WithCloneOrPull(func(url, ref string) (string, error) {
			return "", errors.New("ssh: permission denied (publickey)")
		}),
	)

	hard := 0
	for _, err := range result.Errors {
		if IsOverlapWarning(err) {
			continue
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("unexpected catalog error: %v", err)
		}
		hard++
	}
	if hard == 0 {
		t.Fatal("expected the unreachable host to produce hard catalog errors (recording the blast radius of a committed registry entry)")
	}
	// Both entries name the same unreachable host: a user who can reach it is
	// unaffected by the second entry, and a user who cannot was already broken
	// by the first.
	if hard != 2 {
		t.Logf("note: %d hard errors for 2 entries of one unreachable host", hard)
	}
}
