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

// spec: materialization/unsafe-harness-root-preserves-external-state
func TestRunRejectsEscapingHarnessRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	outside := t.TempDir()
	root := filepath.Join(repo, ".factory")
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	resolved := []resolver.ResolvedArtifact{{
		Entry:  catalog.Entry{Kind: ir.KindSkill, Name: "root-probe", Resource: ir.NewSkill("root-probe", "root boundary control", "inert body", nil, nil, ir.SourceRef{})},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
	}}
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatal(err)
	}
	refused := false
	for _, diagnostic := range report.Diags {
		refused = refused || diagnostic.Code == "materialize.destination.error" && diagnostic.Target == "factory"
	}
	if !refused {
		t.Fatalf("escaping harness root was accepted: %+v", report.Diags)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("refusal wrote external output: %v %v", entries, err)
	}
	if text, err := os.Readlink(root); err != nil || text != outside {
		t.Fatalf("refusal changed root link: %q %v", text, err)
	}
	control := t.TempDir()
	report, err = Run(context.Background(), control, resolved)
	if err != nil || len(report.Diags) != 0 {
		t.Fatalf("real-root control failed: report=%+v err=%v", report, err)
	}
	if _, err := os.Stat(filepath.Join(control, ".factory", "skills", "root-probe", "SKILL.md")); err != nil {
		t.Fatalf("valid control did not install native skill: %v", err)
	}
}

type rootBoundaryCase struct {
	name     string
	target   hcc.Target
	repoRoot string
	userRoot string
	artifact string
	subagent bool
}

var rootBoundaryCases = []rootBoundaryCase{
	{"claude", hcc.TargetClaude, ".claude", ".claude", "skills/root-probe/SKILL.md", false},
	{"factory", hcc.TargetFactory, ".factory", ".factory", "skills/root-probe/SKILL.md", false},
	{"cursor", hcc.TargetCursor, ".cursor", ".cursor", "skills/root-probe/SKILL.md", false},
	{"windsurf", hcc.TargetWindsurf, ".windsurf", ".codeium/windsurf", "skills/root-probe/SKILL.md", false},
	{"gemini", hcc.TargetGemini, ".gemini", ".gemini", "skills/root-probe/SKILL.md", false},
	{"devin", hcc.TargetDevin, ".devin", ".config/devin", "skills/root-probe/SKILL.md", false},
	{"codex-skills", hcc.TargetCodex, ".agents", ".agents", "skills/root-probe/SKILL.md", false},
	{"codex-config", hcc.TargetCodex, ".codex", ".codex", "agents/root-probe.toml", true},
}

func rootBoundaryFixture(t *testing.T, tc rootBoundaryCase, machine bool) (string, string, []resolver.ResolvedArtifact) {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	root, source := filepath.Join(repo, tc.repoRoot), config.RepoCommitted
	if machine {
		root, source = filepath.Join(home, tc.userRoot), config.UserPersonal
	}
	resource := ir.Resource(ir.NewSkill("root-probe", "root control", "inert body", nil, nil, ir.SourceRef{}))
	if tc.subagent {
		resource = ir.NewSubagent("root-probe", "root control", "inert body", "", nil, "", nil, "", nil, ir.SourceRef{})
	}
	return repo, root, []resolver.ResolvedArtifact{{Entry: catalog.Entry{Kind: resource.Kind(), Name: "root-probe", Resource: resource}, Source: source, Harnesses: []hcc.Target{tc.target}}}
}

func requireRootBoundaryRefusal(t *testing.T, report *Report, err error, target hcc.Target) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == "materialize.destination.error" && diagnostic.Target == string(target) {
			return diagnostic.Message
		}
	}
	t.Fatalf("missing harness-boundary refusal: %+v", report.Diags)
	return ""
}

