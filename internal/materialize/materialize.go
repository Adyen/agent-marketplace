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
	"slices"
	"sort"
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

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
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

type skillLink struct {
	name            string
	target          string
	cacheDir        string
	artifacts       []artifact.Artifact
	sourceDir       string // absolute path to the original skill directory on disk
	declaredSupport []string
	kind            ir.ResourceKind // kind of the resource (skill, subagent, command, …)
	resourceName    string          // canonical resource name without file extension
	machineScope    bool            // true when installed to machine-wide (user) scope
	registryURL     string          // URL of the source registry
	registryPath    string          // sub-directory path within the registry (may be empty)
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

// Report holds the aggregate outcome of a materialization run.
type Report struct {
	PerTarget        map[hcc.Target]TargetReport
	PerRoot          []TargetReport
	Diags            []diag.Diagnostic
	BlockedResources []BlockedResource
}

// BlockedResource identifies a resource omitted during compilation for one
// harness target and install scope.
type BlockedResource struct {
	Kind         ir.ResourceKind
	Name         string
	Target       hcc.Target
	MachineScope bool
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
	path            string
	repoRoot        string
	machineScope    bool
	entries         map[string]portableManifestEntry
	pin             portableManifestSnapshot
	publicationHook func(string)
}

type portableSyncContext struct {
	boundary     *destinationBoundary
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
	state.pin, err = readPortableManifestSnapshot(path)
	if err != nil {
		return nil, err
	}
	if state.pin.data == nil {
		return state, nil
	}
	var manifest portableManifest
	if err := yaml.Unmarshal(state.pin.data, &manifest); err != nil {
		return nil, fmt.Errorf("parse materialization manifest %s: %w", path, err)
	}
	if manifest.Version != 0 && manifest.Version != 1 {
		return nil, fmt.Errorf("unsupported materialization manifest version %d", manifest.Version)
	}
	for _, entry := range manifest.Entries {
		clean := filepath.ToSlash(filepath.Clean(entry.Path))
		if entry.Target == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(entry.Path) {
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
		if id, root, _, err := portableEntryDestination(key, "", s.repoRoot, path); err == nil {
			entry.RootID, entry.RootKey = id, ownership.RootKey(root)
		}
	}
	s.entries[portableManifestKey(target, path)] = entry
}

func (s *portableState) matchesCodexRoot(entry portableManifestEntry) bool {
	key := outputKey{target: hcc.Target(targetmeta.TargetCodex), machineScope: s.machineScope}
	id, root, _, err := portableEntryDestination(key, "", s.repoRoot, entry.Path)
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
		return s.removeManifest()
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
	return s.publishManifest(data)
}

// RunOption configures optional behavior for Run.
type RunOption func(*runConfig)

type runConfig struct {
	dryRun                  bool
	scopeFilter             *bool // nil = all scopes; true = machine only; false = repo only
	modes                   config.MaterializationModes
	linkPublicationHook     func(string)
	pruneClassificationHook func(string)
	manifestPublicationHook func(string)
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
	artifacts  []artifact.Artifact
	diags      []diag.Diagnostic
	nativeOnly bool
}

// compilePhaseResult holds the output of the compile phase.
type compilePhaseResult struct {
	linkPublicationHook     func(string)
	pruneClassificationHook func(string)
	manifestPublicationHook func(string)
	scopeFilter             *bool
	allDiags                []diag.Diagnostic
	byTarget                map[outputKey]*targetWork
	skillLinks              map[outputKey]map[string]*skillLinkGroup
	allOutputs              map[outputKey]bool
	blocked                 blockedOutputs
	blockedResources        []BlockedResource
	// artifactMeta maps (outputKey → artifact relative path → skillLink metadata)
	// for artifacts that remain in result.Artifacts (not extracted into skillLinks).
	// Used to generate rich collision hints in syncArtifactLinks.
	artifactMeta map[outputKey]map[string]skillLink
	// Owned aggregate state is opaque to generic orchestration. Each target
	// adapter stores its JSON or TOML work value behind this map and exposes it
	// only through ownedAggregateHandler.
	ownedState map[outputKey]any
}

func (r compilePhaseResult) ownedAggregateState() ownedAggregateState {
	return ownedAggregateState{values: r.ownedState}
}

// Run compiles and materializes the given resolved skills into their
// per-target install roots. It groups skills by hcc.Target (defaulting
// to [TargetClaude] when Harnesses is nil), calls hcc.Compile per-skill-
// per-target, merges fragments, then calls hcc.Materialize with
// WriteModeSync and prune-stale enabled.
//
// Skills with Resource==nil are skipped with a warning diagnostic.
func Run(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, opts ...RunOption) (*Report, error) {
	start := time.Now()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var rc runConfig
	rc.modes = config.DefaultMaterializationModes()
	for _, o := range opts {
		o(&rc)
	}
	slog.Debug("materialize start", "repo_root", repoRoot, "resolved", len(resolved), "dry_run", rc.dryRun,
		"machine_mode", rc.modes.Machine, "project_mode", rc.modes.Project)

	// Phase 1: Group skills by target and track all output keys.
	work, allOutputs, blocked, allDiags := buildWorkItems(resolved, repoRoot)
	slog.Debug("materialize work built", "work_items", len(work), "outputs", len(allOutputs), "blocked", len(blocked), "diagnostics", len(allDiags))

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
		filtered := work[:0]
		for _, w := range work {
			if w.machineScope == *rc.scopeFilter {
				filtered = append(filtered, w)
			}
		}
		work = filtered
	}

	// Phase 2: Compile each work item and collect artifacts per target.
	compileResult := compileWorkItems(work, allDiags, allOutputs, blocked, repoRoot)
	compileResult.scopeFilter = rc.scopeFilter
	slog.Debug("materialize compile complete", "targets", len(compileResult.byTarget), "diagnostics", len(compileResult.allDiags))

	// Phase 3: Materialize per target — write artifacts and sync skill links.
	compileResult.linkPublicationHook = rc.linkPublicationHook
	compileResult.pruneClassificationHook = rc.pruneClassificationHook
	compileResult.manifestPublicationHook = rc.manifestPublicationHook
	report := executeMaterializePlans(ctx, compileResult, rc.dryRun, repoRoot, rc.modes)
	slog.Info("materialize end",
		"duration_ms", time.Since(start).Milliseconds(),
		"targets", len(report.PerRoot),
		"diagnostics", len(report.Diags),
	)
	return report, nil
}

// buildWorkItems groups resolved skills by target and identifies all output
// keys (including empty targets that need pruning).
func buildWorkItems(resolved []resolver.ResolvedArtifact, repoRoot string) ([]skillTarget, map[outputKey]bool, blockedOutputs, []diag.Diagnostic) {
	var work []skillTarget
	var allDiags []diag.Diagnostic
	allOutputs := make(map[outputKey]bool)
	blocked := make(blockedOutputs)

	for _, s := range resolver.MaterializationWinners(resolved) {
		if s.Unresolved {
			// Metadata-only status rows do not materialize and should not
			// claim output keys.
			continue
		}
		targets := s.Harnesses
		if targets == nil {
			targets = []hcc.Target{hcc.TargetClaude}
		}
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

	// Include previously materialized targets even when they have no
	// resolved artifacts in this run, so stale managed files get pruned.
	addExistingOutputKeys(repoRoot, allOutputs)
	addExistingNativeAggregateKeys(repoRoot, allOutputs)
	// A file-mode manifest may be the only remaining record after a harness
	// root is deleted. Include its known targets so deinit can still process
	// that state instead of silently skipping it.
	addPortableManifestOutputKeys(repoRoot, false, allOutputs, &allDiags)
	addPortableManifestOutputKeys(repoRoot, true, allOutputs, &allDiags)

	return work, allOutputs, blocked, allDiags
}

func addPortableManifestOutputKeys(repoRoot string, machineScope bool, allOutputs map[outputKey]bool, diags *[]diag.Diagnostic) {
	state, err := loadPortableState(repoRoot, machineScope)
	if err != nil {
		*diags = append(*diags, diag.Diagnostic{
			Code:     "materialize.manifest.error",
			Severity: diag.SeverityError,
			Message:  fmt.Sprintf("load materialization manifest: %v", err),
		})
		return
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
}

func addExistingOutputKeys(repoRoot string, allOutputs map[outputKey]bool) {
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
		if outputDirExists(repoDir) && !sameOutputDir(repoDir, machineDir) {
			allOutputs[outputKey{target: target, machineScope: false}] = true
		}
		if outputDirExists(machineDir) {
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
					continue
				}
				for _, destination := range destinations {
					// Shared native partitions are not installation markers, but durable
					// managed-link evidence must still revive their output key for prune.
					if !destination.InstallationEvidence {
						if destinationHasManagedSymlink(destination, machineScope) {
							allOutputs[outputKey{target: target, machineScope: machineScope}] = true
							break
						}
						continue
					}
					if outputDirExists(destination.Root) {
						allOutputs[outputKey{target: target, machineScope: machineScope}] = true
						break
					}
				}
			}
		}
	}
}

