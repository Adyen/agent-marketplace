package pipeline

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestMatchingAllResourceIdentitiesByTagIncludesRules(t *testing.T) {
	got := matchingAllResourceIdentitiesByTag(map[string]catalog.LoadedCatalog{
		"registry": {Entries: []catalog.Entry{
			{Kind: ir.KindSkill, Name: "review", Tags: []string{"ops"}},
			{Kind: ir.KindRule, Name: "secure-coding", Tags: []string{"ops"}},
		}},
	}, "ops")
	if len(got) != 2 {
		t.Fatalf("tag matches = %#v, want skill and rule", got)
	}
	if got[0].kind != ir.KindSkill || got[1].kind != ir.KindRule {
		t.Fatalf("tag matches = %#v, want sorted by resource kind", got)
	}
}

func TestMalformedRuleInstallErrorPreservesKindAndMDCPath(t *testing.T) {
	path := "/cache/registry/rules/secure-coding.mdc"
	err := malformedSkillInstallError(map[string]catalog.LoadedCatalog{
		"registry": {Malformed: []skill.MalformedSkill{{
			DirName: "secure-coding.mdc",
			Kind:    "rule",
			Path:    path,
			Error:   errors.New("invalid frontmatter"),
		}}},
	}, "secure-coding", ir.KindRule, true)
	if err == nil || !strings.Contains(err.Error(), "rule \"secure-coding\"") || !strings.Contains(err.Error(), path) {
		t.Fatalf("malformed install error = %v, want rule identity and path", err)
	}
}

func TestFindCatalogResourceKindDisambiguatesCollectionAndRule(t *testing.T) {
	catalogs := map[string]catalog.LoadedCatalog{
		"registry": {Entries: []catalog.Entry{
			{Kind: ir.KindCollection, Name: "shared-name"},
			{Kind: ir.KindRule, Name: "shared-name"},
		}},
	}
	for _, tc := range []struct {
		kind ir.ResourceKind
		want ir.ResourceKind
	}{
		{kind: ir.KindRule, want: ir.KindRule},
		{kind: ir.KindCollection, want: ir.KindCollection},
	} {
		match, err := findCatalogResource(catalogs, "shared-name", tc.kind, true)
		if err != nil {
			t.Fatalf("findCatalogResource(%s): %v", tc.kind, err)
		}
		if match == nil || match.kind != tc.want {
			t.Fatalf("findCatalogResource(%s) = %#v, want %s", tc.kind, match, tc.want)
		}
	}
}
