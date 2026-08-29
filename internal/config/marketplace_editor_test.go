package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

const testMarketplaceDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func testMarketplacePin() MarketplaceResourcePin {
	return MarketplaceResourcePin{
		SourceID:    "source-one",
		SourceSlug:  "source-slug",
		Kind:        "skill",
		Name:        "review",
		Revision:    "v1",
		Digest:      testMarketplaceDigest,
		Acquisition: MarketplaceAcquisitionMirror,
	}
}

func TestEnsureMarketplacePinPersistsExactIndexOnlyOccurrence(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	pin := testMarketplacePin()
	pin.Acquisition = MarketplaceAcquisitionIndexOnly
	pin.SourceCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pin.SourceManifestDigest = testMarketplaceDigest
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	got := cfg.Marketplaces[0].Resources[0]
	if got.Acquisition != pin.Acquisition || got.SourceCommit != pin.SourceCommit || got.SourceManifestDigest != pin.SourceManifestDigest {
		t.Fatalf("persisted index-only pin = %#v", got)
	}
}

func TestEnsureMarketplacePinRejectsInvalidIndexOnlyOccurrenceBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\n# preserve this byte\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	pin := testMarketplacePin()
	pin.Acquisition = MarketplaceAcquisitionIndexOnly
	pin.SourceCommit = "not-a-commit"
	pin.SourceManifestDigest = "not-a-digest"
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err == nil || !strings.Contains(err.Error(), "source_commit") {
		t.Fatalf("invalid index-only pin error = %v, want source_commit validation", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("config mutated before index-only validation: got %q, want %q", got, original)
	}

	pin = testMarketplacePin()
	pin.SourceCommit = strings.Repeat("a", 40)
	pin.SourceManifestDigest = "bad"
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err == nil || !strings.Contains(err.Error(), "source_manifest_digest") {
		t.Fatalf("invalid source manifest digest error = %v, want validation", err)
	}

	pin = testMarketplacePin()
	pin.SourceCommit = "not-a-commit"
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err == nil || !strings.Contains(err.Error(), "require acquisition") {
		t.Fatalf("mirror occurrence fields error = %v, want acquisition validation", err)
	}
}

func TestEnsureMarketplacePinInConfigTargetsSelectedLayersAndIsIdempotent(t *testing.T) {
	for _, filename := range []string{"config.yaml", "config.local.yaml"} {
		t.Run(filename, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".agents", filename)
			pin := testMarketplacePin()
			if err := EnsureMarketplacePinInConfig(path, "HTTPS://MARKET.example/", "market-instance", "public", pin); err != nil {
				t.Fatalf("ensure pin: %v", err)
			}
			first, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := EnsureMarketplacePinInConfig(path, "https://market.example", "market-instance", "new-alias", pin); err != nil {
				t.Fatalf("repeat ensure pin: %v", err)
			}
			second, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(first) != string(second) {
				t.Fatalf("idempotent mutation changed bytes:\nfirst:\n%s\nsecond:\n%s", first, second)
			}
			var cfg Config
			if err := yaml.Unmarshal(second, &cfg); err != nil {
				t.Fatalf("decode edited config: %v", err)
			}
			if len(cfg.Marketplaces) != 1 || cfg.Marketplaces[0].Alias != "public" {
				t.Fatalf("marketplace entry = %#v, want one entry retaining alias public", cfg.Marketplaces)
			}
			if len(cfg.Marketplaces[0].Resources) != 1 || cfg.Marketplaces[0].Resources[0].Name != "review" {
				t.Fatalf("resources = %#v", cfg.Marketplaces[0].Resources)
			}
		})
	}
}

func TestEnsureMarketplacePinTargetsUserLayerPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", testMarketplacePin()); err != nil {
		t.Fatalf("ensure user-layer pin: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("user-layer config was not created: %v", err)
	}
}

func TestEnsureMarketplacePinPreservesInstalledName(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	pin := testMarketplacePin()
	pin.InstalledName = "chosen-review"
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err != nil {
		t.Fatal(err)
	}
	pin.InstalledName = "different-review"
	if err := EnsureMarketplacePinInConfig(path, "https://market.example/", "instance", "public", pin); err == nil || !strings.Contains(err.Error(), "conflicting installed_name") {
		t.Fatalf("installed_name conflict error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Marketplaces[0].Resources[0].InstalledName; got != "chosen-review" {
		t.Fatalf("installed_name = %q, want chosen-review", got)
	}
}

func TestEnsureMarketplacePinAllowsSourceSlugRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	pin := testMarketplacePin()
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err != nil {
		t.Fatal(err)
	}
	pin.SourceSlug = "renamed-source"
	if err := EnsureMarketplacePinInConfig(path, "HTTPS://MARKET.example/", "instance", "public", pin); err != nil {
		t.Fatalf("source slug rename rejected: %v", err)
	}
}

