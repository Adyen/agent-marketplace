package config

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMarketplaceExclusionMatchesExactOptionalPins(t *testing.T) {
	entry := MarketplaceEntry{URL: "https://market.example", Source: Organization}
	pin := MarketplaceResourcePin{RegistrySlug: "tools", RegistryID: "source-id", Kind: "skill", Name: "review"}
	exclude := MarketplaceExclusion{URL: entry.URL, Locator: "tools/skill/review", Source: UserPersonal}
	if !exclude.Matches(entry, pin) || !exclude.Matches(MarketplaceEntry{URL: "https://MARKET.example/", Source: Organization}, pin) {
		t.Fatal("matching optional organization pin was not excluded")
	}
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		own := MarketplaceEntry{URL: entry.URL, Source: source}
		if !(MarketplaceExclusion{URL: entry.URL, Locator: exclude.Locator, Source: source}).Matches(own, pin) {
			t.Fatalf("%v exclusion did not remove its own collection member", source)
		}
	}
	if !(MarketplaceExclusion{URL: entry.URL, Locator: "tools/mcp/hub", Source: UserPersonal}).Matches(entry, MarketplaceResourcePin{RegistrySlug: "tools", Kind: "mcpserver", Name: "hub", InstalledName: "renamed-hub"}) {
		t.Fatal("MCP exclusion must use the v4 kind and original Marketplace name")
	}
	for _, tc := range []struct {
		name    string
		entry   MarketplaceEntry
		pin     MarketplaceResourcePin
		exclude MarketplaceExclusion
	}{
		{name: "required", entry: MarketplaceEntry{URL: entry.URL, Source: Organization, Required: true}, pin: pin, exclude: exclude},
		{name: "other endpoint", entry: MarketplaceEntry{URL: "https://other.example", Source: Organization}, pin: pin, exclude: exclude},
		{name: "other registry", entry: entry, pin: MarketplaceResourcePin{RegistrySlug: "other", Kind: "skill", Name: "review"}, exclude: exclude},
		{name: "other kind", entry: entry, pin: MarketplaceResourcePin{RegistrySlug: "tools", Kind: "command", Name: "review"}, exclude: exclude},
		{name: "other name", entry: entry, pin: MarketplaceResourcePin{RegistrySlug: "tools", Kind: "skill", Name: "audit"}, exclude: exclude},
		{name: "repo override", entry: entry, pin: pin, exclude: MarketplaceExclusion{URL: entry.URL, Locator: exclude.Locator, Source: RepoCommitted}},
		{name: "direct user pin", entry: MarketplaceEntry{URL: entry.URL, Source: UserPersonal}, pin: MarketplaceResourcePin{RegistrySlug: "tools", Kind: "skill", Name: "review", Direct: true}, exclude: exclude},
		{name: "other layer collection member", entry: MarketplaceEntry{URL: entry.URL, Source: RepoCommitted}, pin: pin, exclude: exclude},
		{name: "required own collection member", entry: MarketplaceEntry{URL: entry.URL, Source: UserPersonal, Required: true}, pin: pin, exclude: exclude},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.exclude.Matches(tc.entry, tc.pin) {
				t.Fatal("exclusion escaped its exact optional pin")
			}
		})
	}
}

