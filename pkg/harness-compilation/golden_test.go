package harnesscompile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/testutil"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// goldenBundle is the single shared source bundle every compiler-backed target
// compiles for the golden tests. It deliberately contains nothing that only one
// target can represent, so no target needs its own bundle and no tree can go
// uncompared.
var goldenBundle = filepath.Join("testdata", "resources")

// TestGolden_AllTargets runs CompileBundle against the shared
// testdata/resources bundle for every compiler-backed target, materializes the output to a temp dir, and
// compares against the golden files in testdata/expected/<target>/.
//
// Golden coverage is scoped to CompilerTargets(), not ConversionTargets():
// compiling does not require an ingester, so every registered compiler target
// gets deterministic coverage of its diagnostics.
//
// Run with UPDATE_GOLDEN=1 to refresh the golden files:
//
//	UPDATE_GOLDEN=1 go test -run TestGolden_AllTargets
//
// No target is exempt. Every compiler-backed target has its complete output
// compared byte for byte. Artifacts marked for external reconciliation are
// still golden-tested, but only after this test proves generic Materialize
// refuses the complete result without writing anything.
func TestGolden_AllTargets(t *testing.T) {
	targets := targetmeta.CompilerTargets()

	for _, target := range targets {
		t.Run(string(target), func(t *testing.T) {
			result, err := CompileBundle(os.DirFS(goldenBundle), target,
				WithPluginRoot("/plugins/demo"),
			)
			if err != nil {
				t.Fatalf("CompileBundle(%s): %v", target, err)
			}
			expectedDir := filepath.Join("testdata", "expected", string(target))
			_, statErr := os.Stat(expectedDir)
			hasFixtures := statErr == nil

			// An error diagnostic no longer zeroes the bundle, so it is no
			// longer a reason to stop comparing. What it MUST come with is the
			// per-resource contract: the resources that failed are named, and
			// the resources that compiled still produced output.
			if result.HasErrors() {
				if len(result.Skipped) == 0 {
					t.Fatalf("target %s has error diagnostics but names no skipped resource: %+v", target, result.Diagnostics)
				}
				if len(result.Artifacts) == 0 {
					t.Fatalf("target %s published no artifacts at all: the per-resource error contract regressed to bundle-wide suppression", target)
				}
				for _, skipped := range result.Skipped {
					for _, path := range artifactPaths(result.Artifacts) {
						if strings.Contains(path, skipped.Name) {
							t.Errorf("target %s published %q for skipped resource %q", target, path, skipped.Name)
						}
					}
				}
			}

			if len(result.Artifacts) == 0 {
				// A target that emits only warning diagnostics may legitimately
				// produce zero artifacts (every resource kind reported as lossy
				// or deferred). There is no golden directory to compare in that
				// case, but the diagnostics must exist so loss stays explicit.
				// A target that has golden fixtures may never take this path:
				// otherwise a regression that stops emitting artifacts would be
				// silently accepted.
				if hasFixtures {
					t.Fatalf("target %s produced no artifacts but has golden fixtures in %s", target, expectedDir)
				}
				if len(result.Artifacts) == 0 && len(result.Diagnostics) > 0 {
					return
				}
				t.Fatalf("no artifacts written for target %s", target)
			}

			outDir := t.TempDir()
			stageGoldenResult(t, result, outDir)
			testutil.AssertGoldenDir(t, outDir, expectedDir)
		})
	}
}

