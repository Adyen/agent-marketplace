package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func writeToleranceHookSource(t testing.TB) (string, string) {
	t.Helper()
	src := t.TempDir()
	hooksDir := filepath.Join(src, "hooks")
	definitionPath := filepath.Join(hooksDir, "hooks.json")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definitionPath, []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "run.sh"), []byte("#!/bin/sh\necho deploy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return src, definitionPath
}

// toleranceHookFixture creates a source layout carrying one command hook
// (hooks/hooks.json plus its run.sh support script) and returns the resolved
// artifact selecting it for the given source layer and target. The source
// stands in for a local-directory registry, whose configured source identity
// is local:<path>.
func toleranceHookFixture(t *testing.T, source config.Source, target hcc.Target) (resolver.ResolvedArtifact, string) {
	t.Helper()
	src, definitionPath := writeToleranceHookSource(t)
	loaded, err := catalog.Load(src, "claude")
	if err != nil {
		t.Fatalf("Claude hook catalog: %v", err)
	}
	var entry catalog.Entry
	var hooks int
	for _, candidate := range loaded.Entries {
		if candidate.Kind == ir.KindHook {
			entry = candidate
			hooks++
		}
	}
	if hooks != 1 {
		t.Fatalf("Claude source hooks = %d, want one: %+v", hooks, loaded.Entries)
	}
	hook, ok := entry.Resource.(*ir.Hook)
	if !ok || hook.SourceDefinitionDigest() == "" || hook.Source.File != "hooks/hooks.json" {
		t.Fatalf("Claude source hook is not bound to its definition: %#v", entry.Resource)
	}
	entry.Name = "deploy"
	entry.Path = definitionPath
	return resolver.ResolvedArtifact{
		Entry:       entry,
		Source:      source,
		RegistryURL: src,
		CatalogKey:  src,
		V4Kind:      config.V4SourceDirectory,
		Harnesses:   []hcc.Target{target},
	}, src
}

func frozenLegacyRawHookFixture(t *testing.T, source config.Source, target hcc.Target) (resolver.ResolvedArtifact, string) {
	t.Helper()
	src, definitionPath := writeToleranceHookSource(t)
	hook := ir.NewHookWithMatcher("deploy", "PreToolUse", "Bash", "./run.sh", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	return resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Name: "deploy", Resource: hook, Path: definitionPath},
		Source:      source,
		RegistryURL: src,
		V4Kind:      config.V4SourceDirectory,
		Harnesses:   []hcc.Target{target},
	}, src
}

// toleranceFingerprint computes the version-1 source-hook fingerprint the
// future native writer records: the pinned local-directory source identity,
// the installed name, target, scope, and the real source bytes on disk.
func toleranceFingerprint(t *testing.T, src, target, scope string) string {
	t.Helper()
	digest, err := HookSourceFingerprint(HookSourceFingerprintInput{
		SourceIdentity: "local:" + filepath.Clean(src),
		Name:           "deploy",
		Target:         target,
		Scope:          scope,
		SourceRoot:     src,
		DefinitionRel:  "hooks/hooks.json",
	})
	if err != nil {
		t.Fatalf("source fingerprint: %v", err)
	}
	return digest
}

// marketplaceHookFixture stages the source tree the index-only reconstructed
// resource seam leaves for a selected Marketplace hook (a copy of the exact
// resource directory holding hooks.json beside its support script) and
// returns the resolved artifact the Marketplace resolver hands materialize.
// The pin's original bundle path deliberately does not resolve under the
// staged root: only the staged layout may be fingerprinted.
func marketplaceHookFixture(t *testing.T, target hcc.Target, digest string) (resolver.ResolvedArtifact, string) {
	t.Helper()
	staged := t.TempDir()
	def := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}]}]}}`
	bundle, err := hcc.IngestBundle(fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{Data: []byte(def)},
	}, hcc.HarnessClaude)
	if err != nil {
		t.Fatalf("Claude hook ingest: %v", err)
	}
	if diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("Claude hook ingest diagnostics: %#v", bundle.Diagnostics)
	}
	var sourceHook *ir.Hook
	var hookCount int
	for _, resource := range bundle.Resources {
		if hook, ok := resource.(*ir.Hook); ok {
			sourceHook = hook
			hookCount++
		}
	}
	if hookCount != 1 || sourceHook == nil {
		t.Fatalf("Claude hook resources = %d, want one: %#v", hookCount, bundle.Resources)
	}
	if sourceHook.Name() != "PreToolUse-command" || sourceHook.Source.File != "hooks/hooks.json" || sourceHook.HookSource != "" || sourceHook.Event != "PreToolUse" || sourceHook.Matcher != "Bash" || sourceHook.Type != "command" || sourceHook.Command != "./run.sh" {
		t.Fatalf("Claude source hook = %#v", sourceHook)
	}
	canonical, err := artifact.FromIR(sourceHook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	archive, _, err := artifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decodedArchive, _, err := artifact.Decode(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	resource, err := artifact.DecodeResource(decodedArchive.Manifest.Kind, decodedArchive.Payload)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	hook, ok := resource.(*ir.Hook)
	if !ok || hook.SourceDefinitionDigest() != "" || hook.Name() != "PreToolUse-command" || hook.Source.File != "hooks/hooks.json" {
		t.Fatalf("decoded Marketplace hook = %#v", resource)
	}
	if err := os.WriteFile(filepath.Join(staged, "hooks.json"), []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "run.sh"), []byte("#!/bin/sh\necho deploy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return resolver.ResolvedArtifact{
		Entry:                   catalog.Entry{Kind: ir.KindHook, Name: "deploy", Resource: hook, Path: filepath.Join(staged, "hooks.json")},
		Source:                  config.RepoCommitted,
		RegistryURL:             "https://marketplace.example",
		Harnesses:               []hcc.Target{target},
		IsMarketplace:           true,
		MarketplaceInstanceID:   "mp",
		MarketplaceRegistryID:   "src",
		MarketplaceResourceName: hook.Name(),
		MarketplaceDigest:       digest,
		MarketplaceAcquisition:  config.MarketplaceAcquisitionIndexOnly,
		InstalledName:           "deploy",
		StagedSourceDir:         staged,
	}, staged
}

// spec: hook-native-location-tolerance/source-and-support-changes-invalidate-the-fingerprint
func TestRunIndexOnlyHookDefinitionMismatchIsNotSupportAdmissionRefusal(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	resolved, staged := marketplaceHookFixture(t, hcc.TargetFactory, "sha256:"+strings.Repeat("a", 64))
	definitionPath := filepath.Join(staged, "hooks.json")
	changedDefinition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./changed.sh"}]}]}}`)
	if err := os.WriteFile(definitionPath, changedDefinition, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil || report == nil || !hasDiagCode(report.Diags, "materialize.compile.error") {
		t.Fatalf("index-only definition mismatch was not refused: %+v / %v", report, err)
	}
	if hasDiagCode(report.Diags, diag.CodeMaterializeHookSupportRefused) {
		t.Fatalf("index-only definition-binding refusal was mislabeled as source admission: %+v", report.Diags)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".factory", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("index-only definition-binding refusal published a native hook: %v", err)
	}
}

