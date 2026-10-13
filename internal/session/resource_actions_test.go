package session

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// spec: session-interactions/guarded-submission
func TestRowToggleEligibilityUsesStableDenialOrder(t *testing.T) {
	base := selectedMarketplaceTestRow("mp", "source", "review")
	blocked := base
	blocked.MarketplaceDiscovery = &MarketplaceDiscovery{Item: mpapi.ResourceListItem{Availability: mpapi.AvailabilityBlocked}}
	required := base
	required.Artifact.Required = true
	collectionOnly := base
	collectionOnly.MarketplaceCollectionOnly = true
	organizationCollection := base
	organizationCollection.Artifact.Source = config.Organization
	organizationCollection.MarketplaceCollectionOnly = true

	cases := []struct {
		name          string
		row           Row
		busy          bool
		configInvalid bool
		want          RowToggleDenial
	}{
		{name: "busy wins", row: Row{IsMalformed: true, PendingVerification: true, MarketplaceDiscovery: blocked.MarketplaceDiscovery}, busy: true, configInvalid: true, want: RowToggleBusy},
		{name: "invalid configuration wins", row: Row{IsMalformed: true, PendingVerification: true}, configInvalid: true, want: RowToggleConfigInvalid},
		{name: "malformed wins", row: Row{IsMalformed: true, PendingVerification: true}, want: RowToggleMalformed},
		{name: "verification pending wins", row: Row{PendingVerification: true, MarketplaceDiscovery: blocked.MarketplaceDiscovery}, want: RowTogglePendingVerification},
		{name: "marketplace block wins", row: Row{Artifact: required.Artifact, MarketplaceDiscovery: blocked.MarketplaceDiscovery}, want: RowToggleMarketplaceBlocked},
		{name: "required wins", row: Row{Artifact: required.Artifact, MarketplaceCollectionOnly: true}, want: RowToggleRequired},
		{name: "collection-only is blocked", row: collectionOnly, want: RowToggleCollectionOnly},
		{name: "overridable organization collection is allowed", row: organizationCollection, want: RowToggleEligible},
		{name: "ordinary row is allowed", row: base, want: RowToggleEligible},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := RowToggleEligibility(test.row, test.busy, test.configInvalid); got != test.want {
				t.Fatalf("RowToggleEligibility() = %v, want %v", got, test.want)
			}
		})
	}
}

