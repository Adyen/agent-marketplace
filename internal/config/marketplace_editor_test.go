package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

const testMarketplaceDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
const testMarketplaceRevision = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func testMarketplacePin() MarketplaceResourcePin {
	return MarketplaceResourcePin{
		RegistryID: "source-one", RegistrySlug: "source-slug", Kind: "skill", Name: "review",
		Revision: testMarketplaceRevision, Digest: testMarketplaceDigest, Acquisition: MarketplaceAcquisitionMirror,
	}
}

// These fixtures are authored v4 documents, not converted runtime pins.
func writeMarketplaceV4(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openMarketplaceEditor(t *testing.T, path string, source Source) *LayerEditor {
	t.Helper()
	editor, err := OpenLayerEditor(LayerTarget{Source: source, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	return editor
}

// Exercise the supported candidate API and existing publication/snapshot
// primitives. No exact installation evidence is written into consumer roots.
func commitMarketplaceEdit(path string, source Source, edit func(*LayerEditor) error) (*FileSnapshot, ConfigFileCommit, error) {
	var snapshot *FileSnapshot
	var commit ConfigFileCommit
	err := withConfigPathLock(path, func() error {
		var err error
		snapshot, err = SnapshotConfigFile(path)
		if err != nil {
			return err
		}
		editor, err := OpenLayerEditor(LayerTarget{Source: source, StagedPath: path, DeclaringPath: path})
		if err != nil {
			return err
		}
		if err := edit(editor); err != nil {
			return err
		}
		candidate, err := editor.Render()
		if err != nil {
			return err
		}
		commit = NewConfigFileCommit(candidate.Bytes)
		if !candidate.Changed {
			return nil
		}
		return writeConfigDocumentData(path, candidate.Mode, candidate.Bytes)
	})
	return snapshot, commit, err
}

func marketplaceIncludeEdit(endpoint string, pins ...MarketplaceResourcePin) func(*LayerEditor) error {
	return func(editor *LayerEditor) error {
		binding, err := editor.EnsureMarketplaceSource(endpoint)
		if err != nil {
			return err
		}
		return editor.IncludeMarketplacePins(binding, pins)
	}
}

func requireMarketplaceV4Code(t *testing.T, err error, code string) {
	t.Helper()
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != code {
		t.Fatalf("error = %v, want V4ValidationError code %q", err, code)
	}
}

type marketplaceFileState struct {
	data []byte
	info os.FileInfo
}

func marketplaceState(t *testing.T, path string) marketplaceFileState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return marketplaceFileState{data: data, info: info}
}

func requireMarketplaceUnchanged(t *testing.T, path string, before marketplaceFileState) {
	t.Helper()
	after := marketplaceState(t, path)
	if !bytes.Equal(before.data, after.data) || !os.SameFile(before.info, after.info) || before.info.Mode() != after.info.Mode() || !before.info.ModTime().Equal(after.info.ModTime()) {
		t.Fatalf("refused/no-op edit changed bytes or metadata of %s", path)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") || strings.HasPrefix(entry.Name(), ".config-rollback-") {
			t.Fatalf("staging residue remains: %s", entry.Name())
		}
	}
}

func marketplaceRequestLocators(cfg *V4Config) []string {
	locators := make([]string, 0, len(cfg.Requests))
	for _, request := range cfg.Requests {
		locators = append(locators, request.Locator)
	}
	return locators
}

func marketplaceTestEvidence() lockfile.Document {
	return lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: "https://market.example", InstanceID: "instance",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: "source-slug", RegistryID: "source-one"}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName: "source-slug", RegistryID: "source-one", Kind: "skill", Name: "review", Revision: testMarketplaceRevision,
				ContentDigest: testMarketplaceDigest, ArchiveDigest: lockfile.Digest("sha256:" + strings.Repeat("f", 64)), Acquisition: "mirror",
			}},
		}},
	}
}

