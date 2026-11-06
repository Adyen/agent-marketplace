package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/materialize"
)

func TestRemoveBlockedManagedSkillPreservesSiblings(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repo := filepath.Join(base, "repo")
	for _, dir := range []string{home, repo, filepath.Join(base, "tmp")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("TMPDIR", filepath.Join(base, "tmp"))
	origin, working := createV4GitRegistry(t, "blocked-skills.git")
	writeSkill(t, working, "remove", "REMOVE ORIGINAL", nil)
	writeSkill(t, working, "keep", "KEEP ORIGINAL", nil)
	commitAndPushAll(t, working, "seed skills")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\n")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nharnesses: [factory]\nsources:\n  registry:\n    git: "+origin+"\n    ref: main\nresources:\n  registry:\n    all: true\n")
	withWorkingDir(t, repo)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, out)
	}
	removePath := filepath.Join(repo, ".factory", "skills", "remove")
	keepPath := filepath.Join(repo, ".factory", "skills", "keep")
	for _, path := range []string{removePath, keepPath} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a skill symlink: %v/%v", path, info, err)
		}
	}
	removeTarget, err := os.Readlink(removePath)
	if err != nil || removeTarget == "" {
		t.Fatalf("remove link target = %q err=%v", removeTarget, err)
	}
	keepTarget, err := os.Readlink(keepPath)
	if err != nil || keepTarget == "" {
		t.Fatalf("keep link target = %q err=%v", keepTarget, err)
	}
	keepSkill := filepath.Join(keepPath, "SKILL.md")
	keepEdited := []byte("KEEP EDITED BY USER\n")
	if err := os.WriteFile(keepSkill, keepEdited, 0o644); err != nil {
		t.Fatal(err)
	}
	manualPath := filepath.Join(repo, ".factory", "skills", "manual", "SKILL.md")
	manualBytes := []byte("HANDWRITTEN\n")
	if err := os.MkdirAll(filepath.Dir(manualPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manualPath, manualBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\nallowed_registries: []\n")
	if out, err := executeCommand("remove", "remove", "--kind", "skill", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("blocked skill removal: %v\n%s", err, out)
	}
	if _, err := os.Lstat(removePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed skill link remains: %v", err)
	}
	if got, err := os.Readlink(keepPath); err != nil || got != keepTarget {
		t.Fatalf("keep link target = %q err=%v, want %q", got, err, keepTarget)
	}
	if got, err := os.ReadFile(keepSkill); err != nil || !bytes.Equal(got, keepEdited) {
		t.Fatalf("keep bytes = %q err=%v", got, err)
	}
	if got, err := os.ReadFile(manualPath); err != nil || !bytes.Equal(got, manualBytes) {
		t.Fatalf("handwritten bytes = %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(removeTarget, "SKILL.md")); err != nil {
		t.Fatalf("removed cache target deleted: %v", err)
	}
}

func TestRemoveBlockedManagedSkillFromNestedLocalUsesNestedMaterializationRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repo := filepath.Join(base, "repo")
	nested := filepath.Join(repo, "sub")
	for _, dir := range []string{home, repo, nested, filepath.Join(base, "tmp")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("TMPDIR", filepath.Join(base, "tmp"))
	origin, working := createV4GitRegistry(t, "nested-blocked-skills.git")
	writeSkill(t, working, "remove", "REMOVE ORIGINAL", nil)
	writeSkill(t, working, "keep", "KEEP ORIGINAL", nil)
	commitAndPushAll(t, working, "seed skills")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nharnesses: [factory]\nsources:\n  registry:\n    git: "+origin+"\n    ref: main\nresources:\n  registry:\n    all: true\n")
	writeConfig(t, filepath.Join(nested, ".agents", "config.local.yaml"), "apiVersion: agent-marketplace/v4\nsources: {}\n")
	withWorkingDir(t, nested)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed nested sync: %v\n%s", err, out)
	}
	removePath := filepath.Join(nested, ".factory", "skills", "remove")
	keepPath := filepath.Join(nested, ".factory", "skills", "keep")
	if _, err := os.Lstat(removePath); err != nil {
		t.Fatal(err)
	}
	keepTarget, err := os.Readlink(keepPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keepPath, "SKILL.md"), []byte("KEEP EDITED BY USER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\nallowed_registries: []\n")
	if out, err := executeCommand("remove", "remove", "--kind", "skill", "--from", origin, "--ref", "main"); err != nil {
		t.Fatalf("blocked nested skill removal: %v\n%s", err, out)
	}
	if _, err := os.Lstat(removePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed nested skill link remains: %v", err)
	}
	if got, err := os.Readlink(keepPath); err != nil || got != keepTarget {
		t.Fatalf("nested keep target = %q err=%v, want %q", got, err, keepTarget)
	}
	if got, err := os.ReadFile(filepath.Join(keepPath, "SKILL.md")); err != nil || string(got) != "KEEP EDITED BY USER\n" {
		t.Fatalf("nested keep bytes = %q err=%v", got, err)
	}
}

type managedSkillLinkFixture struct {
	base, home, repo, origin, configPath, removePath, keepPath string
}

func newManagedResourceLinkFixture(t *testing.T, target, scope, kind string) managedSkillLinkFixture {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repo := filepath.Join(base, "repo")
	tmp := filepath.Join(base, "tmp")
	for _, dir := range []string{home, repo, tmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("TMPDIR", tmp)
	if target == "codex" && scope == "user" && kind == "subagent" {
		t.Setenv("CODEX_HOME", filepath.Join(base, "external-codex"))
	}
	origin, working := createV4GitRegistry(t, "blocked-resources-"+target+"-"+scope+"-"+kind+".git")
	if kind == "skill" {
		writeSkill(t, working, "remove", "REMOVE ORIGINAL", nil)
		writeSkill(t, working, "keep", "KEEP ORIGINAL", nil)
	} else {
		dir := map[string]string{"command": "commands", "subagent": "agents"}[kind]
		for _, name := range []string{"remove", "keep"} {
			writeFile(t, filepath.Join(working, dir, name+".md"), "---\nname: "+name+"\ndescription: Test resource\n---\n\n"+name+" original\n")
		}
	}
	commitAndPushAll(t, working, "seed resources")
	harnesses := "[" + target + "]"
	userPath := filepath.Join(home, ".agents", "config.yaml")
	repoPath := filepath.Join(repo, ".agents", "config.yaml")
	registry := "\nsources:\n  registry:\n    git: " + origin + "\n    ref: main\nresources:\n  registry:\n    all: true\n"
	if scope == "user" {
		writeConfig(t, userPath, "\nharnesses: "+harnesses+registry)
		writeConfig(t, repoPath, "\nharnesses: "+harnesses+"\n")
	} else {
		writeConfig(t, userPath, "\nharnesses: "+harnesses+"\n")
		writeConfig(t, repoPath, "\nharnesses: "+harnesses+registry)
	}
	withWorkingDir(t, repo)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, out)
	}
	root := repo
	if scope == "user" {
		root = home
	}
	if scope == "user" && target == "devin" {
		root = filepath.Join(home, ".config")
	}
	if scope == "user" && target == "codex" {
		root = filepath.Join(home, ".agents")
	}
	if kind == "subagent" && target == "codex" {
		if scope == "user" {
			root = os.Getenv("CODEX_HOME")
		} else {
			root = filepath.Join(repo, ".codex")
		}
	}
	switch target {
	case "factory":
		root = filepath.Join(root, ".factory")
	case "claude":
		root = filepath.Join(root, ".claude")
	case "cursor":
		root = filepath.Join(root, ".cursor")
	case "gemini":
		root = filepath.Join(root, ".gemini")
	case "windsurf":
		root = filepath.Join(root, ".windsurf")
	case "devin":
		if scope == "user" {
			root = filepath.Join(root, "devin")
		} else {
			root = filepath.Join(root, ".devin")
		}
	case "codex":
		if scope == "repo" && kind != "subagent" {
			root = filepath.Join(repo, ".agents")
		}
	}
	component := "skills"
	if kind == "subagent" {
		component = "agents"
	}
	ext := ".md"
	if kind == "skill" {
		ext = ""
	}
	if kind == "subagent" && target == "codex" {
		ext = ".toml"
	}
	if kind == "subagent" && target == "factory" {
		component = "droids"
	}
	if kind == "command" {
		component = "commands"
		if target == "devin" || target == "codex" {
			component = "skills"
			ext = ""
		}
		if target == "windsurf" {
			component = "workflows"
		}
		if target == "gemini" {
			ext = ".toml"
		}
	}
	removePath, keepPath := filepath.Join(root, component, "remove"+ext), filepath.Join(root, component, "keep"+ext)
	for _, path := range []string{removePath, keepPath} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if target == "factory" && kind == "command" {
			if !info.Mode().IsRegular() {
				t.Fatalf("forced-copy output is not regular: %s", path)
			}
		} else if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s %s output is not a symlink", target, path)
		}
	}
	return managedSkillLinkFixture{base: base, home: home, repo: repo, origin: origin, configPath: map[bool]string{true: userPath, false: repoPath}[scope == "user"], removePath: removePath, keepPath: keepPath}
}

