package tui

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestBuildTreeIncludesRuleSectionAndLabel(t *testing.T) {
	items := buildTree([]row{mkRow("secure-coding", withAvailable, withKind(ir.KindRule))}, tabScope, nil)
	if got := skillNamesUnder(items, "scope:available:rules"); len(got) != 1 || got[0] != "secure-coding" {
		t.Fatalf("rule section rows = %v, want secure-coding", got)
	}
	if got := resourceKindSectionTitle(ir.KindRule); got != "Rules" {
		t.Fatalf("rule section title = %q, want Rules", got)
	}
}

func TestBuildRowsPreservesMalformedRuleIdentity(t *testing.T) {
	path := "/cache/registry/rules/secure-coding.mdc"
	rows := buildRows(nil, map[string]catalog.LoadedCatalog{
		"registry": {Malformed: []skill.MalformedSkill{{
			DirName: "secure-coding.mdc",
			Kind:    "rule",
			Path:    path,
			Error:   errors.New("invalid frontmatter"),
		}}},
	}, nil, nil, materialize.DriftReport{})
	if len(rows) != 1 {
		t.Fatalf("malformed rows = %d, want 1", len(rows))
	}
	if rows[0].Artifact.Kind != ir.KindRule {
		t.Fatalf("malformed kind = %v, want rule", rows[0].Artifact.Kind)
	}
	if rows[0].Artifact.Name != "secure-coding.mdc" {
		t.Fatalf("malformed name = %q, want secure-coding.mdc", rows[0].Artifact.Name)
	}
	if rows[0].Artifact.Path != path {
		t.Fatalf("malformed path = %q, want %q", rows[0].Artifact.Path, path)
	}
}

func TestBuildRowsRecognizesTopLevelLocalRuleSelectors(t *testing.T) {
	catalogs := map[string]catalog.LoadedCatalog{
		"registry": {Entries: []catalog.Entry{{Kind: ir.KindRule, Name: "secure-coding"}}},
	}
	local := &config.LocalConfig{Rules: config.KindSelector{
		Include: []string{"secure-coding"},
		Exclude: []string{"blocked-rule"},
	}}
	resolved := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Kind: ir.KindRule, Name: "secure-coding"}, CatalogKey: "registry"}}
	rows := buildRows(nil, catalogs, resolved, local, materialize.DriftReport{})
	if len(rows) != 1 || !rows[0].EnabledLocal {
		t.Fatalf("top-level local rule include was ignored: %#v", rows)
	}

	catalogs["registry"] = catalog.LoadedCatalog{
		Entries: []catalog.Entry{{Kind: ir.KindRule, Name: "blocked-rule"}},
	}
	resolved[0].Name = "blocked-rule"
	rows = buildRows(nil, catalogs, resolved, local, materialize.DriftReport{})
	if len(rows) != 1 || !rows[0].ExcludedLocally {
		t.Fatalf("top-level local rule exclude was ignored: %#v", rows)
	}
}
