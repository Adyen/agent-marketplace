package config

import (
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

const v4PlaceMarketplaceYAML = `apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://MARKET.example/}}
resources:
  m:
    include:
      - tools/invented-app%2Fmod/sample
`

func TestV4PlaceMarketplaceLockHydration(t *testing.T) {
	cfg, err := ParseV4([]byte(v4PlaceMarketplaceYAML))
	if err != nil {
		t.Fatalf("ParseV4 scoped Marketplace request: %v", err)
	}
	if len(cfg.Requests) != 1 {
		t.Fatalf("requests = %#v, want one exact request", cfg.Requests)
	}
	request := cfg.Requests[0]
	if request.Locator != "tools/invented-app%2Fmod/sample" || request.Registry != "tools" ||
		request.Kind != "invented-app/mod" || request.Name != "sample" ||
		request.MarketplaceKind() != "invented-app/mod" || request.Options.HasAs {
		t.Fatalf("scoped Marketplace request = %#v", request)
	}

	identity := targetprofile.Identity{
		TargetID: "invented-app", SourceID: "builtin:agent-marketplace", RevisionDigest: v4sha("d"),
	}
	binding := targetprofile.Binding{
		PackageContentDigest: v4sha("a"), DeliveryMappingDigest: v4sha("b"),
		ProfileDigest: v4sha("d"), ProfileIdentity: identity,
	}
	resource := v4LockResource("tools", "tools-id", "invented-app/mod", "sample", v4sha("a"), v4sha("a"))
	resource.ArchiveDigest = lockfile.Digest(v4sha("b"))
	resource.AssessmentDigest = lockfile.Digest(v4sha("c"))
	resource.HostPackageIdentity = &binding
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        []lockfile.MarketplaceResourceLock{resource},
		}},
	})
	if err != nil {
		t.Fatalf("normalize place Marketplace lock: %v", err)
	}
	runtime, err := cfg.ToLegacyConfigWithLock(userLayer, &doc)
	if err != nil {
		t.Fatalf("hydrate place Marketplace lock: %v", err)
	}
	if len(runtime.Marketplaces) != 1 || len(runtime.Marketplaces[0].Resources) != 1 {
		t.Fatalf("runtime Marketplace pins = %#v", runtime.Marketplaces)
	}
	pin := runtime.Marketplaces[0].Resources[0]
	if pin.Kind != "invented-app/mod" || pin.Name != "sample" || pin.Digest != v4sha("a") ||
		pin.LockArchiveDigest != v4sha("b") || pin.LockAssessmentDigest != v4sha("c") ||
		pin.LockHostPackageIdentity != binding {
		t.Fatalf("hydrated place pin = %#v", pin)
	}

	for _, layer := range []layerKind{repoLayer, localLayer} {
		badScopeLock := doc
		badScopeLock.Scope = lockfile.ScopeRepo
		_, err := cfg.ToLegacyConfigWithLock(layer, &badScopeLock)
		if err == nil || !strings.Contains(err.Error(), "marketplace.profile.invalid-scope") {
			t.Errorf("%s scoped request error = %v, want marketplace.profile.invalid-scope before lock validation", layer.name(), err)
		}
	}

	for _, locator := range []string{
		"tools/invented-app%2fmod/sample",
		"tools/invented-app%252Fmod/sample",
		"tools/invented-app/mod/sample",
	} {
		raw := strings.Replace(v4PlaceMarketplaceYAML, "tools/invented-app%2Fmod/sample", locator, 1)
		if _, err := ParseV4([]byte(raw)); err == nil {
			t.Errorf("ParseV4 accepted malformed scoped locator %q", locator)
		}
	}
	withAs := strings.Replace(v4PlaceMarketplaceYAML, "tools/invented-app%2Fmod/sample", "tools/invented-app%2Fmod/sample: {as: renamed}", 1)
	if _, err := ParseV4([]byte(withAs)); err == nil || !strings.Contains(err.Error(), "v4.option-combination") {
		t.Errorf("ParseV4 scoped as override error = %v, want v4.option-combination", err)
	}

	exclusionYAML := v4PlaceMarketplaceYAML + `    exclude:
      - tools/invented-app%2Fmod/*
`
	excluded, err := ParseV4([]byte(exclusionYAML))
	if err != nil {
		t.Fatalf("ParseV4 scoped wildcard exclusion: %v", err)
	}
	if len(excluded.Groups["m"].Include) != 1 || len(excluded.Requests) != 0 || len(excluded.Groups["m"].Exclude) != 1 ||
		excluded.Groups["m"].Exclude[0] != "tools/invented-app%2Fmod/*" {
		t.Fatalf("scoped wildcard exclusion requests/group = %#v / %#v", excluded.Requests, excluded.Groups["m"])
	}
	if _, err := excluded.ToLegacyConfigWithLock(repoLayer, nil); err == nil || !strings.Contains(err.Error(), "marketplace.profile.invalid-scope") {
		t.Errorf("excluded authored scoped request error = %v, want marketplace.profile.invalid-scope", err)
	}
	exactExclusion := MarketplaceExclusion{Locator: "tools/invented-app%2Fmod/sample", Source: UserPersonal}
	entry := MarketplaceEntry{Source: UserPersonal}
	member := MarketplaceResourcePin{RegistrySlug: "tools", Kind: "invented-app/mod", Name: "sample"}
	if !exactExclusion.Matches(entry, member) {
		t.Fatal("exact scoped kind exclusion did not match its own collection member")
	}
}