// spec: session-interactions/guarded-submission
func TestUserMarketplaceActionIsUserOnlyAndRequiresDirectSelection(t *testing.T) {
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "mp"}
	revision := "sha256:" + strings.Repeat("a", 64)
	discovery := discoveryTestRow(endpoint, "src", "skill", "review", revision, revision, 0, nil)
	if action, err := UserMarketplaceAction(discovery); err != nil || action != "install" {
		t.Fatalf("available Marketplace action = %q, %v", action, err)
	}

	user := selectedMarketplaceTestRow("mp", "src", "review")
	user.Artifact.Source = config.UserPersonal
	if action, err := UserMarketplaceAction(user); err != nil || action != "remove" {
		t.Fatalf("legacy direct user pin action = %q, %v", action, err)
	}

	directWithCollection := user
	selections := make(map[config.Source]MarketplaceSelection, len(user.MarketplaceSelections))
	for source, selection := range user.MarketplaceSelections {
		selections[source] = selection
	}
	selection := selections[config.UserPersonal]
	selection.Pin.Direct = true
	selection.Pin.RegistrySlug = "source"
	selection.Pin.SelectedBy = config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "collections", Name: "onboarding", Revision: "sha256:collection"}})
	selections[config.UserPersonal] = selection
	directWithCollection.MarketplaceSelections = selections
	if action, err := UserMarketplaceAction(directWithCollection); err != nil || action != "remove" {
		t.Fatalf("direct user pin with collection selector action = %q, %v", action, err)
	}

	inheritedOnly := directWithCollection
	inheritedSelections := make(map[config.Source]MarketplaceSelection, len(selections))
	for source, selection := range selections {
		inheritedSelections[source] = selection
	}
	selection = inheritedSelections[config.UserPersonal]
	selection.Pin.Direct = false
	inheritedSelections[config.UserPersonal] = selection
	inheritedOnly.MarketplaceSelections = inheritedSelections
	inheritedOnly.MarketplaceCollectionOnly = true
	if action, err := UserMarketplaceAction(inheritedOnly); err != nil || action != "none" {
		t.Fatalf("inherited-only user selection action = %q, %v", action, err)
	}

	organization := user
	organization.Artifact.Source = config.Organization
	organization.MarketplaceSelections = map[config.Source]MarketplaceSelection{
		config.Organization: {
			Entry: endpoint,
			Pin:   config.MarketplaceResourcePin{RegistrySlug: "source", RegistryID: "src", Kind: "skill", Name: "review"},
		},
	}
	if action, err := UserMarketplaceAction(organization); err != nil || action != "exclude" {
		t.Fatalf("optional organization resource action = %q, %v", action, err)
	}
	organization.ExcludedByUser = true
	if action, err := UserMarketplaceAction(organization); err != nil || action != "restore" {
		t.Fatalf("user-excluded organization resource action = %q, %v", action, err)
	}
	organization.Artifact.Required = true
	if action, err := UserMarketplaceAction(organization); err != nil || action != "none" {
		t.Fatalf("required organization resource action = %q, %v", action, err)
	}

	local := user
	local.Artifact.IsMarketplace = false
	if action, err := UserMarketplaceAction(local); err != nil || action != "none" {
		t.Fatalf("local resource action = %q, %v", action, err)
	}

	directCollection := user
	directCollection.Artifact.Kind = ir.KindCollection
	collectionSelections := make(map[config.Source]MarketplaceSelection, len(user.MarketplaceSelections))
	for source, candidate := range user.MarketplaceSelections {
		collectionSelections[source] = candidate
	}
	collectionPin := collectionSelections[config.UserPersonal]
	collectionPin.Pin.Kind = ir.KindCollection.String()
	collectionPin.Pin.Name = "onboarding"
	collectionPin.Pin.Direct = true
	collectionPin.Pin.RegistrySlug = "source"
	collectionPin.Pin.SelectedBy = config.MarketplaceSelectionsFrom([]config.MarketplaceSelection{{RegistryID: "collections", Name: "bundle", Revision: "sha256:" + strings.Repeat("c", 64)}})
	collectionSelections[config.UserPersonal] = collectionPin
	directCollection.MarketplaceSelections = collectionSelections
	if action, err := UserMarketplaceAction(directCollection); err != nil || action != "remove" {
		t.Fatalf("direct user collection pin action = %q, %v", action, err)
	}

	organizationCollection := organization
	organizationCollection.Artifact.Required = false
	organizationCollection.Artifact.Kind = ir.KindCollection
	organizationCollection.ExcludedByUser = false
	organizationCollectionPin := organizationCollection.MarketplaceSelections[config.Organization]
	organizationCollectionPin.Pin.Kind = ir.KindCollection.String()
	organizationCollectionPin.Pin.Name = "onboarding"
	organizationCollection.MarketplaceSelections = map[config.Source]MarketplaceSelection{config.Organization: organizationCollectionPin}
	if action, err := UserMarketplaceAction(organizationCollection); err != nil || action != "none" {
		t.Fatalf("organization collection exclusion action = %q, %v", action, err)
	}
}

// spec: session-interactions/guarded-submission
func TestUserDiscoveryInstallOptionsAnchorBothRootsAtHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	endpoint := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "mp"}
	revision := "sha256:" + strings.Repeat("b", 64)
	row := discoveryTestRow(endpoint, "src", "skill", "review", revision, revision, 0, nil)

	options, err := UserDiscoveryInstallOptions(home, row)
	if err != nil {
		t.Fatalf("UserDiscoveryInstallOptions(home): %v", err)
	}
	if options.RepoRoot != home || options.LocalConfigDir != home {
		t.Fatalf("install roots = repo %q/local %q, want home %q for both", options.RepoRoot, options.LocalConfigDir, home)
	}
	if options.Scope != pipeline.ScopeUser || !options.ScopeSpecified {
		t.Fatalf("install scope = %v/specified:%t, want explicit user scope", options.Scope, options.ScopeSpecified)
	}
	if options.Marketplace != "mp" || options.ExactMarketplaceLocator == nil || options.ExactMarketplaceLocator.Registry != "src" || options.ExactMarketplaceLocator.Name != "review" {
		t.Fatalf("exact install locator = %#v", options.ExactMarketplaceLocator)
	}
	if _, err := UserDiscoveryInstallOptions(t.TempDir(), row); err == nil {
		t.Fatal("non-home root accepted for user Marketplace install")
	}
}
