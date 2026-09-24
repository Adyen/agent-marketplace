package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func newSelfCanonicalV4MarketplaceServer(t *testing.T, instanceID, resourceName string) *httptest.Server {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(resourceName, "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
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
		Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(instanceID), Registry: "src", Kind: "skill", Name: resourceName},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+resourceName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+resourceName):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestInstallMarketplaceEndpointIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newSelfCanonicalV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL)
	if err != nil {
		t.Fatalf("install Marketplace endpoint: %v\n%s", err, output)
	}
	if output != "Added Marketplace "+server.URL+" to "+configPath+"\n" {
		t.Fatalf("output = %q", output)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "marketplace: "+server.URL) {
		t.Fatalf("v4 config missing marketplace source:\n%s", data)
	}
	if strings.Contains(string(data), "marketplaces:") {
		t.Fatalf("v4 config gained legacy marketplaces key:\n%s", data)
	}
	doc := readLockDocument(t, lockPath)
	if doc.APIVersion != lockfile.APIVersionV2 || len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != server.URL || doc.Marketplaces[0].InstanceID != "mp" {
		t.Fatalf("endpoint lock = %#v", doc)
	}
	if len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("endpoint-only lock gained resources: %#v", doc.Marketplaces[0].Resources)
	}
	configAfter, _ := os.ReadFile(configPath)

	output, err = executeCommand("install", server.URL)
	if err != nil {
		t.Fatalf("repeat endpoint install: %v\n%s", err, output)
	}
	if !strings.Contains(output, "already") {
		t.Fatalf("repeat output = %q", output)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(configAfter) {
		t.Fatal("repeat registration rewrote config")
	}
}

func TestInstallMarketplaceResourceIntoV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newSelfCanonicalV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tools/skill/review") {
		t.Fatalf("v4 config missing exact request locator:\n%s", data)
	}
	if strings.Contains(string(data), "marketplaces:") || strings.Contains(string(data), "registries:") {
		t.Fatalf("v4 config gained legacy keys:\n%s", data)
	}
	doc := readLockDocument(t, lockPath)
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != server.URL {
		t.Fatalf("lock endpoints = %#v", doc.Marketplaces)
	}
	var record *lockfile.MarketplaceResourceLock
	for i := range doc.Marketplaces[0].Resources {
		if doc.Marketplaces[0].Resources[i].Kind == "skill" && doc.Marketplaces[0].Resources[i].Name == "review" {
			record = &doc.Marketplaces[0].Resources[i]
		}
	}
	if record == nil || record.RegistryName != "tools" || record.RegistryID != "src" || record.ArchiveDigest == "" || string(record.ContentDigest) == "" {
		t.Fatalf("resource lock = %#v", doc.Marketplaces[0].Resources)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("review was not materialized: %v", err)
	}
}

func TestInstallMarketplaceResourceIntoV4RepoConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	server := newSelfCanonicalV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "repo", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tools/skill/review") {
		t.Fatalf("v4 repo config missing exact request locator:\n%s", data)
	}
	doc := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if doc.Scope != lockfile.ScopeRepo || len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("repo lock = %#v", doc)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("review was not materialized: %v", err)
	}
}

func TestRemoveMarketplaceResourceFromV4UserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newSelfCanonicalV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("review was not materialized: %v", err)
	}

	output, err = executeCommand("remove", "review", "--scope", "user")
	if err != nil {
		t.Fatalf("remove Marketplace resource: %v\n%s", err, output)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tools/skill/review") {
		t.Fatalf("v4 config retained removed request:\n%s", data)
	}
	if !strings.Contains(string(data), "marketplace:") {
		t.Fatalf("v4 config lost endpoint source:\n%s", data)
	}
	doc := readLockDocument(t, lockPath)
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != "mp" {
		t.Fatalf("lock endpoints = %#v", doc.Marketplaces)
	}
	if len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("stale lock resources = %#v", doc.Marketplaces[0].Resources)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review")); !os.IsNotExist(err) {
		t.Fatalf("removed skill still materialized: %v", err)
	}
}