// mirrorHookFixture returns the resolved artifact the Marketplace resolver
// hands materialize for a verified MIRROR hook: no staged source tree exists,
// and Path names the verified artifact archive — which must never be read as
// a source layout. The archive file is created on disk so a read would
// succeed and only the acquisition mode keeps it out of the fingerprint.
func mirrorHookFixture(t *testing.T, target hcc.Target, digest string) (resolver.ResolvedArtifact, string) {
	t.Helper()
	definition := []byte(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"./run.sh"}]}]}`)
	bundle, err := hcc.IngestBundle(fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{Data: definition},
	}, hcc.HarnessClaude)
	if err != nil || diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("Claude mirror bundle ingest: bundle=%#v err=%v", bundle, err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("Claude mirror bundle resources = %d, want one", len(bundle.Resources))
	}
	sourceHook, ok := bundle.Resources[0].(*ir.Hook)
	if !ok || sourceHook.Source.File != "hooks/hooks.json" || sourceHook.Name() != "PreToolUse-command" {
		t.Fatalf("Claude mirror source hook = %#v", bundle.Resources[0])
	}
	canonical, err := artifact.FromIR(sourceHook)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}
	archiveBytes, _, err := artifact.Encode(canonical)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, _, err := artifact.Decode(bytes.NewReader(archiveBytes))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	resource, err := artifact.DecodeResource(decoded.Manifest.Kind, decoded.Payload)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	hook, ok := resource.(*ir.Hook)
	if !ok || hook.Source.File != "hooks/hooks.json" || hook.Name() != "PreToolUse-command" || hook.SourceDefinitionDigest() != "" {
		t.Fatalf("decoded Claude mirror hook = %#v", resource)
	}
	archive := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(archive, archiveBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return resolver.ResolvedArtifact{
		Entry:                   catalog.Entry{Kind: ir.KindHook, Name: "deploy", Resource: hook, Path: archive, SourceHarness: "claude"},
		Source:                  config.RepoCommitted,
		RegistryURL:             "https://marketplace.example",
		Harnesses:               []hcc.Target{target},
		IsMarketplace:           true,
		MarketplaceInstanceID:   "mp",
		MarketplaceRegistryID:   "src",
		MarketplaceResourceName: hook.Name(),
		MarketplaceDigest:       digest,
		MarketplaceAcquisition:  config.MarketplaceAcquisitionMirror,
		InstalledName:           "deploy",
	}, archive
}

// mirrorFingerprint computes the hook-source/mirror/v1 fingerprint a future
// native writer records for a verified mirror hook: verified metadata only,
// never source bytes.
func mirrorFingerprint(t *testing.T, res resolver.ResolvedArtifact, target, scope string) string {
	t.Helper()
	name := res.Name
	if res.InstalledName != "" {
		name = res.InstalledName
	}
	resource := res.MarketplaceResourceName
	if resource == "" {
		resource = res.Name
	}
	digest, err := HookSourceMirrorFingerprint(HookSourceMirrorFingerprintInput{
		InstanceID:    res.MarketplaceInstanceID,
		RegistryID:    res.MarketplaceRegistryID,
		Kind:          res.Resource.Kind().String(),
		ResourceName:  resource,
		Digest:        res.MarketplaceDigest,
		InstalledName: name,
		Target:        target,
		Scope:         scope,
	})
	if err != nil {
		t.Fatalf("mirror fingerprint: %v", err)
	}
	return digest
}

// marketplaceFingerprint computes the version-1 fingerprint a future native
// writer records for the staged Marketplace hook: the exact resource-revision
// source identity from the resolved artifact and the staged source bytes.
func marketplaceFingerprint(t *testing.T, res resolver.ResolvedArtifact, staged, target, scope string) string {
	t.Helper()
	identity, err := nativeHookSourceIdentity(res)
	if err != nil {
		t.Fatalf("marketplace source identity: %v", err)
	}
	digest, err := HookSourceFingerprint(HookSourceFingerprintInput{
		SourceIdentity: identity,
		Name:           "deploy",
		Target:         target,
		Scope:          scope,
		SourceRoot:     staged,
		DefinitionRel:  "hooks.json",
	})
	if err != nil {
		t.Fatalf("marketplace source fingerprint: %v", err)
	}
	return digest
}

// An ownership-proven native entry for a Marketplace-selected hook satisfies
// the same legacy output: the exact pinned resource revision is the source
// identity, so a client that resolved the same pin suppresses its duplicate
// legacy write while a proof recorded for a different revision does not.
// spec: hook-native-location-tolerance/native-owned-entry-prevents-duplicate-legacy-output
// spec: hook-native-location-tolerance/mismatched-source-or-native-value-retains-legacy-behavior
func TestMarketplaceHookNativeProofSuppressesLegacyOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	const pinDigest = "sha256:abababababababababababababababababababababababababababababababab"

	t.Run("exact_revision", func(t *testing.T) {
		repoRoot := t.TempDir()
		resolved, staged := marketplaceHookFixture(t, hcc.TargetFactory, pinDigest)
		nativeRoot := filepath.Join(repoRoot, ".factory")
		layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
		seed := nativeSeedFor(t, hcc.TargetFactory, false)
		nativeBody := seedNativeOwnedHook(t, nativeRoot, layout, seed,
			marketplaceFingerprint(t, resolved, staged, "factory", HookScopeRepository))

		report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		if diag.HasErrors(report.Diags) {
			t.Fatalf("exact revision was refused: %#v", report.Diags)
		}
		if data, err := os.ReadFile(legacyHookFileFor(hcc.TargetFactory, false, repoRoot, "")); err == nil {
			t.Fatalf("marketplace hook legacy output recreated: %s", data)
		}
		nativeAfter, err := os.ReadFile(filepath.Join(nativeRoot, filepath.FromSlash(layout.rel)))
		if err != nil || string(nativeAfter) != nativeBody {
			t.Fatalf("native file changed: %v", err)
		}
	})

	// A proof recorded under a different resource-revision identity — the
	// same native bytes owned for a different pinned digest — is no evidence
	// for this selected hook and the legacy write proceeds.
	t.Run("mismatched_revision", func(t *testing.T) {
		repoRoot := t.TempDir()
		resolved, staged := marketplaceHookFixture(t, hcc.TargetFactory, pinDigest)
		otherRevision := resolved
		otherRevision.MarketplaceDigest = "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
		nativeRoot := filepath.Join(repoRoot, ".factory")
		layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
		seed := nativeSeedFor(t, hcc.TargetFactory, false)
		seedNativeOwnedHook(t, nativeRoot, layout, seed,
			marketplaceFingerprint(t, otherRevision, staged, "factory", HookScopeRepository))

		if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
			t.Fatalf("sync: %v", err)
		}
		data, err := os.ReadFile(legacyHookFileFor(hcc.TargetFactory, false, repoRoot, ""))
		if err != nil || !strings.Contains(string(data), "run.sh") {
			t.Fatalf("mismatched revision suppressed legacy output: %v %q", err, data)
		}
	})

	// A mirror-acquired Marketplace hook carries no staged source tree — only
	// the verified artifact archive. Its native proof is the domain-separated
	// hook-source/mirror/v1 fingerprint over verified metadata, and that
	// proof suppresses the duplicate legacy write.
	// A hook selected through REAL verified Marketplace mirror resolution carries
	// no original source tree: Path is the verified artifact archive. Its
	// domain-separated mirror fingerprint still satisfies an ownership-proven
	// native entry, so the legacy-writing client does not recreate legacy output;
	// proof recorded for a different revision does not.
	// spec: hook-native-location-tolerance/verified-mirror-revision-uses-its-own-fingerprint
	// Exact revision: clean native proof suppresses the legacy write.
	t.Run("mirror_exact_revision", func(t *testing.T) {
		repoRoot := t.TempDir()
		resolved, _ := mirrorHookFixture(t, hcc.TargetFactory, pinDigest)
		seed := nativeSeedFor(t, hcc.TargetFactory, false)
		nativePath := mirrorHookNativeProof(t, repoRoot, seed.rule, resolved.Name, mirrorFingerprint(t, resolved, "factory", HookScopeRepository))
		nativeBody, err := os.ReadFile(nativePath)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
			t.Fatalf("sync: %v", err)
		}
		if data, err := os.ReadFile(legacyHookFileFor(hcc.TargetFactory, false, repoRoot, "")); err == nil {
			t.Fatalf("mirror hook legacy output recreated: %s", data)
		}
		nativeAfter, err := os.ReadFile(nativePath)
		if err != nil || string(nativeAfter) != string(nativeBody) {
			t.Fatalf("native file changed: %v", err)
		}
	})

	// A mirror proof recorded under a different verified content digest is
	// proof of a different resource revision: the legacy write proceeds.
	// Proof recorded for a different verified content digest is proof of a
	// different revision: legacy output proceeds untouched.
	// spec: hook-native-location-tolerance/unverified-or-changed-mirror-revision-never-satisfies
	t.Run("mirror_changed_digest", func(t *testing.T) {
		repoRoot := t.TempDir()
		resolved, _ := mirrorHookFixture(t, hcc.TargetFactory, pinDigest)
		otherRevision := resolved
		otherRevision.MarketplaceDigest = "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
		seed := nativeSeedFor(t, hcc.TargetFactory, false)
		mirrorHookNativeProof(t, repoRoot, seed.rule, resolved.Name, mirrorFingerprint(t, otherRevision, "factory", HookScopeRepository))

		if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
			t.Fatalf("sync: %v", err)
		}
		data, err := os.ReadFile(legacyHookFileFor(hcc.TargetFactory, false, repoRoot, ""))
		if err != nil || !strings.Contains(string(data), "run.sh") {
			t.Fatalf("changed mirror digest suppressed legacy output: %v %q", err, data)
		}
	})

	// A mirror pin without its verified content digest cannot produce a
	// fingerprint at all: nothing is suppressed and no native write happens.
	// spec: hook-native-location-tolerance/unverified-or-changed-mirror-revision-never-satisfies
	t.Run("mirror_missing_digest", func(t *testing.T) {
		repoRoot := t.TempDir()
		resolved, _ := mirrorHookFixture(t, hcc.TargetFactory, "")
		seed := nativeSeedFor(t, hcc.TargetFactory, false)
		withDigest := resolved
		withDigest.MarketplaceDigest = pinDigest
		nativePath := mirrorHookNativeProof(t, repoRoot, seed.rule, resolved.Name, mirrorFingerprint(t, withDigest, "factory", HookScopeRepository))
		nativeBody, err := os.ReadFile(nativePath)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
			t.Fatalf("sync: %v", err)
		}
		data, err := os.ReadFile(legacyHookFileFor(hcc.TargetFactory, false, repoRoot, ""))
		if err != nil || !strings.Contains(string(data), "run.sh") {
			t.Fatalf("missing mirror digest suppressed legacy output: %v %q", err, data)
		}
		nativeAfter, err := os.ReadFile(nativePath)
		if err != nil || string(nativeAfter) != string(nativeBody) {
			t.Fatalf("native file changed: %v", err)
		}
	})

	// A mirror fingerprint must never satisfy proof recorded under the
	// source-tree domain for the same selection: the domains are disjoint,
	// so a source fingerprint proof leaves the mirror hook's legacy write
	// intact.
	// spec: hook-native-location-tolerance/verified-mirror-revision-uses-its-own-fingerprint
	t.Run("mirror_source_domain_does_not_satisfy", func(t *testing.T) {
		repoRoot := t.TempDir()
		resolved, _ := mirrorHookFixture(t, hcc.TargetFactory, pinDigest)
		stagedRes, staged := marketplaceHookFixture(t, hcc.TargetFactory, pinDigest)
		stagedRes.MarketplaceResourceName = resolved.MarketplaceResourceName
		seed := nativeSeedFor(t, hcc.TargetFactory, false)
		mirrorHookNativeProof(t, repoRoot, seed.rule, resolved.Name, marketplaceFingerprint(t, stagedRes, staged, "factory", HookScopeRepository))

		if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
			t.Fatalf("sync: %v", err)
		}
		data, err := os.ReadFile(legacyHookFileFor(hcc.TargetFactory, false, repoRoot, ""))
		if err != nil || !strings.Contains(string(data), "run.sh") {
			t.Fatalf("source-domain proof suppressed mirror legacy output: %v %q", err, data)
		}
	})
}

// The stable source identity for each selectable source kind is derived
// exactly once here; both this reader and the future native writer produce
// the same strings for the same resolved artifact.
func TestNativeHookSourceIdentity(t *testing.T) {
	gitRes := resolver.ResolvedArtifact{
		Entry:        catalog.Entry{Name: "deploy"},
		RegistryURL:  "https://github.com/example/registry.git",
		RegistryPath: "plugins/deploy",
		Ref:          "v1.2.3",
	}
	if got, err := nativeHookSourceIdentity(gitRes); err != nil || got != "git:https://github.com/example/registry@v1.2.3:plugins/deploy" {
		t.Fatalf("git identity = %q, %v", got, err)
	}
	localRes := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Name: "deploy"},
		RegistryURL: t.TempDir(),
		V4Kind:      config.V4SourceDirectory,
	}
	wantLocal := "local:" + filepath.Clean(localRes.RegistryURL)
	if got, err := nativeHookSourceIdentity(localRes); err != nil || got != wantLocal {
		t.Fatalf("local identity = %q, %v want %q", got, err, wantLocal)
	}
	mpRes := resolver.ResolvedArtifact{
		Entry:                   catalog.Entry{Name: "deploy"},
		IsMarketplace:           true,
		MarketplaceResourceName: "deploy-hook",
		MarketplaceDigest:       "sha256:abababababababababababababababababababababababababababababababab",
	}
	if got, err := nativeHookSourceIdentity(mpRes); err != nil || got != "marketplace:resource/deploy-hook@sha256:abababababababababababababababababababababababababababababababab" {
		t.Fatalf("marketplace identity = %q, %v", got, err)
	}
	mpRes.MarketplaceDigest = ""
	if got, err := nativeHookSourceIdentity(mpRes); err == nil {
		t.Fatalf("missing digest identity = %q, want error", got)
	}
}

// nativeSeed describes one target's documented PR #403-native hook output for
// the fixture hook. The shapes are hand-authored to what the catalog-driven
// native writer emits: target-native event names (Cursor's camelCase
// preToolUse, Windsurf's pre_run_command), translated matchers, and rewritten
// per-hook support command paths — every one byte-distinct from the old
// plugin fragment rule {"matcher":"Bash","hooks":[{"command":"./run.sh",...}]}.
type nativeSeed struct {
	event string
	rule  string
	doc   string
}

// nativeSeedFor returns the native document and seeded entry identity for
// (target, machineScope). Windsurf machine scope is exercised by the shared
// file test instead and is not listed here.
func nativeSeedFor(t *testing.T, target hcc.Target, machine bool) nativeSeed {
	t.Helper()
	switch target {
	case hcc.TargetClaude:
		rule := `{"hooks":[{"command":"$CLAUDE_PROJECT_DIR/.claude/am-hooks/deploy/run.sh","type":"command"}],"matcher":"Bash"}`
		return nativeSeed{event: "PreToolUse", rule: rule,
			doc: `{"hooks":{"PreToolUse":[` + rule + `]}}` + "\n"}
	case hcc.TargetCursor:
		rule := `{"command":".cursor/am-hooks/deploy/run.sh","event":"preToolUse","matcher":"Bash|Shell","type":"command"}`
		return nativeSeed{event: "preToolUse", rule: rule,
			doc: `{"version":1,"hooks":{"preToolUse":[` + rule + `]}}` + "\n"}
	case hcc.TargetFactory:
		rule := `{"hooks":[{"command":".factory/am-hooks/deploy/run.sh","type":"command"}],"matcher":"Bash"}`
		return nativeSeed{event: "PreToolUse", rule: rule,
			doc: `{"PreToolUse":[` + rule + `]}` + "\n"}
	case hcc.TargetWindsurf:
		if machine {
			t.Fatal("windsurf machine scope is the shared-file case")
		}
		rule := `{"command":".devin/am-hooks/deploy/run.sh"}`
		return nativeSeed{event: "pre_run_command", rule: rule,
			doc: `{"hooks":{"pre_run_command":[` + rule + `]}}` + "\n"}
	}
	t.Fatalf("no native seed for %s", target)
	return nativeSeed{}
}

// seedNativeOwnedHook writes the target's documented native hook file and
// records clean ownership proof carrying the hook's source fingerprint and
// ownership resource identity — the exact evidence PR #403's writer leaves.
func seedNativeOwnedHook(t *testing.T, nativeRoot string, layout nativeHookLayout, seed nativeSeed, sourceDigest string) string {
	t.Helper()
	seedNativeProof(t, nativeRoot, layout, []byte(seed.doc),
		nativeHookProofEntry(t, seed.event, seed.rule, "deploy", sourceDigest))
	return seed.doc
}

// mirrorHookNativeProof writes the Factory native hook file plus the durable
// ownership record the future native writer leaves for it: the hook/v2
// identity over the native event and canonical rule bytes, the ownership
// resource name, and the supplied source-hook fingerprint.
func mirrorHookNativeProof(t *testing.T, repoRoot, rule, resourceName, sourceDigest string) string {
	t.Helper()
	layout, ok := nativeHookLayoutFor(hcc.TargetFactory, false)
	if !ok {
		t.Fatal("Factory native hook layout is unavailable")
	}
	nativeRoot := filepath.Join(repoRoot, ".factory")
	body := []byte(`{"PreToolUse":[` + rule + `]}` + "\n")
	seedNativeProof(t, nativeRoot, layout, body,
		nativeHookProofEntry(t, "PreToolUse", rule, resourceName, sourceDigest))
	return filepath.Join(nativeRoot, filepath.FromSlash(layout.rel))
}

// legacyHookFileFor returns the file legacy output would (re)create for the
// hook: hooks/hooks.json under the install root for Claude, Cursor, and
// Factory, and the marker-merged .windsurf/hooks.json for Windsurf's
// repository scope.
func legacyHookFileFor(target hcc.Target, machine bool, repoRoot, home string) string {
	if target == hcc.TargetWindsurf {
		if machine {
			return filepath.Join(home, ".codeium", "windsurf", "hooks.json")
		}
		return filepath.Join(repoRoot, ".windsurf", "hooks.json")
	}
	base := repoRoot
	if machine {
		base = home
	}
	return filepath.Join(base, "."+string(target), "hooks", "hooks.json")
}

// spec: hook-native-location-tolerance/native-owned-entry-prevents-duplicate-legacy-output
func TestNativeOwnedHookSuppressesLegacyRecreation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	cases := []struct {
		name      string
		target    hcc.Target
		source    config.Source
		machine   bool
		scopeName string
	}{
		{name: "claude_repository", target: hcc.TargetClaude, source: config.RepoCommitted, machine: false, scopeName: HookScopeRepository},
		{name: "claude_machine", target: hcc.TargetClaude, source: config.UserPersonal, machine: true, scopeName: HookScopeMachine},
		{name: "cursor_repository", target: hcc.TargetCursor, source: config.RepoCommitted, machine: false, scopeName: HookScopeRepository},
		{name: "cursor_machine", target: hcc.TargetCursor, source: config.UserPersonal, machine: true, scopeName: HookScopeMachine},
		{name: "factory_repository", target: hcc.TargetFactory, source: config.RepoCommitted, machine: false, scopeName: HookScopeRepository},
		{name: "factory_machine", target: hcc.TargetFactory, source: config.UserPersonal, machine: true, scopeName: HookScopeMachine},
		{name: "windsurf_repository", target: hcc.TargetWindsurf, source: config.RepoCommitted, machine: false, scopeName: HookScopeRepository},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			resolved, src := toleranceHookFixture(t, tc.source, tc.target)
			layout, ok := nativeHookLayoutFor(tc.target, tc.machine)
			if !ok {
				t.Fatalf("%s layout missing", tc.target)
			}
			nativeRoot, ok := nativeHookRootFor(tc.target, tc.machine, repoRoot, home)
			if !ok {
				t.Fatalf("%s native root missing", tc.target)
			}
			seed := nativeSeedFor(t, tc.target, tc.machine)
			fingerprint := toleranceFingerprint(t, src, string(tc.target), tc.scopeName)
			nativeBody := seedNativeOwnedHook(t, nativeRoot, layout, seed, fingerprint)
			resolvedFingerprint, err := nativeHookFingerprint(resolved, tc.target, tc.machine)
			if err != nil || resolvedFingerprint != fingerprint {
				t.Fatalf("captured source fingerprint = %q / %v, want seeded %q", resolvedFingerprint, err, fingerprint)
			}
			inspection := nativeHookInspectionFor(map[outputKey]*nativeHookInspection{}, outputKey{target: tc.target, machineScope: tc.machine}, repoRoot)
			if inspection.err != nil || !inspection.hookSatisfied(fingerprint, "deploy") {
				t.Fatalf("seeded Native Hook proof does not satisfy the captured hook: %v", inspection.err)
			}

			if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
				t.Fatalf("sync: %v", err)
			}
			legacyFile := legacyHookFileFor(tc.target, tc.machine, repoRoot, home)
			if data, readErr := os.ReadFile(legacyFile); readErr == nil {
				t.Fatalf("legacy hook output recreated under sync: %s", data)
			}
			// No pending legacy write may be reported either: --check sees the
			// same desired set, so nothing may be pending.
			dryReport, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}, WithDryRun())
			if err != nil {
				t.Fatalf("sync --check: %v", err)
			}
			for _, root := range dryReport.PerRoot {
				for _, w := range root.Written {
					if strings.Contains(w, "hooks.json") {
						t.Fatalf("sync --check reports a pending legacy write for %s: per-root=%+v", w, dryReport.PerRoot)
					}
				}
			}
			// The native file keeps its exact bytes.
			nativeAfter, err := os.ReadFile(filepath.Join(nativeRoot, filepath.FromSlash(layout.rel)))
			if err != nil || string(nativeAfter) != nativeBody {
				t.Fatalf("native file changed: %v", err)
			}
			// A second real sync (the --locked path takes the same seam) stays
			// quiet: no legacy file, no new drift.
			if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
				t.Fatalf("second sync: %v", err)
			}
			if data, readErr := os.ReadFile(legacyFile); readErr == nil {
				t.Fatalf("legacy hook output recreated on second sync: %s", data)
			}
		})
	}
}

// Semantically equal JSON with reordered keys classifies owned-clean on the
// canonical value, so a key-order rewrite of the proven native file changes
// nothing: the client recognizes the hook without rewriting either file.
// spec: hook-native-location-tolerance/native-entry-with-reordered-json-keys-still-satisfies
func TestNativeOwnedHookReorderedKeysStillSatisfies(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()
	resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetCursor)

	nativeRoot, ok := nativeHookRootFor(hcc.TargetCursor, false, repoRoot, home)
	if !ok {
		t.Fatal("cursor native root missing")
	}
	layout, _ := nativeHookLayoutFor(hcc.TargetCursor, false)
	seed := nativeSeedFor(t, hcc.TargetCursor, false)
	seedNativeOwnedHook(t, nativeRoot, layout, seed,
		toleranceFingerprint(t, src, "cursor", HookScopeRepository))

	// Rewrite the native file with every key order flipped; bytes differ,
	// canonical values do not.
	reordered := `{"hooks":{"preToolUse":[{"type":"command","matcher":"Bash|Shell","event":"preToolUse","command":".cursor/am-hooks/deploy/run.sh"}]},"version":1}` + "\n"
	writeNativeFile(t, nativeRoot, layout, []byte(reordered))

	if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if data, readErr := os.ReadFile(legacyHookFileFor(hcc.TargetCursor, false, repoRoot, home)); readErr == nil {
		t.Fatalf("legacy hook output recreated for a reordered native entry: %s", data)
	}
	nativeAfter, err := os.ReadFile(filepath.Join(nativeRoot, filepath.FromSlash(layout.rel)))
	if err != nil || string(nativeAfter) != reordered {
		t.Fatalf("native file rewritten: %v", err)
	}
}

// A hook that emits more than one native rule is proven only when EVERY
// recorded entry is still owned-clean. A recorded rule missing from the file,
// or one whose bytes drifted, is partial proof: the legacy write proceeds
// exactly as before and the native bytes are untouched.
// spec: hook-native-location-tolerance/mismatched-source-or-native-value-retains-legacy-behavior
func TestPartialNativeProofRetainsLegacyOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	secondRule := `{"hooks":[{"command":".factory/am-hooks/deploy/audit.sh","type":"command"}],"matcher":"Write"}`
	cases := map[string]func(t *testing.T, nativeRoot string, layout nativeHookLayout, seed nativeSeed, digest string){
		// The writer recorded two rules for this hook; only one survives in
		// the file, so the proof is incomplete.
		"missing_recorded_rule": func(t *testing.T, nativeRoot string, layout nativeHookLayout, seed nativeSeed, digest string) {
			seedNativeProof(t, nativeRoot, layout, []byte(seed.doc),
				nativeHookProofEntry(t, seed.event, seed.rule, "deploy", digest),
				nativeHookProofEntry(t, "PostToolUse", secondRule, "deploy", digest))
		},
		// Both recorded rules exist but one's bytes changed, so it is no
		// longer the recorded identity — partial proof again.
		"drifted_recorded_rule": func(t *testing.T, nativeRoot string, layout nativeHookLayout, seed nativeSeed, digest string) {
			drifted := `{"hooks":[{"command":".factory/am-hooks/deploy/changed.sh","type":"command"}],"matcher":"Write"}`
			body := `{"PostToolUse":[` + drifted + `],"PreToolUse":[` + seed.rule + `]}` + "\n"
			seedNativeProof(t, nativeRoot, layout, []byte(body),
				nativeHookProofEntry(t, seed.event, seed.rule, "deploy", digest),
				nativeHookProofEntry(t, "PostToolUse", secondRule, "deploy", digest))
		},
	}
	for name, seedFn := range cases {
		t.Run(name, func(t *testing.T) {
			repoRoot := t.TempDir()
			resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)
			nativeRoot := filepath.Join(repoRoot, ".factory")
			layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
			seed := nativeSeedFor(t, hcc.TargetFactory, false)
			seedFn(t, nativeRoot, layout, seed, toleranceFingerprint(t, src, "factory", HookScopeRepository))

			if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
				t.Fatalf("sync: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(nativeRoot, "hooks", "hooks.json"))
			if err != nil {
				t.Fatalf("partial proof suppressed legacy output: %v", err)
			}
			if !strings.Contains(string(data), "run.sh") {
				t.Fatalf("legacy output missing the hook under partial proof: %s", data)
			}
		})
	}
}

// A proven sibling does not leak ownership onto unproven hooks: the second
// hook is still written to the legacy file, and the handwritten native entry
// keeps its bytes.
// spec: hook-native-location-tolerance/native-owned-entry-prevents-duplicate-legacy-output
// spec: hook-native-location-tolerance/handwritten-hook-does-not-become-owned
func TestNativeOwnedHookKeepsUnprovenSiblings(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)
	// A second selected hook with NO native entry keeps legacy output.
	siblings, _ := sourceBackedHookFixtures(t, "claude", config.RepoCommitted, hcc.TargetFactory, "audit",
		[]byte(`{"hooks":{"PostToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"./audit.sh"}]}]}}`), nil)
	if len(siblings) != 1 {
		t.Fatalf("Claude sibling hooks = %d, want one", len(siblings))
	}
	sibling := siblings[0]

	nativeRoot := filepath.Join(repoRoot, ".factory")
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	seed := nativeSeedFor(t, hcc.TargetFactory, false)
	// The native file also carries a handwritten rule for another event.
	handwritten := `{"hooks":[{"command":"./hand.sh","type":"command"}],"matcher":"Write"}`
	body := `{"PreToolUse":[` + seed.rule + `],"SessionStart":[` + handwritten + `]}` + "\n"
	seedNativeProof(t, nativeRoot, layout, []byte(body),
		nativeHookProofEntry(t, seed.event, seed.rule, "deploy",
			toleranceFingerprint(t, src, "factory", HookScopeRepository)))

	if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved, sibling}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	legacyFile := filepath.Join(nativeRoot, "hooks", "hooks.json")
	data, err := os.ReadFile(legacyFile)
	if err != nil {
		t.Fatalf("legacy file must still carry the unproven sibling hook: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "audit.sh") {
		t.Fatalf("unproven hook missing from legacy output: %s", content)
	}
	if strings.Contains(content, "run.sh") {
		t.Fatalf("proven hook re-emitted into legacy output: %s", content)
	}
	nativeAfter, err := os.ReadFile(filepath.Join(nativeRoot, "hooks.json"))
	if err != nil || string(nativeAfter) != body {
		t.Fatalf("native file changed: %v", err)
	}
}

// A malformed native file fails closed for that target before any legacy
// mutation: the legacy file keeps its bytes and a stable coded diagnostic
// names the refusal.
// spec: hook-native-location-tolerance/malformed-native-file-leaves-both-locations-unchanged
// spec: hook-native-location-tolerance/read-only-check-cannot-mutate-ownership-state
func TestMalformedNativeHookFilePreservesBothLocations(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, _ := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)

	nativeRoot := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	malformed := []byte(`{"PreToolUse":"not-an-array"}`)
	nativePath := filepath.Join(nativeRoot, "hooks.json")
	if err := os.WriteFile(nativePath, malformed, 0o644); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(nativeRoot, "hooks")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyBefore := []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./old.sh"}],"_managed_by":"agent-marketplace"}]}` + "\n")
	legacyPath := filepath.Join(legacyDir, "hooks.json")
	if err := os.WriteFile(legacyPath, legacyBefore, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("missing %s diagnostic: %#v", diag.CodeHookNativeToleranceRefused, report.Diags)
	}
	if data, readErr := os.ReadFile(nativePath); readErr != nil || string(data) != string(malformed) {
		t.Fatalf("native file changed")
	}
	if data, readErr := os.ReadFile(legacyPath); readErr != nil || string(data) != string(legacyBefore) {
		t.Fatalf("legacy file changed under malformed native input: %v", data)
	}

	// The same refusal must surface under the read-only check without any
	// writes at all: no native file, no legacy file, no ownership-state file.
	stateBeforeCheck := nativeSupportSnapshot(t, stateHome)
	dryReport, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}, WithDryRun())
	if err != nil {
		t.Fatalf("sync --check: %v", err)
	}
	if !diag.HasCode(dryReport.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("check missing %s diagnostic: %#v", diag.CodeHookNativeToleranceRefused, dryReport.Diags)
	}
	if data, readErr := os.ReadFile(legacyPath); readErr != nil || string(data) != string(legacyBefore) {
		t.Fatalf("legacy file changed under --check")
	}
	if data, readErr := os.ReadFile(nativePath); readErr != nil || string(data) != string(malformed) {
		t.Fatalf("native file changed under --check")
	}
	assertNativeSupportSnapshot(t, stateBeforeCheck, nativeSupportSnapshot(t, stateHome))
	lockDir := filepath.Join(stateHome, "agent-marketplace", "locks")
	var stateFiles int
	if walkErr := filepath.Walk(stateHome, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(lockDir, name)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			stateFiles++
		}
		return nil
	}); walkErr != nil {
		t.Fatal(walkErr)
	}
	if stateFiles != 0 {
		t.Fatalf("sync --check wrote %d ownership-state files", stateFiles)
	}
}

