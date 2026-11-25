package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/tui"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/charmbracelet/x/ansi"
)

func newSelfCanonicalV4MarketplaceServer(t *testing.T, instanceID, resourceName string) *httptest.Server {
	return newV4MarketplaceServer(t, instanceID, resourceName, true, nil, nil)
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

func newUpdatableV4MarketplaceServer(t *testing.T, instanceID, resourceName string) (*httptest.Server, func(), func() string) {
	t.Helper()
	build := func(body string) ([]byte, store.AssessedResourceRevision) {
		canonical, err := hccartifact.FromIR(ir.NewSkill(resourceName, "Review", body, nil, nil, ir.SourceRef{}))
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
		return raw, revision
	}
	oldRaw, oldRevision := build("Review carefully.")
	newRaw, newRevision := build("Review carefully, updated.")
	var updated atomic.Bool
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+resourceName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+resourceName):
			if updated.Load() {
				_ = json.NewEncoder(w).Encode(newRevision)
				return
			}
			_ = json.NewEncoder(w).Encode(oldRevision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			if strings.HasSuffix(r.URL.Path, strings.TrimPrefix(string(newRevision.ArchiveDigest), "sha256:")) || strings.HasSuffix(r.URL.Path, string(newRevision.ArchiveDigest)) {
				_, _ = w.Write(newRaw)
				return
			}
			_, _ = w.Write(oldRaw)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, func() { updated.Store(true) }, func() string { return string(newRevision.ContentDigest) }
}

func TestUpdateMarketplaceV4ReplacesLockNotConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, upgrade, latestDigest := newUpdatableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configStat, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	oldDoc := readLockDocument(t, lockPath)
	if len(oldDoc.Marketplaces) != 1 || len(oldDoc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("installed lock = %#v", oldDoc.Marketplaces)
	}
	oldDigest := string(oldDoc.Marketplaces[0].Resources[0].ContentDigest)

	upgrade()
	output, err = executeCommand("update")
	if err != nil {
		t.Fatalf("update: %v\n%s", err, output)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(configBytes) {
		t.Fatalf("v4 config changed:\n%s", after)
	}
	if afterStat, err := os.Stat(configPath); err != nil || afterStat.Mode() != configStat.Mode() || !afterStat.ModTime().Equal(configStat.ModTime()) {
		t.Fatalf("v4 config mode/mtime changed: %v", err)
	}
	doc := readLockDocument(t, lockPath)
	if len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("updated lock = %#v", doc.Marketplaces)
	}
	record := doc.Marketplaces[0].Resources[0]
	if string(record.ContentDigest) != latestDigest() || string(record.ContentDigest) == oldDigest {
		t.Fatalf("lock record = %#v, want latest digest %q", record, latestDigest())
	}
	body, err := os.ReadFile(filepath.Join(home, ".factory", "skills", "review", "SKILL.md"))
	if err != nil {
		t.Fatalf("updated output missing: %v", err)
	}
	if !strings.Contains(string(body), "updated") {
		t.Fatalf("materialized output not refreshed:\n%s", body)
	}
}

