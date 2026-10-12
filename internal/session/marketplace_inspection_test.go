package session

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	inspectiontest "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceinspection "github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestMarketplaceInspectableSkill(t *testing.T) {
	fixture := inspectiontest.NewFixture(t, nil)
	row := marketplaceInspectionTestRow(fixture)
	if !MarketplaceInspectableSkill(row) {
		t.Fatal("exact uninstalled Marketplace skill is not inspectable")
	}

	row.Artifact.Path = "/verified/staged/skill"
	if MarketplaceInspectableSkill(row) {
		t.Fatal("installed/staged Marketplace skill is inspectable")
	}
	row = marketplaceInspectionTestRow(fixture)
	row.Artifact.IsMarketplace = false
	if MarketplaceInspectableSkill(row) {
		t.Fatal("local skill is inspectable through Marketplace inspection")
	}
	row = marketplaceInspectionTestRow(fixture)
	row.Artifact.Kind = ir.KindCommand
	if MarketplaceInspectableSkill(row) {
		t.Fatal("non-skill Marketplace resource is inspectable")
	}
	row = marketplaceInspectionTestRow(fixture)
	row.MarketplaceDiscovery = nil
	if MarketplaceInspectableSkill(row) {
		t.Fatal("row without discovery provenance is inspectable")
	}
	row = marketplaceInspectionTestRow(fixture)
	row.MarketplaceDiscovery.Item.Availability = mpapi.AvailabilityBlocked
	if MarketplaceInspectableSkill(row) {
		t.Fatal("non-installable Marketplace item is inspectable")
	}
}

func TestOpenMarketplaceInspectionLoadsVerifiedManifestAndReadsOnlyMembers(t *testing.T) {
	fixture := inspectiontest.NewFixture(t, nil)
	inspection, err := OpenMarketplaceInspection(context.Background(), marketplaceInspectionTestRow(fixture))
	if err != nil {
		t.Fatalf("open verified inspection: %v", err)
	}
	manifest := inspection.Manifest()
	if manifest.Coordinate.Registry != "registry-1" || manifest.Coordinate.Kind != ir.KindSkill.String() || manifest.Coordinate.Name != "grill" || manifest.RevisionDigest != fixture.Revision || manifest.PrimaryPath != "SKILL.md" {
		t.Fatalf("verified manifest identity = %+v", manifest)
	}
	for _, requestPath := range fixture.Log() {
		if strings.Contains(requestPath, "/inspection/content") {
			t.Fatalf("opening inspection fetched content eagerly: %v", fixture.Log())
		}
	}

	manifest.Files[0].Path = "mutated"
	if got := inspection.Manifest().Files[0].Path; got == "mutated" {
		t.Fatal("Manifest returned the caller-owned files slice")
	}

	primary, err := inspection.ReadFile(context.Background(), "SKILL.md")
	if err != nil || !bytes.Equal(primary, fixture.Primary) {
		t.Fatalf("read primary = %q (err=%v), want verified bytes", primary, err)
	}
	primary[0] = 'x'
	primaryAgain, err := inspection.ReadFile(context.Background(), "SKILL.md")
	if err != nil || !bytes.Equal(primaryAgain, fixture.Primary) {
		t.Fatalf("primary bytes were not copied: %q (err=%v)", primaryAgain, err)
	}
	support, err := inspection.ReadFile(context.Background(), "support/notes.md")
	if err != nil || !bytes.Equal(support, fixture.Support["notes.md"]) {
		t.Fatalf("read support = %q (err=%v), want verified bytes", support, err)
	}

	beforeUnsupported := fixture.Log()
	if _, err := inspection.ReadFile(context.Background(), "support/logo.png"); !errors.Is(err, ErrMarketplaceInspectionUnsupported) {
		t.Fatalf("read unsupported image error = %v", err)
	}
	if after := fixture.Log(); len(after) != len(beforeUnsupported) {
		t.Fatalf("unsupported file issued a network request: before=%v after=%v", beforeUnsupported, after)
	}
	beforeMissing := fixture.Log()
	if _, err := inspection.ReadFile(context.Background(), "support/../private.txt"); err == nil {
		t.Fatal("non-member path was accepted")
	}
	if after := fixture.Log(); len(after) != len(beforeMissing) {
		t.Fatalf("non-member path issued a network request: before=%v after=%v", beforeMissing, after)
	}
}

