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
func runLegacyToleranceFixture(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, options ...RunOption) (*Report, error) {
	if len(resolved) == 0 {
		return nil, fmt.Errorf("unsupported legacy tolerance selection")
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
	for _, selection := range resolved {
		hook, ok := selection.Resource.(*ir.Hook)
		if !ok || hook == nil || !legacyToleranceSelectionSupported(selection, hook) {
			return nil, fmt.Errorf("unsupported legacy tolerance fixture")
		}
		for _, target := range selection.Harnesses {
			if target != hcc.TargetClaude && target != hcc.TargetFactory && target != hcc.TargetCursor && target != hcc.TargetWindsurf {
				return nil, fmt.Errorf("unsupported legacy tolerance target")
			}
			key := outputKey{target: target, machineScope: selection.Source.IsMachineScope()}
			result.allOutputs[key] = true
			w := skillTarget{skill: selection, target: target, machineScope: key.machineScope}
			tw := result.byTarget[key]
			if tw == nil {
				tw = &targetWork{}
				result.byTarget[key] = tw
			}

			fragment, err := frozenLegacyToleranceFragmentForSelection(selection, target, hook)
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
	}
	run := runConfig{modes: config.DefaultMaterializationModes()}
	for _, option := range options {
		if option != nil {
			option(&run)
		}
	}
	return executeMaterializePlans(ctx, result, run.dryRun, repoRoot, run.modes), nil
}

func legacyToleranceSelectionSupported(selection resolver.ResolvedArtifact, hook *ir.Hook) bool {
	localFixture := !selection.IsMarketplace && selection.IsLocal() && selection.V4Kind == config.V4SourceDirectory &&
		selection.Path == filepath.Join(filepath.Clean(selection.RegistryURL), "hooks", "hooks.json")
	indexOnlyFixture := selection.IsMarketplace && selection.MarketplaceAcquisition == config.MarketplaceAcquisitionIndexOnly &&
		selection.StagedSourceDir != "" && selection.Path == filepath.Join(selection.StagedSourceDir, "hooks.json") && hook.Source.File == "hooks/hooks.json"
	mirrorFixture := selection.IsMarketplace && selection.MarketplaceAcquisition == config.MarketplaceAcquisitionMirror &&
		selection.StagedSourceDir == "" && filepath.Ext(selection.Path) == ".gz" && hook.Source.File == "hooks/hooks.json" &&
		selection.Name == "deploy" && selection.InstalledName == "deploy" && selection.MarketplaceResourceName == hook.Name() && hook.SourceDefinitionDigest() == ""
	canonicalIdentity := hook.Name() == selection.Name || hook.SourceDefinitionDigest() != "" || indexOnlyFixture || mirrorFixture
	legacySource := hook.Source.File == "hooks/hooks.json" || hook.Source.File == "deploy-hook/hooks.json"
	rawFixtureMetadata := selection.SourceHarness == "" && selection.RegistryPath == "" && selection.Ref == "" && selection.CatalogKey == ""
	capturedLocalMetadata := legacyToleranceCapturedLocalFixture(selection, hook)
	capturedMirrorMetadata := mirrorFixture && selection.SourceHarness == "claude" && selection.RegistryPath == "" &&
		selection.Ref == "" && selection.CatalogKey == "" && hook.HookSource == "" && selection.RegistryURL != ""
	mirrorFactoryExecute := mirrorFixture && hook.Matcher == "Execute" && len(selection.Harnesses) == 1 && selection.Harnesses[0] == hcc.TargetFactory
	factorySibling := capturedLocalMetadata && selection.Name == "audit" && knownFactoryToleranceSibling(hook)
	matcherSupported := hook.Matcher == "Bash" || mirrorFactoryExecute || factorySibling
	commandSupported := hook.Command == "./run.sh" || hook.Command == sharedSourceCommand || factorySibling
	return selection.Name != "" && canonicalIdentity &&
		(hook.Event == "PreToolUse" || hook.Event == "PostToolUse") && matcherSupported &&
		commandSupported && hook.Type == "command" &&
		hook.Prompt == "" && hook.Timeout == nil && legacySource &&
		legacyToleranceExtraIsCanonical(hook.Extra) && (rawFixtureMetadata || capturedLocalMetadata || capturedMirrorMetadata) &&
		filepath.IsAbs(selection.Path) && selection.Source != config.SourceUnset && len(selection.Harnesses) > 0 &&
		(localFixture || indexOnlyFixture || mirrorFixture)
}

func knownFactoryToleranceSibling(hook *ir.Hook) bool {
	return hook != nil && hook.Event == "PostToolUse" && hook.Matcher == "Read" && hook.Type == "command" &&
		hook.Command == "./audit.sh" && hook.Prompt == "" && hook.Timeout == nil
}

func legacyToleranceCapturedLocalFixture(selection resolver.ResolvedArtifact, hook *ir.Hook) bool {
	return !selection.IsMarketplace && selection.IsLocal() && selection.V4Kind == config.V4SourceDirectory &&
		selection.Path == filepath.Join(filepath.Clean(selection.RegistryURL), "hooks", "hooks.json") &&
		selection.SourceHarness == "claude" && selection.RegistryPath == "" && selection.Ref == "" &&
		selection.RegistryURL != "" && selection.CatalogKey == selection.RegistryURL &&
		(hook.HookSource == "" || hook.HookSource == "claude") && hook.Source.File == "hooks/hooks.json" && hook.SourceDefinitionDigest() != ""
}

func legacyToleranceExtraIsCanonical(extra map[string]any) bool {
	if len(extra) == 0 {
		return true
	}
	value, ok := extra["Extra"]
	return len(extra) == 1 && ok && value == nil
}

func recordLegacyToleranceRefusal(r *compilePhaseResult, key outputKey, w skillTarget, outDir string, err error) {
	r.blocked.blockResource(key, w)
	resourceName := w.skill.Name
	if w.skill.InstalledName != "" {
		resourceName = w.skill.InstalledName
	}
	r.blockedResources = append(r.blockedResources, BlockedResource{
		Kind:         ir.KindHook,
		Name:         resourceName,
		Target:       key.target,
		MachineScope: key.machineScope,
	})
	r.allDiags = append(r.allDiags, ownedAggregateCodedDiag(key, outDir, "", err, diag.CodeHookNativeToleranceRefused))
}

func frozenLegacyToleranceFragment(target hcc.Target, hook *ir.Hook) (artifact.Artifact, error) {
	matcherSupported := hook != nil && hook.Matcher == "Bash" || hook != nil && target == hcc.TargetFactory && hook.Matcher == "Execute" || target == hcc.TargetFactory && knownFactoryToleranceSibling(hook)
	commandSupported := hook != nil && (hook.Command == "./run.sh" || hook.Command == sharedSourceCommand) || target == hcc.TargetFactory && knownFactoryToleranceSibling(hook)
	if hook == nil || hook.Type != "command" || !matcherSupported || hook.Event == "" || !commandSupported {
		return artifact.Artifact{}, fmt.Errorf("unsupported legacy tolerance hook")
	}
	var contents string
	switch target {
	case hcc.TargetClaude:
		contents = fmt.Sprintf(`{"%s":[{"matcher":"Bash","hooks":[{"type":"command","command":%q}]}]}`, hook.Event, hook.Command)
	case hcc.TargetFactory:
		matcher := "Bash"
		if hook.Matcher == "Execute" {
			matcher = "Execute"
		} else if knownFactoryToleranceSibling(hook) {
			matcher = "Read"
		}
		contents = fmt.Sprintf(`{"%s":[{"matcher":%q,"hooks":[{"type":"command","command":%q}]}]}`, hook.Event, matcher, hook.Command)
	case hcc.TargetCursor:
		contents = fmt.Sprintf(`{"hooks":{"%s":[{"event":%q,"command":%q,"matcher":"Bash","type":"command"}]}}`, hook.Event, hook.Event, hook.Command)
	case hcc.TargetWindsurf:
		if hook.Event != "PreToolUse" || hook.Matcher != "Bash" {
			return artifact.Artifact{}, fmt.Errorf("unsupported legacy tolerance Windsurf hook")
		}
		contents = fmt.Sprintf(`{"hooks":{"pre_run_command":[{"command":%q}]}}`, hook.Command)
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

func frozenLegacyToleranceFragmentForSelection(selection resolver.ResolvedArtifact, target hcc.Target, hook *ir.Hook) (artifact.Artifact, error) {
	fragment, err := frozenLegacyToleranceFragment(target, hook)
	if err != nil {
		return fragment, err
	}
	if target == hcc.TargetWindsurf {
		if selection.Source.IsMachineScope() {
			fragment.Path = "hooks.json"
		} else {
			fragment.Path = ".windsurf/hooks.json"
		}
		return fragment, nil
	}
	if target != hcc.TargetFactory || hook.Matcher != "Bash" || !legacyToleranceCapturedLocalFixture(selection, hook) {
		return fragment, nil
	}
	fragment.Bytes = []byte(fmt.Sprintf(`{"%s":[{"matcher":"Execute","hooks":[{"type":"command","command":%q}]}]}`, hook.Event, hook.Command))
	return fragment, nil
}

func TestLegacyToleranceFixtureWritesUnprovenMarkedFactoryFragment(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	resolved, _ := frozenLegacyRawHookFixture(t, config.RepoCommitted, hcc.TargetFactory)

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
