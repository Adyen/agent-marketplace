package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
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

// runLegacyToleranceFixture characterizes the frozen legacy writer, not native Run.
func runLegacyToleranceFixture(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, options ...RunOption) (*Report, error) {
	return runLegacyToleranceFixtureWithIndependentReader(ctx, repoRoot, resolved, false, options...)
}

func runIndependentLegacyToleranceFixture(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, options ...RunOption) (*Report, error) {
	return runLegacyToleranceFixtureWithIndependentReader(ctx, repoRoot, resolved, true, options...)
}

func runLegacyToleranceFixtureWithIndependentReader(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, independentReader bool, options ...RunOption) (*Report, error) {
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
	roots := harness.MakeInstallRoots()
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
			installRoots, ok := roots[target]
			if !ok {
				return nil, fmt.Errorf("unsupported legacy tolerance target")
			}
			legacyRoot := installRoots.For(key.machineScope, repoRoot)
			result.allOutputs[key] = true
			w := skillTarget{skill: selection, target: target, machineScope: key.machineScope}
			tw := result.byTarget[key]
			if tw == nil {
				tw = &targetWork{}
				result.byTarget[key] = tw
			}

			fragment, err := frozenLegacyToleranceFragmentForSelection(selection, target, hook, repoRoot, legacyRoot)
			if err != nil {
				return nil, err
			}
			ins := nativeHookInspectionFor(inspections, key, repoRoot)
			if ins.err != nil {
				recordLegacyToleranceRefusal(&result, key, w, ins.root, ins.err)
				continue
			}
			artifacts := []artifact.Artifact{fragment}
			resourceName := selection.Name
			if selection.InstalledName != "" {
				resourceName = selection.InstalledName
			}
			if ins.proof.HasHookResource(resourceName) {
				suppressed, suppressErr := suppressNativeSatisfiedHookRules(selection, key, legacyRoot, ins, artifacts)
				if suppressErr != nil {
					recordLegacyToleranceRefusal(&result, key, w, legacyRoot, suppressErr)
					continue
				}
				artifacts = suppressed
			} else if independentReader {
				satisfied, satisfactionErr := independentlySatisfiedNativeHook(w, repoRoot, ins)
				if satisfactionErr != nil {
					recordLegacyToleranceRefusal(&result, key, w, legacyRoot, satisfactionErr)
					continue
				}
				if satisfied {
					preserved, preserveErr := preserveNativeSatisfiedLegacyTwins(key, legacyRoot, artifacts)
					if preserveErr != nil {
						recordLegacyToleranceRefusal(&result, key, w, legacyRoot, preserveErr)
						continue
					}
					artifacts = preserved
				}
			}
			if len(artifacts) > 0 {
				artifacts = append(artifacts, legacyToleranceSupportArtifacts(selection, target, key.machineScope, legacyRoot)...)
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
	report := &Report{
		PerTarget:        make(map[hcc.Target]TargetReport),
		Diags:            result.allDiags,
		BlockedResources: result.blockedResources,
	}
	keys := make([]outputKey, 0, len(result.allOutputs))
	for key := range result.allOutputs {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b outputKey) int {
		if a.target != b.target {
			return strings.Compare(string(a.target), string(b.target))
		}
		if a.machineScope != b.machineScope {
			if a.machineScope {
				return 1
			}
			return -1
		}
		return 0
	})
	for _, key := range keys {
		installRoots, ok := roots[key.target]
		if !ok {
			return nil, fmt.Errorf("unsupported legacy tolerance target")
		}
		outDir := installRoots.For(key.machineScope, repoRoot)
		targetReport := TargetReport{Target: key.target, MachineScope: key.machineScope, OutDir: outDir}
		if work := result.byTarget[key]; work != nil && len(work.artifacts) > 0 {
			writeOptions := []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeOverwrite)}
			if run.dryRun {
				writeOptions = append(writeOptions, hcc.WithDryRun())
			}
			writeReport, err := hcc.Materialize(&hcc.Result{Artifacts: work.artifacts}, outDir, writeOptions...)
			if err != nil {
				return nil, fmt.Errorf("materialize legacy tolerance fixture for %s: %w", key.target, err)
			}
			targetReport.Written = writeReport.Written
			targetReport.Skipped = writeReport.Skipped
			targetReport.Removed = writeReport.Removed
			targetReport.Diags = append(targetReport.Diags, work.diags...)
			report.Diags = append(report.Diags, work.diags...)
		}
		report.PerRoot = append(report.PerRoot, targetReport)
		aggregate := report.PerTarget[key.target]
		aggregate.Target = key.target
		aggregate.Written = append(aggregate.Written, targetReport.Written...)
		aggregate.Skipped = append(aggregate.Skipped, targetReport.Skipped...)
		aggregate.Removed = append(aggregate.Removed, targetReport.Removed...)
		aggregate.Diags = append(aggregate.Diags, targetReport.Diags...)
		report.PerTarget[key.target] = aggregate
	}
	return report, nil
}