func TestOpenMarketplaceInspectionRejectsUnverifiedProvenance(t *testing.T) {
	cases := []struct {
		name   string
		mutate inspectiontest.Mutate
	}{
		{
			name: "well-known instance mismatch",
			mutate: func(_ *inspectiontest.Fixture, wellKnown *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
				wellKnown.InstanceID = "another-instance"
			},
		},
		{
			name: "missing inspection assessment",
			mutate: func(_ *inspectiontest.Fixture, _ *mpapi.WellKnown, revision *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
				revision.Assessment.InspectionManifestDigest = ""
			},
		},
		{
			name: "revision coordinate mismatch",
			mutate: func(_ *inspectiontest.Fixture, _ *mpapi.WellKnown, revision *store.AssessedResourceRevision, _ *[]byte, _ map[string][]byte) {
				revision.Coordinate.Name = "different"
			},
		},
		{
			name: "manifest payload digest mismatch",
			mutate: func(_ *inspectiontest.Fixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, payload *[]byte, _ map[string][]byte) {
				*payload = append(append([]byte(nil), *payload...), ' ')
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := inspectiontest.NewFixture(t, tc.mutate)
			if _, err := OpenMarketplaceInspection(context.Background(), marketplaceInspectionTestRow(fixture)); err == nil {
				t.Fatal("unverified inspection provenance was accepted")
			}
			for _, requestPath := range fixture.Log() {
				if strings.Contains(requestPath, "/inspection/content") {
					t.Fatalf("provenance failure fetched content: %v", fixture.Log())
				}
			}
		})
	}
	t.Run("discovery content digest mismatch", func(t *testing.T) {
		fixture := inspectiontest.NewFixture(t, nil)
		row := marketplaceInspectionTestRow(fixture)
		row.MarketplaceDiscovery.Item.ContentDigest = "sha256:" + strings.Repeat("e", 64)
		if _, err := OpenMarketplaceInspection(context.Background(), row); err == nil {
			t.Fatal("manifest accepted a different discovery content digest")
		}
		for _, requestPath := range fixture.Log() {
			if strings.Contains(requestPath, "/inspection/content") {
				t.Fatalf("digest mismatch fetched content: %v", fixture.Log())
			}
		}
	})
}

func TestMarketplaceInspectionReadFileRejectsCorruptBodies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		primary bool
	}{
		{name: "corrupt primary", primary: true},
		{name: "corrupt support"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := inspectiontest.NewFixture(t, func(f *inspectiontest.Fixture, _ *mpapi.WellKnown, _ *store.AssessedResourceRevision, _ *[]byte, bodies map[string][]byte) {
				if tc.primary {
					f.Primary = []byte("corrupt primary")
					return
				}
				bodies[fixtureDigest(f.Support["notes.md"])] = []byte("corrupt support")
			})
			inspection, err := OpenMarketplaceInspection(context.Background(), marketplaceInspectionTestRow(fixture))
			if err != nil {
				t.Fatalf("open verified manifest before body read: %v", err)
			}
			path := "support/notes.md"
			if tc.primary {
				path = "SKILL.md"
			}
			if data, err := inspection.ReadFile(context.Background(), path); err == nil || data != nil {
				t.Fatalf("corrupt body returned data=%q err=%v", data, err)
			}
		})
	}
}

func fixtureDigest(data []byte) string {
	return marketplaceinspection.Digest(data)
}

func marketplaceInspectionTestRow(fixture *inspectiontest.Fixture) Row {
	endpoint := fixture.Endpoint()
	item := fixture.Item()
	return Row{
		Artifact: resolver.ResolvedArtifact{
			Entry:                   catalog.Entry{Kind: ir.KindSkill, Name: item.Name, Description: item.Description},
			IsMarketplace:           true,
			MarketplaceURL:          endpoint.URL,
			MarketplaceInstanceID:   endpoint.InstanceID,
			MarketplaceRegistryID:   item.RegistryID,
			MarketplaceResourceName: item.Name,
		},
		IsAvailable:  true,
		RegistryURL:  endpoint.URL,
		RegistryName: MarketplaceEndpointDisplay(endpoint) + "/" + item.RegistrySlug,
		MarketplaceDiscovery: &MarketplaceDiscovery{
			Endpoint: endpoint,
			Item:     item,
		},
	}
}
