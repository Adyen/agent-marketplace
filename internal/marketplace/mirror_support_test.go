package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type mirrorPublishFixture struct {
	t             *testing.T
	server        *httptest.Server
	archiveDigest string
	contentDigest string
	requestCount  int
}

func newMirrorPublishFixture(t *testing.T) *mirrorPublishFixture {
	return newMirrorPublishFixtureForKind(t, "skill")
}

func newMirrorPublishFixtureForKind(t *testing.T, kind string, overrides ...map[string]hccartifact.SupportFile) *mirrorPublishFixture {
	t.Helper()

	registryDir := t.TempDir()
	skillDir := filepath.Join(registryDir, "skills", "mirror-skill")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillDir, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile := func(rel, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(skillDir, filepath.FromSlash(rel)), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("SKILL.md", "---\nname: mirror-skill\ndescription: Mirror skill\nlicense: MIT\n---\nMirror body.\n", 0o644)
	writeFile("scripts/run.sh", "#!/bin/sh\necho mirror-support\n", 0o755)
	writeFile("docs/nested/note.md", "nested support bytes\n", 0o644)
	if kind == "hook" {
		hooksDir := filepath.Join(registryDir, "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string]string{
			"hooks.json": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh"}]}]}}`,
			"run.sh":     "#!/bin/sh\necho mirror-hook\n",
			"helper.py":  "print('support')\n",
		} {
			mode := os.FileMode(0o644)
			if name == "run.sh" {
				mode = 0o755
			}
			if err := os.WriteFile(filepath.Join(hooksDir, name), []byte(body), mode); err != nil {
				t.Fatal(err)
			}
		}
		for _, files := range overrides {
			for name, file := range files {
				mode := os.FileMode(0o644)
				if file.Executable {
					mode = 0o755
				}
				if err := os.WriteFile(filepath.Join(hooksDir, name), file.Data, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(hooksDir, name), mode); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	mpCfg := mpconfig.Config{
		APIVersion:         "agent-marketplace.io/marketplace/v1",
		AcquisitionDefault: "mirror",
		Refresh:            mpconfig.Refresh{Interval: mpconfig.Duration(30 * time.Minute)},
		Policy: mpconfig.Policy{
			HiddenContent:    "reject",
			LicenseAllowlist: []string{"MIT"},
			Limits: mpconfig.Limits{
				RepositoryBytes:   100 * 1024 * 1024,
				FileCount:         1024,
				FileBytes:         2 * 1024 * 1024,
				ArtifactBytes:     16 * 1024 * 1024,
				CloneDuration:     mpconfig.Duration(5 * time.Minute),
				IngestionDuration: mpconfig.Duration(2 * time.Minute),
			},
		},
	}
	source := store.Registry{
		ID:          store.RegistryID("src"),
		Slug:        "src",
		Ref:         "main",
		Origins:     []string{"file://" + registryDir},
		Acquisition: store.AcquisitionMirror,
		License:     "MIT",
		Roots:       []store.RegistryRoot{{Path: ".", SourceHarness: "claude"}},
	}
	commit := strings.Repeat("c", 40)
	rev, err := ingest.BuildRegistryRevision(context.Background(), mpCfg, store.MarketplaceID("mp"), source, registryDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
	}

	var resource *store.ResourceRevision
	for i := range rev.Resources {
		if rev.Resources[i].Coordinate.Kind == kind {
			resource = &rev.Resources[i]
			break
		}
	}
	if resource == nil || resource.ArchiveDigest == "" {
		t.Fatalf("expected mirror-skill resource revision with archive digest, got %#v", rev.Resources)
	}
	var archive []byte
	for _, blob := range rev.ExtraBlobs {
		if string(blob.Digest) == string(resource.ArchiveDigest) {
			archive = blob.Data
		}
	}
	if len(archive) == 0 {
		t.Fatalf("mirror ingest did not publish an artifact archive for %s", resource.ArchiveDigest)
	}

	fixture := &mirrorPublishFixture{
		t:             t,
		archiveDigest: string(resource.ArchiveDigest),
		contentDigest: string(resource.ContentDigest),
	}
	assessed := assessedRevision(store.ResourceRevision{
		Coordinate:    resource.Coordinate,
		ContentDigest: resource.ContentDigest,
		ArchiveDigest: resource.ArchiveDigest,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		fixture.requestCount++
		if !strings.HasSuffix(r.URL.Path, "/revisions/"+fixture.contentDigest) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(assessed)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		fixture.requestCount++
		expected := "sha256/" + strings.TrimPrefix(fixture.archiveDigest, "sha256:")
		if !strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"+expected) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		fixture.requestCount++
		writeTestWellKnown(w, "mp")
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)
	return fixture
}

// spec: marketplace-api/mirrored-hook-support-installation
func TestMirrorHookSupportFilesInstall(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(fmt.Sprint("machine=", machine), func(t *testing.T) {
			isolateHarnessState(t)
			fixture := newMirrorPublishFixtureForKind(t, "hook")
			cfg := fixture.consumerConfig()
			pin := &cfg.Marketplaces[0].Resources[0]
			pin.Kind, pin.Name = "hook", "PreToolUse-command"
			if machine {
				cfg.Marketplaces[0].Source = config.UserPersonal
			}
			cache := NewCache(t.TempDir())
			r := &Resolver{Cache: cache}
			resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
			if len(errs) != 0 || len(resolved) != 1 {
				t.Fatalf("Resolve: %v, %v", resolved, errs)
			}
			repo := t.TempDir()
			report, err := materialize.Run(context.Background(), repo, resolved)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("materialize: %v, %v", report, err)
			}
			root := repo
			if machine {
				root = os.Getenv("HOME")
			}
			hooksDir := filepath.Join(root, ".factory", "hooks")
			script := filepath.Join(hooksDir, "run.sh")
			data, err := os.ReadFile(script)
			if err != nil || string(data) != "#!/bin/sh\necho mirror-hook\n" {
				t.Fatalf("script: %q, %v", data, err)
			}
			info, err := os.Stat(script)
			if err != nil || info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("script executable: %v, %v", info, err)
			}
			if data, err := os.ReadFile(filepath.Join(hooksDir, "helper.py")); err != nil || string(data) != "print('support')\n" {
				t.Fatalf("helper: %q, %v", data, err)
			}
			aggregate, err := os.ReadFile(filepath.Join(hooksDir, "hooks.json"))
			if err != nil {
				t.Fatal(err)
			}
			var hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal(aggregate, &hooks); err != nil {
				t.Fatal(err)
			}
			installedCommand := hooks["PreToolUse"][0].Hooks[0].Command
			command := exec.Command("/bin/sh", "-c", installedCommand)
			command.Dir = t.TempDir()
			if !machine {
				command.Dir = repo
			}
			if out, err := command.CombinedOutput(); err != nil || string(out) != "mirror-hook\n" {
				t.Fatalf("hook execution: %q, %v", out, err)
			}
			before := fixture.requestCount
			if _, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeCachedNoFetch); len(errs) != 0 {
				t.Fatal(errs)
			}
			if fixture.requestCount != before {
				t.Fatal("cached resolution made network requests")
			}
		})
	}
}

// spec: marketplace-api/mirrored-hook-support-revision-changes
func TestMirrorHookSupportRevisionChanges(t *testing.T) {
	first := newMirrorPublishFixtureForKind(t, "hook")
	repeat := newMirrorPublishFixtureForKind(t, "hook")
	if first.contentDigest != repeat.contentDigest || first.archiveDigest != repeat.archiveDigest {
		t.Fatal("identical hooks did not produce identical revisions and archives")
	}
	for _, file := range []hccartifact.SupportFile{
		{Data: []byte("#!/bin/sh\necho changed\n"), Executable: true},
		{Data: []byte("#!/bin/sh\necho mirror-hook\n"), Executable: false},
	} {
		changed := newMirrorPublishFixtureForKind(t, "hook", map[string]hccartifact.SupportFile{"run.sh": file})
		if changed.contentDigest == first.contentDigest || changed.archiveDigest == first.archiveDigest {
			t.Fatal("support bytes or executable mode changed without a new revision")
		}
	}
}

func (f *mirrorPublishFixture) consumerConfig() *config.Config {
	return mirrorConsumerConfig(f.server.URL, f.contentDigest)
}

func mirrorConsumerConfig(url, contentDigest string) *config.Config {
	return &config.Config{
		Marketplaces: []config.MarketplaceEntry{
			{
				URL:        url,
				InstanceID: "mp",
				Alias:      "test",
				Resources: []config.MarketplaceResourcePin{
					{
						RegistryID: "src",
						Kind:       "skill",
						Name:       "mirror-skill",
						Revision:   "main",
						Digest:     contentDigest,
					},
				},
			},
		},
	}
}

func serveCanonicalMirror(t *testing.T, canonical hccartifact.CanonicalArtifact, extraSupport map[string]hccartifact.SupportFile) (*httptest.Server, string) {
	t.Helper()
	canonical.Support = extraSupport
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Registry:    store.RegistryID("src"),
			Kind:        canonical.Manifest.Kind,
			Name:        canonical.Manifest.Name,
		},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rev)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		writeTestWellKnown(w, "mp")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, canonical.Manifest.ContentDigest
}

