package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

var windsurfLifecycleSupportBody = []byte("#!/bin/sh\necho support\n")

func windsurfSupportLeaves(t *testing.T, repo, hook, leaf string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(repo, "hook-support", "*", hook, filepath.FromSlash(leaf)))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// spec: hook-resources/portable-admission-retains-native-hook-support-boundaries
func TestRunWindsurfDeclaredSupportPortableLifecycle(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	sentinel := filepath.Join(repo, ".windsurf", "sentinel.txt")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
		t.Fatal(err)
	}
	additiveSafetyWrite(t, sentinel, "foreign sentinel")
	sentinelBefore, sentinelMode, sentinelMtime := fileSnapshot(t, sentinel)
	definition := []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"}]}]}`)
	resolved, _ := sourceBackedHookFixtures(t, "claude", config.RepoCommitted, hcc.TargetWindsurf, "check", definition,
		map[string][]byte{"hooks/scripts/check.sh": windsurfLifecycleSupportBody})
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	native := filepath.Join(repo, ".devin", "hooks.json")
	manifest := filepath.Join(repo, ".agents", "materialization-manifest.yaml")

	install, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || hasErrorDiagnostics(install.Diags) {
		t.Fatalf("install: %v / %+v", err, install)
	}
	data, err := os.ReadFile(native)
	if err != nil || !strings.Contains(string(data), "check.sh") {
		t.Fatalf("native hook not published: %q %v", data, err)
	}
	if proof := windsurfNativeProof(t, repo); proof.OwnedCount() != 1 {
		t.Fatalf("owned entries after install = %d, want one private proof", proof.OwnedCount())
	}
	scripts := windsurfSupportLeaves(t, repo, "check", "scripts/check.sh")
	if len(scripts) != 1 {
		t.Fatalf("published support scripts = %v, want exactly one", scripts)
	}
	script := scripts[0]
	if info, err := os.Lstat(script); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("support script is not a regular 0644 file: %v / %v", info, err)
	}
	if got, err := os.ReadFile(script); err != nil || !bytes.Equal(got, windsurfLifecycleSupportBody) {
		t.Fatalf("support script bytes = %q / %v, want source bytes", got, err)
	}
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("portable manifest not written: %v", err)
	}

	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, native)
	scriptBefore, scriptMode, scriptMtime := fileSnapshot(t, script)
	manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, manifest)
	if _, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes), WithDryRun()); err != nil {
		t.Fatalf("dry repeat: %v", err)
	}
	assertFileUnchanged(t, native, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, script, scriptBefore, scriptMode, scriptMtime)
	assertFileUnchanged(t, manifest, manifestBefore, manifestMode, manifestMtime)
	repeat, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || hasErrorDiagnostics(repeat.Diags) {
		t.Fatalf("repeat sync: %v / %+v", err, repeat)
	}
	assertFileUnchanged(t, native, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, script, scriptBefore, scriptMode, scriptMtime)
	assertFileUnchanged(t, manifest, manifestBefore, manifestMode, manifestMtime)

	if _, err := Run(t.Context(), repo, nil, WithMaterializationModes(modes)); err != nil {
		t.Fatalf("deselect: %v", err)
	}
	if data, err := os.ReadFile(native); err == nil && strings.Contains(string(data), "check.sh") {
		t.Fatalf("deselection left the owned native hook: %s", data)
	}
	if _, err := os.Lstat(script); !os.IsNotExist(err) {
		t.Fatalf("deselection left the owned support script: %v", err)
	}
	if proof := windsurfNativeProof(t, repo); proof.OwnedCount() != 0 {
		t.Fatalf("owned entries after deselection = %d, want zero", proof.OwnedCount())
	}
	assertFileUnchanged(t, sentinel, sentinelBefore, sentinelMode, sentinelMtime)
}

// spec: hook-resources/portable-admission-retains-native-hook-support-boundaries
func TestPortableWindsurfSupportNominationsStayBounded(t *testing.T) {
	digest := strings.Repeat("a", 64)
	key := "forged-" + digest
	for _, refused := range []portableNomination{
		{target: hcc.TargetWindsurf, entry: "hook-support", disk: "hook-support", tree: true},
		{target: hcc.TargetWindsurf, entry: "hook-support/" + key, disk: "hook-support/" + key, tree: true},
		{target: hcc.TargetWindsurf, entry: "hook-support/" + key + "/audit", disk: "hook-support/" + key + "/audit", tree: true},
		{target: hcc.TargetWindsurf, entry: "hook-support/forged-short/audit/x.sh", disk: "hook-support/forged-short/audit/x.sh"},
		{target: hcc.TargetWindsurf, entry: "hook-support/forged-" + strings.Repeat("A", 64) + "/audit/x.sh", disk: "hook-support/forged-" + strings.Repeat("A", 64) + "/audit/x.sh"},
		{target: hcc.TargetWindsurf, entry: "other/" + key + "/audit/x.sh", disk: "other/" + key + "/audit/x.sh"},
		{target: hcc.TargetWindsurf, entry: ".devin/hooks.json", disk: ".devin/hooks.json"},
	} {
		t.Run(refused.entry, func(t *testing.T) {
			repo, home := runNativePortableControl(t, hcc.TargetWindsurf, false)
			for _, dryRun := range []bool{false, true} {
				assertPortableNominationRefused(t, repo, home, refused, dryRun)
			}
		})
	}
}
