package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// serveCanonicalMirrorHook publishes one canonical HOOK artifact over the
// Marketplace API the way serveCanonicalMirror does for skills.
func serveCanonicalMirrorHook(t *testing.T, canonical hccartifact.CanonicalArtifact, hookName string) (*httptest.Server, string) {
	t.Helper()
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return serveCanonicalMirrorHookArchive(t, canonical, hookName, raw, archiveDigest)
}

func serveCanonicalMirrorHookArchive(t *testing.T, canonical hccartifact.CanonicalArtifact, hookName string, raw []byte, archiveDigest string) (*httptest.Server, string) {
	t.Helper()
	rev := assessedRevision(store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: store.MarketplaceID("mp"),
			Registry:    store.RegistryID("src"),
			Kind:        "hook",
			Name:        hookName,
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

func mirrorHookConfig(url, name, contentDigest string) *config.Config {
	return &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL:        url,
		InstanceID: "mp",
		Resources: []config.MarketplaceResourcePin{{
			RegistryID: "src", Kind: "hook", Name: name, Revision: "v1", Digest: contentDigest,
			Acquisition: config.MarketplaceAcquisitionMirror,
		}},
	}}}
}

// mirrorFingerprintFor computes the hook-source/mirror/v1 fingerprint the
// future native writer records for this exact resolved mirror artifact.
func mirrorFingerprintFor(t *testing.T, res resolver.ResolvedArtifact, target, scope string) string {
	t.Helper()
	digest, err := materialize.HookSourceMirrorFingerprint(materialize.HookSourceMirrorFingerprintInput{
		InstanceID:    res.MarketplaceInstanceID,
		RegistryID:    res.MarketplaceRegistryID,
		Kind:          res.Resource.Kind().String(),
		ResourceName:  res.MarketplaceResourceName,
		Digest:        res.MarketplaceDigest,
		InstalledName: res.Name,
		Target:        target,
		Scope:         scope,
	})
	if err != nil {
		t.Fatalf("mirror fingerprint: %v", err)
	}
	return digest
}

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
// spec: hook-resources/repeat-sync-is-byte-identical
// spec: hook-native-location-tolerance/verified-mirror-revision-uses-its-own-fingerprint
func TestMirrorHookNativeProofSuppressesLegacyOutput(t *testing.T) {
	isolateHarnessState(t)
	cache := NewCache(t.TempDir())

	hook := ir.NewHookWithMatcher("deploy-hook", "PreToolUse", "Bash", "./run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	canonical, err := hccartifact.FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	server, contentDigest := serveCanonicalMirrorHook(t, canonical, "deploy-hook")
	cfg := mirrorHookConfig(server.URL, "deploy-hook", contentDigest)
	r := &Resolver{Cache: cache}
	resolved, errs := r.Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) > 0 {
		t.Fatalf("Resolve errors: %v", errs)
	}
	if len(resolved) != 1 || resolved[0].Resource == nil {
		t.Fatalf("resolved artifacts = %#v, want one materializable hook", resolved)
	}
	res := resolved[0]
	if res.MarketplaceAcquisition != config.MarketplaceAcquisitionMirror {
		t.Fatalf("acquisition = %q, want %q", res.MarketplaceAcquisition, config.MarketplaceAcquisitionMirror)
	}
	if res.StagedSourceDir != "" || filepath.Base(res.Path) != "artifact.tar.gz" {
		t.Fatalf("mirror hook must carry no staged source tree: StagedSourceDir=%q Path=%q", res.StagedSourceDir, res.Path)
	}
	legacyFile := filepath.Join(".factory", "hooks", "hooks.json")
	nativeFile := filepath.Join(".factory", "hooks.json")
	expectedDigest := mirrorFingerprintFor(t, res, "factory", materialize.HookScopeRepository)

	assertNativeRun := func(t *testing.T, repoRoot string, selected resolver.ResolvedArtifact) ([]byte, os.FileInfo) {
		t.Helper()
		report, err := materialize.Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{selected})
		if err != nil || report == nil || diag.HasErrors(report.Diags) {
			t.Fatalf("materialize.Run: %v / %v", err, report)
		}
		nativePath := filepath.Join(repoRoot, nativeFile)
		data, err := os.ReadFile(nativePath)
		if err != nil || !strings.Contains(string(data), "run.sh") || strings.Contains(string(data), "_managed_by") {
			t.Fatalf("native mirror hook output = %q / %v", data, err)
		}
		if _, err := os.Lstat(filepath.Join(repoRoot, legacyFile)); !os.IsNotExist(err) {
			t.Fatalf("mirror hook legacy output was written: %v", err)
		}
		info, err := os.Stat(nativePath)
		if err != nil {
			t.Fatal(err)
		}
		return data, info
	}
	assertSourceProof := func(t *testing.T, repoRoot string, selected resolver.ResolvedArtifact, wantDigest string) {
		t.Helper()
		store, err := ownership.Open(filepath.Join(repoRoot, ".factory"))
		if err != nil {
			t.Fatal(err)
		}
		proof, err := store.Proof("hooks.json")
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range proof.OwnedIDs() {
			if id.Kind != ownership.KindHook {
				continue
			}
			entry, ok := proof.Entry(id)
			if ok && entry.Resource == selected.Name && entry.SourceDigest == wantDigest && proof.HasHookSource(id, selected.Name, wantDigest) {
				return
			}
		}
		t.Fatalf("native mirror source proof for %q/%q was not recorded: %+v", selected.Name, wantDigest, proof.OwnedIDs())
	}

	t.Run("exact_revision", func(t *testing.T) {
		repoRoot := t.TempDir()
		before, beforeInfo := assertNativeRun(t, repoRoot, res)
		assertSourceProof(t, repoRoot, res, expectedDigest)
		after, afterInfo := assertNativeRun(t, repoRoot, res)
		if !bytes.Equal(before, after) || beforeInfo.Mode() != afterInfo.Mode() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
			t.Fatal("repeated exact mirror sync rewrote the native aggregate")
		}
	})

	t.Run("changed_digest_updates_native_proof_without_legacy_output", func(t *testing.T) {
		repoRoot := t.TempDir()
		before, beforeInfo := assertNativeRun(t, repoRoot, res)
		assertSourceProof(t, repoRoot, res, expectedDigest)
		other := res
		other.MarketplaceDigest = "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
		otherDigest := mirrorFingerprintFor(t, other, "factory", materialize.HookScopeRepository)
		after, afterInfo := assertNativeRun(t, repoRoot, other)
		if !bytes.Equal(before, after) || beforeInfo.Mode() != afterInfo.Mode() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
			t.Fatal("source-proof-only mirror update rewrote the native aggregate")
		}
		assertSourceProof(t, repoRoot, other, otherDigest)
	})
}

