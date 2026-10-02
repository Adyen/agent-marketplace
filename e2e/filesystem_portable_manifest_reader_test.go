package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemPortableManifestReaderAdmission(t *testing.T) {
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v4"} {
		t.Run(version, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			source := filepath.Join(env.workspace, "vendor", "registry", "skills", "portable-probe", "SKILL.md")
			env.WriteFile(source, "---\nname: portable-probe\ndescription: portable\n---\n\noriginal inert content\n")
			env.WriteRepoConfig("apiVersion: " + version + "\nmaterialization:\n  mode: file\n")
			env.MustRunAM("install", "portable-probe", "--from", "./vendor/registry", "--scope", "repo")
			installed := filepath.Join(env.workspace, ".factory", "skills", "portable-probe", "SKILL.md")
			before, err := os.ReadFile(installed)
			if err != nil || !bytes.Contains(before, []byte("original inert content")) {
				t.Fatalf("CLI native portable control missing: %q %v", before, err)
			}
			installedSnapshot := captureDest(t, installed)
			manifest := filepath.Join(env.workspace, ".agents", "materialization-manifest.yaml")
			foreign := filepath.Join(env.home, "foreign-manifest.yaml")
			if err := os.Rename(manifest, foreign); err != nil {
				t.Fatal(err)
			}
			snapshot := captureDest(t, foreign)
			if err := os.Symlink(foreign, manifest); err != nil {
				t.Fatal(err)
			}
			env.WriteFile(source, "---\nname: portable-probe\ndescription: portable\n---\n\nchanged inert content\n")
			for _, args := range [][]string{{"sync", "--check"}, {"sync"}} {
				result := env.RunAM(args...)
				env.AssertExitCode(result, 1)
				if after, err := os.ReadFile(installed); err != nil || !bytes.Equal(before, after) {
					t.Fatalf("CLI manifest refusal changed native output: %q %v", after, err)
				}
				if target, err := os.Readlink(manifest); err != nil || target != foreign {
					t.Fatalf("CLI manifest refusal replaced alias: %q %v", target, err)
				}
				requireDestUntouched(t, installedSnapshot, captureDest(t, installed))
				requireDestUntouched(t, snapshot, captureDest(t, foreign))
			}
		})
	}
}
