package e2e_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Devin CLI reads the repository's .devin/hooks.v1.json, while Devin Desktop
// (the Windsurf target) reads .devin/hooks.json and falls back to
// .windsurf/hooks.json only while .devin/hooks.json defines no hook. With
// both targets selected, each target must write only its own file, keep the
// handwritten handlers of every file, never claim another target's or a
// handwritten entry, and keep an active Windsurf fallback active by copying
// its handlers ahead of the generated one instead of silently shadowing it.
// spec: hook-resources/windsurf-repository-hooks-use-the-devin-desktop-workspace-file
// spec: hook-resources/handwritten-windsurf-fallback-hooks-remain-active
func TestHookInvariantDevinCLIAndDesktopFilesStaySeparate(t *testing.T) {
	const (
		managed     = "echo am-separation-managed"
		cli         = "echo am-separation-handwritten-cli"
		desktop     = "echo am-separation-handwritten-desktop"
		fallback    = "echo am-separation-handwritten-fallback"
		cliFile     = ".devin/hooks.v1.json"
		desktopFile = ".devin/hooks.json"
		fallbackRel = ".windsurf/hooks.json"
	)
	for _, tc := range []struct {
		name string
		seed map[string]string
		// wantDesktop is the ordered command list of the Desktop file after
		// the first sync.
		wantDesktop []string
	}{
		{
			name: "cli-and-desktop-files",
			seed: map[string]string{
				cliFile:     `{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"` + cli + `"}]}]}` + "\n",
				desktopFile: `{"hooks":{"post_write_code":[{"command":"` + desktop + `"}]}}` + "\n",
			},
			wantDesktop: []string{desktop, managed},
		},
		{
			name: "windsurf-fallback",
			seed: map[string]string{
				cliFile:     `{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"` + cli + `"}]}]}` + "\n",
				fallbackRel: `{"hooks":{"post_write_code":[{"command":"` + fallback + `"}]}}` + "\n",
			},
			wantDesktop: []string{fallback, managed},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			for rel, content := range tc.seed {
				e.WriteFile("/workspace/"+rel, content)
			}
			registry := hookInvariantRegistry(e, "separation", `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"`+managed+`"}]}]}}`)
			e.WriteRepoConfig(hookInvariantV4Config("devin, windsurf", registry, "hook/PreToolUse-command"))
			e.InitGitRepo()
			commands := func(rel string) []string {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(e.workspace, filepath.FromSlash(rel)))
				if os.IsNotExist(err) {
					return nil
				}
				if err != nil {
					t.Fatal(err)
				}
				return hookInvariantCommands(t, data)
			}
			fingerprint := func(rel string) string {
				t.Helper()
				got, err := pathFingerprint(filepath.Join(e.workspace, filepath.FromSlash(rel)))
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			assertCommands := func(phase, rel string, want []string) {
				t.Helper()
				got := commands(rel)
				slices.Sort(got)
				sorted := slices.Clone(want)
				slices.Sort(sorted)
				if !slices.Equal(got, sorted) {
					t.Fatalf("%s: %s commands = %q, want %q", phase, rel, got, sorted)
				}
			}

			e.MustRunAM("sync")
			assertCommands("sync", cliFile, []string{cli, managed})
			if got := commands(desktopFile); !slices.Equal(got, tc.wantDesktop) {
				t.Fatalf("sync: %s commands in order = %q, want %q", desktopFile, got, tc.wantDesktop)
			}
			if content, ok := tc.seed[fallbackRel]; ok {
				e.AssertFileContains("/workspace/"+fallbackRel, fallback)
				if got := e.ReadFile("/workspace/" + fallbackRel); got != content {
					t.Fatalf("sync rewrote the handwritten fallback:\n%s", got)
				}
			} else {
				e.AssertFileAbsent("/workspace/" + fallbackRel)
			}
			fallbackBefore := ""
			if _, ok := tc.seed[fallbackRel]; ok {
				fallbackBefore = fingerprint(fallbackRel)
			}
			cliBefore, desktopBefore := fingerprint(cliFile), fingerprint(desktopFile)
			e.MustRunAM("sync")
			e.MustRunAM("sync", "--check")
			if fingerprint(cliFile) != cliBefore || fingerprint(desktopFile) != desktopBefore {
				t.Fatal("repeat sync or check changed a Devin hook file")
			}

			// Deselecting the Windsurf target removes only its owned entry.
			e.WriteRepoConfig(hookInvariantV4Config("devin", registry, "hook/PreToolUse-command"))
			e.MustRunAM("sync")
			if fingerprint(cliFile) != cliBefore {
				t.Fatalf("deselecting Windsurf changed %s", cliFile)
			}
			wantDesktopHandwritten := tc.wantDesktop[:len(tc.wantDesktop)-1]
			assertCommands("windsurf deselection", desktopFile, wantDesktopHandwritten)
			desktopAfter := fingerprint(desktopFile)

			// Deselecting the hook removes only Devin CLI's owned entry.
			e.WriteRepoConfig("apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
			e.MustRunAM("sync")
			assertCommands("hook deselection", cliFile, []string{cli})
			if fingerprint(desktopFile) != desktopAfter {
				t.Fatalf("deselecting the Devin CLI hook changed %s", desktopFile)
			}
			if fallbackBefore != "" && fingerprint(fallbackRel) != fallbackBefore {
				t.Fatal("deselection changed the handwritten fallback")
			}
		})
	}
}
