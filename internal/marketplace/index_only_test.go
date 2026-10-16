package marketplace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestIndexOnlyResolverChecksExactCommitAndReingestedDigests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nlicense: MIT\n---\nReview the code.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "docs", "support.txt"), []byte("support material\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "--initial-branch=main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-q", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(out))
	origin := "file://" + repo
	registry := store.Registry{ID: "src", Slug: "source", Ref: "main", Origins: []string{origin}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	built, err := ingest.BuildRegistryRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "mp", registry, repo, commit)
	if err != nil {
		t.Fatalf("build reference revision: %v", err)
	}
	var revision store.AssessedResourceRevision
	for _, candidate := range built.Resources {
		if candidate.Coordinate.Kind == "skill" && candidate.Coordinate.Name == "review" {
			revision = assessedRevisionOccurrence(candidate, commit, string(built.ManifestDigest))
			break
		}
	}
	if revision.ContentDigest == "" || built.ManifestDigest == "" {
		t.Fatalf("reference revision is incomplete: %#v / %q", revision, built.ManifestDigest)
	}
	revision.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: string(revision.ContentDigest), Reason: "test revocation"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/occurrences/"):
			_ = json.NewEncoder(w).Encode(api.ExactRegistryOccurrence{RegistryID: "src", Ref: "main", Roots: registry.Roots, Commit: commit, RegistryRevisionManifestDigest: built.ManifestDigest, Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: origin, OccurredAt: revisionTime()})
		default:
			_ = json.NewEncoder(w).Encode(api.PublicRegistry{ID: "src", Slug: "source", Origins: []string{origin}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".wrong", SourceHarness: "factory"}}, Acquisition: store.AcquisitionIndexOnly, Listed: true})
		}
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	mux.HandleFunc("/api/v1/manifests/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(built.ManifestPayload) })
	server := httptest.NewServer(mux)
	defer server.Close()
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: string(revision.ContentDigest), Digest: string(revision.ContentDigest), Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: string(built.ManifestDigest)}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	r := &Resolver{Cache: NewCache(t.TempDir())}
	if resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh); len(resolved) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "revoked") {
		t.Fatalf("index-only revoked result without override = %d artifacts, errors %v", len(resolved), errs)
	}
	policy, err := NewRevocationOverridePolicy([]string{"mp/src/skill/review@" + string(revision.ContentDigest)})
	if err != nil {
		t.Fatalf("NewRevocationOverridePolicy: %v", err)
	}
	resolved, errs := r.ResolveWithPolicy(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh, policy)
	if len(errs) != 0 || len(resolved) != 1 || !resolved[0].Revoked {
		t.Fatalf("index-only exact revoked override result = %d artifacts, errors %v, want one revoked artifact", len(resolved), errs)
	}
	if resolved[0].Resource == nil || resolved[0].Path == "" || strings.Contains(resolved[0].Path, "artifact.tar.gz") || strings.Contains(resolved[0].Path, "verified.marker") {
		t.Fatalf("index-only resource must use a reconstructed source path, not cache internals: %#v", resolved[0])
	}
	if _, err := materialize.Run(context.Background(), repo, resolved); err != nil {
		t.Fatalf("materialize reconstructed index-only skill: %v", err)
	}
	materializedSkill := filepath.Join(repo, ".factory", "skills", "review")
	if _, err := os.Stat(filepath.Join(materializedSkill, "docs", "support.txt")); err != nil {
		t.Fatalf("reconstructed support file was not materialized: %v", err)
	}
	var leaked []string
	_ = filepath.WalkDir(materializedSkill, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && entry.Name() != "review" && (entry.Name() == "artifact.tar.gz" || entry.Name() == "verified.marker") {
			leaked = append(leaked, path)
		}
		return nil
	})
	if len(leaked) != 0 {
		t.Fatalf("cache internals leaked into materialized skill: %v", leaked)
	}

	// Cached-no-fetch must use only the occurrence-scoped verified state. The
	// resolver must not construct or contact the still-running Marketplace endpoint.
	offlineResolved, offlineErrs := r.ResolveWithPolicy(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeCachedNoFetch, policy)
	if len(offlineErrs) != 0 || len(offlineResolved) != 1 || offlineResolved[0].Resource == nil {
		t.Fatalf("offline index-only resolve = %d artifacts, errors %v", len(offlineResolved), offlineErrs)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(offlineResolved[0].Path), "docs", "support.txt")); err != nil {
		t.Fatalf("offline reconstructed support file missing: %v", err)
	}

	badDigest := "sha256:" + strings.Repeat("f", 64)
	badPin := pin
	badPin.Digest, badPin.Revision = badDigest, badDigest
	badCfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{badPin}}}}
	if _, errs := r.ResolveWithPolicy(context.Background(), badCfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh, policy); len(errs) == 0 || !strings.Contains(errs[0].Error(), "digest") {
		t.Fatalf("content digest mismatch errors = %v", errs)
	}
	badManifestPin := pin
	badManifestPin.RegistryManifestDigest = "sha256:" + strings.Repeat("f", 64)
	badManifestCfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{badManifestPin}}}}
	if _, errs := r.ResolveWithPolicy(context.Background(), badManifestCfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh, policy); len(errs) == 0 || !strings.Contains(errs[0].Error(), "manifest") {
		t.Fatalf("registry manifest mismatch errors = %v", errs)
	}
}

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
func TestIndexOnlyResolverStagesNativeHookDefinitionsAndMaterializesOffline(t *testing.T) {
	isolateHarnessState(t)
	repo := t.TempDir()
	type sourceCase struct {
		name             string
		harness          hcc.Harness
		target           hcc.Target
		sourceFile       string
		definition       []byte
		nativeFile       string
		payload          string
		peerFile         string
		peerContents     string
		peerMarker       string
		supportScript    []byte
		supportMode      os.FileMode
		installedName    string
		extraSourceFiles map[string][]byte
		hookName         string
	}
	cases := []sourceCase{
		{name: "claude", harness: hcc.HarnessClaude, target: hcc.TargetClaude, sourceFile: ".claude/settings.json", definition: []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo index-claude"}]}]}}`), nativeFile: ".claude/settings.json", payload: "echo index-claude"},
		{
			name: "factory", harness: hcc.HarnessFactory, target: hcc.TargetFactory,
			sourceFile: ".factory/hooks.json",
			definition: []byte(`{"PostToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/check.sh"}]}]}`),
			nativeFile: ".factory/hooks.json", payload: "scripts/check.sh",
			peerFile: ".factory/mcp.json", peerContents: `{"mcpServers":{"peer-factory":{"command":"peer-binary"}}}`, peerMarker: "peer-factory",
			supportScript: []byte("#!/bin/sh\necho index-factory support\n"), supportMode: 0o755, installedName: "installed-factory",
			extraSourceFiles: map[string][]byte{
				".factory/bundle/scripts/nested.sh": []byte("echo nested payload\n"),
				"verified.marker":                   []byte("staging metadata\n"),
				"sibling/other.sh":                  []byte("not hook support\n"),
			},
		},
		{name: "cursor", harness: hcc.HarnessCursor, target: hcc.TargetCursor, sourceFile: ".cursor/hooks.json", definition: []byte(`{"version":1,"hooks":{"beforeSubmitPrompt":[{"type":"command","command":"echo index-cursor"}]}}`), nativeFile: ".cursor/hooks.json", payload: "echo index-cursor"},
		{name: "windsurf", harness: hcc.HarnessWindsurf, target: hcc.TargetWindsurf, sourceFile: ".devin/hooks.json", definition: []byte(`{"hooks":{"pre_run_command":[{"type":"command","command":"echo index-windsurf"}]}}`), nativeFile: ".devin/hooks.json", payload: "echo index-windsurf"},
		{name: "gemini", harness: hcc.HarnessGemini, target: hcc.TargetGemini, sourceFile: ".gemini/settings.json", definition: []byte(`{"hooks":{"BeforeTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":"echo index-gemini"}]}]}}`), nativeFile: ".gemini/settings.json", payload: "echo index-gemini", peerFile: ".gemini/gemini-extension.json", peerContents: `{"mcpServers":{"peer-gemini":{"command":"peer-binary"}}}`, peerMarker: "peer-gemini"},
		{name: "devin", harness: hcc.HarnessDevin, target: hcc.TargetDevin, sourceFile: ".devin/hooks.v1.json", definition: []byte(`{"PostCompaction":[{"type":"prompt","prompt":"index Devin prompt"}]}`), nativeFile: ".devin/hooks.v1.json", payload: "index Devin prompt"},
		{name: "codex", harness: hcc.HarnessCodex, target: hcc.TargetCodex, sourceFile: ".codex/hooks.json", definition: []byte(`{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo index-codex"}]}]}}`), nativeFile: ".codex/hooks.json", payload: "echo index-codex"},
	}
	registryRoots := make([]store.RegistryRoot, 0, len(cases))
	for i := range cases {
		tc := &cases[i]
		rootPath := filepath.ToSlash(filepath.Join(".agents", "native", tc.name))
		rootDir := filepath.Join(repo, filepath.FromSlash(rootPath))
		writeSourceFile := func(rel string, data []byte, mode os.FileMode) {
			t.Helper()
			filePath := filepath.Join(rootDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filePath, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if mode != 0 {
				if err := os.Chmod(filePath, mode); err != nil {
					t.Fatal(err)
				}
			}
		}
		writeSourceFile(tc.sourceFile, tc.definition, 0o644)
		if len(tc.supportScript) > 0 {
			supportPath := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(tc.sourceFile)), "scripts", "check.sh"))
			writeSourceFile(supportPath, tc.supportScript, tc.supportMode)
		}
		if tc.peerFile != "" {
			writeSourceFile(tc.peerFile, []byte(tc.peerContents), 0o644)
		}
		extraPaths := make([]string, 0, len(tc.extraSourceFiles))
		for rel := range tc.extraSourceFiles {
			extraPaths = append(extraPaths, rel)
		}
		sort.Strings(extraPaths)
		for _, rel := range extraPaths {
			writeSourceFile(rel, tc.extraSourceFiles[rel], 0o644)
		}
		bundle, err := hcc.IngestBundle(os.DirFS(rootDir), tc.harness)
		if err != nil || diag.HasErrors(bundle.Diagnostics) {
			t.Fatalf("IngestBundle(%s): %v / %+v", tc.harness, err, bundle.Diagnostics)
		}
		for _, resource := range bundle.Resources {
			if hook, ok := resource.(*ir.Hook); ok {
				if tc.hookName != "" {
					t.Fatalf("IngestBundle(%s) produced multiple hook definitions", tc.harness)
				}
				tc.hookName = hook.Name()
				if hook.Source.File != tc.sourceFile {
					t.Fatalf("IngestBundle(%s) source file = %q, want %q", tc.harness, hook.Source.File, tc.sourceFile)
				}
			}
		}
		if tc.hookName == "" {
			t.Fatalf("IngestBundle(%s) produced no hook", tc.harness)
		}
		registryRoots = append(registryRoots, store.RegistryRoot{Path: rootPath, SourceHarness: string(tc.harness)})
	}
	for _, args := range [][]string{{"init", "-q", "--initial-branch=main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-q", "-m", "native hook sources"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	commitBytes, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(commitBytes))
	origin := "file://" + repo
	registry := store.Registry{ID: "src", Slug: "source", Ref: "main", Origins: []string{origin}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: registryRoots}
	built, err := ingest.BuildRegistryRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "mp", registry, repo, commit)
	if err != nil {
		t.Fatalf("build reference revision: %v", err)
	}
	revisions := make(map[string]store.AssessedResourceRevision, len(built.Resources))
	for _, candidate := range built.Resources {
		revisions[string(candidate.Coordinate.Kind)+"\x00"+candidate.Coordinate.Name] = assessedRevisionOccurrence(candidate, commit, string(built.ManifestDigest))
	}
	var requests atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeTestWellKnown(w, "mp")
	})
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.Contains(r.URL.Path, "/occurrences/") {
			_ = json.NewEncoder(w).Encode(api.ExactRegistryOccurrence{RegistryID: "src", Ref: "main", Roots: registry.Roots, Commit: commit, RegistryRevisionManifestDigest: built.ManifestDigest, Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: origin, OccurredAt: revisionTime()})
			return
		}
		_ = json.NewEncoder(w).Encode(api.PublicRegistry{ID: "src", Slug: "source", Origins: registry.Origins, Ref: "main", Roots: registry.Roots, Acquisition: store.AcquisitionIndexOnly, Listed: true})
	})
	mux.HandleFunc("/api/v1/manifests/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(built.ManifestPayload) })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 8 {
			http.NotFound(w, r)
			return
		}
		revision, ok := revisions[parts[len(parts)-4]+"\x00"+parts[len(parts)-3]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(revision)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	resolverCache := NewCache(t.TempDir())
	r := &Resolver{Cache: resolverCache}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			revision, ok := revisions["hook\x00"+tc.hookName]
			if !ok || revision.ContentDigest == "" {
				t.Fatalf("missing built revision for hook %q", tc.hookName)
			}
			pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "hook", Name: tc.hookName, Revision: string(revision.ContentDigest), Digest: string(revision.ContentDigest), Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: string(built.ManifestDigest), InstalledName: tc.installedName}
			cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
			online, resolveErrors := r.Resolve(context.Background(), cfg, []hcc.Target{tc.target}, resolver.MarketplaceResolveModeRefresh)
			if len(resolveErrors) != 0 || len(online) != 1 {
				t.Fatalf("online Resolve = %d artifacts / %v", len(online), resolveErrors)
			}
			res := online[0]
			hook, ok := res.Resource.(*ir.Hook)
			if !ok || hook.Name() != tc.hookName || hook.Source.File != tc.sourceFile || hook.SourceDefinitionDigest() != "" {
				t.Fatalf("resolved canonical hook = %#v", res.Resource)
			}
			if tc.installedName != "" {
				canonicalName := hook.Name()
				if res.Name != tc.installedName || res.InstalledName != tc.installedName {
					t.Fatalf("resolved installed names = entry %q / installed %q, want %q", res.Name, res.InstalledName, tc.installedName)
				}
				res.Name = "entry-factory"
				if res.Name == canonicalName || res.Name == res.InstalledName || res.InstalledName == canonicalName || res.Name != "entry-factory" || hook.Name() != canonicalName {
					t.Fatalf("Marketplace names = entry %q / installed %q / canonical %q", res.Name, res.InstalledName, hook.Name())
				}
				online[0] = res
			}
			if res.StagedSourceDir == "" || filepath.Base(res.Path) != filepath.Base(tc.sourceFile) {
				t.Fatalf("online staged source = %q / %q, want basename %q", res.StagedSourceDir, res.Path, filepath.Base(tc.sourceFile))
			}
			if info, err := os.Lstat(res.Path); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("online hook definition is not a staged regular file: %v / %v", info, err)
			}
			if _, err := os.Lstat(filepath.Join(res.StagedSourceDir, "SKILL.md")); !os.IsNotExist(err) {
				t.Fatalf("hook staging used a skill fallback: %v", err)
			}
			var peerBefore []byte
			if tc.peerFile != "" {
				peerBefore, err = os.ReadFile(filepath.Join(res.StagedSourceDir, filepath.Base(tc.peerFile)))
				if err != nil || !bytes.Contains(peerBefore, []byte(tc.peerMarker)) {
					t.Fatalf("staged peer aggregate = %q / %v", peerBefore, err)
				}
			}
			workspace := t.TempDir()
			materializeOpts := materialize.WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile})
			report, err := materialize.Run(context.Background(), workspace, online, materializeOpts)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("materialize online hook: %v / %v", err, report)
			}
			nativePath := filepath.Join(workspace, filepath.FromSlash(tc.nativeFile))
			before, err := os.ReadFile(nativePath)
			if err != nil || !bytes.Contains(before, []byte(tc.payload)) || bytes.Contains(before, []byte("_managed_by")) {
				t.Fatalf("native hook output %s = %q / %v", tc.nativeFile, before, err)
			}
			beforeInfo, err := os.Stat(nativePath)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.supportScript) > 0 {
				if !bytes.Contains(before, []byte(filepath.ToSlash(filepath.Join(tc.installedName, "scripts", "check.sh")))) {
					t.Fatalf("Native command does not reference the selected support name: %s", before)
				}
				matches, err := filepath.Glob(filepath.Join(workspace, ".factory", "hook-support", "*", tc.installedName, "scripts", "check.sh"))
				if err != nil || len(matches) != 1 {
					t.Fatalf("index-only support scripts = %v / %v, want one selected script", matches, err)
				}
				installed := matches[0]
				data, err := os.ReadFile(installed)
				if err != nil || !bytes.Equal(data, tc.supportScript) {
					t.Fatalf("index-only support bytes = %q / %v, want %q", data, err, tc.supportScript)
				}
				info, err := os.Stat(installed)
				if err != nil || info.Mode().Perm() != tc.supportMode {
					t.Fatalf("index-only support mode = %v / %v, want %v", info, err, tc.supportMode)
				}
				nestedScript, err := filepath.Glob(filepath.Join(workspace, ".factory", "hook-support", "*", tc.installedName, "bundle", "scripts", "nested.sh"))
				if err != nil || len(nestedScript) != 1 {
					t.Fatalf("nested source support file = %v / %v, want one relative-path-preserved file", nestedScript, err)
				}
				nestedBytes, err := os.ReadFile(nestedScript[0])
				if err != nil || string(nestedBytes) != "echo nested payload\n" {
					t.Fatalf("nested source support bytes = %q / %v", nestedBytes, err)
				}
				for _, excluded := range []string{"verified.marker", filepath.Join("sibling", "other.sh"), "mcp.json"} {
					matches, err := filepath.Glob(filepath.Join(workspace, ".factory", "hook-support", "*", tc.installedName, filepath.FromSlash(excluded)))
					if err != nil || len(matches) != 0 {
						t.Fatalf("excluded source file %s was installed: %v / %v", excluded, matches, err)
					}
				}
			}
			if tc.peerFile != "" {
				if bytes.Contains(before, []byte(tc.peerMarker)) {
					t.Fatalf("unselected peer resource leaked into native hook output: %s", before)
				}
				var installedPeers []string
				if err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, walkErr error) error {
					if walkErr == nil && entry.Name() == filepath.Base(tc.peerFile) {
						installedPeers = append(installedPeers, path)
					}
					return walkErr
				}); err != nil {
					t.Fatal(err)
				}
				if len(installedPeers) != 0 || peerBefore == nil {
					t.Fatalf("peer aggregate control: staged=%q installed=%v", peerBefore, installedPeers)
				}
				store, err := ownership.Open(filepath.Dir(nativePath))
				if err != nil {
					t.Fatal(err)
				}
				proof, err := store.Proof(filepath.Base(nativePath))
				if err != nil {
					t.Fatal(err)
				}
				var sourceDigest, resourceName string
				for _, id := range proof.OwnedIDs() {
					if id.Kind == ownership.KindHook {
						entry, ok := proof.Entry(id)
						if ok {
							sourceDigest = entry.SourceDigest
							resourceName = entry.Resource
						}
					}
				}
				if sourceDigest == "" {
					t.Fatal("staged peer source did not produce captured ownership evidence")
				}
				if tc.installedName != "" && resourceName != tc.installedName {
					t.Fatalf("owned resource name = %q, want installed name %q", resourceName, tc.installedName)
				}
			}
			requestCount := requests.Load()
			offline, offlineErrors := r.Resolve(context.Background(), cfg, []hcc.Target{tc.target}, resolver.MarketplaceResolveModeCachedNoFetch)
			if len(offlineErrors) != 0 || len(offline) != 1 || requests.Load() != requestCount {
				t.Fatalf("offline Resolve = %d artifacts / %v; requests %d -> %d", len(offline), offlineErrors, requestCount, requests.Load())
			}
			if filepath.Base(offline[0].Path) != filepath.Base(tc.sourceFile) {
				t.Fatalf("offline hook path = %q, want basename %q", offline[0].Path, filepath.Base(tc.sourceFile))
			}
			if info, err := os.Lstat(offline[0].Path); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("offline hook definition is not a staged regular file: %v / %v", info, err)
			}
			report, err = materialize.Run(context.Background(), workspace, offline, materializeOpts)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("materialize offline hook: %v / %v", err, report)
			}
			after, err := os.ReadFile(nativePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("offline native output changed: %q -> %q / %v", before, after, err)
			}
			afterInfo, err := os.Stat(nativePath)
			if err != nil || beforeInfo.Mode() != afterInfo.Mode() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatalf("offline native file generation changed: before=%v after=%v err=%v", beforeInfo, afterInfo, err)
			}
		})
	}
}

