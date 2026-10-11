package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func v4sha(letter string) string { return "sha256:" + strings.Repeat(letter, 64) }

func v4LockResource(registryName, registryID, kind, name, revision, content string) lockfile.MarketplaceResourceLock {
	return lockfile.MarketplaceResourceLock{
		RegistryName: registryName, RegistryID: registryID, Kind: kind, Name: name,
		Revision: revision, ContentDigest: lockfile.Digest(content),
		Acquisition: lockfile.MarketplaceAcquisitionMirror,
	}
}

// v4LockFixture builds a normalized v2 lock with two registry bindings, a
// directly requested collection, a derived mirror leaf, a derived index-only
// leaf, and a direct-and-derived artifact.
func v4LockFixture(t *testing.T, scope lockfile.Scope) lockfile.Document {
	t.Helper()
	onboarding := v4LockResource("main", "collections-id", "collection", "onboarding", v4sha("c"), v4sha("c"))
	onboarding.CollectionManifestDigest = lockfile.Digest(v4sha("c"))
	review := v4LockResource("tools", "tools-id", "skill", "review", v4sha("a"), v4sha("a"))
	review.ArchiveDigest = lockfile.Digest(v4sha("b"))
	review.SelectedBy = []lockfile.MarketplaceSelectionLock{
		{RegistryID: "collections-id", Name: "onboarding", Revision: lockfile.Digest(v4sha("c"))},
	}
	leaf := v4LockResource("tools-alias", "tools-id", "skill", "leaf", v4sha("e"), v4sha("e"))
	leaf.ArchiveDigest = lockfile.Digest(v4sha("f"))
	leaf.SelectedBy = []lockfile.MarketplaceSelectionLock{
		{RegistryID: "collections-id", Name: "onboarding", Revision: lockfile.Digest(v4sha("c"))},
	}
	idx := v4LockResource("tools", "tools-id", "skill", "idx", v4sha("7"), v4sha("7"))
	idx.ArchiveDigest = lockfile.Digest(v4sha("8"))
	idx.Acquisition = lockfile.MarketplaceAcquisitionIndexOnly
	idx.RegistryCommit = strings.Repeat("9", 40)
	idx.RegistryManifestDigest = lockfile.Digest(v4sha("6"))
	idx.SourcePath = "skills/idx"
	idx.SelectedBy = []lockfile.MarketplaceSelectionLock{
		{RegistryID: "collections-id", Name: "onboarding", Revision: lockfile.Digest(v4sha("c"))},
	}
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      scope,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL:        "https://market.example",
			InstanceID: "market-id",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{
				{Name: "main", RegistryID: "collections-id"},
				{Name: "tools", RegistryID: "tools-id"},
				{Name: "tools-alias", RegistryID: "tools-id"},
			},
			Resources: []lockfile.MarketplaceResourceLock{onboarding, review, leaf, idx},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

const v4MarketplaceConfigYAML = `apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://MARKET.example/}}
resources:
  m:
    include:
      - tools/skill/review: {as: local-review, revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
      - main/collection/onboarding
`

func v4LockConfig(t *testing.T) *V4Config {
	t.Helper()
	cfg, err := ParseV4([]byte(v4MarketplaceConfigYAML))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestV4MarketplaceLockHydration(t *testing.T) {
	doc := v4LockFixture(t, lockfile.ScopeRepo)
	cfg, err := v4LockConfig(t).ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces) != 1 {
		t.Fatalf("marketplaces = %#v", cfg.Marketplaces)
	}
	entry := cfg.Marketplaces[0]
	if entry.URL != "https://market.example" || entry.InstanceID != "market-id" || entry.Alias != "m" || entry.Source != RepoCommitted {
		t.Fatalf("entry = %#v", entry)
	}
	if len(entry.Resources) != 4 {
		t.Fatalf("pins = %#v", entry.Resources)
	}
	byName := map[string]MarketplaceResourcePin{}
	for _, pin := range entry.Resources {
		byName[pin.Kind+"/"+pin.Name] = pin
	}
	collection := byName["collection/onboarding"]
	if !collection.Direct || collection.RegistrySlug != "main" || collection.LockCollectionManifestDigest != v4sha("c") ||
		collection.LockArchiveDigest != "" || collection.LockSourcePath != "" {
		t.Fatalf("collection pin = %#v", collection)
	}
	review := byName["skill/review"]
	if !review.Direct || review.InstalledName != "local-review" || review.Revision != v4sha("a") ||
		review.Digest != v4sha("a") || review.LockArchiveDigest != v4sha("b") || review.RegistrySlug != "tools" ||
		review.LockCollectionManifestDigest != "" {
		t.Fatalf("review pin = %#v", review)
	}
	if len(review.SelectedBy.List()) != 1 {
		t.Fatalf("review selected_by = %#v", review.SelectedBy)
	}
	leaf := byName["skill/leaf"]
	if leaf.Direct || leaf.RegistrySlug != "tools-alias" || leaf.LockArchiveDigest != v4sha("f") {
		t.Fatalf("derived leaf = %#v", leaf)
	}
	idx := byName["skill/idx"]
	if idx.Direct || idx.Acquisition != MarketplaceAcquisitionIndexOnly || idx.RegistryCommit != strings.Repeat("9", 40) ||
		idx.RegistryManifestDigest != v4sha("6") || idx.LockSourcePath != "skills/idx" || idx.LockArchiveDigest != v4sha("8") {
		t.Fatalf("index-only pin = %#v", idx)
	}
}

func TestV4MarketplaceExplicitRequiredPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		required     bool
	}{
		{name: "omitted", policy: "", required: false},
		{name: "explicit false", policy: "    required: false\n", required: false},
		{name: "explicit true", policy: "    required: true\n", required: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseV4([]byte(v4MarketplaceConfigYAML + tc.policy))
			if err != nil {
				t.Fatal(err)
			}
			doc := v4LockFixture(t, lockfile.ScopeOrganization)
			out, err := cfg.ToLegacyConfigWithLock(organizationLayer, &doc)
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Marketplaces) != 1 || out.Marketplaces[0].Required != tc.required {
				t.Fatalf("Marketplace required policy = %#v, want %t", out.Marketplaces, tc.required)
			}
		})
	}
}

