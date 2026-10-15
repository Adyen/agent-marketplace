package marketplace

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
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func bindingMirrorServer(t *testing.T, raw []byte, contentDigest, archiveDigest string) string {
	t.Helper()
	rev := assessedRevision(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "approved"}, ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest)})
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(rev) })
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) })
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func bindingMirrorConfig(url, digest, installedName string) *config.Config {
	return &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: url, InstanceID: "mp", Source: config.RepoCommitted, Resources: []config.MarketplaceResourcePin{{RegistryID: "src", Kind: "skill", Name: "approved", InstalledName: installedName, Revision: "main", Digest: digest}}}}}
}

func TestResolverRejectsMirrorPayloadIdentityAndInvalidSource(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		wrong     bool
		installed string
	}{
		{name: "identity mismatch", wrong: true},
		{name: "absolute source", source: filepath.Join(t.TempDir(), "SKILL.md")},
		{name: "traversal source", source: "../SKILL.md"},
		{name: "backslash source", source: `skills\SKILL.md`},
		{name: "relative source", source: "skills/SKILL.md"},
		{name: "empty source"},
		{name: "installed alias", installed: "local-review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "approved"
			if tc.wrong {
				name = "shadow"
			}
			skill := ir.NewSkill(name, "description", "body", []string{"user"}, nil, ir.SourceRef{File: tc.source})
			artifact, err := hccartifact.FromIR(skill)
			if err != nil {
				t.Fatal(err)
			}
			artifact.Manifest.RegistryID = "src"
			artifact.Manifest.Name = "approved"
			artifact.Manifest.Revision = "main"
			raw, archiveDigest, err := hccartifact.Encode(artifact)
			if err != nil {
				t.Fatal(err)
			}
			cache := NewCache(t.TempDir())
			url := bindingMirrorServer(t, raw, artifact.Manifest.ContentDigest, archiveDigest)
			cfg := bindingMirrorConfig(url, artifact.Manifest.ContentDigest, tc.installed)
			if _, err := cache.WriteArtifact(archiveDigest, raw); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkArtifactVerified(archiveDigest); err != nil {
				t.Fatal(err)
			}
			if errs := config.ValidateMarketplaceEntries(cfg.Marketplaces); len(errs) > 0 {
				t.Fatalf("config validation: %v", errs)
			}
			resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
			if tc.wrong {
				if !errors.Is(firstError(errs), ErrArtifactIdentityMismatch) || len(resolved) != 0 {
					t.Fatalf("identity result = %#v errors=%v", resolved, errs)
				}
			} else if tc.source == "" || tc.source == "skills/SKILL.md" {
				if len(errs) != 0 || len(resolved) != 1 {
					t.Fatalf("valid source result = %#v errors=%v", resolved, errs)
				}
				if resolved[0].Name != "approved" && tc.installed == "" || tc.installed != "" && (resolved[0].Name != tc.installed || resolved[0].Resource.Name() != "approved") {
					t.Fatalf("installed alias result = name=%q resource=%q", resolved[0].Name, resolved[0].Resource.Name())
				}
			} else if !errors.Is(firstError(errs), ErrArtifactSourcePath) || len(resolved) != 0 {
				t.Fatalf("invalid source result = %#v errors=%v", resolved, errs)
			}
			_, verified, err := cache.ReadArtifact(archiveDigest)
			if err != nil {
				t.Fatal(err)
			}
			if verified && tc.wrong || verified && tc.source != "" && tc.source != "skills/SKILL.md" {
				t.Fatalf("rejected artifact remained verified: %v", verified)
			}
		})
	}
}

func firstError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}

func TestIndexOnlyStagingNamesSeparateSameKindResources(t *testing.T) {
	cache := NewCache(t.TempDir())
	checkout := t.TempDir()
	for _, name := range []string{"first", "second"} {
		path := filepath.Join(checkout, ".agents", "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	registry := store.Registry{Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: strings.Repeat("a", 40), ManifestDigest: "sha256:" + strings.Repeat("b", 64)}
	legacy := filepath.Join(cache.root, "compiled", "marketplace-sources", IndexOnlyKey(identity), "skill")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyFile := filepath.Join(legacy, "SKILL.md")
	if err := os.WriteFile(legacyFile, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstResource := ir.NewSkill("first", "description", "body", []string{"user"}, nil, ir.SourceRef{})
	first, err := stageReconstructedResource(cache, identity, checkout, registry, firstResource, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondResource := ir.NewSkill("second", "description", "body", []string{"user"}, nil, ir.SourceRef{})
	second, err := stageReconstructedResource(cache, identity, checkout, registry, secondResource, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(first, string(filepath.Separator)+"first"+string(filepath.Separator)) || !strings.Contains(second, string(filepath.Separator)+"second"+string(filepath.Separator)) {
		t.Fatalf("staging paths = %q, %q", first, second)
	}
	firstData, _ := os.ReadFile(first)
	secondData, _ := os.ReadFile(second)
	if !bytes.Equal(firstData, []byte("first")) || !bytes.Equal(secondData, []byte("second")) {
		t.Fatalf("staged bytes = %q / %q", firstData, secondData)
	}
	legacyData, err := os.ReadFile(legacyFile)
	if err != nil || string(legacyData) != "legacy" {
		t.Fatalf("legacy resource changed: %q err=%v", legacyData, err)
	}
}

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
func TestHookDefinitionBasenameRequiresSafeCanonicalSourceFile(t *testing.T) {
	for _, tc := range []struct {
		name, sourceFile, want string
		invalid                bool
	}{
		{name: "settings", sourceFile: ".claude/settings.json", want: "settings.json"},
		{name: "hooks v1", sourceFile: ".devin/hooks.v1.json", want: "hooks.v1.json"},
		{name: "nested legacy", sourceFile: "hooks/hooks.json", want: "hooks.json"},
		{name: "empty", invalid: true},
		{name: "dot", sourceFile: ".", invalid: true},
		{name: "parent", sourceFile: "..", invalid: true},
		{name: "parent prefix", sourceFile: "../settings.json", invalid: true},
		{name: "embedded traversal", sourceFile: "hooks/../settings.json", invalid: true},
		{name: "absolute", sourceFile: "/etc/hooks.json", invalid: true},
		{name: "backslash", sourceFile: `hooks\hooks.json`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := hookDefinitionBasename(tc.sourceFile)
			if tc.invalid {
				if !errors.Is(err, ErrArtifactSourcePath) {
					t.Fatalf("hookDefinitionBasename(%q) error = %v, want ErrArtifactSourcePath", tc.sourceFile, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("hookDefinitionBasename(%q) = %q, %v; want %q", tc.sourceFile, got, err, tc.want)
			}
		})
	}
}