func TestEnsureMarketplacePinPersistsExactIndexOnlyOccurrence(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\n")
	pin := testMarketplacePin()
	pin.Acquisition = MarketplaceAcquisitionIndexOnly
	pin.RegistryCommit = strings.Repeat("a", 40)
	pin.RegistryManifestDigest = testMarketplaceDigest
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", pin)); err != nil {
		t.Fatal(err)
	}
	data := marketplaceState(t, path).data
	for _, forbidden := range []string{"instance_id", "registry_commit", "registry_manifest_digest", "acquisition", "digest"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("consumer root contains lock field %q: %s", forbidden, data)
		}
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := marketplaceTestEvidence()
	doc.Marketplaces[0].Resources[0].Acquisition = "index-only"
	doc.Marketplaces[0].Resources[0].RegistryCommit = strings.Repeat("a", 40)
	doc.Marketplaces[0].Resources[0].RegistryManifestDigest = testMarketplaceDigest
	doc.Marketplaces[0].Resources[0].SourcePath = "skills/review"
	// Serialize the independent acquisition-lock format, then hydrate via the real adapter.
	lockBytes, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = lockfile.Decode(lockBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v4.ToLegacyConfigWithLock(repoLayer, &doc); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(path), "lock.yaml")
	if err := os.WriteFile(lockPath, lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	beforeRoot, beforeLock := marketplaceState(t, path), marketplaceState(t, lockPath)
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	requireMarketplaceUnchanged(t, path, beforeRoot)
	requireMarketplaceUnchanged(t, lockPath, beforeLock)
	got := cfg.Marketplaces[0].Resources[0]
	if got.Acquisition != "index-only" || got.RegistryCommit != strings.Repeat("a", 40) || got.RegistryManifestDigest != testMarketplaceDigest || got.LockSourcePath != "skills/review" || !got.Direct {
		t.Fatalf("hydrated index-only occurrence = %#v", got)
	}
}

func TestEnsureMarketplacePinRejectsInvalidIndexOnlyOccurrenceBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "# preserve this byte\nsources:\n  public: {marketplace: https://market.example}\n")
	before := marketplaceState(t, path)
	for _, tc := range []struct{ acquisition, commit, manifest, want string }{
		{"index-only", "not-a-commit", "not-a-digest", "registry_commit"},
		{"index-only", strings.Repeat("a", 40), "bad", "registry_manifest_digest"},
		{"mirror", "not-a-commit", "", "require acquisition"},
	} {
		pin := testMarketplacePin()
		pin.Acquisition, pin.RegistryCommit, pin.RegistryManifestDigest = tc.acquisition, tc.commit, tc.manifest
		if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", pin)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("invalid occurrence error = %v, want %q", err, tc.want)
		}
		requireMarketplaceUnchanged(t, path, before)
	}
}

func TestEnsureMarketplacePinInConfigTargetsSelectedLayersAndIsIdempotent(t *testing.T) {
	for _, tc := range []struct {
		filename string
		source   Source
	}{{"config.yaml", RepoCommitted}, {"config.local.yaml", RepoPersonal}} {
		t.Run(tc.filename, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".agents", tc.filename)
			writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\n")
			if _, _, err := commitMarketplaceEdit(path, tc.source, marketplaceIncludeEdit("HTTPS://MARKET.example/", testMarketplacePin())); err != nil {
				t.Fatal(err)
			}
			first := marketplaceState(t, path)
			if _, _, err := commitMarketplaceEdit(path, tc.source, marketplaceIncludeEdit("https://market.example", testMarketplacePin())); err != nil {
				t.Fatal(err)
			}
			requireMarketplaceUnchanged(t, path, first)
			v4, err := LoadV4ConfigFromPath(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(v4.Sources) != 1 || v4.Sources["public"].CanonicalURL != "https://market.example" || !slices.Equal(marketplaceRequestLocators(v4), []string{"source-slug/skill/review"}) {
				t.Fatalf("source/request = %#v/%#v", v4.Sources, v4.Requests)
			}
		})
	}
}

func TestEnsureMarketplacePinTargetsUserLayerPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	writeMarketplaceV4(t, path, "harnesses: [factory]\n")
	if _, _, err := commitMarketplaceEdit(path, UserPersonal, marketplaceIncludeEdit("https://market.example", testMarketplacePin())); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigIfPresent(path, UserPersonal)
	if err != nil {
		t.Fatal(err)
	}
	if v4.Sources["marketplace"].Kind != V4SourceMarketplace || len(v4.Requests) != 1 || v4.Requests[0].Locator != "source-slug/skill/review" {
		t.Fatalf("user source/selection = %#v/%#v", v4.Sources, v4.Requests)
	}
}

