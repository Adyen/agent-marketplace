package config

import (
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
			v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: ` + tc.endpoint + `}
resources:
  hosted:
    include:
      - tools/skill/gone: {as: ` + installedName + revision + `}` + required + `
`))
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