func destinationHasManagedSymlink(destination harness.Destination, machineScope bool) bool {
	for _, component := range destination.Info.ComponentsForScope(machineScope) {
		entries, err := os.ReadDir(filepath.Join(destination.Root, component))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if isManagedSymlink(filepath.Join(destination.Root, component, entry.Name())) {
				return true
			}
		}
	}
	return false
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

func outputDirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func marketplaceResourceForInstallName(resource ir.Resource, name string) (ir.Resource, error) {
	if err := ir.ValidateName(name); err != nil {
		return nil, err
	}
	switch typed := resource.(type) {
	case *ir.Skill:
		copy := *typed
		copy.SetName(name)
		return &copy, nil
	case *ir.Command:
		copy := *typed
		copy.SetName(name)
		return &copy, nil
	case *ir.Subagent:
		copy := *typed
		copy.SetName(name)
		return &copy, nil
	case *ir.Hook:
		copy := *typed
		copy.SetName(name)
		return &copy, nil
	case *ir.McpServer:
		copy := *typed
		copy.SetName(name)
		return &copy, nil
	default:
		return nil, fmt.Errorf("unsupported Marketplace resource kind %s for installed name", resource.Kind())
	}
}

// compileWorkItems compiles each skill-target pair, splits artifacts by kind,
// and groups results by output key.
func compileWorkItems(work []skillTarget, allDiags []diag.Diagnostic, allOutputs map[outputKey]bool, blocked blockedOutputs, repoRoot string) compilePhaseResult {
	byTarget := make(map[outputKey]*targetWork)
	skillLinks := make(map[outputKey]map[string]*skillLinkGroup)
	artifactMeta := make(map[outputKey]map[string]skillLink)
	ownedState := make(map[outputKey]any)
	ownedAggregates := ownedAggregateState{values: ownedState}
	hookSupport, supportConflicts, supportRefusals := prepareHookSupport(work)
	var blockedResources []BlockedResource
	blockResource := func(key outputKey, w skillTarget) {
		blocked.blockResource(key, w)
		name := w.skill.Name
		if w.skill.IsMarketplace && w.skill.InstalledName != "" {
			name = w.skill.InstalledName
		}
		blockedResources = append(blockedResources, BlockedResource{
			Kind: w.skill.Kind, Name: name, Target: key.target, MachineScope: key.machineScope,
		})
	}

	for index, w := range work {
		key := outputKey{target: w.target, machineScope: w.machineScope}
		binding, hasBinding, bindingErr := materializeAdapterFor(w.target)
		if bindingErr != nil {
			blockResource(key, w)
			allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.adapter.error", Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Name, Message: bindingErr.Error()})
			continue
		}
		var aggregateHandler ownedAggregateHandler
		if hasBinding {
			aggregateHandler = binding.handler
		}
		if supportRefusals[index] != nil {
			blockResource(key, w)
			markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
			allDiags = append(allDiags, diag.Diagnostic{Code: diag.CodeMaterializeHookSupportRefused, Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Name, Message: "Hook support source refused safe admission; affected hooks were not emitted", Hint: "Fix or remove the unsafe support source and re-run sync", Source: compilepkg.SourceOf(w.skill.Resource)})
			continue
		}
		if paths := supportConflicts[index]; len(paths) > 0 {
			blockResource(key, w)
			markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
			allDiags = append(allDiags, diag.Diagnostic{Code: diag.CodeMaterializeHookSupportConflict, Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Name, Message: fmt.Sprintf("hook support paths conflict across selected sources: %s; affected hooks were not emitted", strings.Join(paths, ", ")), Hint: "Rename the conflicting support files or exclude one hook provider", Source: compilepkg.SourceOf(w.skill.Resource)})
			continue
		}
		if w.skill.IsMarketplace && w.skill.InstalledName != "" {
			renamed, err := marketplaceResourceForInstallName(w.skill.Resource, w.skill.InstalledName)
			if err != nil {
				blockResource(key, w)
				markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
				allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.marketplace.alias-invalid", Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Resource.Name(), Message: "Marketplace installed name cannot be materialized safely"})
				continue
			}
			w.skill.Resource = renamed
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
			blockResource(key, w)
			markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
			allDiags = append(allDiags, diag.Diagnostic{
				Code:     "materialize.compile.error",
				Severity: diag.SeverityError,
				Target:   string(w.target),
				Resource: w.skill.Name,
				Message:  fmt.Sprintf("compile resource %q for target %s: %v", w.skill.Name, w.target, err),
			})
			continue
		}

		if result == nil {
			blockResource(key, w)
			markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
			continue
		}

		result.Artifacts = append([]artifact.Artifact(nil), result.Artifacts...)
		if diag.HasErrors(result.Diagnostics) {
			blockResource(key, w)
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
				blockResource(key, w)
				markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
				allDiags = append(allDiags, aggregateHandler.malformedDiagnostic(key, w.skill.Name, aggregateErr))
				continue
			}
			result.Artifacts = rest
		}

		// For hooks, expand ${DROID_PLUGIN_ROOT} to an installed root.
		// Factory Droid only sets DROID_PLUGIN_ROOT for plugin-scoped hooks,
		// not workspace-level hooks (.factory/hooks/). Repository hooks run
		// from the repo root, so their paths can be relative. Machine hooks
		// run from any repo and must always use the absolute machine root.
		if resourceKind == ir.KindHook && !harness.IsInstallationAdapterTarget(w.target) {
			if roots, ok := harness.MakeInstallRoots()[w.target]; ok {
				pluginRoot := roots.For(w.machineScope, repoRoot)
				if pluginRoot != "" {
					if !w.machineScope {
						relPath, err := filepath.Rel(repoRoot, pluginRoot)
						if err == nil && !strings.HasPrefix(relPath, "..") {
							pluginRoot = relPath
						}
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
				var declaredSupport []string
				if w.skill.IsMarketplace {
					if s, ok := w.skill.Resource.(*ir.Skill); ok {
						declaredSupport = s.SupportFiles
					}
				}
				if len(declaredSupport) > 0 && sourceDir == "" {
					blockResource(key, w)
					markOwnedAggregateIncomplete(aggregateHandler, ownedAggregates, key, w)
					allDiags = append(allDiags, diag.Diagnostic{
						Code:     "materialize.support-source.missing",
						Severity: diag.SeverityError,
						Target:   string(w.target),
						Resource: w.skill.Name,
						Message:  fmt.Sprintf("marketplace resource %q declares support files but its staged source %q is unavailable; keeping the previous installation", w.skill.Name, w.skill.Path),
					})
					continue
				}
				addSkillLink(skillLinks, key, installDirRel, skillLink{
					name:            w.skill.Name,
					target:          cachedSkillDir(w.target, w.skill, compiledSkillArtifacts),
					artifacts:       compiledSkillArtifacts,
					sourceDir:       sourceDir,
					declaredSupport: declaredSupport,
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
				result.Artifacts = append(result.Artifacts, hookSupport[index]...)
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
		allDiags:         allDiags,
		byTarget:         byTarget,
		skillLinks:       skillLinks,
		allOutputs:       allOutputs,
		blocked:          blocked,
		blockedResources: blockedResources,
		artifactMeta:     artifactMeta,
		ownedState:       ownedState,
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
func executeMaterializePlans(ctx context.Context, compileResult compilePhaseResult, dryRun bool, repoRoot string, modes config.MaterializationModes) *Report {
	report := &Report{
		PerTarget:        make(map[hcc.Target]TargetReport),
		Diags:            compileResult.allDiags,
		BlockedResources: compileResult.blockedResources,
	}
	coalescedGemini, err := coalesceGeminiNativeAggregates(&compileResult, repoRoot)
	if err != nil {
		slog.Error("shared Gemini destination refused", "code", diag.CodeMaterializeDestinationOverlap, "error", err)
		report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeMaterializeDestinationOverlap, Severity: diag.SeverityError, Target: string(hcc.TargetGemini), Message: err.Error()})
		return report
	}
	coalescedCodexPartitions, preflightDiag := preflightCodexDestinations(&compileResult, repoRoot, modes)
	if preflightDiag != nil {
		report.Diags = append(report.Diags, *preflightDiag)
		return report
	}

	composition, adapterErr := newMaterializeAdapterComposition()
	if adapterErr != nil {
		for key := range compileResult.allOutputs {
			if harness.IsInstallationAdapterTarget(key.target) {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.adapter.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("construct installation adapter registry: %v", adapterErr)})
			}
		}
		return report
	}
	adapters := composition.registry

	// Sort output keys for deterministic PerRoot ordering.
	sortedOutputs := make([]outputKey, 0, len(compileResult.allOutputs))
	for key := range compileResult.allOutputs {
		sortedOutputs = append(sortedOutputs, key)
	}
	slices.SortFunc(sortedOutputs, func(a, b outputKey) int {
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

	for _, key := range sortedOutputs {
		var roots harness.Roots
		var ok bool
		var adapterDestinations []harness.Destination
		var home string
		if adapter, migrated := adapters.Lookup(key.target); migrated {
			if key.machineScope {
				var err error
				home, err = os.UserHomeDir()
				if err != nil {
					report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("resolve home directory: %v", err)})
					continue
				}
			}
			var err error
			adapterDestinations, err = adapter.Resolve(key.machineScope, repoRoot, home)
			if err != nil {
				slog.Error("adapter destination resolution failed", "target", key.target, "machine_scope", key.machineScope, "code", "materialize.destination.error", "error", err)
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
				continue
			}
		} else {
			roots, ok = harness.MakeInstallRoots()[key.target]
		}
		if !ok && len(adapterDestinations) == 0 {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.target.unknown",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("no install root configured for target %q", key.target),
			})
			continue
		}
		outDir := repoRoot
		if len(adapterDestinations) > 0 {
			// A single destination owns the whole output root (Devin); multiple
			// destinations are partitions under the scope base (Codex).
			if len(adapterDestinations) == 1 {
				outDir = adapterDestinations[0].Root
			} else if key.machineScope {
				outDir = home
			}
		} else {
			outDir = roots.For(key.machineScope, repoRoot)
		}

		tw := compileResult.byTarget[key]

		var artifacts []artifact.Artifact
		var targetDiags []diag.Diagnostic
		if tw != nil {
			merged, mergeErr := artifact.MergeFragments(tw.artifacts)
			if mergeErr != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     "materialize.merge.error",
					Severity: diag.SeverityError,
					Target:   string(key.target),
					Message:  fmt.Sprintf("merge fragments for target %s: %v", key.target, mergeErr),
				})
				continue
			}
			artifacts = merged
			targetDiags = tw.diags
			// Aggregate compile diagnostics once at the report level so
			// InSync() reflects compile errors. Per-plan TargetReport.Diags
			// are left empty to avoid duplication across multiple plans.
			report.Diags = append(report.Diags, targetDiags...)
		}

		boundaryHome := home
		if boundaryHome == "" {
			var homeErr error
			boundaryHome, homeErr = os.UserHomeDir()
			if homeErr != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: homeErr.Error()})
				continue
			}
		}
		destinations := adapterDestinations
		if len(destinations) == 0 {
			var destinationErr error
			destinations, destinationErr = harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
			if destinationErr != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: destinationErr.Error()})
				continue
			}
		}
		boundary := destinationBoundary{machineScope: key.machineScope, repoRoot: repoRoot, homeDir: boundaryHome, linkPublicationHook: compileResult.linkPublicationHook, pruneClassificationHook: compileResult.pruneClassificationHook, manifestPublicationHook: compileResult.manifestPublicationHook}
		if key.target == hcc.TargetFactory {
			boundary.legacySkillsDir = filepath.Join(outDir, "skills")
		}
		var directories []string
		for _, destination := range destinations {
			root := destination.Root
			if key.target == hcc.TargetWindsurf && !key.machineScope {
				root = filepath.Join(repoRoot, windsurfDir)
			}
			boundary.roots = append(boundary.roots, root)
			if key.target == hcc.TargetCodex && (!key.machineScope || destination.Prefix == ".agents" || os.Getenv("CODEX_HOME") == "") {
				boundary.roots = append(boundary.roots, filepath.Join(outDir, destination.Prefix))
			}
			if len(destination.AggregateRoutes) > 0 {
				directories = append(directories, filepath.Dir(filepath.Join(destination.Root, destination.AggregateFile)))
			}
		}
		for _, plan := range materializePlans(key.target, outDir, artifacts, linkedComponents(compileResult.skillLinks[key]), key.machineScope) {
			directories = append(directories, plan.outDir)
		}
		for _, group := range orderedSkillLinkGroups(compileResult.skillLinks[key]) {
			directories = append(directories, filepath.Join(outDir, filepath.FromSlash(group.installDirRel)))
		}
		if err := boundary.validate(directories); err != nil {
			slog.Error("materialize destination refused", "target", key.target, "machine_scope", key.machineScope, "error", err)
			report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
			continue
		}
		if !dryRun {
			if err := boundary.directory(outDir, true); err != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.mkdir.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
				continue
			}
		}

		mode := modes.For(string(key.target), key.machineScope)
		// Resource installation mode is distribution policy, not a compiler
		// capability. All targets use the same file-copy or symlink path;
		// shared aggregates retain their separate ownership reconciliation.
		slog.Debug("materialize target", "target", key.target, "machine_scope", key.machineScope, "mode", mode, "out_dir", outDir)
		if !coalescedGemini || key.target != hcc.TargetGemini || key.machineScope {
			artifacts = materializeNativeAggregates(ctx, report, key, repoRoot, artifacts, compileResult.blocked.componentsFor(key), dryRun)
		}
		if tw != nil && tw.nativeOnly {
			slog.Debug("native-only aggregate work complete", "target", key.target, "machine_scope", key.machineScope)
			continue
		}
		report = executeTargetPlans(report, key, outDir, artifacts, compileResult.skillLinks, compileResult.artifactMeta, compileResult.blocked.componentsFor(key), dryRun, mode, repoRoot, coalescedCodexPartitions, boundary)
		if binding, ok := composition.bindings[key.target]; ok {
			report = binding.handler.reconcile(ctx, report, compileResult.ownedAggregateState(), key, outDir, adapterDestinations, dryRun)
		}
	}

	return report
}

