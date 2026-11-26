package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWithoutMarketplaceSelectionsRequiresSameOptionalFloatingRequest(t *testing.T) {
	skipped := []MarketplaceSelectionToSkip{{
		Source: RepoCommitted, SourceName: "hosted", Locator: "tools/skill/gone",
		URL: "https://market.example", Registry: "tools", Kind: "skill", Name: "gone",
		InstalledName: "gone-copy",
	}}
	for _, tc := range []struct {
		name          string
		endpoint      string
		revision      string
		installedName string
		required      bool
		bindSource    Source
		wantSkip      bool
	}{
		{name: "same request", endpoint: "https://market.example", wantSkip: true},
		{name: "changed endpoint", endpoint: "https://other.example"},
		{name: "explicit revision", endpoint: "https://market.example", revision: "sha256:" + strings.Repeat("a", 64)},
		{name: "changed installed name", endpoint: "https://market.example", installedName: "other-copy"},
		{name: "group-level required", endpoint: "https://market.example", required: true},
		{name: "different layer", endpoint: "https://market.example", bindSource: UserPersonal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installedName := tc.installedName
			if installedName == "" {
				installedName = "gone-copy"
			}
			required := ""
			if tc.required {
				required = "\n    required: true"
			}
			revision := ""
			if tc.revision != "" {
				revision = ", revision: " + tc.revision
			}
			dir := t.TempDir()
			root := filepath.Join(dir, "config.yaml")
			// This fragment still matches the old alias even if the root now
			// requests a different alias or an explicit revision.
			writeFile(t, filepath.Join(dir, "earlier.yaml"), `resources:
  hosted: {include: [{tools/skill/gone: {as: gone-copy}}]}`)
			writeFile(t, root, `apiVersion: agent-marketplace/v4
include: earlier.yaml
sources:
  hosted: {marketplace: `+tc.endpoint+`}
resources:
  hosted:
    include:
      - tools/skill/gone: {as: `+installedName+revision+`}`+required+`
`)
			v4, err := LoadV4ConfigFromPath(root)
			if err != nil {
				t.Fatal(err)
			}
			bindSource := tc.bindSource
			if bindSource == SourceUnset {
				bindSource = RepoCommitted
			}
			bound := withoutMarketplaceSelections(v4, bindSource, skipped)
			if got := len(bound.Requests) == 0; got != tc.wantSkip {
				t.Fatalf("request filtered = %t, want %t; requests = %#v", got, tc.wantSkip, bound.Requests)
			}
		})
	}
}

func TestWithoutMarketplaceSelectionsPreservesOtherSourceAndWildcard(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	writeFile(t, filepath.Join(dir, "exclusions.yaml"), `resources:
  hosted: {exclude: [tools/skill/gone, tools/skill/*]}
`)
	writeFile(t, root, `apiVersion: agent-marketplace/v4
include: exclusions.yaml
sources:
  hosted: {marketplace: https://market.example}
  other: {marketplace: https://other.example}
resources:
  hosted: [tools/skill/gone]
  other: [tools/skill/gone]
`)
	v4, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	bound := withoutMarketplaceSelections(v4, RepoCommitted, []MarketplaceSelectionToSkip{{
		Source: RepoCommitted, SourceName: "hosted", Locator: "tools/skill/gone",
		URL: "https://market.example", Registry: "tools", Kind: "skill", Name: "gone", InstalledName: "gone",
	}})
	if len(bound.Requests) != 1 || bound.Requests[0].SourceName != "other" || bound.Requests[0].Locator != "tools/skill/gone" {
		t.Fatalf("remaining requests = %#v", bound.Requests)
	}
	group := bound.Groups["hosted"]
	want := []string{"tools/skill/*"}
	if !slices.Equal(group.Exclude, want) || !slices.Equal(effectiveV4Exclusions(group, nil), want) {
		t.Fatalf("remaining exclusions = %v, decisions = %#v", group.Exclude, group.Decisions)
	}
}
