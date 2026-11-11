package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

func pluginOutputDestinations(key outputKey, repoRoot string) (string, []harness.Destination, error) {
	registry, err := harness.NewInstallationAdapterRegistry()
	if err != nil {
		return "", nil, err
	}
	if adapter, ok := registry.Lookup(key.target); ok {
		home := ""
		if key.machineScope {
			home, err = os.UserHomeDir()
			if err != nil {
				return "", nil, err
			}
		}
		destinations, err := adapter.Resolve(key.machineScope, repoRoot, home)
		if err != nil {
			return "", nil, err
		}
		if len(destinations) == 1 {
			return destinations[0].Root, destinations, nil
		}
		if key.machineScope {
			return home, destinations, nil
		}
		return repoRoot, destinations, nil
	}
	roots, ok := harness.MakeInstallRoots()[key.target]
	if !ok {
		return "", nil, fmt.Errorf("no install root configured for target %q", key.target)
	}
	return roots.For(key.machineScope, repoRoot), nil, nil
}

func pluginOutputPathAllowed(key outputKey, repoRoot, candidate string) bool {
	outDir, _, err := pluginOutputDestinations(key, repoRoot)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(outDir, candidate)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return true
	}
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return false
	}
	for _, destination := range destinations {
		if destination.AggregateFile == "" {
			continue
		}
		if filepath.Clean(candidate) == filepath.Clean(filepath.Join(destination.Root, filepath.FromSlash(destination.AggregateFile))) {
			return true
		}
	}
	return false
}

type pluginAggregateSnapshot struct {
	path       string
	rel        string
	bytes      []byte
	readErr    error
	generation dependencytxn.Generation
	mode       fs.FileMode
}

type pluginBatchRoot struct {
	store            *ownership.Store
	state            ownership.StateSnapshot
	snapshots        map[string]pluginAggregateSnapshot
	mutations        []ownership.Mutation
	satisfactions    map[string][]ownership.HookSatisfaction
	mutationPaths    map[string]bool
	recoveries       []ownership.RecoveryOutcome
	recoveryReported bool
}

func (b *pluginInstanceBatch) unreportedRecoveries(store *ownership.Store) []ownership.RecoveryOutcome {
	root := b.roots[store.RootKey()]
	if root == nil || root.recoveryReported {
		return nil
	}
	root.recoveryReported = true
	return append([]ownership.RecoveryOutcome(nil), root.recoveries...)
}

type pluginInstanceBatch struct {
	instanceKey       string
	activeInstances   map[string]bool
	ownerInstances    map[string]bool
	roots             map[string]*pluginBatchRoot
	participants      []dependencytxn.DesiredFile
	participantByPath map[string]dependencytxn.DesiredFile
	manifestCommits   []nativeHookManifestCommit
	commitOptions     []dependencytxn.Option
	reports           []TargetReport
}

type pluginOutputPlan struct {
	participant dependencytxn.DesiredFile
	ref         ownership.PluginOutputRef
	reportPath  string
	rootID      string
	outDir      string
	changed     bool
	original    []byte
}

func nativeAggregateArtifactDestination(key outputKey, repoRoot, artifactPath string) (string, string, string, bool, error) {
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return "", "", "", false, err
	}
	for _, destination := range destinations {
		for _, route := range destination.AggregateRoutes {
			if route.ArtifactPath == artifactPath {
				return destination.Root, destination.AggregateFile, destination.ID, true, nil
			}
		}
	}
	return "", "", "", false, nil
}

