package materialize

import (
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestMaterializationSourcePathMarketplaceUsesResolvedPath(t *testing.T) {
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Path: filepath.Join(t.TempDir(), "resolved", "SKILL.md"), Resource: ir.NewSkill("approved", "description", "body", nil, nil, ir.SourceRef{File: filepath.Join(t.TempDir(), "untrusted", "SKILL.md")})}, IsMarketplace: true}
	if got := materializationSourcePath(resolved); got != resolved.Path {
		t.Fatalf("marketplace source = %q, want resolved path %q", got, resolved.Path)
	}
}

func TestMaterializationSourcePathLocalUsesResourceSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "SKILL.md")
	resolved := resolver.ResolvedArtifact{Entry: catalog.Entry{Path: filepath.Join(t.TempDir(), "cache", "SKILL.md"), Resource: ir.NewSkill("local", "description", "body", nil, nil, ir.SourceRef{File: source})}}
	if got := materializationSourcePath(resolved); got != source {
		t.Fatalf("local source = %q, want %q", got, source)
	}
}
