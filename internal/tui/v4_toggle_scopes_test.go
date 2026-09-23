package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestV4ScopedToggleActionsUseOneEditorAcrossScopes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	source := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(filepath.Join(source, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		scope scope
		path  string
		data  string
		row   row
		want  string
	}{
		{name: "global", scope: scopeGlobal, path: filepath.Join(home, ".agents", "config.yaml"), data: "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\n", row: v4ScopeRow(source, false), want: "include:\n      - skill/alpha"},
		{name: "project", scope: scopeProject, path: config.ProjectConfigPath(repo), data: "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\n", row: v4ScopeRow(source, false), want: "include:\n      - skill/alpha"},
		{name: "local", scope: scopeLocal, path: config.LocalConfigPath(repo), data: "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\n", row: v4ScopeRow(source, false), want: "include:\n      - skill/alpha"},
		{name: "local-exclude", scope: scopeLocalExclude, path: config.LocalConfigPath(repo), data: "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\nresources:\n  local:\n    exclude: [skill/alpha]\n", row: v4ScopeRow(source, true), want: "exclude: []"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(tc.path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tc.path, []byte(tc.data), 0o640); err != nil {
				t.Fatal(err)
			}
			act := scopedToggleAction(tc.row, tc.scope)
			msg, ok := runScopedToggle(repo, act)().(configWrittenMsg)
			if !ok || msg.err != nil {
				t.Fatalf("toggle = %#v", msg)
			}
			got, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), tc.want) {
				t.Fatalf("config =\n%s\nwant %q", got, tc.want)
			}
		})
	}
}

func v4ScopeRow(source string, excluded bool) row {
	return row{
		Artifact:        resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "alpha", Kind: ir.KindSkill, SourceHarness: "claude"}, RegistryURL: source, V4Kind: config.V4SourceDirectory},
		RegistryURL:     source,
		ExcludedLocally: excluded,
	}
}
