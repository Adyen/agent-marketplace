package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemPortableManifestPublicationKeepsNoopAndMode(t *testing.T) {
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v4"} {
		t.Run(version, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			source := filepath.Join(env.workspace, "vendor", "registry", "skills", "portable-probe", "SKILL.md")
			env.WriteFile(source, "---\nname: portable-probe\ndescription: portable\n---\n\noriginal inert content\n")
			env.WriteRepoConfig("apiVersion: " + version + "\nmaterialization:\n  mode: file\n")
			env.MustRunAM("install", "portable-probe", "--from", "./vendor/registry", "--scope", "repo")
			manifest := filepath.Join(env.workspace, ".agents", "materialization-manifest.yaml")
			if err := os.Chmod(manifest, 0o640); err != nil {
				t.Fatal(err)
			}
			before := captureDest(t, manifest)
			env.MustRunAM("sync", "--check")
			env.MustRunAM("sync")
			requireDestUntouched(t, before, captureDest(t, manifest))
			env.WriteFile(source, "---\nname: portable-probe\ndescription: portable\n---\n\nupdated inert content\n")
			env.MustRunAM("sync")
			if info, err := os.Stat(manifest); err != nil || info.Mode().Perm() != 0o640 {
				t.Fatalf("CLI manifest update lost mode: %v", err)
			}
			installed := filepath.Join(env.workspace, ".factory", "skills", "portable-probe", "SKILL.md")
			if data, err := os.ReadFile(installed); err != nil || !bytes.Contains(data, []byte("updated inert content")) {
				t.Fatalf("CLI native update did not occur: %q %v", data, err)
			}
			updated := captureDest(t, manifest)
			env.MustRunAM("sync")
			requireDestUntouched(t, updated, captureDest(t, manifest))
			env.MustRunAM("remove", "portable-probe", "--scope", "repo")
			if _, err := os.Lstat(manifest); !os.IsNotExist(err) {
				t.Fatalf("CLI did not remove empty manifest: %v", err)
			}
		})
	}
}
