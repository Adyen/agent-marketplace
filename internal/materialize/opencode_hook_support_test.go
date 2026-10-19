package materialize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// openCodeProjectDirEnv is the variable the fixed adapter sets for handlers.
const openCodeProjectDirEnv = "AGENT_MARKETPLACE_PROJECT_DIR"

type openCodeTestDeclaration struct {
	Event, Matcher, Command string
	Timeout                 int
}

func openCodeSupportRoot(t *testing.T, repo string, machine bool) string {
	t.Helper()
	if machine {
		return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	}
	return filepath.Join(repo, ".opencode")
}

func openCodeOnlyDeclaration(t *testing.T, root string) openCodeTestDeclaration {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "hooks", "agent-marketplace", "*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("installed declarations = %v %v, want one", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var declaration openCodeTestDeclaration
	if err := json.Unmarshal(data, &declaration); err != nil {
		t.Fatal(err)
	}
	return declaration
}

func openCodeSupportFiles(t *testing.T, root string) map[string]fs.FileInfo {
	t.Helper()
	files := map[string]fs.FileInfo{}
	support := filepath.Join(root, "hook-support")
	err := filepath.WalkDir(support, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(support, path)
		files[filepath.ToSlash(rel)] = info
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return files
}

// spec: opencode-hooks/hook-support-is-installed-beside-the-adapter
// spec: opencode-hooks/support-and-project-references-follow-the-adapter-project-directory
func TestOpenCodeHookSupportInstallsBesideTheAdapter(t *testing.T) {
	for _, machine := range []bool{false, true} {
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(fmt.Sprintf("machine=%t/%s", machine, mode), func(t *testing.T) {
				repo := openCodeSandbox(t)
				marker := filepath.Join(t.TempDir(), "ran")
				selected := additiveSafetyProvider(t, hcc.TargetOpenCode, machine, `/bin/sh "${DROID_PLUGIN_ROOT}/shared.sh" `+marker, "#!/bin/sh\nprintf support > \"$1\"\n")
				options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				report, err := Run(t.Context(), repo, []resolver.ResolvedArtifact{selected}, options...)
				if err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("sync: %v %v", err, report.Diags)
				}
				root := openCodeSupportRoot(t, repo, machine)
				files := openCodeSupportFiles(t, root)
				if len(files) != 1 {
					t.Fatalf("support files = %v, want the shared script only", files)
				}
				for rel, info := range files {
					if !strings.HasSuffix(rel, "/guard/hooks/shared.sh") {
						t.Fatalf("support path %q is not hook-support/<source>/guard/shared.sh", rel)
					}
					if !machine && !info.Mode().IsRegular() {
						t.Fatalf("repository support %s is %v, want a portable regular copy", rel, info.Mode())
					}
				}
				command := openCodeOnlyDeclaration(t, root).Command
				for _, forbidden := range []string{filepath.Dir(filepath.Dir(selected.Path)), os.Getenv("XDG_CACHE_HOME")} {
					if forbidden != "" && forbidden != "." && strings.Contains(command, forbidden) {
						t.Fatalf("command %q points into a source or cache directory", command)
					}
				}
				if !machine && !strings.Contains(command, `"$`+openCodeProjectDirEnv+`/.opencode/hook-support/`) {
					t.Fatalf("repository command %q does not reference installed support through %s", command, openCodeProjectDirEnv)
				}
				// Run the installed command the way the adapter does: from an
				// unrelated working directory, with the project variable set.
				shell := exec.Command("/bin/sh", "-c", command)
				shell.Dir = t.TempDir()
				shell.Env = append(os.Environ(), openCodeProjectDirEnv+"="+repo)
				if output, err := shell.CombinedOutput(); err != nil {
					t.Fatalf("installed command %q failed: %v %s", command, err, output)
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "support" {
					t.Fatalf("installed support script did not run: %q %v", data, err)
				}

				// Deselection removes the declaration and its owned support.
				report, err = Run(t.Context(), repo, nil, options...)
				if err != nil || diag.HasErrors(report.Diags) {
					t.Fatalf("deselect: %v %v", err, report.Diags)
				}
				if files := openCodeSupportFiles(t, root); len(files) != 0 {
					t.Fatalf("deselected support remains: %v", files)
				}
				if matches, _ := filepath.Glob(filepath.Join(root, "hooks", "agent-marketplace", "*.json")); len(matches) != 0 {
					t.Fatalf("deselected declarations remain: %v", matches)
				}
			})
		}
	}
}