func TestIndexOnlyResolverRejectsPostReingestionManifestMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\nlicense: MIT\n---\n"+body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("same body")
	if err := os.MkdirAll(filepath.Join(skillDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "docs", "support.txt"), []byte("first support\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "--initial-branch=main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"add", "."}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commitOutput, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	firstCommit := strings.TrimSpace(string(commitOutput))
	registry := store.Registry{ID: "src", Slug: "source", Ref: "main", Origins: []string{"file://" + repo}, Acquisition: store.AcquisitionIndexOnly, License: "MIT", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	old, err := ingest.BuildRegistryRevision(context.Background(), marketplaceconfig.Config{Policy: marketplaceconfig.Policy{HiddenContent: "warn", Limits: marketplaceconfig.Limits{IngestionDuration: marketplaceconfig.Duration(2 * time.Minute)}}}, "mp", registry, repo, firstCommit)
	if err != nil {
		t.Fatalf("build first revision: %v", err)
	}
	var oldResource store.AssessedResourceRevision
	for _, candidate := range old.Resources {
		if candidate.Coordinate.Kind == "skill" && candidate.Coordinate.Name == "review" {
			oldResource = assessedRevision(candidate)
			break
		}
	}
	writeSkill("same body")
	if err := os.WriteFile(filepath.Join(skillDir, "docs", "support.txt"), []byte("second support\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add second: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-q", "-m", "second")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit second: %v\n%s", err, out)
	}
	commitOutput, err = exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	secondCommit := strings.TrimSpace(string(commitOutput))

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/manifests/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(old.ManifestPayload) })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(assessedRevisionOccurrence(oldResource.ResourceRevision, secondCommit, string(old.ManifestDigest)))
	})
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/occurrences/") {
			_ = json.NewEncoder(w).Encode(api.ExactRegistryOccurrence{RegistryID: "src", Ref: "main", Roots: registry.Roots, Commit: secondCommit, RegistryRevisionManifestDigest: old.ManifestDigest, Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: "file://" + repo, OccurredAt: time.Now().UTC()})
			return
		}
		_ = json.NewEncoder(w).Encode(api.PublicRegistry{ID: "src", Slug: "source", Origins: []string{"file://" + repo}, Ref: "main", Roots: registry.Roots, Acquisition: store.AcquisitionIndexOnly, Listed: true})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: string(oldResource.ContentDigest), Digest: string(oldResource.ContentDigest), Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: secondCommit, RegistryManifestDigest: string(old.ManifestDigest)}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	_, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "attested registry revision manifest") {
		t.Fatalf("post-reingestion mismatch errors = %v", errs)
	}
}

