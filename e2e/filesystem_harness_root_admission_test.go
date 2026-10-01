package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemHarnessRootAdmission(t *testing.T) {
	for _, tc := range []struct{ target, root, artifact string }{
		{"claude", ".claude", "skills/root-probe/SKILL.md"},
		{"factory", ".factory", "skills/root-probe/SKILL.md"},
		{"cursor", ".cursor", "skills/root-probe/SKILL.md"},
		{"windsurf", ".windsurf", "skills/root-probe/SKILL.md"},
		{"gemini", ".gemini", "skills/root-probe/SKILL.md"},
		{"devin", ".devin", "skills/root-probe/SKILL.md"},
		{"codex", ".codex", "agents/root-agent.toml"},
	} {
		for _, mode := range []string{"symlink", "file"} {
			t.Run(tc.target+"/"+mode, func(t *testing.T) {
				env := newEnv(t)
				registry := env.CreateBareRepo("root-boundary", RegistryFilesEx([]Skill{{Name: "root-probe", Description: "Root boundary control"}}, nil, []Subagent{{Name: "root-agent", Description: "Root boundary subagent"}}, nil))
				env.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [%s]\nmaterialization: {mode: %s}\n", tc.target, mode))
				env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [%s]\nmaterialization: {mode: %s}\nregistries:\n  - url: %s\n    ref: main\n    source_harness: claude\n    skills: {include: [root-probe]}\n    subagents: {include: [root-agent]}\n", tc.target, mode, registry))
				env.MustRunAM("sync")
				root := filepath.Join(env.workspace, tc.root)
				if _, err := os.Stat(filepath.Join(root, tc.artifact)); err != nil {
					t.Fatalf("native positive control missing: %v", err)
				}
				contained := filepath.Join(env.workspace, "native-root")
				if err := os.Rename(root, contained); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("native-root", root); err != nil {
					t.Fatal(err)
				}
				env.MustRunAM("sync")
				env.MustRunAM("sync", "--check")
				if text, err := os.Readlink(root); err != nil || text != "native-root" {
					t.Fatalf("contained native alias changed: %q %v", text, err)
				}
				if err := os.Remove(root); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(env.home, "external-root")
				if err := os.Mkdir(outside, 0o755); err != nil {
					t.Fatal(err)
				}
				victim := filepath.Join(outside, "sentinel")
				env.WriteFile(victim, "preserve external bytes\n")
				before := captureDest(t, victim)
				if err := os.Symlink(outside, root); err != nil {
					t.Fatal(err)
				}
				linkBefore, err := os.Lstat(root)
				if err != nil {
					t.Fatal(err)
				}
				result := env.RunAM("sync")
				env.AssertExitCode(result, 1)
				requireDestUntouched(t, before, captureDest(t, victim))
				if after, err := os.Lstat(root); err != nil || !os.SameFile(linkBefore, after) {
					t.Fatalf("unsafe root replaced: %v", err)
				}
				if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
					t.Fatalf("external output/residue created: %v %v", entries, err)
				}
			})
		}
	}
}
