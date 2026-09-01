package e2e_test

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

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestMarketplaceIndexOnlyRegistryMoveOfflineAndAllKinds(t *testing.T) {
	sourceRepo := t.TempDir()
	writeMarketplaceAllKindsSource(t, sourceRepo)
	gitRun(t, sourceRepo, "init", "-b", "main")
	gitRun(t, sourceRepo, "config", "user.email", "e2e@example.com")
	gitRun(t, sourceRepo, "config", "user.name", "E2E")
	gitRun(t, sourceRepo, "add", ".")
	gitRun(t, sourceRepo, "commit", "-m", "initial")
	commit := strings.TrimSpace(gitOutput(t, sourceRepo, "rev-parse", "HEAD"))
	originOne := "file://" + sourceRepo
	movedRepo := t.TempDir()
	gitRun(t, "", "clone", "--bare", sourceRepo, movedRepo)
	originTwo := "file://" + movedRepo

	acquired, err := acquire.CloneAtCommit(context.Background(), originOne, "main", commit, acquire.Credentials{}, acquire.Limits{RepositoryBytes: 100 * 1024 * 1024, FileCount: 1024, FileBytes: 2 * 1024 * 1024}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer acquired.Release()
	registry := store.Registry{ID: "source-id", Slug: "source", Ref: "main", Origins: []string{originOne}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: []store.RegistryRoot{{Path: ".", SourceHarness: "claude"}}}
	built, err := ingest.BuildRegistryRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "marketplace-id", registry, acquired.Dir, commit)
	if err != nil {
		t.Fatalf("build registry revision: %v", err)
	}

	var revisions []store.AssessedResourceRevision
	for _, revision := range built.Resources {
		if revision.Coordinate.Kind == "collection" {
			continue
		}
		var assessment store.ResourceAssessment
		for _, candidate := range built.Assessments {
			if candidate.Coordinate == revision.Coordinate && candidate.ContentDigest == revision.ContentDigest {
				assessment = candidate
				break
			}
		}
		var occurrence store.ResourceOccurrence
		for _, candidate := range built.ResourceOccurrences {
			if candidate.Coordinate == revision.Coordinate && candidate.ContentDigest == revision.ContentDigest {
				occurrence = candidate
				break
			}
		}
		revisions = append(revisions, store.AssessedResourceRevision{ResourceRevision: revision, Assessment: assessment, Occurrence: occurrence})
	}
	if len(revisions) != 5 {
		t.Fatalf("registry resources = %d, want all five materialized kinds", len(revisions))
	}
	currentOrigin := originOne
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("marketplace-id", "https://example.invalid"))
		case strings.Contains(r.URL.Path, "/occurrences/"):
			_ = json.NewEncoder(w).Encode(mpapi.ExactRegistryOccurrence{RegistryID: "source-id", Ref: "main", Roots: registry.Roots, Commit: commit, RegistryRevisionManifestDigest: built.ManifestDigest, Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: currentOrigin, OccurredAt: time.Now().UTC()})
		case strings.Contains(r.URL.Path, "/registries/"):
			_ = json.NewEncoder(w).Encode(mpapi.PublicRegistry{ID: "source-id", Slug: "source", Origins: []string{currentOrigin}, Ref: "main", Roots: registry.Roots, Acquisition: store.AcquisitionIndexOnly, Listed: true})
		case strings.Contains(r.URL.Path, "/resources/"):
			for _, revision := range revisions {
				if strings.Contains(r.URL.Path, "/"+revision.Coordinate.Kind+"/"+revision.Coordinate.Name) {
					_ = json.NewEncoder(w).Encode(revision)
					return
				}
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	env := newEnv(t)
	var resourceYAML strings.Builder
	for _, revision := range revisions {
		fmt.Fprintf(&resourceYAML, "      - registry_id: source-id\n        kind: %s\n        name: %s\n        revision: %s\n        digest: %s\n        acquisition: index-only\n        registry_commit: %s\n        registry_manifest_digest: %s\n", revision.Coordinate.Kind, revision.Coordinate.Name, revision.ContentDigest, revision.ContentDigest, commit, built.ManifestDigest)
	}
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []")
	env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: %s\n    instance_id: marketplace-id\n    resources:\n%s", server.URL, resourceYAML.String()))

	if result := env.RunAM("sync"); result.ExitCode != 0 {
		t.Fatalf("initial online sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	for _, path := range []string{"/workspace/.factory/skills/skill-one/SKILL.md", "/workspace/.factory/commands/command-one.md", "/workspace/.factory/droids/agent-one.md", "/workspace/.factory/hooks/hooks.json", "/workspace/.factory/mcp.json"} {
		env.AssertFileExists(path)
	}

	// Remove the old origin before the next online sync. Success proves the
	// resolver used only the occurrence's current origin, not origin history.
	currentOrigin = originTwo
	if err := os.RemoveAll(sourceRepo); err != nil {
		t.Fatal(err)
	}
	if result := env.RunAM("sync"); result.ExitCode != 0 {
		t.Fatalf("registry-move online sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if err := os.RemoveAll(movedRepo); err != nil {
		t.Fatal(err)
	}
	requestsBeforeOffline := requests
	if result := env.RunAM("sync", "--offline"); result.ExitCode != 0 {
		t.Fatalf("offline sync failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if requests != requestsBeforeOffline {
		t.Fatalf("offline sync contacted Marketplace %d additional time(s)", requests-requestsBeforeOffline)
	}
}

func writeMarketplaceAllKindsSource(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"skills/skill-one/SKILL.md": "---\nname: skill-one\ndescription: Skill\nlicense: MIT\n---\nskill\n",
		"commands/command-one.md":   "---\nname: command-one\ndescription: Command\n---\ncommand\n",
		"agents/agent-one.md":       "---\nname: agent-one\ndescription: Agent\n---\nagent\n",
		"hooks/hooks.json":          `{"hooks":{"PreToolUse":[{"type":"command","command":"echo hook","matcher":"Read"}]}}`,
		".mcp.json":                 `{"mcpServers":{"server-one":{"command":"echo","args":["server"]}}}`,
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(output)
}
