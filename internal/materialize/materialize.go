// Package materialize replaces the legacy internal/sync and internal/drift
// packages with the harness-compilation (HCC) pipeline. It accepts resolved
// skills (with *ir.Skill resources), compiles each into per-target artifacts
// via hcc.Compile, and materializes the result via hcc.Materialize with
// WriteModeSync (overwrite + prune-stale). This gives us idempotent sync
// with automatic orphan cleanup in a single pass.
package materialize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/codex"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/diagnosticidentity"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

const windsurfDir = ".windsurf"

// Workspace-scoped Windsurf workflows live in .windsurf/workflows/, but
// machine-scoped (global) workflows are only discovered by Windsurf under
// ~/.codeium/windsurf/global_workflows/. That rename is declared once in
// harness.TargetInfo.MachineComponentDirs and applied via machineComponentDir.

// managedByField/managedByValue mark a materialized resource as owned by
// agent-marketplace. The same marker is used for JSON merge artifacts
// (mcp.json, hooks.json) and, for the Factory file-copy workaround, injected
// into command markdown frontmatter.
const (
	managedByField = "_managed_by"
	managedByValue = "agent-marketplace"
)

var bufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

type materializePlan struct {
	outDir       string
	root         string
	rootID       string
	artifacts    []artifact.Artifact
	mode         hcc.WriteMode
	prune        bool
	reportPrefix string
}

type nativeAggregatePlan struct {
	destination       harness.Destination
	rel               string
	desired           map[string]json.RawMessage
	pruneStaleEntries bool
}

type skillLink struct {
	name         string
	target       string
	cacheDir     string
	artifacts    []artifact.Artifact
	sourceDir    string          // absolute path to the original skill directory on disk
	kind         ir.ResourceKind // kind of the resource (skill, subagent, command, …)
	resourceName string          // canonical resource name without file extension
	machineScope bool            // true when installed to machine-wide (user) scope
	registryURL  string          // URL of the source registry
	registryPath string          // sub-directory path within the registry (may be empty)
}

type skillLinkGroup struct {
	installDirRel string
	reportPrefix  string
	links         []skillLink
}

type compiledResourceLink struct {
	name          string
	installDirRel string
	artifactRel   string
	artifacts     []artifact.Artifact
}

type outputKey struct {
	target       hcc.Target
	machineScope bool
}

// rootAwareDiagnostic carries producer context until a Report exists. The
// embedded diagnostic remains the unchanged public HCC value; only the
// materialization projection consumes the scope/root/path fields.
type rootAwareDiagnostic struct {
	diag.Diagnostic
	key    outputKey
	rootID string
	path   string
}

func materializationRootID(key outputKey) string {
	scope := "project"
	if key.machineScope {
		scope = "machine"
	}
	return scope + "-" + string(key.target)
}

// MaterializationOperation identifies the bounded filesystem action or
// diagnostic represented by an issue. These values are intentionally strings:
// report consumers include the TUI and command output, not just Go callers.
type MaterializationOperation string

const (
	MaterializationOperationWrite   MaterializationOperation = "write"
	MaterializationOperationRemove  MaterializationOperation = "remove"
	MaterializationOperationSkip    MaterializationOperation = "skip"
	MaterializationOperationFailure MaterializationOperation = "failure"
)

// MaterializationOutcome describes the result of an operation. It is kept
// separate from Operation so a skipped write and a skipped resource remain
// distinguishable without parsing diagnostic prose.
type MaterializationOutcome string

const (
	MaterializationOutcomeWritten MaterializationOutcome = "written"
	MaterializationOutcomeRemoved MaterializationOutcome = "removed"
	MaterializationOutcomeSkipped MaterializationOutcome = "skipped"
	MaterializationOutcomeFailed  MaterializationOutcome = "failed"
	MaterializationOutcomeWarning MaterializationOutcome = "warning"
)

// MaterializationIssue is the bounded, structured identity of one materialize
// result. Resource and Path are empty only when the seam cannot identify them
// (for example, a target-wide destination failure). Message is diagnostic
// context and is bounded before it is exposed to report consumers.
type MaterializationIssue struct {
	Resource  string
	Target    hcc.Target
	Scope     string
	RootID    string
	Path      string
	Operation MaterializationOperation
	Outcome   MaterializationOutcome
	Code      string
	Severity  diag.Severity
	Message   string

	// identity retains the stable digest used for deduplication after the
	// display fields have been bounded. It is intentionally not serialized or
	// exposed to command consumers.
	identity string

	// presentationIdentity retains the bounded digest used when this issue is
	// grouped across target/root presentations. It is derived before display
	// fields are bounded, and intentionally remains private to this package.
	presentationIdentity string
}

// Report holds the aggregate outcome of a materialization run.
type Report struct {
	PerTarget map[hcc.Target]TargetReport
	PerRoot   []TargetReport
	Diags     []diag.Diagnostic
	Issues    []MaterializationIssue
}

// TargetReport describes what happened for a single harness target.
type TargetReport struct {
	Target       hcc.Target
	MachineScope bool
	RootID       string
	OutDir       string
	Written      []string
	Skipped      []string
	Removed      []string
	Diags        []diag.Diagnostic
	Issues       []MaterializationIssue
	// ResourceByPath is populated only at materialization seams that know the
	// originating resource. It avoids reconstructing subjects from paths.
	ResourceByPath map[string]string
}

// portableManifest records regular files and directory trees installed in file
// mode. Project manifests live in .agents and contain only target-relative
// paths, making them safe to review and commit; user manifests are local state.
type portableManifest struct {
	Version int                     `yaml:"version"`
	Entries []portableManifestEntry `yaml:"entries,omitempty"`
}

type portableManifestEntry struct {
	Target  string `yaml:"target"`
	Path    string `yaml:"path"`
	RootID  string `yaml:"root_id,omitempty"`
	RootKey string `yaml:"root_key,omitempty"`
	// Digest is the SHA-256 digest of the exact file or tree materialized at
	// Path. Empty values are legacy entries from manifests written before
	// content ownership was tracked.
	Digest string `yaml:"digest,omitempty"`
}

type portableState struct {
	path         string
	repoRoot     string
	machineScope bool
	entries      map[string]portableManifestEntry
	destinations []harness.Destination
}

type portableSyncContext struct {
	state        *portableState
	target       hcc.Target
	reportPrefix string
	desired      map[string]bool
}

func portableManifestKey(target hcc.Target, path string) string {
	return string(target) + "\x00" + filepath.ToSlash(path)
}

func portableManifestPath(repoRoot string, machineScope bool) (string, error) {
	if !machineScope {
		return filepath.Join(repoRoot, ".agents", "materialization-manifest.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for materialization manifest: %w", err)
	}
	return filepath.Join(home, ".agent-marketplace", "materialization-manifest.yaml"), nil
}

func loadPortableState(repoRoot string, machineScope bool) (*portableState, error) {
	path, err := portableManifestPath(repoRoot, machineScope)
	if err != nil {
		return nil, err
	}
	state := &portableState{path: path, repoRoot: repoRoot, machineScope: machineScope, entries: make(map[string]portableManifestEntry)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest portableManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse materialization manifest %s: %w", path, err)
	}
	if manifest.Version != 0 && manifest.Version != 1 {
		return nil, fmt.Errorf("unsupported materialization manifest version %d", manifest.Version)
	}
	for _, entry := range manifest.Entries {
		if entry.Target == "" || !hcc.IsValidTarget(entry.Target) {
			return nil, fmt.Errorf("unknown materialization manifest target %q", entry.Target)
		}
		clean := filepath.ToSlash(filepath.Clean(entry.Path))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(entry.Path) {
			return nil, fmt.Errorf("invalid materialization manifest entry %q", entry.Path)
		}
		entry.Path = clean
		state.entries[portableManifestKey(hcc.Target(entry.Target), clean)] = entry
	}
	return state, nil
}

func (s *portableState) entry(target hcc.Target, path string) (portableManifestEntry, bool) {
	entry, ok := s.entries[portableManifestKey(target, path)]
	if ok && target == hcc.Target(targetmeta.TargetCodex) && !s.matchesCodexRoot(entry) {
		return portableManifestEntry{}, false
	}
	return entry, ok
}

func (s *portableState) add(target hcc.Target, path, digest string) {
	path = filepath.ToSlash(filepath.Clean(path))
	entry := portableManifestEntry{Target: string(target), Path: path, Digest: digest}
	if target == hcc.Target(targetmeta.TargetCodex) {
		key := outputKey{target: target, machineScope: s.machineScope}
		if id, root, _, err := portableEntryDestinationForDestinations(key, "", path, s.destinations); err == nil {
			entry.RootID, entry.RootKey = id, ownership.RootKey(root)
		}
	}
	s.entries[portableManifestKey(target, path)] = entry
}

func (s *portableState) matchesCodexRoot(entry portableManifestEntry) bool {
	key := outputKey{target: hcc.Target(targetmeta.TargetCodex), machineScope: s.machineScope}
	id, root, _, err := portableEntryDestinationForDestinations(key, "", entry.Path, s.destinations)
	return err == nil && entry.RootID == id && entry.RootKey == ownership.RootKey(root)
}

func (s *portableState) remove(target hcc.Target, path string) {
	delete(s.entries, portableManifestKey(target, path))
}

// portableOutputPath returns a path below outDir only after verifying that no
// existing parent component is a symlink. Portable manifests are committed
// project input, so lexical validation alone is insufficient: an entry such as
// "skills/escape/file" could otherwise follow a user-created skills/escape
// symlink while hashing, copying, or removing an external file.
//
// The leaf itself is deliberately not resolved. A managed cache symlink at the
// leaf is a supported file-to-symlink migration case; callers decide whether
// that leaf is safe to replace based on ownership.
func portableOutputPath(outDir, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe portable output path %q", rel)
	}
	root, err := filepath.Abs(outDir)
	if err != nil {
		return "", fmt.Errorf("resolve portable output root %q: %w", outDir, err)
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil && !os.IsNotExist(rootErr) {
		return "", fmt.Errorf("resolve portable output root %q: %w", outDir, rootErr)
	}
	path := root
	parts := strings.Split(clean, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			// A missing component cannot redirect the remaining lexical path.
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("unsafe portable output path %q: parent %q is a symlink", rel, path)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("unsafe portable output path %q: parent %q is not a directory", rel, path)
		}
		// Lstat rejects symlinks directly, and EvalSymlinks verifies the
		// resolved component remains below the resolved target root (including
		// on platforms where an ancestor of outDir itself is a symlink).
		if rootErr == nil {
			resolvedParent, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", err
			}
			inside, err := portablePathWithinRoot(resolvedRoot, resolvedParent)
			if err != nil {
				return "", err
			}
			if !inside {
				return "", fmt.Errorf("unsafe portable output path %q: parent %q resolves outside output root", rel, path)
			}
		}
	}
	return filepath.Join(root, clean), nil
}

func portablePathWithinRoot(root, path string) (bool, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel), nil
}

// validatePortableOutputDir verifies that a portable output directory and all
// of its existing parents are real directories. It is also used for dry runs,
// because --check may hash existing output and must not follow a symlink either.
func validatePortableOutputDir(outDir, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	path, err := portableOutputPath(outDir, filepath.ToSlash(filepath.Join(clean, ".placeholder")))
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return "", fmt.Errorf("unsafe portable output directory %q", dir)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return dir, nil
}

// ensurePortableOutputDir creates a portable output directory only after
// validatePortableOutputDir has confirmed no existing component is a symlink.
func ensurePortableOutputDir(outDir, rel string) (string, error) {
	dir, err := validatePortableOutputDir(outDir, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *portableState) save(dryRun bool) error {
	if dryRun {
		return nil
	}
	if len(s.entries) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	entries := make([]portableManifestEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b portableManifestEntry) int {
		if a.Target != b.Target {
			return strings.Compare(a.Target, b.Target)
		}
		return strings.Compare(a.Path, b.Path)
	})
	data, err := yaml.Marshal(portableManifest{Version: 1, Entries: entries})
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".materialization-manifest-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// RunOption configures optional behavior for Run.
type RunOption func(*runConfig)

type runConfig struct {
	dryRun      bool
	scopeFilter *bool // nil = all scopes; true = machine only; false = repo only
	modes       config.MaterializationModes
	pruneFences []PruneFence
}

// PruneFence preserves managed output for one target/scope/resource-kind
// partition whose selected resource could not be materialized.
type PruneFence struct {
	Target       hcc.Target
	MachineScope bool
	Kind         ir.ResourceKind
}

// WithPruneFences preserves affected target/scope/component partitions while
// allowing unrelated valid partitions to materialize and prune normally.
func WithPruneFences(fences []PruneFence) RunOption {
	return func(c *runConfig) { c.pruneFences = append([]PruneFence(nil), fences...) }
}

// WithMaterializationModes supplies the already-resolved scope policy. Keeping
// this explicit prevents a project config from ever changing user-wide output.
func WithMaterializationModes(modes config.MaterializationModes) RunOption {
	return func(c *runConfig) { c.modes = modes }
}

// WithDryRun causes Run to report what it would do without writing files.
// Used by --check mode.
func WithDryRun() RunOption {
	return func(c *runConfig) { c.dryRun = true }
}

// WithMachineScopeOnly restricts materialization to machine-scope (user-level)
// output directories only. Repo-scope artifacts are not touched.
func WithMachineScopeOnly() RunOption {
	return func(c *runConfig) { v := true; c.scopeFilter = &v }
}

// WithRepoScopeOnly restricts materialization to repo-scope output directories
// only. Machine-scope (user-level) artifacts are not touched.
func WithRepoScopeOnly() RunOption {
	return func(c *runConfig) { v := false; c.scopeFilter = &v }
}

// skillTarget pairs a resolved skill with a specific compilation target.
type skillTarget struct {
	skill        resolver.ResolvedArtifact
	target       hcc.Target
	machineScope bool
}

// targetWork holds the compiled artifacts and diagnostics for a single output key.
type targetWork struct {
	artifacts []artifact.Artifact
	diags     []diag.Diagnostic
}

// compilePhaseResult holds the output of the compile phase.
type compilePhaseResult struct {
	allDiags   []diag.Diagnostic
	byTarget   map[outputKey]*targetWork
	skillLinks map[outputKey]map[string]*skillLinkGroup
	allOutputs map[outputKey]bool
	blocked    blockedOutputs
	// artifactMeta maps (outputKey → artifact relative path → skillLink metadata)
	// for artifacts that remain in result.Artifacts (not extracted into skillLinks).
	// Used to generate rich collision hints in syncArtifactLinks.
	artifactMeta map[outputKey]map[string]skillLink
	// Owned aggregate state is opaque to generic orchestration. Each target
	// adapter stores its JSON or TOML work value behind this map and exposes it
	// only through ownedAggregateHandler.
	ownedState map[outputKey]any
	// materializationDiags are producer-owned diagnostics whose output scope is
	// known before a TargetReport exists (compile and aggregate extraction).
	// They are appended through appendTargetDiagnostic during report assembly.
	materializationDiags map[outputKey][]diag.Diagnostic
}

func (r compilePhaseResult) ownedAggregateState() ownedAggregateState {
	return ownedAggregateState{values: r.ownedState}
}

// invalidTargetIntent rejects artifacts whose target partition is unspecified.
func invalidTargetIntent(resolved []resolver.ResolvedArtifact) *diag.Diagnostic {
	for _, resource := range resolved {
		if resource.Harnesses != nil {
			continue
		}
		return &diag.Diagnostic{
			Code:     "materialize.target.nil",
			Severity: diag.SeverityError,
			Resource: resource.Name,
			Message:  fmt.Sprintf("resource %q has no explicit target intent; refusing materialization", resource.Name),
		}
	}
	return nil
}

// Plan is the immutable, no-write result of materialization discovery and
// compilation. The same plan is used for configured backups and execution so
// stale-only outputs cannot fall outside the backup boundary.
type Plan struct {
	compileResult compilePhaseResult
	repoRoot      string
	dryRun        bool
	modes         config.MaterializationModes
	snapshot      executionSnapshot
	ManagedPaths  map[hcc.Target]backup.ManagedPaths
	CodexParts    []backup.CodexPartition
}

type manifestSnapshot struct {
	path    string
	exists  bool
	data    []byte
	entries map[string]portableManifestEntry
}

type pathSnapshot struct {
	exists bool
	mode   os.FileMode
	link   string
	digest string
}

type executionSnapshot struct {
	home, codexHome       string
	homeSet, codexHomeSet bool
	roots                 map[outputKey]string
	destinations          map[outputKey][]harness.Destination
	paths                 map[string]pathSnapshot
	manifests             map[bool]manifestSnapshot
	outputs               map[outputKey]bool
	artifacts             map[outputKey][]artifact.Artifact
	materializePlans      map[outputKey][]materializePlan
	nativeAggregates      map[outputKey][]nativeAggregatePlan
	pruneFences           map[outputKey]map[string]bool
	codexPartitions       map[string]bool
	preflightDiag         *rootAwareDiagnostic
}

func captureExecutionSnapshot(repoRoot string, outputs map[outputKey]bool) (executionSnapshot, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return executionSnapshot{}, fmt.Errorf("resolve home directory: %w", err)
	}
	codexHome, codexHomeSet := os.LookupEnv("CODEX_HOME")
	_, homeSet := os.LookupEnv("HOME")
	snapshot := executionSnapshot{
		home: home, codexHome: codexHome, homeSet: homeSet, codexHomeSet: codexHomeSet,
		roots: make(map[outputKey]string), destinations: make(map[outputKey][]harness.Destination),
		paths: make(map[string]pathSnapshot), manifests: make(map[bool]manifestSnapshot, 2),
		outputs: make(map[outputKey]bool, len(outputs)),
	}
	for key := range outputs {
		snapshot.outputs[key] = true
	}
	roots := harness.MakeInstallRoots()
	for key := range outputs {
		target, machineScope := key.target, key.machineScope
		destinations, err := harness.ResolveDestinations(target, machineScope, repoRoot)
		if err != nil {
			return executionSnapshot{}, fmt.Errorf("resolve %s %s destinations: %w", scopeName(machineScope), target, err)
		}
		snapshot.destinations[key] = destinations
		if target == hcc.Target(targetmeta.TargetCodex) {
			if machineScope {
				snapshot.roots[key] = home
			} else {
				snapshot.roots[key] = repoRoot
			}
		} else if targetRoot, ok := roots[target]; ok {
			snapshot.roots[key] = targetRoot.For(machineScope, repoRoot)
		}
		for _, destination := range destinations {
			snapshot.paths[destination.Root] = inspectPathSnapshot(destination.Root)
			for _, rel := range destination.Info.AggregateFilesForScope(machineScope) {
				snapshot.paths[filepath.Join(destination.Root, rel)] = inspectPathSnapshot(filepath.Join(destination.Root, rel))
			}
		}
		if root := snapshot.roots[key]; root != "" {
			snapshot.paths[root] = inspectPathSnapshot(root)
		}
	}
	for _, machineScope := range []bool{false, true} {
		state, err := loadPortableState(repoRoot, machineScope)
		if err != nil {
			return executionSnapshot{}, fmt.Errorf("load %s materialization manifest: %w", scopeName(machineScope), err)
		}
		data, err := os.ReadFile(state.path)
		if errors.Is(err, os.ErrNotExist) {
			data = nil
		} else if err != nil {
			return executionSnapshot{}, fmt.Errorf("read %s materialization manifest: %w", scopeName(machineScope), err)
		}
		entries := make(map[string]portableManifestEntry, len(state.entries))
		for key, entry := range state.entries {
			entries[key] = entry
		}
		snapshot.manifests[machineScope] = manifestSnapshot{path: state.path, exists: data != nil, data: append([]byte(nil), data...), entries: entries}
	}
	return snapshot, nil
}

func inspectPathSnapshot(path string) pathSnapshot {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return pathSnapshot{}
	}
	if err != nil {
		return pathSnapshot{exists: true}
	}
	snapshot := pathSnapshot{exists: true, mode: info.Mode()}
	if info.Mode()&os.ModeSymlink != 0 {
		snapshot.link, _ = os.Readlink(path)
	} else if info.Mode().IsRegular() {
		if data, readErr := os.ReadFile(path); readErr == nil {
			digest := sha256.Sum256(data)
			snapshot.digest = hex.EncodeToString(digest[:])
		}
	}
	return snapshot
}

func (s executionSnapshot) validate(repoRoot string) error {
	current, err := captureExecutionSnapshot(repoRoot, s.outputs)
	if err != nil {
		return err
	}
	planned := executionSnapshot{
		home: s.home, codexHome: s.codexHome, homeSet: s.homeSet, codexHomeSet: s.codexHomeSet,
		roots: s.roots, destinations: s.destinations, paths: s.paths, manifests: s.manifests, outputs: s.outputs,
	}
	observed := executionSnapshot{
		home: current.home, codexHome: current.codexHome, homeSet: current.homeSet, codexHomeSet: current.codexHomeSet,
		roots: current.roots, destinations: current.destinations, paths: current.paths, manifests: current.manifests, outputs: current.outputs,
	}
	if !reflect.DeepEqual(planned, observed) {
		return errors.New("materialization environment, destination, output root, or portable manifest changed after planning")
	}
	return nil
}

