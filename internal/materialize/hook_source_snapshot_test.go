package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Locked Git resolution pins the IR SourceRef to the immutable exact snapshot
// (an absolute path) while Entry.Path is rebased to the readable catalog
// location. The fingerprint must then derive the bundle-relative definition
// path from the common trailing components and read the authoritative
// snapshot bytes.
func TestGitFingerprintSnapshotPinnedAbsoluteSource(t *testing.T) {
	snapshot := filepath.Join(t.TempDir(), "snapshots", "abc123")
	hooksDir := filepath.Join(snapshot, ".factory", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	def := []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}]}]}`)
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), def, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "run.sh"), []byte("#!/bin/sh\necho deploy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The readable catalog path shares only the bundle-relative suffix.
	readable := filepath.Join(t.TempDir(), "cache", "main", ".factory", "hooks", "hooks.json")

	hook := ir.NewHookWithMatcher("PreToolUse-command", "PreToolUse", "Bash", "./run.sh", "command", nil,
		ir.SourceRef{File: filepath.Join(hooksDir, "hooks.json")})
	res := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Name: "PreToolUse-command", Resource: hook, Path: readable},
		RegistryURL: "/srv/hooks.git",
		Ref:         "main",
	}

	fp, err := nativeHookFingerprint(res, "factory", false)
	if err != nil {
		t.Fatalf("nativeHookFingerprint: %v", err)
	}
	identity, err := nativeHookSourceIdentity(res)
	if err != nil {
		t.Fatalf("nativeHookSourceIdentity: %v", err)
	}
	manual, err := HookSourceFingerprint(HookSourceFingerprintInput{
		SourceIdentity: identity,
		Name:           "PreToolUse-command", Target: "factory", Scope: "repository",
		SourceRoot: snapshot, DefinitionRel: ".factory/hooks/hooks.json",
	})
	if err != nil {
		t.Fatalf("HookSourceFingerprint: %v", err)
	}
	if fp != manual {
		t.Fatalf("fingerprint %q != manual %q", fp, manual)
	}
}

func TestCommonTrailingSlashPath(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"/snap/abc/.factory/hooks/hooks.json", "/cache/main/.factory/hooks/hooks.json", ".factory/hooks/hooks.json"},
		{"/a/b/hooks.json", "/x/y/hooks.json", "hooks.json"},
		{"/a/b/hooks.json", "/x/y/other.json", ""},
		{"/a/b/hooks.json", "", ""},
	}
	for _, tc := range cases {
		if got := commonTrailingSlashPath(filepath.FromSlash(tc.a), filepath.FromSlash(tc.b)); got != tc.want {
			t.Errorf("commonTrailingSlashPath(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// When the snapshot-pinned source shares no trailing component with the
// readable path, the hook cannot be identified and must suppress nothing.
func TestGitFingerprintAbsoluteSourceWithoutCommonSuffixFails(t *testing.T) {
	hook := ir.NewHookWithMatcher("PreToolUse-command", "PreToolUse", "Bash", "./run.sh", "command", nil,
		ir.SourceRef{File: "/snap/abc/.factory/hooks/hooks.json"})
	res := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Name: "PreToolUse-command", Resource: hook, Path: "/cache/main/hooks.d/definition.yaml"},
		RegistryURL: "/srv/hooks.git",
		Ref:         "main",
	}
	fp, err := nativeHookFingerprint(res, "factory", false)
	if err == nil || !strings.Contains(err.Error(), "does not resolve under") {
		t.Fatalf("expected unresolved-source error, got fp=%q err=%v", fp, err)
	}
}
