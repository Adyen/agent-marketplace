package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func geminiSelection(resource ir.Resource, source config.Source) resolver.ResolvedArtifact {
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: resource.Name(), Kind: resource.Kind(), Resource: resource},
		Source:    source,
		Harnesses: []hcc.Target{hcc.TargetGemini},
	}
}

// spec: materialization/deselected-gemini-subagents-are-removed
func TestGeminiSubagentIsRemovedWhenNoLongerSelected(t *testing.T) {
	const handwritten = "---\nname: handwritten\ndescription: written by hand\n---\n\nKeep me.\n"
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, machine := range []bool{false, true} {
			for _, skillRemains := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/machine=%t/skill-remains=%t", mode, machine, skillRemains), func(t *testing.T) {
					home, repo := isolateSelectionEnvironment(t)
					root, source := filepath.Join(repo, ".gemini"), config.RepoCommitted
					if machine {
						root, source = filepath.Join(home, ".gemini"), config.UserPersonal
					}
					agent := geminiSelection(ir.NewSubagent("helper", "Helps", "Instructions.\n", "", nil, "", nil, "", nil, ir.SourceRef{}), source)
					skill := geminiSelection(ir.NewSkill("keep", "Kept", "Body\n", nil, nil, ir.SourceRef{}), source)
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}

					handwrittenPath := filepath.Join(root, "agents", "handwritten.md")
					if err := os.MkdirAll(filepath.Dir(handwrittenPath), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(handwrittenPath, []byte(handwritten), 0o644); err != nil {
						t.Fatal(err)
					}

					installedAgent := filepath.Join(root, "agents", "helper.md")
					installedSkill := filepath.Join(root, "skills", "keep")
					remaining := []resolver.ResolvedArtifact(nil)
					if skillRemains {
						remaining = []resolver.ResolvedArtifact{skill}
					}

					report, err := Run(context.Background(), repo, append([]resolver.ResolvedArtifact{skill}, agent), opts...)
					if err != nil {
						t.Fatal(err)
					}
					requireNoErrorDiags(t, report.Diags)
					for _, installed := range []string{installedAgent, installedSkill} {
						if _, err := os.Lstat(installed); err != nil {
							t.Fatalf("setup: %s was not materialized: %v", installed, err)
						}
					}

					report, err = Run(context.Background(), repo, remaining, opts...)
					if err != nil {
						t.Fatal(err)
					}
					requireNoErrorDiags(t, report.Diags)
					if _, err := os.Lstat(installedAgent); !os.IsNotExist(err) {
						t.Errorf("deselected subagent survived the sync (stat = %v)", err)
					}
					if got := readFileString(t, handwrittenPath); got != handwritten {
						t.Errorf("handwritten subagent changed:\n%s", got)
					}
					_, err = os.Lstat(installedSkill)
					if skillRemains && err != nil {
						t.Errorf("selected skill was pruned with the subagent: %v", err)
					}
					if !skillRemains && !os.IsNotExist(err) {
						t.Errorf("deselected skill survived the sync (stat = %v)", err)
					}

					check, err := Run(context.Background(), repo, remaining, append(opts, WithDryRun())...)
					if err != nil || !check.InSync() {
						t.Errorf("check after the removal reports drift: %+v %v", check, err)
					}
				})
			}
		}
	}
}

