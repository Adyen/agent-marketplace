package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: claude-native-configuration/configuration-directory-inside-or-around-the-repository-root-refuses
// spec: claude-native-configuration/configuration-directory-that-aliases-the-repository-root-refuses
func TestClaudeConfigurationOverlap(t *testing.T) {
	for _, layout := range []string{"equal", "ancestor", "child", "case", "unicode", "alias", "ancestor-alias"} {
		for _, existing := range []bool{false, true} {
			t.Run(layout+map[bool]string{false: "/missing", true: "/existing"}[existing], func(t *testing.T) {
				_, repo, _ := claudeDirectoryFixture(t)
				custom := filepath.Join(repo, ".claude")
				switch layout {
				case "ancestor":
					custom = repo
				case "child":
					custom = filepath.Join(custom, "nested")
				case "case":
					custom = filepath.Join(repo, ".CLAUDE")
				case "unicode":
					parent := t.TempDir()
					repo = filepath.Join(parent, "cafe\u0301")
					if err := os.Mkdir(repo, 0o700); err != nil {
						t.Fatal(err)
					}
					custom = filepath.Join(parent, "caf\u00e9", ".claude")
				case "alias":
					alias := filepath.Join(t.TempDir(), "alias")
					if err := os.Symlink(repo, alias); err != nil {
						t.Fatal(err)
					}
					custom = filepath.Join(alias, ".claude")
				case "ancestor-alias":
					custom = filepath.Join(t.TempDir(), "alias")
					if err := os.Symlink(repo, custom); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("CLAUDE_CONFIG_DIR", custom)
				if existing {
					claudeDirectoryWrite(t, filepath.Join(repo, ".claude", "sentinel.json"), `{"stale":{"_managed_by":"agent-marketplace"}}`)
				}
				resources := []resolver.ResolvedArtifact{
					claudeDirectorySkill("project", config.RepoCommitted, hcc.TargetClaude),
					claudeDirectorySkill("machine", config.UserPersonal, hcc.TargetClaude),
					claudeDirectorySkill("safe", config.RepoCommitted, hcc.TargetFactory),
				}
				for _, dry := range []bool{true, false} {
					var options []RunOption
					if dry {
						options = append(options, WithDryRun())
					}
					report, err := Run(context.Background(), repo, resources, options...)
					if err != nil || !hasDiagCode(report.Diags, diag.CodeMaterializeDestinationOverlap) {
						t.Fatalf("overlap: %v, %+v", err, report)
					}
					for _, dir := range []string{filepath.Join(repo, ".claude", "skills"), filepath.Join(custom, "skills")} {
						if _, err := os.Lstat(dir); !os.IsNotExist(err) {
							t.Fatalf("refusal wrote %s: %v", dir, err)
						}
					}
					if existing && string(claudeDirectoryRead(t, filepath.Join(repo, ".claude", "sentinel.json"))) != `{"stale":{"_managed_by":"agent-marketplace"}}` {
						t.Fatal("refusal pruned existing output")
					}
				}
				claudeDirectoryRead(t, filepath.Join(repo, ".factory/skills/safe/SKILL.md"))
			})
		}
	}
}

// spec: claude-native-configuration/relative-value-refuses-machine-scope-work-before-any-write
// spec: claude-native-configuration/relative-value-does-not-affect-repository-only-output
// spec: claude-native-configuration/repository-scope-ignores-the-variable
// spec: claude-native-configuration/disjoint-configuration-directory-supports-both-scopes
func TestClaudeConfigurationScopes(t *testing.T) {
	for _, customValue := range []string{"relative", "external", "inside-disjoint"} {
		for _, machine := range []bool{false, true} {
			t.Run(customValue+map[bool]string{false: "/repo", true: "/both"}[machine], func(t *testing.T) {
				home, repo, custom := claudeDirectoryFixture(t)
				switch customValue {
				case "relative":
					custom = "relative"
					claudeDirectoryWrite(t, filepath.Join(home, ".claude", "sentinel.json"), `{"untouched":true}`)
				case "inside-disjoint":
					custom = filepath.Join(repo, "account")
				}
				t.Setenv("CLAUDE_CONFIG_DIR", custom)
				resources := append(nativeMCPServerResources(t, hcc.TargetClaude, config.RepoCommitted),
					claudeDirectorySkill("project", config.RepoCommitted, hcc.TargetClaude),
					claudeDirectorySkill("safe", config.RepoCommitted, hcc.TargetFactory))
				if machine {
					resources = append(resources, claudeDirectorySkill("machine", config.UserPersonal, hcc.TargetClaude))
				}
				report, err := Run(context.Background(), repo, resources)
				if err != nil {
					t.Fatal(err)
				}
				if customValue == "relative" && machine {
					if !hasDiagCode(report.Diags, "materialize.destination.error") {
						t.Fatalf("relative root accepted: %+v", report)
					}
				} else if diag.HasErrors(report.Diags) {
					t.Fatalf("scope resolution: %+v", report)
				}
				claudeDirectoryRead(t, filepath.Join(repo, ".claude/skills/project/SKILL.md"))
				claudeDirectoryRead(t, filepath.Join(repo, ".factory/skills/safe/SKILL.md"))
				claudeDirectoryRead(t, filepath.Join(repo, ".mcp.json"))
				if machine && customValue != "relative" {
					claudeDirectoryRead(t, filepath.Join(custom, "skills/machine/SKILL.md"))
				} else if _, err := os.Stat(custom); !os.IsNotExist(err) {
					t.Fatalf("unused root created: %s: %v", custom, err)
				}
				if customValue == "relative" {
					if string(claudeDirectoryRead(t, filepath.Join(home, ".claude", "sentinel.json"))) != `{"untouched":true}` {
						t.Fatal("relative root refusal changed default account")
					}
				} else if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
					t.Fatalf("default root created: %v", err)
				}
			})
		}
	}
}

