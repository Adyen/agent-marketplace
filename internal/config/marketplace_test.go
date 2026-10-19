package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
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

// spec: config-layering/marketplace-entries-are-validated
func TestValidateMarketplaceEntries(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"missing URL", "sources:\n  public: {marketplace: ''}\n", "v4.source-value"},
		{"path traversal", "sources:\n  public: {marketplace: https://market.example/../evil}\n", "v4.marketplace-url"},
		{"invalid kind", "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [tools/rule/review]\n", "v4.locator"},
		{"invalid locator", "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [skill/review]\n", "v4.locator"},
	} {
		t.Run("authored/"+tc.name, func(t *testing.T) {
			_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\n" + tc.body))
			requireMarketplaceV4Code(t, err, tc.code)
		})
	}
	// Runtime evidence validation remains independent of authored consumer schemas.
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

// spec: config-layering/duplicate-and-reserved-marketplace-aliases-are-rejected
func TestValidateMarketplaceEntriesRejectsDuplicateAndReservedAliases(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{"sources:\n  shared: {marketplace: https://one.example}\n  shared: {marketplace: https://two.example}\n", "v4.duplicate-key"},
		{"sources:\n  all: {marketplace: https://three.example}\n", "v4.source-name"},
	} {
		_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\n" + tc.body))
		requireMarketplaceV4Code(t, err, tc.code)
	}
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

// spec: config-layering/layered-alias-accepted-for-one-endpoint-identity
func TestValidateMarketplaceEntriesAllowsLayeredAliasForSameIdentity(t *testing.T) {
	repo := marketplaceLayerFixture(t, RepoCommitted, "public", "HTTPS://MARKET.example/base/", "instance", "")
	local := marketplaceLayerFixture(t, RepoPersonal, "public", "https://market.example/base", "instance", "")
	merged := MergeLayers([]Layer{{Source: RepoCommitted, Config: repo}, {Source: RepoPersonal, Config: local}})
	if len(merged.Marketplaces) != 2 || merged.Marketplaces[0].Alias != "public" || merged.Marketplaces[1].Alias != "public" || merged.Marketplaces[0].Source != RepoCommitted || merged.Marketplaces[1].Source != RepoPersonal {
		t.Fatalf("layered v4 alias/provenance = %#v", merged.Marketplaces)
	}
	if errs := ValidateMarketplaceEntries(merged.Marketplaces); len(errs) != 0 {
		t.Fatalf("verified layered v4 aliases rejected: %v", errs)
	}
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: https://market.example}\n  public: {marketplace: https://market.example}\n"))
	requireMarketplaceV4Code(t, err, "v4.duplicate-key")
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

// spec: configuration-intent/same-endpoint-alias-with-different-instance-ids
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

// spec: config-layering/unsafe-identity-and-endpoint-fields-are-rejected
// spec: config-layering/v4-root-rejects-unsafe-marketplace-source-urls
func TestValidateMarketplaceEntriesRejectsUnsafeIdentityAndEndpointFields(t *testing.T) {
	for _, endpoint := range []string{"http://market.example?token=secret", "http://user:pass@market.example", "http://market.example/%2funsafe", "http://market.example#fragment"} {
		_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: '" + endpoint + "'}\n"))
		requireMarketplaceV4Code(t, err, "v4.marketplace-url")
	}
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  \"bad\\nname\": {marketplace: https://market.example}\n"))
	requireMarketplaceV4Code(t, err, "v4.source-name")
	if _, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: 'HTTPS://MARKET.example/base/'}\n")); err != nil {
		t.Fatalf("valid normalized source rejected: %v", err)
	}
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

// spec: config-layering/marketplace-url-allowlist
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

// spec: config-layering/v4-roots-declare-marketplace-sources-and-resources
func TestMarketplaceConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, `harnesses: [factory]
sources:
  public: {marketplace: https://market.example}
resources:
  public:
    include:
      - source-slug/skill/review: {as: chosen-review, revision: sha256:1111111111111111111111111111111111111111111111111111111111111111}
allowed_marketplaces: ['market\.example']
`)
	// Loading the authored tree must not require acquisition or create a lock.
	cfg, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIVersion != APIVersionV4 || cfg.Sources["public"].CanonicalURL != "https://market.example" || !slices.Equal(marketplaceRequestLocators(cfg), []string{"source-slug/skill/review"}) || cfg.Requests[0].Options.As != "chosen-review" || cfg.Requests[0].Options.Revision != testMarketplaceRevision || cfg.AllowedMarketplaces == nil || !slices.Equal(*cfg.AllowedMarketplaces, []string{`market\.example`}) {
		t.Fatalf("authored v4 source/selection/policy = %#v", cfg)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("authored load created/read an acquisition lock: %v", err)
	}
	// Round-trip the actual authored YAML tree, never serialize the runtime model.
	var node yaml.Node
	if err := yaml.Unmarshal(marketplaceState(t, path).data, &node); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(&node)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseV4Config(data, path, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sources["public"].Kind != V4SourceMarketplace || len(got.Requests) != 1 || got.Requests[0].Options.As != "chosen-review" || got.Requests[0].Options.Revision != testMarketplaceRevision || got.AllowedMarketplaces == nil || !slices.Equal(*got.AllowedMarketplaces, []string{`market\.example`}) {
		t.Fatalf("v4 round-trip = %#v", got)
	}
	_, err = got.ToLegacyConfig(repoLayer)
	requireMarketplaceV4Code(t, err, "v4.marketplace-lock-required")
	doc := marketplaceTestEvidence()
	runtime, err := got.ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if pin := runtime.Marketplaces[0].Resources[0]; pin.RegistryID != "source-one" || pin.InstalledName != "chosen-review" || pin.Digest != testMarketplaceDigest || !pin.Direct {
		t.Fatalf("lock-bound round-trip = %#v", pin)
	}
	doc.Scope = lockfile.ScopeUser
	_, err = got.ToLegacyConfigWithLock(repoLayer, &doc)
	requireMarketplaceV4Code(t, err, "v4.marketplace-lock-mismatch")
}