func newManagedSkillLinkFixture(t *testing.T, target, scope string) managedSkillLinkFixture {
	return newManagedResourceLinkFixture(t, target, scope, "skill")
}

func (f managedSkillLinkFixture) block(t *testing.T) {
	t.Helper()
	path := filepath.Join(f.home, ".agents", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("allowed_registries:")) {
		t.Fatal("fixture already has allowed_registries")
	}
	raw = append(raw, []byte("\nallowed_registries: []\n")...)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f managedSkillLinkFixture) removeKind(t *testing.T, scope, kind string, inferred bool) (string, error) {
	t.Helper()
	args := []string{"remove", "remove", "--from", f.origin, "--ref", "main"}
	if !inferred {
		args = append(args, "--kind", kind)
	}
	if scope == "user" {
		args = append(args, "--scope", "user")
	}
	return executeCommand(args...)
}

func (f managedSkillLinkFixture) remove(t *testing.T, scope string, inferred bool) (string, error) {
	return f.removeKind(t, scope, "skill", inferred)
}

func TestRemoveBlockedManagedSkillMatrix(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cases := []struct {
		target, scope string
		inferred      bool
	}{{"factory", "repo", false}, {"factory", "repo", true}, {"cursor", "repo", false}, {"gemini", "repo", false}, {"windsurf", "repo", false}, {"devin", "repo", false}, {"devin", "user", false}, {"codex", "repo", false}, {"codex", "user", false}}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s/inferred=%t", tc.target, tc.scope, tc.inferred), func(t *testing.T) {
			f := newManagedSkillLinkFixture(t, tc.target, tc.scope)
			removeTarget, err := os.Readlink(f.removePath)
			if err != nil {
				t.Fatal(err)
			}
			keepTarget, err := os.Readlink(f.keepPath)
			if err != nil {
				t.Fatal(err)
			}
			keepSkill := filepath.Join(f.keepPath, "SKILL.md")
			keepEdited := []byte("KEEP EDITED BY USER\n")
			if err := os.WriteFile(keepSkill, keepEdited, 0o644); err != nil {
				t.Fatal(err)
			}
			manual := filepath.Join(filepath.Dir(f.keepPath), "manual", "SKILL.md")
			manualBytes := []byte("HANDWRITTEN\n")
			if err := os.MkdirAll(filepath.Dir(manual), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manual, manualBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			f.block(t)
			before := ownedCommandSnapshot(t, f.base)
			out, err := f.remove(t, tc.scope, tc.inferred)
			if err != nil {
				t.Fatalf("remove: %v\n%s", err, out)
			}
			after := ownedCommandSnapshot(t, f.base)
			for _, path := range []string{f.configPath, f.removePath} {
				rel, err := filepath.Rel(f.base, path)
				if err != nil {
					t.Fatal(err)
				}
				key := filepath.ToSlash(rel)
				delete(before, key)
				delete(after, key)
			}
			if !reflect.DeepEqual(before, after) {
				changed := make([]string, 0)
				for path, value := range before {
					if after[path] != value {
						changed = append(changed, path)
					}
				}
				for path := range after {
					if _, ok := before[path]; !ok {
						changed = append(changed, path)
					}
				}
				t.Fatalf("unrelated state changed: %v", changed)
			}
			if _, err := os.Lstat(f.removePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("remove link remains: %v", err)
			}
			if got, err := os.Readlink(f.keepPath); err != nil || got != keepTarget {
				t.Fatalf("keep target=%q err=%v", got, err)
			}
			if got, err := os.ReadFile(keepSkill); err != nil || !bytes.Equal(got, keepEdited) {
				t.Fatalf("keep=%q err=%v", got, err)
			}
			if got, err := os.ReadFile(manual); err != nil || !bytes.Equal(got, manualBytes) {
				t.Fatalf("manual=%q err=%v", got, err)
			}
			if _, err := os.Stat(filepath.Join(removeTarget, "SKILL.md")); err != nil {
				t.Fatalf("cache target missing: %v", err)
			}
			cfg, err := config.LoadConfigFromPath(f.configPath)
			if err != nil || cfg == nil || len(cfg.Registries) != 1 || !containsStr(cfg.Registries[0].Skills.Exclude, "remove") || containsStr(cfg.Registries[0].Skills.Exclude, "keep") {
				t.Fatalf("config=%#v error=%v", cfg, err)
			}
		})
	}
}

