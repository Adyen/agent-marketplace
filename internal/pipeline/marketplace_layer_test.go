package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

type layerMarketplaceResource struct {
	kind          string
	name          string
	revision      store.AssessedResourceRevision
	raw           []byte
	archiveDigest string
	availability  string
}

type layerMarketplaceServer struct {
	*httptest.Server
	requests   atomic.Int32
	paths      []string
	pathsMu    sync.Mutex
	instanceID string
	registries map[string]store.Registry
	resources  map[string]layerMarketplaceResource
	manifests  map[string][]byte
}

func newLayerMarketplaceServer(t *testing.T) *layerMarketplaceServer {
	t.Helper()
	fake := &layerMarketplaceServer{
		instanceID: "mp",
		registries: map[string]store.Registry{},
		resources:  map[string]layerMarketplaceResource{},
		manifests:  map[string][]byte{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(fake.instanceID, "http://"+r.Host))
	})
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/api/v1/registries/")
		registry, ok := fake.registries[key]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{
			ID: registry.ID, Slug: registry.Slug, Aliases: registry.Aliases, Origins: registry.Origins,
			Ref: registry.Ref, Roots: registry.Roots, Acquisition: registry.Acquisition,
			License: registry.License, Listed: registry.Listed,
		})
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/resources/"), "/")
		if len(segments) != 3 && len(segments) != 5 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		resource, ok := fake.resources[segments[0]+"/"+segments[1]+"/"+segments[2]]
		if !ok {
			if registry, routed := fake.registries[segments[0]]; routed {
				resource, ok = fake.resources[registry.Slug+"/"+segments[1]+"/"+segments[2]]
			}
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if len(segments) == 5 && (segments[3] != "revisions" || segments[4] != string(resource.revision.ContentDigest)) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		response := struct {
			store.AssessedResourceRevision
			CurrentAvailability any `json:"current_availability,omitempty"`
		}{AssessedResourceRevision: resource.revision}
		if resource.availability != "" {
			response.CurrentAvailability = map[string]any{"availability": resource.availability}
		}
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/api/v1/artifacts/sha256/", func(w http.ResponseWriter, r *http.Request) {
		hexDigest := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/sha256/")
		for _, resource := range fake.resources {
			if strings.TrimPrefix(resource.archiveDigest, "sha256:") == hexDigest {
				w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
				_, _ = w.Write(resource.raw)
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux.HandleFunc("/api/v1/manifests/", func(w http.ResponseWriter, r *http.Request) {
		payload, ok := fake.manifests[strings.TrimPrefix(r.URL.Path, "/api/v1/manifests/")]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.requests.Add(1)
		fake.pathsMu.Lock()
		fake.paths = append(fake.paths, r.Method+" "+r.URL.Path)
		fake.pathsMu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (f *layerMarketplaceServer) addRegistry(t *testing.T, slug string) store.Registry {
	t.Helper()
	registry := store.Registry{
		ID: store.RegistryID(slug + "-id"), Slug: slug, Origins: []string{"https://example.com/" + slug + ".git"},
		Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true,
	}
	f.registries[slug] = registry
	f.registries[string(registry.ID)] = registry
	return registry
}

func layerAssessedRevision(revision store.ResourceRevision) store.AssessedResourceRevision {
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
		AnalyzerVersion: "test-analyzer", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		panic(err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
			Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
}

func (f *layerMarketplaceServer) addSkill(t *testing.T, registry store.Registry, name string) {
	t.Helper()
	f.setSkill(t, registry, name, name+" body")
}

func (f *layerMarketplaceServer) setSkill(t *testing.T, registry store.Registry, name, body string) {
	t.Helper()
	skill := ir.NewSkill(name, name+" description", body, []string{"user"}, nil, ir.SourceRef{})
	artifact, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	artifact.Manifest.RegistryID = string(registry.ID)
	artifact.Manifest.RegistrySlug = registry.Slug
	artifact.Manifest.Revision = artifact.Manifest.ContentDigest
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	f.resources[registry.Slug+"/skill/"+name] = layerMarketplaceResource{
		kind: "skill", name: name, raw: raw, archiveDigest: archiveDigest,
		revision: layerAssessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(f.instanceID), Registry: registry.ID, Kind: "skill", Name: name},
			ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}),
	}
}

func (f *layerMarketplaceServer) addCollection(t *testing.T, registry store.Registry, name string, members []hccartifact.CollectionMemberEntry) {
	t.Helper()
	payload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion: hccartifact.CollectionSchemaV1, Name: name, Members: members,
	})
	if err != nil {
		t.Fatalf("EncodeCollectionManifest: %v", err)
	}
	f.manifests[manifestDigest] = payload
	f.resources[registry.Slug+"/collection/"+name] = layerMarketplaceResource{
		kind: "collection", name: name,
		revision: layerAssessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(f.instanceID), Registry: registry.ID, Kind: "collection", Name: name},
			ContentDigest: store.RevisionDigest(manifestDigest),
		}),
	}
}

