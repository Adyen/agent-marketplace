package pipeline

import (
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func pluginTestEntries() []catalog.Entry {
	return []catalog.Entry{
		{Kind: ir.KindPlugin, Name: "a", Plugin: &catalog.PluginEntry{}},
		{Kind: ir.KindPlugin, Name: "b", Plugin: &catalog.PluginEntry{}},
	}
}

func pluginDecisionInclude(kind, name string) config.V4SelectionDecision {
	return config.V4SelectionDecision{Action: config.V4SelectionExplicit, Request: &config.V4ResourceRequest{Kind: kind, Name: name}}
}

func pluginDecisionExclude(exclusion string) config.V4SelectionDecision {
	return config.V4SelectionDecision{Action: config.V4SelectionExclude, Exclusion: exclusion}
}

func resolverPluginSet(entry config.RegistryEntry, entries []catalog.Entry) map[string]bool {
	out := map[string]bool{}
	for _, selected := range resolver.ResourcesSelected(entry, entries) {
		if selected.Kind == ir.KindPlugin {
			out[selected.Name] = true
		}
	}
	return out
}

func specPluginSet(entry config.RegistryEntry, entries []catalog.Entry) map[string]bool {
	spec := catalog.PluginSelectionSpecFor(entry, nil, nil)
	return catalog.PluginSelectedNames(&spec, entries)
}

// The plugin-v1 manifest's committed selection must select exactly the plugin
// set the resolver selects for the same configuration.
func TestPluginLockSelectionMatchesResolver(t *testing.T) {
	entries := pluginTestEntries()
	gitDecl := config.SourceDecl{Kind: config.V4SourceGit, URL: "https://example.com/cat.git", Ref: "main", Format: "claude-marketplace"}
	cases := []struct {
		name string
		decl config.SourceDecl
	}{
		{"required-only group selects nothing", gitDecl},
		{"required with explicit include selects only it", func() config.SourceDecl {
			d := gitDecl
			d.Required = true
			d.Decisions = []config.V4SelectionDecision{pluginDecisionInclude("plugin", "a")}
			return d
		}()},
		{"all with wildcard exclusion empties the selection", func() config.SourceDecl {
			d := gitDecl
			d.All = true
			d.Decisions = []config.V4SelectionDecision{pluginDecisionExclude("*")}
			return d
		}()},
		{"all with kind wildcard exclusion empties the plugin selection", func() config.SourceDecl {
			d := gitDecl
			d.All = true
			d.Decisions = []config.V4SelectionDecision{pluginDecisionExclude("plugin/*")}
			return d
		}()},
		{"exclude then include re-selects in decision order", func() config.SourceDecl {
			d := gitDecl
			d.Decisions = []config.V4SelectionDecision{pluginDecisionExclude("plugin/a"), pluginDecisionInclude("plugin", "a")}
			return d
		}()},
		{"required group exclusions do not remove selected plugins", func() config.SourceDecl {
			d := gitDecl
			d.Required = true
			d.All = true
			d.Decisions = []config.V4SelectionDecision{pluginDecisionExclude("plugin/a")}
			return d
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := registryEntryForDecl(tc.decl)
			got := specPluginSet(entry, entries)
			want := resolverPluginSet(entry, entries)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("spec selection %v does not match resolver selection %v", got, want)
			}
		})
	}
}