func TestV4MarketplaceExcludeOnlyUserConfigNeedsNoAcquisitionLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".agents")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, UserConfigShadowMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: agent-marketplace/v4
sources:
  company: {marketplace: https://market.example}
resources:
  company:
    exclude: [tools/skill/review]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("exclusion-only Marketplace source must not require acquisition: %v", err)
	}
	if len(cfg.Marketplaces) != 0 || len(cfg.MarketplaceExclusions) != 1 || cfg.MarketplaceExclusions[0].Locator != "tools/skill/review" {
		t.Fatalf("user Marketplace exclusions = %#v, marketplaces = %#v", cfg.MarketplaceExclusions, cfg.Marketplaces)
	}
}

func TestV4MarketplaceUserOverrideRejectsBroadSelection(t *testing.T) {
	for _, locator := range []string{"tools/*", "tools/collection/bundle"} {
		cfg, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\nresources: {m: {exclude: [" + locator + "]}}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if code := v4LockErrorCode(t, cfg, userLayer, nil); code != "v4.marketplace-runtime-unsupported" {
			t.Fatalf("locator %q: code = %q", locator, code)
		}
	}
}

func TestV4MarketplaceUserSelectionsAndOrganizationOverrideShareSource(t *testing.T) {
	cfg, err := ParseV4([]byte(v4MarketplaceConfigYAML + "    exclude: [tools/skill/other]\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc := v4LockFixture(t, lockfile.ScopeUser)
	out, err := cfg.ToLegacyConfigWithLock(userLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Marketplaces) != 1 || len(out.Marketplaces[0].Resources) != 4 || len(out.MarketplaceExclusions) != 1 || out.MarketplaceExclusions[0].Locator != "tools/skill/other" {
		t.Fatalf("user Marketplace selections/exclusion = %#v", out)
	}
}

// An exclusion removes a collection-selected member of the layer's own
// selection: exclude takes precedence over include in every writable layer,
// including a resource that is also requested directly.
func TestV4MarketplaceExclusionRemovesOwnCollectionMember(t *testing.T) {
	for _, tc := range []struct {
		kind  layerKind
		scope lockfile.Scope
	}{{userLayer, lockfile.ScopeUser}, {repoLayer, lockfile.ScopeRepo}, {localLayer, lockfile.ScopeLocal}} {
		t.Run(string(tc.scope), func(t *testing.T) {
			cfg, err := ParseV4([]byte(v4MarketplaceConfigYAML + "    exclude: [tools-alias/skill/leaf, tools/skill/review]\n"))
			if err != nil {
				t.Fatal(err)
			}
			doc := v4LockFixture(t, tc.scope)
			out, err := cfg.ToLegacyConfigWithLock(tc.kind, &doc)
			if err != nil {
				t.Fatal(err)
			}
			excluded := map[string]bool{}
			for _, pin := range out.Marketplaces[0].Resources {
				excluded[pin.Name] = out.MarketplacePinExcluded(out.Marketplaces[0], pin)
			}
			want := map[string]bool{"onboarding": false, "review": true, "leaf": true, "idx": false}
			if fmt.Sprint(excluded) != fmt.Sprint(want) {
				t.Fatalf("excluded pins = %v, want %v", excluded, want)
			}
		})
	}
}

func TestV4MarketplaceStaleLockRecordForExclusionOnlySourceIsIgnored(t *testing.T) {
	cfg, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  company: {marketplace: https://market.example}\n  other: {marketplace: https://other.example}\nresources:\n  company: {exclude: [tools/skill/review]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{URL: "https://market.example", InstanceID: "market-id"}, {URL: "https://other.example", InstanceID: "other-id"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.ToLegacyConfigWithLock(userLayer, &doc)
	if err != nil {
		t.Fatalf("stale record of an exclusion-only source must not fail the load: %v", err)
	}
	if len(out.Marketplaces) != 1 || out.Marketplaces[0].URL != "https://other.example" || len(out.MarketplaceExclusions) != 1 {
		t.Fatalf("marketplaces = %#v, exclusions = %#v", out.Marketplaces, out.MarketplaceExclusions)
	}
}

func TestV4MarketplaceEndpointOnlyHydration(t *testing.T) {
	cfg, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources: {m: {marketplace: https://market.example}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := lockfile.Normalize(lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{URL: "https://market.example", InstanceID: "market-id"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cfg.ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Marketplaces) != 1 || len(out.Marketplaces[0].Resources) != 0 || out.Marketplaces[0].Alias != "m" {
		t.Fatalf("endpoint-only entry = %#v", out.Marketplaces)
	}
}

func v4LockErrorCode(t *testing.T, cfg *V4Config, kind layerKind, doc *lockfile.Document) string {
	t.Helper()
	_, err := cfg.ToLegacyConfigWithLock(kind, doc)
	var v4Err *V4ValidationError
	if !errors.As(err, &v4Err) {
		t.Fatalf("error = %T %v, want V4ValidationError", err, err)
	}
	return v4Err.Code
}

func TestV4MarketplaceLockRefusals(t *testing.T) {
	repo := v4LockFixture(t, lockfile.ScopeRepo)
	v1, err := lockfile.Normalize(lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo})
	if err != nil {
		t.Fatal(err)
	}
	wrongScope := v4LockFixture(t, lockfile.ScopeUser)
	extraEndpoint := repo
	extraEndpoint.Marketplaces = append(append([]lockfile.MarketplaceLock(nil), repo.Marketplaces...),
		lockfile.MarketplaceLock{URL: "https://other.example", InstanceID: "other-id"})
	otherEndpoint := repo
	otherEndpoint.Marketplaces = append([]lockfile.MarketplaceLock(nil), repo.Marketplaces...)
	otherEndpoint.Marketplaces[0].URL = "https://other.example"
	extraResource := repo
	extraResource.Marketplaces = append([]lockfile.MarketplaceLock(nil), repo.Marketplaces...)
	extraResource.Marketplaces[0].Resources = append(append([]lockfile.MarketplaceResourceLock(nil), repo.Marketplaces[0].Resources...),
		v4LockResource("tools", "tools-id", "skill", "orphan", v4sha("1"), v4sha("1")))
	extraResource.Marketplaces[0].Resources[len(extraResource.Marketplaces[0].Resources)-1].ArchiveDigest = lockfile.Digest(v4sha("2"))

	cases := map[string]struct {
		yaml string
		doc  *lockfile.Document
		want string
	}{
		"nil lock":        {v4MarketplaceConfigYAML, nil, "v4.marketplace-lock-required"},
		"v1 lock":         {v4MarketplaceConfigYAML, &v1, "v4.marketplace-lock-required"},
		"wrong scope":     {v4MarketplaceConfigYAML, &wrongScope, "v4.marketplace-lock-mismatch"},
		"extra endpoint":  {v4MarketplaceConfigYAML, &extraEndpoint, "v4.marketplace-lock-mismatch"},
		"endpoint differ": {v4MarketplaceConfigYAML, &otherEndpoint, "v4.marketplace-lock-mismatch"},
		"duplicate source endpoint": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}, m2: {marketplace: https://market.example/}}
resources:
  m:
    include: [tools/skill/review]
`, &repo, "v4.marketplace-lock-mismatch"},
		"unknown binding": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [ghost/skill/review]
`, &repo, "v4.marketplace-lock-mismatch"},
		"unknown resource": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/missing]
`, &repo, "v4.marketplace-lock-mismatch"},
		"revision mismatch": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include:
      - tools/skill/review: {revision: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}
`, &repo, "v4.marketplace-lock-mismatch"},
		"unreachable resource": {v4MarketplaceConfigYAML, &extraResource, "v4.marketplace-lock-mismatch"},
		"conflicting binding request": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/review, tools-alias/skill/review, main/collection/onboarding]
`, &repo, "v4.marketplace-lock-mismatch"},
		"malformed lock": {
			v4MarketplaceConfigYAML, &lockfile.Document{
				APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
				Marketplaces: []lockfile.MarketplaceLock{{
					URL: "https://market.example", InstanceID: "market-id",
					RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "tools", RegistryID: "tools-id"}},
					Resources:        []lockfile.MarketplaceResourceLock{v4LockResource("ghost", "tools-id", "skill", "review", v4sha("a"), v4sha("a"))},
				}},
			}, "v4.marketplace-lock-mismatch"},
		"tags policy": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/review]
    tags: [team]
    registries: [tools]
`, &repo, "v4.marketplace-runtime-unsupported"},
		"all policy": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/review]
    all: true
    registries: [tools]
