package materialize

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: materialization/native-marker-alias-detachment-preserves-concurrent-generations
func TestRunMarkerAliasDetachmentRetainsInputGeneration(t *testing.T) {
	for _, tc := range []rootBoundaryCase{
		{name: "factory", target: hcc.TargetFactory, repoRoot: ".factory", userRoot: ".factory", artifact: "mcp.json"},
		{name: "cursor", target: hcc.TargetCursor, repoRoot: ".cursor", userRoot: ".cursor", artifact: "mcp.json"},
		{name: "windsurf", target: hcc.TargetWindsurf, repoRoot: ".windsurf", userRoot: ".codeium/windsurf", artifact: "mcp_config.json"},
	} {
		for _, machine := range []bool{false, true} {
			for _, empty := range []bool{false, true} {
				for _, dryRun := range []bool{false, true} {
					phases := []string{"native-marker-alias-input"}
					if !empty && !dryRun {
						phases = append(phases, "native-marker-alias-staged")
					}
					for _, phase := range phases {
						for _, mutation := range []string{"regular-replacement", "same-link", "disappeared", "target-edit", "target-replacement", "parent-replacement", "parent-alias"} {
							t.Run(fmt.Sprintf("%s/machine=%v/empty=%v/dry=%v/%s/%s", tc.name, machine, empty, dryRun, phase, mutation), func(t *testing.T) {
								testMarkerAliasDetachmentGeneration(t, tc, machine, empty, dryRun, phase, mutation)
							})
						}
					}
				}
			}
		}
	}
}

// spec: materialization/native-marker-alias-staging-preserves-foreign-replacements
// spec: materialization/empty-native-marker-aliases-retain-private-creation-and-no-ops
func TestRunMarkerAliasDetachmentStagingAndEmptyControls(t *testing.T) {
	for _, machine := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			phase, mutation := "native-marker-alias-staged", "stage-replacement"
			if empty {
				phase, mutation = "native-marker-alias-input", "unchanged"
			}
			t.Run(fmt.Sprintf("machine=%v/empty=%v", machine, empty), func(t *testing.T) {
				testMarkerAliasDetachmentGeneration(t, rootBoundaryCase{name: "factory", target: hcc.TargetFactory, repoRoot: ".factory", userRoot: ".factory", artifact: "mcp.json"}, machine, empty, false, phase, mutation)
			})
		}
	}
}

// spec: materialization/native-marker-alias-detachment-preserves-concurrent-generations
func TestRunMarkerAliasRefusesRetargetedSourceParent(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			originalDir, concurrentDir, selectedDir := filepath.Join(repo, "original"), filepath.Join(repo, "concurrent"), filepath.Join(repo, "selected")
			root := filepath.Join(repo, ".factory")
			for _, path := range []string{originalDir, concurrentDir, root} {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			original := `{"mcpServers":{"manual":{"command":"inert original"}}}`
			if empty {
				original = ""
			}
			const concurrent = `{"mcpServers":{"manual":{"command":"inert concurrent"}}}`
			originalPath, concurrentPath := filepath.Join(originalDir, "input.json"), filepath.Join(concurrentDir, "input.json")
			if err := os.WriteFile(originalPath, []byte(original), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(concurrentPath, []byte(concurrent), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(originalDir, selectedDir); err != nil {
				t.Fatal(err)
			}
			leaf := filepath.Join(root, "mcp.json")
			if err := os.Symlink(filepath.Join(selectedDir, "input.json"), leaf); err != nil {
				t.Fatal(err)
			}
			originalInfo, _ := markerAliasTestSnapshot(t, originalPath)
			concurrentInfo, _ := markerAliasTestSnapshot(t, concurrentPath)
			leafInfo, _ := markerAliasTestSnapshot(t, leaf)
			changed := false
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
				if changed || phase != "native-marker-alias-input" || path != leaf {
					return
				}
				changed = true
				if err := os.Rename(selectedDir, filepath.Join(repo, "retained-link")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(concurrentDir, selectedDir); err != nil {
					t.Fatal(err)
				}
			}}))
			desired := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "safe-server", Resource: ir.NewMcpServer("safe-server", "inert desired", nil, nil, nil, ir.SourceRef{})}, Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory}}}
			report, err := Run(t.Context(), repo, desired)
			if !changed {
				t.Fatalf("native source-parent boundary did not run: %+v %v", report, err)
			}
			assertNativeAggregateMetadataPreserved(t, leaf, leafInfo)
			assertNativeAggregateFilePreserved(t, originalPath, []byte(original), originalInfo)
			assertNativeAggregateFilePreserved(t, concurrentPath, []byte(concurrent), concurrentInfo)
			if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
				t.Fatalf("retargeted source parent did not generation-refuse: %+v %v", report, err)
			}
			retry, err := Run(t.Context(), repo, desired)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, retry.Diags)
			if data, err := os.ReadFile(leaf); err != nil || !strings.Contains(string(data), "inert desired") || !strings.Contains(string(data), "inert concurrent") {
				t.Fatalf("healthy source-parent retry did not merge current content: %q %v", data, err)
			}
			assertNativeAggregateFilePreserved(t, originalPath, []byte(original), originalInfo)
			assertNativeAggregateFilePreserved(t, concurrentPath, []byte(concurrent), concurrentInfo)
		})
	}
}