func TestIndexOnlyResolverRejectsDisallowedOriginBeforeGit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	content := "sha256:" + strings.Repeat("c", 64)
	revision := assessedRevisionOccurrence(store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}, ContentDigest: store.RevisionDigest(content)}, commit, manifest)
	var registryRequests int
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		registryRequests++
		_ = json.NewEncoder(w).Encode(api.ExactRegistryOccurrence{RegistryID: "src", Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}, Commit: commit, RegistryRevisionManifestDigest: store.RevisionDigest(manifest), Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: "https://evil.example/repo.git", OccurredAt: time.Now().UTC()})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	allowed := []string{`trusted\\.example/`}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: content, Digest: content, Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifest}
	cfg := &config.Config{AllowedRegistries: &allowed, Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	cache := NewCache(t.TempDir())
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: commit, ManifestDigest: manifest}
	if err := cache.MarkIndexOnlyArtifactVerified(identity, pin.Digest); err != nil {
		t.Fatal(err)
	}
	_, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "allowed_registries") {
		t.Fatalf("disallowed origin errors = %v", errs)
	}
	if registryRequests != 1 {
		t.Fatalf("registry occurrence requests = %d, want 1 and no Git access", registryRequests)
	}
	markerPath := filepath.Join(filepath.Dir(cache.IndexOnlyArtifactPath(identity, pin.Digest)), "verified.marker")
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("refresh failure left occurrence marker present: %v", err)
	}
}

