package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bytes"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/codemode"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type promoteResource struct {
	kind          string
	name          string
	revision      store.AssessedResourceRevision
	raw           []byte
	archiveDigest string
	contentDigest string
}

func promoteArtifact(t *testing.T, kind, name string, resource ir.Resource) promoteResource {
	t.Helper()
	artifact, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	artifact.Manifest.RegistryID = "src"
	artifact.Manifest.RegistrySlug = "tools"
	raw, archiveDigest, err := hccartifact.Encode(artifact)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return promoteResource{
		kind:          kind,
		name:          name,
		raw:           raw,
		archiveDigest: archiveDigest,
		contentDigest: artifact.Manifest.ContentDigest,
		revision: testAssessedMarketplaceRevision(t, store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: kind, Name: name},
			ContentDigest: store.RevisionDigest(artifact.Manifest.ContentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}),
	}
}

func promoteWarnArtifact(t *testing.T, kind, name string, resource ir.Resource) promoteResource {
	t.Helper()
	res := promoteArtifact(t, kind, name, resource)
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               res.revision.Coordinate,
		ContentDigest:            res.revision.ContentDigest,
		AnalyzerVersion:          "agent-marketplace-test/v1",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 store.AssessmentDecisionWarn,
		Findings:                 []store.Finding{{Code: "test.warn", Severity: "medium", Policy: "warn", Message: "fixture warning"}},
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("finalize warn assessment: %v", err)
	}
	res.revision.Assessment = assessment
	return res
}

func newPromoteMarketplaceServer(t *testing.T, resources map[string]promoteResource) *httptest.Server {
	t.Helper()
	registry := mpapi.PublicRegistry{ID: "src", Slug: "tools", Origins: []string{"https://example.com/tools.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "http://"+r.Host))
		case r.URL.Path == "/api/v1/registries/tools" || r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(registry)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/"):
			segments := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/resources/"), "/")
			segments[0] = "tools"
			res, ok := resources[strings.Join(segments[:3], "/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if len(segments) == 5 && (segments[3] != "revisions" || segments[4] != res.contentDigest) {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(res.revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			hexDigest := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/sha256/")
			for _, res := range resources {
				if strings.TrimPrefix(res.archiveDigest, "sha256:") == hexDigest {
					w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
					_, _ = w.Write(res.raw)
					return
				}
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func promoteGitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeSessionEntry(t *testing.T, dir, session string, entry codemode.SessionEntry) {
	t.Helper()
	logDir := filepath.Join(dir, "agent-marketplace", "mcp-sessions")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	entry.Session = session
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	raw, _ := json.Marshal(entry)
	if err := os.WriteFile(filepath.Join(logDir, session+".jsonl"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPromoteSelectsRecordedGitSource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())

	aOrigin, aWorking := createBareRepoWithWorkingClone(t, "promote-a.git")
	writeSkill(t, aWorking, "shared", "registry A body", nil)
	commitAndPushAll(t, aWorking, "a")
	bOrigin, bWorking := createBareRepoWithWorkingClone(t, "promote-b.git")
	writeSkill(t, bWorking, "shared", "registry B body", nil)
	commitAndPushAll(t, bWorking, "b")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  a-src: {git: file://"+filepath.ToSlash(aOrigin)+", ref: main}\n  b-src: {git: file://"+filepath.ToSlash(bOrigin)+", ref: main}\n")
	withWorkingDir(t, repoRoot)

	label := strings.TrimSuffix(strings.ReplaceAll("file://"+filepath.ToSlash(bOrigin), "/", "-"), ".git")
	ref := label + "/skill/shared"
	if !strings.Contains(ref, ":") {
		t.Fatalf("fixture ref %q lost its ambiguous colon label", ref)
	}
	writeSessionEntry(t, home+"/state", "sess-git", codemode.SessionEntry{
		Event: "skill.load", Ref: ref, Revision: "main", Source: "file://" + filepath.ToSlash(bOrigin),
	})

	output, err := executeCommand("promote", ref, "--scope", "repo")
	if err != nil {
		t.Fatalf("promote: %v\n%s", err, output)
	}
	materialized, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "shared", "SKILL.md"))
	if err != nil {
		t.Fatalf("materialized skill: %v", err)
	}
	if !strings.Contains(string(materialized), "registry B body") {
		t.Fatalf("promote materialized the wrong source:\n%s", materialized)
	}
	if strings.Contains(string(materialized), "registry A body") {
		t.Fatalf("promote materialized the colliding registry:\n%s", materialized)
	}
}

func TestPromoteSelectsRecordedLocalSource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())

	dirA := filepath.Join(t.TempDir(), "reg-a")
	writeSkill(t, dirA, "shared", "local A body", nil)
	dirB := filepath.Join(t.TempDir(), "reg-b")
	writeSkill(t, dirB, "shared", "local B body", nil)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	configBody := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  a-src: {directory: " + dirA + "}\n  b-src: {directory: " + dirB + "}\n"
	writeConfig(t, configPath, configBody)
	withWorkingDir(t, repoRoot)

	ref := "local:" + filepath.Base(dirB) + "/skill/shared"
	writeSessionEntry(t, home+"/state", "sess-local", codemode.SessionEntry{
		Event: "skill.load", Ref: ref, Source: dirB,
	})

	output, err := executeCommand("promote", ref, "--scope", "repo")
	if err != nil {
		t.Fatalf("promote: %v\n%s", err, output)
	}
	materialized, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "shared", "SKILL.md"))
	if err != nil {
		t.Fatalf("materialized skill: %v", err)
	}
	if !strings.Contains(string(materialized), "local B body") {
		t.Fatalf("promote materialized the wrong local source:\n%s", materialized)
	}
	if strings.Contains(string(materialized), "local A body") {
		t.Fatalf("promote materialized the colliding registry:\n%s", materialized)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(after), "directory:"); got != 2 {
		t.Fatalf("promote added an unrelated registry source; config:\n%s", after)
	}
}

// spec: code-mode-mcp/recorded-refs-remain-promotable
func TestPromoteAcceptsRefRecordedUnderEarlierLabel(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())

	dir := filepath.Join(t.TempDir(), "reg-b")
	writeSkill(t, dir, "shared", "recorded body", nil)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	// The source name now labels refs `local:b-src/...`; the session entry
	// still carries the earlier derived label `local:reg-b/...`.
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  b-src: {directory: "+dir+"}\n")
	withWorkingDir(t, repoRoot)

	ref := "local:" + filepath.Base(dir) + "/skill/shared"
	writeSessionEntry(t, home+"/state", "sess-relabel", codemode.SessionEntry{
		Event: "skill.load", Ref: ref, Source: dir,
	})

	output, err := executeCommand("promote", ref, "--scope", "repo")
	if err != nil {
		t.Fatalf("promote: %v\n%s", err, output)
	}
	materialized, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "shared", "SKILL.md"))
	if err != nil {
		t.Fatalf("materialized skill: %v", err)
	}
	if !strings.Contains(string(materialized), "recorded body") {
		t.Fatalf("promote did not route through recorded provenance:\n%s", materialized)
	}
}

