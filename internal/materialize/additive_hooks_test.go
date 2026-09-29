package materialize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pkgcatalog "github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type additiveFactoryHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type additiveFactoryGroup struct {
	Matcher string                `json:"matcher"`
	Hooks   []additiveFactoryHook `json:"hooks"`
}

// spec: hook-resources/same-named-hooks-from-two-registries-both-install
func TestAdditiveHooksIngestResolveMaterialize(t *testing.T) {
	tests := []struct {
		name          string
		docs          []string
		want          map[string][]additiveFactoryGroup
		wantRemaining map[string][]additiveFactoryGroup
	}{
		{
			name: "different matchers across registries",
			docs: []string{
				`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]}]}`,
				`{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo second"}]}]}`,
			},
			want: map[string][]additiveFactoryGroup{"PreToolUse": {
				{Matcher: "Bash", Hooks: []additiveFactoryHook{{Type: "command", Command: "echo first"}}},
				{Matcher: "Edit", Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
			wantRemaining: map[string][]additiveFactoryGroup{"PreToolUse": {
				{Matcher: "Edit", Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
		},
		{
			name: "same matcher different commands",
			docs: []string{
				`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]}]}`,
				`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo second"}]}]}`,
			},
			want: map[string][]additiveFactoryGroup{"PreToolUse": {
				{Matcher: "Bash", Hooks: []additiveFactoryHook{{Type: "command", Command: "echo first"}}},
				{Matcher: "Bash", Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
			wantRemaining: map[string][]additiveFactoryGroup{"PreToolUse": {
				{Matcher: "Bash", Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
		},
		{
			name: "session start without matcher",
			docs: []string{
				`{"SessionStart":[{"hooks":[{"type":"command","command":"echo first"}]}]}`,
				`{"SessionStart":[{"hooks":[{"type":"command","command":"echo second"}]}]}`,
			},
			want: map[string][]additiveFactoryGroup{"SessionStart": {
				{Hooks: []additiveFactoryHook{{Type: "command", Command: "echo first"}}},
				{Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
			wantRemaining: map[string][]additiveFactoryGroup{"SessionStart": {
				{Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
		},
		{
			name: "multiple handlers in one group",
			docs: []string{
				`{"SessionStart":[{"hooks":[{"type":"command","command":"echo first"},{"type":"command","command":"echo second"}]}]}`,
			},
			want: map[string][]additiveFactoryGroup{"SessionStart": {
				{Hooks: []additiveFactoryHook{{Type: "command", Command: "echo first"}}},
				{Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
			wantRemaining: map[string][]additiveFactoryGroup{"SessionStart": {
				{Hooks: []additiveFactoryHook{{Type: "command", Command: "echo second"}}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
			cfg := &config.Config{Harnesses: []string{"factory"}}
			catalogs := make(map[string][]pkgcatalog.Entry, len(tt.docs))
			for i, document := range tt.docs {
				registryDir := filepath.Join(t.TempDir(), "registry")
				hooksPath := filepath.Join(registryDir, ".factory", "hooks", "hooks.json")
				if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(hooksPath, []byte(document), 0o644); err != nil {
					t.Fatal(err)
				}
				cat, err := pkgcatalog.Load(registryDir, pkgcatalog.HarnessFactory)
				if err != nil {
					t.Fatalf("load registry %d: %v", i, err)
				}
				cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: registryDir, Source: config.RepoCommitted, Default: true, CatalogKey: registryDir})
				catalogs[registryDir] = cat.Entries
			}

			resolved, errs := resolver.Resolve(cfg, catalogs, nil, resolver.WithTargetHarnesses([]hcc.Target{hcc.TargetFactory}))
			if len(errs) != 0 {
				t.Fatalf("Resolve errors = %v, want none", errs)
			}
			report, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("first Run: %v", err)
			}
			if len(report.Diags) != 0 {
				t.Fatalf("first Run diagnostics = %v, want none", report.Diags)
			}
			hooksPath := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
			firstBytes, err := os.ReadFile(hooksPath)
			if err != nil {
				t.Fatalf("read materialized hooks: %v", err)
			}
			assertFactoryHooks(t, firstBytes, tt.want)

			report, err = Run(context.Background(), repoRoot, resolved)
			if err != nil || len(report.Diags) != 0 {
				t.Fatalf("second Run = %v, diagnostics %v", err, report.Diags)
			}
			secondBytes, err := os.ReadFile(hooksPath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(firstBytes, secondBytes) {
				t.Fatalf("second Run changed hooks bytes\nfirst: %s\nsecond: %s", firstBytes, secondBytes)
			}

			if len(resolved) < 2 {
				t.Fatal("test requires at least two resolved hook occurrences")
			}
			report, err = Run(context.Background(), repoRoot, resolved[1:])
			if err != nil || len(report.Diags) != 0 {
				t.Fatalf("removal Run = %v, diagnostics %v", err, report.Diags)
			}
			remainingBytes, err := os.ReadFile(hooksPath)
			if err != nil {
				t.Fatal(err)
			}
			assertFactoryHooks(t, remainingBytes, tt.wantRemaining)
		})
	}
}

func assertFactoryHooks(t *testing.T, data []byte, want map[string][]additiveFactoryGroup) {
	t.Helper()
	var got map[string][]additiveFactoryGroup
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode materialized hooks: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized hooks = %#v, want %#v", got, want)
	}
}
