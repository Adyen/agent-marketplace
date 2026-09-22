package marketplacelock

import (
	"errors"
	"os"
	"path/filepath"
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
