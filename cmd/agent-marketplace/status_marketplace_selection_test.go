package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// newCountingV4MarketplaceServer serves one skill revision like the shared v4
// fixtures and counts artifact downloads separately from metadata requests, so
// a test can prove a read-only command acquired no artifact while still
// allowing it to query pin state.
func newCountingV4MarketplaceServer(t *testing.T, instanceID, resourceName string) (*httptest.Server, *atomic.Int32) {
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
	var artifactDownloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "http://"+r.Host))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+resourceName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+resourceName):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			artifactDownloads.Add(1)
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, &artifactDownloads
}

// newV4MarketplaceCollectionServer serves a collection root whose single leaf
// is a mirror skill, which is what an organization layer pins when it
// distributes a collection without shipping an organization lock.
func newV4MarketplaceCollectionServer(t *testing.T, instanceID, collectionName, memberName string) *httptest.Server {
	t.Helper()
	server, _ := newDriftableV4MarketplaceCollectionServer(t, instanceID, collectionName, memberName)
	return server
}

func newDriftableV4MarketplaceCollectionServer(t *testing.T, instanceID, collectionName, memberName string) (*httptest.Server, func() string) {
	t.Helper()
	canonical, err := hccartifact.FromIR(ir.NewSkill(memberName, "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	if err != nil {
		t.Fatal(err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.RegistrySlug = "tools"
	leafDigest := canonical.Manifest.ContentDigest
	type leafState struct {
		raw      []byte
		revision store.AssessedResourceRevision
	}
	var leaf atomic.Pointer[leafState]
	encodeLeaf := func() string {
		raw, archiveDigest, err := hccartifact.Encode(canonical)
		if err != nil {
			t.Fatal(err)
		}
		leaf.Store(&leafState{raw: raw, revision: testAssessedMarketplaceRevision(t, store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(instanceID), Registry: "src", Kind: "skill", Name: memberName},
			ContentDigest: store.RevisionDigest(leafDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		})})
		return archiveDigest
	}
	encodeLeaf()
	manifestRaw, rootDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             collectionName,
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: memberName, RegistryID: "src", Revision: leafDigest}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	rootRevision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID(instanceID), Registry: "src", Kind: "collection", Name: collectionName},
		ContentDigest: store.RevisionDigest(rootDigest),
	})
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	var drifts atomic.Int64
	drift := func() string {
		n := drifts.Add(1)
		if canonical.Assets == nil {
			canonical.Assets = map[string][]byte{}
		}
		canonical.Assets[fmt.Sprintf("drift-%d.txt", n)] = []byte(fmt.Sprintf("drift %d", n))
		return encodeLeaf()
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := leaf.Load()
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.com"))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/collection/"+collectionName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/collection/"+collectionName):
			_ = json.NewEncoder(w).Encode(rootRevision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/manifests/"):
			_, _ = w.Write(manifestRaw)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/tools/skill/"+memberName) || strings.HasPrefix(r.URL.Path, "/api/v1/resources/src/skill/"+memberName):
			_ = json.NewEncoder(w).Encode(current.revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(current.raw)
		default:
			http.NotFound(w, r)
		}
	})), drift
}

// statusSection returns the body of one status section, so a test asserts
// against the rows of the table it means rather than the whole report.
func statusSection(output, title string) string {
	marker := "\n" + title + "\n"
	body := "\n" + output
	index := strings.Index(body, marker)
	if index < 0 {
		return ""
	}
	rest := body[index+len(marker):]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func installMarketplaceCollectionForTest(t *testing.T) (string, *httptest.Server, *atomic.Int32, string, string, []byte, []byte) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceCollectionServer(t, "mp", "bundle", "review")
	var downloads atomic.Int32
	inner := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/") {
			downloads.Add(1)
		}
		inner.ServeHTTP(w, r)
	})
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())
	if output, err := executeCommand("install", server.URL+"/r/tools/collection/bundle", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("install Marketplace collection: %v\n%s", err, output)
	}
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync Marketplace collection: %v\n%s", err, output)
	}
	downloads.Store(0)
	return home, server, &downloads, configPath, lockPath, readTestFile(t, configPath), readTestFile(t, lockPath)
}

func assertMarketplaceReadOnly(t *testing.T, command string, downloads *atomic.Int32, configPath, lockPath string, configBefore, lockBefore []byte) {
	t.Helper()
	if n := downloads.Load(); n != 0 || !bytes.Equal(configBefore, readTestFile(t, configPath)) ||
		!bytes.Equal(lockBefore, readTestFile(t, lockPath)) {
		t.Errorf("%s downloaded artifacts or changed config/lock (downloads: %d)", command, n)
	}
}