`, &repo, "v4.marketplace-runtime-unsupported"},
		"registries policy": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/review]
    registries: [tools]
`, &repo, "v4.marketplace-runtime-unsupported"},
		"exclusion-only override outside user layer": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    exclude: [tools/skill/leaf]
`, &repo, "v4.layer-field"},
		"harnesses policy": {
			`apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [tools/skill/review]
    harnesses: [factory]
`, &repo, "v4.marketplace-runtime-unsupported"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := ParseV4([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if code := v4LockErrorCode(t, cfg, repoLayer, tc.doc); code != tc.want {
				t.Fatalf("code = %q, want %q", code, tc.want)
			}
		})
	}
}

func TestV4MarketplaceLockMissingEndpointDeterministic(t *testing.T) {
	cfg, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources:
  z: {marketplace: https://z.example}
  a: {marketplace: https://a.example}
`))
	if err != nil {
		t.Fatal(err)
	}
	doc := &lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo}
	_, err = cfg.toLegacyConfig(repoLayer, doc)
	var v4err *V4ValidationError
	if !errors.As(err, &v4err) {
		t.Fatalf("error = %T %v, want V4ValidationError", err, err)
	}
	if v4err.Code != "v4.marketplace-lock-mismatch" || v4err.Path != "sources.a" {
		t.Fatalf("error = %q %q, want v4.marketplace-lock-mismatch at sources.a", v4err.Code, v4err.Path)
	}
}