func mirrorSkillCanonical(t *testing.T, supportFiles []string) hccartifact.CanonicalArtifact {
	t.Helper()
	skill := ir.NewSkill("mirror-skill", "Mirror skill", "Mirror body.", []string{"user"}, nil, ir.SourceRef{})
	skill.SupportFiles = supportFiles
	canonical, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = "main"
	return canonical
}

func isolateHarnessState(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func assertInstalledMirrorSkill(t *testing.T, skillDir string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(skillDir)
	if err != nil {
		t.Fatalf("resolve installed skill dir %q: %v", skillDir, err)
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		t.Fatalf("installed skill dir %q is not a directory", skillDir)
	}

	runScript := filepath.Join(resolved, "scripts", "run.sh")
	data, err := os.ReadFile(runScript)
	if err != nil {
		t.Fatalf("read installed support script: %v", err)
	}
	if string(data) != "#!/bin/sh\necho mirror-support\n" {
		t.Fatalf("support script bytes = %q", data)
	}
	info, err := os.Stat(runScript)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("support script lost executable mode: %v", info.Mode())
	}

	nested, err := os.ReadFile(filepath.Join(resolved, "docs", "nested", "note.md"))
	if err != nil {
		t.Fatalf("read nested support file: %v", err)
	}
	if string(nested) != "nested support bytes\n" {
		t.Fatalf("nested support bytes = %q", nested)
	}

	if _, err := os.Stat(filepath.Join(resolved, "SKILL.md")); err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}

	assertNoArtifactCacheFiles(t, resolved)
}

