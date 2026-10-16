package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemSkillSupportSourceAdmission(t *testing.T) {
	for _, fixture := range []struct{ name, root string }{
		{"ad-hoc", "apiVersion: agent-marketplace/v4\n"},
		{"configured", "apiVersion: agent-marketplace/v4\nsources: {local: {directory: ./vendor/registry}}\nresources: {local: {include: [skill/support-probe]}}\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			skill := filepath.Join(env.workspace, "vendor", "registry", "skills", "support-probe")
			env.WriteFile(filepath.Join(skill, "SKILL.md"), "---\nname: support-probe\ndescription: Support source control\n---\nOriginal body\n")
			env.WriteFile(filepath.Join(skill, "helper.txt"), "inert support bytes")
			if err := os.Chmod(filepath.Join(skill, "helper.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("helper.txt", filepath.Join(skill, "reference.txt")); err != nil {
				t.Fatal(err)
			}
			env.WriteRepoConfig(fixture.root)
			env.MustRunAM("install", "support-probe", "--from", "./vendor/registry", "--scope", "repo")
			installed := filepath.Join(env.workspace, ".factory", "skills", "support-probe")
			env.AssertFileContains(filepath.Join(installed, "reference.txt"), "inert support bytes")
			info, err := os.Stat(filepath.Join(installed, "reference.txt"))
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("native support mode changed: %v", err)
			}
			before, err := os.ReadFile(filepath.Join(installed, "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(env.home, "external-inert.txt")
			env.WriteFile(external, "foreign inert bytes")
			snapshot := captureDest(t, external)
			if err := os.Symlink(external, filepath.Join(skill, "bad.txt")); err != nil {
				t.Fatal(err)
			}
			env.WriteFile(filepath.Join(skill, "SKILL.md"), "---\nname: support-probe\ndescription: Support source control\n---\nChanged body\n")
			refused := env.RunAM("sync")
			env.AssertExitCode(refused, 1)
			if after, err := os.ReadFile(filepath.Join(installed, "SKILL.md")); err != nil || !bytes.Equal(before, after) {
				t.Fatalf("refused support source changed installed compiled bytes: %q %v", after, err)
			}
			env.AssertFileContains(filepath.Join(installed, "reference.txt"), "inert support bytes")
			env.AssertFileAbsent(filepath.Join(installed, "bad.txt"))
			requireDestUntouched(t, snapshot, captureDest(t, external))
		})
	}
}