// spec: config-layering/v4-root-rejects-legacy-installation-keys
func TestMarketplaceV4RootRefusesLegacyInstallationKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [source-slug/skill/review]\nmarketplaces: []\n")
	before := marketplaceState(t, path)
	_, err := LoadV4ConfigFromPath(path)
	requireMarketplaceV4Code(t, err, "v4.legacy-key")
	_, _, err = commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin()))
	requireMarketplaceV4Code(t, err, "v4.legacy-key")
	requireMarketplaceUnchanged(t, path, before)
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("refusal created lock: %v", err)
	}
}

func marketplaceLayerFixture(t *testing.T, source Source, name, endpoint, instance, policy string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	if source == RepoPersonal {
		path = filepath.Join(filepath.Dir(path), "config.local.yaml")
	}
	writeMarketplaceV4(t, path, "sources:\n  "+name+": {marketplace: "+endpoint+"}\n"+policy)
	v4, err := LoadV4ConfigIfPresent(path, source)
	if err != nil {
		t.Fatal(err)
	}
	kind, _, err := v4LayerForSource(source, path)
	if err != nil {
		t.Fatal(err)
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.Scope(source.LockScope()), Marketplaces: []lockfile.MarketplaceLock{{URL: endpoint, InstanceID: instance}}}
	cfg, err := v4.ToLegacyConfigWithLock(kind, &doc)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMarketplaceMerge(t *testing.T) {
	base := marketplaceLayerFixture(t, UserPersonal, "base", "https://base.example", "base", "allowed_marketplaces: ['base\\.example']\n")
	repo := marketplaceLayerFixture(t, RepoCommitted, "repo", "https://repo.example", "repo", "allowed_marketplaces: ['repo\\.example']\n")

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

// spec: config-layering/layered-merge-retains-per-layer-provenance-and-first-set-allowlist
func TestMergeLayersKeepsMarketplaceProvenanceAndFirstAllowlist(t *testing.T) {
	orgAllow := []string{`org\.example`}
	merged := MergeLayers([]Layer{
		{Source: Organization, Config: marketplaceLayerFixture(t, Organization, "org", "https://org.example", "org", "allowed_marketplaces: ['org\\.example']\n")},
		{Source: UserPersonal, Config: marketplaceLayerFixture(t, UserPersonal, "user", "https://user.example", "user", "allowed_marketplaces: ['.*']\n")},
		{Source: RepoCommitted, Config: marketplaceLayerFixture(t, RepoCommitted, "repo", "https://repo.example", "repo", "")},
		{Source: RepoPersonal, Config: marketplaceLayerFixture(t, RepoPersonal, "local", "https://local.example", "local", "")},
	})

	var got []string
	for _, entry := range merged.Marketplaces {
		got = append(got, entry.InstanceID+"@"+entry.Source.String())
	}
	want := []string{"org@" + Organization.String(), "user@" + UserPersonal.String(), "repo@" + RepoCommitted.String(), "local@" + RepoPersonal.String()}
	if !slices.Equal(got, want) {
		t.Fatalf("merged marketplaces = %v, want %v", got, want)
	}
	if merged.AllowedMarketplaces == nil || !slices.Equal(*merged.AllowedMarketplaces, orgAllow) {
		t.Fatalf("AllowedMarketplaces = %v, want organization allowlist %v", merged.AllowedMarketplaces, orgAllow)
	}
}

// spec: config-layering/personal-local-duplicate-aliases-are-rejected-at-load
func TestLocalMarketplaceValidationCoversAllEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	writeMarketplaceV4(t, path, "sources:\n  shared: {marketplace: https://one.example}\n  shared: {marketplace: https://two.example}\n")
	before := marketplaceState(t, path)
	_, err := LoadLocalConfigFromPath(path)
	requireMarketplaceV4Code(t, err, "v4.duplicate-key")
	requireMarketplaceUnchanged(t, path, before)
}

func TestMarketplaceLayerAllowlist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	writeMarketplaceV4(t, path, "sources:\n  local: {marketplace: https://local.example}\n")
	v4, err := LoadV4ConfigIfPresent(path, RepoPersonal)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Sources) != 1 || v4.Sources["local"].CanonicalURL != "https://local.example" {
		t.Fatalf("personal-local registration = %#v", v4.Sources)
	}
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeLocal, Marketplaces: []lockfile.MarketplaceLock{{URL: "https://local.example", InstanceID: "01JLOCAL"}}}
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "lock.local.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	localCfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(localCfg.Marketplaces) != 1 || localCfg.Marketplaces[0].Source != RepoPersonal || localCfg.Marketplaces[0].InstanceID != "01JLOCAL" {
		t.Fatalf("lock-bound local config = %#v", localCfg.Marketplaces)
	}
	writeMarketplaceV4(t, path, "sources:\n  local: {marketplace: https://local.example}\nallowed_marketplaces: ['.*']\n")
	_, err = LoadV4ConfigIfPresent(path, RepoPersonal)
	requireMarketplaceV4Code(t, err, "v4.layer-field")
}