func assertNoArtifactCacheFiles(t *testing.T, skillDir string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(skillDir)
	if err != nil {
		t.Fatalf("resolve installed skill dir %q: %v", skillDir, err)
	}
	var leaked []string
	err = filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch d.Name() {
		case "artifact.tar.gz", "verified.marker":
			leaked = append(leaked, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(leaked) > 0 {
		t.Fatalf("artifact-cache bookkeeping leaked into installed skill: %v", leaked)
	}
}

func TestMirrorSkillSupportFilesInstall(t *testing.T) {
	isolateHarnessState(t)
	fixture := newMirrorPublishFixture(t)
	cache := NewCache(t.TempDir())
	r := &Resolver{Cache: cache}
	cfg := fixture.consumerConfig()

	resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) > 0 {
		t.Fatalf("Resolve errors: %v", errs)
	}
	if len(resolved) != 1 || resolved[0].Resource == nil {
		t.Fatalf("resolved artifacts = %#v, want one materializable skill", resolved)
	}

	repo := t.TempDir()
	if _, err := materialize.Run(context.Background(), repo, resolved); err != nil {
		t.Fatalf("materialize.Run symlink mode: %v", err)
	}
	assertInstalledMirrorSkill(t, filepath.Join(repo, ".factory", "skills", "mirror-skill"))

	fileRepo := t.TempDir()
	fileModes := config.DefaultMaterializationModes()
	fileModes.Project = config.MaterializationModeFile
	if _, err := materialize.Run(context.Background(), fileRepo, resolved, materialize.WithMaterializationModes(fileModes)); err != nil {
		t.Fatalf("materialize.Run file mode: %v", err)
	}
	assertInstalledMirrorSkill(t, filepath.Join(fileRepo, ".factory", "skills", "mirror-skill"))

	fixture.server.Close()
	cached, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeCachedNoFetch)
	if len(errs) > 0 {
		t.Fatalf("CachedNoFetch Resolve errors: %v", errs)
	}
	if len(cached) != 1 || cached[0].Resource == nil {
		t.Fatalf("CachedNoFetch resolved artifacts = %#v", cached)
	}
	offlineRepo := t.TempDir()
	if _, err := materialize.Run(context.Background(), offlineRepo, cached); err != nil {
		t.Fatalf("offline materialize.Run: %v", err)
	}
	assertInstalledMirrorSkill(t, filepath.Join(offlineRepo, ".factory", "skills", "mirror-skill"))
}