func TestV4VirtualProfileSelectionUsesItsOwnLockRecord(t *testing.T) {
	configYAML := v4PlaceMarketplaceYAML + "      - tools/target-profile/invented-app\n"
	cfg, err := ParseV4([]byte(configYAML))
	if err != nil {
		t.Fatalf("ParseV4 virtual profile request: %v", err)
	}
	if len(cfg.Requests) != 2 || cfg.Requests[1].Locator != "tools/target-profile/invented-app" ||
		cfg.Requests[1].Kind != "target-profile" || cfg.Requests[1].MarketplaceKind() != "target-profile" {
		t.Fatalf("virtual profile request = %#v", cfg.Requests)
	}

	identity := targetprofile.Identity{
		TargetID: "invented-app", SourceID: "builtin:agent-marketplace", RevisionDigest: v4sha("d"),
	}
	doc := v4VirtualProfileLock(t, identity, &identity)
	runtime, err := cfg.ToLegacyConfigWithLock(userLayer, &doc)
	if err != nil {
		t.Fatalf("hydrate profile and package locks: %v", err)
	}
	var profilePin, packagePin *MarketplaceResourcePin
	for i := range runtime.Marketplaces[0].Resources {
		pin := &runtime.Marketplaces[0].Resources[i]
		switch pin.Kind {
		case "target-profile":
			profilePin = pin
		case "invented-app/mod":
			packagePin = pin
		}
	}
	if profilePin == nil || packagePin == nil {
		t.Fatalf("hydrated Marketplace pins = %#v", runtime.Marketplaces[0].Resources)
	}
	if profilePin.LockProfileIdentity != identity ||
		packagePin.LockHostPackageIdentity.ProfileIdentity != identity {
		t.Fatalf("separate profile/package evidence: profile=%#v package=%#v", profilePin.LockProfileIdentity, packagePin.LockHostPackageIdentity)
	}
	selected, err := runtime.SelectedMarketplaceProfiles()
	if err != nil {
		t.Fatalf("SelectedMarketplaceProfiles: %v", err)
	}
	if selected["invented-app"] != identity || len(selected) != 1 {
		t.Fatalf("selected profiles = %#v, want exact locked identity", selected)
	}

	packageOnly, err := ParseV4([]byte(v4PlaceMarketplaceYAML))
	if err != nil {
		t.Fatal(err)
	}
	packageOnlyLock := v4VirtualProfileLock(t, targetprofile.Identity{}, &identity)
	packageOnlyRuntime, err := packageOnly.ToLegacyConfigWithLock(userLayer, &packageOnlyLock)
	if err != nil {
		t.Fatalf("hydrate package-only lock: %v", err)
	}
	_, err = packageOnlyRuntime.SelectedMarketplaceProfiles()
	if err == nil || !strings.Contains(err.Error(), "marketplace.profile.deselected-package") {
		t.Fatalf("package-only selection error = %v, want marketplace.profile.deselected-package", err)
	}

	otherIdentity := identity
	otherIdentity.RevisionDigest = v4sha("e")
	otherProfileLock := v4VirtualProfileLock(t, otherIdentity, &identity)
	otherProfileRuntime, err := cfg.ToLegacyConfigWithLock(userLayer, &otherProfileLock)
	if err != nil {
		t.Fatalf("hydrate different profile lock: %v", err)
	}
	_, err = otherProfileRuntime.SelectedMarketplaceProfiles()
	if err == nil || !strings.Contains(err.Error(), "marketplace.profile.revision-mismatch") {
		t.Fatalf("different profile digest error = %v, want marketplace.profile.revision-mismatch", err)
	}

	duplicateConfig := &Config{Marketplaces: []MarketplaceEntry{
		virtualProfilePinEntry(UserPersonal, identity),
		virtualProfilePinEntry(Organization, identity),
	}}
	duplicateSelection, err := duplicateConfig.SelectedMarketplaceProfiles()
	if err != nil || duplicateSelection["invented-app"] != identity || len(duplicateSelection) != 1 {
		t.Fatalf("identical cross-layer selection = %#v, %v", duplicateSelection, err)
	}
	conflictingIdentity := identity
	conflictingIdentity.SourceID = "git:https%3A%2F%2Fgit.example.com%2Ftools@main#profiles%2Finvented-app.yaml"
	conflictingIdentity.RevisionDigest = v4sha("e")
	conflictingConfig := &Config{Marketplaces: []MarketplaceEntry{
		virtualProfilePinEntry(UserPersonal, identity),
		virtualProfilePinEntry(Organization, conflictingIdentity),
	}}
	_, err = conflictingConfig.SelectedMarketplaceProfiles()
	if err == nil || !strings.Contains(err.Error(), "marketplace.profile.selection-ambiguous") ||
		!strings.Contains(err.Error(), identity.SourceID) || !strings.Contains(err.Error(), conflictingIdentity.SourceID) ||
		!strings.Contains(err.Error(), identity.RevisionDigest) || !strings.Contains(err.Error(), conflictingIdentity.RevisionDigest) ||
		!strings.Contains(err.Error(), UserPersonal.String()) || !strings.Contains(err.Error(), Organization.String()) {
		t.Fatalf("conflicting cross-layer selection error = %v", err)
	}

	virtualOnlyYAML := strings.Replace(v4PlaceMarketplaceYAML, "tools/invented-app%2Fmod/sample", "tools/target-profile/invented-app", 1)
	virtualOnly, err := ParseV4([]byte(virtualOnlyYAML))
	if err != nil {
		t.Fatal(err)
	}
	for _, layer := range []layerKind{repoLayer, localLayer} {
		badLock := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo}
		if _, err := virtualOnly.ToLegacyConfigWithLock(layer, &badLock); err == nil ||
			!strings.Contains(err.Error(), "marketplace.profile.invalid-scope") {
			t.Errorf("%s virtual request error = %v, want marketplace.profile.invalid-scope before lock validation", layer.name(), err)
		}
	}
	if _, err := (&Config{Marketplaces: []MarketplaceEntry{
		virtualProfilePinEntry(RepoCommitted, identity),
	}}).SelectedMarketplaceProfiles(); err == nil || !strings.Contains(err.Error(), "marketplace.profile.invalid-scope") {
		t.Errorf("direct repository profile pin error = %v", err)
	}
	if _, err := (&Config{Marketplaces: []MarketplaceEntry{{
		Source: RepoCommitted, Resources: []MarketplaceResourcePin{*packagePin},
	}}}).SelectedMarketplaceProfiles(); err == nil || !strings.Contains(err.Error(), "marketplace.profile.invalid-scope") {
		t.Errorf("direct repository package pin error = %v", err)
	}
}