// spec: materialization/unsafe-relocated-claude-code-machine-root-is-refused
func TestClaudeConfigurationUnsafeRoot(t *testing.T) {
	for _, shape := range []string{"file", "dangling", "cycle"} {
		t.Run(shape, func(t *testing.T) {
			_, repo, custom := claudeDirectoryFixture(t)
			if shape == "file" {
				claudeDirectoryWrite(t, custom, "do not replace")
			} else {
				target := custom
				if shape == "dangling" {
					target += "-missing"
				}
				if err := os.Symlink(target, custom); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(custom)
			if err != nil {
				t.Fatal(err)
			}
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{claudeDirectorySkill("machine", config.UserPersonal, hcc.TargetClaude)})
			if err != nil || !hasDiagCode(report.Diags, "materialize.destination.error") {
				t.Fatalf("unsafe root accepted: %v, %+v", err, report)
			}
			after, err := os.Lstat(custom)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("unsafe root changed: %v", err)
			}
		})
	}
}

// spec: claude-native-configuration/default-location-output-is-left-in-place
func TestClaudeConfigurationDefaultOutputPreserved(t *testing.T) {
	home, repo, custom := claudeDirectoryFixture(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	resources := append(nativeMCPServerResources(t, hcc.TargetClaude, config.UserPersonal),
		claudeDirectorySkill("old", config.UserPersonal, hcc.TargetClaude))
	before, err := Run(context.Background(), repo, resources)
	if err != nil || diag.HasErrors(before.Diags) {
		t.Fatalf("default install: %v, %+v", err, before)
	}
	mcp := claudeDirectoryRead(t, filepath.Join(home, ".claude.json"))
	skill := claudeDirectoryRead(t, filepath.Join(home, ".claude/skills/old/SKILL.md"))
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	report, err := Run(context.Background(), repo, resources)
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("relocation: %v, %+v", err, report)
	}
	report, err = Run(context.Background(), repo, resources, WithDryRun())
	if err != nil || !report.InSync() {
		t.Fatalf("relocated check: %v, %+v", err, report)
	}
	if !bytes.Equal(mcp, claudeDirectoryRead(t, filepath.Join(home, ".claude.json"))) || !bytes.Equal(skill, claudeDirectoryRead(t, filepath.Join(home, ".claude/skills/old/SKILL.md"))) {
		t.Fatal("old default output changed")
	}
}

// spec: claude-native-configuration/default-location-output-is-left-in-place
func TestClaudeConfigurationAncestorPreservesDefaultAccount(t *testing.T) {
	for _, location := range []string{"home", "parent", "alias"} {
		t.Run(location, func(t *testing.T) {
			home, repo, _ := claudeDirectoryFixture(t)
			oldLegacy := filepath.Join(home, ".claude", ".mcp.json")
			original := `{"mcpServers":{"old":{"_managed_by":"agent-marketplace","command":"keep"}}}`
			claudeDirectoryWrite(t, oldLegacy, original)
			oldNative := filepath.Join(home, ".claude.json")
			claudeDirectoryWrite(t, oldNative, original)
			custom := home
			switch location {
			case "parent":
				custom = filepath.Dir(home)
			case "alias":
				custom = filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(home, custom); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CLAUDE_CONFIG_DIR", custom)
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
				claudeDirectorySkill("new-account", config.UserPersonal, hcc.TargetClaude),
			})
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("relocated sync: %v, %+v", err, report)
			}
			claudeDirectoryRead(t, filepath.Join(custom, "skills/new-account/SKILL.md"))
			if string(claudeDirectoryRead(t, oldLegacy)) != original {
				t.Fatal("relocated root pruned the inactive default account")
			}
			if location == "parent" && string(claudeDirectoryRead(t, oldNative)) != original {
				t.Fatal("relocated root pruned the inactive default configuration")
			}
		})
	}
}