func TestMirrorSkillWithoutSupportLeavesCacheFilesOut(t *testing.T) {
	isolateHarnessState(t)
	cache := NewCache(t.TempDir())
	canonical := mirrorSkillCanonical(t, nil)
	server, contentDigest := serveCanonicalMirror(t, canonical, nil)
	cfg := mirrorConsumerConfig(server.URL, contentDigest)

	r := &Resolver{Cache: cache}
	resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) > 0 {
		t.Fatalf("Resolve errors: %v", errs)
	}
	if len(resolved) != 1 || resolved[0].Resource == nil {
		t.Fatalf("resolved artifacts = %#v, want one materializable skill", resolved)
	}

	repo := t.TempDir()
	if _, err := materialize.Run(context.Background(), repo, resolved); err != nil {
		t.Fatalf("materialize.Run symlink mode: %v", err)
	}
	skillDir := filepath.Join(repo, ".factory", "skills", "mirror-skill")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}
	assertNoArtifactCacheFiles(t, skillDir)

	fileRepo := t.TempDir()
	fileModes := config.DefaultMaterializationModes()
	fileModes.Project = config.MaterializationModeFile
	if _, err := materialize.Run(context.Background(), fileRepo, resolved, materialize.WithMaterializationModes(fileModes)); err != nil {
		t.Fatalf("materialize.Run file mode: %v", err)
	}
	fileSkillDir := filepath.Join(fileRepo, ".factory", "skills", "mirror-skill")
	if _, err := os.Stat(filepath.Join(fileSkillDir, "SKILL.md")); err != nil {
		t.Fatalf("file-mode installed SKILL.md missing: %v", err)
	}
	assertNoArtifactCacheFiles(t, fileSkillDir)
}

func TestMirrorSkillMissingSupportFailsClosed(t *testing.T) {
	isolateHarnessState(t)
	cache := NewCache(t.TempDir())

	canonical := mirrorSkillCanonical(t, []string{"scripts/run.sh"})
	server, contentDigest := serveCanonicalMirror(t, canonical, nil)
	cfg := mirrorConsumerConfig(server.URL, contentDigest)

	r := &Resolver{Cache: cache}
	resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) == 0 {
		t.Fatalf("expected fail-closed resolution error for declared-but-absent support files, got artifacts %#v", resolved)
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "support") {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolution errors do not name the missing support bytes: %v", errs)
	}
}

func TestMirrorSkillBackslashSupportPathRejected(t *testing.T) {
	isolateHarnessState(t)
	cache := NewCache(t.TempDir())

	support := map[string]hccartifact.SupportFile{
		"scripts/run.sh": {Data: []byte("#!/bin/sh\necho mirror-support\n"), Executable: true},
	}
	canonical := mirrorSkillCanonical(t, []string{`scripts\run.sh`})
	server, contentDigest := serveCanonicalMirror(t, canonical, support)
	_, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	cfg := mirrorConsumerConfig(server.URL, contentDigest)

	r := &Resolver{Cache: cache}
	resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) == 0 {
		t.Fatalf("expected resolution to reject backslash declared support path, got artifacts %#v", resolved)
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), `scripts\run.sh`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolution errors do not name the invalid declared support path: %v", errs)
	}
	marker := filepath.Join(filepath.Dir(cache.ArtifactPath(archiveDigest)), "verified.marker")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("verified marker written for rejected artifact: %v", err)
	}
	if _, err := os.Stat(cache.MirrorResourceDir(archiveDigest)); !os.IsNotExist(err) {
		t.Fatalf("support staging directory written for rejected artifact: %v", err)
	}
}

func TestMirrorSkillUndeclaredSupportFailsClosed(t *testing.T) {
	isolateHarnessState(t)
	cache := NewCache(t.TempDir())

	canonical := mirrorSkillCanonical(t, nil)
	server, contentDigest := serveCanonicalMirror(t, canonical, map[string]hccartifact.SupportFile{
		"extra.sh": {Data: []byte("#!/bin/sh\n"), Executable: true},
	})
	cfg := mirrorConsumerConfig(server.URL, contentDigest)

	r := &Resolver{Cache: cache}
	resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) == 0 {
		t.Fatalf("expected fail-closed resolution error for undeclared support members, got artifacts %#v", resolved)
	}
}