func TestEnsureMarketplacePinRejectsConflictingIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	pin := testMarketplacePin()
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err != nil {
		t.Fatal(err)
	}
	pin.Revision = "v2"
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", pin); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestEnsureMarketplacePinRejectsMalformedAndMultiDocumentConfig(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "malformed", data: "marketplaces: [\n", want: "parse config"},
		{name: "multi-document", data: "marketplaces: []\n---\nmarketplaces: []\n", want: "additional YAML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.data), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "", testMarketplacePin()); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateMarketplaceResourcePinRejectsUnsafeInstalledName(t *testing.T) {
	for _, name := range []string{"../escape", "CON.txt", "bad:name", "trailing.", "trailing "} {
		pin := testMarketplacePin()
		pin.InstalledName = name
		if err := ValidateMarketplaceResourcePin(pin); err == nil {
			t.Errorf("unsafe installed name %q accepted", name)
		}
		if errs := ValidateMarketplaceEntries([]MarketplaceEntry{{URL: "https://market.example", InstanceID: "instance", Resources: []MarketplaceResourcePin{pin}}}); len(errs) == 0 {
			t.Errorf("ValidateMarketplaceEntries accepted unsafe installed name %q", name)
		}
	}
	for _, name := range []string{"review.copy", "review copy", "résumé"} {
		pin := testMarketplacePin()
		pin.InstalledName = name
		if err := ValidateMarketplaceResourcePin(pin); err != nil {
			t.Errorf("valid installed name %q rejected: %v", name, err)
		}
	}
}

func TestEnsureMarketplaceEntryRejectsUnsafeIdentityAndDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, entry := range []MarketplaceEntry{
		{URL: "https://market.example", InstanceID: " instance"},
		{URL: "https://market.example", InstanceID: "instance", Alias: "alias/name"},
	} {
		if err := EnsureMarketplaceEntryInConfig(path, entry); err == nil {
			t.Fatalf("unsafe entry %#v accepted", entry)
		}
	}
	data := []byte(`marketplaces:
  - url: https://market.example/
    instance_id: instance
  - url: HTTPS://MARKET.example
    instance_id: instance
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureMarketplaceEntryInConfig(path, MarketplaceEntry{URL: "https://market.example", InstanceID: "instance"}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate entry error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("duplicate rejection changed config bytes:\nbefore:\n%safter:\n%s", before, after)
	}
}

func TestEnsureMarketplaceEntrySerializesIdentityConflicts(t *testing.T) {
	tests := []struct {
		name    string
		entries []MarketplaceEntry
	}{
		{name: "same URL different instance", entries: []MarketplaceEntry{{URL: "https://market.example", InstanceID: "one"}, {URL: "https://market.example", InstanceID: "two"}}},
		{name: "same instance different URL", entries: []MarketplaceEntry{{URL: "https://one.example", InstanceID: "same"}, {URL: "https://two.example", InstanceID: "same"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			start := make(chan struct{})
			errs := make(chan error, len(tc.entries))
			var wg sync.WaitGroup
			for _, entry := range tc.entries {
				wg.Add(1)
				go func(entry MarketplaceEntry) {
					defer wg.Done()
					<-start
					errs <- EnsureMarketplaceEntryInConfig(path, entry)
				}(entry)
			}
			close(start)
			wg.Wait()
			close(errs)
			succeeded, failed := 0, 0
			for err := range errs {
				if err == nil {
					succeeded++
				} else {
					failed++
				}
			}
			if succeeded != 1 || failed != 1 {
				t.Fatalf("succeeded/failed = %d/%d, want 1/1", succeeded, failed)
			}
			cfg, err := LoadConfigFromPath(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Marketplaces) != 1 {
				t.Fatalf("marketplaces = %#v", cfg.Marketplaces)
			}
		})
	}
}

func TestEnsureMarketplacePinFailsClosedAndPreservesBytes(t *testing.T) {
	tests := []string{
		"marketplaces:\n  - https://market.example\n",
		"marketplaces:\n  - url: https://market.example\n    instance_id: instance\n    resources:\n      - skill\n",
		"marketplaces:\n  - url: https://market.example\n    instance_id: instance\n    alias: bad/name\n",
	}
	for i, data := range tests {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "", testMarketplacePin()); err == nil {
				t.Fatal("malformed config accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("failed edit changed config bytes:\nbefore:\n%safter:\n%s", before, after)
			}
		})
	}
}

func TestEnsureMarketplacePinRejectsUnknownAndMalformedMarketplaceFieldsBeforeMutation(t *testing.T) {
	tests := []string{
		"marketplaces:\n  - url: https://market.example\n    instance_id: instance\n    unexpected: true\n",
		"marketplaces:\n  - url: https://market.example\n    instance_id: instance\n    resources:\n      - source_id: source\n        kind: skill\n        name: review\n        revision: v1\n        digest: sha256:0000000000000000000000000000000000000000000000000000000000000000\n        unexpected: true\n",
		"marketplaces:\n  - url: https://market.example\n    instance_id: instance\n    resources:\n      - source_id: [source]\n",
	}
	for i, data := range tests {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "", testMarketplacePin()); err == nil {
				t.Fatal("invalid Marketplace mapping accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("failed edit changed config bytes:\nbefore:\n%safter:\n%s", before, after)
			}
		})
	}
}

func TestEnsureMarketplacePinRejectsYAMLAliasesBeforeMutation(t *testing.T) {
	for name, data := range map[string]string{
		"anchored entry": `marketplaces:
  - &entry
    url: https://market.example
    instance_id: instance
    resources: []
`,
		"aliased entry": `marketplace_defaults: &entry
  url: https://market.example
  instance_id: instance
  resources: []
marketplaces:
  - *entry
`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "", testMarketplacePin()); err == nil || !strings.Contains(err.Error(), "YAML alias or anchor") {
				t.Fatalf("error = %v, want explicit YAML alias/anchor error", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("alias rejection changed config bytes")
			}
		})
	}
}

func TestEnsureMarketplacePinSnapshotWaitsForWriterHoldingPathLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("apiVersion: agent-marketplace/v2\n# writer committed first\n")
	writerReady := make(chan struct{})
	releaseWriter := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- withConfigPathLock(path, func() error {
			close(writerReady)
			<-releaseWriter
			return os.WriteFile(path, concurrent, 0o644)
		})
	}()
	<-writerReady

	type result struct {
		snapshot *FileSnapshot
		commit   ConfigFileCommit
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		snapshot, commit, err := EnsureMarketplacePinInConfigWithSnapshotAndCommit(path, "https://market.example", "instance", "public", testMarketplacePin())
		resultCh <- result{snapshot: snapshot, commit: commit, err: err}
	}()
	close(releaseWriter)
	if err := <-writerDone; err != nil {
		t.Fatalf("concurrent writer: %v", err)
	}
	got := <-resultCh
	if got.err != nil {
		t.Fatalf("ensure pin: %v", got.err)
	}
	if string(got.snapshot.data) != string(concurrent) {
		t.Fatalf("snapshot = %q, want writer bytes %q", got.snapshot.data, concurrent)
	}
	if string(got.commit.Bytes) == string(concurrent) || !strings.Contains(string(got.commit.Bytes), "review") {
		t.Fatalf("commit = %q, want pin based on writer bytes", got.commit.Bytes)
	}
}

func TestUpdateMarketplacePinCompareAndSwap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	old := testMarketplacePin()
	old.InstalledName = "chosen-review"
	if err := EnsureMarketplacePinInConfig(path, "https://market.example", "instance", "public", old); err != nil {
		t.Fatal(err)
	}
	updated := old
	updated.Revision = "sha256:" + strings.Repeat("b", 64)
	updated.Digest = updated.Revision
	updated.SourceSlug = "renamed-source"
	snapshot, commit, err := UpdateMarketplacePinInConfigWithSnapshotAndCommit(path, "HTTPS://MARKET.example/", "instance", old, updated)
	if err != nil {
		t.Fatalf("update pin: %v", err)
	}
	if snapshot == nil || len(commit.Bytes) == 0 || commit.Generation == "" {
		t.Fatalf("update snapshot/commit = %#v/%#v", snapshot, commit)
	}
	if err := snapshot.RestoreIfCurrent(commit); err != nil {
		t.Fatalf("restore update snapshot: %v", err)
	}
	if err := UpdateMarketplacePinInConfig(path, "HTTPS://MARKET.example/", "instance", old, updated); err != nil {
		t.Fatalf("repeat update pin: %v", err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Marketplaces[0].Resources[0]
	if got != updated || got.InstalledName != "chosen-review" {
		t.Fatalf("updated pin = %#v, want %#v", got, updated)
	}
	conflict := updated
	conflict.Revision = "sha256:" + strings.Repeat("c", 64)
	if err := UpdateMarketplacePinInConfig(path, "https://market.example", "instance", old, conflict); err == nil || !strings.Contains(err.Error(), "compare-and-swap conflict") {
		t.Fatalf("CAS conflict error = %v", err)
	}
}

func TestEnsureMarketplacePinSerializesConcurrentDistinctAdds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const count = 12
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pin := testMarketplacePin()
			pin.Name = fmt.Sprintf("review-%d", i)
			errs <- EnsureMarketplacePinInConfig(path, "HTTPS://MARKET.example/", "instance", "public", pin)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ensure: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode concurrent result: %v\n%s", err, data)
	}
	if len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != count {
		t.Fatalf("concurrent result = %#v, want one entry with %d resources", cfg.Marketplaces, count)
	}
}