func TestV4MarketplaceLockHydrationNormalizesLock(t *testing.T) {
	doc := v4LockFixture(t, lockfile.ScopeRepo)
	doc.Marketplaces = append([]lockfile.MarketplaceLock(nil), doc.Marketplaces...)
	doc.Marketplaces[0].URL = "https://MARKET.example/"
	cfg, err := v4LockConfig(t).ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatalf("unnormalized lock rejected: %v", err)
	}
	if len(cfg.Marketplaces) != 1 || cfg.Marketplaces[0].URL != "https://market.example" {
		t.Fatalf("marketplaces = %#v", cfg.Marketplaces)
	}
}

func TestLoadV4MarketplaceConfigWithAdjacentLock(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(v4MarketplaceConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := v4LockFixture(t, lockfile.ScopeRepo)
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != 4 {
		t.Fatalf("marketplaces = %#v", cfg.Marketplaces)
	}
}

func TestLoadV4MarketplaceConfigUsesLogicalLockPathForApprovedLinks(t *testing.T) {
	repo := t.TempDir()
	agents := filepath.Join(repo, ".agents")
	configDir := filepath.Join(repo, "config")
	lockDir := filepath.Join(repo, "locks")
	for _, dir := range []string{agents, configDir, lockDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	configTarget := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configTarget, []byte(v4MarketplaceConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(agents, "config.yaml")
	if err := os.Symlink(filepath.Join("..", "config", "config.yaml"), configPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	lockData, err := lockfile.Encode(v4LockFixture(t, lockfile.ScopeRepo))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockDir, "lock.yaml"), lockData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "locks", "lock.yaml"), filepath.Join(agents, "lock.yaml")); err != nil {
		t.Fatal(err)
	}

	decoy := v4LockFixture(t, lockfile.ScopeRepo)
	decoy.Marketplaces[0].InstanceID = "decoy-instance"
	decoyData, err := lockfile.Encode(decoy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "lock.yaml"), decoyData, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load config through approved links: %v", err)
	}
	if cfg == nil || len(cfg.Marketplaces) != 1 || cfg.Marketplaces[0].InstanceID != "market-id" {
		t.Fatalf("marketplaces = %#v, want the identity from the logical layer lock", cfg)
	}
}

