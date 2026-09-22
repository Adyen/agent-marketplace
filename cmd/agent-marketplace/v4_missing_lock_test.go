package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestWritingCommandsCreateMissingV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "update", "lock"} {
		for _, scope := range []lockfile.Scope{lockfile.ScopeUser, lockfile.ScopeRepo} {
			t.Run(command+"/"+string(scope), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
				server := newV4MarketplaceLockServer(t)
				defer server.Close()
				repo := t.TempDir()
				configRoot := repo
				if scope == lockfile.ScopeUser {
					configRoot = home
				}
				configPath := filepath.Join(configRoot, ".agents", "config.yaml")
				writeConfig(t, configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
				before, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				withWorkingDir(t, repo)

				output, err := executeCommand(command)
				if err != nil {
					t.Fatalf("%s: %v\n%s", command, err, output)
				}
				after, err := os.ReadFile(configPath)
				if err != nil || string(after) != string(before) {
					t.Fatalf("%s changed authored config: %v\nbefore:\n%s\nafter:\n%s", command, err, before, after)
				}
				doc := readLockDocument(t, filepath.Join(configRoot, ".agents", "lock.yaml"))
				if doc.APIVersion != lockfile.APIVersionV2 || doc.Scope != scope || len(doc.Marketplaces) != 1 {
					t.Fatalf("%s lock header = %#v", command, doc)
				}
				locked := doc.Marketplaces[0]
				if locked.URL != server.URL || locked.InstanceID != "mp" || len(locked.Resources) != 1 {
					t.Fatalf("%s Marketplace lock = %#v", command, locked)
				}
				resource := locked.Resources[0]
				if resource.RegistryName != "tools" || resource.RegistryID != "src" || resource.Kind != "skill" || resource.Name != "review" || resource.ArchiveDigest == "" {
					t.Fatalf("%s resource lock = %#v", command, resource)
				}
				if command == "sync" && scope == lockfile.ScopeRepo {
					if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md")); err != nil {
						t.Fatalf("%s did not materialize review: %v", command, err)
					}
				}
			})
		}
	}
}

func TestMissingV4MarketplaceLockHonorsAllowlistBeforeNetwork(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v2\nallowed_marketplaces: ['^https://allowed\\.example$']\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync"); err == nil {
		t.Fatalf("sync unexpectedly bypassed Marketplace allowlist:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("disallowed Marketplace received %d requests", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("disallowed Marketplace created a lock: %v", err)
	}
}

func TestSyncLockedDoesNotBootstrapMissingV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/skill/review]}
`)
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync", "--locked"); err == nil {
		t.Fatalf("sync --locked unexpectedly bootstrapped a lock:\n%s", output)
	}
	if requests.Load() != 0 {
		t.Fatalf("sync --locked made %d Marketplace requests", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("sync --locked created a lock: %v", err)
	}
}

func TestLockCreatesMissingEndpointOnlyV4MarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
	}))
	defer server.Close()
	repo := t.TempDir()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+server.URL+"}\n")
	withWorkingDir(t, repo)

	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("lock endpoint-only source: %v\n%s", err, output)
	}
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != "mp" || len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("endpoint-only lock = %#v", doc)
	}
}

func newV4MarketplaceLockServer(t *testing.T) *httptest.Server {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "tools"
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/review") || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/review"):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
}
