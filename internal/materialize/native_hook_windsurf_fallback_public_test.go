package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

const (
	windsurfFallbackHandler = `{"command":"echo d77-fallback-manual"}`
	windsurfFallbackOther   = `{"command":"echo d77-fallback-post"}`
)

func windsurfFallbackFile(t *testing.T, repo, body string) string {
	t.Helper()
	path := filepath.Join(evalPath(t, repo), ".windsurf", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func windsurfFallbackModes() config.MaterializationModes {
	return config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
}

func windsurfNativeFile(t *testing.T, repo string) string {
	t.Helper()
	return filepath.Join(evalPath(t, repo), ".devin", "hooks.json")
}

func windsurfNativeProof(t *testing.T, repo string) ownership.Proof {
	t.Helper()
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(".devin/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func windsurfHookIdentity(t *testing.T, event, raw string) string {
	t.Helper()
	canonical, err := canonicalOwnedJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ownedJSONHookIdentity(event, canonical)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func windsurfProofOwns(proof ownership.Proof, identity string) bool {
	for _, id := range proof.OwnedIDs() {
		if id.IdentityDigest == identity {
			return true
		}
	}
	return false
}

func TestWindsurfHandwrittenFallbackRemainsActiveBesideNativeHook(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	fallbackBody := `{"hooks":{"pre_run_command":[` + windsurfFallbackHandler + `]}}` + "\n"
	fallback := windsurfFallbackFile(t, repo, fallbackBody)
	native := windsurfNativeFile(t, repo)
	modes := windsurfFallbackModes()
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-selected", "")

	fallbackBefore, fallbackMode, fallbackMtime := fileSnapshot(t, fallback)
	stateBefore := nativeSupportPersistentSnapshot(t, repo, home)
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes), WithDryRun()); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	assertFileUnchanged(t, fallback, fallbackBefore, fallbackMode, fallbackMtime)
	assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Fatalf("dry-run created the native file: %v", err)
	}

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	assertFileUnchanged(t, fallback, fallbackBefore, fallbackMode, fallbackMtime)
	data, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	manual := strings.Index(string(data), windsurfFallbackHandler)
	generated := strings.Index(string(data), "echo d77-selected")
	if manual < 0 || generated < 0 || manual > generated {
		t.Fatalf("native file lacks the handwritten fallback before the generated hook:\n%s", data)
	}
	if strings.Count(string(data), windsurfFallbackHandler) != 1 || strings.Count(string(data), "echo d77-selected") != 1 {
		t.Fatalf("native file duplicated a handler:\n%s", data)
	}
	if windsurfProofOwns(windsurfNativeProof(t, repo), windsurfHookIdentity(t, "pre_run_command", windsurfFallbackHandler)) {
		t.Fatal("copied fallback handler became owned")
	}

	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, native)
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("repeat sync: %v", err)
	}
	assertFileUnchanged(t, native, nativeBefore, nativeMode, nativeMtime)
}

func TestWindsurfFallbackCopiedManualIsSatisfiedWithoutOwnership(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	windsurfFallbackFile(t, repo, `{"hooks":{"pre_run_command":[{"command":"echo d77-same"}]}}`+"\n")
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-same", "")

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(windsurfFallbackModes())); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if n := windsurfNativeProof(t, repo).OwnedCount(); n != 0 {
		t.Fatalf("identical manual entry acquired %d owned entries", n)
	}
	data, err := os.ReadFile(windsurfNativeFile(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "echo d77-same") != 1 {
		t.Fatalf("identical manual entry was duplicated or dropped:\n%s", data)
	}
}

func TestWindsurfFallbackCopiedManualRetainedOnDeselection(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	windsurfFallbackFile(t, repo, `{"hooks":{"pre_run_command":[`+windsurfFallbackHandler+`]}}`+"\n")
	modes := windsurfFallbackModes()
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-selected", "")

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	proof := windsurfNativeProof(t, repo)
	if windsurfProofOwns(proof, windsurfHookIdentity(t, "pre_run_command", windsurfFallbackHandler)) {
		t.Fatal("copied fallback handler became owned")
	}
	if proof.OwnedCount() != 1 {
		t.Fatalf("owned entries = %d, want only the generated hook", proof.OwnedCount())
	}
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("deselecting sync: %v", err)
	}
	data, err := os.ReadFile(windsurfNativeFile(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "echo d77-selected") || strings.Count(string(data), windsurfFallbackHandler) != 1 {
		t.Fatalf("deselection did not remove only the managed hook:\n%s", data)
	}
}

func TestWindsurfFallbackDifferentEventKeepsBoth(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	fallbackBody := `{"hooks":{"post_run_command":[` + windsurfFallbackOther + `]}}` + "\n"
	fallback := windsurfFallbackFile(t, repo, fallbackBody)
	modes := windsurfFallbackModes()
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-selected", "")

	fallbackBefore, fallbackMode, fallbackMtime := fileSnapshot(t, fallback)
	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	assertFileUnchanged(t, fallback, fallbackBefore, fallbackMode, fallbackMtime)
	data, err := os.ReadFile(windsurfNativeFile(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), windsurfFallbackOther) != 1 || strings.Count(string(data), "echo d77-selected") != 1 {
		t.Fatalf("native file did not carry both events exactly once:\n%s", data)
	}
}