func TestLoadV4MarketplaceConfigRejectsInvalidLock(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(v4MarketplaceConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock.yaml"), []byte(`apiVersion: agent-marketplace-lock/v2
scope: repo
marketplaces:
  - url: https://market.example
    instance_id: market-id
    registry_bindings:
      - name: tools
        registry_id: tools-id
    resources:
      - registry_name: ghost
        registry_id: tools-id
        kind: skill
        name: review
        revision: rev-a
        content_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        archive_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        acquisition: mirror
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfigFromPath(configPath)
	if err == nil || !strings.Contains(err.Error(), "decode lock") {
		t.Fatalf("invalid lock bypassed Decode: %v", err)
	}
}

// spec: deployed-acquisition-locks/wrong-scope-in-single-file-read
func TestLoadV4MarketplaceConfigWrongScopeLockRejectedAtRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope lockfile.Scope
		want  string
	}{
		{name: "matching scope binds evidence", scope: lockfile.ScopeRepo},
		{name: "wrong scope rejected before bind", scope: lockfile.ScopeUser, want: "v4.marketplace-lock-mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(v4MarketplaceConfigYAML), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := lockfile.Encode(v4LockFixture(t, tc.scope))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "lock.yaml"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfigFromPath(configPath)
			if tc.want == "" {
				if err != nil || cfg == nil || len(cfg.Marketplaces) != 1 {
					t.Fatalf("control load = %#v, err = %v", cfg, err)
				}
				return
			}
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.want {
				t.Fatalf("error = %v, want V4ValidationError %s rejecting the lock at read", err, tc.want)
			}
		})
	}
}

func TestLoadV4MarketplaceConfigMissingLock(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(v4MarketplaceConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfigFromPath(configPath)
	var v4Err *V4ValidationError
	if !errors.As(err, &v4Err) || v4Err.Code != "v4.marketplace-lock-required" {
		t.Fatalf("error = %v, want v4.marketplace-lock-required", err)
	}
}

func TestLoadV4MarketplaceLockOverlayRead(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(v4MarketplaceConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalConfigPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := lockfile.PathForConfig(canonical)
	if err != nil {
		t.Fatal(err)
	}
	doc := v4LockFixture(t, lockfile.ScopeRepo)
	lockData, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	opts := LoadEffectiveOptions{
		RepoRoot:         dir,
		LocalConfigDir:   dir,
		OrganizationPath: filepath.Join(dir, "org-config.yaml"),
		GlobalPath:       filepath.Join(dir, "user-config.yaml"),
		RepoPath:         configPath,
		LocalPath:        filepath.Join(dir, "local-config.yaml"),
		FileOverlays:     map[string][]byte{lockPath: lockData},
	}
	ec := loadEffectiveForTest(context.Background(), opts)
	if err := errors.Join(ec.Errors...); err != nil {
		t.Fatal(err)
	}
	cfg := ec.Repo.Config
	if cfg == nil || len(cfg.Marketplaces) != 1 {
		t.Fatalf("repo config = %#v", cfg)
	}
	var lockRead *ConfigRead
	for i := range ec.Repo.Reads {
		if ec.Repo.Reads[i].Path == lockPath {
			lockRead = &ec.Repo.Reads[i]
		}
	}
	if lockRead == nil || !lockRead.Overlay || lockRead.Digest != fmt.Sprintf("%x", sha256.Sum256(lockData)) {
		t.Fatalf("lock read = %#v, reads = %#v", lockRead, ec.Repo.Reads)
	}
}
