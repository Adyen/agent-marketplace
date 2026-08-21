package materialize

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestRunRejectsSymlinkedHarnessRoot(t *testing.T) {
	for _, source := range []config.Source{config.RepoCommitted, config.UserPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			repoRoot := t.TempDir()
			external := t.TempDir()
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("must survive"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, filepath.Join(rootForSource(repoRoot, homeDir, source), ".factory")); err != nil {
				t.Fatal(err)
			}

			resolved := []resolver.ResolvedArtifact{{
				Entry:     catalog.Entry{Name: "root-escape", Resource: ir.NewSkill("root-escape", "Root escape", "Body", nil, nil, ir.SourceRef{})},
				Source:    source,
				Harnesses: []hcc.Target{hcc.TargetFactory},
			}}
			report, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(report.Diags) == 0 {
				t.Fatal("expected a diagnostic for the symlinked harness root")
			}
			assertUnchangedFile(t, sentinel, []byte("must survive"), 0o600)
		})
	}
}

func TestRunRejectsMergeSymlinkOutsideTrustedCache(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	factoryDir := filepath.Join(repoRoot, ".factory")
	if err := os.MkdirAll(factoryDir, 0o755); err != nil {
		t.Fatal(err)
	}

	external := t.TempDir()
	externalPath := filepath.Join(external, "mcp.json")
	original := []byte(`{"mcpServers":{"outside":{"command":"outside"}}}`)
	if err := os.WriteFile(externalPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	mergePath := filepath.Join(factoryDir, "mcp.json")
	if err := os.Symlink(externalPath, mergePath); err != nil {
		t.Fatal(err)
	}

	mcp := ir.NewMcpServer("generated", "generated-command", []string{"serve"}, nil, nil, ir.SourceRef{File: "mcp.json", Line: 1})
	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "generated", Resource: mcp},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	foundWriteError := false
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == "materialize.write.error" {
			foundWriteError = true
			break
		}
	}
	if !foundWriteError {
		t.Fatalf("expected merge symlink rejection diagnostic, got %+v", report.Diags)
	}
	if target, err := os.Readlink(mergePath); err != nil || target != externalPath {
		t.Fatalf("merge symlink changed: target=%q err=%v", target, err)
	}
	assertUnchangedFile(t, externalPath, original, 0o600)
}

// Windsurf declares its repo root as "." so its install root resolves to the
// repository itself. Confinement must still be the canonical harness root
// (<repo>/.windsurf); otherwise a committed `.windsurf` symlink redirects every
// managed write to another in-repo directory — .git/hooks lands an executable.
func TestRunConfinesWindsurfRepoScopeToCanonicalRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	gitHooks := filepath.Join(repoRoot, ".git", "hooks")
	if err := os.MkdirAll(gitHooks, 0o755); err != nil {
		t.Fatal(err)
	}
	// Repository-committed, relative, and never leaving the repository, so
	// neither a lexical containment check nor os.Root alone rejects it.
	windsurfRoot := filepath.Join(repoRoot, windsurfDir)
	if err := os.Symlink(".git", windsurfRoot); err != nil {
		t.Fatal(err)
	}

	sourceDir := t.TempDir()
	hooksJSON := filepath.Join(sourceDir, "hooks.json")
	if err := os.WriteFile(hooksJSON, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	hook := ir.NewHookWithMatcher("planted-hook", "PostToolUse", "Edit", "hooks/pre-commit", "command", nil, ir.SourceRef{File: hooksJSON})
	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "planted-hook", Path: hooksJSON, Resource: hook},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetWindsurf},
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagCode(report.Diags, "materialize.mkdir.error") {
		t.Fatalf("expected the symlinked harness root to be refused, got %+v", report.Diags)
	}
	entries, err := os.ReadDir(gitHooks)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("materialization wrote through the .windsurf symlink into .git/hooks: %v", entries)
	}
	if target, err := os.Readlink(windsurfRoot); err != nil || target != ".git" {
		t.Fatalf("the committed symlink was replaced: target=%q err=%v", target, err)
	}
}