func TestEnsureMarketplacePinPreservesInstalledName(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\n")
	pin := testMarketplacePin()
	pin.InstalledName = "chosen-review"
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", pin)); err != nil {
		t.Fatal(err)
	}
	before := marketplaceState(t, path)
	pin.InstalledName = "different-review"
	_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example/", pin))
	requireMarketplaceV4Code(t, err, "v4.duplicate-request")
	requireMarketplaceUnchanged(t, path, before)
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := marketplaceTestEvidence()
	cfg, err := v4.ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Marketplaces[0].Resources[0].InstalledName; got != "chosen-review" {
		t.Fatalf("installed name = %q", got)
	}
}

func TestEnsureMarketplacePinAllowsRegistrySlugRename(t *testing.T) {
	v4, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [source-slug/skill/review]\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc := marketplaceTestEvidence()
	doc.Marketplaces[0].RegistryBindings = append(doc.Marketplaces[0].RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: "renamed-source", RegistryID: "source-one"})
	doc.Marketplaces[0].Resources[0].RegistryName = "renamed-source"
	cfg, err := v4.ToLegacyConfigWithLock(repoLayer, &doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Marketplaces[0].Resources[0]; got.RegistryID != "source-one" || got.RegistrySlug != "renamed-source" || !got.Direct {
		t.Fatalf("renamed occurrence = %#v", got)
	}
}

func TestEnsureMarketplacePinRejectsConflictingIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public:\n    include:\n      - source-slug/skill/review: {revision: sha256:1111111111111111111111111111111111111111111111111111111111111111}\n")
	before := marketplaceState(t, path)
	pin := testMarketplacePin()
	pin.Revision = "sha256:" + strings.Repeat("b", 64)
	_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", pin))
	requireMarketplaceV4Code(t, err, "v4.duplicate-request")
	requireMarketplaceUnchanged(t, path, before)
}

func TestEnsureMarketplacePinRejectsMalformedAndMultiDocumentConfig(t *testing.T) {
	for _, data := range []string{"apiVersion: agent-marketplace/v4\nsources: [\n", "apiVersion: agent-marketplace/v4\nsources: {}\n---\nsources: {}\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		before := marketplaceState(t, path)
		if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin())); err == nil {
			t.Fatal("malformed/multi-document root accepted")
		}
		requireMarketplaceUnchanged(t, path, before)
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
			t.Errorf("unsafe installed name %q accepted by evidence validator", name)
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

func TestEnsureMarketplaceSourceRejectsUnsafeIdentityAndDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\n")
	before := marketplaceState(t, path)
	for _, endpoint := range []string{"https://user:pass@market.example", "https://market.example?token=secret"} {
		if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit(endpoint, testMarketplacePin())); err == nil {
			t.Fatalf("unsafe endpoint %q accepted", endpoint)
		}
		requireMarketplaceUnchanged(t, path, before)
	}
	writeMarketplaceV4(t, path, "sources:\n  one: {marketplace: https://market.example/}\n  two: {marketplace: 'HTTPS://MARKET.example'}\n")
	before = marketplaceState(t, path)
	_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin()))
	requireMarketplaceV4Code(t, err, "config.edit.ambiguous-source")
	requireMarketplaceUnchanged(t, path, before)
	if errs := ValidateMarketplaceEntries([]MarketplaceEntry{{URL: "https://market.example", InstanceID: " instance"}}); len(errs) == 0 {
		t.Fatal("unsafe acquired instance accepted")
	}
}

