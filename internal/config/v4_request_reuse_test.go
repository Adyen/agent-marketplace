package config

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Exercise the public adapter with expanded fragment decisions, not a second
// implementation of the exclusion helper.
func TestV4RequestReusePreservesRuntimeSelectors(t *testing.T) {
	for _, source := range []string{
		"git: https://example.test/tools.git, ref: main",
		"directory: ./resources",
	} {
		t.Run(strings.Split(source, ":")[0], func(t *testing.T) {
			for _, tc := range []struct {
				name       string
				actions    []string
				includes   []string
				excludes   []string
				excludeAll bool
			}{
				{"empty", nil, nil, nil, false},
				{"included", []string{"include:skill/a"}, []string{"a"}, nil, false},
				{"excluded", []string{"include:skill/a", "exclude:skill/a"}, nil, []string{"a"}, false},
				{"reincluded", []string{"exclude:skill/a", "include:skill/a"}, []string{"a"}, nil, false},
				{"required", []string{"required:skill/a", "exclude:skill/a"}, []string{"a"}, nil, false},
				{"replacement", []string{"required:skill/a", "include:skill/a", "exclude:skill/a"}, nil, []string{"a"}, false},
				{"repeated", []string{"include:skill/a", "exclude:skill/a", "include:skill/a", "exclude:skill/a"}, nil, []string{"a"}, false},
				{"unselected", []string{"include:skill/a", "exclude:skill/missing"}, []string{"a"}, []string{"missing"}, false},
				{"ordered", []string{"include:skill/z", "include:skill/a", "exclude:skill/b", "exclude:skill/b", "exclude:skill/c"}, []string{"a", "z"}, []string{"b", "c"}, false},
				{"wildcard", []string{"include:skill/a", "exclude:skill/*", "include:skill/z"}, []string{"z"}, nil, true},
				{"mixed", []string{"include:skill/a", "exclude:skill/*", "include:skill/a", "exclude:skill/a"}, nil, []string{"a"}, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					file := filepath.Join(t.TempDir(), "config.yaml")
					cfg, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\nsources: {g: {"+source+"}}\nresources: {g: {include: [command/kept]}}\n"), file, true)
					if err != nil {
						t.Fatal(err)
					}
					group := cfg.Groups["g"]
					control := group.Decisions[0]
					decisions := v4ExclusionDecisions(tc.actions...)
					for i := range decisions.Decisions {
						decision := &decisions.Decisions[i]
						decision.SourceName = "g"
						decision.Provenance = control.Provenance
						if decision.Request == nil {
							continue
						}
						request := *control.Request
						request.Required = decision.Request.Required
						request.Locator = decision.Locator
						request.Kind, request.Name, _ = strings.Cut(decision.Locator, "/")
						decision.Request = &request
						group.Include = append(group.Include, request)
					}
					group.Decisions = append(group.Decisions, decisions.Decisions...)
					cfg.Groups["g"] = group
					before, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					out, err := cfg.ToLegacyConfig(repoLayer)
					if err != nil || out == nil || len(out.Registries) != 1 {
						t.Fatalf("runtime adapter failed: %v", err)
					}
					entry := out.Registries[0]
					if !reflect.DeepEqual(entry.Skills.Include, tc.includes) ||
						!reflect.DeepEqual(entry.Skills.Exclude, tc.excludes) ||
						entry.Skills.ExcludeAll != tc.excludeAll ||
						!reflect.DeepEqual(entry.Commands.Include, []string{"kept"}) ||
						len(entry.Commands.Exclude) != 0 || entry.Commands.ExcludeAll || entry.ExcludeAll ||
						!reflect.DeepEqual(entry.V4Decisions, group.Decisions) {
						t.Fatalf("selectors or authored decisions changed: %#v", entry)
					}
					after, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatalf("runtime adapter mutated authored configuration: %v", err)
					}
				})
			}
		})
	}
}