func TestMirrorHookResolverCarriesSupportToRun(t *testing.T) {
	isolateHarnessState(t)
	cache := NewCache(t.TempDir())
	script := []byte{0x00, 0x23, 0xff, 0x0a}
	payload := []byte{0x7f, 0x00, 0x55}
	hook := ir.NewHookWithMatcher("deploy-hook", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	canonical, err := hccartifact.FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Support = map[string]hccartifact.SupportFile{
		"scripts/check.sh": {Data: script, Executable: true},
		"data/payload.bin": {Data: payload},
	}
	server, contentDigest := serveCanonicalMirrorHook(t, canonical, "deploy-hook")
	resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), mirrorHookConfig(server.URL, "deploy-hook", contentDigest), []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 0 || len(resolved) != 1 {
		t.Fatalf("Resolve: artifacts=%#v errors=%v", resolved, errs)
	}
	res := resolved[0]
	wantSupport := []struct {
		path string
		data []byte
		mode os.FileMode
	}{{path: "data/payload.bin", data: payload, mode: 0o644}, {path: "scripts/check.sh", data: script, mode: 0o755}}
	if len(res.MarketplaceHookSupport) != len(wantSupport) {
		t.Fatalf("MarketplaceHookSupport has %d records, want %d", len(res.MarketplaceHookSupport), len(wantSupport))
	}
	for i, want := range wantSupport {
		got := res.MarketplaceHookSupport[i]
		if got.Path != want.path || string(got.Bytes) != string(want.data) || got.Mode.Perm() != want.mode {
			t.Fatalf("MarketplaceHookSupport[%d] = {%q %v %v}, want {%q %v %v}", i, got.Path, got.Bytes, got.Mode.Perm(), want.path, want.data, want.mode)
		}
	}
	if filepath.Base(res.Path) != "artifact.tar.gz" || res.StagedSourceDir != "" {
		t.Fatalf("mirror source path changed: Path=%q StagedSourceDir=%q", res.Path, res.StagedSourceDir)
	}
	archivePath := res.Path
	if err := os.WriteFile(archivePath, []byte("changed after verified resolution"), 0o600); err != nil {
		t.Fatalf("mutate cached archive after Resolve: %v", err)
	}
	if res.Path != archivePath {
		t.Fatalf("mirror archive path changed after Resolve: %q, want %q", res.Path, archivePath)
	}

	repoRoot := t.TempDir()
	report, err := materialize.Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{res})
	if err != nil || report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("Run: %v report=%+v", err, report)
	}
	for rel, want := range map[string]struct {
		bytes []byte
		mode  os.FileMode
	}{
		"scripts/check.sh": {bytes: script, mode: 0o755},
		"data/payload.bin": {bytes: payload, mode: 0o644},
	} {
		matches, err := filepath.Glob(filepath.Join(repoRoot, ".factory", "hook-support", "*", "deploy-hook", filepath.FromSlash(rel)))
		if err != nil || len(matches) != 1 {
			t.Fatalf("support file %s matches=%v err=%v", rel, matches, err)
		}
		data, err := os.ReadFile(matches[0])
		if err != nil || string(data) != string(want.bytes) {
			t.Fatalf("support file %s bytes=%v err=%v, want %v", rel, data, err, want.bytes)
		}
		info, err := os.Stat(matches[0])
		if err != nil || info.Mode().Perm() != want.mode {
			t.Fatalf("support file %s mode=%v err=%v, want %v", rel, info.Mode().Perm(), err, want.mode)
		}
	}
}