func legacyToleranceSupportArtifacts(selection resolver.ResolvedArtifact, target hcc.Target, machineScope bool, legacyRoot string) []artifact.Artifact {
	if _, ok := selection.Resource.(*ir.Hook); !ok {
		return nil
	}
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: selection, target: target, machineScope: machineScope}})
	plan, ok := plans[0]
	if !ok || plan.err != nil || len(conflicts[0]) != 0 || plan.relBase == "" {
		return nil
	}
	prefix := plan.relBase + "/"
	bundlePrefix := hookSupportBundlePrefixDir(selection, plan.definitionDir)
	if bundlePrefix != "" {
		bundlePrefix += "/"
	}
	files := make([]artifact.Artifact, 0, len(plan.artifacts))
	for _, file := range plan.artifacts {
		if !strings.HasPrefix(file.Path, prefix) {
			return nil
		}
		relative := strings.TrimPrefix(file.Path, prefix)
		if relative == "" {
			return nil
		}
		relative = strings.TrimPrefix(relative, bundlePrefix)
		file.Path = path.Join("hooks", relative)
		if err := file.Validate(); err != nil {
			return nil
		}
		if _, err := os.Lstat(filepath.Join(legacyRoot, filepath.FromSlash(file.Path))); err == nil || !errors.Is(err, os.ErrNotExist) {
			continue
		}
		files = append(files, file)
	}
	return files
}

