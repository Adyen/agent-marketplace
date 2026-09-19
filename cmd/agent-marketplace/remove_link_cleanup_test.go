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
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
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
	origin, working := createBareRepoWithWorkingClone(t, "blocked-skills.git")
	writeSkill(t, working, "remove", "REMOVE ORIGINAL", nil)
	writeSkill(t, working, "keep", "KEEP ORIGINAL", nil)
	commitAndPushAll(t, working, "seed skills")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [factory]\n")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "\nharnesses: [factory]\nregistries:\n  - url: "+origin+"\n    ref: main\n    default: true\n")
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

type managedSkillLinkFixture struct {
	base, home, repo, origin, configPath, removePath, keepPath string
}

func newManagedSkillLinkFixture(t *testing.T, target, scope string) managedSkillLinkFixture {
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
	origin, working := createBareRepoWithWorkingClone(t, "blocked-skills-"+target+"-"+scope+".git")
	writeSkill(t, working, "remove", "REMOVE ORIGINAL", nil)
	writeSkill(t, working, "keep", "KEEP ORIGINAL", nil)
	commitAndPushAll(t, working, "seed skills")
	harnesses := "[" + target + "]"
	userPath := filepath.Join(home, ".agents", "config.yaml")
	repoPath := filepath.Join(repo, ".agents", "config.yaml")
	registry := "\nregistries:\n  - url: " + origin + "\n    ref: main\n    default: true\n"
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
		if scope == "repo" {
			root = filepath.Join(repo, ".agents")
		}
	}
	for _, name := range []string{"remove", "keep"} {
		info, err := os.Lstat(filepath.Join(root, "skills", name))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s %s output is not a symlink: %v", target, name, err)
		}
	}
	return managedSkillLinkFixture{base: base, home: home, repo: repo, origin: origin, configPath: map[bool]string{true: userPath, false: repoPath}[scope == "user"], removePath: filepath.Join(root, "skills", "remove"), keepPath: filepath.Join(root, "skills", "keep")}
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

func (f managedSkillLinkFixture) remove(t *testing.T, scope string, inferred bool) (string, error) {
	t.Helper()
	args := []string{"remove", "remove", "--from", f.origin, "--ref", "main"}
	if !inferred {
		args = append(args, "--kind", "skill")
	}
	if scope == "user" {
		args = append(args, "--scope", "user")
	}
	return executeCommand(args...)
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

func TestRemoveBlockedManagedSkillAuthorizationChanges(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scenario := range []string{"include", "lock", "restored"} {
		t.Run(scenario, func(t *testing.T) {
			f := newManagedSkillLinkFixture(t, "factory", "repo")
			if scenario == "include" {
				fragment := filepath.Join(f.repo, ".agents", "fragment.yaml")
				if err := os.WriteFile(fragment, []byte("tags: [before]\n"), 0o600); err != nil {
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
			original := commitDependencyTransaction
			t.Cleanup(func() { commitDependencyTransaction = original })
			commitDependencyTransaction = func(ctx context.Context, state string, desired []dependencytxn.DesiredFile, options ...dependencytxn.Option) (dependencytxn.Result, error) {
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
					if err := os.WriteFile(filepath.Join(f.repo, ".agents", "fragment.yaml"), []byte("tags: [after]\n"), 0o600); err != nil {
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
			out, err := f.remove(t, "repo", false)
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