func TestEnsureMarketplaceSourceSerializesNormalizedRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeMarketplaceV4(t, path, "")
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, endpoint := range []string{"HTTPS://MARKET.example/", "https://market.example"} {
		go func(endpoint string) {
			<-start
			_, _, err := commitMarketplaceEdit(path, RepoCommitted, func(editor *LayerEditor) error { _, err := editor.EnsureMarketplaceSource(endpoint); return err })
			errs <- err
		}(endpoint)
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Sources) != 1 || v4.Sources["marketplace"].CanonicalURL != "https://market.example" {
		t.Fatalf("registrations = %#v", v4.Sources)
	}
	// Endpoint instance identity remains lock evidence, not authored registration.
	doc := marketplaceTestEvidence()
	doc.Marketplaces = append(doc.Marketplaces, lockfile.MarketplaceLock{URL: "https://market.example/", InstanceID: "other-instance"})
	if _, err := v4.ToLegacyConfigWithLock(repoLayer, &doc); err == nil {
		t.Fatal("conflicting acquired endpoint identities accepted")
	} else {
		requireMarketplaceV4Code(t, err, "v4.marketplace-lock-mismatch")
	}
}

func TestEnsureMarketplacePinFailsClosedAndPreservesBytes(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{"sources:\n  public: https://market.example\n", "v4.source-type"},
		{"sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [skill]\n", "v4.locator"},
		{"sources:\n  bad/name: {marketplace: https://market.example}\n", "v4.source-name"},
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeMarketplaceV4(t, path, tc.body)
		before := marketplaceState(t, path)
		_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin()))
		requireMarketplaceV4Code(t, err, tc.code)
		requireMarketplaceUnchanged(t, path, before)
	}
}

// spec: config-layering/marketplace-entries-are-validated
func TestEnsureMarketplacePinRejectsUnknownAndMalformedMarketplaceFieldsBeforeMutation(t *testing.T) {
	for _, tc := range []struct{ body, code string }{
		{"sources:\n  public: {marketplace: https://market.example, unexpected: true}\n", "v4.unknown-field"},
		{"sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [{source-slug/skill/review: {unexpected: true}}]\n", "v4.unknown-field"},
		{"sources:\n  public: {marketplace: [source]}\n", "v4.type"},
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeMarketplaceV4(t, path, tc.body)
		before := marketplaceState(t, path)
		_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin()))
		requireMarketplaceV4Code(t, err, tc.code)
		requireMarketplaceUnchanged(t, path, before)
	}
}

func TestEnsureMarketplacePinRejectsYAMLAliasesBeforeMutation(t *testing.T) {
	for _, body := range []string{
		"sources:\n  public: &entry {marketplace: https://market.example}\n  other: *entry\n",
		"sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: &requests [source-slug/skill/review]\n  other: *requests\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeMarketplaceV4(t, path, body)
		before := marketplaceState(t, path)
		_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin()))
		requireMarketplaceV4Code(t, err, "v4.alias-forbidden")
		requireMarketplaceUnchanged(t, path, before)
	}
	// An unused anchor is not an alias and is supported by the strict v4 reader.
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: &entry {marketplace: https://market.example}\n")
	if _, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin())); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil || !slices.Equal(marketplaceRequestLocators(v4), []string{"source-slug/skill/review"}) {
		t.Fatalf("anchor control = %#v, %v", v4, err)
	}
}

func TestEnsureMarketplacePinSnapshotWaitsForWriterHoldingPathLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeMarketplaceV4(t, path, "")
	concurrent := []byte("apiVersion: agent-marketplace/v4\n# writer committed first\n")
	writerReady, releaseWriter := make(chan struct{}), make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- withConfigPathLock(path, func() error { close(writerReady); <-releaseWriter; return os.WriteFile(path, concurrent, 0o600) })
	}()
	<-writerReady
	type result struct {
		snapshot *FileSnapshot
		commit   ConfigFileCommit
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		snapshot, commit, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("https://market.example", testMarketplacePin()))
		resultCh <- result{snapshot, commit, err}
	}()
	close(releaseWriter)
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if !bytes.Equal(got.snapshot.data, concurrent) || !bytes.Contains(got.commit.Bytes, []byte("review")) || !bytes.Contains(got.commit.Bytes, []byte("writer committed first")) {
			t.Fatalf("snapshot/commit = %#v/%s", got.snapshot, got.commit.Bytes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("candidate edit did not resume after path lock release")
	}
}