func legacyToleranceSelectionSupported(selection resolver.ResolvedArtifact, hook *ir.Hook) bool {
	localFixture := !selection.IsMarketplace && selection.IsLocal() && selection.V4Kind == config.V4SourceDirectory &&
		selection.Path == filepath.Join(filepath.Clean(selection.RegistryURL), "hooks", "hooks.json")
	indexOnlyFixture := selection.IsMarketplace && selection.MarketplaceAcquisition == config.MarketplaceAcquisitionIndexOnly &&
		selection.StagedSourceDir != "" && selection.Path == filepath.Join(selection.StagedSourceDir, "hooks.json") && hook.Source.File == "hooks/hooks.json"
	mirrorFixture := selection.IsMarketplace && selection.MarketplaceAcquisition == config.MarketplaceAcquisitionMirror &&
		selection.StagedSourceDir == "" && filepath.Ext(selection.Path) == ".gz" && hook.Source.File == "hooks/hooks.json" &&
		selection.Name == "deploy" && selection.InstalledName == "deploy" && selection.MarketplaceResourceName == hook.Name() && hook.SourceDefinitionDigest() == ""
	if legacyTolerancePureProjectionSource(selection, hook) {
		return true
	}
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

func legacyTolerancePureProjectionSource(selection resolver.ResolvedArtifact, hook *ir.Hook) bool {
	if selection.IsMarketplace || !selection.IsLocal() || selection.V4Kind != config.V4SourceDirectory ||
		selection.RegistryURL == "" || selection.Name == "" || selection.Source == config.SourceUnset || hook == nil ||
		len(selection.Harnesses) == 0 || hook.Name() != selection.Name && hook.SourceDefinitionDigest() == "" {
		return false
	}
	if _, _, _, err := nativeHookProjectionSource(selection); err != nil {
		return false
	}
	if _, err := nativeHookSourceIdentity(selection); err != nil {
		return false
	}
	for _, target := range selection.Harnesses {
		if harness.IsInstallationAdapterTarget(target) {
			return false
		}
		if _, ok := nativeHookLayoutFor(target, selection.Source.IsMachineScope()); !ok {
			return false
		}
		if _, err := nativeHookFingerprintInput(selection, target, selection.Source.IsMachineScope()); err != nil {
			return false
		}
	}
	return true
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

func frozenLegacyToleranceFragmentForSelection(selection resolver.ResolvedArtifact, target hcc.Target, hook *ir.Hook, outputRoots ...string) (artifact.Artifact, error) {
	fragment, err := frozenLegacyToleranceFragment(target, hook)
	if err != nil {
		if !legacyTolerancePureProjectionSource(selection, hook) || len(outputRoots) != 2 {
			return fragment, err
		}
		return compiledLegacyToleranceFragment(selection, target, hook, outputRoots[0], outputRoots[1])
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

func compiledLegacyToleranceFragment(selection resolver.ResolvedArtifact, target hcc.Target, hook *ir.Hook, repoRoot, legacyRoot string) (artifact.Artifact, error) {
	supportBase := filepath.Join(legacyRoot, "hooks")
	if !selection.Source.IsMachineScope() {
		relative, err := filepath.Rel(repoRoot, supportBase)
		if err != nil {
			return artifact.Artifact{}, err
		}
		supportBase = filepath.ToSlash(relative)
	}
	options := []hcc.Option{
		hcc.WithManagedBy("_managed_by", "agent-marketplace"),
		hcc.WithPluginRoot(supportBase),
	}
	if selection.Source.IsMachineScope() {
		options = append(options, hcc.WithScope(hcc.ScopeUser))
	}
	result, err := hcc.Compile(hook, target, options...)
	if err != nil {
		return artifact.Artifact{}, err
	}
	if result == nil || result.HasErrors() {
		return artifact.Artifact{}, fmt.Errorf("legacy hook compilation did not produce a usable result")
	}
	var fragment *artifact.Artifact
	for _, candidate := range result.Artifacts {
		if !candidate.Fragment {
			continue
		}
		if fragment != nil {
			return artifact.Artifact{}, fmt.Errorf("legacy hook compilation produced multiple fragments")
		}
		candidate.ManagedByField = "_managed_by"
		candidate.ManagedByValue = "agent-marketplace"
		candidate.MergeWithExisting = true
		fragment = &candidate
	}
	if fragment == nil {
		return artifact.Artifact{}, fmt.Errorf("legacy hook compilation produced no fragment")
	}
	return *fragment, nil
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

// hookSatisfied reports whether the durable record proves that the selected
// hook — identified by its version-1 source fingerprint and its ownership
// resource name — is completely present in the native file. It never equates
// legacy rule bytes with native bytes: the future native writer records this
// fingerprint and resource identity on every entry it emits for the hook, so
// a match requires that EVERY recorded entry carrying (resource, fingerprint)
// is still present and owned-clean. A missing or drifted recorded entry is
// partial proof and suppresses nothing; a recorded entry for a different
// fingerprint or resource is no evidence for this hook at all.
func (ins *nativeHookInspection) hookSatisfied(fingerprint, resource string) bool {
	matched := false
	for _, id := range ins.proof.OwnedIDs() {
		if id.Kind != ownership.KindHook {
			continue
		}
		if !ins.proof.HasHookSource(id, resource, fingerprint) {
			continue
		}
		valueDigest, present := ins.current[id]
		if !present || ins.proof.Classify(id, valueDigest) != ownership.StatusOwnedClean {
			return false
		}
		matched = true
	}
	for _, satisfaction := range ins.proof.HookSatisfactions() {
		if !slices.Contains(satisfaction.Sources, ownership.HookSourceEvidence{Resource: resource, SourceDigest: fingerprint}) {
			continue
		}
		valueDigest, present := ins.current[satisfaction.ID()]
		if !present || valueDigest != satisfaction.ValueDigest {
			return false
		}
		matched = true
	}
	return matched
}

// nativeHookInspectionFor inspects the target's native hook file at most once
// per output key. Targets without a documented native hook file get an empty
// inspection (nothing can satisfy them); a failed inspection is cached so
// every hook of the same (target, scope) sees the identical refusal.
// Read-only native hook inspections are shared per output key: one
// cached refusal must fail every hook of the same target closed before
// any legacy mutation, and one cached entry set keeps classification
// consistent within the run.
func nativeHookInspectionFor(cache map[outputKey]*nativeHookInspection, key outputKey, repoRoot string) *nativeHookInspection {
	if ins, ok := cache[key]; ok {
		return ins
	}
	ins := &nativeHookInspection{}
	cache[key] = ins
	home := ""
	if key.machineScope {
		if resolved, err := os.UserHomeDir(); err == nil {
			home = resolved
		}
	}
	root, ok := nativeHookRootFor(key.target, key.machineScope, repoRoot, home)
	if !ok {
		return ins
	}
	ins.root = root
	store, err := ownership.Open(root)
	if err != nil {
		ins.err = nativeHookRefused(root, "", fmt.Errorf("open ownership store: %w", err))
		return ins
	}
	parsed, err := inspectNativeHookFile(store, root, key.target, key.machineScope)
	if err != nil {
		ins.err = err
		return ins
	}
	ins.entries = parsed.entries
	ins.current = parsed.current
	ins.proof = parsed.proof
	return ins
}

// hookFragmentEventMap decodes one compiled hook fragment into its event map.
// Fragments arrive in two documented envelopes: a bare event map
// ({event: [rules]}, Factory) or one nested under a "hooks" member
// (Claude, Cursor, Windsurf). Any other shape is not a hook fragment this
// function may rewrite and is left entirely alone.
func hookFragmentEventMap(data []byte) (events map[string][]json.RawMessage, nested bool, ok bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || len(root) == 0 {
		return nil, false, false
	}
	if member, has := root["hooks"]; has {
		var inner map[string][]json.RawMessage
		if err := json.Unmarshal(member, &inner); err == nil && inner != nil {
			return inner, true, true
		}
	}
	events = make(map[string][]json.RawMessage, len(root))
	for name, raw := range root {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '[' {
			return nil, false, false
		}
		var rules []json.RawMessage
		if err := json.Unmarshal(raw, &rules); err != nil {
			return nil, false, false
		}
		events[name] = rules
	}
	return events, false, true
}

// marshalHookFragment re-renders a filtered event map in the same envelope
// the compiler emitted.
func marshalHookFragment(events map[string][]json.RawMessage, nested bool) ([]byte, error) {
	if nested {
		return json.Marshal(map[string]any{"hooks": events})
	}
	return json.Marshal(events)
}

// legacyManagedRulePresent reports whether the existing legacy file already
// holds a MARKED element canonically equal to the desired rule (ignoring the
// _managed_by marker itself). A surviving managed twin must be kept desired:
// dropping it would let the merge delete legacy bytes, which this release is
// forbidden to do. An equal UNMARKED element — the proven native entry itself
// in the Windsurf shared file — does not keep the rule desired: suppressing
// it leaves exactly that element in place, marked-duplicate free.
//
// The file is read through the same descriptor-anchored boundary as every
// other aggregate: a symlink in any component, a non-regular file (a FIFO
// would hang a plain path read forever), or an oversized file is refused
// rather than followed. Any failure to survey the file — unsafe shape, read
// error, or an undecodable/mismatched document — returns an error so the
// caller fails the affected target closed instead of guessing "no managed
// twin" and letting the merge delete user bytes.
func legacyManagedRulePresent(legacyRoot, rel string, nested bool, event string, rule json.RawMessage) (bool, error) {
	raw, err := ownership.ReadAggregateFileAt(legacyRoot, rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	events, fileNested, ok := hookFragmentEventMap(raw)
	if !ok || fileNested != nested {
		return false, fmt.Errorf("legacy hook file %q is not a decodable hook document", filepath.Join(legacyRoot, filepath.FromSlash(rel)))
	}
	want, err := canonicalOwnedJSON(rule)
	if err != nil {
		return false, err
	}
	for _, existing := range events[event] {
		unmarked, marked := stripManagedByMarker(existing)
		if !marked {
			continue
		}
		canonical, err := canonicalOwnedJSON(unmarked)
		if err != nil {
			continue
		}
		if bytes.Equal(canonical, want) {
			return true, nil
		}
	}
	return false, nil
}

// stripManagedByMarker removes the _managed_by member from one JSON object,
// reporting whether the marker was present, and leaving every other field —
// and non-objects — untouched.
func stripManagedByMarker(raw json.RawMessage) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return raw, false
	}
	if _, ok := obj[managedByField]; !ok {
		return raw, false
	}
	delete(obj, managedByField)
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false
	}
	return out, true
}

// suppressNativeSatisfiedHookRules drops one hook's compiled fragment
// artifacts when the durable record proves the SAME selected hook — matching
// version-1 source fingerprint and ownership resource identity — is already
// completely present in the target's documented native file. Native rule
// bytes deliberately play no part: PR #403 emits target-native events,
// translated matchers, and rewritten support-command paths, so byte equality
// with the old fragment could never prove anything.
//
// Suppression is all-or-nothing per hook, never per rule: a hook emitting
// more than one native rule is proven only when every recorded entry is
// still owned-clean (partial proof keeps the whole legacy output). A rule
// whose managed legacy twin is still on disk stays desired so merge
// preserves those bytes instead of deleting them.
//
// A hook whose source fingerprint cannot be computed at all suppresses
// nothing — an identity guess could suppress the wrong entry. And a legacy
// file that cannot be surveyed safely for a surviving managed twin returns
// an error: the caller must fail this target closed rather than risk the
// merge deleting bytes it could not inspect.
func suppressNativeSatisfiedHookRules(res resolver.ResolvedArtifact, key outputKey, legacyRoot string, ins *nativeHookInspection, artifacts []artifact.Artifact) ([]artifact.Artifact, error) {
	fingerprint, err := nativeHookFingerprint(res, key.target, key.machineScope)
	if err != nil {
		return artifacts, nil
	}
	name := res.Name
	if res.InstalledName != "" {
		name = res.InstalledName
	}
	if !ins.hookSatisfied(fingerprint, name) {
		return artifacts, nil
	}
	// A fresh backing array is required: an error return hands the caller's
	// original slice back, and reusing its storage would leak the
	// already-rewritten fragment bytes into that "unchanged" input.
	out := make([]artifact.Artifact, 0, len(artifacts))
	for _, a := range artifacts {
		if !a.Fragment {
			out = append(out, a)
			continue
		}
		events, nested, ok := hookFragmentEventMap(a.Bytes)
		if !ok {
			out = append(out, a)
			continue
		}
		legacyRel := a.Path
		if key.target == hcc.TargetWindsurf && key.machineScope {
			// The machine-scope install root already IS ~/.codeium/windsurf:
			// the compiled ".windsurf/" prefix is a project-scope prefix that
			// machine-scope plans strip before writing.
			legacyRel = strings.TrimPrefix(a.Path, windsurfDir+"/")
		}
		filtered := make(map[string][]json.RawMessage, len(events))
		for _, event := range sortedKeys(events) {
			var kept []json.RawMessage
			for _, rule := range events[event] {
				present, presentErr := legacyManagedRulePresent(legacyRoot, legacyRel, nested, event, rule)
				if presentErr != nil {
					return artifacts, presentErr
				}
				if present {
					kept = append(kept, rule)
				}
			}
			if len(kept) > 0 {
				filtered[event] = kept
			}
		}
		if len(filtered) == 0 {
			// The hook is natively satisfied and no managed legacy twin
			// survives: it contributes nothing to the legacy file.
			continue
		}
		body, marshalErr := marshalHookFragment(filtered, nested)
		if marshalErr != nil {
			out = append(out, a)
			continue
		}
		a.Bytes = body
		out = append(out, a)
	}
	return out, nil
}

// nativeHookRootFor returns the authorized destination root that owns the
// documented native hook file for (target, machineScope) — the same root the
// future native writer's ownership store opens on. It deliberately does NOT
// reuse the legacy install root: Windsurf's repository native file
// (.devin/hooks.json) lives under the repository root, not under .windsurf.
func nativeHookRootFor(target hcc.Target, machineScope bool, repoRoot, home string) (string, bool) {
	if _, ok := nativeHookLayoutFor(target, machineScope); !ok {
		return "", false
	}
	base := repoRoot
	if machineScope {
		if home == "" {
			return "", false
		}
		base = home
	}
	switch target {
	case hcc.TargetClaude:
		return filepath.Join(base, ".claude"), true
	case hcc.TargetCursor:
		return filepath.Join(base, ".cursor"), true
	case hcc.TargetFactory:
		return filepath.Join(base, ".factory"), true
	case hcc.TargetWindsurf:
		if machineScope {
			return filepath.Join(home, ".codeium", "windsurf"), true
		}
		return repoRoot, true
	}
	return "", false
}