// spec: marketplace-api/marketplace-entries-are-validated
func TestValidateMarketplaceEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []MarketplaceEntry
		wantErr string
	}{
		{
			name: "valid",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "01JEXAMPLE",
				Resources: []MarketplaceResourcePin{{
					RegistryID: "01JSOURCE",
					Kind:       "skill",
					Name:       "review",
					Revision:   "v1.0.0",
					Digest:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				}},
			}},
		},
		{
			name:    "missing url",
			entries: []MarketplaceEntry{{InstanceID: "x"}},
			wantErr: "marketplaces[0].url is required",
		},
		{
			name:    "missing instance_id",
			entries: []MarketplaceEntry{{URL: "https://market.example"}},
			wantErr: "marketplaces[0].instance_id is required",
		},
		{
			name:    "path traversal",
			entries: []MarketplaceEntry{{URL: "https://market.example/../evil", InstanceID: "x"}},
			wantErr: "path traversal",
		},
		{
			name: "invalid pin kind",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "x",
				Resources:  []MarketplaceResourcePin{{Kind: "rule"}},
			}},
			wantErr: "not a valid marketplace kind",
		},
		{
			name: "invalid digest",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "x",
				Resources:  []MarketplaceResourcePin{{RegistryID: "s", Kind: "skill", Name: "n", Revision: "r", Digest: "bad"}},
			}},
			wantErr: "digest",
		},
		{
			name: "invalid acquisition",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "x",
				Resources:  []MarketplaceResourcePin{{RegistryID: "s", Kind: "skill", Name: "n", Revision: "r", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000", Acquisition: "clone"}},
			}},
			wantErr: "acquisition",
		},
		{
			name: "development acquisition alias",
			entries: []MarketplaceEntry{{
				URL:        "https://market.example",
				InstanceID: "x",
				Resources:  []MarketplaceResourcePin{{RegistryID: "s", Kind: "skill", Name: "n", Revision: "r", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000000", Acquisition: "index"}},
			}},
			wantErr: "acquisition",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateMarketplaceEntries(tc.entries)
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			found := false
			for _, err := range errs {
				if strings.Contains(err.Error(), tc.wantErr) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, errs)
			}
		})
	}
}

func TestValidateMarketplaceEntriesRejectsDuplicateAndReservedAliases(t *testing.T) {
	entries := []MarketplaceEntry{
		{URL: "https://one.example", InstanceID: "one", Alias: "shared"},
		{URL: "https://two.example", InstanceID: "two", Alias: "shared"},
		{URL: "https://three.example", InstanceID: "three", Alias: "all"},
	}
	errs := ValidateMarketplaceEntries(entries)
	if len(errs) < 2 {
		t.Fatalf("expected duplicate and reserved alias errors, got %v", errs)
	}
	joined := errors.Join(errs...)
	if !strings.Contains(joined.Error(), "duplicates") {
		t.Fatalf("duplicate alias error missing: %v", joined)
	}
	if !strings.Contains(joined.Error(), "reserved") {
		t.Fatalf("reserved alias error missing: %v", joined)
	}
}

func TestValidateMarketplaceEntriesAllowsLayeredAliasForSameIdentity(t *testing.T) {
	entries := []MarketplaceEntry{
		{URL: "HTTPS://MARKET.example/base/", InstanceID: "instance", Alias: "public", Source: RepoCommitted},
		{URL: "https://market.example/base", InstanceID: "instance", Alias: "public", Source: RepoPersonal},
	}
	if errs := ValidateMarketplaceEntries(entries); len(errs) != 0 {
		t.Fatalf("same-identity layered alias rejected: %v", errs)
	}
	entries[1].Source = RepoCommitted
	if errs := ValidateMarketplaceEntries(entries); len(errs) != 0 {
		t.Fatalf("same-identity same-source alias rejected: %v", errs)
	}
	entries[0].Source, entries[1].Source = SourceUnset, SourceUnset
	if errs := ValidateMarketplaceEntries(entries); len(errs) == 0 {
		t.Fatal("same-file duplicate alias accepted")
	}
}

