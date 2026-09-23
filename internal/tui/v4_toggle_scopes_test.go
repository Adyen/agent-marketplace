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

func TestV4ScopedToggleKeepsSymlinkSourceIdentities(t *testing.T) {
	repoRaw := t.TempDir()
	repo, err := filepath.EvalSymlinks(repoRaw)
	if err != nil {
		t.Fatal(err)
	}
	homeRaw := t.TempDir()
	home, err := filepath.EvalSymlinks(homeRaw)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	source := filepath.Join(repo, "registry")
	real := filepath.Join(source, "version-1")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	currentA := filepath.Join(source, "current-a")
	currentB := filepath.Join(source, "current-b")
	if err := os.Symlink(real, currentA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, currentB); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(home, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  one: {directory: " + currentA + "}\n  two: {directory: " + currentB + "}\nresources:\n  two: [skill/beta]\n"
	if err := os.WriteFile(globalPath, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
	act := scopedToggleAction(v4ScopeRow(currentA, false), scopeGlobal)
	msg, ok := runScopedToggle(repo, act)().(configWrittenMsg)
	if !ok || msg.err != nil {
		t.Fatalf("toggle = %#v", msg)
	}
	loaded, err := config.LoadV4ConfigFromPath(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 2 || loaded.Sources["one"].Directory != currentA || loaded.Sources["two"].Directory != currentB {
		t.Fatalf("source identities = %#v", loaded.Sources)
	}
	one := loaded.Groups["one"].Include
	two := loaded.Groups["two"].Include
	if len(one) != 1 || one[0].Locator != "skill/alpha" || len(two) != 1 || two[0].Locator != "skill/beta" {
		t.Fatalf("source selections = one %#v two %#v", one, two)
	}
}

func v4ScopeRow(source string, excluded bool) row {
	return row{
		Artifact:        resolver.ResolvedArtifact{Entry: catalog.Entry{Name: "alpha", Kind: ir.KindSkill, SourceHarness: "claude"}, RegistryURL: source, V4Kind: config.V4SourceDirectory},
		RegistryURL:     source,
		ExcludedLocally: excluded,
	}
}
