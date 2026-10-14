package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemHookSupportSourceAdmission(t *testing.T) {
	for _, fixture := range []struct{ name, root string }{
		{"ad-hoc", "apiVersion: agent-marketplace/v4\n"},
		{"configured", "apiVersion: agent-marketplace/v4\nsources: {local: {directory: ./vendor/registry}}\nresources: {local: {include: [hook/PostToolUse-command]}}\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			source := filepath.Join(env.workspace, "vendor", "registry", "hooks")
			env.WriteFile(filepath.Join(source, "hooks.json"), `{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"inert original hook"}]}]}}`)
			env.WriteFile(filepath.Join(source, "helper.txt"), "inert CLI hook support")
			if err := os.Chmod(filepath.Join(source, "helper.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("helper.txt", filepath.Join(source, "alias.txt")); err != nil {
				t.Fatal(err)
			}
			env.WriteRepoConfig(fixture.root)
			env.MustRunAM("install", "PostToolUse-command", "--kind", "hook", "--from", "./vendor/registry", "--scope", "repo")
			installed := filepath.Join(env.workspace, ".factory", "hooks")
			env.AssertFileContains(filepath.Join(installed, "alias.txt"), "inert CLI hook support")
			info, err := os.Stat(filepath.Join(installed, "alias.txt"))
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("native CLI hook support mode changed: %v", err)
			}
			before, err := os.ReadFile(filepath.Join(installed, "hooks.json"))
			if err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(env.home, "foreign-inert.txt")
			env.WriteFile(external, "foreign inert bytes")
			snapshot := captureDest(t, external)
			if err := os.Symlink(external, filepath.Join(source, "bad.txt")); err != nil {
				t.Fatal(err)
			}
			env.WriteFile(filepath.Join(source, "hooks.json"), `{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"inert changed hook"}]}]}}`)
			refused := env.RunAM("sync")
			env.AssertExitCode(refused, 1)
			if data, err := os.ReadFile(filepath.Join(installed, "hooks.json")); err != nil || !bytes.Equal(before, data) {
				t.Fatalf("refused CLI hook source changed native hooks: %q %v", data, err)
			}
			env.AssertFileContains(filepath.Join(installed, "alias.txt"), "inert CLI hook support")
			env.AssertFileAbsent(filepath.Join(installed, "bad.txt"))
			requireDestUntouched(t, snapshot, captureDest(t, external))
		})
	}
}