func freezeExecutionPlan(snapshot *executionSnapshot, result *compilePhaseResult, repoRoot string, modes config.MaterializationModes) error {
	coalesced, preflight := preflightCodexDestinationsFrozen(result, snapshot.destinations, modes)
	if preflight != nil {
		snapshot.preflightDiag = preflight
		return nil
	}
	snapshot.artifacts = make(map[outputKey][]artifact.Artifact, len(result.allOutputs))
	snapshot.materializePlans = make(map[outputKey][]materializePlan, len(result.allOutputs))
	snapshot.nativeAggregates = make(map[outputKey][]nativeAggregatePlan, len(result.allOutputs))
	snapshot.pruneFences = make(map[outputKey]map[string]bool, len(result.allOutputs))
	snapshot.codexPartitions = coalesced
	keys := make([]outputKey, 0, len(result.allOutputs))
	for key := range result.allOutputs {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b outputKey) int {
		if a.target != b.target {
			return strings.Compare(string(a.target), string(b.target))
		}
		if a.machineScope == b.machineScope {
			return 0
		}
		if a.machineScope {
			return 1
		}
		return -1
	})
	roots := harness.MakeInstallRoots()
	for _, key := range keys {
		tw := result.byTarget[key]
		var artifacts []artifact.Artifact
		if tw != nil {
			merged, err := artifact.MergeFragments(tw.artifacts)
			if err != nil {
				return fmt.Errorf("merge fragments for target %s: %w", key.target, err)
			}
			artifacts = merged
		}
		destinations := snapshot.destinations[key]
		artifacts, nativePlans, err := planNativeAggregates(key, destinations, artifacts, result.blocked.componentsFor(key))
		if err != nil {
			return fmt.Errorf("plan native aggregates for target %s: %w", key.target, err)
		}
		snapshot.artifacts[key] = artifacts
		snapshot.nativeAggregates[key] = nativePlans
		outDir := repoRoot
		if key.target == hcc.Target(targetmeta.TargetCodex) {
			if key.machineScope {
				outDir = snapshot.home
			}
		} else if root, ok := roots[key.target]; ok {
			outDir = root.For(key.machineScope, repoRoot)
		}
		plans := materializePlansWithDestinations(key.target, outDir, artifacts, linkedComponents(result.skillLinks[key]), key.machineScope, destinations)
		for i := range plans {
			if componentBlocked(result.blocked.componentsFor(key), plans[i].reportPrefix) {
				plans[i].mode = hcc.WriteModeOverwrite
				plans[i].prune = false
			}
		}
		snapshot.materializePlans[key] = plans
		snapshot.pruneFences[key] = cloneBoolMap(nativeAggregatePruneFence(key.target, key.machineScope))
	}
	return nil
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	if values == nil {
		return nil
	}
	clone := make(map[string]bool, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// Plan discovers resolved and previously managed output keys, native aggregate
// and portable-manifest state, routes, prune fences, and writes without
// mutating the filesystem. Compilation is included because it determines the
// exact writes and fences execution will use.
func BuildPlan(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, opts ...RunOption) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePlanRoot(repoRoot); err != nil {
		return nil, fmt.Errorf("validate materialization root: %w", err)
	}
	var rc runConfig
	rc.modes = config.DefaultMaterializationModes()
	for _, o := range opts {
		o(&rc)
	}
	slog.Debug("materialize start", "repo_root", repoRoot, "resolved", len(resolved), "dry_run", rc.dryRun,
		"machine_mode", rc.modes.Machine, "project_mode", rc.modes.Project)

	// Phase 1: Group skills by target and track all output keys.
	work, allOutputs, blocked, allDiags, err := buildWorkItems(resolved, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("discover materialization outputs: %w", err)
	}
	aggregateFences := make(map[outputKey]map[ir.ResourceKind]bool)
	applyPruneFences(allOutputs, blocked, aggregateFences, rc.pruneFences)
	slog.Debug("materialize work built", "work_items", len(work), "outputs", len(allOutputs), "blocked", len(blocked), "prune_fences", len(rc.pruneFences), "aggregate_fences", len(aggregateFences), "diagnostics", len(allDiags))

	// Apply scope filter: remove output keys that don't match the requested scope.
	if rc.scopeFilter != nil {
		for key := range allOutputs {
			if key.machineScope != *rc.scopeFilter {
				delete(allOutputs, key)
			}
		}
		for key := range blocked {
			if key.machineScope != *rc.scopeFilter {
				delete(blocked, key)
			}
		}
		for key := range aggregateFences {
			if key.machineScope != *rc.scopeFilter {
				delete(aggregateFences, key)
			}
		}
		filtered := work[:0]
		for _, w := range work {
			if w.machineScope == *rc.scopeFilter {
				filtered = append(filtered, w)
			}
		}
		work = filtered
	}
	if err := validatePlanOutputs(allOutputs, repoRoot); err != nil {
		return nil, fmt.Errorf("validate materialization destinations: %w", err)
	}
	snapshot, err := captureExecutionSnapshot(repoRoot, allOutputs)
	if err != nil {
		return nil, fmt.Errorf("freeze materialization destinations and environment: %w", err)
	}
	// Phase 2: Compile each work item and collect artifacts per target.
	compileResult := compileWorkItems(work, allDiags, allOutputs, blocked, aggregateFences, repoRoot)
	slog.Debug("materialize compile complete", "targets", len(compileResult.byTarget), "diagnostics", len(compileResult.allDiags))
	if err := freezeExecutionPlan(&snapshot, &compileResult, repoRoot, rc.modes); err != nil {
		return nil, fmt.Errorf("freeze materialization execution plan: %w", err)
	}
	managed, codexParts, err := backupInventory(allOutputs, repoRoot, snapshot)
	if err != nil {
		return nil, fmt.Errorf("resolve configured backup inventory: %w", err)
	}
	return &Plan{
		compileResult: compileResult,
		repoRoot:      repoRoot,
		dryRun:        rc.dryRun,
		modes:         rc.modes,
		snapshot:      snapshot,
		ManagedPaths:  managed,
		CodexParts:    codexParts,
	}, nil
}

// Execute applies a previously inspected plan. It is intentionally separate
// from Plan so callers can complete pre-mutation backups before any writes.
func (p *Plan) Execute(ctx context.Context) (*Report, error) {
	if p == nil {
		return nil, errors.New("nil materialization plan")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.snapshot.validate(p.repoRoot); err != nil {
		diagnostic := diag.Diagnostic{
			Code:     "materialize.plan.drift",
			Severity: diag.SeverityError,
			Message:  fmt.Sprintf("refusing materialization after planned inputs changed: %v", err),
			Hint:     "rebuild the materialization plan and backup from the current destinations",
		}
		return &Report{PerTarget: make(map[hcc.Target]TargetReport), Diags: append(append([]diag.Diagnostic(nil), p.compileResult.allDiags...), diagnostic)}, nil
	}
	start := time.Now()
	slog.Debug("materialize execute start", "repo_root", p.repoRoot, "outputs", len(p.compileResult.allOutputs), "dry_run", p.dryRun,
		"machine_mode", p.modes.Machine, "project_mode", p.modes.Project)
	report := executeMaterializePlans(ctx, p.compileResult, p.snapshot, p.dryRun, p.repoRoot, p.modes)
	slog.Info("materialize end", "duration_ms", time.Since(start).Milliseconds(), "targets", len(report.PerRoot), "diagnostics", len(report.Diags))
	return report, nil
}

// applyPruneFences adds narrow component or aggregate-file prune protection
// for selections present but malformed during resolution. It only fences
// outputs that already exist.
func applyPruneFences(allOutputs map[outputKey]bool, blocked blockedOutputs, aggregateFences map[outputKey]map[ir.ResourceKind]bool, fences []PruneFence) {
	for _, fence := range fences {
		if fence.Target == "" {
			continue
		}
		key := outputKey{target: fence.Target, machineScope: fence.MachineScope}
		// buildWorkItems already discovers existing output roots and native
		// aggregate keys. A fence must never create a new empty root merely
		// because a malformed selection named its partition.
		if !allOutputs[key] {
			continue
		}
		blocked.mark(key, blockedComponentsForKind(key, fence.Kind, true)...)
		if handler := ownedAggregateHandlerFor(key.target); handler != nil && len(handler.aggregateRels(fence.Kind, key.machineScope)) > 0 {
			kinds := aggregateFences[key]
			if kinds == nil {
				kinds = make(map[ir.ResourceKind]bool)
				aggregateFences[key] = kinds
			}
			kinds[fence.Kind] = true
		}
	}
}

// Run preserves the one-shot API for callers that do not need to interpose a
// backup. Planning remains no-write and execution consumes that same plan.
func Run(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, opts ...RunOption) (*Report, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if diagnostic := invalidTargetIntent(resolved); diagnostic != nil {
		return &Report{PerTarget: make(map[hcc.Target]TargetReport), Diags: []diag.Diagnostic{*diagnostic}}, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		plan, err := BuildPlan(ctx, repoRoot, resolved, opts...)
		if err != nil {
			return nil, err
		}
		report, err := plan.Execute(ctx)
		if err != nil || !hasDiagnosticCode(report, "materialize.plan.drift") || attempt == 1 {
			return report, err
		}
		// A concurrent one-shot sync may have completed between planning and
		// execution. Rebuild once from the now-current snapshot; direct Plan
		// callers still fail closed on the first drift and can decide whether to
		// retry after their configured backup is rebuilt.
	}
	return nil, errors.New("materialization retry exhausted")
}

func hasDiagnosticCode(report *Report, code string) bool {
	if report == nil {
		return false
	}
	for _, diagnostic := range report.Diags {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func validatePlanRoot(repoRoot string) error {
	if repoRoot == "" {
		return errors.New("materialization root is empty")
	}
	if !filepath.IsAbs(repoRoot) {
		return fmt.Errorf("materialization root %q must be absolute", repoRoot)
	}
	info, err := os.Stat(repoRoot)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("materialization root %q does not exist", repoRoot)
	}
	if err != nil {
		return fmt.Errorf("stat materialization root %q: %w", repoRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("materialization root %q is not a directory", repoRoot)
	}
	return nil
}

// buildWorkItems groups resolved skills by target and identifies all output
// keys (including empty targets that need pruning).
func buildWorkItems(resolved []resolver.ResolvedArtifact, repoRoot string) ([]skillTarget, map[outputKey]bool, blockedOutputs, []diag.Diagnostic, error) {
	var work []skillTarget
	var allDiags []diag.Diagnostic
	allOutputs := make(map[outputKey]bool)
	blocked := make(blockedOutputs)

	for _, s := range resolved {
		if s.Unresolved {
			// Metadata-only status rows do not materialize and should not
			// claim output keys.
			continue
		}
		targets := s.Harnesses
		if len(targets) == 0 {
			continue
		}
		machineScope := s.Source.IsMachineScope()
		for _, t := range targets {
			allOutputs[outputKey{target: t, machineScope: machineScope}] = true
		}
		nilResourceWarned := false
		for _, t := range targets {
			key := outputKey{target: t, machineScope: machineScope}
			if s.Resource == nil {
				// No IR resource means no kind, so there is nothing to
				// attribute the skip to: fail closed on the whole output.
				blocked.blockAll(key)
				if !nilResourceWarned {
					allDiags = append(allDiags, diag.Diagnostic{
						Code:     "materialize.resource.nil",
						Severity: diag.SeverityWarning,
						Resource: s.Name,
						Message:  fmt.Sprintf("resource %q has no IR resource; skipping materialization", s.Name),
					})
					nilResourceWarned = true
				}
				continue
			}
			work = append(work, skillTarget{skill: s, target: t, machineScope: machineScope})
		}
	}

	// Include previously materialized targets for cleanup when the
	// resolved list is empty or contains a non-empty target intent. Explicit
	// empty intents fence only the sibling target+scope partitions that have no
	// non-empty intent in this run; valid non-empty siblings must still prune.
	nonEmptyIntentKeys := make(map[outputKey]bool)
	hasNonEmptyIntent := false
	emptyIntentScopes := make(map[bool]bool)
	for _, resource := range resolved {
		if resource.Harnesses != nil && len(resource.Harnesses) == 0 {
			emptyIntentScopes[resource.Source.IsMachineScope()] = true
		}
		if resource.Unresolved || len(resource.Harnesses) == 0 {
			continue
		}
		hasNonEmptyIntent = true
		for _, target := range resource.Harnesses {
			nonEmptyIntentKeys[outputKey{target: target, machineScope: resource.Source.IsMachineScope()}] = true
		}
	}
	if hasNonEmptyIntent || len(resolved) == 0 {
		seededOutputs := make(map[outputKey]bool, len(allOutputs))
		for key := range allOutputs {
			seededOutputs[key] = true
		}
		if err := addExistingOutputKeys(repoRoot, allOutputs, seededOutputs); err != nil {
			return nil, nil, nil, nil, err
		}
		if err := addExistingNativeAggregateKeys(repoRoot, allOutputs); err != nil {
			return nil, nil, nil, nil, err
		}
		// A file-mode manifest may be the only remaining record after a harness
		// root is deleted. Include its known targets so deinit can still process
		// that state instead of silently skipping it.
		if err := addPortableManifestOutputKeys(repoRoot, false, allOutputs); err != nil {
			return nil, nil, nil, nil, err
		}
		if err := addPortableManifestOutputKeys(repoRoot, true, allOutputs); err != nil {
			return nil, nil, nil, nil, err
		}
		for key := range allOutputs {
			if emptyIntentScopes[key.machineScope] && !nonEmptyIntentKeys[key] {
				blocked.blockAll(key)
			}
		}

	}

	return work, allOutputs, blocked, allDiags, nil
}

func addPortableManifestOutputKeys(repoRoot string, machineScope bool, allOutputs map[outputKey]bool) error {
	state, err := loadPortableState(repoRoot, machineScope)
	if err != nil {
		return fmt.Errorf("load %s materialization manifest: %w", scopeName(machineScope), err)
	}
	roots := harness.MakeInstallRoots()
	adapters, adapterErr := harness.NewInstallationAdapterRegistry()
	for _, entry := range state.entries {
		target := hcc.Target(entry.Target)
		_, legacy := roots[target]
		legacy = legacy && !harness.IsInstallationAdapterTarget(target)
		_, migrated := adapters.Lookup(target)
		if legacy || (adapterErr == nil && migrated) {
			allOutputs[outputKey{target: target, machineScope: machineScope}] = true
		}
	}
	return nil
}

func scopeName(machineScope bool) string {
	if machineScope {
		return "machine-scope"
	}
	return "repo-scope"
}

func addExistingOutputKeys(repoRoot string, allOutputs, seededOutputs map[outputKey]bool) error {
	for target, roots := range harness.MakeInstallRoots() {
		if harness.IsInstallationAdapterTarget(target) {
			continue
		}
		repoDir := repoOutputDir(target, roots, repoRoot)
		machineDir := roots.For(true, repoRoot)
		// When repoRoot is the home directory the repo-scope path is the same
		// filesystem location as the machine-scope path (e.g. ~/.factory ==
		// ~/.factory). Adding both keys would cause the repo-scope pass (which
		// has no resolved artifacts) to prune every file the machine-scope pass
		// just wrote, creating an install→remove→install cycle on every sync.
		repoExists, err := outputDirExists(repoDir)
		if err != nil {
			return fmt.Errorf("inspect %s repo-scope output root %q: %w", target, repoDir, err)
		}
		if repoExists && !sameOutputDir(repoDir, machineDir) {
			allOutputs[outputKey{target: target, machineScope: false}] = true
		}
		machineExists, err := outputDirExists(machineDir)
		if err != nil {
			return fmt.Errorf("inspect %s machine-scope output root %q: %w", target, machineDir, err)
		}
		if machineExists {
			allOutputs[outputKey{target: target, machineScope: true}] = true
		}
	}
	// Adapter-owned destinations cover migrated targets (including Codex's
	// partitioned roots) without maintaining a second target-specific walk.
	if adapters, err := harness.NewInstallationAdapterRegistry(); err == nil {
		for _, target := range adapters.Targets() {
			for _, machineScope := range []bool{false, true} {
				destinations, err := harness.ResolveDestinations(target, machineScope, repoRoot)
				if err != nil {
					if seededOutputs[outputKey{target: target, machineScope: machineScope}] {
						return fmt.Errorf("resolve %s %s destinations: %w", scopeName(machineScope), target, err)
					}
					continue
				}
				for _, destination := range destinations {
					// Shared native partitions are not installation markers, but durable
					// managed-link evidence must still revive their output key for prune.
					if !destination.InstallationEvidence {
						hasManaged, err := destinationHasManagedSymlink(destination, machineScope)
						if err != nil {
							return fmt.Errorf("inspect %s %s destination %q for managed links: %w", target, scopeName(machineScope), destination.Root, err)
						}
						if hasManaged {
							allOutputs[outputKey{target: target, machineScope: machineScope}] = true
							break
						}
						continue
					}
					exists, err := outputDirExists(destination.Root)
					if err != nil {
						return fmt.Errorf("inspect %s %s destination %q: %w", target, scopeName(machineScope), destination.Root, err)
					}
					if exists {
						allOutputs[outputKey{target: target, machineScope: machineScope}] = true
						break
					}
				}

			}
		}
	}
	return nil
}

func destinationHasManagedSymlink(destination harness.Destination, machineScope bool) (bool, error) {
	for _, component := range destination.Info.ComponentsForScope(machineScope) {
		entries, err := os.ReadDir(filepath.Join(destination.Root, component))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read component directory %q: %w", filepath.Join(destination.Root, component), err)
		}
		for _, entry := range entries {
			if isManagedSymlink(filepath.Join(destination.Root, component, entry.Name())) {
				return true, nil
			}
		}
	}
	return false, nil
}

// sameOutputDir reports whether two output directory paths refer to the same
// filesystem location. Uses os.SameFile to handle symlinks (e.g. macOS
// /tmp → /private/tmp).
func sameOutputDir(a, b string) bool {
	if a == b {
		return true
	}
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(sa, sb)
}

func repoOutputDir(target hcc.Target, roots harness.Roots, repoRoot string) string {
	if target == hcc.TargetWindsurf {
		return filepath.Join(repoRoot, windsurfDir)
	}
	return roots.For(false, repoRoot)
}

func outputDirExists(path string) (bool, error) {
	if path == "" {
		return false, errors.New("output directory path is empty")
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat output directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("output path %q is not a directory", path)
	}
	return true, nil
}

func validatePlanOutputs(outputs map[outputKey]bool, repoRoot string) error {
	roots := harness.MakeInstallRoots()
	for key := range outputs {
		if key.target == hcc.Target(targetmeta.TargetCodex) {
			if _, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot); err != nil {
				return fmt.Errorf("resolve %s %s destinations: %w", scopeName(key.machineScope), key.target, err)
			}
			continue
		}
		targetRoots, ok := roots[key.target]
		if !ok {
			return fmt.Errorf("target %q has no install roots", key.target)
		}
		if dir := targetRoots.For(key.machineScope, repoRoot); dir == "" {
			return fmt.Errorf("target %q has no %s output root", key.target, scopeName(key.machineScope))
		}
	}
	return nil
}

func backupPathCoveredByAncestor(rel string, collected []string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, candidate := range collected {
		candidate = filepath.ToSlash(filepath.Clean(candidate))
		if rel == candidate || strings.HasPrefix(rel, candidate+"/") {
			return true
		}
	}
	return false
}

// backupInventory derives configured machine-scope coverage from the exact
// output keys in a plan, including keys revived from stale output and manifests.
func backupInventory(outputs map[outputKey]bool, repoRoot string, snapshot executionSnapshot) (map[hcc.Target]backup.ManagedPaths, []backup.CodexPartition, error) {
	managed := make(map[hcc.Target]backup.ManagedPaths)
	keys := make([]outputKey, 0, len(outputs))
	for key := range outputs {
		if key.machineScope {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b outputKey) int { return strings.Compare(string(a.target), string(b.target)) })
	for _, key := range keys {
		if key.target == hcc.Target(targetmeta.TargetCodex) {
			continue
		}
		root := snapshot.roots[key]
		if root == "" {
			return nil, nil, fmt.Errorf("target %q has no frozen machine root", key.target)
		}
		info, ok := harness.TargetInfoFor(key.target)
		if !ok {
			return nil, nil, fmt.Errorf("target %q has no target metadata", key.target)
		}
		destinations := snapshot.destinations[key]
		mappedFiles := nativeAggregateBackupFilesFromDestinations(destinations)
		relPaths := append([]string(nil), info.ComponentsForScope(true)...)
		aggregates := info.AggregateFilesForScope(true)
		if len(harness.NativeAggregateInstalls(key.target)) > 0 {
			aggregates = nil
			found := false
			for _, destination := range destinations {
				if destination.Root == root {
					aggregates = append(aggregates, destination.Info.AggregateFilesForScope(true)...)
					found = true
				}
			}
			if !found {
				return nil, nil, fmt.Errorf("resolve %s native destination: machine root %q is not mapped", key.target, root)
			}
		}
		for _, rel := range aggregates {
			if !backupPathCoveredByAncestor(rel, relPaths) {
				relPaths = append(relPaths, rel)
			}
		}
		managed[key.target] = backup.ManagedPaths{Root: root, RelPaths: relPaths, MappedFiles: mappedFiles}
	}
	if outputs[outputKey{target: hcc.Target(targetmeta.TargetCodex), machineScope: true}] {
		destinations := snapshot.destinations[outputKey{target: hcc.Target(targetmeta.TargetCodex), machineScope: true}]
		manifest := snapshot.manifests[true]
		paths, err := codexMachineManagedPathsFromSnapshot(destinations, manifest.entries)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve Codex machine backup paths: %w", err)
		}
		parts := make([]backup.CodexPartition, 0, len(destinations))
		for _, destination := range destinations {
			parts = append(parts, backup.CodexPartition{ID: destination.ID, Root: destination.Root, RelPaths: paths[destination.ID]})
		}
		return managed, parts, nil
	}
	return managed, nil, nil
}

func nativeAggregateBackupFilesFromDestinations(destinations []harness.Destination) map[string]string {
	files := make(map[string]string)
	if len(destinations) == 0 {
		return files
	}
	baseRoot := destinations[0].Root
	for _, destination := range destinations[1:] {
		if len(destination.AggregateRoutes) == 0 || destination.Root == baseRoot {
			continue
		}
		for _, rel := range destination.Info.AggregateFilesForScope(true) {
			files[filepath.ToSlash(filepath.Join(".agent-marketplace-native", destination.ID, rel))] = filepath.Join(destination.Root, rel)
		}
	}
	return files
}