func TestRemoveBlockedManagedSkillRefusals(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cases := []struct {
		name, target, setup string
		want                error
	}{
		{"unselected", "factory", "unselected", materialize.ErrOwnedCleanupUnsupported},
		{"missing", "factory", "missing", materialize.ErrManagedLinkCleanupConflict},
		{"retarget", "factory", "retarget", materialize.ErrManagedLinkCleanupConflict},
		{"regular", "factory", "regular", materialize.ErrManagedLinkCleanupConflict},
		{"parent", "factory", "parent", materialize.ErrManagedLinkCleanupConflict},
		{"file mode", "factory", "file", materialize.ErrOwnedCleanupUnsupported},
		{"mixed", "factory", "mixed", materialize.ErrOwnedCleanupUnsupported},
		{"claude", "claude", "claude", materialize.ErrOwnedCleanupUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newManagedSkillLinkFixture(t, tc.target, "repo")
			switch tc.setup {
			case "unselected":
				if out, err := f.remove(t, "repo", false); err != nil {
					t.Fatalf("initial allowed removal: %v\n%s", err, out)
				}
				if _, err := os.Lstat(f.removePath); !os.IsNotExist(err) {
					t.Fatalf("initial link remains: %v", err)
				}
				if err := os.WriteFile(filepath.Join(f.keepPath, "SKILL.md"), []byte("UNSELECTED SIBLING PRESERVED\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(f.removePath); err != nil {
					t.Fatal(err)
				}
			case "retarget":
				target, err := os.Readlink(f.keepPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(f.removePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, f.removePath); err != nil {
					t.Fatal(err)
				}
			case "regular":
				if err := os.Remove(f.removePath); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.removePath, []byte("FOREIGN"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "parent":
				parent := filepath.Dir(f.removePath)
				external := filepath.Join(f.base, "external", "skills")
				if err := os.MkdirAll(external, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(external, "sentinel"), []byte("EXTERNAL"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, parent); err != nil {
					t.Fatal(err)
				}
			case "file":
				path := config.ProjectConfigPath(f.repo)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(raw, []byte("\nmaterialization:\n  mode: file\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
				withWorkingDir(t, f.repo)
				if out, err := executeCommand("sync"); err != nil {
					t.Fatalf("file-mode sync: %v\n%s", err, out)
				}
				if info, err := os.Lstat(f.removePath); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("file-mode target=%v err=%v", info, err)
				}
			case "mixed":
				path := config.ProjectConfigPath(f.repo)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.Replace(raw, []byte("harnesses: [factory]"), []byte("harnesses: [factory, claude]"), 1)
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				withWorkingDir(t, f.repo)
				if out, err := executeCommand("sync"); err != nil {
					t.Fatalf("mixed sync: %v\n%s", err, out)
				}
			}
			f.block(t)
			before := ownedCommandSnapshot(t, f.base)
			out, err := f.remove(t, "repo", false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v output=%s", err, tc.want, out)
			}
			after := ownedCommandSnapshot(t, f.base)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("refusal changed state: before=%v after=%v", before, after)
			}
		})
	}
}

func TestRemoveBlockedManagedResourceAuthorizationChanges(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cases := []struct{ kind, scenario string }{
		{"skill", "include"}, {"skill", "lock"}, {"skill", "restored"},
		{"command", "include"}, {"command", "lock"}, {"command", "restored"},
		{"subagent", "include"}, {"subagent", "lock"}, {"subagent", "restored"},
	}
	for _, tc := range cases {
		t.Run(tc.kind+"/"+tc.scenario, func(t *testing.T) {
			scenario := tc.scenario
			target := "factory"
			if tc.kind != "skill" {
				target = "devin"
			}
			f := newManagedResourceLinkFixture(t, target, "repo", tc.kind)
			if scenario == "include" {
				fragment := filepath.Join(f.repo, ".agents", "fragment.yaml")
				if err := os.WriteFile(fragment, []byte("resources:\n  registry:\n    tags: [before]\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(f.configPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.configPath, append(raw, []byte("\ninclude: [fragment.yaml]\n")...), 0o600); err != nil {
					t.Fatal(err)
				}
				withWorkingDir(t, f.repo)
				if out, err := executeCommand("sync"); err != nil {
					t.Fatalf("include seed: %v\n%s", err, out)
				}
			}
			f.block(t)
			originalConfig, configErr := os.ReadFile(f.configPath)
			if configErr != nil {
				t.Fatal(configErr)
			}
			var expected map[string]string
			original := lifecycle.CommitDependencyTransaction
			t.Cleanup(func() { lifecycle.CommitDependencyTransaction = original })
			lifecycle.CommitDependencyTransaction = func(ctx context.Context, state string, desired []dependencytxn.DesiredFile, options ...dependencytxn.Option) (dependencytxn.Result, error) {
				options = append(options, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
					if point.Stage == dependencytxn.FaultAfterFinalize && scenario == "restored" {
						if err := os.WriteFile(f.configPath, originalConfig, 0o600); err != nil {
							return err
						}
						expected = ownedCommandSnapshot(t, f.base)
					}
					return nil
				}))
				switch scenario {
				case "include":
					if err := os.WriteFile(filepath.Join(f.repo, ".agents", "fragment.yaml"), []byte("resources:\n  registry:\n    tags: [after]\n"), 0o600); err != nil {
						return dependencytxn.Result{}, err
					}
				case "lock":
					path := filepath.Join(f.repo, ".agents", "lock.yaml")
					raw, err := os.ReadFile(path)
					if err != nil {
						return dependencytxn.Result{}, err
					}
					if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
						return dependencytxn.Result{}, err
					}
				}
				if scenario != "restored" {
					expected = ownedCommandSnapshot(t, f.base)
				}
				for path := range expected {
					if strings.HasPrefix(path, "tmp/.agent-marketplace-config-") {
						delete(expected, path)
					}
				}
				return original(ctx, state, desired, options...)
			}
			out, err := f.removeKind(t, "repo", tc.kind, false)
			if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
				t.Fatalf("error=%v output=%s", err, out)
			}
			if scenario == "restored" {
				var committed *dependencytxn.CommittedScopeError
				if !errors.As(err, &committed) {
					t.Fatalf("error=%v missing committed classification", err)
				}
			}
			after := ownedCommandSnapshot(t, f.base)
			for path := range expected {
				if strings.HasPrefix(path, "tmp/.agent-marketplace-config-") {
					delete(expected, path)
				}
			}
			if expected == nil || !reflect.DeepEqual(expected, after) {
				changed := make([]string, 0)
				for path, value := range expected {
					if after[path] != value {
						changed = append(changed, path)
					}
				}
				for path := range after {
					if _, ok := expected[path]; !ok {
						changed = append(changed, path)
					}
				}
				t.Fatalf("authorization race changed fixture state: %v", changed)
			}
		})
	}
}