// A malformed native file that refuses tolerance must also surface on the
// blocked-resources report, under the Marketplace installed name when the
// hook was installed under one, so status identifies the kept hook.
// spec: hook-native-location-tolerance/malformed-native-file-leaves-both-locations-unchanged
func TestMalformedNativeHookFileReportsBlockedResource(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, _ := marketplaceHookFixture(t, hcc.TargetFactory, "sha256:abababababababababababababababababababababababababababababababab")
	resolved.Kind = ir.KindHook
	resolved.IsMarketplace = true
	resolved.InstalledName = "deploy-copy"

	nativeRoot := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeFile := filepath.Join(nativeRoot, "hooks.json")
	legacyFile := filepath.Join(nativeRoot, "hooks", "hooks.json")
	if _, err := os.Lstat(legacyFile); !os.IsNotExist(err) {
		t.Fatalf("legacy aggregate already exists: %v", err)
	}
	if err := os.WriteFile(nativeFile, []byte(`{"PreToolUse":"not-an-array"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativeFile)

	report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	assertFileUnchanged(t, nativeFile, nativeBefore, nativeMode, nativeMtime)
	if _, err := os.Lstat(legacyFile); !os.IsNotExist(err) {
		t.Fatalf("legacy aggregate was created: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("missing %s diagnostic: %#v", diag.CodeHookNativeToleranceRefused, report.Diags)
	}
	want := BlockedResource{Kind: ir.KindHook, Name: "deploy-copy", Target: hcc.TargetFactory}
	found := false
	for _, got := range report.BlockedResources {
		if got == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("blocked resources = %+v, want entry %+v", report.BlockedResources, want)
	}
}

// A malformed native file blocks only its own (target, scope): another
// selected target still reconciles normally.
// spec: hook-native-location-tolerance/malformed-native-file-leaves-both-locations-unchanged
func TestMalformedNativeHookFileDoesNotBlockOtherTargets(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, _ := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)
	resolved.Harnesses = []hcc.Target{hcc.TargetFactory, hcc.TargetCursor}

	factoryRoot := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryRoot, "hooks.json"), []byte(`{"PreToolUse":"not-an-array"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("missing %s diagnostic: %#v", diag.CodeHookNativeToleranceRefused, report.Diags)
	}
	if len(report.BlockedResources) != 1 || report.BlockedResources[0].Name != "deploy" || report.BlockedResources[0].Target != hcc.TargetFactory {
		t.Fatalf("blocked resources = %#v, want only the factory hook", report.BlockedResources)
	}
	// Cursor has no native file: its legacy output proceeds unaffected.
	data, err := os.ReadFile(filepath.Join(repoRoot, ".cursor", "hooks", "hooks.json"))
	if err != nil || !strings.Contains(string(data), "run.sh") {
		t.Fatalf("independent target did not reconcile: %v %q", err, data)
	}
}

// Windsurf's machine-scope native file IS the legacy file
// (~/.codeium/windsurf/hooks.json): a proven native entry suppresses the
// marked duplicate while the handwritten sibling keeps its exact bytes and
// order inside the same document.
// spec: hook-native-location-tolerance/windsurf-machine-hooks-share-one-file
func TestWindsurfMachineSharedFileKeepsSiblings(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()
	resolved, src := toleranceHookFixture(t, config.UserPersonal, hcc.TargetWindsurf)

	// The shared file carries the proven rule under the Windsurf-native
	// pre_run_command event plus a handwritten sibling elsewhere.
	sharedDir := filepath.Join(home, ".codeium", "windsurf")
	provenRule := `{"command":".codeium/windsurf/am-hooks/deploy/run.sh"}`
	handwritten := `{"command":"./hand.sh","show_output":true}`
	sharedBody := []byte(`{"hooks":{"post_cascade_response":[` + handwritten + `],"pre_run_command":[` + provenRule + `]}}` + "\n")
	layout, _ := nativeHookLayoutFor(hcc.TargetWindsurf, true)
	seedNativeProof(t, sharedDir, layout, sharedBody,
		nativeHookProofEntry(t, "pre_run_command", provenRule, "deploy",
			toleranceFingerprint(t, src, "windsurf", HookScopeMachine)))

	if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	sharedPath := filepath.Join(sharedDir, "hooks.json")
	after, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("shared file: %v", err)
	}
	// No marked duplicate of the proven rule may be added, and the
	// handwritten sibling keeps its bytes — ideally the whole file is
	// byte-identical because nothing had to change.
	if string(after) != string(sharedBody) {
		t.Fatalf("shared file changed:\nbefore: %s\nafter:  %s", sharedBody, after)
	}
}

// A native entry whose recorded source fingerprint does not match this
// selected hook — or whose resource identity belongs to another resource —
// proves nothing for it: the legacy write proceeds exactly as before and the
// native bytes are untouched.
// spec: hook-native-location-tolerance/mismatched-source-or-native-value-retains-legacy-behavior
func TestMismatchedNativeProofRetainsLegacyOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cases := map[string]func(t *testing.T, src string, seed nativeSeed) []ownership.Entry{
		// Proof exists for the entry's bytes but records a DIFFERENT hook's
		// source fingerprint.
		"different_fingerprint": func(t *testing.T, src string, seed nativeSeed) []ownership.Entry {
			return []ownership.Entry{nativeHookProofEntry(t, seed.event, seed.rule, "deploy",
				"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")}
		},
		// Proof carries this hook's fingerprint but a different resource
		// identity — a name collision must not count as proof.
		"different_resource": func(t *testing.T, src string, seed nativeSeed) []ownership.Entry {
			return []ownership.Entry{nativeHookProofEntry(t, seed.event, seed.rule, "other-hook",
				toleranceFingerprint(t, src, "factory", HookScopeRepository))}
		},
		// Proof carries the right fingerprint and resource but the on-disk
		// rule is not the recorded identity, so it classifies unmanaged.
		"changed_native_rule": func(t *testing.T, src string, seed nativeSeed) []ownership.Entry {
			return []ownership.Entry{nativeHookProofEntry(t, seed.event,
				`{"hooks":[{"command":".factory/am-hooks/deploy/other.sh","type":"command"}],"matcher":"Bash"}`,
				"deploy", toleranceFingerprint(t, src, "factory", HookScopeRepository))}
		},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			repoRoot := t.TempDir()
			resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)
			nativeRoot := filepath.Join(repoRoot, ".factory")
			layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
			seed := nativeSeedFor(t, hcc.TargetFactory, false)
			seedNativeProof(t, nativeRoot, layout, []byte(seed.doc), entries(t, src, seed)...)
			nativeBefore, err := os.ReadFile(filepath.Join(nativeRoot, "hooks.json"))
			if err != nil {
				t.Fatal(err)
			}

			if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
				t.Fatalf("sync: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(nativeRoot, "hooks", "hooks.json"))
			if err != nil {
				t.Fatalf("legacy output must be retained for a mismatched proof: %v", err)
			}
			if !strings.Contains(string(data), "run.sh") {
				t.Fatalf("mismatched proof suppressed legacy output: %s", data)
			}
			nativeAfter, err := os.ReadFile(filepath.Join(nativeRoot, "hooks.json"))
			if err != nil || string(nativeAfter) != string(nativeBefore) {
				t.Fatalf("native file changed under mismatched proof: %v", err)
			}
		})
	}
}