// compileWorkItems compiles each skill-target pair, splits artifacts by kind,
// and groups results by output key.
func compileWorkItems(work []skillTarget, allDiags []diag.Diagnostic, allOutputs map[outputKey]bool, blocked blockedOutputs, aggregateFences map[outputKey]map[ir.ResourceKind]bool, repoRoot string) compilePhaseResult {
	byTarget := make(map[outputKey]*targetWork)
	skillLinks := make(map[outputKey]map[string]*skillLinkGroup)
	artifactMeta := make(map[outputKey]map[string]skillLink)
	ownedState := make(map[outputKey]any)
	materializationDiags := make(map[outputKey][]diag.Diagnostic)
	ownedAggregates := ownedAggregateState{values: ownedState}
	markOwnedAggregateFences(ownedAggregates, aggregateFences)

	for _, w := range work {
		key := outputKey{target: w.target, machineScope: w.machineScope}
		binding, hasBinding, bindingErr := materializeAdapterFor(w.target)
		if bindingErr != nil {
			blocked.blockResource(key, w)
			allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.adapter.error", Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Name, Message: bindingErr.Error()})
			continue
		}
		var aggregateHandler ownedAggregateHandler
		if hasBinding {
			aggregateHandler = binding.handler
		}
		compileOptions := []hcc.Option{}
		if pluginRoot := sourceBundleRoot(w.skill); pluginRoot != "" {
			compileOptions = append(compileOptions, hcc.WithPluginRoot(pluginRoot))
		}
		// Resources that emit merge-with-existing JSON fragments must carry the
		// ownership marker so materialization merges into (rather than clobbers)
		// any pre-existing file. Skills and subagents contribute entries to the
		// plugin manifest (.factory-plugin/.claude-plugin plugin.json), which is
		// merged per-entry just like mcp.json and hooks.json.
		//
		// Ownership-reconciled targets are deliberately excluded. An in-file
		// `_managed_by` marker would turn file contents into proof of ownership,
		// exactly the heuristic the durable ownership store exists to avoid.
		info, knownTarget := harness.TargetInfoFor(w.target)
		if !knownTarget || !info.OwnershipReconciledAggregates {
			switch w.skill.Resource.Kind() {
			case ir.KindMcpServer, ir.KindHook, ir.KindSkill, ir.KindSubagent:
				compileOptions = append(compileOptions, hcc.WithManagedBy("_managed_by", "agent-marketplace"))
			}
		} else if w.machineScope {
			// Ownership-backed compilers use ScopeUser to select their native
			// machine aggregate layout. ScopeProject is the default, so only the
			// machine-scope pass has to say so.
			compileOptions = append(compileOptions, hcc.WithScope(hcc.ScopeUser))
		}
		result, err := hcc.Compile(w.skill.Resource, w.target, compileOptions...)
		if err != nil {
			blocked.blockResource(key, w)
			markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
			materializationDiags[key] = append(materializationDiags[key], diag.Diagnostic{
				Code:     "materialize.compile.error",
				Severity: diag.SeverityError,
				Target:   string(w.target),
				Resource: w.skill.Name,
				Message:  fmt.Sprintf("compile resource %q for target %s: %v", w.skill.Name, w.target, err),
			})
			continue
		}

		if result == nil {
			blocked.blockResource(key, w)
			markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
			continue
		}

		result.Artifacts = append([]artifact.Artifact(nil), result.Artifacts...)
		if diag.HasErrors(result.Diagnostics) {
			blocked.blockResource(key, w)
			markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
			tw := byTarget[key]
			if tw == nil {
				tw = &targetWork{}
				byTarget[key] = tw
			}
			tw.diags = append(tw.diags, result.Diagnostics...)
			continue
		}
		resourceKind := w.skill.Resource.Kind()

		// Ownership-reconciled fragments leave the generic artifact pipeline
		// before any marker-merge or whole-file plan can see them.
		if aggregateHandler != nil {
			rest, aggregateErr := aggregateHandler.extract(ownedAggregates, key, result.Artifacts, w.skill.Name)
			if aggregateErr != nil {
				blocked.blockResource(key, w)
				markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
				materializationDiags[key] = append(materializationDiags[key], aggregateHandler.malformedDiagnostic(key, w.skill.Name, aggregateErr))
				continue
			}
			result.Artifacts = rest
		}

		// For hooks, expand ${DROID_PLUGIN_ROOT} to a relative path.
		// Factory Droid only sets DROID_PLUGIN_ROOT for plugin-scoped hooks,
		// not workspace-level hooks (.factory/hooks/). By expanding to a
		// relative path at materialization time, the hook command works
		// without the env var. Factory Droid runs hooks from the repo root,
		// so ".factory/hooks/script.sh" resolves correctly.
		if resourceKind == ir.KindHook && !harness.IsInstallationAdapterTarget(w.target) {
			if roots, ok := harness.MakeInstallRoots()[w.target]; ok {
				pluginRoot := roots.For(w.machineScope, repoRoot)
				if pluginRoot != "" {
					// Use a relative path when the plugin root is inside the
					// repo (repo-scope). For machine-scope (outside the repo),
					// fall back to the absolute path.
					relPath, err := filepath.Rel(repoRoot, pluginRoot)
					if err == nil && !strings.HasPrefix(relPath, "..") {
						pluginRoot = relPath
					}
					for i := range result.Artifacts {
						result.Artifacts[i].Bytes = bytes.ReplaceAll(
							result.Artifacts[i].Bytes,
							[]byte("${DROID_PLUGIN_ROOT}"),
							[]byte(pluginRoot),
						)
					}
				}
			}
		}

		if usesSkillDir(w.target, resourceKind) {
			result.Artifacts = ensureSkillFrontmatter(w.skill, w.target, result.Artifacts)
			var compiledSkillArtifacts []artifact.Artifact
			var installDirRel string
			result.Artifacts, compiledSkillArtifacts, installDirRel = splitSkillArtifacts(w.target, w.skill.Name, result.Artifacts)
			installDirRel = machineSkillInstallDirRel(w.target, w.machineScope, installDirRel)
			if len(compiledSkillArtifacts) > 0 {
				sourceDir := materializationSourcePath(w.skill)
				if info, err := os.Stat(sourceDir); err == nil && !info.IsDir() {
					sourceDir = filepath.Dir(sourceDir)
				}
				addSkillLink(skillLinks, key, installDirRel, skillLink{
					name:      w.skill.Name,
					target:    cachedSkillDir(w.target, w.skill, compiledSkillArtifacts),
					artifacts: compiledSkillArtifacts,
					sourceDir: sourceDir,
					// The link records the real resource kind, so a `remove`
					// hint for a colliding Devin command says --kind command
					// even though the output shape is a skill directory.
					kind:         resourceKind,
					resourceName: w.skill.Name,
					machineScope: w.machineScope,
					registryURL:  w.skill.RegistryURL,
					registryPath: w.skill.RegistryPath,
				})
				if diag.HasCode(result.Diagnostics, diag.CodeSkillSupportFilesDeferred) {
					slog.Debug("skill support files delegated to materialization", "target", w.target, "resource", w.skill.Name, "source_dir", sourceDir)
					result.Diagnostics = slices.DeleteFunc(result.Diagnostics, func(d diag.Diagnostic) bool {
						return d.Code == diag.CodeSkillSupportFilesDeferred
					})
				}
			}
		} else {
			// For hooks, collect support files (scripts, etc.) from the
			// source directory and add them as regular non-fragment artifacts.
			// They will be symlinked into the hooks directory alongside the
			// merged hooks.json, and automatically pruned by staleArtifactLinks
			// (which only removes symlinks, never the regular hooks.json file)
			// when a hook is excluded.
			if resourceKind == ir.KindHook {
				result.Artifacts = append(result.Artifacts, collectHookSupportFiles(materializationSourcePath(w.skill), w.target)...)
			}
			var linked []compiledResourceLink
			result.Artifacts, linked = splitResourceArtifacts(w.target, resourceKind, w.skill.Name, result.Artifacts)
			for _, link := range linked {
				installDirRel := machineSkillInstallDirRel(w.target, w.machineScope, link.installDirRel)
				cacheDir := cachedResourceDir(w.target, resourceKind, w.skill, link.artifacts)
				addSkillLink(skillLinks, key, installDirRel, skillLink{
					name:         link.name,
					target:       filepath.Join(cacheDir, filepath.FromSlash(link.artifactRel)),
					cacheDir:     cacheDir,
					artifacts:    link.artifacts,
					kind:         resourceKind,
					resourceName: w.skill.Name,
					machineScope: w.machineScope,
					registryURL:  w.skill.RegistryURL,
					registryPath: w.skill.RegistryPath,
				})
			}
		}

		tw := byTarget[key]
		if tw == nil {
			tw = &targetWork{}
			byTarget[key] = tw
		}
		// Track metadata for non-fragment artifacts that weren't extracted into
		// skillLinks so syncArtifactLinks can generate rich collision hints.
		meta := skillLink{
			kind:         resourceKind,
			resourceName: w.skill.Name,
			machineScope: w.machineScope,
			registryURL:  w.skill.RegistryURL,
			registryPath: w.skill.RegistryPath,
		}
		if artifactMeta[key] == nil {
			artifactMeta[key] = make(map[string]skillLink)
		}
		for _, a := range result.Artifacts {
			if !a.Fragment {
				artifactMeta[key][a.Path] = meta
			}
		}
		tw.artifacts = append(tw.artifacts, result.Artifacts...)
		tw.diags = append(tw.diags, result.Diagnostics...)
	}

	return compilePhaseResult{
		allDiags:             allDiags,
		byTarget:             byTarget,
		skillLinks:           skillLinks,
		allOutputs:           allOutputs,
		blocked:              blocked,
		artifactMeta:         artifactMeta,
		ownedState:           ownedState,
		materializationDiags: materializationDiags,
	}
}

func sourceBundleRoot(resource resolver.ResolvedArtifact) string {
	source := filepath.Clean(filepath.FromSlash(compilepkg.SourceOf(resource.Resource).File))
	if source == "." || filepath.IsAbs(source) || source == ".." || strings.HasPrefix(source, ".."+string(os.PathSeparator)) {
		return ""
	}
	path := filepath.Clean(resource.Path)
	root := path
	for range strings.Split(source, string(os.PathSeparator)) {
		root = filepath.Dir(root)
	}
	if filepath.Clean(filepath.Join(root, source)) != path {
		return ""
	}
	return root
}

// usesSkillDir reports whether a resource kind materializes as a skill DIRECTORY
// (skills/<name>/ plus support files) for a target. It is the single lookup that
// routes a Devin command through the same directory-symlink path as a skill,
// per TargetInfo.SkillDirKinds.
func usesSkillDir(target hcc.Target, kind ir.ResourceKind) bool {
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return kind == ir.KindSkill
	}
	return info.UsesSkillDir(kind)
}

// markOwnedAggregateFences carries resolver-level malformed selections into
// the ownership reconciler. Such a selection has no work item to reach the
// compile-time skip paths, but its aggregate contribution is still incomplete
// and must not make a stale owned entry look removed from configuration.
func markOwnedAggregateFences(state ownedAggregateState, fences map[outputKey]map[ir.ResourceKind]bool) {
	for key, kinds := range fences {
		handler := ownedAggregateHandlerFor(key.target)
		if handler == nil {
			continue
		}
		for kind := range kinds {
			handler.markIncomplete(state, key, kind)
		}
	}
}

// markOwnedAggregateIncomplete records that a skipped resource may have
// contributed to one of its target's shared files. The concrete handler keeps
// suppression at the narrowest format-specific file boundary.
func markOwnedAggregateIncomplete(handler ownedAggregateHandler, state ownedAggregateState, key outputKey, w skillTarget) {
	if handler == nil || w.skill.Resource == nil {
		return
	}
	handler.markIncomplete(state, key, w.skill.Resource.Kind())
}

// executeMaterializePlans writes compiled artifacts to disk and syncs skill links.
//
// ctx reaches the owned-aggregate handlers because their per-canonical-root
// critical sections must abandon a contended wait when the run is cancelled.
func executeMaterializePlans(ctx context.Context, compileResult compilePhaseResult, snapshot executionSnapshot, dryRun bool, repoRoot string, modes config.MaterializationModes) *Report {
	report := &Report{
		PerTarget: make(map[hcc.Target]TargetReport),
		Diags:     compileResult.allDiags,
	}
	for key, diagnostics := range compileResult.materializationDiags {
		appendTargetDiagnostics(report, key, "", diagnostics)
	}
	if snapshot.preflightDiag != nil {
		appendTargetDiagnostic(report, snapshot.preflightDiag.key, snapshot.preflightDiag.rootID, snapshot.preflightDiag.Diagnostic, snapshot.preflightDiag.path)
		return report
	}
	states := make(map[bool]*portableState, 2)
	for _, machineScope := range []bool{false, true} {
		manifest := snapshot.manifests[machineScope]
		entries := make(map[string]portableManifestEntry, len(manifest.entries))
		for key, entry := range manifest.entries {
			entries[key] = entry
		}
		states[machineScope] = &portableState{
			path: manifest.path, repoRoot: repoRoot, machineScope: machineScope, entries: entries,
			destinations: snapshot.destinations[outputKey{target: hcc.Target(targetmeta.TargetCodex), machineScope: machineScope}],
		}
	}

	_, adapterErr := newMaterializeAdapterComposition()
	if adapterErr != nil {
		for key := range compileResult.allOutputs {
			if harness.IsInstallationAdapterTarget(key.target) {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.adapter.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("construct installation adapter registry: %v", adapterErr)})
			}
		}
		return report
	}
	// Sort output keys for deterministic PerRoot ordering.
	sortedOutputs := make([]outputKey, 0, len(compileResult.allOutputs))
	for key := range compileResult.allOutputs {
		sortedOutputs = append(sortedOutputs, key)
	}
	slices.SortFunc(sortedOutputs, func(a, b outputKey) int {
		if a.target != b.target {
			return strings.Compare(string(a.target), string(b.target))
		}
		if a.machineScope == b.machineScope {
			return 0
		}
		if a.machineScope {
			return 1
		}
		return -1
	})

	for _, key := range sortedOutputs {
		outDir := snapshot.roots[key]
		if outDir == "" {
			appendTargetDiagnostics(report, key, "", []diag.Diagnostic{{Code: "materialize.target.unknown", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("no frozen output root for target %q", key.target)}})
			continue
		}
		if !dryRun {
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				appendTargetDiagnostics(report, key, "", []diag.Diagnostic{{Code: "materialize.mkdir.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("create output directory %s: %v", outDir, err)}})
				continue
			}
		}
		tw := compileResult.byTarget[key]
		if tw != nil {
			appendTargetDiagnostics(report, key, "", tw.diags)
		}
		mode := modes.For(string(key.target), key.machineScope)
		slog.Debug("materialize target", "target", key.target, "machine_scope", key.machineScope, "mode", mode, "out_dir", outDir)
		executeNativeAggregatePlans(ctx, report, key, snapshot.nativeAggregates[key], dryRun)
		report = executeTargetPlans(report, key, outDir, snapshot.materializePlans[key], states[key.machineScope], compileResult.skillLinks, compileResult.artifactMeta, compileResult.blocked.componentsFor(key), dryRun, mode, repoRoot, snapshot.codexPartitions, snapshot.pruneFences[key])
		if aggregateHandler := ownedAggregateHandlerFor(key.target); aggregateHandler != nil {
			report = aggregateHandler.reconcile(ctx, report, compileResult.ownedAggregateState(), key, outDir, snapshot.destinations[key], dryRun)

		}
	}
	report.rebuildIssues()
	return report
}

// executeTargetPlans writes artifact plans and skill links for a single target.
func executeTargetPlans(report *Report, key outputKey, outDir string, plans []materializePlan, state *portableState, skillLinks map[outputKey]map[string]*skillLinkGroup, artifactMeta map[outputKey]map[string]skillLink, blocked map[string]bool, dryRun bool, mode config.MaterializationMode, repoRoot string, coalescedCodexPartitions map[string]bool, pruneFence map[string]bool) *Report {
	fileDesired := make(map[string]bool)
	// Track which plan output directories had merge artifacts, so we can
	// prune managed merge entries from directories NOT covered by any plan.
	dirsCoveredByMergePlans := make(map[string]bool)
	// File mode must also prune stale cache-backed symlinks from every regular
	// component directory, including components that have no desired artifacts.
	portableCleanupDirs := make(map[string]string)

	for _, plan := range plans {
		if plan.rootID == "" {
			plan.rootID = materializationRootID(key)
		}
		regularArtifacts, mergeArtifacts := splitMergeArtifacts(plan.artifacts)
		if mode == config.MaterializationModeFile {
			planRoot := outDir
			if plan.root != "" {
				planRoot = plan.root
			}
			rel, err := filepath.Rel(planRoot, plan.outDir)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				appendTargetDiagnostic(report, key, plan.rootID, diag.Diagnostic{Code: "materialize.mkdir.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("unsafe portable output directory %s", plan.outDir)}, plan.reportPrefix)
				continue
			}
			validate := validatePortableOutputDir
			if !dryRun {
				validate = ensurePortableOutputDir
			}
			if _, err := validate(planRoot, rel); err != nil {
				appendTargetDiagnostic(report, key, plan.rootID, diag.Diagnostic{
					Code:     "materialize.mkdir.error",
					Severity: diag.SeverityError,
					Target:   string(key.target),
					Message:  fmt.Sprintf("validate portable output directory %s: %v", plan.outDir, err),
				}, plan.reportPrefix)
				continue
			}
			portableCleanupDirs[plan.outDir] = plan.reportPrefix
		} else if !dryRun {
			if err := os.MkdirAll(plan.outDir, 0o755); err != nil {
				appendTargetDiagnostic(report, key, plan.rootID, diag.Diagnostic{
					Code:     "materialize.mkdir.error",
					Severity: diag.SeverityError,
					Target:   string(key.target),
					Message:  fmt.Sprintf("create output directory %s: %v", plan.outDir, err),
				}, plan.reportPrefix)
				continue
			}
		}

		cacheMachineScope, cacheOutDir := key.machineScope, outDir
		if key.target == hcc.Target(targetmeta.TargetCodex) && coalescedCodexPartitions[codexPlanPartitionKey(plan)] {
			// A coalesced physical partition must also have one cache target;
			// otherwise identical repo/machine bytes produce different symlink
			// targets and rewrite the destination twice.
			cacheMachineScope, cacheOutDir = false, plan.root
		}
		cacheDir := cachedPlanDir(key.target, cacheMachineScope, cacheOutDir, plan.reportPrefix, plan.artifacts)
		if matErr := prepareArtifactLinkCache(cacheDir, regularArtifacts, dryRun); matErr != nil {
			appendTargetDiagnostic(report, key, plan.rootID, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("prepare artifact cache for target %s: %v", key.target, matErr),
			}, plan.reportPrefix)
			continue
		}

		// Don't prune via syncArtifactLinks when all artifacts for this
		// component are merge fragments (e.g. hooks). Pruning an empty
		// regularArtifacts set against a non-empty directory would delete
		// files that materializeMergeArtifacts is about to write,
		// causing an infinite write-then-delete cycle on every sync.
		// Empty components (no artifacts at all) still prune normally so
		// stale directories are cleaned up when a resource is removed.
		wantPrune := plan.prune || plan.mode == hcc.WriteModeSync
		if wantPrune && len(regularArtifacts) == 0 && len(mergeArtifacts) > 0 {
			wantPrune = false
		}
		planCollisionMeta := make(map[string]skillLink, len(regularArtifacts))
		for _, a := range regularArtifacts {
			if meta, ok := artifactMeta[key][a.Path]; ok {
				planCollisionMeta[a.Path] = meta
				continue
			}
			if plan.reportPrefix != "" {
				fullPath := filepath.ToSlash(filepath.Join(plan.reportPrefix, a.Path))
				if meta, ok := artifactMeta[key][fullPath]; ok {
					planCollisionMeta[a.Path] = meta
				}
			}
		}
		var writeReport *hcc.WriteReport
		var writeDiags []diag.Diagnostic
		var matErr error
		if mode == config.MaterializationModeFile {
			writeReport, writeDiags, matErr = syncPortableArtifactFiles(plan.outDir, regularArtifacts, cacheDir, dryRun, wantPrune, planCollisionMeta, state, key.target, plan.reportPrefix, fileDesired)
		} else {
			writeReport, writeDiags, matErr = syncArtifactLinks(plan.outDir, regularArtifacts, cacheDir, dryRun, wantPrune, planCollisionMeta, portableSyncContext{state: state, target: key.target, reportPrefix: plan.reportPrefix, desired: fileDesired})
		}
		if matErr != nil {
			appendTargetDiagnostic(report, key, plan.rootID, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("materialize target %s to %s: %v", key.target, plan.outDir, matErr),
			}, plan.reportPrefix)
			continue
		}
		if len(writeDiags) > 0 {
			for i := range writeDiags {
				writeDiags[i].Target = string(key.target)
			}
			appendTargetDiagnostics(report, key, plan.rootID, writeDiags)
		}
		mergeReport, mergeDiags, mergeErr := materializeMergeArtifacts(plan.outDir, mergeArtifacts, dryRun, wantPrune)
		if len(mergeArtifacts) > 0 {
			dirsCoveredByMergePlans[plan.outDir] = true
		}
		// Per-file prune refusals are reported even if the merge itself also
		// failed: each names a different file.
		appendTargetDiagnostics(report, key, plan.rootID, retargetPruneDiags(mergeDiags, key.target, plan.reportPrefix))
		if mergeErr != nil {
			appendTargetDiagnostic(report, key, plan.rootID, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("merge target %s config in %s: %v", key.target, plan.outDir, mergeErr),
			}, plan.reportPrefix)
			continue
		}
		writeReport.Written = append(writeReport.Written, mergeReport.Written...)
		writeReport.Skipped = append(writeReport.Skipped, mergeReport.Skipped...)
		writeReport.Removed = append(writeReport.Removed, mergeReport.Removed...)

		tr := TargetReport{
			Target:         key.target,
			MachineScope:   key.machineScope,
			RootID:         plan.rootID,
			OutDir:         writeReport.OutDir,
			Written:        writeReport.Written,
			Skipped:        writeReport.Skipped,
			Removed:        writeReport.Removed,
			ResourceByPath: resourceMapForPaths(append(append(append([]string{}, writeReport.Written...), writeReport.Skipped...), writeReport.Removed...), plan.reportPrefix, artifactMeta[key]),
		}
		tr.buildIssues()
		report.PerRoot = append(report.PerRoot, tr)
		rootReport := tr
		if key.target != hcc.Target(targetmeta.TargetCodex) {
			rootReport.OutDir = outDir
		}
		rootReport.Written = prefixReportPaths(plan.reportPrefix, writeReport.Written)
		rootReport.Skipped = prefixReportPaths(plan.reportPrefix, writeReport.Skipped)
		rootReport.Removed = prefixReportPaths(plan.reportPrefix, writeReport.Removed)
		report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], rootReport)
	}

	// Resource directories are normally also regular artifact components, but
	// register them explicitly so a target with a direct-resource-only install
	// directory is still cleaned on an empty desired run.
	if mode == config.MaterializationModeFile {
		if info, ok := harness.TargetInfoFor(key.target); ok {
			for _, installDirRel := range info.ResourceDirs {
				installDirRel = machineSkillInstallDirRel(key.target, key.machineScope, installDirRel)
				dir := filepath.Join(outDir, filepath.FromSlash(installDirRel))
				exists, err := outputDirExists(dir)
				if err != nil {
					report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("inspect portable resource cleanup directory %s: %v", dir, err)})
					continue
				}
				if exists {
					if _, err := validatePortableOutputDir(outDir, installDirRel); err != nil {
						appendTargetDiagnostic(report, key, codexRootIDForPrefix(key, installDirRel), diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("validate portable resource cleanup directory %s: %v", dir, err)}, installDirRel)
						continue
					}
					portableCleanupDirs[dir] = filepath.ToSlash(installDirRel)
				}
			}
		}
	}

	// Prune managed merge entries from directories that were not covered by
	// any plan's merge artifacts. This handles the deinit case where the
	// resolved set is empty and no plans exist for root-level directories
	// that contain merge files (like mcp.json). Directories already covered
	// by a merge-artifact plan are handled by materializeMergeArtifacts.
	pruneOutDir := outDir
	pruneReportPrefix := ""
	if key.target == hcc.TargetWindsurf && !key.machineScope {
		pruneOutDir = filepath.Join(outDir, windsurfDir)
		pruneReportPrefix = windsurfDir
	}
	var pruneReport *hcc.WriteReport
	var pruneDiags []diag.Diagnostic
	// Root-level managed merge files belong to no component, so only the
	// wildcard block reaches them.
	if key.target == hcc.Target(targetmeta.TargetCodex) {
		pruneReport = &hcc.WriteReport{OutDir: pruneOutDir}
	} else if !componentBlocked(blocked, "") {
		pruneReport, pruneDiags = pruneUncoveredManagedMergeFiles(pruneOutDir, dirsCoveredByMergePlans, dryRun, pruneFence)
	} else {
		pruneReport = &hcc.WriteReport{OutDir: pruneOutDir}
	}
	// A file this walk refused is a file we could not PRUNE, which is not the
	// same thing as a resource we could not COMPILE. `blocked` (bead
	// agent-marketplace-5exj.20) suppresses pruning because a resource skipped
	// this run is indistinguishable from one that was removed from config; a
	// prune refusal makes nothing ambiguous — we know exactly what we wanted to
	// do and the destination's own syntax stopped us — and the refused file was
	// left byte-identical, so there is nothing to suppress. It therefore blocks
	// no component and costs no sibling its prune; it only reports itself.
	appendTargetDiagnostics(report, key, codexRootIDForPrefix(key, pruneReportPrefix), retargetPruneDiags(pruneDiags, key.target, pruneReportPrefix))
	if len(pruneReport.Written) > 0 || len(pruneReport.Removed) > 0 {
		tr := TargetReport{
			Target:       key.target,
			MachineScope: key.machineScope,
			OutDir:       pruneOutDir,
			Written:      pruneReport.Written,
			Removed:      pruneReport.Removed,
		}
		report.PerRoot = append(report.PerRoot, tr)
		rootReport := tr
		rootReport.OutDir = outDir
		rootReport.Written = prefixReportPaths(pruneReportPrefix, pruneReport.Written)
		rootReport.Removed = prefixReportPaths(pruneReportPrefix, pruneReport.Removed)
		report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], rootReport)
	}

	// Track file-copy component dirs handled by a group so the uncovered
	// prune pass below doesn't double-process them.
	copyDirsCovered := make(map[string]bool)

	for _, group := range orderedSkillLinkGroups(skillLinks[key]) {
		groupDir := filepath.Join(outDir, filepath.FromSlash(group.installDirRel))
		if mode == config.MaterializationModeFile {
			validate := validatePortableOutputDir
			if !dryRun {
				validate = ensurePortableOutputDir
			}
			if _, err := validate(outDir, group.installDirRel); err != nil {
				appendTargetDiagnostic(report, key, codexRootIDForPrefix(key, group.reportPrefix), diag.Diagnostic{Code: "materialize.mkdir.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("validate portable resource directory %s: %v", groupDir, err)}, group.reportPrefix)
				continue
			}
			portableCleanupDirs[groupDir] = group.reportPrefix
		}
		symlinkLinks, copyLinks := partitionFileCopyLinks(key.target, group.links)

		if len(symlinkLinks) > 0 {
			if err := prepareSkillLinkCaches(symlinkLinks, dryRun); err != nil {
				appendTargetDiagnostic(report, key, codexRootIDForPrefix(key, group.reportPrefix), diag.Diagnostic{
					Code:     "materialize.write.error",
					Severity: diag.SeverityError,
					Target:   string(key.target),
					Message:  fmt.Sprintf("prepare skill cache for target %s: %v", key.target, err),
				}, group.reportPrefix)
				continue
			}
			var tr TargetReport
			var err error
			if mode == config.MaterializationModeFile {
				tr, err = syncPortableSkillTrees(groupDir, symlinkLinks, dryRun, state, key.target, group.reportPrefix, fileDesired)
			} else {
				tr, err = syncSkillLinks(groupDir, symlinkLinks, dryRun, !componentBlocked(blocked, group.installDirRel), portableSyncContext{state: state, target: key.target, reportPrefix: group.reportPrefix, desired: fileDesired})
			}
			report = appendGroupReport(report, key, outDir, group.reportPrefix, tr, err, "materialize target %s to %s")
			if err != nil {
				continue
			}
		}

		if len(copyLinks) > 0 {
			copyDirsCovered[groupDir] = true
			tr, err := syncCommandFiles(groupDir, copyLinks, dryRun, !componentBlocked(blocked, group.installDirRel))
			report = appendGroupReport(report, key, outDir, group.reportPrefix, tr, err, "copy command files for target %s to %s")
		}
	}

	// Prune managed file-copy resources (e.g. Factory commands) from their
	// install dirs when no group covered them this run — this handles the
	// removal case where the resolved set no longer contains the resource.
	// Regular files are never pruned by the symlink-oriented artifact plan,
	// so they must be cleaned up here.
	report = pruneUncoveredFileCopyDirs(report, key, outDir, copyDirsCovered, blocked, dryRun)

	// The two manifest-driven prune passes below walk the target's whole
	// recorded state rather than one component directory, so they cannot be
	// narrowed per component and stay suppressed by any skip.
	if mode == config.MaterializationModeFile && !anyBlocked(blocked) {
		report = prunePortableStaleSymlinks(report, key, outDir, portableCleanupDirs, fileDesired, dryRun)
	}

	var portableReports []TargetReport
	var portableDiags []diag.Diagnostic
	var portableErr error
	if !anyBlocked(blocked) {
		portableReports, portableDiags, portableErr = prunePortableEntries(state, key, outDir, repoRoot, fileDesired, dryRun)
	}
	if portableErr != nil {
		slog.Error("materialize manifest prune failed", "target", key.target, "machine_scope", key.machineScope, "error", portableErr)
		appendTargetDiagnostics(report, key, "", []diag.Diagnostic{{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: portableErr.Error()}})
	} else {
		for i := range portableDiags {
			portableDiags[i].Target = string(key.target)
		}
		appendTargetDiagnostics(report, key, "", portableDiags)
		for _, tr := range portableReports {
			report.PerRoot = append(report.PerRoot, tr)
			report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], tr)
		}
	}
	if err := state.save(dryRun); err != nil {
		slog.Error("materialize manifest save failed", "target", key.target, "machine_scope", key.machineScope, "error", err)
		appendTargetDiagnostics(report, key, "", []diag.Diagnostic{{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()}})
	} else {
		slog.Debug("materialize manifest synchronized", "target", key.target, "machine_scope", key.machineScope, "entries", len(state.entries), "path", state.path)
	}

	return report
}