// spec: marketplace-protocols/same-endpoint-alias-with-different-instance-ids
func TestValidateMarketplaceEntriesRejectsAliasWithDifferentInstanceIDs(t *testing.T) {
	organization := MarketplaceEntry{URL: "https://market.example", InstanceID: "bootstrap", Alias: "org", Source: Organization}
	for _, tc := range []struct {
		name  string
		layer MarketplaceEntry
		unset bool
	}{
		{"same URL with a verified instance", MarketplaceEntry{URL: "https://market.example/", InstanceID: "behavior-marketplace", Alias: "org", Source: UserPersonal}, false},
		{"another URL", MarketplaceEntry{URL: "https://other.example", InstanceID: "behavior-marketplace", Alias: "org", Source: UserPersonal}, false},
		{"same file", MarketplaceEntry{URL: "https://market.example", InstanceID: "behavior-marketplace", Alias: "org"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := []MarketplaceEntry{organization, tc.layer}
			if tc.unset {
				entries[0].Source = SourceUnset
			}
			errs := ValidateMarketplaceEntries(entries)
			if len(errs) == 0 || !strings.Contains(errors.Join(errs...).Error(), "duplicates") {
				t.Fatalf("ValidateMarketplaceEntries(%q) = %v, want duplicate alias", tc.name, errs)
			}
		})
	}
	verified := []MarketplaceEntry{
		{URL: "https://market.example", InstanceID: "behavior-marketplace", Alias: "org", Source: Organization},
		{URL: "https://market.example", InstanceID: "other-instance", Alias: "org", Source: UserPersonal},
	}
	if errs := ValidateMarketplaceEntries(verified); len(errs) == 0 {
		t.Fatal("two verified instances for one alias were accepted")
	}
}

func TestValidateMarketplaceEntriesRejectsUnsafeIdentityAndEndpointFields(t *testing.T) {
	for _, entry := range []MarketplaceEntry{
		{URL: "http://market.example?token=secret", InstanceID: "instance"},
		{URL: "http://user:pass@market.example", InstanceID: "instance"},
		{URL: "http://market.example/%2funsafe", InstanceID: "instance"},
		{URL: "http://market.example", InstanceID: " instance"},
		{URL: "http://market.example", InstanceID: "instance/id"},
		{URL: "http://market.example", InstanceID: "instance", Alias: "alias\n"},
		{URL: "http://market.example", InstanceID: "instance", Resources: []MarketplaceResourcePin{{RegistryID: "source", RegistrySlug: "source/slug", Kind: "skill", Name: "name", Revision: "v1", Digest: testMarketplaceDigest}}},
	} {
		if errs := ValidateMarketplaceEntries([]MarketplaceEntry{entry}); len(errs) == 0 {
			t.Fatalf("ValidateMarketplaceEntries accepted unsafe entry %#v", entry)
		}
	}
	if errs := ValidateMarketplaceEntries([]MarketplaceEntry{{URL: "HTTPS://MARKET.example/base/", InstanceID: "instance", Alias: "public"}}); len(errs) != 0 {
		t.Fatalf("valid endpoint rejected: %v", errs)
	}
}

// spec: marketplace-api/marketplace-url-allowlist
func TestValidateMarketplaceURLs(t *testing.T) {
	entries := []MarketplaceEntry{
		{URL: "https://market.example.com/skills"},
		{URL: "https://evil.example.com/skills"},
	}

	if err := ValidateMarketplaceURLs([]MarketplaceEntry{{URL: "HTTPS://MARKET.example.com/"}}, []string{`^https://market\.example\.com$`}); err != nil {
		t.Fatalf("canonical URL should match allowlist: %v", err)
	}

	err := ValidateMarketplaceURLs(entries, []string{`market\.example\.com`})
	if err == nil {
		t.Fatalf("expected blocked URL error, got nil")
	}
	var blocked *MarketplaceURLBlockedError
	if !errors.As(err, &blocked) || len(blocked.BlockedURLs) != 1 || !strings.Contains(blocked.BlockedURLs[0], "evil.example.com") {
		t.Fatalf("error = %#v, want *MarketplaceURLBlockedError listing only evil.example.com", err)
	}

	if err := ValidateMarketplaceURLs(entries, nil); err != nil {
		t.Fatalf("nil allowlist should permit all URLs: %v", err)
	}

	if err := ValidateMarketplaceURLs(entries, []string{}); err == nil {
		t.Fatalf("empty allowlist should deny all URLs")
	}
}

