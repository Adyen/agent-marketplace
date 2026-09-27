package materialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
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

func TestMaterializationSourcePathLocalUsesResourceSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "SKILL.md")
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Path: filepath.Join(t.TempDir(), "cache", "SKILL.md"), Resource: ir.NewSkill("local", "description", "body", nil, nil, ir.SourceRef{File: source})}}
	if got := materializationSourcePath(resolved); got != source {
		t.Fatalf("local source = %q, want %q", got, source)
	}
}
