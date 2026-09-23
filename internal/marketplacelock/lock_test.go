package marketplacelock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestOrganizationProvisionalResolvesRelativePath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir("organization", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("organization", "config.yaml"), []byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: https://marketplace.example}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	layer, data, missing, err := OrganizationProvisional(filepath.Join("organization", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !missing {
		t.Fatal("expected missing organization Marketplace lock")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	wantLock := filepath.Join(canonicalRoot, "organization", "lock.yaml")
	if layer.LockPath != wantLock {
		t.Fatalf("lock path = %q, want %q", layer.LockPath, wantLock)
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Scope != lockfile.ScopeOrganization || len(doc.Marketplaces) != 1 {
		t.Fatalf("provisional lock = %#v", doc)
	}
}

func TestPruneToV4PreservesReachableCollectionEvidence(t *testing.T) {
	collectionDigest := lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	leafDigest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	archiveDigest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	current := lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "mp",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "src"}},
			Resources: []lockfile.MarketplaceResourceLock{
				{RegistryName: "tools", RegistryID: "src", Kind: "collection", Name: "bundle", Revision: string(collectionDigest), ContentDigest: collectionDigest, CollectionManifestDigest: collectionDigest, Acquisition: lockfile.MarketplaceAcquisitionMirror},
				{RegistryName: "tools", RegistryID: "src", Kind: "skill", Name: "review", Revision: string(leafDigest), ContentDigest: leafDigest, ArchiveDigest: archiveDigest, Acquisition: lockfile.MarketplaceAcquisitionMirror, SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "src", Name: "bundle", Revision: collectionDigest}}},
			},
		}},
	}
	encoded, err := lockfile.Encode(current)
	if err != nil {
		t.Fatal(err)
	}
	current, err = lockfile.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		resources string
		want      int
		wantEdges int
	}{
		{name: "collection", resources: "resources: {hosted: {include: [tools/collection/bundle]}}\n", want: 2, wantEdges: 1},
		{name: "direct leaf", resources: "resources: {hosted: {include: [tools/skill/review]}}\n", want: 1, wantEdges: 0},
		{name: "endpoint only", want: 0, wantEdges: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			v4, err := config.ParseV4Config([]byte("apiVersion: agent-marketplace/v4\nsources: {hosted: {marketplace: https://market.example}}\n"+test.resources), "", true)
			if err != nil {
				t.Fatal(err)
			}
			pruned, err := PruneToV4(v4, current)
			if err != nil {
				t.Fatal(err)
			}
			if len(pruned.Marketplaces) != 1 || len(pruned.Marketplaces[0].Resources) != test.want {
				t.Fatalf("pruned evidence = %#v", pruned)
			}
			for _, resource := range pruned.Marketplaces[0].Resources {
				if resource.Kind == "skill" && len(resource.SelectedBy) != test.wantEdges {
					t.Fatalf("retained leaf has stale collection edges: %#v", resource)
				}
			}
		})
	}
}

func TestHasRequiredError(t *testing.T) {
	required := &config.V4ValidationError{Code: "v4.marketplace-lock-required"}
	if !HasRequiredError([]error{errors.New("other"), errors.Join(errors.New("wrapped"), required)}) {
		t.Fatal("nested Marketplace lock requirement was not detected")
	}
	if HasRequiredError([]error{errors.New("other")}) {
		t.Fatal("unrelated error was classified as a Marketplace lock requirement")
	}
}

func TestMarketplaceResourceBudgetUsesConservativeEncodedMinimum(t *testing.T) {
	digest := lockfile.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")
	resource := lockfile.MarketplaceResourceLock{
		RegistryName:  "x",
		RegistryID:    "x",
		Kind:          "hook",
		Name:          "x",
		Revision:      "x",
		ContentDigest: digest,
		ArchiveDigest: digest,
		Acquisition:   lockfile.MarketplaceAcquisitionMirror,
	}
	doc := lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeOrganization,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL:              "https://x.example",
			InstanceID:       "x",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "x", RegistryID: "x"}},
			Resources:        []lockfile.MarketplaceResourceLock{resource},
		}},
	}
	one, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	resource.Name = "y"
	doc.Marketplaces[0].Resources = append(doc.Marketplaces[0].Resources, resource)
	two, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if delta := len(two) - len(one); delta < minimumMarketplaceResourceLockBytes {
		t.Fatalf("minimum encoded Marketplace resource = %d bytes, budget assumes at least %d", delta, minimumMarketplaceResourceLockBytes)
	}
}
