package materialize

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestMaterializationSourcePathMarketplaceUsesResolvedPath(t *testing.T) {
	staged := filepath.Join(t.TempDir(), "resolved")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Path: staged, Resource: ir.NewSkill("approved", "description", "body", nil, nil, ir.SourceRef{File: filepath.Join(t.TempDir(), "untrusted", "SKILL.md")})}, IsMarketplace: true}
	if got := materializationSourcePath(resolved); got != resolved.Path {
		t.Fatalf("marketplace source = %q, want resolved path %q", got, resolved.Path)
	}
}

func TestMaterializationSourcePathMarketplaceArchiveFileIsNotASourceTree(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "artifacts", "sha256-x", "artifact.tar.gz")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Path: archive, Resource: ir.NewSkill("approved", "description", "body", nil, nil, ir.SourceRef{})}, IsMarketplace: true}
	if got := materializationSourcePath(resolved); got != "" {
		t.Fatalf("marketplace archive source = %q, want empty so the artifact cache dir is never a support-file source tree", got)
	}
}

func TestMaterializationSourcePathMarketplaceEmptyPathIsNotASourceTree(t *testing.T) {
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Resource: ir.NewSkill("approved", "description", "body", nil, nil, ir.SourceRef{})}, IsMarketplace: true}
	if got := materializationSourcePath(resolved); got != "" {
		t.Fatalf("marketplace empty-path source = %q, want empty so the working directory is never a support-file source tree", got)
	}
}

func TestMaterializationSourcePathMarketplaceMissingDirIsNotASourceTree(t *testing.T) {
	parent := t.TempDir()
	sibling := filepath.Join(parent, "sibling-digest")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "sibling.sh"), []byte("sibling bytes\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(parent, "selected-digest")
	skill := ir.NewSkill("mirror-skill", "description", "body", nil, nil, ir.SourceRef{})
	skill.SupportFiles = []string{"scripts/run.sh"}
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Path: missing, Resource: skill}, IsMarketplace: true}
	if got := materializationSourcePath(resolved); got != "" {
		t.Fatalf("marketplace missing staged dir source = %q, want empty so the shared staging parent never becomes a support-file source tree", got)
	}
}

func TestMirrorStagedSupportRemovalFailsClosedAndPreservesLastGood(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	repoRoot := t.TempDir()

	stagedParent := t.TempDir()
	staged := filepath.Join(stagedParent, "digest-a")
	if err := os.MkdirAll(filepath.Join(staged, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "scripts", "run.sh"), []byte("#!/bin/sh\necho good\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := ir.NewSkill("mirror-skill", "Mirror skill", "body", nil, nil, ir.SourceRef{})
	skill.SupportFiles = []string{"scripts/run.sh"}
	resolved := []resolver.ResolvedArtifact{{
		Entry:         catalog.Entry{Kind: ir.KindSkill, Name: "mirror-skill", Path: staged, Resource: skill},
		IsMarketplace: true,
		Harnesses:     []hcc.Target{hcc.TargetFactory},
	}}

	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("initial materialize.Run: %v", err)
	}
	installed := filepath.Join(repoRoot, ".factory", "skills", "mirror-skill")
	goodSupport, err := os.ReadFile(filepath.Join(installed, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("installed support script missing after first run: %v", err)
	}
	goodSkill, err := os.ReadFile(filepath.Join(installed, "SKILL.md"))
	if err != nil {
		t.Fatalf("installed SKILL.md missing after first run: %v", err)
	}

	if err := os.RemoveAll(staged); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(stagedParent, "digest-b")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "sibling-secret.txt"), []byte("sibling staged bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second materialize.Run: %v", err)
	}
	sawSourceError := false
	for _, d := range second.Diags {
		if d.Severity == diag.SeverityError && d.Code == "materialize.support-source.missing" {
			sawSourceError = true
		}
	}
	if !sawSourceError {
		t.Fatalf("missing staged support source synced without a materialize.support-source.missing error: %#v", second.Diags)
	}
	after, err := os.ReadFile(filepath.Join(installed, "scripts", "run.sh"))
	if err != nil || string(after) != string(goodSupport) {
		t.Fatalf("last good support script not preserved: %q, %v", after, err)
	}
	afterSkill, err := os.ReadFile(filepath.Join(installed, "SKILL.md"))
	if err != nil || string(afterSkill) != string(goodSkill) {
		t.Fatalf("last good SKILL.md not preserved: %q, %v", afterSkill, err)
	}
	if _, err := os.Stat(filepath.Join(installed, "sibling-secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("sibling staged resource leaked into installed skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installed, "digest-b", "sibling-secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("sibling staged tree leaked into installed skill: %v", err)
	}
}

func TestMaterializationSourcePathLocalUsesResourceSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "SKILL.md")
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Path: filepath.Join(t.TempDir(), "cache", "SKILL.md"), Resource: ir.NewSkill("local", "description", "body", nil, nil, ir.SourceRef{File: source})}}
	if got := materializationSourcePath(resolved); got != source {
		t.Fatalf("local source = %q, want %q", got, source)
	}
}
