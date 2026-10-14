package marketplacelock

import (
	"errors"
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

// spec: resource-prerequisites/updating-a-requiring-resource-prunes-its-stale-requires-held-pins
func TestReachableResourcesPrunesRequiresHeldRecordAfterParentUpdate(t *testing.T) {
	oldDigest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	newDigest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	childDigest := lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	v4 := &config.V4Config{
		Sources: map[string]config.V4Source{"hosted": {Kind: config.V4SourceMarketplace}},
		Requests: []config.V4ResourceRequest{
			{SourceName: "hosted", Registry: "tools", Kind: "skill", Name: "parent"},
		},
	}
	resources := []lockfile.MarketplaceResourceLock{
		{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "parent", ContentDigest: newDigest},
		{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "helper", ContentDigest: childDigest, SelectedBy: []lockfile.MarketplaceSelectionLock{
			{RegistryID: "registry-id", Kind: "skill", Name: "parent", Revision: oldDigest},
		}},
	}
	retained, err := reachableResources(v4, "hosted", resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 1 || retained[0].Name != "parent" {
		t.Fatalf("stale requires edge must drop the record it alone held, retained = %#v", retained)
	}
	for _, record := range retained {
		for _, edge := range record.SelectedBy {
			if edge.Kind != "" {
				t.Fatalf("rewrite must not add requires edges, got %#v", edge)
			}
		}
	}
}

// spec: resource-prerequisites/unrelated-lock-rewrites-keep-valid-requires-edges
func TestReachableResourcesKeepsValidRequiresEdges(t *testing.T) {
	parentDigest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	goneDigest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	childDigest := lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	validEdge := lockfile.MarketplaceSelectionLock{RegistryID: "registry-id", Kind: "skill", Name: "parent", Revision: parentDigest}
	v4 := &config.V4Config{
		Sources: map[string]config.V4Source{"hosted": {Kind: config.V4SourceMarketplace}},
		Requests: []config.V4ResourceRequest{
			{SourceName: "hosted", Registry: "tools", Kind: "skill", Name: "parent"},
			{SourceName: "hosted", Registry: "tools", Kind: "skill", Name: "sibling"},
		},
	}
	resources := []lockfile.MarketplaceResourceLock{
		{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "parent", ContentDigest: parentDigest},
		{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "helper", ContentDigest: childDigest, SelectedBy: []lockfile.MarketplaceSelectionLock{
			validEdge,
			{RegistryID: "registry-id", Kind: "skill", Name: "gone", Revision: goneDigest},
		}},
		{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "sibling", ContentDigest: childDigest},
	}
	retained, err := reachableResources(v4, "hosted", resources)
	if err != nil {
		t.Fatal(err)
	}
	var helper *lockfile.MarketplaceResourceLock
	for i := range retained {
		if retained[i].Name == "helper" {
			helper = &retained[i]
		}
	}
	if helper == nil {
		t.Fatalf("requires edge with a live parent must keep its record, retained = %#v", retained)
		return
	}
	if !reflect.DeepEqual(helper.SelectedBy, []lockfile.MarketplaceSelectionLock{validEdge}) {
		t.Fatalf("dangling requires edges must drop while valid ones stay, got %#v", helper.SelectedBy)
	}
}

func TestRemoveRequiresHeldRecordsDropsRecordAndDependents(t *testing.T) {
	parentDigest := lockfile.Digest("sha256:" + strings.Repeat("a", 64))
	childDigest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	grandchildDigest := lockfile.Digest("sha256:" + strings.Repeat("c", 64))
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser, Marketplaces: []lockfile.MarketplaceLock{{
		URL:        "https://market.example",
		InstanceID: "mp",
		Resources: []lockfile.MarketplaceResourceLock{
			{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "parent", ContentDigest: parentDigest},
			{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "helper", ContentDigest: childDigest, SelectedBy: []lockfile.MarketplaceSelectionLock{
				{RegistryID: "registry-id", Kind: "skill", Name: "parent", Revision: parentDigest},
			}},
			{RegistryName: "tools", RegistryID: "registry-id", Kind: "skill", Name: "grandchild", ContentDigest: grandchildDigest, SelectedBy: []lockfile.MarketplaceSelectionLock{
				{RegistryID: "registry-id", Kind: "skill", Name: "helper", Revision: childDigest},
			}},
		},
	}}}
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}

	stripped, err := RemoveRequiresHeldRecords(doc, entry, []config.MarketplaceResourcePin{
		{RegistryID: "registry-id", Kind: "skill", Name: "helper", Digest: string(childDigest)},
	})
	if err != nil {
		t.Fatal(err)
	}
	v4 := &config.V4Config{
		Sources: map[string]config.V4Source{"hosted": {Kind: config.V4SourceMarketplace, CanonicalURL: "https://market.example"}},
		Requests: []config.V4ResourceRequest{
			{SourceName: "hosted", Registry: "tools", Kind: "skill", Name: "parent"},
		},
	}
	retained, err := reachableResources(v4, "hosted", stripped.Marketplaces[0].Resources)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 1 || retained[0].Name != "parent" {
		t.Fatalf("removing the held record must also prune dependents of it, retained = %#v", retained)
	}
	if doc.Marketplaces[0].Resources[1].Name != "helper" {
		t.Fatal("strip mutated the input lock document")
	}
}