func writeFileMode(t *testing.T, path string, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func layerMarketplaceLockDoc(t *testing.T, lockPath string) lockfile.Document {
	t.Helper()
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock %q: %v", lockPath, err)
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		t.Fatalf("decode lock %q: %v", lockPath, err)
	}
	return doc
}

func marketplaceLockResource(doc lockfile.Document, kind, name string) *lockfile.MarketplaceResourceLock {
	for i := range doc.Marketplaces {
		for j := range doc.Marketplaces[i].Resources {
			if doc.Marketplaces[i].Resources[j].Kind == kind && doc.Marketplaces[i].Resources[j].Name == name {
				return &doc.Marketplaces[i].Resources[j]
			}
		}
	}
	return nil
}

func TestMutateMarketplaceLayerV4EnsureEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}

	for i := 0; i < 2; i++ {
		configBefore, _ := os.ReadFile(configPath)
		var lockBefore []byte
		lockBefore, _ = os.ReadFile(lockPath)
		configStat, _ := os.Stat(configPath)
		if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
			return m.EnsureEndpoint(entry)
		}, nil); err != nil {
			t.Fatalf("registration %d: %v", i, err)
		}
		configAfter, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(configAfter), "marketplace: "+fake.URL) {
			t.Fatalf("v4 config missing marketplace source:\n%s", configAfter)
		}
		if strings.Contains(string(configAfter), "marketplaces:") {
			t.Fatalf("v4 config gained legacy marketplaces key:\n%s", configAfter)
		}
		doc := layerMarketplaceLockDoc(t, lockPath)
		if doc.APIVersion != lockfile.APIVersionV2 {
			t.Fatalf("lock apiVersion = %q", doc.APIVersion)
		}
		if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != fake.URL || doc.Marketplaces[0].InstanceID != fake.instanceID {
			t.Fatalf("lock endpoint records = %#v", doc.Marketplaces)
		}
		if i == 0 {
			continue
		}
		lockAfter, _ := os.ReadFile(lockPath)
		if string(configAfter) != string(configBefore) || string(lockAfter) != string(lockBefore) {
			t.Fatal("second registration changed config or lock bytes")
		}
		statAfter, _ := os.Stat(configPath)
		if statAfter.ModTime() != configStat.ModTime() {
			t.Fatal("second registration rewrote config")
		}
	}
}

