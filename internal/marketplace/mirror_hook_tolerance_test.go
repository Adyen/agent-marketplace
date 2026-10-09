package marketplace

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
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
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

// mirrorHookNativeProof writes the Factory native hook file plus the durable
// ownership record the future native writer leaves for it: the hook/v2
// identity over the native event and canonical rule bytes, the ownership
// resource name, and the supplied source-hook fingerprint.
func mirrorHookNativeProof(t *testing.T, repoRoot, rule, resourceName, sourceDigest string) string {
	t.Helper()
	var value any
	decoder := json.NewDecoder(strings.NewReader(rule))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	valueDigest := ownership.Digest(canonical)

	nativeRoot := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"PreToolUse":[` + rule + `]}` + "\n"
	st, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply(ownership.Mutation{
		Rel:       "hooks.json",
		AbsFile:   filepath.Join(nativeRoot, "hooks.json"),
		NextBytes: []byte(body),
		Entries: []ownership.Entry{{
			Kind:           ownership.KindHook,
			Container:      "PreToolUse",
			IdentityDigest: ownership.HookIdentityDigest("PreToolUse", valueDigest),
			ValueDigest:    valueDigest,
			Resource:       resourceName,
			SourceDigest:   sourceDigest,
		}},
		Reason: ownership.ReasonSync,
	}); err != nil {
		t.Fatal(err)
	}
	return body
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

// A hook selected through REAL verified Marketplace mirror resolution carries
// no original source tree: Path is the verified artifact archive. Its
// domain-separated mirror fingerprint still satisfies an ownership-proven
// native entry, so the legacy-writing client does not recreate legacy output;
// proof recorded for a different revision does not.
// spec: hook-native-location-tolerance/verified-mirror-revision-uses-its-own-fingerprint
// spec: hook-native-location-tolerance/unverified-or-changed-mirror-revision-never-satisfies
func TestMirrorHookNativeProofSuppressesLegacyOutput(t *testing.T) {
	isolateHarnessState(t)
	cache := NewCache(t.TempDir())

	hook := ir.NewHookWithMatcher("deploy-hook", "PreToolUse", "Bash", "./run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	canonical, err := hccartifact.FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	server, contentDigest := serveCanonicalMirrorHook(t, canonical, "deploy-hook")
	cfg := &config.Config{
		Marketplaces: []config.MarketplaceEntry{
			{
				URL:        server.URL,
				InstanceID: "mp",
				Resources: []config.MarketplaceResourcePin{
					{
						RegistryID:  "src",
						Kind:        "hook",
						Name:        "deploy-hook",
						Revision:    "v1",
						Digest:      contentDigest,
						Acquisition: config.MarketplaceAcquisitionMirror,
					},
				},
			},
		},
	}
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

	nativeRule := `{"hooks":[{"command":".factory/am-hooks/deploy-hook/run.sh","type":"command"}],"matcher":"Bash"}`
	legacyFile := func(repoRoot string) string {
		return filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
	}

	// Exact revision: clean native proof suppresses the legacy write.
	t.Run("exact_revision", func(t *testing.T) {
		repoRoot := t.TempDir()
		nativeBody := mirrorHookNativeProof(t, repoRoot, nativeRule, res.Name,
			mirrorFingerprintFor(t, res, "factory", materialize.HookScopeRepository))

		if _, err := materialize.Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{res}); err != nil {
			t.Fatalf("materialize.Run: %v", err)
		}
		if data, err := os.ReadFile(legacyFile(repoRoot)); err == nil {
			t.Fatalf("mirror hook legacy output recreated: %s", data)
		}
		nativeAfter, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "hooks.json"))
		if err != nil || string(nativeAfter) != nativeBody {
			t.Fatalf("native file changed: %v", err)
		}
	})

	// Proof recorded for a different verified content digest is proof of a
	// different revision: legacy output proceeds untouched.
	t.Run("changed_digest", func(t *testing.T) {
		repoRoot := t.TempDir()
		other := res
		other.MarketplaceDigest = "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
		mirrorHookNativeProof(t, repoRoot, nativeRule, res.Name,
			mirrorFingerprintFor(t, other, "factory", materialize.HookScopeRepository))

		if _, err := materialize.Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{res}); err != nil {
			t.Fatalf("materialize.Run: %v", err)
		}
		data, err := os.ReadFile(legacyFile(repoRoot))
		if err != nil || !strings.Contains(string(data), "run.sh") {
			t.Fatalf("changed mirror digest suppressed legacy output: %v %q", err, data)
		}
	})
}
