package main

import (
	"bytes"
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
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// seedNativeHookProof writes the target's documented native hook file and the
// durable ownership record exactly as the future native writer (#403) leaves
// them: each entry's hook/v2 identity over the native event and canonical
// rule bytes, its ownership resource name, and the version-1 source-hook
// fingerprint.
func seedNativeHookProof(t *testing.T, nativeRoot, rel, body string, entries ...ownership.Entry) {
	t.Helper()
	abs := filepath.Join(nativeRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	var priorDigest string
	if current, err := ownership.ReadAggregateFile(abs); err == nil {
		priorDigest = ownership.Digest(current)
	}
	if _, err := store.Apply(ownership.Mutation{
		Rel:               rel,
		AbsFile:           abs,
		NextBytes:         []byte(body),
		ExpectPriorDigest: priorDigest,
		Entries:           entries,
		Reason:            ownership.ReasonSync,
	}); err != nil {
		t.Fatal(err)
	}
}

// nativeHookProofEntry builds the durable record the future native writer
// stores for one emitted native rule.
func nativeHookProofEntry(t *testing.T, event, rule, resource, sourceDigest string) ownership.Entry {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader([]byte(rule)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	valueDigest := ownership.Digest(canonical)
	return ownership.Entry{
		Kind:           ownership.KindHook,
		Container:      event,
		IdentityDigest: ownership.HookIdentityDigest(event, valueDigest),
		ValueDigest:    valueDigest,
		Resource:       resource,
		SourceDigest:   sourceDigest,
	}
}

// After a simulated #403 migration — the selected hook already present in the
// target's documented native file with clean ownership proof and a matching
// version-1 source fingerprint — the legacy-writing candidate must satisfy
// that hook instead of recreating its legacy file, under sync, sync --locked,
// and sync --check. The committed lock bytes must not change.
// spec: hook-native-location-tolerance/native-owned-entry-prevents-duplicate-legacy-output
// spec: hook-native-location-tolerance/read-only-check-cannot-mutate-ownership-state
func TestSyncToleratesOwnershipProvenNativeHook(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Git registry authored in the documented Factory source layout; the
	// working clone holds the same bytes the resolved snapshot does, so the
	// fingerprint can be computed over it.
	origin, working := createBareRepoWithWorkingClone(t, "hooks.git")
	writeFile(t, filepath.Join(working, ".factory", "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}]}]}`)
	writeFile(t, filepath.Join(working, ".factory", "hooks", "run.sh"), "#!/bin/sh\necho deploy\n")
	commitAndPushAll(t, working, "Add hook")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hooks:
    git: file://`+origin+`
    ref: main
resources:
  hooks: {all: true}
`)
	withWorkingDir(t, repoRoot)

	// Resolve once to learn the installed hook name, then simulate the future
	// native writer for that exact selection. Git registries resolve through
	// the committed lock, so create it first.
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("lock: %v\n%s", err, output)
	}
	output, err := executeCommand("list", "--kind", "hook")
	if err != nil {
		t.Fatalf("list hooks: %v\n%s", err, output)
	}
	hookName := ""
	_, rows := parseTable(t, output)
	for _, row := range rows {
		fields := strings.Split(row, "|")
		if len(fields) > 2 && fields[1] == "hook" {
			hookName = strings.TrimSpace(fields[2])
			break
		}
	}
	if hookName == "" {
		t.Fatalf("no hook resolved from registry:\n%s", output)
	}

	fingerprint, err := materialize.HookSourceFingerprint(materialize.HookSourceFingerprintInput{
		SourceIdentity: "git:" + config.NormalizeGitURL(origin) + "@main:",
		Name:           hookName,
		Target:         "factory",
		Scope:          materialize.HookScopeRepository,
		SourceRoot:     working,
		DefinitionRel:  ".factory/hooks/hooks.json",
	})
	if err != nil {
		t.Fatalf("source fingerprint: %v", err)
	}
	t.Logf("seeded fingerprint=%q", fingerprint)

	// The future native writer's Factory output: the top-level event map with
	// its rewritten support command path — byte-distinct from anything the
	// legacy fragment compiler emits.
	nativeRule := `{"hooks":[{"command":"./run.sh","type":"command"}],"matcher":"Bash"}`
	nativeBody := `{"PreToolUse":[` + nativeRule + `]}` + "\n"
	nativeRoot := filepath.Join(repoRoot, ".factory")
	seedNativeHookProof(t, nativeRoot, "hooks.json", nativeBody,
		nativeHookProofEntry(t, "PreToolUse", nativeRule, hookName, fingerprint))
	nativePath := filepath.Join(nativeRoot, "hooks.json")
	legacyPath := filepath.Join(nativeRoot, "hooks", "hooks.json")
	nativeBefore, nativeInfo := statSnapshot(t, nativePath)
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	lockBefore, lockInfo := statSnapshot(t, lockPath)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	configBefore, configInfo := statSnapshot(t, configPath)

	// sync: no legacy re-creation, native bytes untouched.
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(legacyPath); err == nil {
		t.Fatalf("sync recreated the proven hook's legacy output: %s", data)
	}
	if data, err := os.ReadFile(nativePath); err != nil || string(data) != nativeBody {
		t.Fatalf("sync changed the native file: %v; actual=%s expected=%s", err, data, nativeBody)
	}
	assertStatUnchanged(t, "sync native", nativePath, nativeBefore, nativeInfo)
	assertStatUnchanged(t, "sync lock", lockPath, lockBefore, lockInfo)
	assertStatUnchanged(t, "sync config", configPath, configBefore, configInfo)

	// sync --locked: same satisfied decision, byte-identical lock.
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(legacyPath); err == nil {
		t.Fatalf("sync --locked recreated the legacy output: %s", data)
	}
	if data, err := os.ReadFile(nativePath); err != nil || string(data) != nativeBody {
		t.Fatalf("sync --locked changed the native file: %v; actual=%s expected=%s", err, data, nativeBody)
	}
	assertStatUnchanged(t, "sync --locked native", nativePath, nativeBefore, nativeInfo)
	assertStatUnchanged(t, "sync --locked config", configPath, configBefore, configInfo)
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(lockBefore, lockAfter) {
		t.Fatalf("sync --locked changed committed lock bytes: %v", err)
	}
	assertStatUnchanged(t, "sync --locked lock", lockPath, lockBefore, lockInfo)

	// sync --check: no pending legacy entry reported, nothing written.
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync --check: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(legacyPath); err == nil {
		t.Fatalf("sync --check wrote the legacy output: %s", data)
	}
	if data, err := os.ReadFile(nativePath); err != nil || string(data) != nativeBody {
		t.Fatalf("sync --check changed the native file: %v; actual=%s expected=%s", err, data, nativeBody)
	}
	assertStatUnchanged(t, "sync --check native", nativePath, nativeBefore, nativeInfo)
	assertStatUnchanged(t, "sync --check lock", lockPath, lockBefore, lockInfo)
	assertStatUnchanged(t, "sync --check config", configPath, configBefore, configInfo)
}

// The same tolerance through REAL verified Marketplace mirror resolution:
// the selected hook's original source files never exist on this client — only
// the verified artifact archive does — so its ownership-proven native entry
// carries the domain-separated hook-source/mirror/v1 fingerprint. Under sync
// and sync --check the legacy file must not be recreated; a proof recorded
// for a different revision keeps legacy output.
// spec: hook-native-location-tolerance/verified-mirror-revision-uses-its-own-fingerprint
// spec: hook-native-location-tolerance/unverified-or-changed-mirror-revision-never-satisfies
func TestSyncToleratesMarketplaceMirrorNativeHook(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	hook := ir.NewHookWithMatcher("deploy-hook", "PreToolUse", "Bash", "./run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	canonical, err := hccartifact.FromIR(hook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	// Serve the payload as encoders before #724 published it, with the null
	// Extra member, so the installed native output below stays the proven one.
	var fields map[string]any
	if err := json.Unmarshal(canonical.Payload, &fields); err != nil {
		t.Fatalf("payload: %v", err)
	}
	fields["Extra"] = nil
	if canonical.Payload, err = json.Marshal(fields); err != nil {
		t.Fatalf("payload: %v", err)
	}
	canonical.Manifest.ContentDigest = hccartifact.ContentDigest(canonical.Payload)
	raw, archiveDigest, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	contentDigest := canonical.Manifest.ContentDigest
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: store.MarketplaceID("mp"), Registry: "src", Kind: "hook", Name: "deploy-hook"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(archiveDigest),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "http://"+r.Host))
		case r.URL.Path == "/api/v1/registries/src":
			_ = json.NewEncoder(w).Encode(mpapi.PublicRegistry{ID: "src", Slug: "src", Origins: []string{"https://example.com/hooks.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		case strings.Contains(r.URL.Path, "/revisions/"+contentDigest):
			_ = json.NewEncoder(w).Encode(revision)
		case strings.HasPrefix(r.URL.Path, "/api/v1/artifacts/"):
			w.Header().Set("Content-Type", "application/vnd.agent-marketplace.artifact.v1.tar+gzip")
			_, _ = w.Write(raw)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  hosted: {marketplace: `+server.URL+`}
resources:
  hosted:
    include:
      - src/hook/deploy-hook:
          revision: `+contentDigest+`
`)
	withWorkingDir(t, repoRoot)
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	configBefore, configInfo := statSnapshot(t, configPath)
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("mirror fixture unexpectedly has a Git lock: %v", err)
	}

	fingerprint, err := materialize.HookSourceMirrorFingerprint(materialize.HookSourceMirrorFingerprintInput{
		InstanceID:    "mp",
		RegistryID:    "src",
		Kind:          "hook",
		ResourceName:  "deploy-hook",
		Digest:        contentDigest,
		InstalledName: "deploy-hook",
		Target:        "factory",
		Scope:         materialize.HookScopeRepository,
	})
	if err != nil {
		t.Fatalf("mirror fingerprint: %v", err)
	}

	nativeRule := `{"hooks":[{"Extra":null,"command":"./run.sh","type":"command"}],"matcher":"Execute"}`
	nativeBody := `{"PreToolUse":[` + nativeRule + `]}` + "\n"
	nativeRoot := filepath.Join(repoRoot, ".factory")
	seedNativeHookProof(t, nativeRoot, "hooks.json", nativeBody,
		nativeHookProofEntry(t, "PreToolUse", nativeRule, "deploy-hook", fingerprint))
	nativePath := filepath.Join(nativeRoot, "hooks.json")
	legacyPath := filepath.Join(nativeRoot, "hooks", "hooks.json")
	nativeBefore, nativeInfo := statSnapshot(t, nativePath)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(legacyPath); err == nil {
		t.Fatalf("sync recreated the proven mirror hook's legacy output: %s", data)
	}
	if data, err := os.ReadFile(nativePath); err != nil || string(data) != nativeBody {
		t.Fatalf("sync changed the native file: %v; actual=%s expected=%s", err, data, nativeBody)
	}
	assertStatUnchanged(t, "sync native", nativePath, nativeBefore, nativeInfo)
	assertStatUnchanged(t, "sync config", configPath, configBefore, configInfo)
	// A v4 Marketplace source keeps exact acquisition evidence in the layer
	// lock; the mirror fixture gains only that Marketplace record, never Git.
	lockDoc := readExactManifestLock(t, lockPath)
	if len(lockDoc.Registries) != 0 || len(lockDoc.Marketplaces) != 1 || lockDoc.Marketplaces[0].InstanceID != "mp" {
		t.Fatalf("sync lock = %+v, want only the mirror's Marketplace record", lockDoc)
	}
	lockBefore, lockInfo := statSnapshot(t, lockPath)

	// sync --check: no pending legacy entry reported, nothing written.
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync --check: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(legacyPath); err == nil {
		t.Fatalf("sync --check wrote the legacy output: %s", data)
	}
	if data, err := os.ReadFile(nativePath); err != nil || string(data) != nativeBody {
		t.Fatalf("sync --check changed the native file: %v; actual=%s expected=%s", err, data, nativeBody)
	}
	assertStatUnchanged(t, "sync --check native", nativePath, nativeBefore, nativeInfo)
	assertStatUnchanged(t, "sync --check config", configPath, configBefore, configInfo)
	assertStatUnchanged(t, "sync --check lock", lockPath, lockBefore, lockInfo)
}