func TestIndexOnlyResolverRejectsBareLocalOriginBeforeGit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	content := "sha256:" + strings.Repeat("c", 64)
	origin := filepath.Join(t.TempDir(), "not-a-git-origin")
	marker := filepath.Join(t.TempDir(), "git-ran")
	bin := t.TempDir()
	git := filepath.Join(bin, "git")
	if err := os.WriteFile(git, []byte("#!/bin/sh\nprintf x > "+marker+"\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	revision := assessedRevisionOccurrence(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(content),
	}, commit, manifest)
	occurrenceRequests, registryRequests := 0, 0
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) { writeTestWellKnown(w, "mp") })
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(revision) })
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/occurrences/") {
			occurrenceRequests++
			_ = json.NewEncoder(w).Encode(api.ExactRegistryOccurrence{RegistryID: "src", Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}, Commit: commit, RegistryRevisionManifestDigest: store.RevisionDigest(manifest), Acquisition: store.AcquisitionIndexOnly, AcquisitionOrigin: origin, OccurredAt: time.Now().UTC()})
			return
		}
		registryRequests++
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: content, Digest: content, Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifest}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	_, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "bare relative or absolute paths") {
		t.Fatalf("unsafe local origin errors = %v", errs)
	}
	if occurrenceRequests != 1 || registryRequests != 0 {
		t.Fatalf("registry requests = occurrence %d, metadata %d; origin should fail before source acquisition", occurrenceRequests, registryRequests)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Git was invoked for unsafe local origin; marker stat = %v", err)
	}
}

