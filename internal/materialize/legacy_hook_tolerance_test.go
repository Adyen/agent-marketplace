package materialize

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// runLegacyToleranceFixture keeps the frozen Factory and Cursor fragments on the generic materializer path.
func runLegacyToleranceFixture(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact) (*Report, error) {
	if len(resolved) != 1 {
		return nil, fmt.Errorf("unsupported legacy tolerance selection")
	}
	selection := resolved[0]
	hook, ok := selection.Resource.(*ir.Hook)
	if !ok || hook == nil || selection.Name != "deploy" || hook.Name() != "deploy" ||
		hook.Event != "PreToolUse" || hook.Matcher != "Bash" || hook.Command != "./run.sh" ||
		hook.Type != "command" || hook.HookSource != "" || hook.Prompt != "" || hook.Timeout != nil ||
		hook.Source.File != "hooks/hooks.json" || len(hook.Extra) != 0 || selection.SourceHarness != "" ||
		selection.IsMarketplace || selection.InstalledName != "" || !selection.IsLocal() ||
		selection.V4Kind != config.V4SourceDirectory || selection.RegistryPath != "" ||
		selection.Ref != "" || selection.CatalogKey != "" || !filepath.IsAbs(selection.Path) ||
		filepath.Clean(selection.Path) != filepath.Join(filepath.Clean(selection.RegistryURL), "hooks", "hooks.json") ||
		selection.Source == config.SourceUnset || len(selection.Harnesses) == 0 {
		return nil, fmt.Errorf("unsupported legacy tolerance fixture")
	}

	result := compilePhaseResult{
		byTarget:     make(map[outputKey]*targetWork),
		skillLinks:   make(map[outputKey]map[string]*skillLinkGroup),
		allOutputs:   make(map[outputKey]bool),
		blocked:      make(blockedOutputs),
		artifactMeta: make(map[outputKey]map[string]skillLink),
		ownedState:   make(map[outputKey]any),
	}
	inspections := make(map[outputKey]*nativeHookInspection)
	seen := make(map[outputKey]bool)
	for _, target := range selection.Harnesses {
		if target != hcc.TargetFactory && target != hcc.TargetCursor {
			return nil, fmt.Errorf("unsupported legacy tolerance target")
		}
		key := outputKey{target: target, machineScope: selection.Source.IsMachineScope()}
		if seen[key] {
			return nil, fmt.Errorf("duplicate legacy tolerance output key")
		}
		seen[key] = true
		result.allOutputs[key] = true
		w := skillTarget{skill: selection, target: target, machineScope: key.machineScope}
		tw := &targetWork{}
		result.byTarget[key] = tw

		fragment, err := frozenLegacyToleranceFragment(target)
		if err != nil {
			return nil, err
		}
		ins := nativeHookInspectionFor(inspections, key, repoRoot)
		if ins.err != nil {
			recordLegacyToleranceRefusal(&result, key, w, ins.root, ins.err)
			continue
		}
		artifacts := []artifact.Artifact{fragment}
		if len(ins.entries) > 0 || ins.proof.OwnedCount() > 0 {
			roots := harness.MakeInstallRoots()
			installRoots, ok := roots[target]
			if !ok {
				return nil, fmt.Errorf("unsupported legacy tolerance target")
			}
			legacyRoot := installRoots.For(key.machineScope, repoRoot)
			suppressed, suppressErr := suppressNativeSatisfiedHookRules(selection, key, legacyRoot, ins, artifacts)
			if suppressErr != nil {
				recordLegacyToleranceRefusal(&result, key, w, legacyRoot, suppressErr)
				continue
			}
			artifacts = suppressed
		}
		tw.artifacts = append(tw.artifacts, artifacts...)
	}
	return executeMaterializePlans(ctx, result, false, repoRoot, config.DefaultMaterializationModes()), nil
}

func recordLegacyToleranceRefusal(r *compilePhaseResult, key outputKey, w skillTarget, outDir string, err error) {
	r.blocked.blockResource(key, w)
	r.blockedResources = append(r.blockedResources, BlockedResource{
		Kind:         ir.KindHook,
		Name:         w.skill.Name,
		Target:       key.target,
		MachineScope: key.machineScope,
	})
	r.allDiags = append(r.allDiags, ownedAggregateCodedDiag(key, outDir, "", err, diag.CodeHookNativeToleranceRefused))
}

func frozenLegacyToleranceFragment(target hcc.Target) (artifact.Artifact, error) {
	var contents string
	switch target {
	case hcc.TargetFactory:
		contents = `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./run.sh"}]}]}`
	case hcc.TargetCursor:
		contents = `{"hooks":{"PreToolUse":[{"event":"PreToolUse","command":"./run.sh","matcher":"Bash","type":"command"}]}}`
	default:
		return artifact.Artifact{}, fmt.Errorf("unsupported legacy tolerance target")
	}
	return artifact.Artifact{
		Path:              "hooks/hooks.json",
		Bytes:             []byte(contents),
		Fragment:          true,
		FragmentKey:       "hooks",
		MergeWithExisting: true,
		ManagedByField:    "_managed_by",
		ManagedByValue:    "agent-marketplace",
	}, nil
}

func TestLegacyToleranceFixtureWritesUnprovenMarkedFactoryFragment(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, _ := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)

	report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil || report == nil || diag.HasErrors(report.Diags) {
		t.Fatal("legacy compatibility fixture did not materialize")
	}
	legacyPath := filepath.Join(repoRoot, ".factory", "hooks", "hooks.json")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal("legacy compatibility output is missing")
	}
	var document struct {
		PreToolUse []struct {
			Matcher   string `json:"matcher"`
			ManagedBy string `json:"_managed_by"`
			Hooks     []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"PreToolUse"`
	}
	if err := json.Unmarshal(data, &document); err != nil || len(document.PreToolUse) != 1 ||
		document.PreToolUse[0].Matcher != "Bash" || document.PreToolUse[0].ManagedBy != "agent-marketplace" ||
		len(document.PreToolUse[0].Hooks) != 1 || document.PreToolUse[0].Hooks[0].Type != "command" ||
		document.PreToolUse[0].Hooks[0].Command != "./run.sh" {
		t.Fatal("legacy compatibility output did not contain the frozen marked Factory rule")
	}
}

func TestLegacyToleranceFixtureCompleteProofWithoutLegacyTwinSuppressesOutput(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, src := toleranceHookFixture(t, config.RepoCommitted, hcc.TargetFactory)
	nativeRoot := filepath.Join(repoRoot, ".factory")
	layout, ok := nativeHookLayoutFor(hcc.TargetFactory, false)
	if !ok {
		t.Fatal("Factory native hook layout is unavailable")
	}
	seedNativeOwnedHook(t, nativeRoot, layout, nativeSeedFor(t, hcc.TargetFactory, false),
		toleranceFingerprint(t, src, "factory", HookScopeRepository))
	nativePath := filepath.Join(nativeRoot, filepath.FromSlash(layout.rel))
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal("ownership store is unavailable")
	}
	statePath := store.StateFile()
	stateBefore, stateMode, stateMtime := fileSnapshot(t, statePath)

	report, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil || report == nil || diag.HasErrors(report.Diags) {
		t.Fatal("proven native compatibility fixture did not complete")
	}
	legacyPath := filepath.Join(nativeRoot, "hooks", "hooks.json")
	if _, err := os.Lstat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("complete native proof without a legacy twin created legacy output")
	}
	assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, statePath, stateBefore, stateMode, stateMtime)
}