// stageGoldenResult writes a compiler result into an empty test directory.
// Generic Materialize remains the path for ordinary artifacts. Externally
// reconciled aggregates are written by the test only after Materialize proves
// it refuses the complete result before mutating the destination.
func stageGoldenResult(t *testing.T, result *Result, outDir string) {
	t.Helper()

	ordinary := &Result{Diagnostics: result.Diagnostics, Skipped: result.Skipped}
	var external []Artifact
	for _, candidate := range result.Artifacts {
		if candidate.ExternalReconciliationRequired {
			external = append(external, candidate)
			continue
		}
		ordinary.Artifacts = append(ordinary.Artifacts, candidate)
	}

	if len(external) > 0 {
		if _, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite)); !errors.Is(err, ErrExternalReconciliationRequired) {
			t.Fatalf("Materialize externally reconciled result error = %v, want %v", err, ErrExternalReconciliationRequired)
		}
		entries, err := os.ReadDir(outDir)
		if err != nil {
			t.Fatalf("read refused materialization directory: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("Materialize wrote %d entries before refusing external reconciliation", len(entries))
		}
	}

	if len(ordinary.Artifacts) > 0 {
		if _, err := Materialize(ordinary, outDir, WithWriteMode(WriteModeOverwrite)); err != nil && !errors.Is(err, ErrPartialResult) {
			t.Fatalf("Materialize ordinary golden artifacts: %v", err)
		}
	}

	for _, candidate := range external {
		if err := candidate.Validate(); err != nil {
			t.Fatalf("validate golden artifact %q: %v", candidate.Path, err)
		}
		path := filepath.Join(outDir, candidate.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden artifact directory %q: %v", filepath.Dir(path), err)
		}
		mode := candidate.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(path, candidate.Bytes, mode); err != nil {
			t.Fatalf("write golden artifact %q: %v", candidate.Path, err)
		}
	}
}

// TestGolden_PerResourceKind tests each of the 5 resource kinds individually
// across every compiler-backed target.
// It verifies that each resource kind compiles without error and produces
// at least one artifact (or a diagnostic for dropped resources).
func TestGolden_PerResourceKind(t *testing.T) {
	src := os.DirFS(goldenBundle)
	targets := targetmeta.CompilerTargets()

	tests := []struct {
		name string
		load func() ([]Resource, error)
	}{
		{
			name: "skill",
			load: func() ([]Resource, error) {
				s, err := LoadSkill(src, "skills/demo-skill")
				if err != nil {
					return nil, err
				}
				return []Resource{s}, nil
			},
		},
		{
			name: "command",
			load: func() ([]Resource, error) {
				c, err := LoadCommand(src, "commands/demo-cmd.md")
				if err != nil {
					return nil, err
				}
				return []Resource{c}, nil
			},
		},
		{
			name: "subagent",
			load: func() ([]Resource, error) {
				s, err := LoadSubagent(src, "agents/demo-agent.md")
				if err != nil {
					return nil, err
				}
				return []Resource{s}, nil
			},
		},
		{
			name: "hook",
			load: func() ([]Resource, error) {
				hooks, _, err := LoadHooks(src, "hooks/hooks.json")
				if err != nil {
					return nil, err
				}
				resources := make([]Resource, len(hooks))
				for i, h := range hooks {
					resources[i] = h
				}
				return resources, nil
			},
		},
		{
			name: "mcpserver",
			load: func() ([]Resource, error) {
				servers, err := LoadMcpServers(src, ".mcp.json")
				if err != nil {
					return nil, err
				}
				resources := make([]Resource, len(servers))
				for i, s := range servers {
					resources[i] = s
				}
				return resources, nil
			},
		},
	}

	for _, tt := range tests {
		for _, target := range targets {
			name := tt.name + "/" + string(target)
			t.Run(name, func(t *testing.T) {
				resources, err := tt.load()
				if err != nil {
					t.Fatalf("load %s: %v", tt.name, err)
				}

				var allArtifacts []Artifact
				var allDiags []Diagnostic
				for _, r := range resources {
					result, err := Compile(r, target, WithPluginRoot("/plugins/demo"))
					if err != nil {
						t.Fatalf("Compile(%s/%s): %v", tt.name, target, err)
					}
					allArtifacts = append(allArtifacts, result.Artifacts...)
					allDiags = append(allDiags, result.Diagnostics...)
				}

				// For lossy targets (windsurf/gemini), some resources produce
				// no artifacts but emit diagnostics instead. That's expected.
				hasArtifacts := len(allArtifacts) > 0
				hasDiags := len(allDiags) > 0
				if !hasArtifacts && !hasDiags {
					t.Fatalf("expected at least one artifact or diagnostic for %s/%s", tt.name, target)
				}
			})
		}
	}
}