func TestReconstructedArtifactIdentityIsAnalyzerIndependent(t *testing.T) {
	pin := config.MarketplaceResourcePin{RegistryID: "requested", Kind: "skill", Name: "review", Digest: "sha256:" + strings.Repeat("a", 64)}
	artifact := hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{RegistryID: "other", Kind: "skill", Name: "review", ContentDigest: pin.Digest}}
	if err := validateReconstructedArtifactIdentity(pin, artifact); err != nil {
		t.Fatalf("analyzer-independent identity validation failed: %v", err)
	}
}

func TestStageReconstructedResourceRejectsSymlinkedSupportParent(t *testing.T) {
	checkout := t.TempDir()
	skillDir := filepath.Join(checkout, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillDir, "docs")); err != nil {
		t.Fatal(err)
	}

	registry := store.Registry{Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	skill := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{})
	_, err := stageReconstructedResource(NewCache(t.TempDir()), IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: strings.Repeat("a", 40), ManifestDigest: "sha256:" + strings.Repeat("b", 64)}, checkout, registry, skill, []string{"docs/secret.txt"})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("stage symlinked support parent error = %v, want symlink rejection", err)
	}
}

func TestCachedIndexOnlyFailuresInvalidateOccurrenceMarker(t *testing.T) {
	resource := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{})
	canonical, err := hccartifact.FromIR(resource)
	if err != nil {
		t.Fatal(err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	canonical.Manifest.RegistryID = "src"
	canonical.Manifest.Revision = contentDigest
	validRaw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	identity := IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: commit, ManifestDigest: manifest}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: contentDigest, Digest: contentDigest, Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifest}

	cases := []struct {
		name       string
		artifact   func() []byte
		stageFault bool
	}{
		{name: "decode", artifact: func() []byte { return []byte("not an archive") }},
		{name: "content", artifact: func() []byte {
			bad := canonical
			bad.Manifest.ContentDigest = "sha256:" + strings.Repeat("c", 64)
			raw, _, _ := hccartifact.Encode(bad)
			return raw
		}},
		{name: "payload", artifact: func() []byte {
			bad := canonical
			bad.Payload = []byte(`{"tampered":true}`)
			raw, _, _ := hccartifact.Encode(bad)
			return raw
		}},
		{name: "staging", artifact: func() []byte { return validRaw }, stageFault: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := NewCache(t.TempDir())
			if _, err := cache.WriteIndexOnlyResourceRevision(identity, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, assessedRevisionOccurrence(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(contentDigest), ArchiveDigest: store.ArchiveDigest(archiveDigest),
			}, commit, manifest)); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.WriteIndexOnlyArtifact(identity, pin.Digest, tc.artifact()); err != nil {
				t.Fatal(err)
			}
			if err := cache.MarkIndexOnlyArtifactVerified(identity, pin.Digest); err != nil {
				t.Fatal(err)
			}
			stagedDir := cachedReconstructedResourcePath(cache, identity, pin.Kind, pin.Name)
			if tc.stageFault {
				outside := t.TempDir()
				if err := os.Symlink(outside, filepath.Join(cache.root, "compiled")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(stagedDir, 0o755); err != nil {
				t.Fatal(err)
			} else if err := os.WriteFile(filepath.Join(stagedDir, "SKILL.md"), []byte("cached"), 0o644); err != nil {
				t.Fatal(err)
			}

			shared := NewCache(cache.root)
			if _, err := shared.WriteArtifact(archiveDigest, validRaw); err != nil {
				t.Fatal(err)
			}
			if err := shared.MarkArtifactVerified(archiveDigest); err != nil {
				t.Fatal(err)
			}
			if _, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://market.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeCachedNoFetch); len(errs) == 0 {
				t.Fatal("cached invalid index-only artifact unexpectedly resolved")
			}
			marker := filepath.Join(filepath.Dir(cache.IndexOnlyArtifactPath(identity, pin.Digest)), "verified.marker")
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("occurrence marker still present after %s failure: %v", tc.name, err)
			}
			if _, verified, err := cache.ReadArtifact(archiveDigest); err != nil || !verified {
				t.Fatalf("shared mirror marker changed after %s failure: verified=%t err=%v", tc.name, verified, err)
			}
		})
	}
}

func tamperCanonicalArchiveDigest(t *testing.T, raw []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(writer)
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == hccartifact.CanonicalManifestPath {
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest["archive_digest"] = "sha256:" + strings.Repeat("d", 64)
			data, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			header.Size = int64(len(data))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestStageReconstructedResourceRejectsSymlinkedCacheAncestor(t *testing.T) {
	checkout := t.TempDir()
	skillDir := filepath.Join(checkout, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheRoot := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cacheRoot, "compiled")); err != nil {
		t.Fatal(err)
	}
	registry := store.Registry{Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}}}
	skill := ir.NewSkill("review", "Review", "body", nil, nil, ir.SourceRef{})
	_, err := stageReconstructedResource(NewCache(cacheRoot), IndexOnlyIdentity{Marketplace: "mp", RegistryID: "src", Commit: strings.Repeat("a", 40), ManifestDigest: "sha256:" + strings.Repeat("b", 64)}, checkout, registry, skill, nil)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("stage symlinked cache ancestor error = %v, want symlink rejection", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("staging wrote outside cache root: %v", entries)
	}
}

func revisionTime() time.Time { return time.Now().UTC() }