func newDriftableV4MarketplaceServer(t *testing.T, instanceID, resourceName string) (*httptest.Server, func() string, func(string)) {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(resourceName, "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "tools"
	var version atomic.Int64
	var state atomic.Pointer[struct {
		raw      []byte
		revision store.AssessedResourceRevision
	}]
	set := func() string {
		raw, archiveDigest, err := hccartifact.Encode(canonical)
		if err != nil {
			t.Fatal(err)
		}
		revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(instanceID), Registry: "src", Kind: "skill", Name: resourceName},
			ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		})
		state.Store(&struct {
			raw      []byte
			revision store.AssessedResourceRevision
		}{raw: raw, revision: revision})
		return archiveDigest
	}
	set()
	var broken sync.Map
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := state.Load()
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+resourceName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+resourceName):
			_ = json.NewEncoder(w).Encode(current.revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			digest := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/")
			if _, bad := broken.Load(strings.Replace(digest, "/", ":", 1)); bad {
				http.Error(w, "artifact unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(current.raw)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, func() string {
			n := version.Add(1)
			if canonical.Assets == nil {
				canonical.Assets = map[string][]byte{}
			}
			canonical.Assets[fmt.Sprintf("drift-%d.txt", n)] = []byte(fmt.Sprintf("drift %d", n))
			return set()
		}, func(digest string) {
			broken.Store(digest, true)
		}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestSyncRefreshesDriftedLockArchiveEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldDoc := readLockDocument(t, lockPath)
	if len(oldDoc.Marketplaces) != 1 || len(oldDoc.Marketplaces[0].Resources) != 1 {
		t.Fatalf("installed lock = %#v", oldDoc.Marketplaces)
	}
	oldRecord := oldDoc.Marketplaces[0].Resources[0]
	if oldRecord.ArchiveDigest == "" || string(oldRecord.ContentDigest) == "" {
		t.Fatalf("installed record lacks evidence: %#v", oldRecord)
	}

	newDigest := drift()
	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("sync after archive drift: %v\n%s", err, output)
	}
	doc := readLockDocument(t, lockPath)
	record := doc.Marketplaces[0].Resources[0]
	if string(record.ArchiveDigest) != newDigest {
		t.Fatalf("lock archive digest = %q, want refreshed %q", record.ArchiveDigest, newDigest)
	}
	if record.ContentDigest != oldRecord.ContentDigest || record.Revision != oldRecord.Revision {
		t.Fatalf("refresh changed revision identity: %#v", record)
	}
	if doc.APIVersion != oldDoc.APIVersion || doc.Scope != oldDoc.Scope {
		t.Fatalf("lock byte format changed: %#v", doc)
	}
	body, err := os.ReadFile(filepath.Join(home, ".factory", "skills", "review", "SKILL.md"))
	if err != nil || !strings.Contains(string(body), "Review carefully") {
		t.Fatalf("materialized output broken: %v", err)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestSyncLockedRejectsDriftedArchiveEvidenceAndUpdateHeals(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldDoc := readLockDocument(t, lockPath)
	oldRecord := oldDoc.Marketplaces[0].Resources[0]

	newDigest := drift()
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err = executeCommand("sync", "--locked")
	if err == nil {
		t.Fatalf("sync --locked accepted drifted evidence:\n%s", output)
	}
	if !strings.Contains(output, "locked archive digest drifted") || !strings.Contains(output, "sync") || !strings.Contains(output, "update") {
		t.Fatalf("locked error not actionable: %v\n%s", err, output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("sync --locked rewrote the lock: %v", err)
	}

	output, err = executeCommand("update")
	if err != nil {
		t.Fatalf("update after archive drift: %v\n%s", err, output)
	}
	if !strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("update emitted no archive-evidence-refresh diagnostic:\n%s", output)
	}
	if !strings.Contains(output, string(oldRecord.ArchiveDigest)) || !strings.Contains(output, newDigest) {
		t.Fatalf("update diagnostic lacks old/new archive evidence:\n%s", output)
	}
	if strings.Contains(output, fmt.Sprintf("updated %q -> %q", oldRecord.ContentDigest, oldRecord.ContentDigest)) {
		t.Fatalf("update misreported unchanged content as a revision advance:\n%s", output)
	}
	doc := readLockDocument(t, lockPath)
	record := doc.Marketplaces[0].Resources[0]
	if string(record.ArchiveDigest) != newDigest || record.ContentDigest != oldRecord.ContentDigest {
		t.Fatalf("update did not heal lock evidence: %#v", record)
	}

	output, err = executeCommand("update")
	if err != nil {
		t.Fatalf("healthy update: %v\n%s", err, output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("healthy update emitted archive refresh diagnostic:\n%s", output)
	}
	if !strings.Contains(output, "content digest unchanged") {
		t.Fatalf("healthy update output changed:\n%s", output)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestUpdateHealsDriftedArchiveEvidenceForCollectionMember(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift := newDriftableV4MarketplaceCollectionServer(t, "mp", "bundle", "review")
	defer server.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("install", server.URL+"/r/tools/collection/bundle", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("install Marketplace collection: %v\n%s", err, output)
	}
	records := func() map[string]lockfile.MarketplaceResourceLock {
		doc := readLockDocument(t, lockPath)
		if len(doc.Marketplaces) != 1 {
			t.Fatalf("lock marketplaces = %#v", doc.Marketplaces)
		}
		byKind := map[string]lockfile.MarketplaceResourceLock{}
		for _, record := range doc.Marketplaces[0].Resources {
			byKind[record.Kind] = record
		}
		return byKind
	}
	before := records()
	if before["collection"].Name != "bundle" || before["skill"].Name != "review" || len(before["skill"].SelectedBy) == 0 {
		t.Fatalf("installed lock lacks a collection-selected member: %#v", before)
	}

	newDigest := drift()
	if output, err := executeCommand("sync", "--locked"); err == nil || !strings.Contains(output, "locked archive digest drifted") {
		t.Fatalf("sync --locked accepted drifted member evidence: %v\n%s", err, output)
	}

	output, err := executeCommand("update")
	if err != nil {
		t.Fatalf("update after member archive drift: %v\n%s", err, output)
	}
	if !strings.Contains(output, "marketplace.archive-evidence-refreshed") || !strings.Contains(output, newDigest) {
		t.Fatalf("update did not report the member's archive-evidence refresh:\n%s", output)
	}
	after := records()
	member := after["skill"]
	if string(member.ArchiveDigest) != newDigest || member.ContentDigest != before["skill"].ContentDigest || member.Revision != before["skill"].Revision {
		t.Fatalf("update did not heal member lock evidence: %#v", member)
	}
	if fmt.Sprint(member.SelectedBy) != fmt.Sprint(before["skill"].SelectedBy) {
		t.Fatalf("update changed member selection: %#v, was %#v", member, before["skill"])
	}
	if fmt.Sprint(after["collection"]) != fmt.Sprint(before["collection"]) {
		t.Fatalf("update changed the unchanged collection record: %#v, was %#v", after["collection"], before["collection"])
	}
	for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v after update healed the lock: %v\n%s", args, err, output)
		}
	}

	lockBefore := readTestFile(t, lockPath)
	output, err = executeCommand("update")
	if err != nil {
		t.Fatalf("healthy update: %v\n%s", err, output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") || !strings.Contains(output, "leaf is pinned by a collection revision") {
		t.Fatalf("healthy update output changed:\n%s", output)
	}
	if !bytes.Equal(lockBefore, readTestFile(t, lockPath)) {
		t.Fatal("healthy update rewrote the lock")
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestSyncRefreshesDriftedArchiveEvidenceAcrossEndpoints(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	serverA, driftA, _ := newDriftableV4MarketplaceServer(t, "mp-a", "review")
	defer serverA.Close()
	serverB, driftB, _ := newDriftableV4MarketplaceServer(t, "mp-b", "audit")
	defer serverB.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	for _, tc := range []struct {
		server *httptest.Server
		name   string
	}{{serverA, "review"}, {serverB, "audit"}} {
		if output, err := executeCommand("install", tc.server.URL+"/r/tools/skill/"+tc.name, "--scope", "user", "--yes"); err != nil {
			t.Fatalf("install from %s: %v\n%s", tc.server.URL, err, output)
		}
	}
	newA, newB := driftA(), driftB()
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync after dual-endpoint drift: %v\n%s", err, output)
	}
	if !strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("sync emitted no evidence-refresh diagnostic:\n%s", output)
	}
	doc := readLockDocument(t, lockPath)
	if len(doc.Marketplaces) != 2 {
		t.Fatalf("lock marketplaces = %#v", doc.Marketplaces)
	}
	got := map[string]string{}
	for _, mp := range doc.Marketplaces {
		if len(mp.Resources) != 1 {
			t.Fatalf("lock resources for %s = %#v", mp.URL, mp.Resources)
		}
		got[mp.URL] = string(mp.Resources[0].ArchiveDigest)
	}
	if got[serverA.URL] != newA || got[serverB.URL] != newB {
		t.Fatalf("refreshed digests = %#v, want %q for %s and %q for %s", got, newA, serverA.URL, newB, serverB.URL)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestSyncFailedArchiveReacquisitionPreservesLastGood(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, failArtifact := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldDoc := readLockDocument(t, lockPath)
	oldRecord := oldDoc.Marketplaces[0].Resources[0]
	installedPath := filepath.Join(home, ".factory", "skills", "review", "SKILL.md")
	installedBefore, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatal(err)
	}
	failArtifact(drift())

	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err = executeCommand("sync")
	if err == nil {
		t.Fatalf("sync with unreachable advertised artifact succeeded:\n%s", output)
	}
	if !strings.Contains(output, "fetch artifact") {
		t.Fatalf("sync did not surface the reacquisition failure:\n%s", output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("failed reacquisition reported a lock refresh that never persisted:\n%s", output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("failed reacquisition rewrote the lock: %v", err)
	}
	installedAfter, err := os.ReadFile(installedPath)
	if err != nil || string(installedAfter) != string(installedBefore) {
		t.Fatalf("failed reacquisition touched installed output: %v", err)
	}
	doc := readLockDocument(t, lockPath)
	record := doc.Marketplaces[0].Resources[0]
	if record.ArchiveDigest != oldRecord.ArchiveDigest || record.ContentDigest != oldRecord.ContentDigest {
		t.Fatalf("failed reacquisition changed evidence: %#v", record)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestSyncDriftedLockRefreshFailsWhenLockModeChanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	newDigest := drift()
	resp, err := http.Get(server.URL + "/api/v1/resources/tools/skill/review")
	if err != nil {
		t.Fatal(err)
	}
	var advertised struct {
		ArchiveDigest string `json:"archive_digest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&advertised); err != nil {
		_ = resp.Body.Close()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if advertised.ArchiveDigest != newDigest {
		t.Fatalf("server advertises archive digest %q, want refreshed %q", advertised.ArchiveDigest, newDigest)
	}
	artifactResp, err := http.Get(server.URL + "/api/v1/artifacts/" + strings.Replace(newDigest, ":", "/", 1))
	if err != nil {
		t.Fatal(err)
	}
	_ = artifactResp.Body.Close()
	if artifactResp.StatusCode != http.StatusOK {
		t.Fatalf("advertised artifact %s not fetchable: %d", newDigest, artifactResp.StatusCode)
	}

	commit := lifecycle.CommitDependencyTransaction
	var mutated bool
	lifecycle.CommitDependencyTransaction = func(ctx context.Context, stateDir string, desired []dependencytxn.DesiredFile, opts ...dependencytxn.Option) (dependencytxn.Result, error) {
		for _, file := range desired {
			if file.Path == lockPath && !file.Delete {
				if err := os.Chmod(lockPath, 0o600); err != nil {
					t.Fatal(err)
				}
				mutated = true
			}
		}
		return commit(ctx, stateDir, desired, opts...)
	}
	t.Cleanup(func() { lifecycle.CommitDependencyTransaction = commit })

	output, err = executeCommand("sync")
	if err == nil {
		t.Fatalf("sync committed refreshed evidence onto a lock whose mode drifted:\n%s", output)
	}
	if !mutated {
		t.Fatal("sync never committed the refreshed Marketplace lock")
	}
	if !strings.Contains(output, "refresh Marketplace lock evidence") {
		t.Fatalf("sync failure does not identify the evidence refresh: %v\n%s", err, output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("sync reported a lock refresh that never persisted:\n%s", output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("failed evidence refresh rewrote the lock: %v", err)
	}
	info, err := os.Stat(lockPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("failed evidence refresh changed the lock mode: %v", info.Mode())
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestSyncCheckRejectsDriftedLockArchiveEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nmaterialization: {mode: file}\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	installedPath := filepath.Join(home, ".factory", "skills", "review", "SKILL.md")
	installedInfo, err := os.Lstat(installedPath)
	if err != nil || installedInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("file-mode install did not materialize a regular file: %v", err)
	}
	installedBefore, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err = executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("healthy sync --check: %v\n%s", err, output)
	}

	drift()
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err = executeCommand("sync", "--check")
	if err == nil {
		t.Fatalf("sync --check accepted drifted lock archive evidence:\n%s", output)
	}
	if !strings.Contains(output, "locked archive digest drifted") || !strings.Contains(output, "sync") || !strings.Contains(output, "update") {
		t.Fatalf("sync --check error not actionable: %v\n%s", err, output)
	}

	output, err = executeCommand("sync", "--offline")
	if err == nil {
		t.Fatalf("sync --offline accepted drifted lock archive evidence:\n%s", output)
	}
	if !strings.Contains(output, "locked archive digest drifted") {
		t.Fatalf("sync --offline error not actionable: %v\n%s", err, output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("sync --offline reported a lock refresh:\n%s", output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("sync --check rewrote the lock: %v", err)
	}
	installedAfter, err := os.ReadFile(installedPath)
	if err != nil || string(installedAfter) != string(installedBefore) {
		t.Fatalf("sync --check touched installed output: %v", err)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestSyncDriftedLockRefreshReportsNothingWhenSyncFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	serverA, driftA, _ := newDriftableV4MarketplaceServer(t, "mp-a", "review")
	defer serverA.Close()
	serverB, driftB, failArtifactB := newDriftableV4MarketplaceServer(t, "mp-b", "audit")
	defer serverB.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	writeSkill(t, filepath.Join(repo, "resources"), "present", "Present", nil)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/ghost]}}\n")
	withWorkingDir(t, repo)

	for _, tc := range []struct {
		server *httptest.Server
		name   string
	}{{serverA, "review"}, {serverB, "audit"}} {
		if output, err := executeCommand("install", tc.server.URL+"/r/tools/skill/"+tc.name, "--scope", "user", "--yes"); err != nil {
			t.Fatalf("install from %s: %v\n%s", tc.server.URL, err, output)
		}
	}
	oldDoc := readLockDocument(t, lockPath)
	if len(oldDoc.Marketplaces) != 2 {
		t.Fatalf("lock marketplaces = %#v", oldDoc.Marketplaces)
	}

	driftA()
	failArtifactB(driftB())
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("sync with unreachable advertised artifact succeeded:\n%s", output)
	}
	if !strings.Contains(output, "fetch artifact") {
		t.Fatalf("sync did not surface the unrelated failure:\n%s", output)
	}
	if !strings.Contains(output, `skill "ghost" was selected but not found`) {
		t.Fatalf("sync dropped the unrelated warning:\n%s", output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("failed sync reported a lock refresh that never persisted:\n%s", output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("failed sync rewrote the lock: %v", err)
	}
	doc := readLockDocument(t, lockPath)
	for i, mp := range doc.Marketplaces {
		for j, record := range mp.Resources {
			old := oldDoc.Marketplaces[i].Resources[j]
			if record.ArchiveDigest != old.ArchiveDigest || record.ContentDigest != old.ContentDigest {
				t.Fatalf("failed sync changed lock evidence: %#v -> %#v", old, record)
			}
		}
	}
}

func warmCachedMarketplaceArtifact(t *testing.T, server *httptest.Server, archiveDigest string) {
	t.Helper()
	resp, err := http.Get(server.URL + "/api/v1/artifacts/" + strings.Replace(archiveDigest, ":", "/", 1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch advertised artifact %s: status %d, %v", archiveDigest, resp.StatusCode, err)
	}
	mcache := marketplace.NewCache(filepath.Join(cache.CacheDir(), "marketplace"))
	if _, err := mcache.WriteArtifact(archiveDigest, raw); err != nil {
		t.Fatalf("write cached artifact: %v", err)
	}
	if err := mcache.MarkArtifactVerified(archiveDigest); err != nil {
		t.Fatalf("mark cached artifact verified: %v", err)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestStatusDoesNotClaimArchiveRefreshWithStaleLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	installedPath := filepath.Join(home, ".factory", "skills", "review", "SKILL.md")
	installedBefore, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatal(err)
	}
	oldRecord := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]

	newDigest := drift()
	output, err = executeCommand("sync", "--check")
	if err == nil || !strings.Contains(output, "locked archive digest drifted") {
		t.Fatalf("sync --check accepted drifted evidence: %v\n%s", err, output)
	}
	warmCachedMarketplaceArtifact(t, server, newDigest)

	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err = executeCommand("status")
	if err != nil {
		t.Fatalf("status after cached drift evidence: %v\n%s", err, output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("status claimed a lock refresh it never wrote:\n%s", output)
	}
	if synced := statusSection(output, "SYNCED RESOURCES"); !strings.Contains(synced, "review") {
		t.Fatalf("status did not resolve the cached refreshed artifact:\n%s", output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("status rewrote the lock: %v", err)
	}
	installedAfter, err := os.ReadFile(installedPath)
	if err != nil || string(installedAfter) != string(installedBefore) {
		t.Fatalf("status touched installed output: %v", err)
	}
	if got := readLockDocument(t, lockPath).Marketplaces[0].Resources[0].ArchiveDigest; got != oldRecord.ArchiveDigest {
		t.Fatalf("status changed lock archive digest %q -> %q", oldRecord.ArchiveDigest, got)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
func TestRemoveDoesNotClaimArchiveRefreshForRemainingPin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	writeSkill(t, filepath.Join(repo, "resources"), "present", "Present", nil)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/present]}}\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync initial selections: %v\n%s", err, output)
	}
	oldRecord := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]

	drift()
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err = executeCommand("remove", "present")
	if err != nil {
		t.Fatalf("remove unrelated directory selection: %v\n%s", err, output)
	}
	if strings.Contains(output, "marketplace.archive-evidence-refreshed") {
		t.Fatalf("remove claimed a Marketplace lock refresh it never wrote:\n%s", output)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("remove rewrote the Marketplace lock: %v", err)
	}
	if got := readLockDocument(t, lockPath).Marketplaces[0].Resources[0].ArchiveDigest; got != oldRecord.ArchiveDigest {
		t.Fatalf("remove changed lock archive digest %q -> %q", oldRecord.ArchiveDigest, got)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("remove lost the remaining Marketplace installation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "present")); !os.IsNotExist(err) {
		t.Fatalf("remove left the removed skill materialized: %v", err)
	}
}

// spec: marketplace-api/locked-archive-evidence-refreshes-on-identical-content
// spec: marketplace-api/tui-sync-and-update-report-the-refresh-non-fatally
func TestTUISnapshotDoesNotClaimArchiveRefreshWithStaleLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, drift, _ := newDriftableV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	repo := t.TempDir()
	withWorkingDir(t, repo)

	output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes")
	if err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	oldRecord := readLockDocument(t, lockPath).Marketplaces[0].Resources[0]

	newDigest := drift()
	output, err = executeCommand("sync", "--check")
	if err == nil || !strings.Contains(output, "locked archive digest drifted") {
		t.Fatalf("sync --check accepted drifted evidence: %v\n%s", err, output)
	}
	warmCachedMarketplaceArtifact(t, server, newDigest)

	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	var drive func(m tui.Model, cmd tea.Cmd) tui.Model
	drive = func(m tui.Model, cmd tea.Cmd) tui.Model {
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				return m
			}
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					m = drive(m, c)
				}
				return m
			}
			next, follow := m.Update(msg)
			m = next.(tui.Model)
			if _, isTick := msg.(spinner.TickMsg); isTick {
				follow = nil
			}
			cmd = follow
		}
		return m
	}
	press := func(m tui.Model, r rune) tui.Model {
		next, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		return drive(next.(tui.Model), cmd)
	}
	assertNoRefreshClaim := func(m tui.Model, phase string) tui.Model {
		m = press(m, 'e')
		if out := ansi.Strip(m.View().Content); strings.Contains(out, "marketplace.archive-evidence-refreshed") {
			t.Fatalf("%s snapshot claimed a lock refresh it never wrote:\n%s", phase, out)
		}
		return press(m, 'e')
	}

	m := tui.NewModel(repo, home, tui.WithWorkspace(mustWorkspace(t, repo)))
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = drive(next.(tui.Model), cmd)
	m = drive(m, m.Init())
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "review") {
		t.Fatalf("initial snapshot did not resolve the cached resource:\n%s", out)
	}
	m = assertNoRefreshClaim(m, "initial")
	m = press(m, 'r')
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "review") {
		t.Fatalf("refresh snapshot did not resolve the cached resource:\n%s", out)
	}
	assertNoRefreshClaim(m, "refresh")

	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || string(lockAfter) != string(lockBefore) {
		t.Fatalf("TUI snapshots rewrote the lock: %v", err)
	}
	if got := readLockDocument(t, lockPath).Marketplaces[0].Resources[0].ArchiveDigest; got != oldRecord.ArchiveDigest {
		t.Fatalf("TUI snapshots changed lock archive digest %q -> %q", oldRecord.ArchiveDigest, got)
	}
}