func TestMirrorHookResolverSupportlessControl(t *testing.T) {
	isolateHarnessState(t)
	canonical, err := hccartifact.FromIR(ir.NewHookWithMatcher("plain-hook", "PreToolUse", "Bash", "echo plain", "command", nil, ir.SourceRef{File: "hooks/hooks.json"}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	server, contentDigest := serveCanonicalMirrorHook(t, canonical, "plain-hook")
	resolved, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), mirrorHookConfig(server.URL, "plain-hook", contentDigest), []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) != 0 || len(resolved) != 1 {
		t.Fatalf("Resolve: artifacts=%#v errors=%v", resolved, errs)
	}
	repoRoot := t.TempDir()
	report, err := materialize.Run(context.Background(), repoRoot, resolved)
	if err != nil || report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("Run: %v report=%+v", err, report)
	}
	matches, err := filepath.Glob(filepath.Join(repoRoot, ".factory", "hook-support", "*", "*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("support-less mirror installed files: %v err=%v", matches, err)
	}
}

func TestMirrorHookInvalidArchiveDoesNotLeakSupport(t *testing.T) {
	isolateHarnessState(t)
	canonical, err := hccartifact.FromIR(ir.NewHook("broken-hook", "SessionStart", "echo plain", "command", nil, ir.SourceRef{File: "hooks/hooks.json"}))
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	canonical.Support = map[string]hccartifact.SupportFile{"secret.bin": {Data: []byte{0x00, 0xff}}}
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)/2] ^= 0x01
	server, contentDigest := serveCanonicalMirrorHookArchive(t, canonical, "broken-hook", tampered, archiveDigest)
	resolved, errs := (&Resolver{Cache: NewCache(t.TempDir())}).Resolve(context.Background(), mirrorHookConfig(server.URL, "broken-hook", contentDigest), []hcc.Target{hcc.TargetFactory}, resolver.MarketplaceResolveModeRefresh)
	if len(errs) == 0 || len(resolved) != 0 {
		t.Fatalf("invalid archive resolution leaked artifacts=%#v errors=%v", resolved, errs)
	}
}
