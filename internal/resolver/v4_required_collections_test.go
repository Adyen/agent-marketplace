package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: resource-selection/collection-keeps-required-all-selection
// spec: resource-selection/collection-preserves-authored-required-subset
// spec: resource-selection/required-policy-alone-selects-no-resources
func TestV4CollectionPreservesRequiredSelection(t *testing.T) {
	for _, tc := range []struct {
		name, group         string
		wantAlpha, wantBeta bool
	}{
		{"all", "{all: true, required: true}", true, true},
		{"subset", "{include: [skill/mandatory-alpha], required: true}", true, false},
		{"policy only", "{required: true}", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, catalogs := requiredCollectionFixture(1)
			for _, collection := range []bool{false, true} {
				root := t.TempDir()
				var layers []config.Layer
				for _, layer := range []struct {
					source config.Source
					name   string
					group  string
					url    string
				}{
					{config.Organization, "mandatory", tc.group, "git@example.invalid:mandatory.git"},
					{config.RepoCommitted, "optional", "[]", "git@example.invalid:optional.git"},
				} {
					if layer.source == config.RepoCommitted && collection {
						layer.group = "{include: [collection/shared]}"
					}
					path := filepath.Join(root, layer.name+".yaml")
					data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources: {%s: {git: %s, ref: main}}\nresources: {%s: %s}\n", layer.name, layer.url, layer.name, layer.group)
					if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
						t.Fatal(err)
					}
					cfg, err := config.LoadConfigFromPath(path)
					if err != nil {
						t.Fatal(err)
					}
					layers = append(layers, config.Layer{Source: layer.source, Config: cfg})
				}
				cfg := config.MergeLayers(layers)
				setCatalogKeys(cfg)
				resolved, errs := Resolve(cfg, catalogs, nil)
				if len(errs) != 0 {
					t.Fatalf("collection=%t: %v", collection, errs)
				}
				for _, want := range []struct {
					name     string
					selected bool
				}{
					{"mandatory-alpha", tc.wantAlpha},
					{"mandatory-beta", tc.wantBeta},
				} {
					got := findResolved(resolved, want.name)
					if (got != nil) != want.selected {
						t.Fatalf("collection=%t: %s selected=%t, want %t", collection, want.name, got != nil, want.selected)
					}
					if got != nil && (!got.Required || got.Source != config.Organization) {
						t.Fatalf("required organization selection lost authority: %+v", got)
					}
				}
				member := findResolved(resolved, "optional-member")
				if (member != nil) != collection {
					t.Fatalf("collection=%t: member = %+v", collection, member)
				}
				if member != nil && (member.Source != config.RepoCommitted || member.Via != "collection:shared@1.0.0") {
					t.Fatalf("collection member lost repository provenance: %+v", member)
				}
			}
		})
	}
}