func assertMarketplaceStatusUnknown(t *testing.T, output string) {
	t.Helper()
	drift := statusSection(output, "DRIFT")
	if !strings.Contains(drift, "Unknown") || !strings.Contains(drift, "Marketplace cache evidence") ||
		!strings.Contains(drift, "agent-marketplace sync") || strings.Contains(drift, "In sync") || strings.Contains(drift, "file(s) to remove") ||
		strings.Contains(drift, "file(s) to write") ||
		strings.Contains(output, "ORPHANED FILES") {
		t.Errorf("status did not report unknown Marketplace drift without orphan rows or file counts:\n%s", output)
	}
}

// writeSkillRequiring writes a skill that declares a prerequisite, which is the
// only way to make the resolver's requirement validation observable.
func writeSkillRequiring(t *testing.T, root, name, description string, requires []string) {
	t.Helper()
	content := strings.Builder{}
	content.WriteString("---\n")
	content.WriteString("name: " + name + "\n")
	content.WriteString("description: " + description + "\n")
	content.WriteString("metadata:\n")
	content.WriteString("  requires:\n")
	for _, requirement := range requires {
		content.WriteString("    - " + requirement + "\n")
	}
	content.WriteString("---\n")
	content.WriteString("\n# " + name + "\n")
	writeFile(t, filepath.Join(root, "skills", name, "SKILL.md"), content.String())
}

// spec: status-reporting/synced-marketplace-selection-appears-in-synced-resources
// spec: status-reporting/status-downloads-no-artifacts-and-writes-no-files
// spec: status-reporting/status-counts-drift-when-marketplace-cache-evidence-is-complete
func TestStatusIncludesSyncedMarketplaceSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, artifactDownloads := newCountingV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("review was not materialized: %v", err)
	}
	configBefore := readTestFile(t, configPath)
	lockBefore := readTestFile(t, lockPath)
	artifactDownloads.Store(0)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status reported a healthy Marketplace selection as a failure: %v\n%s", err, output)
	}
	if synced := statusSection(output, "SYNCED RESOURCES"); !strings.Contains(synced, "review") {
		t.Fatalf("Marketplace selection missing from SYNCED RESOURCES:\n%s", output)
	}
	if strings.Contains(output, "ORPHANED FILES") {
		t.Fatalf("status reported the Marketplace selection as orphaned:\n%s", output)
	}
	if drift := statusSection(output, "DRIFT"); !strings.Contains(drift, "In sync") {
		t.Fatalf("status reported drift for a synced Marketplace selection:\n%s", output)
	}

	if got := artifactDownloads.Load(); got != 0 {
		t.Fatalf("status downloaded %d Marketplace artifact(s); it must resolve from the verified cache", got)
	}
	if !bytes.Equal(configBefore, readTestFile(t, configPath)) {
		t.Fatal("status rewrote the configuration")
	}
	if !bytes.Equal(lockBefore, readTestFile(t, lockPath)) {
		t.Fatal("status rewrote the lock")
	}

	// status and sync --check must agree about the same tree.
	if checkOutput, checkErr := executeCommand("sync", "--check"); checkErr != nil {
		t.Fatalf("sync --check disagreed with a clean status: %v\n%s", checkErr, checkOutput)
	}

	if err := os.Remove(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("remove materialized Marketplace output: %v", err)
	}
	output, err = executeCommand("status")
	if err != nil {
		t.Fatalf("status failed for cached Marketplace drift: %v\n%s", err, output)
	}
	drift := statusSection(output, "DRIFT")
	if !strings.Contains(drift, "file(s) to write") || strings.Contains(drift, "Unknown") {
		t.Fatalf("status did not report real drift with complete Marketplace cache evidence:\n%s", output)
	}
}

// spec: status-reporting/organization-collection-members-are-not-orphaned-without-an-organization-lock
func TestStatusDoesNotOrphanOrganizationMarketplaceCollectionWithoutLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server := newV4MarketplaceCollectionServer(t, "mp", "bundle", "review")
	defer server.Close()

	authority := t.TempDir()
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted: {include: [tools/collection/bundle]}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync an organization Marketplace collection without an organization lock: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "review", "SKILL.md")); err != nil {
		t.Fatalf("collection member was not materialized: %v", err)
	}

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status failed for an organization collection without an organization lock: %v\n%s", err, output)
	}
	if strings.Contains(output, "ORPHANED FILES") {
		t.Fatalf("status orphaned an organization collection member:\n%s", output)
	}
	if synced := statusSection(output, "SYNCED RESOURCES"); !strings.Contains(synced, "review") {
		t.Fatalf("organization collection member missing from SYNCED RESOURCES:\n%s", output)
	}
	if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("status created an organization lock: %v", err)
	}
}

