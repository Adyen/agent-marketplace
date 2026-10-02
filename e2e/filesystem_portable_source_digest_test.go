package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemPortableSourceCheckRefusesForeignSupport(t *testing.T) {
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v4"} {
		t.Run(version, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			source := filepath.Join(env.workspace, "vendor", "registry", "skills", "hash-probe")
			env.WriteFile(filepath.Join(source, "SKILL.md"), "---\nname: hash-probe\ndescription: hash control\n---\n\ninert native body\n")
			env.WriteFile(filepath.Join(source, "reference.txt"), "contained inert data")
			env.WriteRepoConfig("apiVersion: " + version + "\nmaterialization:\n  mode: file\n")
			env.MustRunAM("install", "hash-probe", "--from", "./vendor/registry", "--scope", "repo")
			env.MustRunAM("sync", "--check")
			installed := filepath.Join(env.workspace, ".factory", "skills", "hash-probe", "reference.txt")
			before := captureDest(t, installed)
			foreign := filepath.Join(env.home, "foreign-support.txt")
			env.WriteFile(foreign, "foreign inert data")
			foreignBefore := captureDest(t, foreign)
			if err := os.Rename(filepath.Join(source, "reference.txt"), filepath.Join(source, "retained.txt")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(foreign, filepath.Join(source, "reference.txt")); err != nil {
				t.Fatal(err)
			}
			result := env.RunAM("sync", "--check")
			env.AssertExitCode(result, 1)
			requireDestUntouched(t, before, captureDest(t, installed))
			requireDestUntouched(t, foreignBefore, captureDest(t, foreign))
			if data, err := os.ReadFile(installed); err != nil || !bytes.Equal(data, []byte("contained inert data")) {
				t.Fatalf("source check changed native control: %q %v", data, err)
			}
		})
	}
}