// A managed legacy entry that still exists is NOT deleted just because the
// native proof exists (no migration or deletion in this release): the hook
// simply must not be re-emitted after it is gone. Here it survives unchanged.
// spec: hook-native-location-tolerance/native-owned-entry-prevents-duplicate-legacy-output
func TestNativeOwnedHookDoesNotDeleteSurvivingLegacyEntry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)

	nativeRoot := filepath.Join(repoRoot, ".factory")
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	seedNativeOwnedHook(t, nativeRoot, layout, nativeSeedFor(t, hcc.TargetFactory, false),
		toleranceFingerprint(t, src, "factory", HookScopeRepository))
	nativePath := filepath.Join(nativeRoot, filepath.FromSlash(layout.rel))
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

	legacyDir := filepath.Join(nativeRoot, "hooks")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyBefore := []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}],"_managed_by":"agent-marketplace"}]}` + "\n")
	legacyPath := filepath.Join(legacyDir, "hooks.json")
	if err := os.WriteFile(legacyPath, legacyBefore, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil || string(data) != string(legacyBefore) {
		t.Fatalf("surviving managed legacy entry changed or was deleted: %v %q", err, data)
	}
	assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
}

// When a valid native proof exists, the surviving-managed-twin check reads
// the legacy file through the same descriptor-anchored boundary as every
// other aggregate read: a symlinked legacy file, a symlinked parent
// directory, a FIFO, or a malformed legacy document can never be followed,
// block, or be mistaken for "no managed twin". Each unsafe shape fails the
// affected (target, scope) closed with the stable coded diagnostic, leaves
// the legacy path and everything it pointed at byte-identical, and does not
// block an independent target.
//
// This test deliberately carries no spec annotation: the malformed-file
// scenario's WHEN names a malformed/unsafe NATIVE file, already witnessed by
// TestMalformedNativeHookFilePreservesBothLocations and
// TestMalformedNativeHookFileDoesNotBlockOtherTargets. The unsafe LEGACY read
// boundary exercised here is implementation hardening inside the same
// fail-closed seam, not that scenario's trigger.
func TestNativeProofWithUnsafeLegacyFileFailsClosed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	managedTwin := []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}],"_managed_by":"agent-marketplace"}]}` + "\n")

	// setup seeds valid ownership-proven native output for the factory hook
	// and returns the legacy hooks directory that the twin check must read.
	setup := func(t *testing.T) (resolver.ResolvedArtifact, string, string) {
		repoRoot := t.TempDir()
		resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)
		resolved.Harnesses = []hcc.Target{hcc.TargetFactory, hcc.TargetCursor}
		nativeRoot := filepath.Join(repoRoot, ".factory")
		layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
		seedNativeOwnedHook(t, nativeRoot, layout, nativeSeedFor(t, hcc.TargetFactory, false),
			toleranceFingerprint(t, src, "factory", HookScopeRepository))
		legacyDir := filepath.Join(nativeRoot, "hooks")
		if err := os.MkdirAll(legacyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		return resolved, repoRoot, legacyDir
	}
	assertRefused := func(t *testing.T, repoRoot string, resolved resolver.ResolvedArtifact) {
		report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
			t.Fatalf("unsafe legacy file must fail closed with %s: %#v",
				diag.CodeHookNativeToleranceRefused, report.Diags)
		}
		// The independent cursor target reconciles normally.
		data, err := os.ReadFile(filepath.Join(repoRoot, ".cursor", "hooks", "hooks.json"))
		if err != nil || !strings.Contains(string(data), "run.sh") {
			t.Fatalf("independent target did not reconcile: %v %q", err, data)
		}
	}

	// A symlinked legacy file must never be followed: the managed twin it
	// points at is not evidence, and no write may travel through the link.
	t.Run("symlinked_legacy_file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink fixture requires unix")
		}
		resolved, repoRoot, legacyDir := setup(t)
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, managedTwin, 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(legacyDir, "hooks.json")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		assertRefused(t, repoRoot, resolved)
		if target, err := os.Readlink(link); err != nil || target != outside {
			t.Fatalf("legacy symlink replaced: %v %q", err, target)
		}
		if data, err := os.ReadFile(outside); err != nil || string(data) != string(managedTwin) {
			t.Fatalf("external file behind legacy symlink changed: %v %q", err, data)
		}
	})

	// A symlinked parent directory escapes the legacy root just as surely:
	// the descriptor walk must refuse it before the file is ever reached.
	t.Run("symlinked_legacy_parent", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink fixture requires unix")
		}
		resolved, repoRoot, legacyDir := setup(t)
		outsideDir := t.TempDir()
		outside := filepath.Join(outsideDir, "hooks.json")
		if err := os.WriteFile(outside, managedTwin, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(legacyDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideDir, legacyDir); err != nil {
			t.Fatal(err)
		}
		assertRefused(t, repoRoot, resolved)
		if data, err := os.ReadFile(outside); err != nil || string(data) != string(managedTwin) {
			t.Fatalf("external file behind symlinked parent changed: %v %q", err, data)
		}
	})

	// A FIFO at the legacy path would hang a plain os.ReadFile forever. The
	// bounded descriptor read must refuse it immediately; the test itself is
	// deadline-bound so a regression cannot wedge the suite.
	t.Run("fifo_legacy_file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("fifo fixture requires unix")
		}
		resolved, repoRoot, legacyDir := setup(t)
		fifo := filepath.Join(legacyDir, "hooks.json")
		if err := syscall.Mkfifo(fifo, 0o644); err != nil {
			t.Fatal(err)
		}
		type result struct {
			report *Report
			err    error
		}
		done := make(chan result, 1)
		go func() {
			report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
			done <- result{report, err}
		}()
		var res result
		select {
		case res = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("sync hung on FIFO legacy file")
		}
		if res.err != nil {
			t.Fatalf("sync: %v", res.err)
		}
		if !diag.HasCode(res.report.Diags, diag.CodeHookNativeToleranceRefused) {
			t.Fatalf("FIFO legacy file must fail closed with %s: %#v",
				diag.CodeHookNativeToleranceRefused, res.report.Diags)
		}
		if info, err := os.Lstat(fifo); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			t.Fatalf("FIFO legacy path replaced: %v", err)
		}
	})

	// A legacy file that exists but cannot be decoded cannot be surveyed for
	// a surviving managed twin: treating it as absent could drop user bytes.
	t.Run("malformed_legacy_file", func(t *testing.T) {
		resolved, repoRoot, legacyDir := setup(t)
		malformed := []byte("this is not json\n")
		legacyPath := filepath.Join(legacyDir, "hooks.json")
		if err := os.WriteFile(legacyPath, malformed, 0o644); err != nil {
			t.Fatal(err)
		}
		assertRefused(t, repoRoot, resolved)
		if data, err := os.ReadFile(legacyPath); err != nil || string(data) != string(malformed) {
			t.Fatalf("malformed legacy file changed: %v %q", err, data)
		}
	})

	// Positive control: an ordinary legacy file holding a marked managed
	// twin still keeps its exact bytes with no refusal.
	t.Run("ordinary_managed_twin", func(t *testing.T) {
		resolved, repoRoot, legacyDir := setup(t)
		nativeRoot := filepath.Join(repoRoot, ".factory")
		layout, ok := nativeHookLayoutFor(hcc.TargetFactory, false)
		if !ok {
			t.Fatal("Factory native hook layout is unavailable")
		}
		nativePath := filepath.Join(nativeRoot, filepath.FromSlash(layout.rel))
		nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
		store, err := ownership.Open(nativeRoot)
		if err != nil {
			t.Fatal(err)
		}
		stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
		legacyPath := filepath.Join(legacyDir, "hooks.json")
		if err := os.WriteFile(legacyPath, managedTwin, 0o644); err != nil {
			t.Fatal(err)
		}
		report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		if diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
			t.Fatalf("ordinary managed twin refused: %#v", report.Diags)
		}
		if data, err := os.ReadFile(legacyPath); err != nil || string(data) != string(managedTwin) {
			t.Fatalf("ordinary managed twin changed or deleted: %v %q", err, data)
		}
		assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
		assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	})
}

