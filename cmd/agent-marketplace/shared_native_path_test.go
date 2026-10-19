package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// spec: resource-selection/required-skill-keeps-a-native-path-a-same-name-command-shares
func TestSyncRequiredSkillKeepsSharedNativePath(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	// The cache path of each source decides which shared-path write lands
	// last, so cover both orders as well as both materialization modes.
	for _, order := range []struct{ name, org, user string }{
		{"organization-sorts-first", "a-org.git", "z-user.git"},
		{"user-sorts-first", "z-org.git", "a-user.git"},
	} {
		for _, mode := range []string{"symlink", "file"} {
			t.Run(order.name+"/"+mode, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				orgOrigin, orgWorking := createBareRepoWithWorkingClone(t, order.org)
				writeSkill(t, orgWorking, "audit", "Organization required skill", nil)
				commitAndPushAll(t, orgWorking, "organization skill")
				userOrigin, userWorking := createBareRepoWithWorkingClone(t, order.user)
				writeCommand(t, userWorking, "audit", "User selected command", "echo user")
				commitAndPushAll(t, userWorking, "user command")

				orgPath := filepath.Join(t.TempDir(), "org.yaml")
				writeConfig(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses: [devin, codex, claude]
sources:
  org:
    git: file://`+orgOrigin+`
    ref: main
    source_harness: claude
resources:
  org:
    all: true
    required: true
`)
				config.SetTestOrganizationConfigPath(t, orgPath)
				writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [devin, codex, claude]
materialization:
  mode: `+mode+`
sources:
  user:
    git: file://`+userOrigin+`
    ref: main
    source_harness: claude
resources:
  user:
    include: [command/audit]
`)
				repoRoot := filepath.Join(t.TempDir(), "repo")
				if err := os.MkdirAll(repoRoot, 0o755); err != nil {
					t.Fatal(err)
				}
				withWorkingDir(t, repoRoot)
				if output, err := executeCommand("lock", "--scope", "organization"); err != nil {
					t.Fatalf("seed organization lock: %v\n%s", err, output)
				}

				output, err := executeCommand("sync")
				if err != nil {
					t.Fatalf("sync: %v\n%s", err, output)
				}
				for _, shared := range []string{
					filepath.Join(home, ".config", "devin", "skills", "audit", "SKILL.md"),
					filepath.Join(home, ".agents", "skills", "audit", "SKILL.md"),
				} {
					data, readErr := os.ReadFile(shared)
					if readErr != nil || !strings.Contains(string(data), "Organization required skill") {
						t.Errorf("%s does not hold the required skill: %v\n%s\nsync output:\n%s", shared, readErr, data, output)
					}
				}
				if !strings.Contains(output, "materialize.collision.shared-path") || !strings.Contains(output, `command "audit"`) || !strings.Contains(output, `skill "audit"`) {
					t.Errorf("sync did not report the displaced command:\n%s", output)
				}
				// Claude keeps skills and commands apart, so both install there.
				claudeSkill, skillErr := os.ReadFile(filepath.Join(home, ".claude", "skills", "audit", "SKILL.md"))
				claudeCommand, commandErr := os.ReadFile(filepath.Join(home, ".claude", "commands", "audit.md"))
				if skillErr != nil || commandErr != nil || !strings.Contains(string(claudeSkill), "Organization required skill") || !strings.Contains(string(claudeCommand), "User selected command") {
					t.Errorf("Claude must keep both resources: skill=%v command=%v\n%s\n%s", skillErr, commandErr, claudeSkill, claudeCommand)
				}
				if output, err := executeCommand("sync", "--check"); err != nil {
					t.Errorf("sync --check after sync: %v\n%s", err, output)
				}
			})
		}
	}
}
