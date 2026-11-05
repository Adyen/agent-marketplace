package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestDeinitCachedOrganizationWithStoppedEndpoint(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	pin, revision, raw := deinitMarketplaceFixture(t, "org-review", "org-source")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("deinit-marketplace", "https://example.com"))
		case r.URL.Path == "/api/v1/registries/org-source":
			_ = json.NewEncoder(w).Encode(mpapi.PublicRegistry{ID: "org-source", Slug: "org-source", Origins: []string{"https://example.com/org-source.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case strings.Contains(r.URL.Path, "/revisions/"+pin.Digest):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	orgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted:\n    include:\n      - org-source/skill/org-review: {revision: "+pin.Digest+"}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	personal := t.TempDir()
	writeSkill(t, personal, "user-review", "User review", nil)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  personal: {directory: "+personal+"}\nresources:\n  personal: [skill/user-review]\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("warm Marketplace sync: %v\n%s", err, output)
	}
	if requests.Load() == 0 {
		t.Fatal("warm sync did not acquire real server evidence")
	}
	assertFactorySkillExists(t, home, "org-review")
	assertFactorySkillExists(t, home, "user-review")
	server.Close()
	if output, err := executeCommand("deinit"); err != nil {
		t.Fatalf("teardown with stopped endpoint: %v\n%s", err, output)
	}
	assertFactorySkillExists(t, home, "org-review")
	assertSkillLinkAbsent(t, home, "user-review")
	if _, err := os.Lstat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("teardown published an organization lock: %v", err)
	}
}