// executeTargetPlans writes artifact plans and skill links for a single target.
func executeTargetPlans(report *Report, key outputKey, outDir string, artifacts []artifact.Artifact, skillLinks map[outputKey]map[string]*skillLinkGroup, artifactMeta map[outputKey]map[string]skillLink, blocked map[string]bool, dryRun bool, mode config.MaterializationMode, repoRoot string, coalescedCodexPartitions map[string]bool, boundary destinationBoundary) *Report {
	state, stateErr := loadPortableState(repoRoot, key.machineScope)
	if stateErr != nil {
		slog.Error("materialize manifest load failed", "target", key.target, "machine_scope", key.machineScope, "error", stateErr)
		report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: stateErr.Error()})
		return report
	}
	state.publicationHook = boundary.manifestPublicationHook
	fileDesired := make(map[string]bool)
	// Track which plan output directories had merge artifacts, so we can
	// prune managed merge entries from directories NOT covered by any plan.
	dirsCoveredByMergePlans := make(map[string]bool)
	// File mode must also prune stale cache-backed symlinks from every regular
	// component directory, including components that have no desired artifacts.
	portableCleanupDirs := make(map[string]string)

	plans := materializePlans(key.target, outDir, artifacts, linkedComponents(skillLinks[key]), key.machineScope)
	// Prune suppression is per COMPONENT: only the components a skipped
	// resource would have written into may keep their stale files.
	for i := range plans {
		if componentBlocked(blocked, plans[i].reportPrefix) {
			plans[i].mode = hcc.WriteModeOverwrite
			plans[i].prune = false
			dirsCoveredByMergePlans[plans[i].outDir] = true
		}
	}
	for _, plan := range plans {
		regularArtifacts, mergeArtifacts := splitMergeArtifacts(plan.artifacts)
		if err := boundary.directory(plan.outDir, !dryRun); err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
			continue
		}
		if mode == config.MaterializationModeFile {
			planRoot := outDir
			if plan.root != "" {
				planRoot = plan.root
			}
			rel, err := filepath.Rel(planRoot, plan.outDir)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.mkdir.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("unsafe portable output directory %s", plan.outDir)})
				continue
			}
			portableCleanupDirs[plan.outDir] = plan.reportPrefix
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
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("prepare artifact cache for target %s: %v", key.target, matErr),
			})
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
			writeReport, writeDiags, matErr = syncPortableArtifactFiles(plan.outDir, regularArtifacts, cacheDir, dryRun, wantPrune, planCollisionMeta, state, key.target, plan.reportPrefix, fileDesired, &boundary)
		} else {
			writeReport, writeDiags, matErr = syncArtifactLinks(plan.outDir, regularArtifacts, cacheDir, dryRun, wantPrune, planCollisionMeta, portableSyncContext{state: state, target: key.target, reportPrefix: plan.reportPrefix, desired: fileDesired, boundary: &boundary})
		}
		if matErr != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("materialize target %s to %s: %v", key.target, plan.outDir, matErr),
			})
			continue
		}
		if len(writeDiags) > 0 {
			for i := range writeDiags {
				writeDiags[i].Target = string(key.target)
			}
			report.Diags = append(report.Diags, writeDiags...)
		}
		if componentBlocked(blocked, plan.reportPrefix) && componentName(plan.reportPrefix) == componentName(resourceInstallDirRel(key.target, ir.KindHook)) {
			retained, err := retainBlockedHookArtifacts(plan.outDir, mergeArtifacts)
			if err != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("retain blocked hook entries in %s: %v", plan.outDir, err)})
				continue
			}
			mergeArtifacts = retained
		}
		mergeReport, mergeDiags, mergeErr := materializeMergeArtifacts(plan.outDir, mergeArtifacts, dryRun, wantPrune, &boundary)
		if len(mergeArtifacts) > 0 {
			dirsCoveredByMergePlans[plan.outDir] = true
		}
		// Per-file prune refusals are reported even if the merge itself also
		// failed: each names a different file.
		report.Diags = append(report.Diags, retargetPruneDiags(mergeDiags, key.target, plan.reportPrefix)...)
		if mergeErr != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("merge target %s config in %s: %v", key.target, plan.outDir, mergeErr),
			})
			continue
		}
		writeReport.Written = append(writeReport.Written, mergeReport.Written...)
		writeReport.Skipped = append(writeReport.Skipped, mergeReport.Skipped...)
		writeReport.Removed = append(writeReport.Removed, mergeReport.Removed...)

		tr := TargetReport{
			Target:       key.target,
			MachineScope: key.machineScope,
			RootID:       plan.rootID,
			OutDir:       writeReport.OutDir,
			Written:      writeReport.Written,
			Skipped:      writeReport.Skipped,
			Removed:      writeReport.Removed,
		}
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
				if outputDirExists(dir) {
					if err := boundary.directory(dir, false); err != nil {
						report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("validate portable resource cleanup directory %s: %v", dir, err)})
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
		pruneReport, pruneDiags = pruneUncoveredManagedMergeFiles(pruneOutDir, dirsCoveredByMergePlans, dryRun, nativeAggregatePruneFence(key.target, key.machineScope))
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
	report.Diags = append(report.Diags, retargetPruneDiags(pruneDiags, key.target, pruneReportPrefix)...)
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
		if err := boundary.directory(groupDir, !dryRun); err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
			continue
		}
		if mode == config.MaterializationModeFile {
			portableCleanupDirs[groupDir] = group.reportPrefix
		}
		symlinkLinks, copyLinks := partitionFileCopyLinks(key.target, group.links)

		if len(symlinkLinks) > 0 {
			for _, link := range symlinkLinks {
				fileDesired[filepath.ToSlash(filepath.Join(group.reportPrefix, link.name))] = true
			}
			if err := prepareSkillLinkCaches(symlinkLinks, dryRun); err != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     "materialize.write.error",
					Severity: diag.SeverityError,
					Target:   string(key.target),
					Message:  fmt.Sprintf("prepare skill cache for target %s: %v", key.target, err),
				})
				continue
			}
			var tr TargetReport
			var err error
			if mode == config.MaterializationModeFile {
				tr, err = syncPortableSkillTrees(groupDir, symlinkLinks, dryRun, state, key.target, group.reportPrefix, fileDesired, &boundary)
			} else {
				tr, err = syncSkillLinks(groupDir, symlinkLinks, dryRun, !componentBlocked(blocked, group.installDirRel), portableSyncContext{state: state, target: key.target, reportPrefix: group.reportPrefix, desired: fileDesired, boundary: &boundary})
			}
			report = appendGroupReport(report, key, outDir, group.reportPrefix, tr, err, "materialize target %s to %s")
			if err != nil {
				continue
			}
		}

		if len(copyLinks) > 0 {
			copyDirsCovered[groupDir] = true
			tr, err := syncCommandFiles(groupDir, copyLinks, dryRun, !componentBlocked(blocked, group.installDirRel), &boundary)
			report = appendGroupReport(report, key, outDir, group.reportPrefix, tr, err, "copy command files for target %s to %s")
		}
	}

	// Prune managed file-copy resources (e.g. Factory commands) from their
	// install dirs when no group covered them this run — this handles the
	// removal case where the resolved set no longer contains the resource.
	// Regular files are never pruned by the symlink-oriented artifact plan,
	// so they must be cleaned up here.
	report = pruneUncoveredFileCopyDirs(report, key, outDir, copyDirsCovered, blocked, dryRun, &boundary)

	// The two manifest-driven prune passes below walk the target's whole
	// recorded state rather than one component directory, so they cannot be
	// narrowed per component and stay suppressed by any skip.
	if mode == config.MaterializationModeFile && !anyBlocked(blocked) {
		report = prunePortableStaleSymlinks(report, key, outDir, portableCleanupDirs, fileDesired, dryRun, &boundary)
	}

	var portableReports []TargetReport
	var portableDiags []diag.Diagnostic
	var portableErr error
	if !anyBlocked(blocked) {
		portableReports, portableDiags, portableErr = prunePortableEntries(state, key, outDir, repoRoot, fileDesired, dryRun, &boundary)
	}
	if portableErr != nil {
		slog.Error("materialize manifest prune failed", "target", key.target, "machine_scope", key.machineScope, "error", portableErr)
		report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: portableErr.Error()})
	} else {
		for i := range portableDiags {
			portableDiags[i].Target = string(key.target)
		}
		report.Diags = append(report.Diags, portableDiags...)
		for _, tr := range portableReports {
			report.PerRoot = append(report.PerRoot, tr)
			report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], tr)
		}
	}
	if err := state.save(dryRun); err != nil {
		slog.Error("materialize manifest save failed", "target", key.target, "machine_scope", key.machineScope, "error", err)
		report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
	} else {
		slog.Debug("materialize manifest synchronized", "target", key.target, "machine_scope", key.machineScope, "entries", len(state.entries), "path", state.path)
	}

	return report
}

