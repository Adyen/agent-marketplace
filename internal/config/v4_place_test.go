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
