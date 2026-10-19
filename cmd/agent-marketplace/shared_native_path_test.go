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
	// Write order follows snapshot paths, which hash each source and commit,
	// so it varies between runs. The projection must not depend on it: two
	// source-name pairs per materialization mode sample it, and the resolver
	// tests cover both input orders.
	for _, order := range []struct{ name, org, user string }{
		{"org-a-user-z", "a-org.git", "z-user.git"},
		{"org-z-user-a", "z-org.git", "a-user.git"},
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

// A shared path that changes owner in either direction leaves no residue of
// the previous resource and stays in sync.
func TestSyncSharedNativePathChangesOwnerCleanly(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, mode := range []string{"symlink", "file"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			orgOrigin, orgWorking := createBareRepoWithWorkingClone(t, "org.git")
			writeSkill(t, orgWorking, "audit", "Organization required skill", nil)
			writeFile(t, filepath.Join(orgWorking, "skills", "audit", "reference.md"), "org reference\n")
			commitAndPushAll(t, orgWorking, "org")
			userOrigin, userWorking := createBareRepoWithWorkingClone(t, "user.git")
			writeCommand(t, userWorking, "audit", "User selected command", "echo user")
			commitAndPushAll(t, userWorking, "user")
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin, codex]\nmaterialization:\n  mode: "+mode+"\nsources:\n  user:\n    git: file://"+userOrigin+"\n    ref: main\nresources:\n  user:\n    include: [command/audit]\n")
			repo := filepath.Join(t.TempDir(), "repo")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, repo)
			if out, err := executeCommand("sync"); err != nil {
				t.Fatalf("command-only sync: %v\n%s", err, out)
			}
			devin := filepath.Join(home, ".config", "devin", "skills", "audit")
			if data, err := os.ReadFile(filepath.Join(devin, "SKILL.md")); err != nil || !strings.Contains(string(data), "User selected command") {
				t.Fatalf("control: command not installed first: %v\n%s", err, data)
			}
			orgPath := filepath.Join(t.TempDir(), "org.yaml")
			writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin, codex]\nsources:\n  org:\n    git: file://"+orgOrigin+"\n    ref: main\nresources:\n  org:\n    all: true\n    required: true\n")
			config.SetTestOrganizationConfigPath(t, orgPath)
			if out, err := executeCommand("lock", "--scope", "organization"); err != nil {
				t.Fatalf("lock: %v\n%s", err, out)
			}
			if out, err := executeCommand("sync"); err != nil {
				t.Fatalf("transition sync: %v\n%s", err, out)
			}
			for _, dir := range []string{devin, filepath.Join(home, ".agents", "skills", "audit")} {
				data, readErr := os.ReadFile(filepath.Join(dir, "SKILL.md"))
				ref, refErr := os.ReadFile(filepath.Join(dir, "reference.md"))
				if readErr != nil || !strings.Contains(string(data), "Organization required skill") || refErr != nil || string(ref) != "org reference\n" {
					t.Errorf("%s after transition: %v %v\n%s", dir, readErr, refErr, data)
				}
			}
			if out, err := executeCommand("sync", "--check"); err != nil {
				t.Errorf("sync --check: %v\n%s", err, out)
			}
			// Back again: drop the organization requirement; the command returns.
			config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "absent.yaml"))
			if out, err := executeCommand("sync"); err != nil {
				t.Fatalf("reverse sync: %v\n%s", err, out)
			}
			data, readErr := os.ReadFile(filepath.Join(devin, "SKILL.md"))
			if readErr != nil || !strings.Contains(string(data), "User selected command") {
				t.Errorf("command not restored after the requirement is gone: %v\n%s", readErr, data)
			}
			if _, statErr := os.Lstat(filepath.Join(devin, "reference.md")); !os.IsNotExist(statErr) {
				t.Errorf("skill residue reference.md left in the command's directory: %v", statErr)
			}
			if out, err := executeCommand("sync", "--check"); err != nil {
				t.Errorf("sync --check after reverse: %v\n%s", err, out)
			}
		})
	}
}

// spec: resource-selection/required-skill-keeps-a-native-path-a-case-variant-command-shares
func TestSyncRequiredSkillKeepsPathACaseVariantCommandShares(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, mode := range []string{"symlink", "file"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			orgOrigin, orgWorking := createBareRepoWithWorkingClone(t, "org.git")
			writeSkill(t, orgWorking, "audit", "Organization required skill", nil)
			commitAndPushAll(t, orgWorking, "organization skill")
			// OpenCode names a command after its file, and resource names may
			// contain upper-case letters.
			userOrigin, userWorking := createBareRepoWithWorkingClone(t, "user.git")
			writeFile(t, filepath.Join(userWorking, ".opencode", "commands", "Audit.md"), "---\ndescription: User selected command\n---\nRun the user command.\n")
			commitAndPushAll(t, userWorking, "user command")
			orgPath := filepath.Join(t.TempDir(), "org.yaml")
			writeConfig(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [devin, codex, claude]\nsources:\n  org:\n    git: file://"+orgOrigin+"\n    ref: main\nresources:\n  org:\n    all: true\n    required: true\n")
			config.SetTestOrganizationConfigPath(t, orgPath)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin, codex, claude]\nmaterialization:\n  mode: "+mode+"\nsources:\n  user:\n    git: file://"+userOrigin+"\n    ref: main\n    source_harness: opencode\nresources:\n  user:\n    include: [command/Audit]\n")
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
			for _, root := range []string{filepath.Join(home, ".config", "devin", "skills"), filepath.Join(home, ".agents", "skills")} {
				data, readErr := os.ReadFile(filepath.Join(root, "audit", "SKILL.md"))
				if readErr != nil || !strings.Contains(string(data), "Organization required skill") {
					t.Errorf("%s/audit does not hold the required skill: %v\n%s", root, readErr, data)
				}
				// On a case-sensitive host a separate Audit entry would survive
				// here and replace the skill on case-insensitive ones.
				entries, readErr := os.ReadDir(root)
				if readErr != nil {
					t.Fatal(readErr)
				}
				for _, entry := range entries {
					if entry.Name() == "Audit" {
						t.Errorf("%s/Audit was installed next to the required skill", root)
					}
				}
			}
			if !strings.Contains(output, "materialize.collision.shared-path") || !strings.Contains(output, `command "Audit"`) {
				t.Errorf("sync did not report the displaced command:\n%s", output)
			}
			command, readErr := os.ReadFile(filepath.Join(home, ".claude", "commands", "Audit.md"))
			if readErr != nil || !strings.Contains(string(command), "Run the user command.") {
				t.Errorf("Claude Code must keep the command: %v\n%s", readErr, command)
			}
			if output, err := executeCommand("sync", "--check"); err != nil {
				t.Errorf("sync --check after sync: %v\n%s", err, output)
			}
		})
	}
}