// pruneUncoveredFileCopyDirs removes orphaned managed file-copy resources from
// the target's file-copy install dirs that were not handled by any copy group.
func pruneUncoveredFileCopyDirs(report *Report, key outputKey, outDir string, covered map[string]bool, blocked map[string]bool, dryRun bool, boundaries ...*destinationBoundary) *Report {
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
		if covered[dir] || seen[dir] || !outputDirExists(dir) {
			continue
		}
		seen[dir] = true
		tr := TargetReport{OutDir: dir}
		if err := pruneStaleCommandFiles(dir, nil, dryRun, &tr, boundaries...); err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("prune managed command files in %s: %v", dir, err),
			})
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
		tr.RootID = codexRootIDForPrefix(key, reportPrefix)
	}
	if err != nil {
		report.Diags = append(report.Diags, diag.Diagnostic{
			Code:     "materialize.write.error",
			Severity: diag.SeverityError,
			Target:   string(key.target),
			Message:  fmt.Sprintf(errFormat+": %v", key.target, tr.OutDir, err),
		})
		return report
	}
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
	if target == hcc.Target(targetmeta.TargetCodex) {
		return codexMaterializePlans(outDir, artifacts, linkedSkills, machineScope)
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

func verifyDeclaredSupportFiles(sourceDir string, declared []string) error {
	for _, rel := range declared {
		path := filepath.Join(sourceDir, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("declared support file %q is unavailable under %s: %w", rel, sourceDir, err)
		}
		if info.IsDir() {
			return fmt.Errorf("declared support file %q under %s is a directory", rel, sourceDir)
		}
	}
	return nil
}

func materializationSourcePath(resolved resolver.ResolvedArtifact) string {
	if resolved.IsMarketplace {
		if resolved.Path == "" {
			return ""
		}
		info, err := os.Stat(resolved.Path)
		if err != nil {
			return ""
		}
		if info.IsDir() {
			return resolved.Path
		}
		if filepath.Base(resolved.Path) == "artifact.tar.gz" {
			return ""
		}
		return filepath.Dir(resolved.Path)
	}
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

func materializeMergeArtifacts(outDir string, artifacts []artifact.Artifact, dryRun bool, pruneStaleEntries bool, boundary ...*destinationBoundary) (*hcc.WriteReport, []diag.Diagnostic, error) {
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
	for _, a := range artifacts {
		if err := a.Validate(); err != nil {
			return nil, nil, fmt.Errorf("invalid merge artifact path %q: %w", a.Path, err)
		}
		destPath := filepath.Join(outDir, filepath.FromSlash(a.Path))
		if info, err := os.Lstat(destPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			var capturedBoundary *destinationBoundary
			if len(boundary) > 0 {
				capturedBoundary = boundary[0]
			}
			existing, mode, readErr := markerAliasSnapshot(outDir, destPath, capturedBoundary) // follows symlink
			// get target file info for permissions
			if readErr != nil {
				return nil, nil, fmt.Errorf("read symlink target %s: %w", destPath, readErr)
			}
			if dryRun {
				continue
			}
			if len(existing) > 0 {
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
			rel, relErr := filepath.Rel(outDir, path)
			if relErr == nil && protected[filepath.ToSlash(rel)+"/"] {
				return filepath.SkipDir
			}
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

func ensureSymlinkPathWithOwnership(linkPath, target string, dryRun bool, manifestOwned bool, boundary ...*destinationBoundary) ensureSymlinkResult {
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

	var capturedBoundary *destinationBoundary
	if len(boundary) > 0 {
		capturedBoundary = boundary[0]
	}
	return ensureSymlinkResult{err: publishCacheLink(linkPath, target, info, capturedBoundary)}
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
func syncPortableArtifactFiles(outDir string, artifacts []artifact.Artifact, cacheDir string, dryRun bool, _ bool, collisionMeta map[string]skillLink, state *portableState, target hcc.Target, reportPrefix string, desired map[string]bool, boundaries ...*destinationBoundary) (*hcc.WriteReport, []diag.Diagnostic, error) {
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
			var boundary *destinationBoundary
			if len(boundaries) > 0 {
				boundary = boundaries[0]
			}
			if err := publishPortableFile(src, dst, info, boundary); err != nil {
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
	if portableLinkIsSingleArtifact(link) {
		return portableArtifactDigest(link.artifacts[0]), nil
	}
	return portableSkillVirtualDigest(link)
}

// portableLinkIsSingleArtifact reports a link to one compiled cache file, whose
// desired content comes from the artifact alone rather than a source tree.
func portableLinkIsSingleArtifact(link skillLink) bool {
	return link.cacheDir != "" && len(link.artifacts) == 1 && link.target == filepath.Join(link.cacheDir, filepath.FromSlash(link.artifacts[0].Path))
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
	err := walkPortableSkillSupport(link, func(childRel string) {
		ensureParents(childRel)
		// copyDir does not chmod an already-created directory, so an
		// artifact-created parent keeps its existing 0755 mode.
		if _, exists := nodes[childRel]; !exists {
			nodes[childRel] = portableVirtualNode{dir: true, mode: 0o755}
		}
	}, func(root *fsboundary.Dir, sourceChild, childRel string) error {
		data, identity, err := readSupportSourceSnapshot(root, sourceChild)
		if err != nil {
			return err
		}
		ensureParents(childRel)
		nodes[childRel] = portableVirtualNode{mode: identity.Mode.Perm(), content: data}
		return nil
	})
	if err != nil {
		return "", err
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

// walkPortableSkillSupport visits the support tree prepareSkillLinkCache would
// copy from link.sourceDir: contained directory aliases are expanded through
// the captured source root, escaping/dangling/cyclic aliases refuse, and VCS
// internals and the compiled primary are skipped. visitFile receives the
// captured root and the root-relative source path of each support file.
func walkPortableSkillSupport(link skillLink, visitDir func(childRel string), visitFile func(root *fsboundary.Dir, sourceChild, childRel string) error) error {
	if link.sourceDir == "" {
		return nil
	}
	if len(link.declaredSupport) > 0 {
		if err := verifyDeclaredSupportFiles(link.sourceDir, link.declaredSupport); err != nil {
			return err
		}
	}
	active := make(map[string]bool)
	var addSupport func(*fsboundary.Dir, string, string) error
	addSupport = func(root *fsboundary.Dir, sourceRel, rel string) error {
		if active[sourceRel] {
			return fmt.Errorf("%w: cyclic support directory alias", fsboundary.ErrUnsafePath)
		}
		active[sourceRel] = true
		defer delete(active, sourceRel)
		entries, err := readSupportSourceDirectory(root, sourceRel)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childRel := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			// Mirror copySupportFilesExcluding: VCS internals are never part
			// of the support tree, exactly like the real cache copy.
			if childRel == "SKILL.md" || entry.Name() == ".git" {
				continue
			}
			sourceChild, isDir, err := supportSourceChild(root, sourceRel, entry)
			if errors.Is(err, fs.SkipDir) {
				continue
			}
			if err != nil {
				return err
			}
			if isDir { // copyFile/copyDir follows source links.
				visitDir(childRel)
				if err := addSupport(root, sourceChild, childRel); err != nil {
					return err
				}
				continue
			}
			if err := visitFile(root, sourceChild, childRel); err != nil {
				return err
			}
		}
		return nil
	}
	absolute, err := filepath.Abs(link.sourceDir)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(absolute)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	err = addSupport(root, ".", ".")
	if closeErr := root.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

// validatePortableSkillSupportStructure applies the support-tree authority
// rules of the dry-run digest without reading payload bytes: aliases must stay
// contained and every support payload must be a single-link regular file. A
// read-only check uses it for symlinks whose desired content is not needed.
func validatePortableSkillSupportStructure(link skillLink) error {
	return walkPortableSkillSupport(link, func(string) {}, inspectSupportSourceLeaf)
}

func syncPortableSkillTrees(skillsDir string, links []skillLink, dryRun bool, state *portableState, target hcc.Target, reportPrefix string, desired map[string]bool, boundaries ...*destinationBoundary) (TargetReport, error) {
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
			sourceInfo, err := os.Stat(link.target)
			if err != nil {
				return report, err
			}
			if sourceInfo.IsDir() {
				var boundary *destinationBoundary
				if len(boundaries) > 0 {
					boundary = boundaries[0]
				}
				ownedDigest := ""
				if owned && info != nil && info.Mode()&os.ModeSymlink == 0 {
					ownedDigest = entry.Digest
				}
				if err := publishPortableTree(link.target, dst, info, ownedDigest, boundary); err != nil {
					return report, err
				}
			} else {
				var boundary *destinationBoundary
				if len(boundaries) > 0 {
					boundary = boundaries[0]
				}
				if err := publishPortableFile(link.target, dst, info, boundary); err != nil {
					return report, err
				}
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

func prunePortableEntries(state *portableState, key outputKey, outDir, repoRoot string, desired map[string]bool, dryRun bool, boundaries ...*destinationBoundary) ([]TargetReport, []diag.Diagnostic, error) {
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
		if info, ok := harness.TargetInfoFor(key.target); ok && info.PreservesLegacyPath(entry.Path, key.machineScope) {
			continue
		}
		rootID, root, rel, err := portableEntryDestination(key, outDir, repoRoot, entry.Path)
		if err != nil {
			return nil, diags, err
		}
		path, err := portableOutputPath(root, rel)
		if err != nil {
			return nil, diags, err
		}
		observed, statErr := os.Lstat(path)
		err = statErr
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
			var boundary *destinationBoundary
			if len(boundaries) > 0 {
				boundary = boundaries[0]
			}
			if err := removePortableOutput(path, observed, entry.Digest, boundary); err != nil {
				return nil, diags, err
			}
			cleanEmptyPortableArtifactDirs(root, filepath.Dir(filepath.FromSlash(rel)), boundary)
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
func prunePortableStaleSymlinks(report *Report, key outputKey, outDir string, cleanupDirs map[string]string, desired map[string]bool, dryRun bool, boundaries ...*destinationBoundary) *Report {
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
		snapshots := make(map[string]managedLinkSnapshot)
		stale, diags, err := staleArtifactLinks(dir, desiredInDir, snapshots)
		if err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("prune stale portable symlinks in %s: %v", dir, err)})
			continue
		}
		tr := TargetReport{OutDir: dir, Diags: diags}
		for _, rel := range stale {
			path, pathErr := portableOutputPath(dir, rel)
			if pathErr != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: pathErr.Error()})
				continue
			}
			var boundary *destinationBoundary
			if len(boundaries) > 0 {
				boundary = boundaries[0]
			}
			if err := pruneManagedLink(path, snapshots[rel], boundary, dryRun); err != nil {
				if !missingPruneEntry(err) {
					report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("remove stale portable symlink %s: %v", path, err)})
				}
				continue
			}
			tr.Removed = append(tr.Removed, rel)
			if !dryRun {
				cleanEmptyPortableArtifactDirs(dir, filepath.Dir(filepath.FromSlash(rel)), boundary)
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
		var boundary *destinationBoundary
		if len(portable) > 0 {
			boundary = portable[0].boundary
		}
		result := ensureSymlinkPathWithOwnership(linkPath, cacheTarget, dryRun, migrationAllowed, boundary)
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
		snapshots := make(map[string]managedLinkSnapshot)
		stale, staleDiags, err := staleArtifactLinks(outDir, desired, snapshots)
		if err != nil {
			return report, diags, err
		}
		diags = append(diags, staleDiags...)
		for _, path := range stale {
			leaf := filepath.Join(outDir, filepath.FromSlash(path))
			var boundary *destinationBoundary
			if len(portable) > 0 {
				boundary = portable[0].boundary
			}
			if err := pruneManagedLink(leaf, snapshots[path], boundary, dryRun); err != nil {
				if missingPruneEntry(err) {
					continue
				}
				return report, diags, err
			}
			report.Removed = append(report.Removed, path)
			if !dryRun {
				cleanEmptyPortableArtifactDirs(outDir, filepath.Dir(filepath.FromSlash(path)), boundary)
			}
		}
	}

	sort.Strings(report.Written)
	sort.Strings(report.Skipped)
	sort.Strings(report.Removed)
	return report, diags, nil
}

func staleArtifactLinks(outDir string, desired map[string]string, snapshots ...map[string]managedLinkSnapshot) ([]string, []diag.Diagnostic, error) {
	var stale []string
	var diags []diag.Diagnostic
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if missingPruneEntry(err) {
			return nil
		}
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
		snapshot, err := observeManagedLink(path)
		if missingPruneEntry(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if snapshot.observed.Mode()&os.ModeSymlink == 0 {
			return fsboundary.ErrGenerationConflict
		}
		// Only mark managed symlinks as stale. User symlinks
		// (pointing outside the cache dir) are preserved.
		if !strings.HasPrefix(snapshot.text, cache.CacheDir()+string(os.PathSeparator)) {
			diags = append(diags, diag.Diagnostic{
				Code:     "materialize.collision.user-owned",
				Severity: diag.SeverityInfo,
				Message:  fmt.Sprintf("preserved user-owned symlink %q (not managed by agent-marketplace)", relSlash),
			})
			return nil
		}
		stale = append(stale, relSlash)
		if len(snapshots) > 0 {
			snapshots[0][relSlash] = snapshot
		}
		return nil
	})
	if err != nil {
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
func cleanEmptyPortableArtifactDirs(outDir, relPath string, boundaries ...*destinationBoundary) {
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
		var boundary *destinationBoundary
		if len(boundaries) > 0 {
			boundary = boundaries[0]
		}
		if err := removeEmptyPortableDirectory(outDir, dirPath, boundary); err != nil {
			return
		}
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
	compiled := make(map[string]bool, len(link.artifacts))
	for _, a := range link.artifacts {
		compiled[filepath.ToSlash(filepath.Clean(a.Path))] = true
	}
	supportStage := ""
	if link.sourceDir != "" {
		if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
			return err
		}
		var err error
		supportStage, err = os.MkdirTemp(filepath.Dir(cacheDir), ".support-staging-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(supportStage) }()
		if err := copyScopedSupport(link.sourceDir, supportStage, compiled, "", len(link.declaredSupport) == 0, writeSupportStage); err != nil {
			return err
		}
		if err := verifyDeclaredSupportFiles(supportStage, link.declaredSupport); err != nil {
			return err
		}
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
	if supportStage != "" {
		if err := pruneSkillCacheSupportFiles(cacheDir, compiled); err != nil {
			return err
		}
		if err := copySupportFilesExcluding(supportStage, cacheDir, compiled, "", false); err != nil {
			return err
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
	return copySupportFilesExcluding(srcDir, dstDir, nil, "", true)
}

func copySupportFilesExcluding(srcDir, dstDir string, compiled map[string]bool, rel string, missingOK bool) error {
	return copyScopedSupport(srcDir, dstDir, compiled, rel, missingOK, writeSupportSnapshot)
}

// collectHookSupportFiles scans the directory containing the source hooks.json
// for non-hooks.json files and returns them as regular (non-fragment) artifacts.
// These artifacts are symlinked into the hooks output directory alongside the
// merged hooks.json, and are automatically pruned by staleArtifactLinks when
// a hook is excluded (only symlinks are pruned, never the regular hooks.json).
func collectHookSupportFiles(sourcePath string, target hcc.Target, sourceHarness string) ([]artifact.Artifact, error) {
	if sourcePath == "" {
		return nil, nil
	}
	hooksDirRel := resourceInstallDirRel(target, ir.KindHook)
	if hooksDirRel == "" {
		return nil, nil
	}
	sourceDir, err := filepath.Abs(filepath.Dir(sourcePath))
	if err != nil {
		return nil, err
	}
	root, err := fsboundary.OpenDir(sourceDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	entries, err := readSupportSourceDirectory(root, ".")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	excluded := hookSupportExclusions(sourcePath, sourceHarness)
	var artifacts []artifact.Artifact
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "hooks.json" || excluded[name] || (sourceHarness == string(hcc.TargetWindsurf) && filepath.Base(sourceDir) == "hooks" && strings.HasSuffix(name, ".json")) {
			continue
		}
		data, identity, err := readSupportSourceSnapshot(root, name)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact.Artifact{Path: hooksDirRel + "/" + name, Bytes: data, Mode: identity.Mode})
	}
	return artifacts, nil
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

func writePortableDigestPath(h io.Writer, path, rel string) (err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return writePortableDigestEntry(h, root, filepath.Base(path), rel)
}

func writePortableRegularDigest(h io.Writer, path, rel string, info os.FileInfo) (err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return writePortableRegularDigestAt(h, root, filepath.Base(path), rel, info)
}

func writePortableRegularDigestAt(h io.Writer, root *fsboundary.Dir, name, rel string, info os.FileInfo) (err error) {
	before, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return err
	}
	file, err := fsboundary.CaptureDestination(root, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil || !file.Identity().SameState(before) {
		return fmt.Errorf("%w: portable output changed before digest read", fsboundary.ErrGenerationConflict)
	}
	return file.ReadTargetWith(func(reader io.Reader) error {
		if _, err := fmt.Fprintf(h, "file\x00%s\x00%#o\x00%d\x00", filepath.ToSlash(rel), before.Mode.Perm(), before.Size); err != nil {
			return err
		}
		_, err := io.Copy(h, reader)
		return err
	})
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

// skillPruneObservedHook runs after a skill-directory prune entry is
// enumerated. It is nil in production and set only by serial tests in this
// package.
var skillPruneObservedHook func(path string)

func syncSkillLinks(skillsDir string, links []skillLink, dryRun bool, pruneStaleEntries bool, portable ...portableSyncContext) (TargetReport, error) {
	var state *portableState
	var target hcc.Target
	var reportPrefix string
	var portableDesired map[string]bool
	if len(portable) > 0 {
		state, target, reportPrefix, portableDesired = portable[0].state, portable[0].target, portable[0].reportPrefix, portable[0].desired
	}
	report := TargetReport{OutDir: skillsDir}
	desired := make(map[string]string, len(links))
	linkMeta := make(map[string]skillLink, len(links))
	orderedLinks := append([]skillLink(nil), links...)
	slices.SortFunc(orderedLinks, func(a, b skillLink) int {
		if a.name != b.name {
			return strings.Compare(a.name, b.name)
		}
		return strings.Compare(a.target, b.target)
	})
	for _, link := range orderedLinks {
		desired[link.name] = link.target
		linkMeta[link.name] = link
		// A real sync revalidates the source support tree while preparing the
		// link cache. A read-only check skips that and hashes desired content
		// only for legacy migration, so apply the same support-tree authority
		// structurally, without reading payload bytes, to every linked source.
		if state != nil && dryRun && !portableLinkIsSingleArtifact(link) {
			if err := validatePortableSkillSupportStructure(link); err != nil {
				return report, err
			}
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
				ownedDigest := entry.Digest
				if ownedDigest == "" {
					ownedDigest, digestErr = portableLinkedDesiredDigest(linkMeta[name], dryRun)
					if digestErr != nil {
						return report, digestErr
					}
				}
				migrationAllowed = currentDigest == ownedDigest
			} else {
				migrationAllowed = true
			}
		}
		var boundary *destinationBoundary
		if len(portable) > 0 {
			boundary = portable[0].boundary
		}
		result := ensureSymlinkPathWithOwnership(linkPath, targetPath, dryRun, migrationAllowed, boundary)
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
		snapshot, err := observeManagedLink(path)
		if missingPruneEntry(err) {
			continue
		}
		if err != nil {
			return report, err
		}
		if skillPruneObservedHook != nil {
			skillPruneObservedHook(path)
		}
		if snapshot.observed.Mode()&os.ModeSymlink == 0 {
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
			//
			// Classification and removal stay bound to the enumerated
			// generation: a replacement after enumeration fails removal's
			// identity check instead of becoming the observed authority.
			observed := snapshot.observed
			managed := observed.Mode().IsRegular() && isManagedCommandFile(path)
			if !managed {
				if _, err := os.Lstat(path); os.IsNotExist(err) {
					continue
				}
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     "materialize.collision.user-owned",
					Severity: diag.SeverityInfo,
					Message:  fmt.Sprintf("preserved user-owned file %q (not managed by agent-marketplace)", name),
				})
				continue
			}
			var boundary *destinationBoundary
			if len(portable) > 0 {
				boundary = portable[0].boundary
			}
			if boundary != nil && boundary.pruneClassificationHook != nil {
				boundary.pruneClassificationHook(path)
			}
			if dryRun {
				_, err = readNativeCommandFile(path, observed, boundary)
			} else {
				err = removeNativeCommandFile(path, observed, boundary)
			}
			if err != nil {
				if missingNativeCommandPruneEntry(err) {
					continue
				}
				return report, err
			}
			report.Removed = append(report.Removed, name)
			continue
		}
		// Symlink — only prune if it points to our cache (managed).
		// User symlinks (pointing outside the cache) are preserved.
		if !strings.HasPrefix(snapshot.text, cache.CacheDir()+string(os.PathSeparator)) {
			if pruneStaleEntries {
				report.Diags = append(report.Diags, diag.Diagnostic{
					Code:     "materialize.collision.user-owned",
					Severity: diag.SeverityInfo,
					Message:  fmt.Sprintf("preserved user-owned symlink %q (not managed by agent-marketplace)", name),
				})
			}
			continue
		}
		var boundary *destinationBoundary
		if len(portable) > 0 {
			boundary = portable[0].boundary
		}
		if err := pruneManagedLink(path, snapshot, boundary, dryRun); err != nil {
			if missingPruneEntry(err) {
				continue
			}
			return report, err
		}
		report.Removed = append(report.Removed, name)
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
func syncCommandFiles(dir string, links []skillLink, dryRun, pruneStaleEntries bool, boundaries ...*destinationBoundary) (TargetReport, error) {
	var boundary *destinationBoundary
	if len(boundaries) > 0 {
		boundary = boundaries[0]
	}
	report := TargetReport{OutDir: dir}
	desired := make(map[string][]byte, len(links))
	linkMeta := make(map[string]skillLink, len(links))
	for _, link := range links {
		if len(link.artifacts) == 0 {
			continue
		}
		desired[link.name] = injectManagedByFrontmatter(link.artifacts[0].Bytes)
		linkMeta[link.name] = link
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
			existing, readErr := readNativeCommandFile(filePath, info, boundary)
			if readErr != nil {
				return report, readErr
			}
			if bytes.Equal(existing, content) {
				report.Skipped = append(report.Skipped, name)
				continue
			}
		}

		if dryRun {
			report.Written = append(report.Written, name)
			continue
		}
		mode := os.FileMode(0o644)
		if info != nil && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
		if err := publishPortableBytes(filePath, content, mode, info, boundary); err != nil {
			return report, err
		}
		report.Written = append(report.Written, name)
	}

	if pruneStaleEntries {
		if err := pruneStaleCommandFiles(dir, desired, dryRun, &report, boundary); err != nil {
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
func pruneStaleCommandFiles(dir string, desired map[string][]byte, dryRun bool, report *TargetReport, boundaries ...*destinationBoundary) error {
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
		snapshot, observeErr := observeManagedLink(path)
		if missingPruneEntry(observeErr) {
			continue
		}
		if observeErr != nil {
			return observeErr
		}
		info := snapshot.observed
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if !strings.HasPrefix(snapshot.text, cache.CacheDir()+string(os.PathSeparator)) {
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
		var boundary *destinationBoundary
		if len(boundaries) > 0 {
			boundary = boundaries[0]
		}
		var cleanupErr error
		if info.Mode()&os.ModeSymlink != 0 {
			cleanupErr = pruneManagedLink(path, snapshot, boundary, dryRun)
		} else {
			if boundary != nil && boundary.pruneClassificationHook != nil {
				boundary.pruneClassificationHook(path)
			}
			if dryRun && info.Mode().IsRegular() {
				_, cleanupErr = readNativeCommandFile(path, info, boundary)
			} else if !dryRun {
				cleanupErr = removeNativeCommandFile(path, info, boundary)
			}
		}
		if cleanupErr != nil {
			if missingNativeCommandPruneEntry(cleanupErr) {
				continue
			}
			return cleanupErr
		}
		report.Removed = append(report.Removed, name)
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
	if isManagedCommandContent(content) {
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
	data, err := readNativeCommandFile(path, nil, nil)
	return err == nil && isManagedCommandContent(data)
}

func isManagedCommandContent(data []byte) bool {
	if !bytes.Contains(data, []byte(managedByField)) || !hasFrontmatter(data) {
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
// or removed files, and no error diagnostics). Useful for --check mode.
func (r *Report) InSync() bool {
	if r.TotalWritten() > 0 || r.TotalRemoved() > 0 {
		return false
	}
	for _, d := range r.Diags {
		if d.IsError() {
			return false
		}
	}
	return true
}