// spec: materialization/contained-root-and-scope-aliases-remain-usable
func TestRunHarnessRootAliasesRemainUsable(t *testing.T) {
	for _, tc := range rootBoundaryCases {
		for _, machine := range []bool{false, true} {
			for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
				t.Run(fmt.Sprintf("%s/machine=%v/%s", tc.name, machine, mode), func(t *testing.T) {
					repo, root, resolved := rootBoundaryFixture(t, tc, machine)
					if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
						t.Fatal(err)
					}
					contained := filepath.Join(filepath.Dir(root), "native-output")
					if err := os.Mkdir(contained, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("native-output", root); err != nil {
						t.Fatal(err)
					}
					before, err := os.Lstat(root)
					if err != nil {
						t.Fatal(err)
					}
					alias := filepath.Join(t.TempDir(), "scope-alias")
					if machine {
						home, err := os.UserHomeDir()
						if err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(home, alias); err != nil {
							t.Fatal(err)
						}
						t.Setenv("HOME", alias)
						t.Setenv("CODEX_HOME", filepath.Join(alias, ".codex"))
					} else {
						if err := os.Symlink(repo, alias); err != nil {
							t.Fatal(err)
						}
						repo = alias
					}
					modes := config.MaterializationModes{Project: mode, Machine: mode}
					report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(modes))
					if err != nil {
						t.Fatal(err)
					}
					requireNoErrorDiags(t, report.Diags)
					if raw, err := os.ReadFile(filepath.Join(contained, tc.artifact)); err != nil || !strings.Contains(string(raw), "inert body") {
						t.Fatalf("native output missing: %q %v", raw, err)
					}
					if after, err := os.Lstat(root); err != nil || !os.SameFile(before, after) {
						t.Fatalf("root alias was replaced: %v", err)
					}
				})
			}
		}
	}
}