// pruneUncoveredFileCopyDirs removes orphaned managed file-copy resources from
// the target's file-copy install dirs that were not handled by any copy group.
func pruneUncoveredFileCopyDirs(report *Report, key outputKey, outDir string, covered map[string]bool, blocked map[string]bool, dryRun bool) *Report {
	info, ok := harness.TargetInfoFor(key.target)
	if !ok || len(info.FileCopyKinds) == 0 {
		return report
	}
	seen := make(map[string]bool)
	for kind := range info.FileCopyKinds {
		installDirRel := info.ResourceInstallDir(kind)
		if installDirRel == "" {
			continue
		}
		if componentBlocked(blocked, installDirRel) {
			continue
		}
		dir := filepath.Join(outDir, filepath.FromSlash(installDirRel))
		exists, err := outputDirExists(dir)
		if err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("inspect managed command directory %s: %v", dir, err),
			})
			continue
		}
		if covered[dir] || seen[dir] || !exists {
			continue
		}
		seen[dir] = true
		tr := TargetReport{OutDir: dir}
		if err := pruneStaleCommandFiles(dir, nil, dryRun, &tr); err != nil {
			appendTargetDiagnostic(report, key, codexRootIDForPrefix(key, installDirRel), diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("prune managed command files in %s: %v", dir, err),
			}, installDirRel)
			continue
		}
		sort.Strings(tr.Removed)
		report = appendGroupReport(report, key, outDir, installDirRel, tr, nil, "")
	}
	return report
}

// partitionFileCopyLinks splits a group's links into those materialized as
// symlinks (default) and those materialized as real files (file-copy), based
// on the target's TargetInfo.FileCopyKinds.
func partitionFileCopyLinks(target hcc.Target, links []skillLink) (symlinkLinks, copyLinks []skillLink) {
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return links, nil
	}
	for _, link := range links {
		if info.UsesFileCopy(link.kind) {
			copyLinks = append(copyLinks, link)
		} else {
			symlinkLinks = append(symlinkLinks, link)
		}
	}
	return symlinkLinks, copyLinks
}

// appendGroupReport folds a skill-link/copy group TargetReport into the
// aggregate Report, stamping the target/scope and prefixing report paths.
func appendGroupReport(report *Report, key outputKey, outDir, reportPrefix string, tr TargetReport, err error, errFormat string) *Report {
	tr.Target = key.target
	tr.MachineScope = key.machineScope
	if tr.RootID == "" {
		tr.RootID = materializationRootID(key)
		if codexRootID := codexRootIDForPrefix(key, reportPrefix); codexRootID != "" {
			tr.RootID = codexRootID
		}
	}
	if err != nil {
		appendTargetDiagnostic(report, key, tr.RootID, diag.Diagnostic{
			Code:     "materialize.write.error",
			Severity: diag.SeverityError,
			Target:   string(key.target),
			Message:  fmt.Sprintf(errFormat+": %v", key.target, tr.OutDir, err),
		}, reportPrefix)
		return report
	}
	tr.buildIssues()
	report.PerRoot = append(report.PerRoot, tr)
	if len(tr.Diags) > 0 {
		for i := range tr.Diags {
			tr.Diags[i].Target = string(key.target)
		}
		report.Diags = append(report.Diags, tr.Diags...)
	}
	rootReport := tr
	rootReport.OutDir = outDir
	rootReport.Written = prefixReportPaths(reportPrefix, tr.Written)
	rootReport.Skipped = prefixReportPaths(reportPrefix, tr.Skipped)
	rootReport.Removed = prefixReportPaths(reportPrefix, tr.Removed)
	report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], rootReport)
	return report
}

func codexRootIDForPrefix(key outputKey, rel string) string {
	if key.target != hcc.Target(targetmeta.TargetCodex) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	suffix := ""
	switch {
	case rel == ".agents" || strings.HasPrefix(rel, ".agents/"):
		suffix = "agents"
	case rel == ".codex" || strings.HasPrefix(rel, ".codex/"):
		suffix = "codex"
	default:
		return ""
	}
	scope := "repo"
	if key.machineScope {
		scope = "machine"
	}
	return scope + "-" + suffix
}

func materializePlans(target hcc.Target, outDir string, artifacts []artifact.Artifact, linkedSkills map[string]bool, machineScope bool) []materializePlan {
	return materializePlansWithDestinations(target, outDir, artifacts, linkedSkills, machineScope, nil)
}

func materializePlansWithDestinations(target hcc.Target, outDir string, artifacts []artifact.Artifact, linkedSkills map[string]bool, machineScope bool, destinations []harness.Destination) []materializePlan {
	if target == hcc.Target(targetmeta.TargetCodex) {
		if destinations == nil {
			return codexMaterializePlans(outDir, artifacts, linkedSkills, machineScope)
		}
		return codexMaterializePlansForDestinations(outDir, artifacts, linkedSkills, destinations)
	}
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return subtreeMaterializePlans(outDir, artifacts, generatedComponents(target), artifactRootPrefix(target), noPruneComponents(target, linkedSkills))
	}
	if info.PlanStrategy == harness.PlanWindsurf {
		return windsurfMaterializePlans(outDir, artifacts, machineScope, noPruneComponents(target, linkedSkills))
	}
	return subtreeMaterializePlans(outDir, artifacts, info.Components, info.ArtifactRootPrefix, noPruneComponents(target, linkedSkills))
}

func codexMaterializePlans(repoRoot string, artifacts []artifact.Artifact, linkedSkills map[string]bool, machineScope bool) []materializePlan {
	destinations, err := harness.ResolveDestinations(hcc.Target(targetmeta.TargetCodex), machineScope, repoRoot)
	if err != nil {
		slog.Error("resolve Codex materialization destinations", "code", "materialize.destination.error", "error", err)
		return nil
	}
	return codexMaterializePlansForDestinations(repoRoot, artifacts, linkedSkills, destinations)
}

func codexMaterializePlansForDestinations(repoRoot string, artifacts []artifact.Artifact, linkedSkills map[string]bool, destinations []harness.Destination) []materializePlan {
	var plans []materializePlan
	for _, destination := range destinations {
		var routed []artifact.Artifact
		prefix := strings.TrimSuffix(filepath.ToSlash(destination.Prefix), "/") + "/"
		for _, candidate := range artifacts {
			if !strings.HasPrefix(filepath.ToSlash(candidate.Path), prefix) {
				continue
			}
			rel, routeErr := destination.RouteArtifact(candidate.Path)
			if routeErr != nil {
				slog.Error("route Codex artifact", "code", "materialize.destination.error", "destination", destination.ID, "error", routeErr)
				continue
			}
			candidate.Path = rel
			routed = append(routed, candidate)
		}
		destinationPlans := subtreeMaterializePlans(destination.Root, routed, destination.Info.Components, destination.Info.ArtifactRootPrefix, noPruneComponents(hcc.Target(targetmeta.TargetCodex), linkedSkills))
		for i := range destinationPlans {
			destinationPlans[i].root = destination.Root
			destinationPlans[i].rootID = destination.ID
			destinationPlans[i].reportPrefix = filepath.ToSlash(filepath.Join(destination.Prefix, destinationPlans[i].reportPrefix))
		}
		plans = append(plans, destinationPlans...)
	}
	return plans
}

func subtreeMaterializePlans(outDir string, artifacts []artifact.Artifact, components []string, rootPrefix string, noPrune map[string]bool) []materializePlan {
	var root []artifact.Artifact
	scoped := make(map[string][]artifact.Artifact)
	for _, a := range artifacts {
		a.Path = trimArtifactRootPrefix(a.Path, rootPrefix)
		component, rest, ok := strings.Cut(a.Path, "/")
		if ok {
			a.Path = rest
			scoped[component] = append(scoped[component], a)
		} else {
			root = append(root, a)
		}
	}

	var plans []materializePlan
	for _, component := range orderedComponents(components, scoped) {
		componentOutDir := filepath.Join(outDir, component)
		if len(scoped[component]) == 0 {
			if _, err := os.Stat(componentOutDir); err != nil {
				continue
			}
		}
		plans = append(plans, materializePlan{
			outDir:       componentOutDir,
			artifacts:    scoped[component],
			mode:         writeModeForComponent(component, noPrune),
			prune:        !noPrune[component],
			reportPrefix: component,
		})
	}
	if len(root) > 0 {
		plans = append(plans, materializePlan{
			outDir:    outDir,
			artifacts: root,
			mode:      hcc.WriteModeOverwrite,
			prune:     false,
		})
	}
	return plans
}

func generatedComponents(target hcc.Target) []string {
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return nil
	}
	return info.Components
}

func writeModeForComponent(component string, noPrune map[string]bool) hcc.WriteMode {
	if noPrune[component] {
		return hcc.WriteModeOverwrite
	}
	return hcc.WriteModeSync
}

func noPruneComponents(_ hcc.Target, linkedComponents map[string]bool) map[string]bool {
	return linkedComponents
}

func artifactRootPrefix(target hcc.Target) string {
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return ""
	}
	return info.ArtifactRootPrefix
}

func trimArtifactRootPrefix(path, rootPrefix string) string {
	if rootPrefix == "" {
		return path
	}
	return strings.TrimPrefix(path, rootPrefix+"/")
}

func orderedComponents(components []string, scoped map[string][]artifact.Artifact) []string {
	seen := make(map[string]bool, len(components)+len(scoped))
	ordered := make([]string, 0, len(components)+len(scoped))
	for _, component := range components {
		seen[component] = true
		ordered = append(ordered, component)
	}
	var extra []string
	for component := range scoped {
		if !seen[component] {
			extra = append(extra, component)
		}
	}
	sort.Strings(extra)
	return append(ordered, extra...)
}

func windsurfMaterializePlans(outDir string, artifacts []artifact.Artifact, machineScope bool, noPrune map[string]bool) []materializePlan {
	var root []artifact.Artifact
	scoped := make(map[string][]artifact.Artifact)
	for _, a := range artifacts {
		if strings.HasPrefix(a.Path, windsurfDir+"/") {
			a.Path = strings.TrimPrefix(a.Path, windsurfDir+"/")
			component, rest, ok := strings.Cut(a.Path, "/")
			if ok {
				a.Path = rest
				scoped[component] = append(scoped[component], a)
			} else {
				root = append(root, a)
			}
			continue
		}
		root = append(root, a)
	}

	var plans []materializePlan
	for _, component := range orderedComponents(generatedComponents(hcc.TargetWindsurf), scoped) {
		componentArtifacts := scoped[component]
		componentOutDir := filepath.Join(outDir, windsurfDir, component)
		reportPrefix := filepath.Join(windsurfDir, component)
		// pruneKey identifies the component for prune/no-prune lookups. It must
		// match the install-dir name used by linked resources (see
		// machineSkillInstallDirRel / linkedComponents) so we never prune a
		// directory that holds symlinked workflows.
		pruneKey := component
		// For machine-scoped Windsurf, the outDir is already
		// ~/.codeium/windsurf — skip the .windsurf/ nesting. Global workflows
		// additionally live under global_workflows/ rather than workflows/,
		// since Windsurf only discovers machine-wide slash commands there.
		if machineScope {
			dirName := machineComponentDir(hcc.TargetWindsurf, component)
			componentOutDir = filepath.Join(outDir, dirName)
			reportPrefix = dirName
			pruneKey = dirName
		}
		if len(componentArtifacts) == 0 {
			if _, err := os.Stat(componentOutDir); err != nil {
				continue
			}
		}
		plans = append(plans, materializePlan{
			outDir:       componentOutDir,
			artifacts:    componentArtifacts,
			mode:         writeModeForComponent(pruneKey, noPrune),
			prune:        !noPrune[pruneKey],
			reportPrefix: reportPrefix,
		})
	}
	if len(root) > 0 {
		// For repo-scoped Windsurf, root artifacts (like mcp_config.json)
		// must go under .windsurf/ — the repo root itself is not the
		// Windsurf config directory. Machine-scoped outDir is already
		// ~/.codeium/windsurf, so no prefix is needed.
		rootOutDir := outDir
		rootReportPrefix := ""
		if !machineScope {
			rootOutDir = filepath.Join(outDir, windsurfDir)
			rootReportPrefix = windsurfDir
		}
		plans = append(plans, materializePlan{
			outDir:       rootOutDir,
			artifacts:    root,
			mode:         hcc.WriteModeOverwrite,
			prune:        false,
			reportPrefix: rootReportPrefix,
		})
	}
	return plans
}

func prefixReportPaths(prefix string, paths []string) []string {
	if prefix == "" || len(paths) == 0 {
		return paths
	}
	prefixed := make([]string, len(paths))
	for i, path := range paths {
		prefixed[i] = filepath.Join(prefix, path)
	}
	return prefixed
}

func splitSkillArtifacts(target hcc.Target, skillName string, artifacts []artifact.Artifact) ([]artifact.Artifact, []artifact.Artifact, string) {
	var rest []artifact.Artifact
	var skillArtifacts []artifact.Artifact
	var installDirRel string
	for _, a := range artifacts {
		a.Path = trimArtifactRootPrefix(a.Path, artifactRootPrefix(target))
		currentInstallDirRel, skillRel, ok := splitSkillArtifactPath(target, skillName, a.Path)
		if !ok {
			rest = append(rest, a)
			continue
		}
		a.Path = skillRel
		skillArtifacts = append(skillArtifacts, a)
		if installDirRel == "" {
			installDirRel = currentInstallDirRel
		}
	}
	return rest, skillArtifacts, installDirRel
}