func planPluginArtifacts(key outputKey, outDir, repoRoot, instanceKey string, artifacts []artifact.Artifact, prior ownership.PluginState, portable *portableState, dryRun bool) ([]pluginOutputPlan, []ownership.PluginOutputRef, error) {
	if len(artifacts) == 0 {
		return nil, nil, nil
	}
	merged, err := artifact.MergeFragments(artifacts)
	if err != nil {
		return nil, nil, err
	}
	priorByPath := make(map[string]ownership.PluginOutputRef, len(prior.Outputs))
	for _, output := range prior.Outputs {
		priorByPath[filepath.Clean(output.Path)] = output
	}
	var plans []pluginOutputPlan
	var refs []ownership.PluginOutputRef
	for _, materialize := range materializePlans(key.target, outDir, merged, nil, key.machineScope) {
		planned, err := artifact.MergeFragments(materialize.artifacts)
		if err != nil {
			return nil, nil, err
		}
		for _, candidate := range planned {
			if err := candidate.Validate(); err != nil {
				return nil, nil, err
			}
			policy, err := candidate.Policy()
			if err != nil {
				return nil, nil, err
			}
			if policy.IsExternal() {
				return nil, nil, fmt.Errorf("plugin artifact %q still requires external reconciliation", candidate.Path)
			}
			outputRoot, outputRel, rootID := materialize.outDir, candidate.Path, materialize.rootID
			aggregateRoot, aggregateRel, aggregateID, aggregate, err := nativeAggregateArtifactDestination(key, repoRoot, candidate.Path)
			if err != nil {
				return nil, nil, err
			}
			if aggregate {
				outputRoot, outputRel, rootID = aggregateRoot, aggregateRel, aggregateID
			}
			destination, err := portableOutputPath(outputRoot, outputRel)
			if err != nil {
				return nil, nil, err
			}
			manifestPath := filepath.ToSlash(filepath.Join(materialize.reportPrefix, candidate.Path))
			if aggregate {
				manifestPath = filepath.ToSlash(outputRel)
			}
			originalBytes, expected, currentMode, linkTarget, err := readPluginOutput(destination)
			if err != nil {
				return nil, nil, err
			}
			outputMode := portableArtifactMode(candidate.Mode)
			mergeBytes := originalBytes
			nextBytes := append([]byte(nil), candidate.Bytes...)
			var markerField, markerValue string
			var containers, shapes []string
			if policy.Mode == artifact.ReconciliationMarker {
				if linkTarget != "" {
					if !isManagedSymlink(destination) {
						return nil, nil, fmt.Errorf("%w: unmanaged symlink at plugin aggregate %s", dependencytxn.ErrUnsafePath, destination)
					}
					var targetGeneration dependencytxn.Generation
					targetBytes, targetGeneration, targetMode, readErr := dependencytxn.ReadSnapshot(linkTarget)
					if readErr != nil || !targetGeneration.Exists {
						if readErr == nil {
							readErr = dependencytxn.ErrGenerationConflict
						}
						return nil, nil, readErr
					}
					mergeBytes, outputMode = targetBytes, targetMode.Perm()
				} else if expected.Exists {
					outputMode = currentMode.Perm()
				}
				markerValue = pluginMarkerValue(instanceKey)
				nextBytes, _, err = artifact.MergeWithExistingJSON(mergeBytes, candidate.Bytes, policy.MarkerField, markerValue)
				if err != nil {
					return nil, nil, err
				}
				containers, shapes, err = pluginArtifactContainers(candidate.Bytes)
				if err != nil {
					return nil, nil, err
				}
				markerField = policy.MarkerField
			}
			newDigest := ownership.Digest(nextBytes)
			changed := !expected.Exists || linkTarget != "" || currentMode.Perm() != outputMode.Perm() ||
				!bytes.Equal(originalBytes, nextBytes)
			currentDigest := ""
			if expected.Exists && linkTarget == "" {
				currentDigest = portableArtifactDigest(artifact.Artifact{Bytes: originalBytes, Mode: currentMode})
			}
			ref := ownership.PluginOutputRef{
				Path: filepath.Clean(destination), Digest: newDigest, Mode: uint32(outputMode.Perm()),
				MarkerField: markerField, MarkerValue: markerValue,
				Containers: containers, MarkerShapes: shapes,
			}
			if markerField == "" {
				ref.ManifestPath = manifestPath
				ref.Digest = portableArtifactDigest(artifact.Artifact{Bytes: nextBytes, Mode: outputMode})
			}
			priorOutput, hadPriorOutput := priorByPath[ref.Path]
			if markerField == "" && expected.Exists {
				if hadPriorOutput {
					if linkTarget != "" {
						if priorOutput.LinkTarget == "" || priorOutput.LinkTarget != linkTarget {
							return nil, nil, fmt.Errorf("%w: plugin output link changed: %s", dependencytxn.ErrGenerationConflict, destination)
						}
					} else {
						if currentDigest != priorOutput.Digest || uint32(currentMode.Perm()) != priorOutput.Mode {
							return nil, nil, fmt.Errorf("%w: plugin-owned output changed: %s", dependencytxn.ErrGenerationConflict, destination)
						}
					}
				} else {
					entry, owned := portable.entry(key.target, manifestPath)
					if !owned || entry.PluginInstance != "" && entry.PluginInstance != instanceKey ||
						entry.PluginInstance == "" && !hasPriorPluginState(prior, instanceKey) {
						return nil, nil, fmt.Errorf("%w: unmanaged plugin output exists: %s", dependencytxn.ErrUnsafePath, destination)
					}
					if linkTarget != "" {
						if !isManagedSymlink(destination) {
							return nil, nil, fmt.Errorf("%w: unmanaged plugin output symlink: %s", dependencytxn.ErrUnsafePath, destination)
						}
					} else if entry.Digest != "" && entry.Digest != currentDigest {
						return nil, nil, fmt.Errorf("%w: modified plugin output exists: %s", dependencytxn.ErrUnsafePath, destination)
					}
				}
			}
			if !dryRun {
				if _, err := ensurePortableOutputDir(outputRoot, filepath.ToSlash(filepath.Dir(outputRel))); err != nil {
					return nil, nil, err
				}
			}
			participant := dependencytxn.DesiredFile{
				Path: destination, Expected: expected, ExpectedMode: currentMode,
				ExpectedLinkTarget: linkTarget, Bytes: nextBytes, Mode: outputMode,
			}
			plans = append(plans, pluginOutputPlan{
				participant: participant,
				ref:         ref,
				reportPath:  manifestPath,
				rootID:      rootID,
				outDir:      outputRoot,
				changed:     changed,
				original:    append([]byte(nil), mergeBytes...),
			})
			refs = append(refs, ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	sort.Slice(plans, func(i, j int) bool { return plans[i].participant.Path < plans[j].participant.Path })
	return plans, refs, nil
}

func pluginMarkerValue(instanceKey string) string {
	return "agent-marketplace-plugin-" + instanceKey
}

func hasPriorPluginState(state ownership.PluginState, instanceKey string) bool {
	return state.Instance.InstanceKey == instanceKey && state.Instance.GenerationKey != ""
}

func pluginArtifactContainers(data []byte) ([]string, []string, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, nil, err
	}
	containers := make([]string, 0, len(document))
	for container := range document {
		containers = append(containers, container)
	}
	sort.Strings(containers)
	shapes := make([]string, 0, len(containers))
	for _, container := range containers {
		var array []json.RawMessage
		if err := json.Unmarshal(document[container], &array); err == nil {
			shapes = append(shapes, "array")
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(document[container], &object); err != nil {
			return nil, nil, fmt.Errorf("aggregate container %q is not an object or array", container)
		}
		mapArrays := len(object) > 0
		for _, raw := range object {
			var value []json.RawMessage
			if err := json.Unmarshal(raw, &value); err != nil {
				mapArrays = false
				break
			}
		}
		if mapArrays {
			shapes = append(shapes, "map-arrays")
		} else {
			shapes = append(shapes, "object")
		}
	}
	return containers, shapes, nil
}

func emptyPluginMarkerDocument(ref ownership.PluginOutputRef) ([]byte, error) {
	document := make(map[string]any, len(ref.Containers))
	for i, container := range ref.Containers {
		switch ref.MarkerShapes[i] {
		case "array":
			document[container] = []any{}
		case "object", "map-arrays":
			document[container] = map[string]any{}
		default:
			return nil, fmt.Errorf("unsupported plugin marker shape %q", ref.MarkerShapes[i])
		}
	}
	return json.Marshal(document)
}

func readPluginOutput(path string) ([]byte, dependencytxn.Generation, fs.FileMode, string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, dependencytxn.Generation{}, 0, "", nil
	}
	if err != nil {
		return nil, dependencytxn.Generation{}, 0, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, generation, err := dependencytxn.ReadSymlinkSnapshot(path)
		return nil, generation, 0, target, err
	}
	if !info.Mode().IsRegular() {
		return nil, dependencytxn.Generation{}, 0, "", fmt.Errorf("%w: plugin output is not a regular file", dependencytxn.ErrUnsafePath)
	}
	data, generation, mode, err := dependencytxn.ReadSnapshot(path)
	return data, generation, mode, "", err
}

func newPluginInstanceBatch(instanceKey string, activeInstances map[string]bool) *pluginInstanceBatch {
	return &pluginInstanceBatch{
		instanceKey:       instanceKey,
		activeInstances:   activeInstances,
		ownerInstances:    make(map[string]bool),
		roots:             make(map[string]*pluginBatchRoot),
		participantByPath: make(map[string]dependencytxn.DesiredFile),
	}
}

func (b *pluginInstanceBatch) root(ctx context.Context, store *ownership.Store, dryRun bool) (*pluginBatchRoot, error) {
	key := store.RootKey()
	if root, ok := b.roots[key]; ok {
		return root, nil
	}
	var state ownership.StateSnapshot
	var recoveries []ownership.RecoveryOutcome
	load := func() error {
		var err error
		if !dryRun {
			recoveries, err = store.Recover()
			if err != nil {
				return err
			}
		}
		state, err = store.SnapshotState()
		return err
	}
	if dryRun {
		if err := load(); err != nil {
			return nil, err
		}
	} else if err := store.WithWriteSessionContext(ctx, "plugin-instance-batch", load); err != nil {
		return nil, err
	}
	root := &pluginBatchRoot{
		store:         store,
		state:         state,
		snapshots:     make(map[string]pluginAggregateSnapshot),
		satisfactions: make(map[string][]ownership.HookSatisfaction),
		mutationPaths: make(map[string]bool),
		recoveries:    recoveries,
	}
	b.roots[key] = root
	return root, nil
}

func (b *pluginInstanceBatch) proof(ctx context.Context, store *ownership.Store, rel string, dryRun bool) (ownership.Proof, error) {
	root, err := b.root(ctx, store, dryRun)
	if err != nil {
		return ownership.Proof{}, err
	}
	return store.ProofFromSnapshot(root.state, rel)
}

func (b *pluginInstanceBatch) readAggregate(ctx context.Context, store *ownership.Store, rel string, dryRun bool) (pluginAggregateSnapshot, error) {
	root, err := b.root(ctx, store, dryRun)
	if err != nil {
		return pluginAggregateSnapshot{}, err
	}
	if snapshot, ok := root.snapshots[rel]; ok {
		return snapshot, nil
	}
	data, generation, mode, err := store.ReadAggregateSnapshot(rel)
	if err != nil && !os.IsNotExist(err) {
		return pluginAggregateSnapshot{}, err
	}
	snapshot := pluginAggregateSnapshot{
		path:       filepath.Join(store.CanonicalRoot(), filepath.FromSlash(rel)),
		rel:        rel,
		bytes:      append([]byte(nil), data...),
		readErr:    err,
		generation: generation,
		mode:       mode,
	}
	root.snapshots[rel] = snapshot
	return snapshot, nil
}

func (b *pluginInstanceBatch) addMutation(ctx context.Context, store *ownership.Store, mutation ownership.Mutation, satisfactions []ownership.HookSatisfaction) error {
	root, err := b.root(ctx, store, false)
	if err != nil {
		return err
	}
	if root.mutationPaths[mutation.Rel] {
		return fmt.Errorf("plugin instance batch contains duplicate ownership mutation %q", mutation.Rel)
	}
	root.mutationPaths[mutation.Rel] = true
	root.mutations = append(root.mutations, mutation)
	root.satisfactions[mutation.Rel] = append([]ownership.HookSatisfaction(nil), satisfactions...)
	return nil
}

func (b *pluginInstanceBatch) addParticipant(participant dependencytxn.DesiredFile) error {
	clean := filepath.Clean(participant.Path)
	if existing, ok := b.participantByPath[clean]; ok {
		if existing.Expected != participant.Expected || existing.ExpectedMode != participant.ExpectedMode ||
			existing.ExpectedLinkTarget != participant.ExpectedLinkTarget || existing.Delete != participant.Delete ||
			existing.Mode != participant.Mode || !bytes.Equal(existing.Bytes, participant.Bytes) {
			return fmt.Errorf("%w: conflicting plugin instance participant for %s", dependencytxn.ErrGenerationConflict, clean)
		}
		return nil
	}
	b.participantByPath[clean] = participant
	b.participants = append(b.participants, participant)
	return nil
}

func (b *pluginInstanceBatch) addManifestCommit(commit nativeHookManifestCommit) {
	b.manifestCommits = append(b.manifestCommits, commit)
}

func (b *pluginInstanceBatch) addAggregateReport(key outputKey, rootID, outDir, rel string, removed bool) {
	report := TargetReport{Target: key.target, MachineScope: key.machineScope, RootID: rootID, OutDir: outDir}
	if removed {
		report.Removed = []string{rel}
	} else {
		report.Written = []string{rel}
	}
	b.reports = append(b.reports, report)
}

func (b *pluginInstanceBatch) appendReports(report *Report) {
	for _, targetReport := range b.reports {
		report.PerRoot = append(report.PerRoot, targetReport)
		report.PerTarget[targetReport.Target] = mergeTargetReports(report.PerTarget[targetReport.Target], targetReport)
	}
}

func (b *pluginInstanceBatch) backupPreparedMutation(ctx context.Context, store *ownership.Store, mutation ownership.Mutation, raw []byte, readErr error) error {
	root, err := b.root(ctx, store, false)
	if err != nil {
		return err
	}
	priorAbsent := os.IsNotExist(readErr)
	_, err = store.BackupPreparedMutationSnapshot(mutation, root.state, raw, priorAbsent)
	return err
}

func (b *pluginInstanceBatch) commit(ctx context.Context) (dependencytxn.Result, error) {
	roots := make([]*pluginBatchRoot, 0, len(b.roots))
	for _, root := range b.roots {
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].store.RootKey() < roots[j].store.RootKey() })
	var result dependencytxn.Result
	var publish func(int) error
	publish = func(index int) error {
		if index < len(roots) {
			return roots[index].store.WithWriteSessionContext(ctx, "plugin-instance-commit", func() error {
				return publish(index + 1)
			})
		}
		files := append([]dependencytxn.DesiredFile(nil), b.participants...)
		for _, root := range roots {
			participant, err := root.store.PrepareBatch(root.state, root.mutations, root.satisfactions)
			if err != nil {
				return err
			}
			if participant.Path != "" {
				files = append(files, participant)
			}
		}
		stateDir, err := dependencytxn.StateDir()
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.Delete {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(file.Path), 0o700); err != nil {
				return fmt.Errorf("prepare plugin instance transaction directory: %w", err)
			}
		}
		result, err = dependencytxn.Commit(ctx, stateDir, files, b.commitOptions...)
		return err
	}
	err := publish(0)
	if err != nil {
		return result, err
	}
	for _, manifestCommit := range b.manifestCommits {
		if err := refreshNativeHookManifestCommit(manifestCommit); err != nil {
			return result, err
		}
	}
	return result, nil
}