// spec: opencode-hooks/declaration-changes-are-listed-for-review
func TestOpenCodeHookDeclarationChangesAreReviewed(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			repo := openCodeSandbox(t)
			options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
			root := filepath.Join(repo, ".opencode")
			// A handwritten declaration is executed by the adapter but is not
			// an owned entry, so it is never listed.
			additiveSafetyWrite(t, filepath.Join(root, "hooks", "agent-marketplace", "manual.json"), `{"format":"agent-marketplace/opencode-hook-v1","name":"manual","event":"PreToolUse","command":"manual-check","timeout":60}`)
			hook := func(command string) []resolver.ResolvedArtifact {
				return []resolver.ResolvedArtifact{openCodeResource(ir.NewHookWithMatcher("guard", "PreToolUse", "Bash", command, "command", nil, ir.SourceRef{}))}
			}
			review := func(report *Report, dryRun bool) []HookChange {
				t.Helper()
				code := diag.CodeMaterializeHookReview
				if dryRun {
					code = diag.CodeMaterializeHookReviewPending
				}
				if len(report.HookChanges) > 0 != diag.HasCode(report.Diags, code) {
					t.Fatalf("review rows %v without matching %s diagnostic: %v", report.HookChanges, code, report.Diags)
				}
				for _, change := range report.HookChanges {
					if strings.Contains(change.File, "manual.json") {
						t.Fatalf("unowned declaration listed: %#v", change)
					}
				}
				return report.HookChanges
			}
			commandOf := func(raw json.RawMessage) string {
				var fields struct{ Command string }
				_ = json.Unmarshal(raw, &fields)
				return fields.Command
			}

			report, err := Run(t.Context(), repo, hook("first"), options...)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("add: %v %v", err, report.Diags)
			}
			if changes := review(report, false); len(changes) != 1 || len(changes[0].Before) != 0 || commandOf(changes[0].After) != "first" || changes[0].Event != "PreToolUse" || changes[0].Matcher != "Bash" {
				t.Fatalf("add review = %#v", changes)
			}
			if report, err = Run(t.Context(), repo, hook("first"), options...); err != nil || len(review(report, false)) != 0 {
				t.Fatalf("unchanged sync listed hook changes: %v %#v", err, report.HookChanges)
			}

			installed := filepath.Join(root, "hooks", "agent-marketplace", "guard.json")
			before, _ := os.ReadFile(installed)
			report, err = Run(t.Context(), repo, hook("second"), append(options, WithDryRun())...)
			if changes := review(report, true); err != nil || len(changes) != 1 || commandOf(changes[0].Before) != "first" || commandOf(changes[0].After) != "second" {
				t.Fatalf("pending review = %v %#v", err, changes)
			}
			if after, _ := os.ReadFile(installed); !bytes.Equal(before, after) {
				t.Fatal("check modified the installed declaration")
			}
			if report.InSync() {
				t.Fatal("check reported a pending declaration change as in sync")
			}

			report, err = Run(t.Context(), repo, hook("second"), options...)
			if changes := review(report, false); err != nil || len(changes) != 1 || commandOf(changes[0].Before) != "first" || commandOf(changes[0].After) != "second" {
				t.Fatalf("replace review = %v %#v", err, changes)
			}
			report, err = Run(t.Context(), repo, nil, options...)
			if changes := review(report, false); err != nil || len(changes) != 1 || commandOf(changes[0].Before) != "second" || len(changes[0].After) != 0 {
				t.Fatalf("remove review = %v %#v", err, changes)
			}
		})
	}
}