func machineSkillInstallDirRel(target hcc.Target, machineScope bool, installDirRel string) string {
	if target != hcc.TargetWindsurf || !machineScope {
		return installDirRel
	}
	// The Windsurf Machine root is ~/.codeium/windsurf, so strip the
	// .windsurf/ prefix that the compiler adds — the base directory
	// already represents the Windsurf config root.
	rel := installDirRel
	if strings.HasPrefix(rel, windsurfDir+"/") {
		rel = strings.TrimPrefix(rel, windsurfDir+"/")
	}
	// Machine-wide (global) workflows live in global_workflows/, not
	// workflows/ — Windsurf only discovers global slash commands there.
	return machineComponentDir(target, rel)
}

// machineComponentDir maps a repo-scope component/install directory name to the
// directory name used at machine scope, using the shared harness target
// metadata (harness.TargetInfo.MachineComponentDirs) so materialization and
// backup managed-path collection can never drift apart.
func machineComponentDir(target hcc.Target, component string) string {
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return component
	}
	return info.ComponentDirForScope(component, true)
}

func ensureSkillFrontmatter(s resolver.ResolvedArtifact, target hcc.Target, artifacts []artifact.Artifact) []artifact.Artifact {
	if s.Resource == nil {
		return artifacts
	}
	skillResource, ok := s.Resource.(*ir.Skill)
	if !ok {
		return artifacts
	}

	info, ok := harness.TargetInfoFor(target)
	if !ok || info.SkillPathPattern == "" {
		return artifacts
	}

	skillPath := fmt.Sprintf(info.SkillPathPattern, s.Name)
	for i := range artifacts {
		if artifacts[i].Path != skillPath || hasFrontmatter(artifacts[i].Bytes) {
			continue
		}
		artifacts[i].Bytes = []byte(renderSkillFrontmatter(skillResource) + string(artifacts[i].Bytes))
	}
	return artifacts
}

func hasFrontmatter(content []byte) bool {
	return strings.HasPrefix(strings.TrimLeft(string(content), "\ufeff\n\r\t "), "---\n")
}

func renderSkillFrontmatter(s *ir.Skill) string {
	frontmatter := struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}{
		Name:        s.Name(),
		Description: s.Description,
	}
	data, err := yaml.Marshal(frontmatter)
	if err != nil {
		return fmt.Sprintf("---\nname: %q\ndescription: %q\n---\n\n", s.Name(), s.Description)
	}
	return "---\n" + string(data) + "---\n\n"
}

// splitSkillArtifactPath splits a skill-directory artifact path into the
// install directory (the resource root the target's native layout declares
// for skills — including a partition prefix, e.g. Codex's ".agents/skills")
// and the path within the named resource's directory. Anchoring on the
// declared root keeps the split coupled to the topology authority instead of
// a literal element; targets without a registered layout keep the historical
// scan for a "skills" element.
func splitSkillArtifactPath(target hcc.Target, skillName, path string) (string, string, bool) {
	if root, ok := targetmeta.NativeResourceRoot(target, ir.KindSkill); ok && root != "." {
		prefix := root + "/" + skillName + "/"
		if artifactRel, found := strings.CutPrefix(path, prefix); found && artifactRel != "" {
			return root, artifactRel, true
		}
		return "", "", false
	}
	parts := strings.Split(path, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] != "skills" || parts[i+1] != skillName {
			continue
		}
		installDirRel := strings.Join(parts[:i+1], "/")
		artifactRel := strings.Join(parts[i+2:], "/")
		if installDirRel == "" || artifactRel == "" {
			return "", "", false
		}
		return installDirRel, artifactRel, true
	}
	return "", "", false
}

func splitResourceArtifacts(target hcc.Target, kind ir.ResourceKind, resourceName string, artifacts []artifact.Artifact) ([]artifact.Artifact, []compiledResourceLink) {
	installDirRel := resourceInstallDirRel(target, kind)
	if installDirRel == "" {
		return artifacts, nil
	}
	var rest []artifact.Artifact
	var links []compiledResourceLink
	prefix := installDirRel + "/"
	for _, a := range artifacts {
		if a.Fragment {
			rest = append(rest, a)
			continue
		}
		a.Path = trimArtifactRootPrefix(a.Path, artifactRootPrefix(target))
		artifactRel, ok := strings.CutPrefix(a.Path, prefix)
		if !ok || !resourceArtifactMatches(resourceName, artifactRel) {
			rest = append(rest, a)
			continue
		}
		a.Path = artifactRel
		links = append(links, compiledResourceLink{
			name:          artifactRel,
			installDirRel: installDirRel,
			artifactRel:   artifactRel,
			artifacts:     []artifact.Artifact{a},
		})
	}
	return rest, links
}

func resourceInstallDirRel(target hcc.Target, kind ir.ResourceKind) string {
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return ""
	}
	return info.ResourceInstallDir(kind)
}

func resourceArtifactMatches(resourceName, artifactRel string) bool {
	if strings.Contains(artifactRel, "/") {
		return false
	}
	base := strings.TrimSuffix(artifactRel, filepath.Ext(artifactRel))
	return base == resourceName
}

// collisionRemoveHint builds the warning message for a user-owned file that
// collides with a managed resource. It uses the canonical resource name, kind,
// scope, and registry coordinates so the suggested remove command is correct
// and unambiguous without manual trial and error. absolutePath is the
// full on-disk path of the conflicting file shown in the rename suggestion.
func collisionRemoveHint(absolutePath, fileName string, meta skillLink) string {
	resName := meta.resourceName
	if resName == "" {
		// Fallback: strip extension to recover resource name (e.g. "foo.md" → "foo").
		resName = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	}
	var flags []string
	if meta.kind != ir.KindSkill {
		flags = append(flags, "--kind "+meta.kind.String())
	}
	if meta.machineScope {
		flags = append(flags, "--scope user")
	}
	if meta.registryURL != "" {
		flags = append(flags, "--from "+meta.registryURL)
	}
	if meta.registryPath != "" {
		flags = append(flags, "--path "+meta.registryPath)
	}
	flagStr := ""
	if len(flags) > 0 {
		flagStr = strings.Join(flags, " ") + " "
	}
	removeCmd := fmt.Sprintf("agent-marketplace remove %s%q", flagStr, resName)
	return fmt.Sprintf(
		"collision: preserved user-owned %q (not managed by agent-marketplace); "+
			"to resolve, exclude this resource (%s) or rename the conflicting file at %q",
		fileName, removeCmd, absolutePath,
	)
}

func addSkillLink(groups map[outputKey]map[string]*skillLinkGroup, key outputKey, installDirRel string, link skillLink) {
	if groups[key] == nil {
		groups[key] = make(map[string]*skillLinkGroup)
	}
	group := groups[key][installDirRel]
	if group == nil {
		group = &skillLinkGroup{
			installDirRel: installDirRel,
			reportPrefix:  filepath.FromSlash(installDirRel),
		}
		groups[key][installDirRel] = group
	}
	group.links = append(group.links, link)
}

func orderedSkillLinkGroups(groups map[string]*skillLinkGroup) []skillLinkGroup {
	if len(groups) == 0 {
		return nil
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make([]skillLinkGroup, 0, len(keys))
	for _, key := range keys {
		group := *groups[key]
		slices.SortFunc(group.links, func(a, b skillLink) int {
			if a.name != b.name {
				return strings.Compare(a.name, b.name)
			}
			if a.kind != b.kind {
				return int(a.kind - b.kind)
			}
			if a.target != b.target {
				return strings.Compare(a.target, b.target)
			}
			return strings.Compare(a.resourceName, b.resourceName)
		})
		ordered = append(ordered, group)
	}
	return ordered
}

func linkedComponents(groups map[string]*skillLinkGroup) map[string]bool {
	if len(groups) == 0 {
		return nil
	}
	components := make(map[string]bool, len(groups))
	for installDirRel := range groups {
		components[componentName(installDirRel)] = true
	}
	return components
}

func materializationSourcePath(resolved resolver.ResolvedArtifact) string {
	var source string
	switch resource := resolved.Resource.(type) {
	case *ir.Skill:
		source = resource.Source.File
	case *ir.Command:
		source = resource.Source.File
	case *ir.Subagent:
		source = resource.Source.File
	case *ir.Hook:
		source = resource.Source.File
	case *ir.McpServer:
		source = resource.Source.File
	case *ir.Rule:
		source = resource.Source.File
	case *ir.Collection:
		source = resource.Source.File
	}
	if filepath.IsAbs(source) {
		return source
	}
	return resolved.Path
}

func cachedSkillDir(target hcc.Target, s resolver.ResolvedArtifact, _ []artifact.Artifact) string {
	key := strings.Join([]string{
		string(target),
		s.CatalogKey,
		s.RegistryURL,
		s.Ref,
		s.Path,
		s.Name,
	}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(cache.CacheDir(), "compiled", string(target), hex.EncodeToString(sum[:])[:16], s.Name)
}

func cachedResourceDir(target hcc.Target, kind ir.ResourceKind, s resolver.ResolvedArtifact, _ []artifact.Artifact) string {
	key := strings.Join([]string{
		string(target),
		kind.String(),
		s.CatalogKey,
		s.RegistryURL,
		s.Ref,
		s.Path,
		s.Name,
	}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(cache.CacheDir(), "compiled", string(target), hex.EncodeToString(sum[:])[:16], kind.String(), s.Name)
}

func cachedPlanDir(target hcc.Target, machineScope bool, outDir, reportPrefix string, _ []artifact.Artifact) string {
	key := strings.Join([]string{
		string(target),
		fmt.Sprintf("%v", machineScope),
		outDir,
		reportPrefix,
	}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(cache.CacheDir(), "compiled", string(target), hex.EncodeToString(sum[:])[:16], "artifacts")
}

func prepareArtifactLinkCache(cacheDir string, artifacts []artifact.Artifact, dryRun bool) error {
	if dryRun || len(artifacts) == 0 {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	result := &hcc.Result{Artifacts: artifacts}
	_, err := hcc.Materialize(result, cacheDir, hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true))
	return err
}

func splitMergeArtifacts(artifacts []artifact.Artifact) ([]artifact.Artifact, []artifact.Artifact) {
	var regular []artifact.Artifact
	var merge []artifact.Artifact
	for _, a := range artifacts {
		if a.MergeWithExisting {
			merge = append(merge, a)
			continue
		}
		regular = append(regular, a)
	}
	return regular, merge
}

func materializeMergeArtifacts(outDir string, artifacts []artifact.Artifact, dryRun bool, pruneStaleEntries bool) (*hcc.WriteReport, []diag.Diagnostic, error) {
	if len(artifacts) == 0 {
		if !pruneStaleEntries {
			return &hcc.WriteReport{OutDir: outDir}, nil, nil
		}
		pruneReport, pruneDiags := pruneManagedMergeFiles(outDir, dryRun)
		return pruneReport, pruneDiags, nil
	}
	// Replace any existing symlinks at merge artifact paths with regular
	// files before writing. Merge artifacts (like mcp.json) must be regular
	// files on disk so that user entries and managed entries coexist in one
	// self-contained file. If a previous sync left a symlink here, the
	// symlink target only contains managed entries; we read through it first
	// so hcc.Materialize can merge from the real content, then replace the
	// symlink with a regular file.
	if !dryRun {
		for _, a := range artifacts {
			if err := a.Validate(); err != nil {
				return nil, nil, fmt.Errorf("invalid merge artifact path %q: %w", a.Path, err)
			}
			destPath := filepath.Join(outDir, filepath.FromSlash(a.Path))
			if info, err := os.Lstat(destPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
				existing, readErr := os.ReadFile(destPath) // follows symlink
				if readErr != nil && !os.IsNotExist(readErr) {
					return nil, nil, fmt.Errorf("read symlink target %s: %w", destPath, readErr)
				}
				targetInfo, statErr := os.Stat(destPath) // get target file info for permissions
				if readErr == nil && len(existing) > 0 {
					mode := fs.FileMode(0o644)
					if statErr == nil {
						mode = targetInfo.Mode().Perm()
					}
					if mode == 0 {
						mode = 0o644
					}
					if err := atomicWriteFile(destPath, existing, mode); err != nil {
						return nil, nil, fmt.Errorf("replace symlink %s: %w", destPath, err)
					}
				} else if err := os.Remove(destPath); err != nil {
					return nil, nil, fmt.Errorf("remove symlink %s: %w", destPath, err)
				}
			}
		}
	}
	options := []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeOverwrite)}
	if dryRun {
		options = append(options, hcc.WithDryRun())
	}
	result := &hcc.Result{Artifacts: artifacts}
	writeReport, writeErr := hcc.Materialize(result, outDir, options...)
	return writeReport, nil, writeErr
}

// pruneUncoveredManagedMergeFiles walks outDir and its subdirectories for
// JSON files containing managed entries, but skips directories that were
// already handled by materializeMergeArtifacts (tracked in
// dirsCoveredByMergePlans). This ensures that root-level merge files (like
// mcp.json) and files in directories without a merge-artifact plan still get
// their managed entries pruned during deinit.
//
// Failure is PER FILE (bead agent-marketplace-5exj.25). A file this walk cannot
// prune — a duplicate-key refusal from stripManagedEntriesFromFile, or an
// unwritable destination — yields one diagnostic naming that file and the walk
// carries on pruning every other file in the root. It used to return the error
// out of the WalkDir callback, so a single refused file aborted the walk and
// silently left every not-yet-visited sibling un-pruned. That mirrors what
// ownedJSONAggregateWork.incomplete already does for Devin's aggregates and what
// the plan calls for: "Aggregate updates are atomic per file, not per target"
// and "Malformed destination JSON blocks that file only."
//
// The returned diagnostics carry the failure's own stable code (e.g.
// json.duplicate-key) at its catalogued error severity, so `sync` still exits
// non-zero: a prune that did not happen is a real failure, exactly as bead .18
// decided for the refusal itself and bead .20 decided for skipped Devin
// aggregate entries.
func pruneUncoveredManagedMergeFiles(outDir string, dirsCoveredByMergePlans map[string]bool, dryRun bool, protected map[string]bool) (*hcc.WriteReport, []diag.Diagnostic) {
	report := &hcc.WriteReport{OutDir: outDir}
	var diags []diag.Diagnostic
	info, err := os.Stat(outDir)
	if err != nil || !info.IsDir() {
		return report, nil
	}
	// WalkDir visits lexically, so the diagnostic order is deterministic.
	_ = filepath.WalkDir(outDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Permission denied or other walk errors: skip the entry
			// rather than aborting the entire walk.
			return nil
		}
		if d.IsDir() {
			// Skip directories that were already handled by a
			// merge-artifact plan (materializeMergeArtifacts).
			if dirsCoveredByMergePlans[path] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		rel, relErr := filepath.Rel(outDir, path)
		if relErr != nil {
			return nil
		}
		// A target's declared aggregate files are reconciled by an
		// ownership-backed path and must never be touched by
		// stripManagedEntriesFromFile, which decides ownership from an in-file
		// marker any tool can write. (Since bead agent-marketplace-5exj.18 that
		// path is comment- and duplicate-key-safe, but the ownership argument is
		// unchanged.) Skipping them here is what keeps a user's handwritten Devin
		// config.json out of that code path entirely.
		if protected[filepath.ToSlash(rel)] {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		cleaned, removed, pruneErr := stripManagedEntriesFromFile(path, dryRun)
		if pruneErr != nil {
			// PER-FILE failure: report this file and keep walking.
			diags = append(diags, managedMergePruneDiag(relSlash, path, pruneErr))
			return nil
		}
		if len(removed) > 0 {
			if len(cleaned) == 0 {
				report.Removed = append(report.Removed, relSlash)
			} else {
				report.Written = append(report.Written, relSlash)
			}
		}
		return nil
	})
	return report, diags
}

// managedMergePruneDiag turns a single file's prune failure into a keyed,
// file-scoped diagnostic.
//
// Resource is the OUTPUT-ROOT-RELATIVE PATH of the file. `Diagnostic.Resource`
// names "the resource that produced this diagnostic", and this failure is not
// produced by any marketplace resource: nothing in the desired set was being
// installed, the walk was removing our own leftover markers from a file the USER
// owns. The only identity the failure has is that file, and it is the identity a
// user needs in order to act ("fix this file"). Using the same relative path the
// WriteReport uses for that file keeps the diagnostic joinable with the report
// rows, and keeps it free of absolute home paths. Source.File is normalized to
// the same relative path (the jsonc parser saw the absolute one) while keeping
// the CodedError's 1-based line, which is what makes the refusal actionable.
//
// A failure that carries no stable code — an unwritable destination, say — falls
// back to materialize.write.error, the code the aborting caller used to emit, so
// no failure loses severity in the move to per-file reporting.
func managedMergePruneDiag(rel, path string, err error) diag.Diagnostic {
	if coded, ok := diag.AsCodedError(err); ok && coded.Code != "" {
		d := coded.Diagnostic(rel)
		d.Source.File = rel
		// Only the code and the location are logged. The offending value is
		// never carried by the error and must never be logged: these are user
		// MCP files holding env values, headers, and OAuth secrets.
		slog.Warn("materialize prune skipped one file, walk continues",
			"path", path,
			"code", coded.Code,
			"line", coded.Source.Line,
		)
		return d
	}
	slog.Warn("materialize prune skipped one file, walk continues",
		"path", path,
		"code", "materialize.write.error",
	)
	return diag.Diagnostic{
		Code:     "materialize.write.error",
		Severity: diag.SeverityError,
		Resource: rel,
		Message:  fmt.Sprintf("prune managed entries from %s: %v", path, err),
		Source:   diag.SourceRef{File: rel},
	}
}

// retargetPruneDiags stamps the target on per-file prune diagnostics and lifts
// their file-scoped Resource into the same output-root-relative namespace
// prefixReportPaths puts the TargetReport rows in (Windsurf's repo-scope
// ".windsurf/" wrapper is the only prefix in practice).
func retargetPruneDiags(diags []diag.Diagnostic, target hcc.Target, prefix string) []diag.Diagnostic {
	for i := range diags {
		diags[i].Target = string(target)
		if prefix == "" || diags[i].Resource == "" {
			continue
		}
		diags[i].Resource = filepath.ToSlash(filepath.Join(prefix, diags[i].Resource))
		diags[i].Source.File = diags[i].Resource
	}
	return diags
}

// appendTargetDiagnostics records diagnostics in both the normal report list
// and the structured projection. The latter is the producer seam that knows
// outputKey scope and (for partitioned roots) root identity; generic HCC
// Diagnostic semantics remain unchanged.
func appendTargetDiagnostics(report *Report, key outputKey, rootID string, diags []diag.Diagnostic) {
	for _, d := range diags {
		appendTargetDiagnostic(report, key, rootID, d, d.Source.File)
	}
}

// appendTargetDiagnostic keeps the public HCC diagnostic untouched while
// allowing a materialization seam to supply a safe, output-relative path for
// its structured issue. Report-level failures must not turn an absolute output
// root into a user-visible RootID or structured path.
func appendTargetDiagnostic(report *Report, key outputKey, rootID string, d diag.Diagnostic, issuePath string) {
	report.Diags = append(report.Diags, d)
	if rootID == "" && key.target != "" {
		rootID = materializationRootID(key)
	}
	if !IsMaterializationDiagnostic(d) {
		return
	}
	operation, outcome := MaterializationOperationFailure, MaterializationOutcomeFailed
	if !d.IsError() {
		operation, outcome = MaterializationOperationSkip, MaterializationOutcomeWarning
	}
	if issuePath == "" {
		issuePath = d.Source.File
	}
	if filepath.IsAbs(issuePath) {
		issuePath = ""
	}
	report.Issues = append(report.Issues, MaterializationIssue{
		Resource: d.Resource, Target: key.target,
		Scope: materializationScope(key.machineScope), RootID: rootID,
		Path: issuePath, Operation: operation, Outcome: outcome,
		Code: d.Code, Severity: d.Severity,
		Message: diagnosticidentity.NormalizeMessage(d.Message),
	})
}

// aggregateProtectedPaths returns the output-root-relative aggregate files a
// target reconciles through the ownership-backed path in the given scope.
func aggregateProtectedPaths(target hcc.Target, machineScope bool) map[string]bool {
	info, ok := harness.TargetInfoFor(target)
	if !ok {
		return nil
	}
	return aggregateProtectedPathsFor(info, machineScope)
}

// aggregateProtectedPathsFor is aggregateProtectedPaths over an explicit
// TargetInfo, so the predicate can be exercised against hypothetical targets
// without mutating the real registry.
//
// The fence is driven by harness.TargetInfo.OwnershipReconciledAggregates and
// by nothing else. It used to be driven by SymlinkOnly, which was correct only
// because Devin happened to be both the only symlink-only target and the only
// ownership-reconciled one; the two properties are unrelated, and the first
// target migrated to internal/ownership without also being symlink-only would
// have silently fallen back into the marker path it was migrated away from
// (bead agent-marketplace-5exj.29).
func aggregateProtectedPathsFor(info harness.TargetInfo, machineScope bool) map[string]bool {
	if !info.OwnershipReconciledAggregates {
		// A marker-merge target's aggregates stay the marker-merge path's
		// responsibility; excluding them would silently stop pruning stale
		// managed entries out of them.
		return nil
	}
	rels := info.AggregateFilesForScope(machineScope)
	if len(rels) == 0 {
		return nil
	}
	protected := make(map[string]bool, len(rels))
	for _, rel := range rels {
		protected[filepath.ToSlash(rel)] = true
	}
	return protected
}

// pruneManagedMergeFiles walks outDir for JSON files that contain managed
// entries (marked with _managed_by: agent-marketplace) and strips those
// entries. User-owned entries are preserved. Files that become empty after
// stripping are deleted.
//
// It is pruneUncoveredManagedMergeFiles with nothing covered and nothing
// protected — the deinit case where no merge-artifact plan claimed a directory.
// Sharing the body is what keeps the per-file failure handling identical in both
// walks, which was the whole point of bead agent-marketplace-5exj.25: the two
// callbacks had drifted into two copies of the same abort-the-walk mistake.
func pruneManagedMergeFiles(outDir string, dryRun bool) (*hcc.WriteReport, []diag.Diagnostic) {
	return pruneUncoveredManagedMergeFiles(outDir, nil, dryRun, nil)
}

// stripManagedEntriesFromFile reads a JSON file, removes all top-level
// container entries that carry the _managed_by marker, and writes the
// result back. Empty top-level containers are preserved so config files like
// mcp.json remain valid with an empty mcpServers object after the last managed
// entry is removed. Returns the cleaned bytes and the names of removed entries.
//
// The file is a USER-OWNED harness config (mcp.json, hooks.json, plugin.json,
// settings.json): everything in it that we did not generate is the user's, and
// the marketplace only ever deletes the entries that carry its own marker. Two
// properties follow from that and are why this path goes through
// pkg/harness-compilation/jsonc instead of encoding/json (bead
// agent-marketplace-5exj.18):
//
//   - Duplicate object keys FAIL CLOSED. encoding/json collapses them
//     last-value-wins, and because this function writes its result back, the
//     collapse was persisted: a second "headers" or "env" block silently
//     disappeared from the user's file. jsonc.Parse rejects the document with
//     the same stable *diag.CodedError (json.duplicate-key) the read-only
//     loaders got in bead .16, and nothing is written. Preserving the
//     duplicates instead was rejected: we would have to keep writing a document
//     whose meaning is parser-dependent, and this function's own decision
//     ("which entries carry the marker") is undefined when a container key
//     appears twice. Refusing costs the user one un-pruned file plus a
//     diagnostic naming the file and line; collapsing costs them data.
//   - Comments and key order SURVIVE. jsonc patches concrete syntax and never
//     re-serializes a member it did not touch, so a handwritten entry keeps its
//     exact bytes, its position, and the comments written above it. The old
//     MarshalIndent round-trip destroyed all three on every prune.
//
// Malformed JSON keeps the historical behaviour of being left completely alone
// with no error: we never had a coherent view of that file, and failing a sync
// over a file the user already has to fix by hand adds nothing.
func stripManagedEntriesFromFile(path string, dryRun bool) (cleaned []byte, removed []string, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, nil, readErr
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil, nil
	}
	// Quick check: skip files that definitely don't contain managed entries.
	if !bytes.Contains(data, []byte(`"`+managedByField+`"`)) {
		return nil, nil, nil
	}
	doc, parseErr := jsonc.Parse(path, data)
	if parseErr != nil {
		if coded, ok := diag.AsCodedError(parseErr); ok && coded.Code == diag.CodeJSONDuplicateKey {
			// Only the path, the code, and the line are logged. The duplicated
			// value is never carried by the error and must never be logged:
			// these files hold MCP env values, headers, and OAuth secrets.
			slog.Error("materialize prune refused duplicate-key JSON",
				"path", path,
				"code", coded.Code,
				"line", coded.Source.Line,
			)
			return nil, nil, parseErr
		}
		return nil, nil, nil // not JSON we can reason about, leave it alone
	}
	root := doc.Root()
	if root.Kind() != jsonc.KindObject {
		return data, nil, nil
	}
	// Source order, so both the write-back and the reported names are
	// deterministic; the previous map-range implementation was neither.
	for _, key := range root.Keys() {
		container, ok := root.Member(key)
		if !ok {
			continue
		}
		switch container.Kind() {
		case jsonc.KindObject:
			// Map-of-maps container (e.g. mcp.json's mcpServers).
			for _, name := range container.Keys() {
				entry, found := container.Member(name)
				if !found || !isManagedNode(entry) {
					continue
				}
				doc.DeleteMember(container, name)
				removed = append(removed, name)
			}
		case jsonc.KindArray:
			// Array-of-objects container (e.g. plugin.json's skills/droids/
			// agents, hooks.json's PostToolUse). Drop managed elements,
			// preserve everything else byte-for-byte.
			kept := make([][]byte, 0, container.Len())
			dropped := 0
			for i := 0; i < container.Len(); i++ {
				entry, found := container.Element(i)
				if !found {
					continue
				}
				if isManagedNode(entry) {
					dropped++
					if name := nodeStringMember(entry, "name"); name != "" {
						removed = append(removed, name)
					} else {
						// Unnamed managed entry (e.g. hooks.json rule
						// groups): use the container key as the removed
						// identifier so the write-back is triggered.
						removed = append(removed, key)
					}
					continue
				}
				kept = append(kept, entry.Raw())
			}
			if dropped == 0 {
				continue
			}
			if len(kept) == 0 {
				doc.DeleteMember(root, key)
			} else {
				doc.SetElements(container, kept)
			}
		case jsonc.KindLiteral:
			// Scalars cannot hold a managed entry.
		}
	}
	if len(removed) == 0 {
		return data, nil, nil // nothing to strip
	}
	result := doc.Bytes()
	if !dryRun {
		if writeErr := atomicWriteFile(path, result, 0o644); writeErr != nil {
			return nil, removed, writeErr
		}
	}
	return result, removed, nil
}

// isManagedNode reports whether a concrete-syntax JSON object carries the
// _managed_by: agent-marketplace ownership marker.
func isManagedNode(node *jsonc.Node) bool {
	if node == nil || node.Kind() != jsonc.KindObject {
		return false
	}
	return nodeStringMember(node, managedByField) == managedByValue
}

// nodeStringMember returns the decoded string value of a member, or "" when the
// member is absent or is not a JSON string. Decoding the literal (rather than
// comparing raw bytes) keeps escaped spellings such as "agent-\u006darketplace"
// recognizable.
func nodeStringMember(node *jsonc.Node, key string) string {
	if node == nil || node.Kind() != jsonc.KindObject {
		return ""
	}
	member, ok := node.Member(key)
	if !ok || member.Kind() != jsonc.KindLiteral {
		return ""
	}
	var value string
	if err := json.Unmarshal(member.Raw(), &value); err != nil {
		return ""
	}
	return value
}

// ensureSymlinkResult holds the outcome of an ensureSymlinkPath call.
type ensureSymlinkResult struct {
	alreadySynced    bool  // symlink already points to the desired target
	skippedUserOwned bool  // path exists but is user-owned (not managed by us)
	err              error // I/O or permission error
}

func ensureSymlinkPath(linkPath, target string, dryRun bool) ensureSymlinkResult {
	return ensureSymlinkPathWithOwnership(linkPath, target, dryRun, false)
}

func ensureSymlinkPathWithOwnership(linkPath, target string, dryRun bool, manifestOwned bool) ensureSymlinkResult {
	info, err := os.Lstat(linkPath)
	switch {
	case err == nil:
		// Path already exists — check ownership before replacing.
		if info.Mode()&os.ModeSymlink != 0 {
			if isManagedSymlink(linkPath) {
				currentTarget, err := os.Readlink(linkPath)
				if err != nil {
					return ensureSymlinkResult{err: err}
				}
				if currentTarget == target {
					return ensureSymlinkResult{alreadySynced: true}
				}
				// Managed symlink with wrong target — safe to replace.
			} else {
				// User-owned symlink — refuse to replace.
				return ensureSymlinkResult{skippedUserOwned: true}
			}
		} else if !manifestOwned {
			// Non-symlink (regular file or directory) — refuse to replace unless
			// it is explicitly owned by a portable file-mode manifest.
			return ensureSymlinkResult{skippedUserOwned: true}
		}
	case os.IsNotExist(err):
		info = nil
	default:
		return ensureSymlinkResult{err: err}
	}

	if dryRun {
		return ensureSymlinkResult{}
	}

	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return ensureSymlinkResult{err: err}
	}
	if info != nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(linkPath); err != nil && !os.IsNotExist(err) {
			return ensureSymlinkResult{err: err}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(linkPath), ".agent-marketplace-link-*")
	if err != nil {
		return ensureSymlinkResult{err: err}
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return ensureSymlinkResult{err: err}
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		return ensureSymlinkResult{err: err}
	}
	if err := os.Symlink(target, tmpPath); err != nil {
		return ensureSymlinkResult{err: err}
	}
	if err := os.Rename(tmpPath, linkPath); err != nil {
		return ensureSymlinkResult{err: err}
	}
	return ensureSymlinkResult{}
}

