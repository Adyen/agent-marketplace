package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestE2EOwnedAggregatesRejectUnsafeLeaves(t *testing.T) {
	for _, target := range []struct{ name, mcp, hook, code string }{
		{"devin", ".devin/mcp_config.json", ".devin/hooks.v1.json", "materialize.devin.aggregate-write"},
		{"codex", ".codex/config.toml", ".codex/hooks.json", "materialize.codex.aggregate-malformed"},
	} {
		for _, leaf := range []struct{ name, path, sibling string }{{"mcp", target.mcp, target.hook}, {"hook", target.hook, target.mcp}} {
			for _, kind := range []string{"fifo", "fifo-link", "regular-link", "dangling-link", "hard-link"} {
				t.Run(target.name+"/"+leaf.name+"/"+kind, func(t *testing.T) {
					t.Parallel()
					env := newEnv(t)
					env.InitGitRepo()
					files := RegistryFilesEx(nil, nil, nil, []MCPServer{{Name: "fixture", Command: "fixture-server"}})
					files["hooks/hooks.json"] = `{"hooks":{"PostToolUse":[{"type":"command","command":"echo fixture-hook","matcher":"Bash"}]}}`
					url := env.CreateBareRepo("aggregate-read", files)
					env.WriteGlobalConfig(fmt.Sprintf("harnesses: [%s]\nregistries: []\n", target.name))
					env.WriteRepoConfig(fmt.Sprintf("registries:\n  - url: %s\n    ref: main\n    source_harness: claude\n    mcp: [fixture]\n    hooks: [PostToolUse-command]\n", url))
					env.MustRunAM("sync")
					env.MustRunAM("sync", "--check")
					env.AssertFileContains("/workspace/"+target.mcp, "fixture-server")
					env.AssertFileContains("/workspace/"+target.hook, "fixture-hook")
					path := filepath.Join(env.workspace, filepath.FromSlash(leaf.path))
					saved := path + ".saved"
					if err := os.Rename(path, saved); err != nil {
						t.Fatal(err)
					}
					var err error
					switch kind {
					case "fifo":
						err = syscall.Mkfifo(path, 0600)
					case "fifo-link":
						fifo := filepath.Join(t.TempDir(), "fifo")
						if err = syscall.Mkfifo(fifo, 0600); err == nil {
							err = os.Symlink(fifo, path)
						}
					case "regular-link":
						err = os.Symlink(saved, path)
					case "dangling-link":
						err = os.Symlink(filepath.Join(t.TempDir(), "absent"), path)
					case "hard-link":
						err = os.Link(saved, path)
					}
					if err != nil {
						t.Fatal(err)
					}
					info, err := os.Lstat(path)
					if err != nil {
						t.Fatal(err)
					}
					link, _ := os.Readlink(path)
					entries := func() []string {
						items, err := os.ReadDir(filepath.Dir(path))
						if err != nil {
							t.Fatal(err)
						}
						names := make([]string, 0, len(items))
						for _, item := range items {
							names = append(names, item.Name())
						}
						return names
					}
					beforeEntries := entries()
					state := filepath.Join(env.home, ".local", "state", "agent-marketplace")
					protected := []string{saved, filepath.Join(env.workspace, leaf.sibling), filepath.Join(env.workspace, ".agents", "config.yaml"), filepath.Join(env.workspace, ".agents", "lock.yaml"), filepath.Join(state, "ownership"), filepath.Join(state, "safety-backups")}
					before := make(map[string]string, len(protected))
					for _, p := range protected {
						if _, err := os.Stat(p); err != nil {
							t.Fatalf("preservation fixture is absent: %s: %v", p, err)
						}
						fingerprint, err := pathFingerprint(p)
						if err != nil {
							t.Fatal(err)
						}
						before[p] = fingerprint
					}
					for _, args := range [][]string{{"sync", "--check"}, {"status"}, {"doctor", "--repair"}, {"sync"}} {
						ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
						cmd := exec.CommandContext(ctx, env.binary, args...)
						cmd.Dir, cmd.Env = env.workspace, env.subprocessEnv()
						output, runErr := cmd.CombinedOutput()
						deadlineErr := ctx.Err()
						cancel()
						if deadlineErr != nil {
							t.Fatalf("%v blocked: %v output=%s", args, deadlineErr, output)
						}
						if args[0] == "status" {
							if runErr == nil || !strings.Contains(string(output), "DRIFT") || strings.Contains(string(output), "\nIn sync\n") || !strings.Contains(string(output), target.code) {
								t.Fatalf("status did not complete its drift probe: %v output=%s", runErr, output)
							}
						} else if runErr == nil || !strings.Contains(string(output), target.code) {
							t.Fatalf("%v error=%v output=%s", args, runErr, output)
						}
						if !slices.Equal(entries(), beforeEntries) {
							t.Fatalf("%v left target-directory residue", args)
						}
						after, err := os.Lstat(path)
						if err != nil || !os.SameFile(info, after) || after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
							t.Fatalf("unsafe leaf changed: %v", err)
						}
						if got, _ := os.Readlink(path); got != link {
							t.Fatalf("symlink changed: %q != %q", got, link)
						}
						for _, p := range protected {
							got, err := pathFingerprint(p)
							if err != nil || got != before[p] {
								t.Fatalf("%v changed protected path %s: %v", args, p, err)
							}
						}
					}
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(saved, path); err != nil {
						t.Fatal(err)
					}
					env.MustRunAM("sync", "--check")
					beforeSync, err := pathFingerprint(filepath.Dir(path))
					if err != nil {
						t.Fatal(err)
					}
					env.MustRunAM("sync")
					afterSync, err := pathFingerprint(filepath.Dir(path))
					if err != nil || beforeSync != afterSync {
						t.Fatalf("regular control was not a no-op: %v", err)
					}
				})
			}
		}
	}
}