func TestMutateMarketplaceLayerV4IncludePins(t *testing.T) {
	for _, scope := range []struct {
		name       string
		source     config.Source
		configPath func(home, repo string) string
		lockPath   func(home, repo string) string
	}{
		{name: "user", source: config.UserPersonal,
			configPath: func(home, repo string) string { return filepath.Join(home, ".agents", "config.yaml") },
			lockPath:   func(home, repo string) string { return filepath.Join(home, ".agents", "lock.yaml") }},
		{name: "repo", source: config.RepoCommitted,
			configPath: func(home, repo string) string { return filepath.Join(repo, ".agents", "config.yaml") },
			lockPath:   func(home, repo string) string { return filepath.Join(repo, ".agents", "lock.yaml") }},
		{name: "local", source: config.RepoPersonal,
			configPath: func(home, repo string) string { return filepath.Join(repo, ".agents", "config.local.yaml") },
			lockPath:   func(home, repo string) string { return filepath.Join(repo, ".agents", "lock.local.yaml") }},
	} {
		t.Run(scope.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			repo := t.TempDir()
			userConfig := filepath.Join(home, ".agents", "config.yaml")
			writeFileMode(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
			configPath := scope.configPath(home, repo)
			lockPath := scope.lockPath(home, repo)
			if configPath != userConfig {
				writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\n", 0o600)
			}
			fake := newLayerMarketplaceServer(t)
			registry := fake.addRegistry(t, "source")
			fake.addSkill(t, registry, "review")

			pins := layerPreparePins(t, fake, "skill", "review")
			if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, scope.source, func(m *MarketplaceLayerMutation) error {
				return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins)
			}, nil); err != nil {
				t.Fatalf("MutateMarketplaceLayer: %v", err)
			}
			configData, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(configData), "source/skill/review") {
				t.Fatalf("v4 config missing exact request locator:\n%s", configData)
			}
			if strings.Contains(string(configData), "marketplaces:") || strings.Contains(string(configData), "registries:") {
				t.Fatalf("v4 config gained legacy keys:\n%s", configData)
			}
			doc := layerMarketplaceLockDoc(t, lockPath)
			if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != fake.URL {
				t.Fatalf("lock endpoints = %#v", doc.Marketplaces)
			}
			record := marketplaceLockResource(doc, "skill", "review")
			if record == nil {
				t.Fatalf("lock records = %#v", doc.Marketplaces)
			}
			if record.RegistryName != "source" || record.RegistryID != string(registry.ID) || record.Revision == "" || string(record.ContentDigest) == "" || record.ArchiveDigest == "" {
				t.Fatalf("lock resource record = %#v", record)
			}
			if record.Acquisition != lockfile.MarketplaceAcquisitionMirror {
				t.Fatalf("acquisition = %#v", record.Acquisition)
			}
			stat, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if stat.Mode().Perm() != 0o600 {
				t.Fatalf("config mode = %v", stat.Mode().Perm())
			}
		})
	}
}

func TestToggleMarketplacePinSubdirectorySync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	child := filepath.Join(repo, "component")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	pin := layerPreparePins(t, fake, "skill", "review")[0]
	configPath := filepath.Join(child, ".agents", "config.local.yaml")
	if _, err := MutateMarketplaceLayer(context.Background(), repo, child, configPath, config.RepoPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, []config.MarketplaceResourcePin{pin})
	}, nil); err != nil {
		t.Fatalf("seed child pin: %v", err)
	}
	if err := ToggleMarketplacePin(context.Background(), repo, child, configPath, config.RepoPersonal, entry, pin, true); err != nil {
		t.Fatalf("enable in child: %v", err)
	}
	childSkill := filepath.Join(child, ".devin", "skills", "review", "SKILL.md")
	parentSkill := filepath.Join(repo, ".devin", "skills", "review", "SKILL.md")
	if _, err := os.Stat(childSkill); err != nil {
		t.Fatalf("child skill not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(parentSkill)); !os.IsNotExist(err) {
		t.Fatalf("toggle wrote parent skill: %v", err)
	}
	if err := ToggleMarketplacePin(context.Background(), repo, child, configPath, config.RepoPersonal, entry, pin, false); err != nil {
		t.Fatalf("disable in child: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(childSkill)); !os.IsNotExist(err) {
		t.Fatalf("child skill still present after disable: %v", err)
	}
}

func TestMutateMarketplaceLayerV4ReinstallClearsExactExclusion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  mp: {marketplace: "+fake.URL+"}\nresources:\n  mp: {exclude: [source/skill/review, source/skill/other]}\n", 0o600)
	pins := layerPreparePins(t, fake, "skill", "review")
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins)
	}, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	loaded, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load installed config and lock: %v", err)
	}
	if len(loaded.Marketplaces) != 1 || len(loaded.Marketplaces[0].Resources) != 1 || loaded.MarketplacePinExcluded(loaded.Marketplaces[0], loaded.Marketplaces[0].Resources[0]) {
		t.Fatalf("installed resource is not selected: %#v", loaded)
	}
	if len(loaded.MarketplaceExclusions) != 1 || loaded.MarketplaceExclusions[0].Locator != "source/skill/other" {
		t.Fatalf("unrelated exclusion lost: %#v", loaded.MarketplaceExclusions)
	}
}