// The project materialization manifest is committed content naming files to
// DELETE, so an entry outside the canonical harness root has to be refused per
// entry rather than trusted because it is "target-relative".
func TestRunRefusesManifestEntryOutsideCanonicalRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	victimRel := ".github/workflows/ci.yml"
	victim := filepath.Join(repoRoot, filepath.FromSlash(victimRel))
	if err := os.MkdirAll(filepath.Dir(victim), 0o755); err != nil {
		t.Fatal(err)
	}
	victimBody := []byte("name: ci\non: [push]\n")
	if err := os.WriteFile(victim, victimBody, 0o644); err != nil {
		t.Fatal(err)
	}
	// An attacker who authored the victim file can compute its digest offline,
	// so a digest match cannot be the thing that authorizes the delete.
	digest, err := portablePathDigest(victim)
	if err != nil {
		t.Fatal(err)
	}

	data, err := yaml.Marshal(portableManifest{Version: 1, Entries: []portableManifestEntry{{
		Target: string(hcc.TargetWindsurf), Path: victimRel, Digest: digest,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "materialization-manifest.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagCode(report.Diags, "materialize.manifest.error") {
		t.Fatalf("expected the out-of-root manifest entry to be refused, got %+v", report.Diags)
	}
	for _, tr := range report.TargetReports() {
		if slices.Contains(tr.Removed, victimRel) {
			t.Fatalf("target %s reported removing %s", tr.Target, victimRel)
		}
	}
	assertUnchangedFile(t, victim, victimBody, 0o644)
}

// A symlinked checkout directory is an ordinary developer setup (and is what
// every macOS temp directory looks like). Confinement canonicalizes ancestors
// instead of refusing them, so sync still works there.
func TestRunMaterializesUnderSymlinkedRepositoryCheckout(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	realCheckout := t.TempDir()
	linkedCheckout := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(realCheckout, linkedCheckout); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), linkedCheckout, []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "linked-skill", Resource: ir.NewSkill("linked-skill", "Linked", "Body", nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if diag.HasErrors(report.Diags) {
		t.Fatalf("sync failed under a symlinked checkout: %+v", report.Diags)
	}
	if _, err := os.Stat(filepath.Join(realCheckout, ".factory", "skills", "linked-skill", "SKILL.md")); err != nil {
		t.Fatalf("nothing materialized under the symlinked checkout: %v", err)
	}
}

func rootForSource(repoRoot, homeDir string, source config.Source) string {
	if source.IsMachineScope() {
		return homeDir
	}
	return repoRoot
}

func assertUnchangedFile(t *testing.T, path string, want []byte, wantMode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("sentinel bytes changed: got %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat sentinel: %v", err)
	}
	if info.Mode().Perm() != wantMode.Perm() {
		t.Fatalf("sentinel mode changed: got %o, want %o", info.Mode().Perm(), wantMode.Perm())
	}
}

func TestRunRejectsSymlinkedNestedHookDirectory(t *testing.T) {
	for _, mode := range []config.MaterializationMode{
		config.MaterializationModeSymlink,
		config.MaterializationModeFile,
	} {
		t.Run(string(mode), func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			repoRoot := t.TempDir()
			external := t.TempDir()
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("must survive"), 0o600); err != nil {
				t.Fatal(err)
			}
			factoryRoot := filepath.Join(repoRoot, ".factory")
			if err := os.MkdirAll(factoryRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, filepath.Join(factoryRoot, "hooks")); err != nil {
				t.Fatal(err)
			}

			sourceDir := t.TempDir()
			hook := ir.NewHookWithMatcher("nested-hook", "PostToolUse", "Edit", "${DROID_PLUGIN_ROOT}/hooks/check.sh", "command", nil, ir.SourceRef{File: filepath.Join(sourceDir, "hooks.json")})
			resolved := []resolver.ResolvedArtifact{{
				Entry:     catalog.Entry{Name: "nested-hook", Path: filepath.Join(sourceDir, "hooks.json"), Resource: hook},
				Source:    config.RepoCommitted,
				Harnesses: []hcc.Target{hcc.TargetFactory},
			}}
			report, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(report.Diags) == 0 {
				t.Fatal("expected a diagnostic for the symlinked hook directory")
			}
			assertUnchangedFile(t, sentinel, []byte("must survive"), 0o600)
		})
	}
}
