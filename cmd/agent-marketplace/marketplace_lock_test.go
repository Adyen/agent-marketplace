package main

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const (
	testMarketplaceDigest  = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testMarketplaceArchive = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestExactMarketplaceViewCopiesLockAuthorityWithoutPersistingIt(t *testing.T) {
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.Marketplace{{
		URL: "https://market.example", InstanceID: "mp", Resources: []lockfile.MarketplaceResource{{
			SourceID: "src", Kind: "skill", Name: "review", Revision: lockfile.Digest(testMarketplaceDigest), Acquisition: lockfile.AcquisitionMirror,
			ArchiveDigest: lockfile.Digest(testMarketplaceArchive), Direct: true,
		}},
	}}}
	input := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "https://market.example/", InstanceID: "mp", Source: config.RepoCommitted,
		Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: "review", InstalledName: "local-review"}},
	}}}

	got, err := exactMarketplaceView(map[config.Source]lockfile.Document{config.RepoCommitted: doc})(input)
	if err != nil {
		t.Fatal(err)
	}
	pin := got.Marketplaces[0].Resources[0]
	if pin.Revision != testMarketplaceDigest || pin.Digest != testMarketplaceDigest || pin.ArchiveDigest != testMarketplaceArchive || pin.InstalledName != "local-review" {
		t.Fatalf("exact pin = %#v", pin)
	}
	if input.Marketplaces[0].Resources[0].Revision != "" || input.Marketplaces[0].Resources[0].ArchiveDigest != "" {
		t.Fatalf("input config was mutated: %#v", input.Marketplaces[0].Resources[0])
	}
}

func TestMarketplaceUpgradeRevisionAdvanceAndIdenticalRevisionFreshness(t *testing.T) {
	oldRecord := lockfile.MarketplaceResource{
		SourceID: "src", Kind: "skill", Name: "review", Revision: lockfile.Digest(testMarketplaceDigest),
		Acquisition: lockfile.AcquisitionMirror, ArchiveDigest: lockfile.Digest(testMarketplaceArchive), Direct: true,
	}
	current := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.Marketplace{{
		URL: "https://market.example/api", InstanceID: "mp", Resources: []lockfile.MarketplaceResource{oldRecord},
	}}}
	requirements := []lockfile.MarketplaceRequirements{{URL: "https://market.example/api/", InstanceID: "mp", Resources: []lockfile.MarketplaceRequirement{{SourceID: "src", Kind: "skill", Name: "review"}}}}

	for _, tc := range []struct {
		name      string
		revision  lockfile.Digest
		archive   lockfile.Digest
		wantFresh bool
	}{
		{name: "actual revision advance", revision: lockfile.Digest("sha256:" + strings.Repeat("c", 64)), archive: lockfile.Digest("sha256:" + strings.Repeat("d", 64))},
		{name: "identical resolved revision", revision: lockfile.Digest(testMarketplaceDigest), archive: lockfile.Digest(testMarketplaceArchive), wantFresh: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := lockfile.Plan(lockfile.Requirements{Scope: lockfile.ScopeRepo, Marketplaces: requirements}, &current, lockfile.OperationUpgrade)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Desired.Marketplaces) != 0 || len(plan.Acquisitions) != 1 {
				t.Fatalf("upgrade plan retained replacement target: %#v", plan)
			}
			incoming := lockfile.Marketplace{URL: "HTTPS://MARKET.EXAMPLE/api/", InstanceID: "mp", Resources: []lockfile.MarketplaceResource{{
				SourceID: "src", Kind: "skill", Name: "review", Revision: tc.revision,
				Acquisition: lockfile.AcquisitionMirror, ArchiveDigest: tc.archive, Direct: true,
			}}}
			if err := mergeMarketplaceEndpoint(&plan.Desired, incoming); err != nil {
				t.Fatal(err)
			}
			if err := lockfile.ReconcileMarketplaceGraph(&plan.Desired, requirements); err != nil {
				t.Fatal(err)
			}
			desired, err := lockfile.Normalize(plan.Desired)
			if err != nil {
				t.Fatal(err)
			}
			if got := desired.Marketplaces[0].Resources[0].Revision; got != tc.revision {
				t.Fatalf("revision = %q, want %q", got, tc.revision)
			}
			fresh, err := marketplaceLockFresh(lockfile.ScopeRepo, &current, desired)
			if err != nil {
				t.Fatal(err)
			}
			if fresh != tc.wantFresh {
				t.Fatalf("fresh = %v, want %v", fresh, tc.wantFresh)
			}
		})
	}
}

func TestMarketplaceIndexOnlyLockUsesTypedCommitAndRuntimeUsesRawCommit(t *testing.T) {
	prepared := &marketplace.PreparedInstall{
		Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin: config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", Digest: testMarketplaceDigest,
			Acquisition: config.MarketplaceAcquisitionIndexOnly, SourceCommit: "0123456789abcdef0123456789abcdef01234567", SourceManifestDigest: testMarketplaceArchive},
		Revision: store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{ContentDigest: store.RevisionDigest(testMarketplaceDigest)}},
	}
	record, err := marketplaceResourceRecord(prepared, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.SourceCommit.String() != "sha1:0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("typed lock commit = %q", record.SourceCommit.String())
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo, Marketplaces: []lockfile.Marketplace{{URL: "https://market.example", InstanceID: "mp", Resources: []lockfile.MarketplaceResource{record}}}}
	input := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: "review"}}}}}
	got, err := exactMarketplaceView(map[config.Source]lockfile.Document{config.RepoCommitted: doc})(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Marketplaces[0].Resources[0].SourceCommit != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("runtime commit = %q", got.Marketplaces[0].Resources[0].SourceCommit)
	}
}