func TestMarkerAliasDetachmentGenerationConflictIdentity(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			root := t.TempDir()
			payload, leaf, retained := filepath.Join(root, "payload.json"), filepath.Join(root, "mcp.json"), filepath.Join(root, "original-link")
			original := `{"mcpServers":{}}`
			if empty {
				original = ""
			}
			if err := os.WriteFile(payload, []byte(original), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(payload, leaf); err != nil {
				t.Fatal(err)
			}
			changed := false
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(phase, path string) {
				if changed || phase != "native-marker-alias-input" || path != leaf {
					return
				}
				changed = true
				if err := os.Rename(leaf, retained); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(leaf, []byte("inert concurrent replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			}}))
			if err := materializeMarkerAlias(root, leaf, nil, false); !changed || !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("wrong detachment refusal identity: changed=%v err=%v", changed, err)
			}
			if data, err := os.ReadFile(leaf); err != nil || string(data) != "inert concurrent replacement" {
				t.Fatalf("typed refusal changed replacement: %q %v", data, err)
			}
		})
	}
}

func testMarkerAliasDetachmentGeneration(t *testing.T, tc rootBoundaryCase, machine, empty, dryRun bool, phase, mutation string) {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	scope, root, source := repo, filepath.Join(repo, tc.repoRoot), config.RepoCommitted
	if machine {
		scope, root, source = home, filepath.Join(home, tc.userRoot), config.UserPersonal
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload, leaf := filepath.Join(scope, "handwritten.json"), filepath.Join(root, tc.artifact)
	original := "{\n // original handwritten comment\n \"mcpServers\": {\"manual\": {\"command\": \"inert original\"}},\n \"theme\": \"original\"\n}\n"
	if empty {
		original = ""
	}
	if err := os.WriteFile(payload, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(payload, leaf); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(keep, []byte("inert unrelated sibling"), 0o644); err != nil {
		t.Fatal(err)
	}
	retainedLeaf, retainedTarget, retainedRoot := filepath.Join(t.TempDir(), "original-link"), filepath.Join(t.TempDir(), "original-target"), filepath.Join(t.TempDir(), "original-root")
	const concurrent = "{\n // concurrent handwritten comment\n \"mcpServers\": {\"concurrent\": {\"command\": \"inert concurrent\"}},\n \"theme\": \"concurrent\"\n}\n"
	var leafBefore, targetBefore, keepBefore, retainedBefore, foreignBefore, preservedBefore, rootAliasBefore os.FileInfo
	var leafBytes, targetBytes, retainedBytes []byte
	foreignPath, preservedLeaf := "", ""
	changed := false
	logger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(logger) })
	slog.SetDefault(slog.New(sdkStaleReadObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), mutate: func(observedPhase, path string) {
		if changed || observedPhase != phase || path != leaf {
			return
		}
		changed = true
		switch mutation {
		case "regular-replacement", "same-link", "disappeared":
			if err := os.Rename(leaf, retainedLeaf); err != nil {
				t.Fatal(err)
			}
			preservedLeaf = retainedLeaf
			switch mutation {
			case "regular-replacement":
				if err := os.WriteFile(leaf, []byte(concurrent), 0o600); err != nil {
					t.Fatal(err)
				}
			case "same-link":
				if err := os.Symlink(payload, leaf); err != nil {
					t.Fatal(err)
				}
			}
		case "stage-replacement":
			stages, err := filepath.Glob(filepath.Join(root, ".agent-marketplace-*"))
			if err != nil || len(stages) != 1 {
				t.Fatalf("expected one actual native replacement stage: %v %v", stages, err)
			}
			foreignPath = stages[0]
			if err := os.Rename(foreignPath, retainedTarget); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(foreignPath, []byte(concurrent), 0o640); err != nil {
				t.Fatal(err)
			}
			retainedBefore, retainedBytes = markerAliasTestSnapshot(t, retainedTarget)
			foreignBefore, _ = markerAliasTestSnapshot(t, foreignPath)
		case "target-edit":
			if err := os.WriteFile(payload, []byte(concurrent), 0o640); err != nil {
				t.Fatal(err)
			}
		case "target-replacement":
			if err := os.Rename(payload, retainedTarget); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(payload, []byte(original), 0o640); err != nil {
				t.Fatal(err)
			}
			retainedBefore, retainedBytes = markerAliasTestSnapshot(t, retainedTarget)
		case "parent-replacement", "parent-alias":
			if err := os.Rename(root, retainedRoot); err != nil {
				t.Fatal(err)
			}
			preservedLeaf = filepath.Join(retainedRoot, tc.artifact)
			keep = filepath.Join(retainedRoot, "keep.txt")
			if mutation == "parent-alias" {
				foreignRoot := t.TempDir()
				foreignPath = filepath.Join(foreignRoot, tc.artifact)
				if err := os.WriteFile(foreignPath, []byte(concurrent), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreignRoot, root); err != nil {
					t.Fatal(err)
				}
				foreignBefore, _ = markerAliasTestSnapshot(t, foreignPath)
			} else {
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(leaf, []byte(concurrent), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if mutation != "disappeared" {
			leafBefore, leafBytes = markerAliasTestSnapshot(t, leaf)
		}
		targetBefore, targetBytes = markerAliasTestSnapshot(t, payload)
		keepBefore, _ = markerAliasTestSnapshot(t, keep)
		if preservedLeaf != "" {
			var err error
			preservedBefore, err = os.Lstat(preservedLeaf)
			if err != nil {
				t.Fatal(err)
			}
		}
		if mutation == "parent-alias" {
			var err error
			rootAliasBefore, err = os.Lstat(root)
			if err != nil {
				t.Fatal(err)
			}
		}
	}}))
	desired := []resolver.ResolvedArtifact{{Entry: catalog.Entry{Name: "safe-server", Resource: ir.NewMcpServer("safe-server", "inert desired", nil, nil, nil, ir.SourceRef{})}, Source: source, Harnesses: []hcc.Target{tc.target}}}
	var options []RunOption
	if dryRun {
		options = append(options, WithDryRun())
	}
	report, err := Run(t.Context(), repo, desired, options...)
	if !changed {
		t.Fatalf("actual native alias input boundary did not run: %+v %v", report, err)
	}
	if mutation == "unchanged" {
		if err != nil {
			t.Fatal(err)
		}
		requireNoErrorDiags(t, report.Diags)
		info, data := markerAliasTestSnapshot(t, leaf)
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !strings.Contains(string(data), "inert desired") {
			t.Fatalf("empty alias control did not create private native content: %v %q", info.Mode(), data)
		}
		assertNativeAggregateFilePreserved(t, payload, targetBytes, targetBefore)
		repeat, err := Run(t.Context(), repo, desired)
		if err != nil {
			t.Fatal(err)
		}
		requireNoErrorDiags(t, repeat.Diags)
		assertNativeAggregateFilePreserved(t, leaf, data, info)
		return
	}
	if mutation == "disappeared" {
		if _, err := os.Lstat(leaf); !os.IsNotExist(err) {
			t.Fatalf("refusal recreated the disappeared destination: %v", err)
		}
	} else {
		assertNativeAggregateFilePreserved(t, leaf, leafBytes, leafBefore)
	}
	assertNativeAggregateFilePreserved(t, payload, targetBytes, targetBefore)
	assertNativeAggregateFilePreserved(t, keep, []byte("inert unrelated sibling"), keepBefore)
	if rootAliasBefore != nil {
		assertNativeAggregateMetadataPreserved(t, root, rootAliasBefore)
	}
	if preservedLeaf != "" {
		assertNativeAggregateMetadataPreserved(t, preservedLeaf, preservedBefore)
		if text, err := os.Readlink(preservedLeaf); err != nil || text != payload {
			t.Fatalf("refusal changed the retained original alias: %q %v", text, err)
		}
	}
	if retainedBefore != nil {
		assertNativeAggregateFilePreserved(t, retainedTarget, retainedBytes, retainedBefore)
	}
	if foreignPath != "" {
		assertNativeAggregateFilePreserved(t, foreignPath, []byte(concurrent), foreignBefore)
	}
	if err != nil || !hasDiagCode(report.Diags, "materialize.write.error") {
		t.Fatalf("native alias detachment did not report generation refusal: %+v %v", report, err)
	}
	for _, directory := range []string{root, retainedRoot} {
		for _, pattern := range []string{".agent-marketplace-*", ".tmp-*"} {
			paths, err := filepath.Glob(filepath.Join(directory, pattern))
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				if path != foreignPath {
					t.Fatalf("refusal left attempt staging: %s", path)
				}
			}
		}
	}
	if mutation == "parent-alias" {
		if err := os.Remove(root); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(retainedRoot, root); err != nil {
			t.Fatal(err)
		}
	}
	if mutation == "stage-replacement" {
		relocated := filepath.Join(t.TempDir(), "foreign-stage")
		if err := os.Rename(foreignPath, relocated); err != nil {
			t.Fatal(err)
		}
		foreignPath = relocated
	}
	retry, err := Run(t.Context(), repo, desired)
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, retry.Diags)
	if raw, err := os.ReadFile(leaf); err != nil || !strings.Contains(string(raw), "inert desired") {
		t.Fatalf("healthy retry did not actually publish native content: %q %v", raw, err)
	}
	assertNativeAggregateFilePreserved(t, payload, targetBytes, targetBefore)
	if foreignPath != "" {
		assertNativeAggregateFilePreserved(t, foreignPath, []byte(concurrent), foreignBefore)
	}
}

func markerAliasTestSnapshot(t *testing.T, path string) (os.FileInfo, []byte) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return info, data
}