// spec: code-mode-mcp/recorded-git-source-installed-at-recorded-commit
func TestPromoteLocksRecordedGitCommit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t, "promote-pin.git")
	writeSkill(t, working, "review", "commit A body", nil)
	commitAndPushAll(t, working, "a")
	commitA := promoteGitHead(t, working)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  src: {git: file://"+filepath.ToSlash(origin)+", ref: main}\n")
	withWorkingDir(t, repoRoot)

	builder := &codemode.SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh, RepoRoot: repoRoot}
	ec := config.EffectiveConfig{Merged: &config.Config{Registries: []config.RegistryEntry{
		{URL: "file://" + filepath.ToSlash(origin), Ref: "main"},
	}}}
	snap, errs := builder.BuildSnapshot(context.Background(), ec)
	if len(errs) > 0 {
		t.Fatalf("BuildSnapshot errors: %v", errs)
	}
	if len(snap.Resources) != 1 {
		t.Fatalf("snapshot resources = %+v", snap.Resources)
	}
	res := snap.Resources[0]

	writeSkill(t, working, "review", "commit B body", nil)
	commitAndPushAll(t, working, "b")

	writeSessionEntry(t, home+"/state", "sess-pin", codemode.SessionEntry{
		Event: "skill.load", Ref: res.Ref, Revision: res.Revision, Source: res.Origin().SourceURL,
	})

	output, err := executeCommand("promote", res.Ref, "--scope", "repo")
	if err != nil {
		t.Fatalf("promote: %v\n%s", err, output)
	}
	materialized, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", "review", "SKILL.md"))
	if err != nil {
		t.Fatalf("materialized skill: %v", err)
	}
	if !strings.Contains(string(materialized), "commit A body") {
		t.Fatalf("promote did not pin the recorded commit %s:\n%s", commitA, materialized)
	}
	if strings.Contains(string(materialized), "commit B body") {
		t.Fatalf("promote installed a newer commit than the recorded snapshot:\n%s", materialized)
	}
}