func TestRemoveBlockedManagedSkillSupportedMix(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newManagedSkillLinkFixture(t, "factory", "repo")
	raw, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte("harnesses: [factory]"), []byte("harnesses: [factory, devin]"), 1)
	if err := os.WriteFile(f.configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, f.repo)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("mixed sync: %v\n%s", err, out)
	}
	devinRemove := filepath.Join(f.repo, ".devin", "skills", "remove")
	devinKeep := filepath.Join(f.repo, ".devin", "skills", "keep")
	for _, path := range []string{devinRemove, devinKeep} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("mixed output %s is not symlink: %v", path, err)
		}
	}
	factoryKeepBytes := []byte("FACTORY KEEP EDITED\n")
	devinKeepBytes := []byte("DEVIN KEEP EDITED\n")
	if err := os.WriteFile(filepath.Join(f.keepPath, "SKILL.md"), factoryKeepBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devinKeep, "SKILL.md"), devinKeepBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	f.block(t)
	before := ownedCommandSnapshot(t, f.base)
	out, err := f.remove(t, "repo", false)
	if err != nil {
		t.Fatalf("mixed remove: %v\n%s", err, out)
	}
	after := ownedCommandSnapshot(t, f.base)
	for _, path := range []string{f.configPath, f.removePath, devinRemove} {
		rel, err := filepath.Rel(f.base, path)
		if err != nil {
			t.Fatal(err)
		}
		key := filepath.ToSlash(rel)
		delete(before, key)
		delete(after, key)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("mixed cleanup changed unrelated state")
	}
	for _, path := range []string{f.removePath, devinRemove} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("link remains %s: %v", path, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(f.keepPath, "SKILL.md")); err != nil || !bytes.Equal(got, factoryKeepBytes) {
		t.Fatalf("factory keep=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(devinKeep, "SKILL.md")); err != nil || !bytes.Equal(got, devinKeepBytes) {
		t.Fatalf("devin keep=%q err=%v", got, err)
	}
	cfg, err := config.LoadConfigFromPath(f.configPath)
	if err != nil || cfg == nil || len(cfg.Registries) != 1 || !containsStr(cfg.Registries[0].Skills.Exclude, "remove") || containsStr(cfg.Registries[0].Skills.Exclude, "keep") {
		t.Fatalf("config=%#v err=%v", cfg, err)
	}
}