// A mid-loop survey failure must leave the caller's artifact slice
// untouched: the suppressed-output accumulator must not reuse the input
// backing array, or a fail-closed error return would still carry the
// already-rewritten fragment bytes. No spec annotation: the malformed-file
// scenario's WHEN names a malformed NATIVE file, witnessed by
// TestMalformedNativeHookFilePreservesBothLocations; this guards the
// error-return contract of the same seam only.
func TestSuppressNativeSatisfiedHookRulesErrorLeavesInputUnchanged(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)
	nativeRoot := filepath.Join(repoRoot, ".factory")
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	seedNativeOwnedHook(t, nativeRoot, layout, nativeSeedFor(t, hcc.TargetFactory, false),
		toleranceFingerprint(t, src, "factory", HookScopeRepository))
	ins := nativeHookInspectionFor(map[outputKey]*nativeHookInspection{},
		outputKey{target: hcc.TargetFactory}, repoRoot)
	if ins.err != nil {
		t.Fatalf("inspection: %v", ins.err)
	}
	legacyRoot := nativeRoot

	// The first fragment's managed legacy twin survives, so it is kept and
	// re-marshaled; the second fragment's legacy file is malformed, which
	// must abort with an error AND leave both input artifacts' bytes intact.
	rule := `{"hooks":[{"command":"./run.sh","type":"command"}],"matcher":"Bash"}`
	twin := `{"PreToolUse":[{"hooks":[{"command":"./run.sh","type":"command"}],"matcher":"Bash","_managed_by":"agent-marketplace"}]}` + "\n"
	firstPath := "hooks/hooks.json"
	firstAbs := filepath.Join(legacyRoot, filepath.FromSlash(firstPath))
	if err := os.MkdirAll(filepath.Dir(firstAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstAbs, []byte(twin), 0o644); err != nil {
		t.Fatal(err)
	}
	secondPath := "hooks/other.json"
	secondAbs := filepath.Join(legacyRoot, filepath.FromSlash(secondPath))
	if err := os.WriteFile(secondAbs, []byte("this is not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	firstBytes := []byte(`{"PreToolUse": [` + rule + `]}`)
	secondBytes := []byte(`{"PreToolUse":[` + rule + `]}`)
	artifacts := []artifact.Artifact{
		{Path: firstPath, Bytes: firstBytes, Fragment: true},
		{Path: secondPath, Bytes: secondBytes, Fragment: true},
	}

	if _, err := suppressNativeSatisfiedHookRules(resolved, outputKey{target: hcc.TargetFactory},
		legacyRoot, ins, artifacts); err == nil {
		t.Fatal("suppression must fail on the malformed second legacy file")
	}
	if !bytes.Equal(artifacts[0].Bytes, firstBytes) {
		t.Fatalf("error return rewrote input artifact bytes: %s", artifacts[0].Bytes)
	}
	if !bytes.Equal(artifacts[1].Bytes, secondBytes) {
		t.Fatalf("error return rewrote input artifact bytes: %s", artifacts[1].Bytes)
	}
}

// Guard: the seeded native rule for each target must be byte-distinct from
// the legacy fragment rule so these tests can never pass by accidental
// legacy/native byte equality — the very defect g7o2-native-shape-proof
// rejected.
func TestNativeSeedsAreDistinctFromLegacyFragment(t *testing.T) {
	legacyRule := `{"hooks":[{"command":"./run.sh","type":"command"}],"matcher":"Bash"}`
	for _, target := range []hcc.Target{hcc.TargetClaude, hcc.TargetCursor, hcc.TargetFactory, hcc.TargetWindsurf} {
		machine := false
		seed := nativeSeedFor(t, target, machine)
		canonicalNative, err := canonicalOwnedJSON([]byte(seed.rule))
		if err != nil {
			t.Fatalf("%s: native seed is not canonicalizable: %v", target, err)
		}
		canonicalLegacy, err := canonicalOwnedJSON([]byte(legacyRule))
		if err != nil {
			t.Fatal(err)
		}
		if string(canonicalNative) == string(canonicalLegacy) {
			t.Fatalf("%s: native seed must differ from the legacy rule", target)
		}
		if seed.event == "PreToolUse" && target != hcc.TargetClaude && target != hcc.TargetFactory {
			t.Fatalf("%s: expected a target-native event name, got %q", target, seed.event)
		}
		// The rejected implementation keyed proof on the LEGACY event and
		// canonical legacy rule bytes. Prove the seeded native identity can
		// never collide with it, so these tests fail under byte equality.
		legacyIdentity := ownership.HookIdentityDigest("PreToolUse", ownership.Digest(canonicalLegacy))
		nativeIdentity := ownership.HookIdentityDigest(seed.event, ownership.Digest(canonicalNative))
		if legacyIdentity == nativeIdentity {
			t.Fatalf("%s: native identity must differ from the legacy identity", target)
		}
	}
}
