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

func TestExclusionOnlyMarketplaceSourceIsNotLocked(t *testing.T) {
	v4, err := config.ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources:
  company: {marketplace: https://company.example}
  hosted: {marketplace: https://hosted.example}
resources:
  company: {exclude: [tools/skill/review]}
`))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Provisional(v4, lockfile.ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != "https://hosted.example" {
		t.Fatalf("provisional lock = %#v, want only the acquiring source", doc.Marketplaces)
	}
	delete(v4.Sources, "hosted")
	if HasMarketplaceSource(v4) {
		t.Fatal("exclusion-only source must not require a Marketplace lock")
	}
}

func TestHasRequiredError(t *testing.T) {
	required := &config.V4ValidationError{Code: "v4.marketplace-lock-required"}
	if !HasRequiredError([]error{errors.New("other"), errors.Join(errors.New("wrapped"), required)}) {
		t.Fatal("nested Marketplace lock requirement was not detected")
	}
	mismatch := &config.V4ValidationError{Code: "v4.marketplace-lock-mismatch"}
	if !HasMismatchError([]error{errors.New("other"), errors.Join(errors.New("wrapped"), mismatch)}) {
		t.Fatal("nested Marketplace lock mismatch was not detected")
	}
	if HasRequiredError([]error{mismatch}) {
		t.Fatal("Marketplace lock mismatch was classified as a lock requirement")
	}
	if HasRequiredError([]error{errors.New("other")}) || HasMismatchError([]error{errors.New("other")}) {
		t.Fatal("unrelated error was classified as a Marketplace lock error")
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

func TestMergeRejectsRegistryBindingChange(t *testing.T) {
	v4, err := config.ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: https://hosted.example}
resources:
  hosted: {include: [tools/skill/review]}
`))
	if err != nil {
		t.Fatal(err)
	}
	digest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	current := lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL:              "https://hosted.example",
			InstanceID:       "instance",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "old-id"}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName: "tools", RegistryID: "new-id", Kind: "skill", Name: "review",
				Revision: string(digest), ContentDigest: digest, ArchiveDigest: digest,
				Acquisition: lockfile.MarketplaceAcquisitionMirror,
			}},
		}},
	}
	entry := config.MarketplaceEntry{URL: "https://hosted.example", InstanceID: "instance", Source: config.UserPersonal}

	_, err = MergePinsToV4(v4, current, entry, nil, nil)
	var mismatch *config.V4ValidationError
	if !errors.As(err, &mismatch) || mismatch.Code != "v4.marketplace-lock-mismatch" {
		t.Fatalf("rebound registry accepted: %v", err)
	}
	if !strings.Contains(mismatch.Msg, "old-id") || !strings.Contains(mismatch.Msg, "new-id") {
		t.Fatalf("binding error = %q", mismatch.Msg)
	}
}