func TestMarketplaceConfigRoundTrip(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"factory"},
		Marketplaces: []MarketplaceEntry{{
			URL:        "https://market.example",
			InstanceID: "01JEXAMPLE",
			Resources: []MarketplaceResourcePin{{
				RegistryID:  "01JSOURCE",
				Kind:        "skill",
				Name:        "review",
				Revision:    "v1.0.0",
				Digest:      "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Acquisition: "mirror",
			}},
		}},
		AllowedMarketplaces: &[]string{`market\.example\.com`},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Config
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Marketplaces) != 1 || got.Marketplaces[0].InstanceID != cfg.Marketplaces[0].InstanceID {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	if got.AllowedMarketplaces == nil || !slices.Equal(*got.AllowedMarketplaces, *cfg.AllowedMarketplaces) {
		t.Fatalf("AllowedMarketplaces round-trip mismatch: %v", got.AllowedMarketplaces)
	}
}

func TestMarketplaceMerge(t *testing.T) {
	base := &Config{
		AllowedMarketplaces: &[]string{`base\.example`},
		Marketplaces: []MarketplaceEntry{
			{URL: "https://base.example", InstanceID: "base"},
		},
	}
	repo := &Config{
		AllowedMarketplaces: &[]string{`repo\.example`},
		Marketplaces: []MarketplaceEntry{
			{URL: "https://repo.example", InstanceID: "repo"},
		},
	}

	merged := Merge(base, repo)

	// First non-nil wins for allowlist.
	if merged.AllowedMarketplaces == nil || !slices.Equal(*merged.AllowedMarketplaces, []string{`base\.example`}) {
		t.Fatalf("AllowedMarketplaces should be first-non-nil-wins, got %v", merged.AllowedMarketplaces)
	}

	// Marketplaces accumulate.
	if len(merged.Marketplaces) != 2 {
		t.Fatalf("expected 2 merged marketplaces, got %d", len(merged.Marketplaces))
	}
}

// spec: marketplace-api/layered-config-wiring
func TestMergeLayersKeepsMarketplaceProvenanceAndFirstAllowlist(t *testing.T) {
	orgAllow := []string{`org\.example`}
	userAllow := []string{`.*`}
	merged := MergeLayers([]Layer{
		{Source: Organization, Config: &Config{AllowedMarketplaces: &orgAllow, Marketplaces: []MarketplaceEntry{{URL: "https://org.example", InstanceID: "org"}}}},
		{Source: UserPersonal, Config: &Config{AllowedMarketplaces: &userAllow, Marketplaces: []MarketplaceEntry{{URL: "https://user.example", InstanceID: "user"}}}},
		{Source: RepoCommitted, Config: &Config{Marketplaces: []MarketplaceEntry{{URL: "https://repo.example", InstanceID: "repo"}}}},
	})

	var got []string
	for _, entry := range merged.Marketplaces {
		got = append(got, entry.InstanceID+"@"+entry.Source.String())
	}
	want := []string{"org@" + Organization.String(), "user@" + UserPersonal.String(), "repo@" + RepoCommitted.String()}
	if !slices.Equal(got, want) {
		t.Fatalf("merged marketplaces = %v, want %v", got, want)
	}
	if merged.AllowedMarketplaces == nil || !slices.Equal(*merged.AllowedMarketplaces, orgAllow) {
		t.Fatalf("AllowedMarketplaces = %v, want organization allowlist %v", merged.AllowedMarketplaces, orgAllow)
	}
}

func TestLocalMarketplaceValidationCoversAllEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	writeFile(t, path, "marketplaces:\n  - url: https://one.example\n    instance_id: one\n    alias: shared\n  - url: https://two.example\n    instance_id: two\n    alias: shared\n")
	if _, err := LoadLocalConfigFromPath(path); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("local Marketplace alias validation error = %v", err)
	}
}

func TestMarketplaceLayerAllowlist(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.local.yaml")
	writeFile(t, path, "marketplaces:\n  - url: https://local.example\n    instance_id: 01JLOCAL\n    resources: []\n")

	localCfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadLocalConfigFromPath: %v", err)
	}
	if len(localCfg.Marketplaces) != 1 {
		t.Fatalf("expected 1 marketplace in local config, got %d", len(localCfg.Marketplaces))
	}
}