func TestRemoveBlockedManagedResourceRefusals(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cases := []struct {
		target, scope, kind, setup string
		want                       error
	}{
		{"factory", "repo", "command", "copy", materialize.ErrOwnedCleanupUnsupported},
		{"claude", "repo", "subagent", "shared", materialize.ErrOwnedCleanupUnsupported},
		{"gemini", "repo", "subagent", "generic", materialize.ErrOwnedCleanupUnsupported},
		{"codex", "repo", "subagent", "generic", materialize.ErrOwnedCleanupUnsupported},
		{"codex", "user", "subagent", "custom-root", materialize.ErrOwnedCleanupUnsupported},
		{"devin", "repo", "command", "file-mode", materialize.ErrOwnedCleanupUnsupported},
		{"devin", "repo", "command", "mixed", materialize.ErrOwnedCleanupUnsupported},
		{"devin", "repo", "subagent", "missing", materialize.ErrManagedLinkCleanupConflict},
		{"devin", "repo", "subagent", "retarget", materialize.ErrManagedLinkCleanupConflict},
	}
	for _, tc := range cases {
		t.Run(tc.target+"/"+tc.scope+"/"+tc.kind+"/"+tc.setup, func(t *testing.T) {
			f := newManagedResourceLinkFixture(t, tc.target, tc.scope, tc.kind)
			switch tc.setup {
			case "missing":
				if err := os.Remove(f.removePath); err != nil {
					t.Fatal(err)
				}
			case "retarget":
				keep, err := os.Readlink(f.keepPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(f.removePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(keep, f.removePath); err != nil {
					t.Fatal(err)
				}
			case "file-mode", "mixed":
				raw, err := os.ReadFile(f.configPath)
				if err != nil {
					t.Fatal(err)
				}
				if tc.setup == "file-mode" {
					raw = append(raw, []byte("\nmaterialization:\n  mode: file\n")...)
				} else {
					raw = bytes.Replace(raw, []byte("harnesses: [devin]"), []byte("harnesses: [devin, factory]"), 1)
				}
				if err := os.WriteFile(f.configPath, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				if out, err := executeCommand("sync"); err != nil {
					t.Fatalf("seed mode/mix: %v\n%s", err, out)
				}
				if tc.setup == "file-mode" {
					info, err := os.Lstat(f.removePath)
					if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
						t.Fatalf("not a file-mode tree: %v", err)
					}
				} else {
					info, err := os.Lstat(filepath.Join(f.repo, ".factory", "commands", "remove.md"))
					if err != nil || !info.Mode().IsRegular() {
						t.Fatalf("mixed copy not installed: %v", err)
					}
				}
			case "custom-root":
				if f.removePath != filepath.Join(f.base, "external-codex", "agents", "remove.toml") {
					t.Fatalf("wrong custom output: %s", f.removePath)
				}
			}
			f.block(t)
			before := ownedCommandSnapshot(t, f.base)
			out, err := f.removeKind(t, tc.scope, tc.kind, false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v output=%s", err, tc.want, out)
			}
			if after := ownedCommandSnapshot(t, f.base); !reflect.DeepEqual(before, after) {
				t.Fatal("refused resource cleanup mutated fixture")
			}
		})
	}
}

func TestRemoveBlockedManagedResourcePreservesSiblings(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	cases := []struct {
		target, scope, kind string
		inferred            bool
	}{
		{"claude", "repo", "command", false}, {"cursor", "repo", "command", false}, {"gemini", "repo", "command", true}, {"windsurf", "repo", "command", false}, {"devin", "repo", "command", false}, {"devin", "user", "command", false}, {"codex", "repo", "command", false}, {"codex", "user", "command", false},
		{"factory", "repo", "subagent", false}, {"cursor", "repo", "subagent", true}, {"devin", "repo", "subagent", false}, {"devin", "user", "subagent", false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s/%s/inferred=%t", tc.target, tc.scope, tc.kind, tc.inferred), func(t *testing.T) {
			f := newManagedResourceLinkFixture(t, tc.target, tc.scope, tc.kind)
			for _, path := range []string{f.removePath, f.keepPath} {
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("%s: %v", path, err)
				}
			}
			removeCache, err := os.Readlink(f.removePath)
			if err != nil {
				t.Fatal(err)
			}
			keepCache, err := os.Readlink(f.keepPath)
			if err != nil {
				t.Fatal(err)
			}
			keepBytes := []byte("KEEP EDITED RESOURCE\n")
			keepPath := f.keepPath
			if tc.kind == "command" && (tc.target == "devin" || tc.target == "codex") {
				keepPath = filepath.Join(f.keepPath, "SKILL.md")
			}
			if err := os.WriteFile(keepPath, keepBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			f.block(t)
			before := ownedCommandSnapshot(t, f.base)
			out, err := f.removeKind(t, tc.scope, tc.kind, tc.inferred)
			if err != nil {
				t.Fatalf("remove: %v\n%s", err, out)
			}
			after := ownedCommandSnapshot(t, f.base)
			for _, path := range []string{f.configPath, f.removePath} {
				rel, err := filepath.Rel(f.base, path)
				if err != nil {
					t.Fatal(err)
				}
				delete(before, filepath.ToSlash(rel))
				delete(after, filepath.ToSlash(rel))
			}
			if !reflect.DeepEqual(before, after) {
				changed := make([]string, 0)
				for path, value := range before {
					if after[path] != value {
						changed = append(changed, path)
					}
				}
				for path := range after {
					if _, ok := before[path]; !ok {
						changed = append(changed, path)
					}
				}
				t.Fatalf("resource cleanup changed unrelated state: %v", changed)
			}
			if _, err := os.Lstat(f.removePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("target remains: %v", err)
			}
			if got, err := os.ReadFile(keepPath); err != nil || !bytes.Equal(got, keepBytes) {
				t.Fatalf("keep=%q err=%v", got, err)
			}
			if got, err := os.Readlink(f.keepPath); err != nil || got != keepCache {
				t.Fatalf("keep link=%q error=%v", got, err)
			}
			if _, err := os.Stat(removeCache); err != nil {
				t.Fatalf("removed resource cache changed: %v", err)
			}
			cfg, err := config.LoadConfigFromPath(f.configPath)
			if err != nil || cfg == nil || len(cfg.Registries) != 1 {
				t.Fatalf("config=%#v err=%v", cfg, err)
			}
			if tc.kind == "command" && (!containsStr(cfg.Registries[0].Commands.Exclude, "remove") || containsStr(cfg.Registries[0].Commands.Exclude, "keep")) {
				t.Fatalf("command selector=%#v", cfg.Registries[0].Commands)
			}
			if tc.kind == "subagent" && (!containsStr(cfg.Registries[0].Subagents.Exclude, "remove") || containsStr(cfg.Registries[0].Subagents.Exclude, "keep")) {
				t.Fatalf("subagent selector=%#v", cfg.Registries[0].Subagents)
			}
		})
	}
}

func TestRemoveBlockedManagedSkillFIFOAuthorization(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	if os.Getenv("DEPENDENCY_FIFO_CHILD") != "1" {
		ready := filepath.Join(t.TempDir(), "ready")
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		result := runCommandProcess(ctx, t, t.Name(), exactCommandTestPattern(t.Name()), "DEPENDENCY_FIFO_CHILD=1", "DEPENDENCY_FIFO_READY="+ready)
		raw, err := os.ReadFile(ready)
		if err != nil || string(raw) != "ready" {
			t.Fatalf("did not reach FIFO authorization read: %q %v; %s", raw, err, result.output)
		}
		if result.err != nil {
			t.Fatalf("FIFO authorization did not refuse promptly: %v\n%s", result.err, result.output)
		}
		return
	}
	f := newManagedSkillLinkFixture(t, "factory", "repo")
	fragment := filepath.Join(f.repo, ".agents", "fragment.yaml")
	raw, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fragment, []byte("resources:\n  registry:\n    tags: [before]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, append(raw, []byte("\ninclude: [fragment.yaml]\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, f.repo)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("include seed: %v\n%s", err, out)
	}
	f.block(t)
	original := lifecycle.CommitDependencyTransaction
	t.Cleanup(func() { lifecycle.CommitDependencyTransaction = original })
	var expected map[string]string
	var fifoInfo os.FileInfo
	lifecycle.CommitDependencyTransaction = func(ctx context.Context, state string, desired []dependencytxn.DesiredFile, options ...dependencytxn.Option) (dependencytxn.Result, error) {
		if fifoInfo != nil {
			t.Fatal("unexpected second publication attempt")
		}
		if err := os.Rename(fragment, fragment+".saved"); err != nil {
			return dependencytxn.Result{}, err
		}
		if err := syscall.Mkfifo(fragment, 0o600); err != nil {
			return dependencytxn.Result{}, err
		}
		fifoInfo, err = os.Lstat(fragment)
		if err != nil || fifoInfo.Mode()&os.ModeNamedPipe == 0 {
			return dependencytxn.Result{}, fmt.Errorf("FIFO injection failed: %v", err)
		}
		expected = ownedCommandSnapshot(t, f.base, fragment)
		for path := range expected {
			if strings.HasPrefix(path, "tmp/.agent-marketplace-config-") {
				delete(expected, path)
			}
		}
		if err := os.WriteFile(os.Getenv("DEPENDENCY_FIFO_READY"), []byte("ready"), 0o600); err != nil {
			return dependencytxn.Result{}, err
		}
		return original(ctx, state, desired, options...)
	}
	out, err := f.remove(t, "repo", false)
	if !errors.Is(err, dependencytxn.ErrGenerationConflict) || !errors.Is(err, dependencytxn.ErrUnsafePath) {
		t.Fatalf("error=%v output=%s", err, out)
	}
	if fifoInfo == nil || expected == nil {
		t.Fatal("FIFO injection did not run")
	}
	info, statErr := os.Lstat(fragment)
	if statErr != nil || info.Mode()&os.ModeNamedPipe == 0 || !os.SameFile(fifoInfo, info) {
		t.Fatalf("FIFO changed: %v %v", info, statErr)
	}
	if after := ownedCommandSnapshot(t, f.base, fragment); !reflect.DeepEqual(expected, after) {
		t.Fatal("FIFO refusal changed files or left staging residue")
	}
	if _, err := os.Readlink(f.removePath); err != nil {
		t.Fatalf("remove link changed: %v", err)
	}
	if _, err := os.Readlink(f.keepPath); err != nil {
		t.Fatalf("keep link changed: %v", err)
	}
}
