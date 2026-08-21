package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeRejectsSymlinkedSkillsDirectory(t *testing.T) {
	for _, tc := range []struct {
		name        string
		root        string
		writeConfig func(*Env, string)
	}{
		{
			name: "repo",
			root: "/workspace/.factory",
			writeConfig: func(env *Env, registry string) {
				env.WriteGlobalConfig("harnesses:\n  - factory\n")
				env.WriteRepoConfig("registries:\n  - url: " + registry + "\n    ref: main\n    skills:\n      - symlinked-skill\n")
			},
		},
		{
			name: "machine",
			root: "/root/.factory",
			writeConfig: func(env *Env, registry string) {
				env.WriteGlobalConfig("harnesses:\n  - factory\nregistries:\n  - url: " + registry + "\n    ref: main\n    skills:\n      - symlinked-skill\n")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			registry := env.CreateBareRepo("symlinked-skills", SkillsOnly(Skill{Name: "symlinked-skill", Description: "symlink rejection"}))
			tc.writeConfig(env, registry)

			root := env.mustResolvePath(tc.root)
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(env.home, "outside")
			if err := os.MkdirAll(external, 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("must survive"), 0o600); err != nil {
				t.Fatal(err)
			}
			skills := filepath.Join(root, "skills")
			if err := os.Symlink(external, skills); err != nil {
				t.Fatal(err)
			}

			result := env.RunAM("sync")
			if result.ExitCode == 0 {
				t.Fatalf("sync accepted symlinked skills directory:\n%s", result.Combined)
			}
			env.AssertOutputContains(result, "symlink")
			got, err := os.ReadFile(sentinel)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "must survive" {
				t.Fatalf("sentinel bytes changed: got %q", got)
			}
			info, err := os.Stat(sentinel)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("sentinel mode changed: got %o", info.Mode().Perm())
			}
			linkInfo, err := os.Lstat(skills)
			if err != nil {
				t.Fatal(err)
			}
			if linkInfo.Mode()&os.ModeSymlink == 0 {
				t.Fatal("sync replaced the symlink instead of rejecting it")
			}
		})
	}
}