// portableOwnership is the verdict for a manifest-claimed output that is no
// longer a managed symlink.
type portableOwnership int

const (
	// portableOwnershipAdopt means the destination already equals the desired
	// output, so the entry can be re-stamped and the write skipped.
	portableOwnershipAdopt portableOwnership = iota
	// portableOwnershipReplace means the previously written output is intact,
	// so overwriting it with fresh content is safe.
	portableOwnershipReplace
	// portableOwnershipCollision means the destination was modified outside of
	// sync and must be preserved.
	portableOwnershipCollision
)

func portableOwnershipDecision(entry portableManifestEntry, currentDigest, desiredDigest string) portableOwnership {
	// When the destination already equals what we would write there is no
	// conflict regardless of the recorded digest. This keeps a stale manifest
	// entry (e.g. from a merge, cherry-pick, or cross-CLI-version checkout)
	// from producing a false collision that also suppresses later genuine
	// updates.
	if currentDigest == desiredDigest {
		return portableOwnershipAdopt
	}
	// Legacy entries carry no digest, so ownership can no longer be proven once
	// the destination diverges from the desired content.
	if entry.Digest == "" || currentDigest != entry.Digest {
		return portableOwnershipCollision
	}
	return portableOwnershipReplace
}

// syncPortableArtifactFiles copies cache outputs instead of linking them. A
// manifest entry proves ownership only while its saved digest still describes
// the destination; this prevents sync from overwriting user edits.
//
// The prune flag the symlink path honors is intentionally ignored: regular
// files cannot be recognized as managed by inspecting the filesystem, so
// removal is driven exclusively by manifest entries (see prunePortableEntries).
// Plan-level pruning here would delete user-authored files.
func syncPortableArtifactFiles(outDir string, artifacts []artifact.Artifact, cacheDir string, dryRun bool, _ bool, collisionMeta map[string]skillLink, state *portableState, target hcc.Target, reportPrefix string, desired map[string]bool) (*hcc.WriteReport, []diag.Diagnostic, error) {
	report := &hcc.WriteReport{OutDir: outDir}
	var diags []diag.Diagnostic
	for _, a := range artifacts {
		if err := a.Validate(); err != nil {
			return report, diags, err
		}
		path := filepath.ToSlash(filepath.Join(reportPrefix, a.Path))
		desired[path] = true
		desiredDigest := portableArtifactDigest(a)
		dst, err := portableOutputPath(outDir, a.Path)
		if err != nil {
			return report, diags, err
		}
		src := filepath.Join(cacheDir, filepath.FromSlash(a.Path))
		info, err := os.Lstat(dst)
		if err != nil && !os.IsNotExist(err) {
			return report, diags, err
		}
		entry, owned := state.entry(target, path)
		if err == nil {
			managedLink := info.Mode()&os.ModeSymlink != 0 && isManagedSymlink(dst)
			if !owned && !managedLink {
				report.Skipped = append(report.Skipped, a.Path)
				diags = append(diags, portableArtifactCollision(dst, a.Path, collisionMeta))
				continue
			}
			if owned && !managedLink {
				currentDigest, digestErr := portablePathDigest(dst)
				if digestErr != nil {
					return report, diags, digestErr
				}
				switch portableOwnershipDecision(entry, currentDigest, desiredDigest) {
				case portableOwnershipAdopt:
					if !dryRun {
						state.add(target, path, desiredDigest)
					}
					report.Skipped = append(report.Skipped, a.Path)
					continue
				case portableOwnershipCollision:
					report.Skipped = append(report.Skipped, a.Path)
					diags = append(diags, portableArtifactCollision(dst, a.Path, collisionMeta))
					continue
				case portableOwnershipReplace:
				}
			}
		}
		if !dryRun {
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return report, diags, err
			}
			if info != nil && (info.Mode()&os.ModeSymlink != 0 || info.IsDir()) {
				if err := os.RemoveAll(dst); err != nil {
					return report, diags, err
				}
			}
			if err := copyFile(src, dst); err != nil {
				return report, diags, err
			}
			state.add(target, path, desiredDigest)
		}
		report.Written = append(report.Written, a.Path)
	}
	sort.Strings(report.Written)
	sort.Strings(report.Skipped)
	return report, diags, nil
}

func portableArtifactCollision(dst, path string, collisionMeta map[string]skillLink) diag.Diagnostic {
	d := diag.Diagnostic{Code: "materialize.collision.user-owned", Severity: diag.SeverityWarning}
	if meta, ok := collisionMeta[path]; ok {
		d.Resource, d.Message = meta.resourceName, collisionRemoveHint(dst, path, meta)
	} else {
		d.Message = fmt.Sprintf("collision: preserved modified materialized output %q", dst)
	}
	return d
}

func portableLinkedDesiredDigest(link skillLink, dryRun bool) (string, error) {
	if !dryRun {
		return portablePathDigest(link.target)
	}
	if link.cacheDir != "" && len(link.artifacts) == 1 && link.target == filepath.Join(link.cacheDir, filepath.FromSlash(link.artifacts[0].Path)) {
		return portableArtifactDigest(link.artifacts[0]), nil
	}
	return portableSkillVirtualDigest(link)
}

type portableVirtualNode struct {
	dir     bool
	mode    fs.FileMode
	content []byte
}

// portableSkillVirtualDigest describes the cache tree prepareSkillLinkCache
// would produce without populating that cache. This lets --check correctly
// identify a pending copy or a user edit on a first dry run.
func portableSkillVirtualDigest(link skillLink) (string, error) {
	nodes := map[string]portableVirtualNode{"": {dir: true, mode: 0o755}}
	ensureParents := func(path string) {
		for dir := filepath.ToSlash(filepath.Dir(path)); dir != "." && dir != ""; dir = filepath.ToSlash(filepath.Dir(dir)) {
			if _, ok := nodes[dir]; !ok {
				nodes[dir] = portableVirtualNode{dir: true, mode: 0o755}
			}
		}
	}
	for _, a := range link.artifacts {
		path := filepath.ToSlash(filepath.Clean(a.Path))
		ensureParents(path)
		nodes[path] = portableVirtualNode{mode: portableArtifactMode(a.Mode), content: a.Bytes}
	}
	var addSupport func(string, string) error
	addSupport = func(root, rel string) error {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, entry := range entries {
			childRel := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			if childRel == "SKILL.md" {
				continue
			}
			childPath := filepath.Join(root, entry.Name())
			info, err := os.Stat(childPath) // copyFile/copyDir follows source links.
			if err != nil {
				return err
			}
			if info.IsDir() {
				ensureParents(childRel)
				// copyDir does not chmod an already-created directory, so an
				// artifact-created parent keeps its existing 0755 mode.
				if _, exists := nodes[childRel]; !exists {
					nodes[childRel] = portableVirtualNode{dir: true, mode: info.Mode().Perm()}
				}
				if err := addSupport(childPath, childRel); err != nil {
					return err
				}
				continue
			}
			data, err := os.ReadFile(childPath)
			if err != nil {
				return err
			}
			ensureParents(childRel)
			nodes[childRel] = portableVirtualNode{mode: info.Mode().Perm(), content: data}
		}
		return nil
	}
	if link.sourceDir != "" {
		if err := addSupport(link.sourceDir, ""); err != nil {
			return "", err
		}
	}
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		node := nodes[path]
		if node.dir {
			_, _ = fmt.Fprintf(h, "dir\x00%s\x00%#o\x00", path, node.mode.Perm())
			continue
		}
		_, _ = fmt.Fprintf(h, "file\x00%s\x00%#o\x00%d\x00", path, node.mode.Perm(), len(node.content))
		_, _ = h.Write(node.content)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func syncPortableSkillTrees(skillsDir string, links []skillLink, dryRun bool, state *portableState, target hcc.Target, reportPrefix string, desired map[string]bool) (TargetReport, error) {
	report := TargetReport{OutDir: skillsDir}
	if !dryRun {
		if err := ensureDirectoryReplacingSymlink(skillsDir); err != nil {
			return report, err
		}
	}
	for _, link := range links {
		path := filepath.ToSlash(filepath.Join(reportPrefix, link.name))
		desired[path] = true
		desiredDigest, err := portableLinkedDesiredDigest(link, dryRun)
		if err != nil {
			return report, err
		}
		dst, err := portableOutputPath(skillsDir, link.name)
		if err != nil {
			return report, err
		}
		info, err := os.Lstat(dst)
		if err != nil && !os.IsNotExist(err) {
			return report, err
		}
		entry, owned := state.entry(target, path)
		if err == nil {
			managedLink := info.Mode()&os.ModeSymlink != 0 && isManagedSymlink(dst)
			if !owned && !managedLink {
				report.Skipped = append(report.Skipped, link.name)
				report.Diags = append(report.Diags, portableSkillCollision(dst, link))
				continue
			}
			if owned && !managedLink {
				currentDigest, digestErr := portablePathDigest(dst)
				if digestErr != nil {
					return report, digestErr
				}
				switch portableOwnershipDecision(entry, currentDigest, desiredDigest) {
				case portableOwnershipAdopt:
					if !dryRun {
						state.add(target, path, desiredDigest)
					}
					report.Skipped = append(report.Skipped, link.name)
					continue
				case portableOwnershipCollision:
					report.Skipped = append(report.Skipped, link.name)
					report.Diags = append(report.Diags, portableSkillCollision(dst, link))
					continue
				case portableOwnershipReplace:
				}
			}
		}
		if !dryRun {
			if info != nil {
				if err := os.RemoveAll(dst); err != nil {
					return report, err
				}
			}
			sourceInfo, err := os.Stat(link.target)
			if err != nil {
				return report, err
			}
			if sourceInfo.IsDir() {
				if err := copyDir(link.target, dst); err != nil {
					return report, err
				}
			} else if err := copyFile(link.target, dst); err != nil {
				return report, err
			}
			state.add(target, path, desiredDigest)
		}
		report.Written = append(report.Written, link.name)
	}
	sort.Strings(report.Written)
	sort.Strings(report.Skipped)
	return report, nil
}

func portableSkillCollision(dst string, link skillLink) diag.Diagnostic {
	return diag.Diagnostic{Code: "materialize.collision.user-owned", Severity: diag.SeverityWarning, Resource: link.resourceName, Message: collisionRemoveHint(dst, link.name, link)}
}

func prunePortableEntries(state *portableState, key outputKey, outDir, repoRoot string, desired map[string]bool, dryRun bool) ([]TargetReport, []diag.Diagnostic, error) {
	reports := make(map[string]*TargetReport)
	var diags []diag.Diagnostic
	keys := make([]string, 0, len(state.entries))
	for key := range state.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, stateKey := range keys {
		entry := state.entries[stateKey]
		if entry.Target != string(key.target) || desired[entry.Path] {
			continue
		}
		if key.target == hcc.Target(targetmeta.TargetCodex) && !state.matchesCodexRoot(entry) {
			continue
		}
		rootID, root, rel, err := portableEntryDestinationForDestinations(key, outDir, entry.Path, state.destinations)
		if err != nil {
			return nil, diags, err
		}
		path, err := portableOutputPath(root, rel)
		if err != nil {
			return nil, diags, err
		}
		_, err = os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, diags, err
		}
		if err == nil {
			currentDigest, digestErr := portablePathDigest(path)
			if digestErr != nil {
				return nil, diags, digestErr
			}
			if entry.Digest == "" || currentDigest != entry.Digest {
				diags = append(diags, diag.Diagnostic{
					Code:     "materialize.collision.user-owned",
					Severity: diag.SeverityWarning,
					Message:  fmt.Sprintf("collision: preserved modified materialized output %q while pruning", path),
				})
				continue
			}
		}
		if !dryRun {
			if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
				return nil, diags, err
			}
			cleanEmptyPortableArtifactDirs(root, filepath.Dir(filepath.FromSlash(rel)))
			delete(state.entries, stateKey)
		}
		report := reports[root]
		if report == nil {
			report = &TargetReport{Target: key.target, MachineScope: key.machineScope, RootID: rootID, OutDir: root}
			reports[root] = report
		}
		report.Removed = append(report.Removed, rel)
	}
	roots := make([]string, 0, len(reports))
	for root := range reports {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	out := make([]TargetReport, 0, len(roots))
	for _, root := range roots {
		sort.Strings(reports[root].Removed)
		out = append(out, *reports[root])
	}
	return out, diags, nil
}

func portableEntryDestination(key outputKey, outDir, repoRoot, entryPath string) (string, string, string, error) {
	if key.target != hcc.Target(targetmeta.TargetCodex) {
		return "default", outDir, entryPath, nil
	}
	destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
	if err != nil {
		return "", "", "", err
	}
	return portableEntryDestinationForDestinations(key, outDir, entryPath, destinations)
}

func portableEntryDestinationForDestinations(key outputKey, outDir, entryPath string, destinations []harness.Destination) (string, string, string, error) {
	if key.target != hcc.Target(targetmeta.TargetCodex) {
		return "default", outDir, entryPath, nil
	}
	entryPath = filepath.ToSlash(entryPath)
	for _, destination := range destinations {
		prefix := strings.TrimSuffix(filepath.ToSlash(destination.Prefix), "/") + "/"
		if rel, ok := strings.CutPrefix(entryPath, prefix); ok {
			return destination.ID, destination.Root, rel, nil
		}
	}
	return "", "", "", fmt.Errorf("portable Codex path %q is outside native destinations", entryPath)
}

