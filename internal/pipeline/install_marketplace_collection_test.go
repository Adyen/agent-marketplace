package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func prepareRollbackMarketplace(t *testing.T) (config.MarketplaceEntry, []config.MarketplaceResourcePin) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(home, "missing-org.yaml"))
	writeFileMode(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	return config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, layerPreparePins(t, fake, "skill", "review")
}

func mutateRollbackMarketplace(path string, entry config.MarketplaceEntry, pins []config.MarketplaceResourcePin, sync func() ([]error, error)) ([]error, error) {
	repo := filepath.Dir(filepath.Dir(path))
	return MutateMarketplaceLayer(context.Background(), repo, repo, path, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, pins)
	}, sync)
}

func TestEnsureMarketplacePinsAndSyncRollsBackWholeCollectionBatch(t *testing.T) {
	entry, _ := prepareRollbackMarketplace(t)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	fake.addCollection(t, registry, "bundle", []hccartifact.CollectionMemberEntry{{RegistryID: string(registry.ID), Kind: "skill", Name: "review", Revision: string(fake.resources["source/skill/review"].revision.ContentDigest)}})
	entry.URL, entry.InstanceID = fake.URL, fake.instanceID
	pins := layerPreparePins(t, fake, "collection", "bundle")
	repo := t.TempDir()
	path := filepath.Join(repo, ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v4\n# preserve whole batch\n")
	writeFileMode(t, path, string(original), 0o600)
	wantErr := errors.New("leaf materialization failed")
	called := false
	_, err := mutateRollbackMarketplace(path, entry, pins, func() ([]error, error) {
		called = true
		if data, err := os.ReadFile(path); err != nil || string(data) == string(original) {
			t.Fatalf("collection was not published before sync: %s, %v", data, err)
		}
		return nil, wantErr
	})
	if !called || !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, called = %t", err, called)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatalf("collection batch was not rolled back: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed batch left lock: %v", err)
	}
}