func v4VirtualProfileLock(t *testing.T, profileIdentity targetprofile.Identity, packageIdentity *targetprofile.Identity) lockfile.Document {
	t.Helper()
	var resources []lockfile.MarketplaceResourceLock
	if profileIdentity != (targetprofile.Identity{}) {
		virtual := v4LockResource("tools", "tools-id", "target-profile", "invented-app", profileIdentity.RevisionDigest, profileIdentity.RevisionDigest)
		virtual.ProfileIdentity = &profileIdentity
		resources = append(resources, virtual)
	}
	if packageIdentity != nil {
		binding := targetprofile.Binding{
			PackageContentDigest: v4sha("a"), DeliveryMappingDigest: v4sha("b"),
			ProfileDigest: packageIdentity.RevisionDigest, ProfileIdentity: *packageIdentity,
		}
		packageResource := v4LockResource("tools", "tools-id", "invented-app/mod", "sample", v4sha("a"), v4sha("a"))
		packageResource.ArchiveDigest = lockfile.Digest(v4sha("b"))
		packageResource.AssessmentDigest = lockfile.Digest(v4sha("c"))
		packageResource.HostPackageIdentity = &binding
		resources = append(resources, packageResource)
	}
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
			Resources:        resources,
		}},
	})
	if err != nil {
		t.Fatalf("normalize virtual profile lock: %v", err)
	}
	return doc
}

func virtualProfilePinEntry(source Source, identity targetprofile.Identity) MarketplaceEntry {
	pin := MarketplaceResourcePin{
		Kind: "target-profile", Name: identity.TargetID, Revision: identity.RevisionDigest,
		Digest: identity.RevisionDigest, Direct: true, LockProfileIdentity: identity,
	}
	return MarketplaceEntry{Source: source, Resources: []MarketplaceResourcePin{pin}}
}