// spec: code-mode-mcp/unprovenanced-ref-fails-closed
func TestPromoteLegacyAmbiguousColonRefFailsClosed(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	writeSessionEntry(t, home+"/state", "sess-legacy", codemode.SessionEntry{
		Event: "skill.load", Ref: "github.com:org-repo/skill/x", Revision: "main",
	})
	_, err := executeCommand("promote", "github.com:org-repo/skill/x", "--scope", "user")
	if err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("expected a fail-closed provenance error, got %v", err)
	}
}

// spec: code-mode-mcp/marketplace-promotion-pins-exact-digest
func TestPromoteMarketplacePinsExactContentDigest(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())

	skillRes := promoteArtifact(t, "skill", "review", ir.NewSkill("review", "Review", "Review carefully.", nil, nil, ir.SourceRef{}))
	mcpRes := promoteArtifact(t, "mcpserver", "remote", ir.NewMcpServer("remote", "echo", []string{"hi"}, nil, nil, ir.SourceRef{}))
	server := newPromoteMarketplaceServer(t, map[string]promoteResource{
		"tools/skill/review":     skillRes,
		"tools/mcpserver/remote": mcpRes,
	})

	configPath := filepath.Join(home, ".agents", "config.yaml")
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  mp: {marketplace: "+server.URL+"}\n")
	withWorkingDir(t, t.TempDir())
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("lock endpoint source: %v\n%s", err, output)
	}

	writeSessionEntry(t, home+"/state", "sess-mp", codemode.SessionEntry{
		Event: "server.start", Ref: "mp:tools/mcp/remote", Revision: mcpRes.contentDigest,
		Source: "marketplace:" + server.URL, Digest: mcpRes.contentDigest,
	})
	output, err := executeCommand("promote", "mp:tools/mcp/remote", "--scope", "user")
	if err != nil {
		t.Fatalf("promote mcp server: %v\n%s", err, output)
	}

	writeSessionEntry(t, home+"/state", "sess-mp-skill", codemode.SessionEntry{
		Event: "skill.load", Ref: "mp:tools/skill/review", Revision: skillRes.contentDigest,
		Source: "marketplace:" + server.URL, Digest: skillRes.contentDigest,
	})
	output, err = executeCommand("promote", "mp:tools/skill/review", "--scope", "user")
	if err != nil {
		t.Fatalf("promote skill: %v\n%s", err, output)
	}

	doc := readLockDocument(t, lockPath)
	pins := map[string]string{}
	for _, mp := range doc.Marketplaces {
		for _, r := range mp.Resources {
			pins[r.Kind+"/"+r.Name] = string(r.ContentDigest)
		}
	}
	if pins["skill/review"] != skillRes.contentDigest {
		t.Fatalf("skill pin = %v, want content digest %s", pins, skillRes.contentDigest)
	}
	if pins["mcpserver/remote"] != mcpRes.contentDigest {
		t.Fatalf("mcp pin = %v, want content digest %s", pins, mcpRes.contentDigest)
	}
	for _, pin := range pins {
		if pin == mcpRes.archiveDigest || pin == skillRes.archiveDigest {
			t.Fatalf("pin recorded the archive digest instead of the content digest: %v", pins)
		}
	}
}

func setupPromoteWarnMarketplace(t *testing.T) (server *httptest.Server, configPath, lockPath string, res promoteResource) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("TMPDIR", t.TempDir())

	res = promoteWarnArtifact(t, "skill", "risky", ir.NewSkill("risky", "Risky", "Risky body.", nil, nil, ir.SourceRef{}))
	server = newPromoteMarketplaceServer(t, map[string]promoteResource{"tools/skill/risky": res})

	configPath = filepath.Join(home, ".agents", "config.yaml")
	lockPath = filepath.Join(home, ".agents", "lock.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  mp: {marketplace: "+server.URL+"}\n")
	withWorkingDir(t, t.TempDir())
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("lock endpoint source: %v\n%s", err, output)
	}
	writeSessionEntry(t, home+"/state", "sess-warn", codemode.SessionEntry{
		Event: "skill.load", Ref: "mp:tools/skill/risky", Revision: res.contentDigest,
		Source: "marketplace:" + server.URL, Digest: res.contentDigest,
	})
	return server, configPath, lockPath, res
}