func TestDeinitCachedOrganizationEvidencePreservesOwnership(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, name := range []string{
		"warm", "dry run", "cold", "missing metadata", "missing artifact", "tampered artifact",
		"symlinked metadata", "symlinked artifact", "wrong coordinate", "wrong digest", "missing identity", "revoked",
		"floating request", "changed request", "unsupported root", "untrusted root", "deployed mismatch", "source only",
	} {
		t.Run(name, func(t *testing.T) {
			scenario := setupDeinitOrganizationMarketplace(t)
			orgPath := config.OrganizationConfigPath()
			repo := config.FindRepoRoot()
			intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: repo})
			if len(intent.Errors) != 0 {
				t.Fatal(intent.Errors)
			}
			endpoint := intent.Layers[config.Organization].MarketplaceSources()[0].URL
			digest := string(scenario.revision.ContentDigest)
			metadataPath := scenario.cache.MetadataPath(endpoint, "org-source", "skill", "org-review", digest)
			artifactPath := scenario.cache.ArtifactPath(string(scenario.revision.ArchiveDigest))

			// Install real user-owned output alongside the organization output, so
			// a successful teardown must prune something while retaining handwritten data.
			userSource := t.TempDir()
			writeSkill(t, userSource, "user-review", "User review", nil)
			userPath := filepath.Join(scenario.homeDir, ".agents", "config.yaml")
			writeConfig(t, userPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  personal: {directory: "+userSource+"}\nresources:\n  personal: [skill/user-review]\n")
			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("seed real user-owned output: %v\n%s", err, output)
			}
			assertFactorySkillExists(t, scenario.homeDir, "user-review")
			handwritten := filepath.Join(scenario.homeDir, ".factory", "skills", "handwritten", "SKILL.md")
			writeFile(t, handwritten, "---\nname: handwritten\ndescription: Mine\n---\nKeep this content.\n")
			orgOutput := filepath.Join(scenario.homeDir, ".factory", "skills", "org-review", "SKILL.md")
			userOutput := filepath.Join(scenario.homeDir, ".factory", "skills", "user-review", "SKILL.md")

			switch name {
			case "cold":
				if err := os.RemoveAll(filepath.Dir(filepath.Dir(metadataPath))); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(filepath.Dir(artifactPath)); err != nil {
					t.Fatal(err)
				}
			case "missing metadata":
				// A valid archive plus metadata in an unrelated endpoint namespace
				// must not supply identity for this exact configured request.
				revision, ok, err := scenario.cache.ReadResourceRevision(endpoint, "org-source", "skill", "org-review", digest)
				if err != nil || !ok {
					t.Fatalf("read warm metadata: %t, %v", ok, err)
				}
				if _, err := scenario.cache.WriteResourceRevision("https://unrelated.example", "org-source", "skill", "org-review", digest, revision); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(metadataPath); err != nil {
					t.Fatal(err)
				}
			case "missing artifact":
				if err := os.Remove(artifactPath); err != nil {
					t.Fatal(err)
				}
			case "tampered artifact":
				writeFile(t, artifactPath, "not the verified archive\n")
			case "untrusted root":
				if err := os.Chmod(orgPath, 0o666); err != nil {
					t.Fatal(err)
				}
			case "deployed mismatch":
				writeFile(t, filepath.Join(filepath.Dir(orgPath), "lock.yaml"), "apiVersion: agent-marketplace-lock/v2\nscope: organization\n")
			case "symlinked metadata", "symlinked artifact":
				path := metadataPath
				if name == "symlinked artifact" {
					path = artifactPath
				}
				external := filepath.Join(t.TempDir(), "external")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, external, string(data))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					got, err := os.ReadFile(external)
					if err != nil || !bytes.Equal(got, data) {
						t.Errorf("symlink target changed: %v", err)
					}
				})
			case "wrong coordinate", "wrong digest", "missing identity", "revoked":
				revision, ok, err := scenario.cache.ReadResourceRevision(endpoint, "org-source", "skill", "org-review", digest)
				if err != nil || !ok {
					t.Fatalf("read warm metadata: %t, %v", ok, err)
				}
				switch name {
				case "wrong coordinate":
					revision.Coordinate.Name = "other-resource"
				case "wrong digest":
					revision.ContentDigest = store.RevisionDigest("sha256:" + strings.Repeat("b", 64))
				case "missing identity":
					revision.Coordinate.Marketplace = ""
				case "revoked":
					revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: digest, Reason: "withdrawn", AppliedAt: time.Now().UTC()}
				}
				if _, err := scenario.cache.WriteResourceRevision(endpoint, "org-source", "skill", "org-review", digest, revision); err != nil {
					t.Fatal(err)
				}
			case "floating request", "changed request", "unsupported root", "source only":
				data, err := os.ReadFile(orgPath)
				if err != nil {
					t.Fatal(err)
				}
				switch name {
				case "floating request":
					data = bytes.ReplaceAll(data, []byte("{revision: "+digest+"}"), []byte("{}"))
				case "changed request":
					data = bytes.ReplaceAll(data, []byte(digest), []byte("sha256:"+strings.Repeat("a", 64)))
				case "unsupported root":
					data = bytes.ReplaceAll(data, []byte("agent-marketplace/v4"), []byte("agent-marketplace/v3"))
				case "source only":
					data, _, _ = bytes.Cut(data, []byte("resources:\n"))
				}
				writeFile(t, orgPath, string(data))
			}

			preserved := map[string][]byte{}
			for _, path := range []string{orgPath, userPath, handwritten, orgOutput} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				preserved[path] = data
			}
			if name == "source only" {
				delete(preserved, orgOutput)
			}
			if name != "warm" && name != "source only" {
				data, err := os.ReadFile(userOutput)
				if err != nil {
					t.Fatal(err)
				}
				preserved[userOutput] = data
			}
			scenario.failEndpoint.Store(true)
			args := []string{"deinit"}
			if name == "dry run" {
				args = append(args, "--dry-run")
			}
			output, err := executeCommand(args...)
			if name == "warm" || name == "dry run" || name == "source only" {
				if err != nil {
					t.Fatalf("cached teardown: %v\n%s", err, output)
				}
			} else if err == nil {
				t.Fatalf("unsafe evidence allowed teardown:\n%s", output)
			}
			if name == "unsupported root" {
				assertCommandFormatRefusal(t, err, "v4.api-version", orgPath)
			}
			if name == "untrusted root" {
				var trustErr *config.OrganizationTrustError
				if !errors.As(err, &trustErr) || trustErr.Code != "organization.trust.config.writable" {
					t.Fatalf("cached evidence bypassed organization trust: %v", err)
				}
			}
			if name == "deployed mismatch" {
				var lockErr *config.V4ValidationError
				if !errors.As(err, &lockErr) || lockErr.Code != "v4.marketplace-lock-mismatch" {
					t.Fatalf("cached evidence bypassed deployed exact lock: %v", err)
				}
			}
			if name == "revoked" {
				var pinErr *marketplace.PinError
				if !errors.As(err, &pinErr) {
					t.Fatalf("revocation did not reach cached pin policy: %v", err)
				}
			}
			if scenario.requests.Load() != 0 {
				t.Errorf("teardown contacted offline endpoint %d time(s)", scenario.requests.Load())
			}
			if name == "warm" || name == "source only" {
				assertSkillLinkAbsent(t, scenario.homeDir, "user-review")
			}
			if name == "source only" {
				assertSkillLinkAbsent(t, scenario.homeDir, "org-review")
			}
			for path, want := range preserved {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("teardown changed preserved file %s: %v", path, err)
				}
			}
			orgLockPath := filepath.Join(filepath.Dir(orgPath), "lock.yaml")
			if name == "deployed mismatch" {
				if data, err := os.ReadFile(orgLockPath); err != nil || string(data) != "apiVersion: agent-marketplace-lock/v2\nscope: organization\n" {
					t.Errorf("teardown replaced deployed lock: %q, %v", data, err)
				}
			} else if _, err := os.Lstat(orgLockPath); !os.IsNotExist(err) {
				t.Errorf("teardown published an organization lock: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(userPath), "lock.yaml")); !os.IsNotExist(err) {
				t.Errorf("teardown bootstrapped a user lock: %v", err)
			}
			if name == "warm" || name == "dry run" {
				// A separate strict read must not start accepting cached/transient
				// evidence merely because teardown could reconstruct its desired set.
				strict := pipeline.LoadEffective(context.Background(), pipeline.ConfigLoadStrict, config.LoadEffectiveOptions{RepoRoot: repo}, nil)
				if !pipeline.NeedsLockBootstrap(strict.Errors) {
					t.Fatalf("strict load adopted teardown evidence: %v", strict.Errors)
				}
			}
		})
	}
}
