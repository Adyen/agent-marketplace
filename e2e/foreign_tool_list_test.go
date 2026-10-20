package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

// foreignToolListSources are registries whose review skill and deploy
// command carry an allowed-tools list. Devin and Factory lists carry their
// own harness's provenance; the Claude list is canonical vocabulary. Every
// registry also ships an unrestricted plain skill as a positive control.
var foreignToolListSources = []struct {
	harness, list string
	foreign       bool
	files         map[string]string
}{
	{"devin", "read", true, map[string]string{
		".devin/skills/review/SKILL.md": "---\nname: review\ndescription: Review\nallowed-tools:\n  - read\n  - exec\n---\nInert.\n",
		".devin/skills/deploy/SKILL.md": "---\nname: deploy\ndescription: Deploy\nx-agent-marketplace-kind: command\nallowed-tools:\n  - read\n  - exec\n---\nInert.\n",
		".devin/skills/plain/SKILL.md":  "---\nname: plain\ndescription: Plain\n---\nInert.\n",
	}},
	{"factory", "Edit", true, map[string]string{
		".factory/skills/review/SKILL.md": "---\nname: review\ndescription: Review\nallowed-tools:\n  - Read\n  - Edit\n---\nInert.\n",
		".factory/commands/deploy.md":     "---\ndescription: Deploy\nallowed-tools:\n  - Read\n  - Edit\n---\nInert.\n",
		".factory/skills/plain/SKILL.md":  "---\nname: plain\ndescription: Plain\n---\nInert.\n",
	}},
	{"claude", "Read", false, map[string]string{
		"skills/review/SKILL.md": "---\nname: review\ndescription: Review\nallowed-tools:\n  - Read\n---\nInert.\n",
		"commands/deploy.md":     "---\ndescription: Deploy\nallowed-tools:\n  - Read\n---\nInert.\n",
		"skills/plain/SKILL.md":  "---\nname: plain\ndescription: Plain\n---\nInert.\n",
	}},
}

// Claude, Cursor and Gemini read allowed-tools as a pre-approval, so a list
// with another harness's provenance must never reach the skill or command
// that install and sync actually leave in their native layout, in either
// installation mode. A canonical list is installed unchanged. OpenCode cannot
// enforce any restriction, so it refuses every restricted skill and command
// while the unrestricted skill still installs.
// spec: harness-compilation/devin-and-factory-skill-tool-lists-are-omitted-on-pre-approval-targets
// spec: harness-compilation/devin-and-factory-command-tool-lists-are-omitted-on-pre-approval-targets
func TestForeignToolListsNeverBecomeInstalledPreApprovals(t *testing.T) {
	commandPaths := map[string]string{
		"claude":   ".claude/commands/deploy.md",
		"cursor":   ".cursor/commands/deploy.md",
		"gemini":   ".gemini/commands/deploy.toml",
		"opencode": ".opencode/commands/deploy.md",
	}
	for _, source := range foreignToolListSources {
		for _, target := range []string{"claude", "cursor", "gemini", "opencode"} {
			for _, mode := range []string{"symlink", "file"} {
				t.Run(source.harness+"->"+target+"/"+mode, func(t *testing.T) {
					t.Parallel()
					env := newEnv(t)
					env.InitGitRepo()
					env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [%s]\nmaterialization:\n  mode: %s\n", target, mode))
					url := env.CreateBareRepo(source.harness+"-tools", source.files)
					skillPath := func(name string) string { return "/workspace/." + target + "/skills/" + name + "/SKILL.md" }
					commandPath := "/workspace/" + commandPaths[target]

					env.MustRunAM("install", "plain", "--kind", "skill", "--from", url, "--scope", "repo")
					assertCompatibilityShape(t, env.mustResolvePath(skillPath("plain")), mode, true)
					env.AssertFileContains(skillPath("plain"), "name: plain")

					review := env.RunAM("install", "review", "--kind", "skill", "--from", url, "--scope", "repo")
					deploy := env.RunAM("install", "deploy", "--kind", "command", "--from", url, "--scope", "repo")
					if target == "opencode" {
						for _, refused := range []struct {
							result ExecResult
							code   string
						}{{review, "skill.opencode.security-restriction-unsupported"}, {deploy, "command.opencode.restriction-unsupported"}} {
							if refused.result.ExitCode == 0 || !strings.Contains(refused.result.Combined, refused.code) {
								t.Fatalf("OpenCode install of a restricted resource exited %d without %s:\n%s", refused.result.ExitCode, refused.code, refused.result.Combined)
							}
						}
						env.AssertFileAbsent(skillPath("review"))
						env.AssertFileAbsent(commandPath)
						env.AssertFileContains(skillPath("plain"), "name: plain")
						return
					}
					env.AssertExitCode(review, 0)
					env.AssertExitCode(deploy, 0)
					assertCompatibilityShape(t, env.mustResolvePath(skillPath("review")), mode, true)

					sync := env.MustRunAM("sync")
					if got := strings.Contains(sync.Combined, "skill.allowed-tools-omitted"); got != source.foreign {
						t.Fatalf("sync reported skill.allowed-tools-omitted = %t, want %t:\n%s", got, source.foreign, sync.Combined)
					}
					for _, installed := range []string{skillPath("review"), commandPath} {
						body := env.ReadFile(installed)
						if source.foreign && (strings.Contains(body, "allowed-tools") || strings.Contains(body, source.list)) {
							t.Fatalf("%s carries the %s tool list as a pre-approval:\n%s", installed, source.harness, body)
						}
						if !source.foreign && (!strings.Contains(body, "allowed-tools") || !strings.Contains(body, source.list)) {
							t.Fatalf("%s lost the canonical tool list:\n%s", installed, body)
						}
					}
					env.MustRunAM("sync", "--check")
				})
			}
		}
	}
}
