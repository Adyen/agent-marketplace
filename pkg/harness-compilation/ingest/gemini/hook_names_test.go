package gemini

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"pgregory.net/rapid"
)

func hookNameSource(tb testing.TB, events map[string][]string, grouped bool) fstest.MapFS {
	tb.Helper()
	payload := map[string]any{}
	for event, types := range events {
		rules := make([]map[string]any, 0, len(types))
		for i, kind := range types {
			rules = append(rules, map[string]any{
				"type": kind, "command": fmt.Sprintf("%s:%d", event, i),
				"matcher": "Read", "timeout": 0, "x-keep": "keep",
			})
		}
		if grouped {
			payload[event] = []any{map[string]any{"matcher": "Read", "x-group": "keep", "hooks": rules}}
		} else {
			payload[event] = rules
		}
	}
	data, err := json.Marshal(map[string]any{"hooks": payload})
	if err != nil {
		tb.Fatal(err)
	}
	return fstest.MapFS{
		extensionManifestPath: {Data: []byte(`{"name":"r42","version":"1.0.0"}`)},
		settingsPath:          {Data: data},
	}
}

func TestLoadHooksPreservesCollisionNames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events map[string][]string
		want   []string
		warns  int
	}{
		{"empty", map[string][]string{}, nil, 0},
		{"defaults", map[string][]string{"BeforeTool": {"", "command", ""}}, []string{"pretooluse-command", "pretooluse-command-2", "pretooluse-command-3"}, 0},
		{"reserved-suffix", map[string][]string{"BeforeTool": {"command-2", "command", "command"}}, []string{"pretooluse-command-2", "pretooluse-command", "pretooluse-command-3"}, 0},
		{"nested-suffix", map[string][]string{"BeforeTool": {"command", "command", "command-2", "command", "command-2"}}, []string{"pretooluse-command", "pretooluse-command-2", "pretooluse-command-2-2", "pretooluse-command-3", "pretooluse-command-2-3"}, 0},
		{"canonical-alias", map[string][]string{"BeforeTool": {"command"}, "PreToolUse": {"command"}, "PRETOOLUSE": {"command"}}, []string{"pretooluse-command", "pretooluse-command-2", "pretooluse-command-3"}, 2},
		{"event-normalization", map[string][]string{"Custom Event": {"command"}, "Custom-Event": {"command"}}, []string{"custom-event-command", "custom-event-command-2"}, 2},
		{"type-case", map[string][]string{"BeforeTool": {"Command", "command", "Command"}}, []string{"pretooluse-Command", "pretooluse-command", "pretooluse-Command-2"}, 0},
	} {
		for _, grouped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/grouped=%t", tc.name, grouped), func(t *testing.T) {
				src := hookNameSource(t, tc.events, grouped)
				before := append([]byte(nil), src[settingsPath].Data...)
				hooks, diagnostics, err := LoadHooks(src, settingsPath)
				if err != nil || len(diagnostics) != tc.warns {
					t.Fatalf("load hooks: %v, diagnostics=%+v", err, diagnostics)
				}
				var names []string
				for _, hook := range hooks {
					names = append(names, hook.Name())
				}
				if !reflect.DeepEqual(names, tc.want) {
					t.Fatalf("names = %#v, want %#v", names, tc.want)
				}
				for _, d := range diagnostics {
					if d.Code != diag.CodeIngestGeminiHookEventPartialMapping || d.Severity != diag.SeverityWarning {
						t.Fatalf("unexpected diagnostic: %+v", d)
					}
				}
				bundle, err := LoadBundle(src)
				if err != nil || len(bundle.Resources) != len(names) {
					t.Fatalf("load bundle: %v, bundle=%+v", err, bundle)
				}
				for i, resource := range bundle.Resources {
					if resource.Name() != names[i] {
						t.Fatal("bundle resource keys differ from direct ingestion")
					}
				}
				if !reflect.DeepEqual(before, src[settingsPath].Data) {
					t.Fatal("hook ingestion changed source bytes")
				}
			})
		}
	}
}

func TestLoadHooksNamesMatchRestartOracle(t *testing.T) {
	tb := t
	rapid.Check(t, func(t *rapid.T) {
		events := map[string][]string{}
		for _, event := range []string{"BeforeTool", "PreToolUse", "PRETOOLUSE", "Custom Event", "Custom-Event"} {
			types := rapid.SliceOfN(rapid.SampledFrom([]string{"", "command", "command-2", "command-2-2", "command-3", "prompt"}), 0, 12).Draw(t, event)
			events[event] = append([]string{"command", "command"}, types...)
		}
		grouped := rapid.Bool().Draw(t, "grouped")
		src := hookNameSource(tb, events, grouped)
		hooks, diagnostics, err := LoadHooks(src, settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		for event := range events {
			order = append(order, event)
		}
		sort.Strings(order)
		used := map[string]bool{}
		index, warnings := 0, 0
		for _, event := range order {
			for i, kind := range events[event] {
				if kind == "" {
					kind = "command"
				}
				stem := event
				if event == "BeforeTool" {
					stem = "PreToolUse"
				} else {
					warnings++
				}
				base := strings.ToLower(strings.ReplaceAll(stem, " ", "-")) + "-" + kind
				name := base
				// Pin the pre-R42 restart scan, not the candidate cursor.
				for suffix := 2; used[name]; suffix++ {
					name = fmt.Sprintf("%s-%d", base, suffix)
				}
				used[name] = true
				if index >= len(hooks) {
					t.Fatal("hook omitted")
				}
				h := hooks[index]
				if h.Name() != name || h.Event != event || h.Type != kind || h.Command != fmt.Sprintf("%s:%d", event, i) || h.Matcher != "Read" || h.HookSource != "gemini" || h.Source.File != settingsPath || h.Timeout == nil || *h.Timeout != 0 || h.Extra["x-keep"] != "keep" {
					t.Fatalf("hook %d changed: name=%q hook=%+v, want name=%q", index, h.Name(), h, name)
				}
				if grouped && (h.Ext.Gemini == nil || h.Ext.Gemini.HookGroupExtra["x-group"] != "keep") {
					t.Fatal("native group metadata changed")
				}
				index++
			}
		}
		if len(hooks) != index || len(diagnostics) != warnings {
			t.Fatalf("hooks/diagnostics = %d/%d, want %d/%d", len(hooks), len(diagnostics), index, warnings)
		}
	})
}

func TestLoadHooksCollisionAllocationGrowth(t *testing.T) {
	const count = 512
	src := hookNameCollisionSource(t, count, true)
	var last string
	allocations := testing.AllocsPerRun(2, func() {
		hooks, diagnostics, err := LoadHooks(src, settingsPath)
		if err != nil || len(hooks) != count || len(diagnostics) != 0 {
			t.Fatalf("hook load failed: %v, diagnostics=%+v", err, diagnostics)
		}
		last = hooks[count-1].Name()
	})
	if last != "pretooluse-command-512" {
		t.Fatalf("last hook = %q", last)
	}
	if budget := float64(128 * count); allocations > budget {
		t.Fatalf("allocations = %v, want <=%v without repeated suffix scans", allocations, budget)
	}
}