// spec: materialization/a-symlinked-gemini-agents-directory-is-refused-without-a-selected-subagent
func TestSymlinkedGeminiAgentsDirectoryIsRefusedWithoutASelectedSubagent(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, machine := range []bool{false, true} {
			for _, skillSelected := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/machine=%t/skill-selected=%t", mode, machine, skillSelected), func(t *testing.T) {
					home, repo := isolateSelectionEnvironment(t)
					t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
					root, source := filepath.Join(repo, ".gemini"), config.RepoCommitted
					if machine {
						root, source = filepath.Join(home, ".gemini"), config.UserPersonal
					}
					external := t.TempDir()
					sentinel := filepath.Join(external, "dotfile.md")
					if err := os.WriteFile(sentinel, []byte("dotfile\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					data, fileMode, mtime := fileSnapshot(t, sentinel)
					if err := os.MkdirAll(root, 0o755); err != nil {
						t.Fatal(err)
					}
					link := filepath.Join(root, "agents")
					if err := os.Symlink(external, link); err != nil {
						t.Fatal(err)
					}
					canonicalRoot, err := filepath.EvalSymlinks(root)
					if err != nil {
						t.Fatal(err)
					}
					var selected []resolver.ResolvedArtifact
					if skillSelected {
						selected = append(selected, geminiSelection(ir.NewSkill("keep", "Kept", "Body\n", nil, nil, ir.SourceRef{}), source))
					}
					modes := WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})

					for _, check := range []bool{false, true} {
						opts := []RunOption{modes}
						if check {
							opts = append(opts, WithDryRun())
						}
						report, err := Run(context.Background(), repo, selected, opts...)
						message := requireRootBoundaryRefusal(t, report, err, hcc.TargetGemini)
						if want := filepath.Join(canonicalRoot, "agents"); !strings.Contains(message, want) || !strings.Contains(message, "is a symbolic link") {
							t.Fatalf("check=%t: refusal %q does not name %s as a symbolic link", check, message, want)
						}
						if target, err := os.Readlink(link); err != nil || target != external {
							t.Fatalf("check=%t: link changed: %q, %v", check, target, err)
						}
						if entries, err := os.ReadDir(external); err != nil || len(entries) != 1 {
							t.Fatalf("check=%t: something was written through the link: %v, %v", check, entries, err)
						}
						assertFileUnchanged(t, sentinel, data, fileMode, mtime)
						if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
							t.Fatalf("check=%t: output was written next to the refused link: %v, %v", check, entries, err)
						}
					}
				})
			}
		}
	}
}

// spec: materialization/a-dangling-gemini-agents-link-is-ignored-without-a-selected-subagent
func TestDanglingGeminiAgentsLinkIsIgnoredWithoutASelectedSubagent(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		for _, machine := range []bool{false, true} {
			for _, skillSelected := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/machine=%t/skill-selected=%t", mode, machine, skillSelected), func(t *testing.T) {
					home, repo := isolateSelectionEnvironment(t)
					t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
					root, source := filepath.Join(repo, ".gemini"), config.RepoCommitted
					if machine {
						root, source = filepath.Join(home, ".gemini"), config.UserPersonal
					}
					missing := filepath.Join(t.TempDir(), "missing")
					if err := os.MkdirAll(root, 0o755); err != nil {
						t.Fatal(err)
					}
					link := filepath.Join(root, "agents")
					if err := os.Symlink(missing, link); err != nil {
						t.Fatal(err)
					}
					var selected []resolver.ResolvedArtifact
					if skillSelected {
						selected = append(selected, geminiSelection(ir.NewSkill("keep", "Kept", "Body\n", nil, nil, ir.SourceRef{}), source))
					}
					opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}

					report, err := Run(context.Background(), repo, selected, opts...)
					if err != nil {
						t.Fatal(err)
					}
					if len(report.Diags) != 0 {
						t.Fatalf("the dangling link produced diagnostics: %+v", report.Diags)
					}
					if _, err := os.Lstat(filepath.Join(root, "skills", "keep")); skillSelected && err != nil {
						t.Errorf("selected skill was not installed next to the dangling link: %v", err)
					}
					if target, err := os.Readlink(link); err != nil || target != missing {
						t.Errorf("dangling link changed: %q, %v", target, err)
					}
					if _, err := os.Stat(missing); !os.IsNotExist(err) {
						t.Errorf("something was created behind the dangling link (stat = %v)", err)
					}

					check, err := Run(context.Background(), repo, selected, append(opts, WithDryRun())...)
					if err != nil || !check.InSync() || len(check.Diags) != 0 {
						t.Errorf("check after the sync reports drift or diagnostics: %+v %v", check, err)
					}
				})
			}
		}
	}
}
