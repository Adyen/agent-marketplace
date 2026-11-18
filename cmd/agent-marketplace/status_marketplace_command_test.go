package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestStatusReportsMarketplacePinsWithoutDroppingThem(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	}))
	defer server.Close()

	repoRoot := t.TempDir()
	writeConfig(t, repoRoot+"/.agents/config.yaml", `
apiVersion: agent-marketplace/v4
sources:
  team: {marketplace: `+server.URL+`}
resources:
  team:
    include:
      - source-slug/skill/review: {as: review-installed}
`)
	writeFile(t, repoRoot+"/.agents/lock.yaml", `apiVersion: agent-marketplace-lock/v2
scope: repo
marketplaces:
  - url: `+server.URL+`
    instance_id: mp
    registry_bindings:
      - name: source-slug
        registry_id: src
    resources:
      - registry_name: source-slug
        registry_id: src
        kind: skill
        name: review
        revision: v1
        content_digest: `+contentDigest+`
        archive_digest: sha256:`+strings.Repeat("b", 64)+`
        acquisition: mirror
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}
	for _, needle := range []string{
		"MARKETPLACE PINS", "ALIAS", "REGISTRY_SLUG", "INSTALLED_NAME", "team", "source-slug", "review-installed", "v1", contentDigest,
		"current", "false", "REPO",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected Marketplace status output to contain %q, got:\n%s", needle, output)
		}
	}
}

// spec: status-reporting/lock-required-error-keeps-the-declared-harnesses
func TestStatusKeepsConfiguredHarnessesWhenMarketplaceLockIsMissing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	server := newV4MarketplaceLockServer(t)
	defer server.Close()

	for _, tc := range []struct {
		name        string
		userConfig  string
		errorCode   string
		wantHarness bool
		duplicate   bool
	}{
		{
			name:        "user harness declaration",
			userConfig:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
			errorCode:   "v4.marketplace-lock-required",
			wantHarness: true,
		},
		{
			name:       "no configured harnesses",
			userConfig: "apiVersion: agent-marketplace/v4\n",
			errorCode:  "v4.marketplace-lock-required",
		},
		{
			name:       "invalid user configuration",
			userConfig: "apiVersion: agent-marketplace/v3\nharnesses: [factory]\n",
			errorCode:  "v4.api-version",
		},
		{
			name:       "duplicate Marketplace endpoint declaration",
			userConfig: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
			errorCode:  "v4.marketplace-lock-mismatch",
			duplicate:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			writeFile(t, filepath.Join(home, ".agents", "config.yaml"), tc.userConfig)

			repo := t.TempDir()
			sources := "  hosted: {marketplace: " + server.URL + "}\n"
			if tc.duplicate {
				sources += "  alias: {marketplace: " + server.URL + "}\n"
			}
			writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n"+sources+`resources:
  hosted: {include: [tools/skill/review]}
`)
			withWorkingDir(t, repo)

			output, err := executeCommand("status")
			if err == nil || (!strings.Contains(output, tc.errorCode) && !strings.Contains(err.Error(), tc.errorCode)) {
				t.Fatalf("status error = %v, want nonzero %s:\n%s", err, tc.errorCode, output)
			}
			if tc.duplicate && !strings.Contains(output+"\n"+err.Error(), "duplicate Marketplace source declaration") {
				t.Fatalf("status error = %v, want duplicate Marketplace source diagnostic:\n%s", err, output)
			}

			start := strings.Index(output, "TARGET HARNESS SET")
			if start < 0 {
				t.Fatalf("status omitted TARGET HARNESS SET:\n%s", output)
			}
			targetSet := output[start:]
			if end := strings.Index(targetSet, "\n\n"); end >= 0 {
				targetSet = targetSet[:end]
			}
			if tc.wantHarness {
				if !strings.Contains(targetSet, "factory") || !strings.Contains(targetSet, "USER") {
					t.Fatalf("TARGET HARNESS SET row = %q, want factory with source USER:\n%s", targetSet, output)
				}
				if strings.Contains(output, "add harnesses to ~/.agents/config.yaml") {
					t.Fatalf("status suggested adding already-declared harnesses:\n%s", output)
				}
			} else {
				if !strings.Contains(targetSet, "none configured") {
					t.Fatalf("TARGET HARNESS SET row = %q, want none configured:\n%s", targetSet, output)
				}
				if strings.Contains(targetSet, "factory") {
					t.Fatalf("TARGET HARNESS SET used harnesses from an invalid or empty configuration:\n%s", output)
				}
				if !strings.Contains(output, "add harnesses to ~/.agents/config.yaml") {
					t.Fatalf("status omitted the existing add-harnesses hint:\n%s", output)
				}
			}
		})
	}
}