// spec: status-reporting/missing-marketplace-cache-reports-unknown-drift-without-orphan-rows
// spec: status-reporting/never-acquired-marketplace-pin-reports-unknown-drift
func TestStatusReportsUnknownDriftWhenMarketplaceCacheIsMissingOrNeverAcquired(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, server, downloads, configPath, lockPath, configBefore, lockBefore := installMarketplaceCollectionForTest(t)
	defer server.Close()
	if err := os.RemoveAll(filepath.Join(cache.CacheDir(), "marketplace")); err != nil {
		t.Fatalf("remove Marketplace cache: %v", err)
	}
	output, err := executeCommand("status")
	if err != nil {
		t.Errorf("status failed for a soft missing-cache pin: %v\n%s", err, output)
	}
	assertMarketplaceStatusUnknown(t, output)
	if !strings.Contains(output, "src/collection/bundle") {
		t.Errorf("status warning did not retain the Marketplace pin coordinate:\n%s", output)
	}
	if err := os.RemoveAll(filepath.Join(home, ".factory", "skills", "review")); err != nil {
		t.Fatalf("remove materialized collection member: %v", err)
	}

	output, err = executeCommand("status")
	if err != nil {
		t.Errorf("status failed for a never-acquired Marketplace pin: %v\n%s", err, output)
	}
	assertMarketplaceStatusUnknown(t, output)
	assertMarketplaceReadOnly(t, "status", downloads, configPath, lockPath, configBefore, lockBefore)
}

// spec: status-reporting/unsynced-marketplace-pin-reports-an-attributable-error-without-orphan-rows
func TestStatusReportsUnresolvableMarketplacePinWithoutOrphanRows(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, artifactDownloads := newCountingV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	withWorkingDir(t, t.TempDir())

	if output, err := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes"); err != nil {
		t.Fatalf("install Marketplace resource: %v\n%s", err, output)
	}
	configBefore := readTestFile(t, configPath)
	lockBefore := readTestFile(t, lockPath)

	// Drop the verified artifact evidence the pin needs. The pin stays
	// selected and its outputs stay on disk, which is exactly the state that
	// used to be reported as an orphan.
	artifacts := filepath.Join(cache.CacheDir(), "marketplace", "artifacts")
	if err := os.RemoveAll(artifacts); err != nil {
		t.Fatalf("remove cached Marketplace artifacts: %v", err)
	}
	artifactDownloads.Store(0)

	output, err := executeCommand("status")
	if err == nil {
		t.Fatalf("status exited 0 for a pin with no verified artifact:\n%s", output)
	}
	errorsSection := statusSection(output, "ERRORS")
	if !strings.Contains(errorsSection, "src/skill/review") {
		t.Fatalf("status did not attribute the failure to the Marketplace pin:\n%s", output)
	}
	if !strings.Contains(errorsSection, "agent-marketplace sync") {
		t.Fatalf("status did not name the acquisition remedy:\n%s", output)
	}
	if strings.Contains(output, "ORPHANED FILES") {
		t.Fatalf("status orphaned the outputs of a pin it could not resolve:\n%s", output)
	}
	assertMarketplaceStatusUnknown(t, output)
	if strings.Contains(statusSection(output, "DRIFT"), "exact registry view incomplete") {
		t.Fatalf("status used generic unknown-drift wording:\n%s", output)
	}
	assertMarketplaceReadOnly(t, "status", artifactDownloads, configPath, lockPath, configBefore, lockBefore)
}

// spec: resource-prerequisites/doctor-treats-a-marketplace-selection-as-a-satisfied-prerequisite
func TestDoctorTreatsMarketplaceSelectionAsSatisfiedPrerequisite(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	server, _ := newCountingV4MarketplaceServer(t, "mp", "review")
	defer server.Close()
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")

	repo := t.TempDir()
	writeSkillRequiring(t, filepath.Join(repo, "resources"), "audit", "Audit the change", []string{"skill:review"})
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/audit]}}\n")
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync the requiring skill: %v\n%s", err, output)
	}
	// Control: with no provider for the prerequisite, doctor must report it.
	// Without this the later assertion could pass vacuously.
	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, output)
	}
	if !strings.Contains(output, `requires skill "review"`) {
		t.Fatalf("doctor did not report the unmet prerequisite before it was provided:\n%s", output)
	}

	if installOutput, installErr := executeCommand("install", server.URL+"/r/tools/skill/review", "--scope", "user", "--yes"); installErr != nil {
		t.Fatalf("install the prerequisite from the Marketplace: %v\n%s", installErr, installOutput)
	}

	output, err = executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor after installing the Marketplace prerequisite: %v\n%s", err, output)
	}
	if strings.Contains(output, `requires skill "review"`) {
		t.Fatalf("doctor reported a prerequisite satisfied by a Marketplace selection as unmet:\n%s", output)
	}
}