// prunePortableStaleSymlinks removes cache-backed symlinks left behind after a
// mode switch or deinit. File mode normally has only regular files, so these
// links are not represented in its portable manifest. Restricting the scan to
// known harness component and resource directories keeps project and target
// scopes isolated while preserving user-owned symlinks.
func prunePortableStaleSymlinks(report *Report, key outputKey, outDir string, cleanupDirs map[string]string, desired map[string]bool, dryRun bool) *Report {
	dirs := make([]string, 0, len(cleanupDirs))
	for dir := range cleanupDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		prefix := cleanupDirs[dir]
		desiredInDir := make(map[string]string)
		prefix = filepath.ToSlash(filepath.Clean(prefix))
		for path := range desired {
			path = filepath.ToSlash(path)
			if prefix == "." || prefix == "" {
				desiredInDir[path] = ""
				continue
			}
			if rel, ok := strings.CutPrefix(path, prefix+"/"); ok {
				desiredInDir[rel] = ""
			}
		}
		stale, diags, err := staleArtifactLinks(dir, desiredInDir)
		if err != nil {
			appendTargetDiagnostic(report, key, codexRootIDForPrefix(key, prefix), diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("prune stale portable symlinks in %s: %v", dir, err)}, prefix)
			continue
		}
		tr := TargetReport{OutDir: dir, Removed: stale, Diags: diags}
		if !dryRun {
			for _, rel := range stale {
				path, pathErr := portableOutputPath(dir, rel)
				if pathErr != nil {
					appendTargetDiagnostic(report, key, codexRootIDForPrefix(key, prefix), diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: pathErr.Error()}, prefix)
					continue
				}
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					appendTargetDiagnostic(report, key, codexRootIDForPrefix(key, prefix), diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("remove stale portable symlink %s: %v", path, err)}, filepath.ToSlash(filepath.Join(prefix, rel)))
					continue
				}
				cleanEmptyPortableArtifactDirs(dir, filepath.Dir(filepath.FromSlash(rel)))
			}
		}
		report = appendGroupReport(report, key, outDir, prefix, tr, nil, "")
	}
	return report
}

func syncArtifactLinks(outDir string, artifacts []artifact.Artifact, cacheDir string, dryRun bool, pruneStaleEntries bool, collisionMeta map[string]skillLink, portable ...portableSyncContext) (*hcc.WriteReport, []diag.Diagnostic, error) {
	var state *portableState
	var target hcc.Target
	var reportPrefix string
	var portableDesired map[string]bool
	if len(portable) > 0 {
		state, target, reportPrefix, portableDesired = portable[0].state, portable[0].target, portable[0].reportPrefix, portable[0].desired
	}
	report := &hcc.WriteReport{OutDir: outDir}
	var diags []diag.Diagnostic
	desired := make(map[string]string, len(artifacts))
	desiredDigests := make(map[string]string, len(artifacts))
	for _, a := range artifacts {
		if err := a.Validate(); err != nil {
			return report, diags, err
		}
		desired[a.Path] = filepath.Join(cacheDir, filepath.FromSlash(a.Path))
		desiredDigests[a.Path] = portableArtifactDigest(a)
	}

	if !dryRun {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return report, diags, err
		}
	}

	paths := make([]string, 0, len(desired))
	for path := range desired {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		cacheTarget := desired[path]
		linkPath := filepath.Join(outDir, filepath.FromSlash(path))
		manifestPath := filepath.ToSlash(filepath.Join(reportPrefix, path))
		if portableDesired != nil {
			portableDesired[manifestPath] = true
		}
		entry, manifestOwned := portableManifestEntry{}, false
		if state != nil {
			entry, manifestOwned = state.entry(target, manifestPath)
		}
		migrationAllowed := false
		if manifestOwned {
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink == 0 {
				currentDigest, digestErr := portablePathDigest(linkPath)
				if digestErr != nil {
					return report, diags, digestErr
				}
				// A legacy entry can migrate only when it matches the desired
				// file; a digest-bearing entry instead matches its last output.
				migrationAllowed = (entry.Digest != "" && currentDigest == entry.Digest) ||
					(entry.Digest == "" && currentDigest == desiredDigests[path])
			} else {
				migrationAllowed = true
			}
		}
		result := ensureSymlinkPathWithOwnership(linkPath, cacheTarget, dryRun, migrationAllowed)
		if result.err != nil {
			return report, diags, result.err
		}
		if result.skippedUserOwned {
			report.Skipped = append(report.Skipped, path)
			d := diag.Diagnostic{
				Code:     "materialize.collision.user-owned",
				Severity: diag.SeverityWarning,
			}
			if meta, ok := collisionMeta[path]; ok {
				resName := meta.resourceName
				if resName == "" {
					resName = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				}
				d.Resource = resName
				d.Message = collisionRemoveHint(linkPath, path, meta)
			} else {
				d.Message = fmt.Sprintf(
					"collision: preserved user-owned %q (not managed by agent-marketplace); "+
						"to resolve, exclude the resource or rename the conflicting file at %q",
					path, linkPath,
				)
			}
			diags = append(diags, d)
			continue
		}
		if result.alreadySynced {
			report.Skipped = append(report.Skipped, path)
			if state != nil && !dryRun {
				state.remove(target, manifestPath)
			}
			continue
		}
		report.Written = append(report.Written, path)
		if state != nil && !dryRun {
			state.remove(target, manifestPath)
		}
	}

	if pruneStaleEntries {
		stale, staleDiags, err := staleArtifactLinks(outDir, desired)
		if err != nil {
			return report, diags, err
		}
		diags = append(diags, staleDiags...)
		report.Removed = stale
		if !dryRun {
			for _, path := range stale {
				if err := os.RemoveAll(filepath.Join(outDir, filepath.FromSlash(path))); err != nil && !os.IsNotExist(err) {
					return report, diags, err
				}
				cleanEmptyArtifactDirs(outDir, filepath.Dir(filepath.FromSlash(path)))
			}
		}
	}

	sort.Strings(report.Written)
	sort.Strings(report.Skipped)
	sort.Strings(report.Removed)
	return report, diags, nil
}

func staleArtifactLinks(outDir string, desired map[string]string) ([]string, []diag.Diagnostic, error) {
	var stale []string
	var diags []diag.Diagnostic
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Only prune symlinks — regular files are user-owned.
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if _, ok := desired[relSlash]; ok {
			return nil
		}
		// Only mark managed symlinks as stale. User symlinks
		// (pointing outside the cache dir) are preserved.
		if !isManagedSymlink(path) {
			diags = append(diags, diag.Diagnostic{
				Code:     "materialize.collision.user-owned",
				Severity: diag.SeverityInfo,
				Message:  fmt.Sprintf("preserved user-owned symlink %q (not managed by agent-marketplace)", relSlash),
			})
			return nil
		}
		stale = append(stale, relSlash)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, diags, nil
		}
		return nil, diags, err
	}
	sort.Strings(stale)
	return stale, diags, nil
}

func cleanEmptyArtifactDirs(outDir, relPath string) {
	if relPath == "." || relPath == "" {
		return
	}
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for i := len(parts); i >= 1; i-- {
		dirPath := filepath.Join(outDir, filepath.FromSlash(strings.Join(parts[:i], "/")))
		if dirPath == outDir {
			break
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil || len(entries) > 0 {
			break
		}
		_ = os.Remove(dirPath)
	}
}

// cleanEmptyPortableArtifactDirs is the portable counterpart of
// cleanEmptyArtifactDirs. Each directory is validated before reading or
// removing it so cleanup never traverses an intermediate symlink.
func cleanEmptyPortableArtifactDirs(outDir, relPath string) {
	if relPath == "." || relPath == "" {
		return
	}
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for i := len(parts); i >= 1; i-- {
		rel := filepath.FromSlash(strings.Join(parts[:i], "/"))
		// Validate rel as a leaf by asking for a fictitious child. This checks
		// rel and all of its parents without following rel if it is a symlink.
		child, err := portableOutputPath(outDir, filepath.ToSlash(filepath.Join(rel, ".placeholder")))
		if err != nil {
			return
		}
		dirPath := filepath.Dir(child)
		info, err := os.Lstat(dirPath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil || len(entries) > 0 {
			return
		}
		_ = os.Remove(dirPath)
	}
}

func prepareSkillLinkCaches(links []skillLink, dryRun bool) error {
	if dryRun {
		return nil
	}
	for _, link := range links {
		if err := prepareSkillLinkCache(link); err != nil {
			return err
		}
	}
	return nil
}

func prepareSkillLinkCache(link skillLink) error {
	if len(link.artifacts) == 0 {
		return nil
	}
	cacheDir := link.cacheDir
	if cacheDir == "" {
		cacheDir = link.target
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	// Write compiled artifacts (SKILL.md) without pruning, because
	// support files copied below are not in the artifact list and
	// would be incorrectly pruned.
	result := &hcc.Result{Artifacts: link.artifacts}
	_, err := hcc.Materialize(result, cacheDir, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	if err != nil {
		return err
	}
	// Copy support files from the source skill directory into the cache
	// so the symlinked skill directory contains the full skill content
	// (not just the compiled SKILL.md). Prune the old support tree first:
	// overwrite materialization intentionally does not prune because compiled
	// artifacts and source support files share the same cache directory.
	if link.sourceDir != "" {
		compiled := make(map[string]bool, len(link.artifacts))
		for _, a := range link.artifacts {
			compiled[filepath.ToSlash(filepath.Clean(a.Path))] = true
		}
		if err := pruneSkillCacheSupportFiles(cacheDir, compiled); err != nil {
			return err
		}
		if copyErr := copySupportFilesExcluding(link.sourceDir, cacheDir, compiled, ""); copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// pruneSkillCacheSupportFiles deletes every previous support-file entry while
// retaining current compiled artifacts and the directories required to contain
// them. The cache is fully managed; this makes its support subtree exactly
// reflect the current source tree on every real sync.
func pruneSkillCacheSupportFiles(cacheDir string, compiled map[string]bool) error {
	var prune func(string, string) error
	prune = func(dir, rel string) error {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childRel := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			childPath := filepath.Join(dir, entry.Name())
			keep := compiled[childRel]
			containsCompiled := false
			for artifactPath := range compiled {
				if strings.HasPrefix(artifactPath, childRel+"/") {
					containsCompiled = true
					break
				}
			}
			if keep {
				continue
			}
			if containsCompiled && entry.IsDir() {
				if err := prune(childPath, childRel); err != nil {
					return err
				}
				continue
			}
			if err := os.RemoveAll(childPath); err != nil {
				return err
			}
		}
		return nil
	}
	return prune(cacheDir, "")
}

// copySupportFiles copies all files from srcDir to dstDir except the compiled
// SKILL.md artifact. It remains a small convenience wrapper for callers that
// do not need to protect additional compiled artifact paths.
func copySupportFiles(srcDir, dstDir string) error {
	return copySupportFilesExcluding(srcDir, dstDir, nil, "")
}

func copySupportFilesExcluding(srcDir, dstDir string, compiled map[string]bool, rel string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read source skill dir %s: %w", srcDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		childRel := filepath.ToSlash(filepath.Join(rel, name))
		if childRel == "SKILL.md" || compiled[childRel] {
			continue
		}
		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(dstDir, name)

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return fmt.Errorf("create support dir %s: %w", childRel, err)
			}
			if err := copySupportFilesExcluding(srcPath, dstPath, compiled, childRel); err != nil {
				return fmt.Errorf("copy support dir %s: %w", childRel, err)
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy support file %s: %w", childRel, err)
		}
	}
	return nil
}

// collectHookSupportFiles scans the directory containing the source hooks.json
// for non-hooks.json files and returns them as regular (non-fragment) artifacts.
// These artifacts are symlinked into the hooks output directory alongside the
// merged hooks.json, and are automatically pruned by staleArtifactLinks when
// a hook is excluded (only symlinks are pruned, never the regular hooks.json).
func collectHookSupportFiles(sourcePath string, target hcc.Target) []artifact.Artifact {
	if sourcePath == "" {
		return nil
	}
	sourceDir := filepath.Dir(sourcePath)
	names, err := catalog.HookSupportFiles(sourcePath)
	if err != nil {
		return nil
	}
	hooksDirRel := resourceInstallDirRel(target, ir.KindHook)
	if hooksDirRel == "" {
		return nil
	}
	var artifacts []artifact.Artifact
	for _, name := range names {
		srcPath := filepath.Join(sourceDir, name)
		data, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		info, err := os.Stat(srcPath)
		if err != nil {
			continue
		}
		artifacts = append(artifacts, artifact.Artifact{
			Path:  hooksDirRel + "/" + name,
			Bytes: data,
			Mode:  info.Mode(),
		})
	}
	return artifacts
}

func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	// If dst exists and matches, skip.
	if dstInfo, err := os.Lstat(dst); err == nil {
		if dstInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("destination %s is a symlink; refusing to follow or overwrite it", dst)
		}
		if os.SameFile(srcInfo, dstInfo) {
			return nil
		}
		if dstInfo.Size() == srcInfo.Size() {
			same, err := filesEqual(src, dst)
			if err != nil {
				return err
			}
			if same {
				if dstInfo.Mode() != srcInfo.Mode() {
					return os.Chmod(dst, srcInfo.Mode())
				}
				return nil
			}
		}
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWriteFile(dst, data, srcInfo.Mode())
}

func atomicWriteFile(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agent-marketplace-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(mode.Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// portablePathDigest returns a stable digest for a materialized regular file or
// tree. It intentionally includes relative paths, file types, permissions, and
// bytes so a chmod, added support file, or content edit revokes manifest
// ownership. Lstat keeps output symlinks from being followed.
func portablePathDigest(path string) (string, error) {
	h := sha256.New()
	if err := writePortableDigestPath(h, path, ""); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func writePortableDigestPath(h io.Writer, path, rel string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	mode := info.Mode()
	writeHeader := func(kind string) {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%#o\x00", kind, filepath.ToSlash(rel), mode.Perm())
	}
	switch {
	case mode.IsRegular():
		writeHeader("file")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%d\x00", len(data))
		_, _ = h.Write(data)
		return nil
	case mode.IsDir():
		writeHeader("dir")
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := writePortableDigestPath(h, filepath.Join(path, entry.Name()), filepath.Join(rel, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	case mode&os.ModeSymlink != 0:
		writeHeader("symlink")
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%d\x00%s", len(target), target)
		return nil
	default:
		writeHeader("other")
		return nil
	}
}

func portableArtifactMode(mode fs.FileMode) fs.FileMode {
	if mode == 0 {
		return 0o644
	}
	return mode.Perm()
}

func portableArtifactDigest(a artifact.Artifact) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "file\x00\x00%#o\x00%d\x00", portableArtifactMode(a.Mode), len(a.Bytes))
	_, _ = h.Write(a.Bytes)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func filesEqual(src, dst string) (_ bool, err error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(err, srcFile.Close())
	}()

	dstFile, err := os.Open(dst)
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(err, dstFile.Close())
	}()

	srcBufPtr := bufPool.Get().(*[]byte)
	dstBufPtr := bufPool.Get().(*[]byte)
	defer bufPool.Put(srcBufPtr)
	defer bufPool.Put(dstBufPtr)
	srcBuf := *srcBufPtr
	dstBuf := *dstBufPtr
	for {
		srcN, srcErr := srcFile.Read(srcBuf)
		dstN, dstErr := dstFile.Read(dstBuf)
		if srcN != dstN || !bytes.Equal(srcBuf[:srcN], dstBuf[:dstN]) {
			return false, nil
		}
		if srcErr == io.EOF && dstErr == io.EOF {
			return true, nil
		}
		if srcErr == io.EOF || dstErr == io.EOF {
			return false, nil
		}
		if srcErr != nil {
			return false, srcErr
		}
		if dstErr != nil {
			return false, dstErr
		}
	}
}

func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func syncSkillLinks(skillsDir string, links []skillLink, dryRun bool, pruneStaleEntries bool, portable ...portableSyncContext) (TargetReport, error) {
	var state *portableState
	var target hcc.Target
	var reportPrefix string
	var portableDesired map[string]bool
	if len(portable) > 0 {
		state, target, reportPrefix, portableDesired = portable[0].state, portable[0].target, portable[0].reportPrefix, portable[0].desired
	}
	report := TargetReport{OutDir: skillsDir, ResourceByPath: make(map[string]string, len(links))}
	desired := make(map[string]string, len(links))
	desiredDigests := make(map[string]string, len(links))
	linkMeta := make(map[string]skillLink, len(links))
	orderedLinks := append([]skillLink(nil), links...)
	slices.SortFunc(orderedLinks, func(a, b skillLink) int {
		if a.name != b.name {
			return strings.Compare(a.name, b.name)
		}
		return strings.Compare(a.target, b.target)
	})
	for _, link := range orderedLinks {
		if state != nil {
			digest, err := portableLinkedDesiredDigest(link, dryRun)
			if err != nil {
				return report, err
			}
			desiredDigests[link.name] = digest
		}
		desired[link.name] = link.target
		linkMeta[link.name] = link
		if link.resourceName != "" {
			report.ResourceByPath[link.name] = link.resourceName
		}
	}

	if !dryRun {
		if err := ensureDirectoryReplacingSymlink(skillsDir); err != nil {
			return report, err
		}
	}

	for name, targetPath := range desired {
		linkPath := filepath.Join(skillsDir, name)
		manifestPath := filepath.ToSlash(filepath.Join(reportPrefix, name))
		if portableDesired != nil {
			portableDesired[manifestPath] = true
		}
		entry, manifestOwned := portableManifestEntry{}, false
		if state != nil {
			entry, manifestOwned = state.entry(target, manifestPath)
		}
		migrationAllowed := false
		if manifestOwned {
			if info, err := os.Lstat(linkPath); err == nil && info.Mode()&os.ModeSymlink == 0 {
				currentDigest, digestErr := portablePathDigest(linkPath)
				if digestErr != nil {
					return report, digestErr
				}
				migrationAllowed = (entry.Digest != "" && currentDigest == entry.Digest) ||
					(entry.Digest == "" && currentDigest == desiredDigests[name])
			} else {
				migrationAllowed = true
			}
		}
		result := ensureSymlinkPathWithOwnership(linkPath, targetPath, dryRun, migrationAllowed)
		if result.err != nil {
			return report, result.err
		}
		if result.skippedUserOwned {
			report.Skipped = append(report.Skipped, name)
			meta := linkMeta[name]
			resName := meta.resourceName
			if resName == "" {
				resName = strings.TrimSuffix(name, filepath.Ext(name))
			}
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.collision.user-owned",
				Severity: diag.SeverityWarning,
				Resource: resName,
				Message:  collisionRemoveHint(linkPath, name, meta),
			})
			continue
		}
		if result.alreadySynced {
			report.Skipped = append(report.Skipped, name)
			if state != nil && !dryRun {
				state.remove(target, manifestPath)
			}
			continue
		}
		report.Written = append(report.Written, name)
		if state != nil && !dryRun {
			state.remove(target, manifestPath)
		}
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) || dryRun {
			return report, nil
		}
		return report, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if _, keep := desired[name]; keep {
			continue
		}
		path := filepath.Join(skillsDir, name)
		if _, ok := existingSymlinkTarget(path); !ok {
			// Non-symlink directories are user-owned skill directories
			// and must be preserved. Mirror the principle from
			// staleArtifactLinks which also preserves user-owned content.
			if entry.IsDir() {
				if pruneStaleEntries {
					report.Diags = append(report.Diags, diag.Diagnostic{
						Code:     "materialize.skill.user-owned-preserved",
						Severity: diag.SeverityInfo,
						Message:  fmt.Sprintf("preserved user-owned skill directory %q (not managed by sync)", name),
					})
				}
				continue
			}
			if !pruneStaleEntries {
				continue
			}
			// Non-symlink regular files are only pruned when they carry the
			// agent-marketplace ownership marker. User-authored files (e.g.
			// hand-written Factory droids like worker.md) have no marker and
			// must be preserved — managed resources in this directory are
			// materialized as symlinks, so an unmarked regular file is always
			// user-owned.
			if !isManagedCommandFile(path) {
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     "materialize.collision.user-owned",
					Severity: diag.SeverityInfo,
					Message:  fmt.Sprintf("preserved user-owned file %q (not managed by agent-marketplace)", name),
				})
				continue
			}
			report.Removed = append(report.Removed, staleSkillEntryReportPath(path, name))
			if dryRun {
				continue
			}
			if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
				return report, err
			}
			continue
		}
		// Symlink — only prune if it points to our cache (managed).
		// User symlinks (pointing outside the cache) are preserved.
		if !isManagedSymlink(path) {
			if pruneStaleEntries {
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     "materialize.collision.user-owned",
					Severity: diag.SeverityInfo,
					Message:  fmt.Sprintf("preserved user-owned symlink %q (not managed by agent-marketplace)", name),
				})
			}
			continue
		}
		report.Removed = append(report.Removed, name)
		if dryRun {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return report, err
		}
	}

	return report, nil
}

func ensureDirectoryReplacingSymlink(path string) error {
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return os.MkdirAll(path, 0o755)
}

// syncCommandFiles materializes resource links as real files (not symlinks),
// injecting a `_managed_by` frontmatter marker so the files can be safely
// overwritten and pruned. It is the file-copy counterpart of syncSkillLinks.
//
// TEMPORARY WORKAROUND: this path exists only because Factory's droid CLI does
// not follow symlinked command files. Remove it (and TargetInfo.FileCopyKinds)
// once Factory resolves symlink loading for commands.
func syncCommandFiles(dir string, links []skillLink, dryRun, pruneStaleEntries bool) (TargetReport, error) {
	report := TargetReport{OutDir: dir, ResourceByPath: make(map[string]string, len(links))}
	desired := make(map[string][]byte, len(links))
	linkMeta := make(map[string]skillLink, len(links))
	for _, link := range links {
		if len(link.artifacts) == 0 {
			continue
		}
		desired[link.name] = injectManagedByFrontmatter(link.artifacts[0].Bytes)
		linkMeta[link.name] = link
		if link.resourceName != "" {
			report.ResourceByPath[link.name] = link.resourceName
		}
	}

	if !dryRun {
		if err := ensureDirectoryReplacingSymlink(dir); err != nil {
			return report, err
		}
	}

	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		content := desired[name]
		filePath := filepath.Join(dir, name)
		info, err := os.Lstat(filePath)
		switch {
		case os.IsNotExist(err):
			// No existing entry — write it.
		case err != nil:
			return report, err
		case info.Mode()&os.ModeSymlink != 0:
			// Migrate a previously materialized managed symlink to a file.
			// User-owned symlinks are preserved with a collision warning.
			if !isManagedSymlink(filePath) {
				report.Skipped = append(report.Skipped, name)
				report.Diags = append(report.Diags, userOwnedCommandDiag(filePath, name, linkMeta[name]))
				continue
			}
		case info.IsDir():
			report.Skipped = append(report.Skipped, name)
			report.Diags = append(report.Diags, userOwnedCommandDiag(filePath, name, linkMeta[name]))
			continue
		default:
			// Regular file — only overwrite if it carries our marker.
			if !isManagedCommandFile(filePath) {
				report.Skipped = append(report.Skipped, name)
				report.Diags = append(report.Diags, userOwnedCommandDiag(filePath, name, linkMeta[name]))
				continue
			}
			if existing, readErr := os.ReadFile(filePath); readErr == nil && bytes.Equal(existing, content) {
				report.Skipped = append(report.Skipped, name)
				continue
			}
		}

		report.Written = append(report.Written, name)
		if dryRun {
			continue
		}
		if info != nil && info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				return report, err
			}
		}
		if err := atomicWriteFile(filePath, content, 0o644); err != nil {
			return report, err
		}
	}

	if pruneStaleEntries {
		if err := pruneStaleCommandFiles(dir, desired, dryRun, &report); err != nil {
			return report, err
		}
	}

	sort.Strings(report.Written)
	sort.Strings(report.Skipped)
	sort.Strings(report.Removed)
	return report, nil
}

