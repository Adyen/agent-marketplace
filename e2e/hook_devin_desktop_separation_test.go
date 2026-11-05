package e2e_test

import (
	"encoding/json"
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
		// wantDesktop is the ordered pre_run_command list of the Desktop
		// file after the first sync: handwritten handlers come first.
		wantDesktop []string
	}{
		{
			name: "cli-and-desktop-files",
			seed: map[string]string{
				cliFile:     `{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"` + cli + `"}]}]}` + "\n",
				desktopFile: `{"hooks":{"pre_run_command":[{"command":"` + desktop + `"}]}}` + "\n",
			},
			wantDesktop: []string{desktop, managed},
		},
		{
			name: "windsurf-fallback",
			seed: map[string]string{
				cliFile:     `{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"` + cli + `"}]}]}` + "\n",
				fallbackRel: `{"hooks":{"pre_run_command":[{"command":"` + fallback + `"}]}}` + "\n",
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

			// assertDesktopHandlers requires the Desktop file's hooks member
			// to hold exactly want, in order, under pre_run_command and no
			// handler under any other event.
			assertDesktopHandlers := func(phase string, want []string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(e.workspace, filepath.FromSlash(desktopFile)))
				if os.IsNotExist(err) && len(want) == 0 {
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var desktopDocument struct {
					Hooks map[string][]struct {
						Command string `json:"command"`
					} `json:"hooks"`
				}
				if err := json.Unmarshal(data, &desktopDocument); err != nil {
					t.Fatalf("%s: decode %s: %v", phase, desktopFile, err)
				}
				var ordered []string
				others := 0
				for event, handlers := range desktopDocument.Hooks {
					for _, handler := range handlers {
						if event == "pre_run_command" {
							ordered = append(ordered, handler.Command)
						} else {
							others++
						}
					}
				}
				if others != 0 || !slices.Equal(ordered, want) {
					t.Fatalf("%s: %s pre_run_command handlers = %q (%d under other events), want %q", phase, desktopFile, ordered, others, want)
				}
			}

			e.MustRunAM("sync")
			assertCommands("sync", cliFile, []string{cli, managed})
			assertDesktopHandlers("sync", tc.wantDesktop)
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
			assertDesktopHandlers("windsurf deselection", wantDesktopHandwritten)
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