func TestWindsurfFallbackMixedLegacyManagedTransaction(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	native := windsurfNativeFile(t, repo)
	marked := `{"command":"echo d77-legacy-marked","_managed_by":"agent-marketplace"}`
	seed := `{"hooks":{"pre_run_command":[` + windsurfFallbackHandler + `,` + marked + `]}}` + "\n"
	fallback := windsurfFallbackFile(t, repo, seed)
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-selected", "")

	if _, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(windsurfFallbackModes())); err != nil {
		t.Fatalf("sync: %v", err)
	}
	data, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "echo d77-legacy-marked") {
		t.Fatalf("recognized legacy marked handler survived:\n%s", data)
	}
	if strings.Count(string(data), windsurfFallbackHandler) != 1 || strings.Count(string(data), "echo d77-selected") != 1 {
		t.Fatalf("mixed transaction duplicated or dropped a handler:\n%s", data)
	}
	fallbackData, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fallbackData), windsurfFallbackHandler) {
		t.Fatalf("handwritten fallback handler was dropped from the fallback:\n%s", fallbackData)
	}
	if windsurfProofOwns(windsurfNativeProof(t, repo), windsurfHookIdentity(t, "pre_run_command", windsurfFallbackHandler)) {
		t.Fatal("handwritten fallback handler became owned")
	}
	info, err := os.Stat(fallback)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("fallback file mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestWindsurfFallbackAllSelectedRefusedLeavesNativeAbsent(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	fallback := windsurfFallbackFile(t, repo, `{"hooks":{"pre_run_command":[`+windsurfFallbackHandler+`]}}`+"\n")
	refused := nativeSupportSource(t, "claude", "hooks/hooks.json", map[string]string{
		"hooks/hooks.json": `{"SubagentStop":[{"hooks":[{"type":"command","command":"echo refused"}]}]}`,
	}, "refused")
	refused.Harnesses = []hcc.Target{hcc.TargetWindsurf}
	seedNativeHookJSONLock(t, evalPath(t, repo))
	before, mode, mtime := fileSnapshot(t, fallback)
	stateBefore := nativeSupportPersistentSnapshot(t, repo, home)

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{refused}, WithMaterializationModes(windsurfFallbackModes()))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeHookWindsurfEventUnmappable) {
		t.Fatalf("refused-only selection diagnostics = %v, want %s", report.Diags, diag.CodeHookWindsurfEventUnmappable)
	}
	assertFileUnchanged(t, fallback, before, mode, mtime)
	if _, err := os.Stat(windsurfNativeFile(t, repo)); !os.IsNotExist(err) {
		t.Fatal("refused-only selection created the native file")
	}
	assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
}

func TestWindsurfFallbackMalformedRefusesWithCodedDiagnostic(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	fallback := windsurfFallbackFile(t, repo, `{"hooks":{"pre_run_command":[`)
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-selected", "")
	before, mode, mtime := fileSnapshot(t, fallback)

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(windsurfFallbackModes()))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeMaterializeHookLegacyMalformed) {
		t.Fatalf("malformed fallback diagnostics = %v, want %s", report.Diags, diag.CodeMaterializeHookLegacyMalformed)
	}
	assertFileUnchanged(t, fallback, before, mode, mtime)
	if _, err := os.Stat(windsurfNativeFile(t, repo)); !os.IsNotExist(err) {
		t.Fatal("malformed fallback did not refuse native publication")
	}
}

func TestWindsurfFallbackSymlinkRefusesWithCodedDiagnostic(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	target := filepath.Join(t.TempDir(), "outside-hooks.json")
	if err := os.WriteFile(target, []byte(`{"hooks":{"pre_run_command":[`+windsurfFallbackHandler+`]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(evalPath(t, repo), ".windsurf", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	targetBefore, targetMode, targetMtime := fileSnapshot(t, target)
	selected := additiveSafetyProvider(t, hcc.TargetWindsurf, false, "echo d77-selected", "")

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{selected}, WithMaterializationModes(windsurfFallbackModes()))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeMaterializeHookLegacyMalformed) {
		t.Fatalf("symlinked fallback diagnostics = %v, want %s", report.Diags, diag.CodeMaterializeHookLegacyMalformed)
	}
	assertFileUnchanged(t, target, targetBefore, targetMode, targetMtime)
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatalf("symlinked fallback changed: %q, %v", got, err)
	}
	if _, err := os.Stat(windsurfNativeFile(t, repo)); !os.IsNotExist(err) {
		t.Fatal("symlinked fallback did not refuse native publication")
	}
	if entries, err := os.ReadDir(filepath.Join(evalPath(t, repo), ".devin")); err == nil && len(entries) != 0 {
		t.Fatalf("refusal left residue in .devin: %v", entries)
	}
}
