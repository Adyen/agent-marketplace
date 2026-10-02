package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestSyncPreservesInstalledMirrorSkillWhenArchiveOmitsDeclaredSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	repoRoot := t.TempDir()

	skill := ir.NewSkill("mirror-skill", "Mirror skill", "Mirror body.", nil, nil, ir.SourceRef{})
	skill.SupportFiles = []string{"scripts/run.sh"}
	stale, err := hccartifact.FromIR(skill)
	if err != nil {
		t.Fatal(err)
	}
	stale.Manifest.RegistryID = "src"
	stale.Manifest.Revision = "main"
	staleRaw, staleArchiveDigest, err := hccartifact.Encode(stale)
	if err != nil {
		t.Fatal(err)
	}
	good := stale
	good.Support = map[string]hccartifact.SupportFile{
		"scripts/run.sh": {Data: []byte("#!/bin/sh\necho support\n"), Executable: true},
	}
	goodRaw, goodArchiveDigest, err := hccartifact.Encode(good)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := stale.Manifest.ContentDigest
	if good.Manifest.ContentDigest != contentDigest {
		t.Fatal("support members must not change the payload content digest")
	}

	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "mirror-skill"}
	revisionFor := func(archiveDigest string) store.AssessedResourceRevision {
		return testAssessedResourceRevision(t, store.ResourceRevision{
			Coordinate:    coordinate,
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		}, store.AssessmentDecisionAllow, nil)
	}
	current := struct {
		rev store.AssessedResourceRevision
		raw []byte
	}{rev: revisionFor(goodArchiveDigest), raw: goodRaw}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "http://"+r.Host))
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/revisions/"+contentDigest) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(current.rev)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(current.raw)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	entry := config.MarketplaceEntry{
		URL:        server.URL,
		InstanceID: "mp",
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "src", Kind: "skill", Name: "mirror-skill", Revision: "main", Digest: contentDigest,
		}},
	}
	syncOpts := ResolveOptions{
		Workspace: mustWorkspace(t, repoRoot),
		NoBackup:  true,
		Loaders: Loaders{
			Org: func() (*config.Config, error) { return &config.Config{}, nil },
			Global: func() (*config.Config, error) {
				return &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}}, nil
			},
			Repo: func() (*config.Config, error) {
				return &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil
			},
			Local: func() (*config.LocalConfig, error) { return &config.LocalConfig{}, nil },
		},
	}

	first := Sync(context.Background(), syncOpts)
	if len(first.Errors) != 0 {
		t.Fatalf("initial sync errors = %v", first.Errors)
	}
	skillDir := filepath.Join(repoRoot, ".factory", "skills", "mirror-skill")
	support := filepath.Join(skillDir, "scripts", "run.sh")
	supportBytes, err := os.ReadFile(support)
	if err != nil {
		t.Fatalf("installed support script missing: %v", err)
	}
	info, err := os.Stat(support)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed support script lost executable mode: %v", info.Mode())
	}
	skillDoc, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}

	current.rev = revisionFor(staleArchiveDigest)
	current.raw = staleRaw
	second := Sync(context.Background(), syncOpts)
	if len(second.Errors) == 0 {
		t.Fatal("stale archive without declared support bytes synced without error")
	}
	found := false
	for _, err := range second.Errors {
		if strings.Contains(err.Error(), "support") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sync errors do not name the missing support bytes: %v", second.Errors)
	}

	afterSupport, err := os.ReadFile(support)
	if err != nil {
		t.Fatalf("last good support script removed: %v", err)
	}
	if string(afterSupport) != string(supportBytes) {
		t.Fatalf("support script bytes changed to %q", afterSupport)
	}
	afterSkillDoc, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("last good SKILL.md removed: %v", err)
	}
	if string(afterSkillDoc) != string(skillDoc) {
		t.Fatal("installed SKILL.md bytes changed")
	}
	var bookkeeping []string
	_ = filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && (d.Name() == "artifact.tar.gz" || d.Name() == "verified.marker") {
			bookkeeping = append(bookkeeping, path)
		}
		return nil
	})
	if len(bookkeeping) > 0 {
		t.Fatalf("artifact-cache bookkeeping leaked into installed skill: %v", bookkeeping)
	}
}