func lockHasResourcePin(t *testing.T, lockPath, name string) bool {
	t.Helper()
	doc := readLockDocument(t, lockPath)
	for _, mp := range doc.Marketplaces {
		for _, r := range mp.Resources {
			if r.Name == name {
				return true
			}
		}
	}
	return false
}

// spec: code-mode-mcp/marketplace-findings-gate-on-consent
func TestPromoteMarketplaceWarnRefusesWithoutConsent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	server, configPath, lockPath, _ := setupPromoteWarnMarketplace(t)
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("promote", "mp:tools/skill/risky", "--scope", "user")
	if err == nil || !strings.Contains(err.Error(), "risk acceptance") {
		t.Fatalf("expected consent refusal, got %v\n%s", err, output)
	}
	if strings.Contains(output, server.URL) {
		t.Fatalf("promote leaked the marketplace source URL:\n%s", output)
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatalf("config changed without consent: %v\n%s", err, configAfter)
	}
	if lockHasResourcePin(t, lockPath, "risky") {
		t.Fatal("lock recorded the resource without consent")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(configPath)), ".factory", "skills", "risky", "SKILL.md")); !os.IsNotExist(statErr) {
		t.Fatal("materialized the resource without consent")
	}
}

func TestPromoteMarketplaceWarnAcceptRiskInstalls(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, lockPath, res := setupPromoteWarnMarketplace(t)
	for _, flag := range []string{"--accept-risk", "--yes"} {
		output, err := executeCommand("promote", "mp:tools/skill/risky", "--scope", "user", flag)
		if err != nil {
			t.Fatalf("promote %s: %v\n%s", flag, err, output)
		}
		if !lockHasResourcePin(t, lockPath, "risky") {
			t.Fatalf("promote %s did not record the lock pin", flag)
		}
	}
	doc := readLockDocument(t, lockPath)
	var pin string
	for _, mp := range doc.Marketplaces {
		for _, r := range mp.Resources {
			if r.Name == "risky" {
				pin = string(r.ContentDigest)
			}
		}
	}
	if pin != res.contentDigest {
		t.Fatalf("pin = %q, want %q", pin, res.contentDigest)
	}
}

func TestPromoteMarketplaceWarnInteractiveConsent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, configPath, lockPath, _ := setupPromoteWarnMarketplace(t)
	scope, err := pipeline.ParseScope("user")
	if err != nil {
		t.Fatal(err)
	}

	for _, reply := range []string{"", "n\n"} {
		var out bytes.Buffer
		if err := runPromote(context.Background(), &out, strings.NewReader(reply), "mp:tools/skill/risky", scope, true, true, false); err != nil {
			t.Fatalf("interactive decline %q: %v", reply, err)
		}
		if !strings.Contains(out.String(), "Nothing has been installed.") {
			t.Fatalf("interactive decline %q output: %s", reply, out.String())
		}
		if lockHasResourcePin(t, lockPath, "risky") {
			t.Fatalf("interactive decline %q installed the resource", reply)
		}
	}

	var out bytes.Buffer
	if err := runPromote(context.Background(), &out, strings.NewReader("y\n"), "mp:tools/skill/risky", scope, true, true, false); err != nil {
		t.Fatalf("interactive accept: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Install despite these risks?") {
		t.Fatalf("interactive accept skipped the risk summary:\n%s", out.String())
	}
	if !lockHasResourcePin(t, lockPath, "risky") {
		t.Fatal("interactive accept did not record the lock pin")
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Contains(configAfter, []byte("risky")) {
		t.Fatalf("interactive accept did not update config: %v\n%s", err, configAfter)
	}
}

// spec: code-mode-mcp/risk-flags-rejected-for-non-marketplace-sources
func TestPromoteRejectsRiskFlagsForNonMarketplaceSource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	writeSessionEntry(t, home+"/state", "sess-local", codemode.SessionEntry{
		Event: "skill.load", Ref: "local:reg/skill/x", Source: t.TempDir(),
	})
	for _, flag := range []string{"--accept-risk", "--yes"} {
		_, err := executeCommand("promote", "local:reg/skill/x", flag)
		if err == nil || !strings.Contains(err.Error(), "Marketplace") {
			t.Fatalf("promote %s on a non-Marketplace entry: got %v", flag, err)
		}
	}
}