func layerPreparePins(t *testing.T, fake *layerMarketplaceServer, kind, name string) []config.MarketplaceResourcePin {
	t.Helper()
	resolver := &marketplace.Resolver{}
	plan, err := resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID},
		marketplace.MarketplaceLocator{URL: fake.URL, Registry: "source", Kind: kind, Name: name},
		[]hcc.Target{hcc.TargetDevin})
	if err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	return marketplaceInstallPins(plan)
}

func TestMutateMarketplaceLayerV4Collection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")
	member := fake.resources["source/skill/review"]
	fake.addCollection(t, registry, "bundle", []hccartifact.CollectionMemberEntry{
		{Kind: "skill", Name: "review", RegistryID: string(registry.ID), Revision: string(member.revision.ContentDigest)},
	})

	resolver := &marketplace.Resolver{}
	plan, err := resolver.PrepareLocator(context.Background(), config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID},
		marketplace.MarketplaceLocator{URL: fake.URL, Registry: "source", Kind: "collection", Name: "bundle"}, nil)
	if err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	pins := marketplaceInstallPins(plan)
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}, pins)
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configData), "source/collection/bundle") {
		t.Fatalf("v4 config missing collection request:\n%s", configData)
	}
	if strings.Contains(string(configData), "skill/review") {
		t.Fatalf("v4 config should not request collection leaves:\n%s", configData)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	collection := marketplaceLockResource(doc, "collection", "bundle")
	if collection == nil {
		t.Fatalf("lock records = %#v", doc.Marketplaces)
	}
	leaf := marketplaceLockResource(doc, "skill", "review")
	if leaf == nil {
		t.Fatalf("lock missing collection leaf: %#v", doc.Marketplaces)
	}
	if len(leaf.SelectedBy) != 1 || leaf.SelectedBy[0].Name != "bundle" {
		t.Fatalf("leaf selection edges = %#v", leaf.SelectedBy)
	}
}

func TestMutateMarketplaceLayerV4PreservesGitLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	commitOID, err := lockfile.ParseGitOID("sha1:" + strings.Repeat("f", 40))
	if err != nil {
		t.Fatal(err)
	}
	existing := lockfile.Document{
		APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeUser,
		Registries: []lockfile.GitRegistry{{URL: "https://example.com/git-registry.git", Ref: "main", Commit: commitOID, Roots: []lockfile.GitRoot{{Path: ".agents", SourceHarness: "claude", RevisionSchema: "v4", RevisionDigest: lockfile.Digest("sha256:" + strings.Repeat("e", 64))}}}},
	}
	lockData, err := lockfile.Encode(existing)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, lockPath, string(lockData), 0o600)
	fake := newLayerMarketplaceServer(t)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID})
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	doc := layerMarketplaceLockDoc(t, lockPath)
	if len(doc.Registries) != 1 || doc.Registries[0].URL != "https://example.com/git-registry" || doc.Registries[0].Commit != commitOID {
		t.Fatalf("git lock records = %#v", doc.Registries)
	}
	if len(doc.Marketplaces) != 1 {
		t.Fatalf("lock endpoints = %#v", doc.Marketplaces)
	}
	stat, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %v", stat.Mode().Perm())
	}
}

