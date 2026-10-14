package e2e_test

import (
	"strings"
	"testing"
)

// The old migration flag matrix is now evidence of the absence of that public
// surface, not operational command coverage. Unsupported bytes remain authored.
func TestConsumerMigrationCommandIsAbsent(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteFile("/root/.agents/config.yaml", "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n")
	env.WriteFile("/workspace/.agents/config.yaml", "apiVersion: agent-marketplace/v3\nregistries: []\n")
	before := snapshotConsumerEvidence(t, env)
	for _, args := range [][]string{
		{"migrate"}, {"migrate", "--to", "v2"}, {"migrate", "--to", "v4"},
		{"migrate", "--scope", "all", "--dry-run", "--harnesses", "cursor"},
	} {
		result := env.RunAM(args...)
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "unknown command") {
			t.Fatalf("retired migration surface %v still works:\n%s", args, result.Combined)
		}
		assertConsumerEvidence(t, env, before)
	}
	for _, args := range [][]string{{"help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}} {
		result := env.MustRunAM(args...)
		if strings.Contains(result.Combined, "migrate") {
			t.Fatalf("help/completion %v exposes consumer migration:\n%s", args, result.Combined)
		}
	}
}