func findRootBoundaryCase(t *testing.T, name string) rootBoundaryCase {
	t.Helper()
	for _, tc := range rootBoundaryCases {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("missing root boundary case %q", name)
	return rootBoundaryCase{}
}

// spec: materialization/contained-root-and-scope-aliases-remain-usable
func TestRunWindsurfFileModeRetargetedContainedRootAliasIsStable(t *testing.T) {
	tc := findRootBoundaryCase(t, "windsurf")
	repo, root, resolved := rootBoundaryFixture(t, tc, false)
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	initial, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if err != nil || initial == nil {
		t.Fatalf("initial Windsurf file-mode Run: report=%+v err=%v", initial, err)
	}
	requireNoErrorDiags(t, initial.Diags)
	if initial.TotalWritten() == 0 {
		t.Fatal("initial Windsurf file-mode Run did not materialize the skill")
	}
	skillPath := filepath.Join(root, tc.artifact)
	before, mode, mtime := fileSnapshot(t, skillPath)
	if mode.Perm() != 0o644 || !strings.Contains(string(before), "inert body") {
		t.Fatalf("initial Windsurf file-mode skill = %q / %v", before, mode)
	}
	skillInfo, err := os.Stat(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, manifestPath)
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	nativeRoot := filepath.Join(filepath.Dir(root), "native-root")
	if err := os.Rename(root, nativeRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("native-root", root); err != nil {
		t.Fatal(err)
	}
	rootLinkBefore, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	rootTargetBefore, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	rootTargetName, err := os.Readlink(root)
	if err != nil || rootTargetName != "native-root" {
		t.Fatalf("contained Windsurf root alias = %q / %v", rootTargetName, err)
	}
	for _, tc := range []struct {
		name   string
		dryRun bool
	}{{name: "repeat"}, {name: "check", dryRun: true}} {
		t.Run(tc.name, func(t *testing.T) {
			options := []RunOption{WithMaterializationModes(modes)}
			if tc.dryRun {
				options = append(options, WithDryRun())
			}
			report, err := Run(t.Context(), repo, resolved, options...)
			if err != nil || report == nil {
				t.Fatalf("%s Windsurf file-mode Run: report=%+v err=%v", tc.name, report, err)
			}
			assertFileUnchanged(t, skillPath, before, mode, mtime)
			if after, err := os.Stat(skillPath); err != nil || !os.SameFile(skillInfo, after) {
				t.Fatalf("%s changed the installed skill identity: %v", tc.name, err)
			}
			assertFileUnchanged(t, manifestPath, manifestBefore, manifestMode, manifestMtime)
			if after, err := os.Stat(manifestPath); err != nil || !os.SameFile(manifestInfo, after) {
				t.Fatalf("%s changed the portable manifest identity: %v", tc.name, err)
			}
			if after, err := os.Lstat(root); err != nil || !os.SameFile(rootLinkBefore, after) || rootLinkBefore.Mode() != after.Mode() || !rootLinkBefore.ModTime().Equal(after.ModTime()) {
				t.Fatalf("%s changed the admitted root alias: %v", tc.name, err)
			}
			if after, err := os.Stat(root); err != nil || !os.SameFile(rootTargetBefore, after) {
				t.Fatalf("%s changed the admitted root target: %v", tc.name, err)
			}
			if target, err := os.Readlink(root); err != nil || target != rootTargetName {
				t.Fatalf("%s changed root alias text: %q / %v", tc.name, target, err)
			}
			requireNoErrorDiags(t, report.Diags)
			if !report.InSync() || report.TotalWritten() != 0 || report.TotalRemoved() != 0 {
				t.Fatalf("%s Windsurf root alias drifted: %+v", tc.name, report)
			}
		})
	}
}

// spec: materialization/unsafe-output-ancestor-refuses-before-target-writes
func TestRunWindsurfSymlinkedSkillDirectoryPreservesExternalState(t *testing.T) {
	tc := findRootBoundaryCase(t, "windsurf")
	repo, root, resolved := rootBoundaryFixture(t, tc, false)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	victim := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(victim, []byte("preserve external bytes\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	victimBefore, victimMode, victimMtime := fileSnapshot(t, victim)
	victimInfo, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(root, "skills")
	if err := os.Symlink(outside, skillsDir); err != nil {
		t.Fatal(err)
	}
	linkBefore, err := os.Lstat(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	linkTarget, err := os.Readlink(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(t.Context(), repo, resolved, WithMaterializationModes(modes))
	if report == nil {
		t.Fatal("symlinked Windsurf skill directory returned a nil report")
	}
	requireRootBoundaryRefusal(t, report, err, hcc.TargetWindsurf)
	assertFileUnchanged(t, victim, victimBefore, victimMode, victimMtime)
	if after, err := os.Stat(victim); err != nil || !os.SameFile(victimInfo, after) {
		t.Fatalf("refusal changed external sentinel identity: %v", err)
	}
	if after, err := os.Lstat(skillsDir); err != nil || !os.SameFile(linkBefore, after) || linkBefore.Mode() != after.Mode() || !linkBefore.ModTime().Equal(after.ModTime()) {
		t.Fatalf("refusal changed the symlinked skill directory: %v", err)
	}
	if after, err := os.Readlink(skillsDir); err != nil || after != linkTarget {
		t.Fatalf("refusal changed the skill directory target: %q / %v", after, err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
		t.Fatalf("refusal left external output or staging residue: %v / %v", entries, err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("refusal created portable manifest state: %v", err)
	}
}

func TestRunUnsafeRootShapesPreserveState(t *testing.T) {
	for _, tc := range rootBoundaryCases {
		for _, machine := range []bool{false, true} {
			for _, shape := range []string{"escape", "dangling", "cycle", "file"} {
				if machine && shape == "escape" {
					continue
				}
				t.Run(fmt.Sprintf("%s/machine=%v/%s", tc.name, machine, shape), func(t *testing.T) {
					repo, root, resolved := rootBoundaryFixture(t, tc, machine)
					if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
						t.Fatal(err)
					}
					outside := t.TempDir()
					victim := filepath.Join(outside, "sentinel")
					if err := os.WriteFile(victim, []byte("preserve"), 0o640); err != nil {
						t.Fatal(err)
					}
					victimBefore, err := os.Stat(victim)
					if err != nil {
						t.Fatal(err)
					}
					switch shape {
					case "escape":
						err = os.Symlink(outside, root)
					case "dangling":
						err = os.Symlink("missing", root)
					case "cycle":
						err = os.Symlink(filepath.Base(root), root)
					case "file":
						err = os.WriteFile(root, []byte("not a directory"), 0o640)
					}
					if err != nil {
						t.Fatal(err)
					}
					before, err := os.Lstat(root)
					if err != nil {
						t.Fatal(err)
					}
					report, err := Run(context.Background(), repo, resolved)
					requireRootBoundaryRefusal(t, report, err, tc.target)
					if after, err := os.Lstat(root); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
						t.Fatalf("refusal changed root: %v", err)
					}
					if raw, err := os.ReadFile(victim); err != nil || string(raw) != "preserve" {
						t.Fatalf("external bytes changed: %q %v", raw, err)
					}
					if after, err := os.Stat(victim); err != nil || !os.SameFile(victimBefore, after) || victimBefore.Mode() != after.Mode() || !victimBefore.ModTime().Equal(after.ModTime()) {
						t.Fatalf("external metadata changed: %v", err)
					}
					if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
						t.Fatalf("external staging/output residue: %v %v", entries, err)
					}
				})
			}
		}
	}
}

// spec: materialization/unsafe-output-ancestor-refuses-before-target-writes
// spec: materialization/unsafe-output-ancestor-refusal-names-the-offending-path
func TestRunUnsafeOutputAncestorPreservesState(t *testing.T) {
	tc := rootBoundaryCases[1]
	for _, kind := range []string{"symlink", "regular-file"} {
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(kind+"/"+string(mode), func(t *testing.T) {
				repo, root, resolved := rootBoundaryFixture(t, tc, false)
				if err := os.Mkdir(root, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := t.TempDir()
				ancestor := filepath.Join(root, "skills")
				wantReason := "is a symbolic link"
				if kind == "symlink" {
					if err := os.Symlink(outside, ancestor); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(ancestor, []byte("preserve"), 0o600); err != nil {
						t.Fatal(err)
					}
					wantReason = "is not a directory"
				}
				before, err := os.Lstat(ancestor)
				if err != nil {
					t.Fatal(err)
				}
				canonicalRoot, err := filepath.EvalSymlinks(root)
				if err != nil {
					t.Fatal(err)
				}
				canonicalAncestor := filepath.Join(canonicalRoot, "skills")
				report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
				message := requireRootBoundaryRefusal(t, report, err, tc.target)
				if !strings.Contains(message, canonicalAncestor) || !strings.Contains(message, wantReason) {
					t.Fatalf("refusal %q does not name %s and %q", message, canonicalAncestor, wantReason)
				}
				if after, err := os.Lstat(ancestor); err != nil || !os.SameFile(before, after) {
					t.Fatalf("unsafe ancestor replaced: %v", err)
				}
				if kind == "symlink" {
					if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
						t.Fatalf("external output written: %v %v", entries, err)
					}
				} else if raw, err := os.ReadFile(ancestor); err != nil || string(raw) != "preserve" {
					t.Fatalf("non-directory ancestor changed: %q %v", raw, err)
				}
			})
		}
	}
}

func TestRunCreatesMissingMachineScope(t *testing.T) {
	tc := rootBoundaryCases[1]
	repo, _, resolved := rootBoundaryFixture(t, tc, true)
	home := filepath.Join(t.TempDir(), "fresh-home")
	t.Setenv("HOME", home)
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, report.Diags)
	if _, err := os.Stat(filepath.Join(home, ".factory", tc.artifact)); err != nil {
		t.Fatalf("fresh machine scope did not receive native output: %v", err)
	}
}

func TestRunCodexRelocationCreatesOnlyConfiguredPartition(t *testing.T) {
	tc := rootBoundaryCases[len(rootBoundaryCases)-1]
	repo, original, resolved := rootBoundaryFixture(t, tc, true)
	relocated := filepath.Join(t.TempDir(), "missing-parent", "codex")
	t.Setenv("CODEX_HOME", relocated)
	report, err := Run(context.Background(), repo, resolved)
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, report.Diags)
	if _, err := os.Stat(filepath.Join(relocated, tc.artifact)); err != nil {
		t.Fatalf("relocated native artifact missing: %v", err)
	}
	if _, err := os.Lstat(original); !os.IsNotExist(err) {
		t.Fatalf("default Codex root was touched: %v", err)
	}
}

// spec: materialization/read-only-root-admission-creates-no-output
func TestRunRootAdmissionDryRunCreatesNoOutput(t *testing.T) {
	for _, tc := range rootBoundaryCases {
		t.Run(tc.name, func(t *testing.T) {
			repo, root, resolved := rootBoundaryFixture(t, tc, false)
			report, err := Run(context.Background(), repo, resolved, WithDryRun())
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, report.Diags)
			if report.TotalWritten() == 0 {
				t.Fatal("dry-run did not exercise pending native output")
			}
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("read-only admission created output root: %v", err)
			}
		})
	}
}
