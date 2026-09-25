package marketplacelock

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestMergePinsToV4DropsEdgesToRemovedCollections(t *testing.T) {
	removedDigest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	sharedDigest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	memberDigest := lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	archiveDigest := lockfile.Digest("sha256:" + strings.Repeat("d", 64))
	removedEdge := lockfile.MarketplaceSelectionLock{RegistryID: "registry-id", Name: "removed", Revision: removedDigest}
	sharedEdge := lockfile.MarketplaceSelectionLock{RegistryID: "registry-id", Name: "shared", Revision: sharedDigest}
	collection := func(name string, digest lockfile.Digest) lockfile.MarketplaceResourceLock {
		return lockfile.MarketplaceResourceLock{RegistryName: "tools", RegistryID: "registry-id", Kind: "collection", Name: name, Revision: string(digest), ContentDigest: digest, CollectionManifestDigest: digest, Acquisition: lockfile.MarketplaceAcquisitionMirror}
	}
	for _, tc := range []struct {
		name, source, requests string
		selectedBy             []lockfile.MarketplaceSelectionLock
		wantEdges              []lockfile.MarketplaceSelectionLock
		wantCollections        []string
	}{
		{name: "direct member", source: "https://market.example", requests: "tools/skill/member", selectedBy: []lockfile.MarketplaceSelectionLock{removedEdge}},
		{name: "shared member", source: "https://market.example", requests: "tools/collection/shared", selectedBy: []lockfile.MarketplaceSelectionLock{removedEdge, sharedEdge}, wantEdges: []lockfile.MarketplaceSelectionLock{sharedEdge}, wantCollections: []string{"shared"}},
		{name: "pruned endpoint direct member", source: "https://other.example", requests: "tools/skill/member", selectedBy: []lockfile.MarketplaceSelectionLock{removedEdge}},
		{name: "pruned endpoint shared member", source: "https://other.example", requests: "tools/collection/shared", selectedBy: []lockfile.MarketplaceSelectionLock{removedEdge, sharedEdge}, wantEdges: []lockfile.MarketplaceSelectionLock{sharedEdge}, wantCollections: []string{"shared"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v4, err := config.ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  primary: {marketplace: https://market.example}\n  secondary: {marketplace: https://other.example}\nresources:\n  primary: {include: [" + tc.requests + "]}\n  secondary: {include: [" + tc.requests + "]}\n"))
			if err != nil {
				t.Fatal(err)
			}
			resources := []lockfile.MarketplaceResourceLock{
				collection("removed", removedDigest), collection("shared", sharedDigest),
				{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "member", Revision: string(memberDigest), ContentDigest: memberDigest, ArchiveDigest: archiveDigest, Acquisition: lockfile.MarketplaceAcquisitionMirror, SelectedBy: tc.selectedBy},
			}
			current := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser, Marketplaces: []lockfile.MarketplaceLock{
				{URL: "https://market.example", InstanceID: "primary-id", RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "registry-id"}}, Resources: resources},
				{URL: "https://other.example", InstanceID: "secondary-id", RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "registry-id"}}, Resources: resources},
			}}
			if _, err := lockfile.Normalize(current); err != nil {
				t.Fatalf("input lock is invalid: %v", err)
			}
			merged, err := MergePinsToV4(v4, current, config.MarketplaceEntry{URL: tc.source, InstanceID: map[string]string{"https://market.example": "primary-id", "https://other.example": "secondary-id"}[tc.source]}, nil, nil)
			if err != nil {
				t.Fatalf("merge rejected retained member: %v", err)
			}
			if _, err := lockfile.Normalize(merged); err != nil {
				t.Fatalf("merged lock is invalid: %v", err)
			}
			if len(merged.Marketplaces) != 2 {
				t.Fatalf("endpoint count = %d, want 2", len(merged.Marketplaces))
			}
			for _, endpoint := range merged.Marketplaces {
				var collections []string
				var member *lockfile.MarketplaceResourceLock
				for i := range endpoint.Resources {
					record := &endpoint.Resources[i]
					if record.Kind == "collection" {
						collections = append(collections, record.Name)
					} else if record.Kind == "skill" && record.Name == "member" {
						member = record
					}
				}
				if member == nil || !reflect.DeepEqual(collections, tc.wantCollections) || !reflect.DeepEqual(member.SelectedBy, tc.wantEdges) {
					t.Fatalf("endpoint %s retained resources = %#v, want collections %v and member edges %v", endpoint.URL, endpoint.Resources, tc.wantCollections, tc.wantEdges)
				}
			}
		})
	}
}

func TestReachableResourcesRequiresMatchingCollectionDigestForEdges(t *testing.T) {
	currentDigest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	oldDigest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	v4 := &config.V4Config{
		Sources: map[string]config.V4Source{"hosted": {Kind: config.V4SourceMarketplace}},
		Requests: []config.V4ResourceRequest{
			{SourceName: "hosted", Registry: "tools", Kind: "collection", Name: "bundle"},
			{SourceName: "hosted", Registry: "tools", Kind: "skill", Name: "member"},
		},
	}
	resources := []lockfile.MarketplaceResourceLock{
		{RegistryName: "tools", RegistryID: "registry-id", Kind: "collection", Name: "bundle", ContentDigest: currentDigest},
		{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "member", SelectedBy: []lockfile.MarketplaceSelectionLock{{RegistryID: "registry-id", Name: "bundle", Revision: oldDigest}}},
	}
	retained, err := reachableResources(v4, "hosted", resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 2 || retained[1].Name != "member" || len(retained[1].SelectedBy) != 0 {
		t.Fatalf("stale collection digest survived: %#v", retained)
	}
	if len(resources[1].SelectedBy) != 1 {
		t.Fatal("reachability mutated the input")
	}
}

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