func TestMutateMarketplaceLayerV4BatchValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)
	registry := fake.addRegistry(t, "source")
	fake.addSkill(t, registry, "review")

	pins := layerPreparePins(t, fake, "skill", "review")
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}

	dup := append(append([]config.MarketplaceResourcePin{}, pins...), pins...)
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, dup)
	}, nil); err != nil {
		t.Fatalf("identical duplicate batch: %v", err)
	}
	configData, _ := os.ReadFile(configPath)
	if strings.Count(string(configData), "source/skill/review") != 1 {
		t.Fatalf("duplicate batch wrote duplicate requests:\n%s", configData)
	}

	conflicting := append(append([]config.MarketplaceResourcePin{}, pins...), func() config.MarketplaceResourcePin {
		pin := pins[0]
		pin.InstalledName = "renamed"
		return pin
	}())
	configNow, _ := os.ReadFile(configPath)
	lockNow, _ := os.ReadFile(lockPath)
	_, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, conflicting)
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "v4.duplicate-request") {
		t.Fatalf("conflicting batch error = %v", err)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configNow) {
		t.Fatal("conflicting batch mutated config")
	}
	if after, _ := os.ReadFile(lockPath); string(after) != string(lockNow) {
		t.Fatal("conflicting batch mutated lock")
	}

	invalid := []config.MarketplaceResourcePin{{Kind: "skill", Name: "bogus"}}
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, invalid)
	}, nil); err == nil {
		t.Fatal("invalid pin accepted")
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configNow) {
		t.Fatal("invalid batch mutated config")
	}
}

func TestMutateMarketplaceLayerV4SyncFailureRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n", 0o600)
	configBefore, _ := os.ReadFile(configPath)
	fake := newLayerMarketplaceServer(t)
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	syncErr := errors.New("sync exploded")

	_, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(entry)
	}, func() ([]error, error) { return nil, syncErr })
	if err == nil || !strings.Contains(err.Error(), "sync exploded") {
		t.Fatalf("sync failure error = %v", err)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configBefore) {
		t.Fatalf("config not rolled back:\n%s", after)
	}
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Fatal("lock not rolled back")
	}

	concurrent := []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\n# concurrent writer\n")
	_, err = MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(entry)
	}, func() ([]error, error) {
		if writeErr := os.WriteFile(configPath, concurrent, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		return nil, syncErr
	})
	if err == nil {
		t.Fatal("concurrent write + sync failure returned success")
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(concurrent) {
		t.Fatalf("concurrent config write not preserved:\n%s", after)
	}
}

func TestMutateMarketplaceLayerV4IncludeRootOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	fragment := filepath.Join(home, ".agents", "fragment.yaml")
	writeFileMode(t, fragment, "harnesses: [devin]\n", 0o600)
	configPath := filepath.Join(home, ".agents", "config.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v4\ninclude: [fragment.yaml]\n", 0o600)
	fragmentBefore, _ := os.ReadFile(fragment)
	fake := newLayerMarketplaceServer(t)

	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID})
	}, nil); err != nil {
		t.Fatalf("MutateMarketplaceLayer: %v", err)
	}
	configData, _ := os.ReadFile(configPath)
	if !strings.Contains(string(configData), "marketplace: "+fake.URL) {
		t.Fatalf("root config missing marketplace source:\n%s", configData)
	}
	if after, _ := os.ReadFile(fragment); string(after) != string(fragmentBefore) {
		t.Fatal("included fragment was modified")
	}
}

func TestMutateMarketplaceLayerRefusesUnsupportedEndpointRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	writeFileMode(t, configPath, "apiVersion: agent-marketplace/v2\nharnesses: [devin]\n", 0o600)
	fake := newLayerMarketplaceServer(t)

	before, _ := os.ReadFile(configPath)
	called := false
	_, err := MutateMarketplaceLayer(context.Background(), repo, repo, configPath, config.UserPersonal, func(m *MarketplaceLayerMutation) error {
		called = true
		return m.EnsureEndpoint(config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID})
	}, nil)
	var validation *config.V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != configPath {
		t.Fatalf("format refusal: %v", err)
	}
	if called || fake.requests.Load() != 0 {
		t.Fatalf("new work started: callback=%t, requests=%d", called, fake.requests.Load())
	}
	configData, _ := os.ReadFile(configPath)
	if string(configData) != string(before) {
		t.Fatal("refusal changed root")
	}
}