// A fragment-owned wildcard exclusion suppresses a Marketplace include the
// same way it suppresses a Git include: the root cannot silently override it.
func TestIncludeMarketplacePinsRefusesInheritedWildcardExclusion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	fragment := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(fragment, []byte("resources:\n  public: {exclude: ['source-slug/skill/*']}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeMarketplaceV4(t, path, "include: policy.yaml\nsources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [source-slug/command/run]\n")
	before := marketplaceState(t, path)
	fragmentBefore := marketplaceState(t, fragment)
	editor := openMarketplaceEditor(t, path, RepoCommitted)
	binding, err := editor.EnsureMarketplaceSource("https://market.example")
	if err != nil {
		t.Fatal(err)
	}
	requireMarketplaceV4Code(t, editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{testMarketplacePin()}), "v4.edit-exclusion")
	candidate, err := editor.Render()
	if err != nil || candidate.Changed {
		t.Fatalf("refused include changed the candidate: %#v, %v", candidate, err)
	}
	requireMarketplaceUnchanged(t, path, before)
	requireMarketplaceUnchanged(t, fragment, fragmentBefore)
	// Control: a kind outside the inherited wildcard is still accepted.
	command := testMarketplacePin()
	command.Kind, command.Name = "command", "deploy"
	if err := editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{command}); err != nil {
		t.Fatalf("control include: %v", err)
	}
	if candidate, err = editor.Render(); err != nil || !candidate.Changed || !slices.Contains(marketplaceRequestLocators(editor.RenderedV4()), "source-slug/command/deploy") {
		t.Fatalf("control include candidate = %#v, %v", candidate, err)
	}
}

func TestUpdateMarketplaceRequestCandidateRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeMarketplaceV4(t, path, "sources:\n  public: {marketplace: https://market.example}\nresources:\n  public: [{source-slug/skill/review: {as: chosen-review, revision: sha256:1111111111111111111111111111111111111111111111111111111111111111}}]\n")
	before := marketplaceState(t, path)
	old := testMarketplacePin()
	old.InstalledName = "chosen-review"
	updated := old
	updated.Revision = "sha256:" + strings.Repeat("b", 64)
	snapshot, commit, err := commitMarketplaceEdit(path, RepoCommitted, func(editor *LayerEditor) error {
		binding, err := editor.EnsureMarketplaceSource("HTTPS://MARKET.example/")
		if err != nil {
			return err
		}
		if err := editor.RemoveMarketplacePins(binding, []MarketplaceResourcePin{old}); err != nil {
			return err
		}
		return editor.IncludeMarketplacePins(binding, []MarketplaceResourcePin{updated})
	})
	if err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v4.Requests) != 1 || v4.Requests[0].Options.As != "chosen-review" || v4.Requests[0].Options.Revision != "sha256:"+strings.Repeat("b", 64) || snapshot == nil || commit.Generation == "" {
		t.Fatalf("updated request/CAS = %#v/%#v", v4.Requests, commit)
	}
	if err := snapshot.RestoreIfCurrent(commit); err != nil {
		t.Fatal(err)
	}
	after := marketplaceState(t, path)
	if !bytes.Equal(after.data, before.data) || after.info.Mode() != before.info.Mode() {
		t.Fatal("rollback did not restore exact bytes and mode")
	}
}

func TestEnsureMarketplacePinSerializesConcurrentDistinctAdds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeMarketplaceV4(t, path, "")
	const count = 12
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pin := testMarketplacePin()
			pin.Name = fmt.Sprintf("review-%d", i)
			_, _, err := commitMarketplaceEdit(path, RepoCommitted, marketplaceIncludeEdit("HTTPS://MARKET.example/", pin))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"source-slug/skill/review-0", "source-slug/skill/review-1", "source-slug/skill/review-10", "source-slug/skill/review-11", "source-slug/skill/review-2", "source-slug/skill/review-3", "source-slug/skill/review-4", "source-slug/skill/review-5", "source-slug/skill/review-6", "source-slug/skill/review-7", "source-slug/skill/review-8", "source-slug/skill/review-9"}
	if len(v4.Sources) != 1 || !slices.Equal(marketplaceRequestLocators(v4), want) {
		t.Fatalf("concurrent sources/requests = %#v/%#v", v4.Sources, v4.Requests)
	}
}