// pruneStaleCommandFiles removes managed command files and stale managed
// symlinks in dir that are no longer desired. User-owned entries are preserved.
func pruneStaleCommandFiles(dir string, desired map[string][]byte, dryRun bool, report *TargetReport) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if _, keep := desired[name]; keep {
			continue
		}
		path := filepath.Join(dir, name)
		info, lerr := os.Lstat(path)
		if lerr != nil {
			continue
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if !isManagedSymlink(path) {
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     "materialize.collision.user-owned",
					Severity: diag.SeverityInfo,
					Message:  fmt.Sprintf("preserved user-owned symlink %q (not managed by agent-marketplace)", name),
				})
				continue
			}
		case info.IsDir():
			continue
		default:
			if !isManagedCommandFile(path) {
				continue
			}
		}
		report.Removed = append(report.Removed, name)
		if dryRun {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func userOwnedCommandDiag(filePath, name string, meta skillLink) diag.Diagnostic {
	resName := meta.resourceName
	if resName == "" {
		resName = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return diag.Diagnostic{
		Code:     "materialize.collision.user-owned",
		Severity: diag.SeverityWarning,
		Resource: resName,
		Message:  collisionRemoveHint(filePath, name, meta),
	}
}

// injectManagedByFrontmatter returns content with a `_managed_by:
// agent-marketplace` key in its YAML frontmatter. If content already has a
// frontmatter block, the key is inserted right after the opening delimiter
// (preserving existing keys/order); otherwise a new block is prepended. The
// operation is idempotent.
func injectManagedByFrontmatter(content []byte) []byte {
	marker := managedByField + ": " + managedByValue
	if bytes.Contains(content, []byte(marker)) {
		return content
	}
	if !hasFrontmatter(content) {
		header := "---\n" + marker + "\n---\n\n"
		return append([]byte(header), content...)
	}
	// Insert the marker line immediately after the opening "---\n".
	idx := bytes.Index(content, []byte("---\n"))
	insertAt := idx + len("---\n")
	out := make([]byte, 0, len(content)+len(marker)+1)
	out = append(out, content[:insertAt]...)
	out = append(out, []byte(marker+"\n")...)
	out = append(out, content[insertAt:]...)
	return out
}

// isManagedCommandFile reports whether the file at path is a command markdown
// file carrying the `_managed_by: agent-marketplace` ownership marker in its
// leading frontmatter block.
func isManagedCommandFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if !bytes.Contains(data, []byte(managedByField)) {
		return false
	}
	if !hasFrontmatter(data) {
		return false
	}
	trimmed := strings.TrimLeft(string(data), "\ufeff\n\r\t ")
	end := strings.Index(trimmed[len("---\n"):], "\n---")
	if end < 0 {
		return false
	}
	block := trimmed[len("---\n") : len("---\n")+end]
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) == managedByField+": "+managedByValue {
			return true
		}
	}
	return false
}

func staleSkillEntryReportPath(path, name string) string {
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
		return filepath.Join(name, "SKILL.md")
	}
	return name
}

func existingSymlinkTarget(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", false
	}
	return target, true
}

// isManagedSymlink reports whether the path is a symlink whose target
// resides under the agent-marketplace cache directory. Only symlinks
// targeting the cache are considered agent-marketplace-managed; all
// others are treated as user-owned and must not be replaced or pruned.
func isManagedSymlink(path string) bool {
	target, ok := existingSymlinkTarget(path)
	if !ok {
		return false
	}
	return strings.HasPrefix(target, cache.CacheDir()+string(os.PathSeparator))
}

func codexMachineManagedPathsFromSnapshot(destinations []harness.Destination, entries map[string]portableManifestEntry) (map[string][]string, error) {
	paths := make(map[string]map[string]bool, len(destinations))
	for _, destination := range destinations {
		paths[destination.ID] = make(map[string]bool)
		for _, component := range destination.Info.ComponentsForScope(true) {
			entries, err := os.ReadDir(filepath.Join(destination.Root, component))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			for _, entry := range entries {
				rel := filepath.Join(component, entry.Name())
				if isManagedSymlink(filepath.Join(destination.Root, rel)) {
					paths[destination.ID][filepath.ToSlash(rel)] = true
				}
			}
		}
		store, err := ownership.Open(destination.Root)
		if err != nil {
			return nil, err
		}
		for _, rel := range destination.Info.AggregateFilesForScope(true) {
			proof, err := store.Proof(rel)
			if err == nil && proof.OwnedCount() > 0 {
				paths[destination.ID][filepath.ToSlash(rel)] = true
			}
		}
	}
	for _, entry := range entries {
		if entry.Target != string(hcc.Target(targetmeta.TargetCodex)) {
			continue
		}
		for _, destination := range destinations {
			prefix := strings.TrimSuffix(filepath.ToSlash(destination.Prefix), "/") + "/"
			if rel, ok := strings.CutPrefix(filepath.ToSlash(entry.Path), prefix); ok {
				paths[destination.ID][filepath.ToSlash(rel)] = true
				break
			}
		}
	}
	result := make(map[string][]string, len(paths))
	for id, set := range paths {
		for rel := range set {
			result[id] = append(result[id], rel)
		}
		sort.Strings(result[id])
	}
	return result, nil
}

// CodexMachineManagedPaths returns existing, evidenced machine output grouped
// by native destination ID. It deliberately never returns a whole component
// directory: configured backup/restore must not capture handwritten siblings.
func CodexMachineManagedPaths(repoRoot string) (map[string][]string, error) {
	target := hcc.Target(targetmeta.TargetCodex)
	destinations, err := harness.ResolveDestinations(target, true, repoRoot)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]map[string]bool, len(destinations))
	for _, destination := range destinations {
		paths[destination.ID] = make(map[string]bool)
		for _, component := range destination.Info.ComponentsForScope(true) {
			entries, err := os.ReadDir(filepath.Join(destination.Root, component))
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			for _, entry := range entries {
				rel := filepath.Join(component, entry.Name())
				if isManagedSymlink(filepath.Join(destination.Root, rel)) {
					paths[destination.ID][filepath.ToSlash(rel)] = true
				}
			}
		}
		store, err := ownership.Open(destination.Root)
		if err != nil {
			return nil, err
		}
		for _, rel := range destination.Info.AggregateFilesForScope(true) {
			proof, err := store.Proof(rel)
			if err == nil && proof.OwnedCount() > 0 {
				paths[destination.ID][filepath.ToSlash(rel)] = true
			}
		}
	}
	state, err := loadPortableState(repoRoot, true)
	if err != nil {
		return nil, err
	}
	key := outputKey{target: target, machineScope: true}
	for _, entry := range state.entries {
		if entry.Target != string(target) {
			continue
		}
		if !state.matchesCodexRoot(entry) {
			continue
		}
		id, _, rel, err := portableEntryDestination(key, "", repoRoot, entry.Path)
		if err != nil {
			return nil, err
		}
		paths[id][filepath.ToSlash(rel)] = true
	}
	result := make(map[string][]string, len(paths))
	for id, set := range paths {
		for rel := range set {
			result[id] = append(result[id], rel)
		}
		sort.Strings(result[id])
	}
	return result, nil
}

const (
	maxMaterializationIssueMessage = 1024
	maxMaterializationIssueField   = 256
	maxMaterializationIssueEnum    = 64
)

func resourceMapForPaths(paths []string, prefix string, metadata map[string]skillLink) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	resources := make(map[string]string)
	for _, path := range paths {
		path = filepath.ToSlash(path)
		for artifactPath, meta := range metadata {
			artifactPath = filepath.ToSlash(artifactPath)
			if artifactPath == path || filepath.ToSlash(filepath.Join(prefix, path)) == artifactPath {
				if meta.resourceName != "" {
					resources[path] = meta.resourceName
				}
				break
			}
		}
	}
	return resources
}

func materializationScope(machine bool) string {
	if machine {
		return "machine"
	}
	return "project"
}

// IsMaterializationDiagnostic reports whether a diagnostic was authored by the
// materialization layer rather than by the harness compiler. Compiler
// diagnostics stay in the CLI's normal diagnostic renderer; only diagnostics
// owned by materialization become structured SurfaceIssues.
func IsMaterializationDiagnostic(d diag.Diagnostic) bool {
	return IsMaterializationDiagnosticCode(d.Code)
}

func IsMaterializationDiagnosticCode(code string) bool {
	// ownership.* diagnostics are materialization-owned.
	return strings.HasPrefix(code, "materialize.") || strings.HasPrefix(code, "ownership.")
}

func boundedMaterializationMessage(message string) string {
	return diag.SanitizeText(message, maxMaterializationIssueMessage)
}

func sanitizeMaterializationIssue(issue MaterializationIssue) MaterializationIssue {
	issue.Resource = diag.SanitizeText(issue.Resource, maxMaterializationIssueField)
	issue.Target = hcc.Target(diag.SanitizeText(string(issue.Target), maxMaterializationIssueField))
	issue.Scope = diag.SanitizeText(issue.Scope, maxMaterializationIssueField)
	issue.RootID = redactAbsoluteMaterializationPath(diag.SanitizeText(issue.RootID, maxMaterializationIssueField))
	issue.Path = redactAbsoluteMaterializationPath(diag.SanitizeText(issue.Path, maxMaterializationIssueField))
	issue.Operation = MaterializationOperation(diag.SanitizeText(string(issue.Operation), maxMaterializationIssueEnum))
	issue.Outcome = MaterializationOutcome(diag.SanitizeText(string(issue.Outcome), maxMaterializationIssueEnum))
	issue.Code = diag.SanitizeText(issue.Code, maxMaterializationIssueField)
	issue.Message = diagnosticidentity.NormalizeMessage(boundedMaterializationMessage(issue.Message))
	return issue
}

func redactAbsoluteMaterializationPath(value string) string {
	if filepath.IsAbs(value) || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		return ""
	}
	if len(value) > 2 && value[1] == ':' && (value[2] == '/' || value[2] == '\\') {
		return ""
	}
	return value
}

func (tr *TargetReport) buildIssues() {
	if tr == nil {
		return
	}
	// TargetReport.Issues may already contain producer-authored records. Keep
	// them and merge diagnostics-derived records before Report canonicalization.
	existing := append([]MaterializationIssue(nil), tr.Issues...)
	issues := make([]MaterializationIssue, 0, len(existing)+len(tr.Written)+len(tr.Skipped)+len(tr.Removed)+len(tr.Diags))
	seen := make(map[string]struct{}, len(existing)+len(tr.Diags))
	appendIssue := func(issue MaterializationIssue) {
		key := materializationIssueKey(issue)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		issues = append(issues, issue)
	}
	for _, issue := range existing {
		appendIssue(issue)
	}
	addPath := func(path string, operation MaterializationOperation, outcome MaterializationOutcome) {
		path = filepath.ToSlash(path)
		appendIssue(MaterializationIssue{
			Resource: tr.ResourceByPath[path], Target: tr.Target, Scope: materializationScope(tr.MachineScope),
			RootID: tr.RootID, Path: path, Operation: operation, Outcome: outcome,
			Severity: diag.SeverityInfo,
		})
	}
	for _, path := range tr.Written {
		addPath(path, MaterializationOperationWrite, MaterializationOutcomeWritten)
	}
	for _, path := range tr.Skipped {
		addPath(path, MaterializationOperationSkip, MaterializationOutcomeSkipped)
	}
	for _, path := range tr.Removed {
		addPath(path, MaterializationOperationRemove, MaterializationOutcomeRemoved)
	}
	for _, d := range tr.Diags {
		if !IsMaterializationDiagnostic(d) {
			continue
		}
		operation, outcome := MaterializationOperationFailure, MaterializationOutcomeFailed
		if !d.IsError() {
			operation, outcome = MaterializationOperationSkip, MaterializationOutcomeWarning
		}
		path := d.Source.File
		if path == "" {
			path = skippedPathForResource(*tr, d.Resource)
		}
		appendIssue(MaterializationIssue{
			Resource: d.Resource, Target: tr.Target, Scope: materializationScope(tr.MachineScope), RootID: tr.RootID,
			Path: path, Code: d.Code, Severity: d.Severity, Message: d.Message,
			Operation: operation, Outcome: outcome,
		})
	}
	tr.Issues = issues
}

func skippedPathForResource(tr TargetReport, resource string) string {
	if resource == "" || len(tr.ResourceByPath) == 0 {
		return ""
	}
	var match string
	for _, path := range tr.Skipped {
		path = filepath.ToSlash(path)
		if tr.ResourceByPath[path] != resource {
			continue
		}
		if match != "" {
			return ""
		}
		match = path
	}
	return match
}

func fullMaterializationField(value string) string {
	return diagnosticidentity.FullText(value)
}

func materializationIssueKey(issue MaterializationIssue) string {
	if issue.identity != "" {
		return issue.identity
	}
	return materializationIssueDigest(issue)
}

func materializationIssuePresentationKey(issue MaterializationIssue) string {
	if issue.presentationIdentity != "" {
		return issue.presentationIdentity
	}
	issue.identity = ""
	issue.Target, issue.RootID = "", ""
	return materializationIssueDigest(issue)
}

func materializationIssueDigest(issue MaterializationIssue) string {
	identity := materializationIssueCanonicalIdentity(issue)
	sum := sha256.Sum256([]byte(identity))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func materializationIssueCanonicalIdentity(issue MaterializationIssue) string {
	return diagnosticidentity.Canonical(
		diagnosticidentity.Field{Label: "target", Value: fullMaterializationField(string(issue.Target))},
		diagnosticidentity.Field{Label: "scope", Value: fullMaterializationField(issue.Scope)},
		diagnosticidentity.Field{Label: "root", Value: fullMaterializationField(issue.RootID)},
		diagnosticidentity.Field{Label: "resource", Value: fullMaterializationField(issue.Resource)},
		diagnosticidentity.Field{Label: "source.file", Value: fullMaterializationField(issue.Path)},
		diagnosticidentity.Field{Label: "operation", Value: fullMaterializationField(string(issue.Operation))},
		diagnosticidentity.Field{Label: "outcome", Value: fullMaterializationField(string(issue.Outcome))},
		diagnosticidentity.Field{Label: "code", Value: fullMaterializationField(issue.Code)},
		diagnosticidentity.Field{Label: "message", Value: diagnosticidentity.NormalizeMessage(fullMaterializationField(issue.Message))},
		diagnosticidentity.Field{Label: "severity", Value: strconv.Itoa(int(issue.Severity))},
	)
}

func materializationDiagnosticKey(issue MaterializationIssue) string {
	issue.identity = ""
	issue.Scope, issue.RootID, issue.Path = "", "", ""
	issue.Operation, issue.Outcome = "", ""
	return materializationIssueKey(issue)
}

func materializationDiagnosticFallbackKey(issue MaterializationIssue) string {
	issue.identity = ""
	issue.Scope, issue.RootID, issue.Path = "", "", ""
	issue.Operation, issue.Outcome = "", ""
	return materializationIssueKey(issue)
}

func normalizeMaterializationIssue(issue MaterializationIssue) MaterializationIssue {
	issue.identity = materializationIssueKey(issue)
	issue.presentationIdentity = materializationIssuePresentationKey(issue)
	return sanitizeMaterializationIssue(issue)
}

func (r *Report) rebuildIssues() {
	if r == nil {
		return
	}
	issues := make([]MaterializationIssue, 0)
	seen := make(map[string]struct{})
	appendIssue := func(issue MaterializationIssue) {
		if issue.Code != "" && !IsMaterializationDiagnosticCode(issue.Code) {
			return
		}
		key := materializationIssueKey(issue)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		issues = append(issues, normalizeMaterializationIssue(issue))
	}

	// Report.Issues can be populated by callers before materialization adds its
	// generated issues. Preserve those records, but sanitize and deduplicate
	// them through the same path as generated records.
	for _, issue := range r.Issues {
		appendIssue(issue)
	}
	reports := r.TargetReports()
	for i := range reports {
		reports[i].buildIssues()
		for _, issue := range reports[i].Issues {
			appendIssue(issue)
		}
		if len(r.PerRoot) == 0 && r.PerTarget != nil {
			r.PerTarget[reports[i].Target] = reports[i]
		}
	}
	// Diagnostics are also retained when no target report was produced (for
	// example, a materialization or destination failure). A producer may have
	// already attached a diagnostic to a root-aware issue; do not add a second,
	// un-attributed copy from Report.Diags in that case.
	attributed := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		if IsMaterializationDiagnosticCode(issue.Code) {
			attributed[materializationDiagnosticKey(issue)] = struct{}{}
			// Report-level producer issues may carry an output-relative Path
			// for structured consumers while the generic Diagnostic has no
			// source path. Keep a path-free fallback only for that comparison;
			// the public issue identity still retains Path.
			attributed[materializationDiagnosticFallbackKey(issue)] = struct{}{}
		}
	}
	for _, d := range r.Diags {
		if !IsMaterializationDiagnostic(d) {
			continue
		}
		issue := MaterializationIssue{
			Resource: d.Resource, Target: hcc.Target(d.Target), Code: d.Code, Severity: d.Severity,
			Path: d.Source.File, Message: d.Message, Operation: MaterializationOperationFailure,
			Outcome: MaterializationOutcomeFailed,
		}
		if _, ok := attributed[materializationDiagnosticKey(issue)]; ok {
			continue
		}
		if _, ok := attributed[materializationDiagnosticFallbackKey(issue)]; ok {
			continue
		}
		reports := r.TargetReports()
		matches := make([]TargetReport, 0, 1)
		for _, tr := range reports {
			if issue.Target != "" && tr.Target != issue.Target {
				continue
			}
			if issue.Resource != "" && !reportHasResource(tr, issue.Resource) {
				continue
			}
			matches = append(matches, tr)
		}
		if len(matches) == 1 {
			issue.Scope = materializationScope(matches[0].MachineScope)
			issue.RootID = matches[0].RootID
		}
		appendIssue(issue)
	}
	r.Issues = issues
}

func reportHasResource(tr TargetReport, resource string) bool {
	if resource == "" {
		return true
	}
	for _, candidate := range tr.ResourceByPath {
		if candidate == resource {
			return true
		}
	}
	for _, issue := range tr.Issues {
		if issue.Resource == resource {
			return true
		}
	}
	return false
}

func (r *Report) TargetReports() []TargetReport {
	if r == nil {
		return nil
	}
	if len(r.PerRoot) > 0 {
		return r.PerRoot
	}
	reports := make([]TargetReport, 0, len(r.PerTarget))
	for _, tr := range r.PerTarget {
		reports = append(reports, tr)
	}
	slices.SortFunc(reports, func(a, b TargetReport) int {
		if a.Target != b.Target {
			return strings.Compare(string(a.Target), string(b.Target))
		}
		if a.MachineScope != b.MachineScope {
			if a.MachineScope {
				return 1
			}
			return -1
		}
		return strings.Compare(a.RootID, b.RootID)
	})
	return reports
}

func mergeTargetReports(a, b TargetReport) TargetReport {
	if a.OutDir == "" {
		a.Target = b.Target
		a.MachineScope = b.MachineScope
		a.OutDir = b.OutDir
	}
	a.Written = append(a.Written, b.Written...)
	a.Skipped = append(a.Skipped, b.Skipped...)
	a.Removed = append(a.Removed, b.Removed...)
	a.Diags = append(a.Diags, b.Diags...)
	a.Issues = append(a.Issues, b.Issues...)
	if len(b.ResourceByPath) > 0 {
		if a.ResourceByPath == nil {
			a.ResourceByPath = make(map[string]string, len(b.ResourceByPath))
		}
		for path, resource := range b.ResourceByPath {
			a.ResourceByPath[path] = resource
		}
	}
	return a
}

// TotalWritten returns the total number of files written across all targets.
func (r *Report) TotalWritten() int {
	n := 0
	for _, tr := range r.TargetReports() {
		n += len(tr.Written)
	}
	return n
}

// TotalSkipped returns the total number of files skipped across all targets.
func (r *Report) TotalSkipped() int {
	n := 0
	for _, tr := range r.TargetReports() {
		n += len(tr.Skipped)
	}
	return n
}

// TotalRemoved returns the total number of files removed across all targets.
func (r *Report) TotalRemoved() int {
	n := 0
	for _, tr := range r.TargetReports() {
		n += len(tr.Removed)
	}
	return n
}

// InSync reports whether all targets have zero pending changes (no written
// or removed files, no error diagnostics, and no retained structured
// MaterializationIssue errors). Useful for --check mode.
func (r *Report) InSync() bool {
	r.rebuildIssues()
	if r.TotalWritten() > 0 || r.TotalRemoved() > 0 {
		return false
	}
	for _, d := range r.Diags {
		if d.IsError() {
			return false
		}
	}
	for _, issue := range r.Issues {
		if issue.Severity.AtLeastAsSevereAs(diag.SeverityError) {
			return false
		}
	}
	return true
}
