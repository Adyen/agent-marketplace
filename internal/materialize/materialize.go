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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
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
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/packagegen"
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

type nativeHookSupportPlanKey struct {
	outDir       string
	reportPrefix string
}

type nativeHookSupportPlanPublication struct {
	supportTargets map[string]string
	manifestPaths  map[string]bool
	writeReport    *hcc.WriteReport
	diagnostics    []diag.Diagnostic
}

type nativeHookSupportPublication struct {
	plans          map[nativeHookSupportPlanKey]*nativeHookSupportPlanPublication
	manifestStates map[outputKey]*portableState
}

type nativeRepositorySupportWriteFaultKey struct{}
type nativeRepositorySupportCommitFaultKey struct{}

func nativeHookSupportPlanKeyFor(plan materializePlan) nativeHookSupportPlanKey {
	return nativeHookSupportPlanKey{outDir: filepath.Clean(plan.outDir), reportPrefix: filepath.ToSlash(plan.reportPrefix)}
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
	PerTarget          map[hcc.Target]TargetReport
	PerRoot            []TargetReport
	Diags              []diag.Diagnostic
	BlockedResources   []BlockedResource
	HookChanges        []HookChange `json:"-"`
	RetainedPluginData []string     `json:"retained_plugin_data,omitempty"`
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
	Target         string `yaml:"target"`
	Path           string `yaml:"path"`
	RootID         string `yaml:"root_id,omitempty"`
	RootKey        string `yaml:"root_key,omitempty"`
	PluginInstance string `yaml:"plugin_instance,omitempty"`
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
	raw             []byte
	mode            fs.FileMode
	exists          bool
	pin             portableManifestSnapshot
	publicationHook func(string)
}

type portableSyncContext struct {
	boundary       *destinationBoundary
	state          *portableState
	target         hcc.Target
	reportPrefix   string
	desired        map[string]bool
	supportDesired map[string]string
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
	state.raw = append([]byte(nil), state.pin.data...)
	state.exists = state.pin.data != nil
	state.mode = state.pin.file.Mode.Perm()
	state.entries, err = decodePortableManifestEntries(path, state.pin.data, machineScope)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func decodePortableManifestEntries(path string, data []byte, machineScope bool) (map[string]portableManifestEntry, error) {
	entries := make(map[string]portableManifestEntry)
	if data == nil {
		return entries, nil
	}
	var manifest portableManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
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
		if entry.PluginInstance != "" {
			if _, err := ownership.PluginStatePath(entry.PluginInstance); err != nil {
				return nil, fmt.Errorf("invalid plugin-owned materialization entry %q", entry.Path)
			}
		}
		if err := admitPortableNomination(hcc.Target(entry.Target), machineScope, entry.Path, clean); err != nil {
			return nil, err
		}
		entry.Path = clean
		entries[portableManifestKey(hcc.Target(entry.Target), clean)] = entry
	}
	return entries, nil
}

type portableNominationPartition struct {
	prefix     string
	components []string
	// aggregates holds the partition's shared files as manifest paths, indexed
	// by scope (repository, then machine).
	aggregates [2][]string
}

var portableNominationPartitions = func() map[hcc.Target][]portableNominationPartition {
	partitions := make(map[hcc.Target][]portableNominationPartition)
	for _, definition := range targetmeta.Definitions() {
		if !definition.PartitionedRoots {
			continue
		}
		layout, _ := targetmeta.LookupNativeLayout(definition.Target)
		for _, partition := range layout.Partitions {
			nomination := portableNominationPartition{prefix: strings.TrimSuffix(partition.Prefix, "/") + "/", components: partition.Components}
			for _, aggregate := range partition.Aggregates {
				nomination.aggregates[0] = append(nomination.aggregates[0], nomination.prefix+aggregate.Project.Path)
				nomination.aggregates[1] = append(nomination.aggregates[1], nomination.prefix+aggregate.User.Path)
			}
			partitions[definition.Target] = append(partitions[definition.Target], nomination)
		}
	}
	return partitions
}()

// admitPortableNomination refuses a manifest entry that does not name one
// resource inside a native component directory. Writers record only that
// shape, while harness target roots also hold shared harness configuration
// (some of it inside component directories, such as hooks/hooks.json) and the
// Codex .agents partition holds Agent Marketplace configuration and lockfiles.
// A digest proves no authorship because committed bytes are public, so a
// matching digest must not make such a path prunable. Single-root targets get
// no component allowlist: their declared components are not exhaustive
// (Gemini subagents, rules), and refusing a genuine entry would block the
// scope's materialization.
func admitPortableNomination(target hcc.Target, machineScope bool, path, clean string) error {
	definition, ok := targetmeta.Lookup(string(target))
	if !ok {
		return nil
	}
	scope := 0
	if machineScope {
		scope = 1
	}
	rel, components, aggregates := clean, []string(nil), []string(nil)
	if definition.PartitionedRoots {
		matched := false
		for _, partition := range portableNominationPartitions[target] {
			if after, ok := strings.CutPrefix(clean, partition.prefix); ok {
				rel, components, aggregates, matched = after, partition.components, partition.aggregates[scope], true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: portable %s path %q is outside native destinations", fsboundary.ErrUnsafePath, target, path)
		}
	} else {
		info, ok := harness.TargetInfoFor(target)
		if !ok {
			return fmt.Errorf("%w: portable %s path %q has no native layout", fsboundary.ErrUnsafePath, target, path)
		}
		aggregates = info.AggregateFilesForScope(machineScope)
		if target == hcc.TargetWindsurf && !machineScope {
			if after, found := strings.CutPrefix(clean, windsurfDir+"/"); found {
				rel = after
			} else if nativeHookSupportNomination(target, machineScope, clean) {
				rel = clean
			} else {
				return fmt.Errorf("%w: portable Windsurf path %q is outside native output", fsboundary.ErrUnsafePath, path)
			}
		}
	}
	component, resource, _ := strings.Cut(rel, "/")
	if resource == "" || (definition.PartitionedRoots && !slices.Contains(components, component)) {
		return fmt.Errorf("%w: portable %s path %q does not name a resource inside a native component", fsboundary.ErrUnsafePath, target, path)
	}
	for _, aggregate := range aggregates {
		if nominationCovers(clean, filepath.ToSlash(filepath.Clean(aggregate))) {
			return fmt.Errorf("%w: portable %s path %q names shared harness configuration", fsboundary.ErrUnsafePath, target, path)
		}
	}
	return nil
}

func nativeHookSupportNomination(target hcc.Target, machineScope bool, clean string) bool {
	declaration, err := nativeHookAggregateForScope(targetmeta.Target(target), machineScope)
	if err != nil {
		return false
	}
	root := path.Join(declaration.partition.Prefix, declaration.aggregate.SupportDir)
	rest, ok := strings.CutPrefix(clean, root+"/")
	if !ok {
		return false
	}
	sourceKey, rest, ok := strings.Cut(rest, "/")
	if !ok || !validNativeSupportSourceKey(sourceKey) {
		return false
	}
	hookName, fileRel, ok := strings.Cut(rest, "/")
	if !ok || ir.ValidateName(hookName) != nil || !fs.ValidPath(fileRel) || fileRel == "." {
		return false
	}
	return clean == path.Join(root, sourceKey, hookName, fileRel)
}

func validNativeSupportSourceKey(key string) bool {
	dash := strings.LastIndexByte(key, '-')
	if dash <= 0 {
		return false
	}
	slug := key[:dash]
	if len(slug) > 64 || strings.ContainsAny(slug[:1], "-.") || strings.ContainsAny(slug[len(slug)-1:], "-.") {
		return false
	}
	for _, r := range slug {
		if !hookSupportSlugRune(r) {
			return false
		}
	}
	digest := key[dash+1:]
	if len(digest) != sha256.Size*2 {
		return false
	}
	for _, r := range digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// nominationCovers reports whether nomination equals path or is one of its
// ancestors. Elements compare case-insensitively because the default macOS
// and Windows filesystems resolve a case variant to the same file.
func nominationCovers(nomination, path string) bool {
	for {
		element, nominationRest, nominationMore := strings.Cut(nomination, "/")
		pathElement, pathRest, pathMore := strings.Cut(path, "/")
		if !strings.EqualFold(element, pathElement) {
			return false
		}
		if !nominationMore {
			return true
		}
		if !pathMore {
			return false
		}
		nomination, path = nominationRest, pathRest
	}
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

func (s *portableState) addPlugin(target hcc.Target, path, digest, instanceKey string) {
	s.add(target, path, digest)
	entry := s.entries[portableManifestKey(target, filepath.ToSlash(filepath.Clean(path)))]
	entry.PluginInstance = instanceKey
	s.entries[portableManifestKey(target, entry.Path)] = entry
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

func (s *portableState) serialized() ([]byte, bool, error) {
	if len(s.entries) == 0 {
		return nil, false, nil
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
	return data, true, err
}

func (s *portableState) save(dryRun bool) error {
	if dryRun {
		return nil
	}
	data, hasEntries, err := s.serialized()
	if err != nil {
		return err
	}
	if !hasEntries {
		return s.removeManifest()
	}
	return s.publishManifest(data)
}

// RunOption configures optional behavior for Run.
type RunOption func(*runConfig)

type runConfig struct {
	dryRun                  bool
	pluginStateReconcile    bool
	scopeFilter             *bool // nil = all scopes; true = machine only; false = repo only
	modes                   config.MaterializationModes
	beforeSupportWrite      func(string, string) error
	supportCommitFault      dependencytxn.FaultHook
	linkPublicationHook     func(string)
	pruneClassificationHook func(string)
	pruneEnumerationHook    func(string)
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

func WithPluginStateReconciliation() RunOption {
	return func(c *runConfig) { c.pluginStateReconcile = true }
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

func withNativeHookSupportWriteFault(fault func(string, string) error) RunOption {
	return func(c *runConfig) { c.beforeSupportWrite = fault }
}

func withNativeHookSupportCommitFault(fault dependencytxn.FaultHook) RunOption {
	return func(c *runConfig) { c.supportCommitFault = fault }
}

// skillTarget pairs a resolved skill with a specific compilation target.
type skillTarget struct {
	skill        resolver.ResolvedArtifact
	target       hcc.Target
	machineScope bool
}

type hookSupportOwner struct {
	skill               skillTarget
	resourceName        string
	sourceDigest        string
	sourceSnapshot      *hookSourceSnapshot
	sourceExcludedPeers []string
}

type hookSourceSnapshotKey struct {
	snapshot      *hookSourceSnapshot
	excludedPeers string
}

func blockedResourceFor(key outputKey, w skillTarget) BlockedResource {
	name := w.skill.Name
	if w.skill.InstalledName != "" {
		name = w.skill.InstalledName
	}
	return BlockedResource{Kind: w.skill.Kind, Name: name, Target: key.target, MachineScope: key.machineScope}
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
	pruneEnumerationHook    func(string)
	manifestPublicationHook func(string)
	scopeFilter             *bool
	allDiags                []diag.Diagnostic
	byTarget                map[outputKey]*targetWork
	skillLinks              map[outputKey]map[string]*skillLinkGroup
	allOutputs              map[outputKey]bool
	blocked                 blockedOutputs
	blockedResources        []BlockedResource
	nativeHooks             map[outputKey]*ownedJSONAggregateWork
	nativeMCPEntries        map[outputKey][]ownedJSONEntry
	nativeMCPConflicts      map[outputKey]map[string]bool
	nativeMCPIncomplete     map[outputKey]bool
	hookSupportPaths        map[outputKey]map[string]bool
	hookSupportOwners       map[outputKey]map[string][]hookSupportOwner
	hookResources           map[outputKey][]BlockedResource
	selectedHookNames       map[outputKey]map[string]bool
	unknownHookSelection    map[outputKey]bool
	mcpResources            map[outputKey][]BlockedResource
	// artifactMeta maps (outputKey → artifact relative path → skillLink metadata)
	// for artifacts that remain in result.Artifacts (not extracted into skillLinks).
	// Used to generate rich collision hints in syncArtifactLinks.
	artifactMeta map[outputKey]map[string]skillLink
	// Owned aggregate state is opaque to generic orchestration. Each target
	// adapter stores its JSON or TOML work value behind this map and exposes it
	// only through ownedAggregateHandler.
	ownedState    map[outputKey]any
	plugins       map[string]*pluginInstancePlan
	pluginBlocked map[string]bool
}

type pluginInstancePlan struct {
	key           outputKey
	ref           ownership.PluginGenerationRef
	artifacts     []artifact.Artifact
	hooks         *ownedJSONAggregateWork
	mcpEntries    []ownedJSONEntry
	registrations []ownedJSONEntry
	aggregate     ownedAggregateState
	aggregateSet  bool
	handler       ownedAggregateHandler
	members       []ownership.PluginMemberRef
	catalog       nativePluginCatalog
	removed       bool
}

func (r compilePhaseResult) ownedAggregateState() ownedAggregateState {
	nativeHooks := make(map[outputKey]bool, len(r.nativeHooks))
	for key := range r.nativeHooks {
		nativeHooks[key] = true
	}
	return ownedAggregateState{values: r.ownedState, nativeHooks: nativeHooks, writeSessions: make(map[string]ownedAggregateWriteSession)}
}

// Run compiles and materializes the given resolved skills into their
// per-target install roots. It groups skills by hcc.Target (defaulting
// to [TargetClaude] when Harnesses is nil), calls hcc.Compile per-skill-
// per-target, merges fragments, then calls hcc.Materialize with
// WriteModeSync and prune-stale enabled.
//
// Skills with Resource==nil are skipped with a warning diagnostic.
func Run(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, opts ...RunOption) (*Report, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var rc runConfig
	rc.modes = config.DefaultMaterializationModes()
	for _, o := range opts {
		o(&rc)
	}
	if rc.supportCommitFault != nil {
		ctx = context.WithValue(ctx, nativeRepositorySupportCommitFaultKey{}, rc.supportCommitFault)
	}
	hasPluginPackage := false
	for _, resource := range resolved {
		if resource.PluginPackage != nil && resource.PluginPackage.Package != nil {
			hasPluginPackage = true
			break
		}
	}
	if hasPluginPackage && !rc.dryRun {
		var report *Report
		err := packagegen.WithPackagesLock(ctx, func() error {
			var runErr error
			report, runErr = runMaterialization(ctx, repoRoot, resolved, rc, true)
			return runErr
		})
		return report, err
	}
	return runMaterialization(ctx, repoRoot, resolved, rc, false)
}

func runMaterialization(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact, rc runConfig, packagesLocked bool) (*Report, error) {
	start := time.Now()
	slog.Debug("materialize start", "repo_root", repoRoot, "resolved", len(resolved), "dry_run", rc.dryRun,
		"machine_mode", rc.modes.Machine, "project_mode", rc.modes.Project)

	pluginNamespaceBlocked, pluginNamespaceDiags := pluginNamespaceRefusals(resolved)
	pluginDependencyBlocked, pluginDependencyDiags := pluginDependencyRefusals(resolved)
	for identity := range pluginDependencyBlocked {
		pluginNamespaceBlocked[identity] = true
	}
	resolved, pluginGenerationDiags := preparePluginGenerations(resolved, repoRoot, rc.dryRun, packagesLocked, pluginNamespaceBlocked)
	// Phase 1: Group skills by target and track all output keys.
	work, allOutputs, blocked, allDiags := buildWorkItems(resolved, repoRoot, pluginNamespaceBlocked)
	allDiags = append(allDiags, pluginGenerationDiags...)
	allDiags = append(allDiags, pluginNamespaceDiags...)
	allDiags = append(allDiags, pluginDependencyDiags...)
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
	markNilSharedNativeMCPInputs(resolved, repoRoot, rc.scopeFilter, compileResult.nativeMCPIncomplete)
	compileResult.scopeFilter = rc.scopeFilter
	slog.Debug("materialize compile complete", "targets", len(compileResult.byTarget), "diagnostics", len(compileResult.allDiags))

	// Phase 3: Materialize per target — write artifacts and sync skill links.
	compileResult.linkPublicationHook = rc.linkPublicationHook
	compileResult.pruneClassificationHook = rc.pruneClassificationHook
	compileResult.pruneEnumerationHook = rc.pruneEnumerationHook
	compileResult.manifestPublicationHook = rc.manifestPublicationHook
	report := executeMaterializePlans(ctx, compileResult, rc.dryRun, repoRoot, rc.modes, rc.beforeSupportWrite)
	if rc.pluginStateReconcile {
		reconcilePluginInstances(ctx, repoRoot, resolved, compileResult, report, pluginNamespaceBlocked, rc.dryRun, packagesLocked, rc.supportCommitFault)
	}
	slog.Info("materialize end",
		"duration_ms", time.Since(start).Milliseconds(),
		"targets", len(report.PerRoot),
		"diagnostics", len(report.Diags),
	)
	return report, nil
}

func pluginDependencyRefusals(resolved []resolver.ResolvedArtifact) (map[string]bool, []diag.Diagnostic) {
	blocked := make(map[string]bool)
	reported := make(map[string]bool)
	var diagnostics []diag.Diagnostic
	for _, resource := range resolver.MaterializationWinners(resolved) {
		pkg := resource.PluginPackage
		if pkg == nil || pkg.Package == nil || !pkg.Package.HasDependencies() || pkg.Delivery == "native" {
			continue
		}
		for _, target := range resolver.HarnessTargets(resource.Harnesses) {
			identity := pluginInstanceIdentity(resource, target)
			blocked[identity] = true
			if reported[identity] {
				continue
			}
			diagnostics = append(diagnostics, diag.Diagnostic{
				Code:     diag.CodePluginFormatDependenciesUnsupported,
				Severity: diag.SeverityError,
				Target:   string(target),
				Resource: "plugin/" + pkg.Name,
				Message:  "plugin declares dependencies that cannot be installed; no members were installed",
				Source:   diag.SourceRef{File: pkg.PackageRoot},
			})
			reported[identity] = true
		}
	}
	return blocked, diagnostics
}

func preparePluginGenerations(resolved []resolver.ResolvedArtifact, repoRoot string, dryRun, packagesLocked bool, namespaceBlocked map[string]bool) ([]resolver.ResolvedArtifact, []diag.Diagnostic) {
	out := append([]resolver.ResolvedArtifact(nil), resolved...)
	type generationResult struct {
		generation packagegen.Generation
		err        error
	}
	generations := make(map[string]generationResult)
	var diagnostics []diag.Diagnostic
	reported := make(map[string]bool)
	for i := range out {
		resource := &out[i]
		pkg := resource.PluginPackage
		if pkg == nil || pkg.Package == nil {
			continue
		}
		targets := resolver.HarnessTargets(resource.Harnesses)
		if len(targets) == 0 {
			continue
		}
		pluginPackage := *pkg
		scope := pluginGenerationScope(*resource, repoRoot)
		pluginPackage.GenerationScope = scope
		pluginPackage.TargetGenerations = make(map[string]resolver.PluginTargetGeneration, len(targets))
		for _, target := range targets {
			pluginPackage.InstanceKey = packagegen.InstanceKey(pkg.Name, target, scope)
			if namespaceBlocked[pluginInstanceIdentity(*resource, target)] ||
				pkg.Delivery != "native" && len(pkg.DecodeFailures) > 0 {
				continue
			}
			input := packagegen.Input{
				Plugin:             pkg.Package,
				PackageRoot:        pkg.PackageRoot,
				Target:             target,
				Delivery:           pkg.Delivery,
				Scope:              scope,
				Exclusions:         pkg.Exclusions,
				CatalogFingerprint: pkg.CatalogFingerprint,
			}
			identity := packagegen.BuildKey(input)
			generation, found := generations[identity]
			if !found {
				if dryRun {
					generation.generation, generation.err = packagegen.Check(input)
				} else if packagesLocked {
					generation.generation, generation.err = packagegen.PublishUnderPackagesLock(input)
				} else {
					generation.generation, generation.err = packagegen.Publish(input)
				}
				generations[identity] = generation
			}
			pluginPackage.GenerationKey = generation.generation.Key
			pluginPackage.GenerationPath = generation.generation.Path
			pluginPackage.DataPath = generation.generation.DataPath
			if generation.err != nil {
				pluginPackage.GenerationError = generation.err.Error()
				slog.Warn("plugin package generation preparation failed", "plugin", pkg.Name, "target", target, "generation_key", generation.generation.Key, "error", generation.err)
				if !reported[identity] {
					code := diag.CodePluginGenerationPublish
					if dryRun {
						code = diag.CodePluginGenerationMissing
					}
					diagnostics = append(diagnostics, diag.Diagnostic{
						Code:     code,
						Severity: diag.SeverityError,
						Target:   string(target),
						Resource: "plugin/" + pkg.Name,
						Message:  fmt.Sprintf("plugin package generation could not be prepared safely: %v", generation.err),
						Source:   diag.SourceRef{File: pkg.PackageRoot},
					})
					reported[identity] = true
				}
			}
			targetGeneration := resolver.PluginTargetGeneration{
				Scope: scope, Delivery: pluginPackage.Delivery, NativeRefresh: pluginPackage.NativeRefresh,
				ReloadRequired: pluginPackage.ReloadRequired, InstanceKey: pluginPackage.InstanceKey,
				GenerationKey: pluginPackage.GenerationKey, GenerationPath: pluginPackage.GenerationPath,
				DataPath: pluginPackage.DataPath,
			}
			if generation.err != nil {
				targetGeneration.GenerationError = generation.err.Error()
			}
			pluginPackage.TargetGenerations[string(target)] = targetGeneration
		}
		resource.PluginPackage = &pluginPackage
	}
	return out, diagnostics
}

func markNilSharedNativeMCPInputs(resolved []resolver.ResolvedArtifact, repoRoot string, scopeFilter *bool, incomplete map[outputKey]bool) {
	if incomplete == nil {
		return
	}
	for _, resource := range resolver.MaterializationWinners(resolved) {
		if resource.Unresolved || resource.Resource != nil || resource.Kind != ir.KindMcpServer {
			continue
		}
		targets := resolver.HarnessTargets(resource.Harnesses)
		if len(targets) == 0 {
			continue
		}
		machineScope := resource.Source.IsMachineScope()
		if scopeFilter != nil && machineScope != *scopeFilter {
			continue
		}
		for _, target := range targets {
			key := outputKey{target: target, machineScope: machineScope}
			if nativeHookSharesMCPRoute(key, repoRoot) {
				incomplete[key] = true
			}
		}
	}
}

// buildWorkItems groups resolved skills by target and identifies all output
// keys (including empty targets that need pruning).
func buildWorkItems(resolved []resolver.ResolvedArtifact, repoRoot string, pluginNamespaceBlocked map[string]bool) ([]skillTarget, map[outputKey]bool, blockedOutputs, []diag.Diagnostic) {
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
		targets := resolver.HarnessTargets(s.Harnesses)
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
			if s.PluginPackage != nil && pluginNamespaceBlocked[pluginInstanceIdentity(s, t)] {
				blocked.blockResource(key, skillTarget{skill: s, target: t, machineScope: machineScope})
				continue
			}
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
			selected := s
			if s.PluginPackage != nil {
				pkg := *s.PluginPackage
				if generation, ok := pkg.TargetGenerations[string(t)]; ok {
					pkg.GenerationScope = generation.Scope
					pkg.Delivery = generation.Delivery
					pkg.NativeRefresh = generation.NativeRefresh
					pkg.ReloadRequired = generation.ReloadRequired
					pkg.InstanceKey = generation.InstanceKey
					pkg.GenerationKey = generation.GenerationKey
					pkg.GenerationPath = generation.GenerationPath
					pkg.DataPath = generation.DataPath
					pkg.GenerationError = generation.GenerationError
				}
				selected.PluginPackage = &pkg
			}
			work = append(work, skillTarget{skill: selected, target: t, machineScope: machineScope})
		}
	}

	// Include previously materialized targets even when they have no
	// resolved artifacts in this run, so stale managed files get pruned.
	addExistingOutputKeys(repoRoot, allOutputs)
	addExistingNativeAggregateKeys(repoRoot, allOutputs)
	addExistingNativeHookOutputKeys(repoRoot, allOutputs)
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
		cloned := *typed
		cloned.SetName(name)
		return &cloned, nil
	case *ir.Command:
		cloned := *typed
		cloned.SetName(name)
		return &cloned, nil
	case *ir.Subagent:
		cloned := *typed
		cloned.SetName(name)
		return &cloned, nil
	case *ir.Hook:
		cloned := *typed
		cloned.SetName(name)
		return &cloned, nil
	case *ir.McpServer:
		cloned := *typed
		cloned.SetName(name)
		return &cloned, nil
	default:
		return nil, fmt.Errorf("unsupported Marketplace resource kind %s for installed name", resource.Kind())
	}
}

func hookDuplicateExecutionDiagnostics(work []skillTarget) []diag.Diagnostic {
	selectedByScope := [2]map[targetmeta.Target]struct{}{
		make(map[targetmeta.Target]struct{}),
		make(map[targetmeta.Target]struct{}),
	}
	for _, item := range work {
		if item.skill.Resource == nil || item.skill.Resource.Kind() != ir.KindHook {
			continue
		}
		scope := 0
		if item.machineScope {
			scope = 1
		}
		selectedByScope[scope][targetmeta.Target(item.target)] = struct{}{}
	}

	var diagnostics []diag.Diagnostic
	for scope, machineScope := range []bool{false, true} {
		selectedSet := selectedByScope[scope]
		if len(selectedSet) == 0 {
			continue
		}
		selected := make([]targetmeta.Target, 0, len(selectedSet))
		for target := range selectedSet {
			selected = append(selected, target)
		}
		risks, err := hookpolicy.DuplicateExecutionRisks(selected)
		if err != nil {
			diagnostics = append(diagnostics, diag.Diagnostic{
				Code:     "materialize.compile.error",
				Severity: diag.SeverityError,
				Message:  "Hook duplicate-execution risk could not be evaluated",
			})
			continue
		}
		scopeName := "repository"
		if machineScope {
			scopeName = "machine"
		}
		for _, risk := range risks {
			loader := string(risk.Loader)
			loaded := string(risk.Loaded)
			diagnostics = append(diagnostics, diag.Diagnostic{
				Code:     diag.CodeHookDuplicateExecutionRisk,
				Severity: diag.SeverityWarning,
				Target:   loader,
				Message:  fmt.Sprintf("Selected harness targets %s and %s may execute the same hooks in %s scope; %s also loads %s hook files", loader, loaded, scopeName, loader, loaded),
			})
		}
	}
	return diagnostics
}

// compileWorkItems compiles each skill-target pair, splits artifacts by kind,
// and groups results by output key.
func compileWorkItems(work []skillTarget, allDiags []diag.Diagnostic, allOutputs map[outputKey]bool, blocked blockedOutputs, repoRoot string) compilePhaseResult {
	allDiags = append(allDiags, hookDuplicateExecutionDiagnostics(work)...)
	pluginBlocked, pluginDiags := preflightPluginInstances(work)
	allDiags = append(allDiags, pluginDiags...)
	byTarget := make(map[outputKey]*targetWork)
	skillLinks := make(map[outputKey]map[string]*skillLinkGroup)
	artifactMeta := make(map[outputKey]map[string]skillLink)
	ownedState := make(map[outputKey]any)
	ownedAggregates := ownedAggregateState{values: ownedState}
	pluginInstances := make(map[string]*pluginInstancePlan)
	pluginPlanFor := func(w skillTarget) *pluginInstancePlan {
		pkg := w.skill.PluginPackage
		if pkg == nil || pkg.InstanceKey == "" || pkg.GenerationKey == "" {
			return nil
		}
		plan := pluginInstances[pkg.InstanceKey]
		if plan == nil {
			plan = &pluginInstancePlan{
				key: outputKey{target: w.target, machineScope: w.machineScope},
				ref: ownership.PluginGenerationRef{
					InstanceKey:    pkg.InstanceKey,
					Name:           pkg.Name,
					Target:         string(w.target),
					Scope:          pkg.GenerationScope,
					GenerationKey:  pkg.GenerationKey,
					Delivery:       pkg.Delivery,
					Refresh:        pkg.NativeRefresh,
					ReloadRequired: pkg.ReloadRequired,
					SourceRevision: pkg.SourceRevision,
				},
			}
			pluginInstances[pkg.InstanceKey] = plan
		}
		kind := w.skill.Resource.Kind().String()
		name := w.skill.Name
		if w.skill.InstalledName != "" {
			name = w.skill.InstalledName
		}
		member := ownership.PluginMemberRef{Kind: kind, Name: name}
		found := false
		for _, existing := range plan.members {
			if existing == member {
				found = true
				break
			}
		}
		if !found {
			plan.members = append(plan.members, member)
		}
		return plan
	}
	nativeHooks := make(map[outputKey]*ownedJSONAggregateWork)
	nativeMCPEntries := make(map[outputKey][]ownedJSONEntry)
	nativeMCPConflicts := make(map[outputKey]map[string]bool)
	nativeMCPIncomplete := make(map[outputKey]bool)
	hookSupportPaths := make(map[outputKey]map[string]bool)
	hookSupportOwners := make(map[outputKey]map[string][]hookSupportOwner)
	hookResources := make(map[outputKey][]BlockedResource)
	selectedHookNames := make(map[outputKey]map[string]bool)
	unknownHookSelection := make(map[outputKey]bool)
	for key := range blocked {
		if blocked.componentsFor(key)[blockedAllComponents] {
			unknownHookSelection[key] = true
		}
	}
	mcpResources := make(map[outputKey][]BlockedResource)
	hookSupport, supportConflicts := prepareHookSupport(work, repoRoot)
	for key := range allOutputs {
		if nativeHookOutputPresent(key, repoRoot) {
			nativeHooks[key] = &ownedJSONAggregateWork{}
		}
	}
	nativeHookWork := func(key outputKey) *ownedJSONAggregateWork {
		if nativeHooks[key] == nil {
			nativeHooks[key] = &ownedJSONAggregateWork{}
		}
		return nativeHooks[key]
	}
	markNativeHookIncomplete := func(key outputKey) {
		_, file, err := nativeHookJSONLayout(key, repoRoot)
		if err != nil {
			return
		}
		nativeHookWork(key).markIncomplete(file.rel)
	}
	var blockedResources []BlockedResource
	blockResource := func(key outputKey, w skillTarget) {
		blocked.blockResource(key, w)
		blockedResources = append(blockedResources, blockedResourceFor(key, w))
	}
	blockPluginInstance := func(w skillTarget) {
		if w.skill.PluginPackage == nil {
			return
		}
		pluginBlocked[pluginInstanceIdentity(w.skill, w.target)] = true
		delete(pluginInstances, w.skill.PluginPackage.InstanceKey)
	}
	markKindIncomplete := func(key outputKey, kind ir.ResourceKind, handler ownedAggregateHandler) {
		if kind == ir.KindHook {
			markNativeHookIncomplete(key)
			return
		}
		if kind == ir.KindMcpServer && nativeHookSharesMCPRoute(key, repoRoot) {
			nativeMCPIncomplete[key] = true
			markNativeHookIncomplete(key)
			if info, ok := harness.TargetInfoFor(key.target); ok {
				blocked.mark(key, hookBlockedComponents(key.target, info, key.machineScope)...)
			}
			return
		}
		markOwnedAggregateKindIncomplete(handler, ownedAggregates, key, kind)
	}
	markResourceIncomplete := func(key outputKey, w skillTarget, handler ownedAggregateHandler) {
		if w.skill.Resource != nil {
			markKindIncomplete(key, w.skill.Resource.Kind(), handler)
		}
	}

	for index, w := range work {
		key := outputKey{target: w.target, machineScope: w.machineScope}
		if w.skill.PluginPackage != nil && w.skill.PluginPackage.GenerationError != "" {
			blockResource(key, w)
			continue
		}
		if w.skill.PluginPackage != nil && pluginBlocked[pluginInstanceIdentity(w.skill, w.target)] {
			blockResource(key, w)
			handler := ownedAggregateHandlerFor(key.target)
			markResourceIncomplete(key, w, handler)
			if len(w.skill.PluginPackage.DecodeFailures) > 0 {
				for _, kind := range w.skill.PluginPackage.DecodeKinds {
					blocked.blockKind(key, kind)
					markKindIncomplete(key, kind, handler)
				}
			}
			continue
		}
		if w.skill.PluginPackage != nil && w.skill.PluginPackage.Delivery == "native" {
			if plan := pluginPlanFor(w); plan != nil {
				plan.handler = ownedAggregateHandlerFor(w.target)
			}
			continue
		}
		binding, hasBinding, bindingErr := materializeAdapterFor(w.target)
		if bindingErr != nil {
			blockResource(key, w)
			if w.skill.Resource != nil && w.skill.Resource.Kind() == ir.KindHook {
				markNativeHookIncomplete(key)
			}
			allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.adapter.error", Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Name, Message: bindingErr.Error()})
			continue
		}
		resourceKind := w.skill.Resource.Kind()
		isHook := resourceKind == ir.KindHook
		var aggregateHandler ownedAggregateHandler
		if hasBinding && !isHook {
			aggregateHandler = binding.handler
		}
		var plan hookSupportPlan
		if isHook {
			nativeHookWork(key)
			plan = hookSupport[index]
			resourceName := w.skill.Name
			if w.skill.InstalledName != "" {
				resourceName = w.skill.InstalledName
			}
			hookResources[key] = append(hookResources[key], BlockedResource{Kind: ir.KindHook, Name: resourceName, Target: key.target, MachineScope: key.machineScope})
			if selectedHookNames[key] == nil {
				selectedHookNames[key] = make(map[string]bool)
			}
			selectedHookNames[key][resourceName] = true
			if _, err := nativeHookSourceIdentity(w.skill); err != nil {
				unknownHookSelection[key] = true
			}
		} else if resourceKind == ir.KindMcpServer && nativeHookSharesMCPRoute(key, repoRoot) {
			nativeHookWork(key)
			resourceName := w.skill.Name
			if w.skill.InstalledName != "" {
				resourceName = w.skill.InstalledName
			}
			mcpResources[key] = append(mcpResources[key], BlockedResource{Kind: ir.KindMcpServer, Name: resourceName, Target: key.target, MachineScope: key.machineScope})
		}
		if paths := supportConflicts[index]; len(paths) > 0 {
			unknownHookSelection[key] = true
			blockResource(key, w)
			markResourceIncomplete(key, w, aggregateHandler)
			allDiags = append(allDiags, diag.Diagnostic{Code: diag.CodeMaterializeHookSupportConflict, Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Name, Message: fmt.Sprintf("hook support paths conflict across selected sources: %s; affected hooks were not emitted", strings.Join(paths, ", ")), Hint: "Rename the conflicting support files or exclude one hook provider", Source: compilepkg.SourceOf(w.skill.Resource)})
			continue
		}
		if isHook && plan.err != nil {
			unknownHookSelection[key] = true
			blockResource(key, w)
			markResourceIncomplete(key, w, aggregateHandler)
			source := compilepkg.SourceOf(w.skill.Resource)
			allDiags = append(allDiags, diag.Diagnostic{
				Code:     "materialize.compile.error",
				Severity: diag.SeverityError,
				Target:   string(w.target),
				Resource: w.skill.Name,
				Message:  "Hook support could not be prepared safely; hook suppressed",
				Source:   source,
			})
			if plan.supportAdmissionRefused {
				allDiags = append(allDiags, diag.Diagnostic{
					Code:     diag.CodeMaterializeHookSupportRefused,
					Severity: diag.SeverityError,
					Target:   string(w.target),
					Resource: w.skill.Name,
					Message:  "Hook support source could not be admitted safely; installed hook content preserved",
					Source:   source,
				})
			}
			continue
		}
		if isHook && plan.sourceDigest == "" {
			unknownHookSelection[key] = true
			blockResource(key, w)
			markResourceIncomplete(key, w, aggregateHandler)
			allDiags = append(allDiags, diag.Diagnostic{
				Code:     "materialize.compile.error",
				Severity: diag.SeverityError,
				Target:   string(w.target),
				Resource: w.skill.Name,
				Message:  "Hook source proof is unavailable; hook suppressed",
				Source:   compilepkg.SourceOf(w.skill.Resource),
			})
			continue
		}
		compileOptions := []hcc.Option{}
		if w.skill.PluginPackage != nil {
			resource, options, err := pluginMemberCompileInput(w)
			if err != nil {
				blockResource(key, w)
				markResourceIncomplete(key, w, aggregateHandler)
				allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.marketplace.alias-invalid", Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Resource.Name(), Message: "Marketplace installed name cannot be materialized safely"})
				continue
			}
			w.skill.Resource = resource
			compileOptions = append(compileOptions, options...)
		} else if w.skill.IsMarketplace && w.skill.InstalledName != "" {
			renamed, err := marketplaceResourceForInstallName(w.skill.Resource, w.skill.InstalledName)
			if err != nil {
				blockResource(key, w)
				markResourceIncomplete(key, w, aggregateHandler)
				allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.marketplace.alias-invalid", Severity: diag.SeverityError, Target: string(w.target), Resource: w.skill.Resource.Name(), Message: "Marketplace installed name cannot be materialized safely"})
				continue
			}
			w.skill.Resource = renamed
		}
		if isHook {
			commandBase, err := hookSupportCommandBase(w, repoRoot, plan.relBase)
			if err != nil {
				blockResource(key, w)
				markResourceIncomplete(key, w, aggregateHandler)
				allDiags = append(allDiags, diag.Diagnostic{
					Code:     "materialize.compile.error",
					Severity: diag.SeverityError,
					Target:   string(w.target),
					Resource: w.skill.Name,
					Message:  "Hook support destination could not be resolved safely; hook suppressed",
					Source:   compilepkg.SourceOf(w.skill.Resource),
				})
				continue
			}
			if w.skill.PluginPackage == nil {
				compileOptions = append(compileOptions, hcc.WithPluginRoot(commandBase), hcc.WithNativeHookOutput())
			}
			if w.machineScope {
				compileOptions = append(compileOptions, hcc.WithScope(hcc.ScopeUser))
			}
		} else if w.skill.PluginPackage == nil {
			if pluginRoot := sourceBundleRoot(w.skill); pluginRoot != "" {
				compileOptions = append(compileOptions, hcc.WithPluginRoot(pluginRoot))
			}
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
			case ir.KindMcpServer, ir.KindSkill, ir.KindSubagent:
				compileOptions = append(compileOptions, hcc.WithManagedBy("_managed_by", "agent-marketplace"))
			}
		} else if w.machineScope && !isHook {
			// Ownership-backed compilers use ScopeUser to select their native
			// machine aggregate layout. ScopeProject is the default, so only the
			// machine-scope pass has to say so.
			compileOptions = append(compileOptions, hcc.WithScope(hcc.ScopeUser))
		}
		result, err := hcc.Compile(w.skill.Resource, w.target, compileOptions...)
		if err != nil {
			blockResource(key, w)
			markResourceIncomplete(key, w, aggregateHandler)
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
			blockPluginInstance(w)
			markResourceIncomplete(key, w, aggregateHandler)
			continue
		}

		result.Artifacts = append([]artifact.Artifact(nil), result.Artifacts...)
		if diag.HasErrors(result.Diagnostics) {
			blockResource(key, w)
			blockPluginInstance(w)
			markResourceIncomplete(key, w, aggregateHandler)
			tw := byTarget[key]
			if tw == nil {
				tw = &targetWork{}
				byTarget[key] = tw
			}
			tw.diags = append(tw.diags, result.Diagnostics...)
			continue
		}
		// Ownership-reconciled fragments leave the generic artifact pipeline
		// before any marker-merge or whole-file plan can see them.
		// Legacy tolerance for ownership-proven native hooks: a selected hook
		// the durable record proves is already completely present in the
		// target's documented native file — matching source fingerprint and
		// ownership resource identity, never raw legacy rule equality — is
		// satisfied and must not be re-emitted into the legacy file. A
		// malformed or unsafe native file fails this whole (target, scope)
		// closed before any legacy mutation.
		if resourceKind == ir.KindMcpServer && nativeHookSharesMCPRoute(key, repoRoot) {
			entries, remaining, mcpErr := nativeHookSharedMCPEntries(key, repoRoot, result.Artifacts)
			attachPluginInstanceOwner(entries, w.skill.PluginPackage)
			resourceName := w.skill.Name
			if w.skill.InstalledName != "" {
				resourceName = w.skill.InstalledName
			}
			if mcpErr != nil || len(entries) == 0 {
				blockResource(key, w)
				blockPluginInstance(w)
				markResourceIncomplete(key, w, aggregateHandler)
				allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.compile.error", Severity: diag.SeverityError, Target: string(w.target), Resource: resourceName, Message: "shared MCP output could not be prepared safely; previous entries were retained", Source: compilepkg.SourceOf(w.skill.Resource)})
				continue
			}
			result.Artifacts = remaining
			pluginPlan := pluginPlanFor(w)
			if pluginPlan != nil {
				pluginPlan.mcpEntries = append(pluginPlan.mcpEntries, entries...)
			}
			for _, entry := range entries {
				if nativeMCPConflicts[key][entry.key] {
					continue
				}
				duplicate := -1
				for i, existing := range nativeMCPEntries[key] {
					if existing.key == entry.key {
						duplicate = i
						break
					}
				}
				if duplicate >= 0 {
					if bytes.Equal(nativeMCPEntries[key][duplicate].raw, entry.raw) {
						continue
					}
					nativeMCPEntries[key] = slices.DeleteFunc(nativeMCPEntries[key], func(existing ownedJSONEntry) bool { return existing.key == entry.key })
					if nativeMCPConflicts[key] == nil {
						nativeMCPConflicts[key] = make(map[string]bool)
					}
					nativeMCPConflicts[key][entry.key] = true
					nativeMCPIncomplete[key] = true
					blockResource(key, w)
					allDiags = append(allDiags, diag.Diagnostic{Code: "materialize.compile.error", Severity: diag.SeverityError, Target: string(w.target), Resource: resourceName, Message: "selected MCP resources conflict on a shared server key; previous entry was retained", Source: compilepkg.SourceOf(w.skill.Resource)})
					continue
				}
				nativeMCPEntries[key] = append(nativeMCPEntries[key], entry)
			}
		}
		if isHook {
			resourceName := w.skill.Name
			if w.skill.InstalledName != "" {
				resourceName = w.skill.InstalledName
			}
			entries, hookErr := nativeHookEntriesForArtifacts(key, repoRoot, resourceName, plan.sourceDigest, result.Artifacts)
			attachPluginInstanceOwner(entries, w.skill.PluginPackage)
			if hookErr != nil {
				if plan.sourceDigest == "" {
					unknownHookSelection[key] = true
				}
				blockResource(key, w)
				blockPluginInstance(w)
				markResourceIncomplete(key, w, aggregateHandler)
				allDiags = append(allDiags, diag.Diagnostic{
					Code:     "materialize.compile.error",
					Severity: diag.SeverityError,
					Target:   string(w.target),
					Resource: resourceName,
					Message:  fmt.Sprintf("compile native hook for target %s: %v", w.target, hookErr),
					Source:   compilepkg.SourceOf(w.skill.Resource),
				})
				continue
			}
			for _, entry := range entries {
				if pluginPlan := pluginPlanFor(w); pluginPlan != nil {
					if pluginPlan.hooks == nil {
						pluginPlan.hooks = &ownedJSONAggregateWork{}
					}
					pluginPlan.hooks.add(entry)
				} else {
					nativeHookWork(key).add(entry)
				}
			}
			result.Artifacts = nil
		} else if aggregateHandler != nil {
			aggregateState := ownedAggregates
			pluginInstanceKey := ""
			if pluginPlan := pluginPlanFor(w); pluginPlan != nil {
				pluginInstanceKey = pluginPlan.ref.InstanceKey
				if pluginPlan.aggregate.values == nil {
					pluginPlan.aggregate = ownedAggregateState{values: make(map[outputKey]any)}
				}
				pluginPlan.aggregateSet = true
				pluginPlan.handler = aggregateHandler
				aggregateState = pluginPlan.aggregate
			}
			rest, aggregateErr := aggregateHandler.extract(aggregateState, key, result.Artifacts, w.skill.Name, pluginInstanceKey)
			if aggregateErr != nil {
				blockResource(key, w)
				blockPluginInstance(w)
				markResourceIncomplete(key, w, aggregateHandler)
				allDiags = append(allDiags, aggregateHandler.malformedDiagnostic(key, w.skill.Name, aggregateErr))
				continue
			}
			if pluginPlan := pluginPlanFor(w); pluginPlan != nil {
				pluginPlan.aggregate = aggregateState
			}
			result.Artifacts = rest
		}

		if pluginPlan := pluginPlanFor(w); pluginPlan != nil {
			if usesSkillDir(w.target, resourceKind) {
				result.Artifacts = ensureSkillFrontmatter(w.skill, w.target, result.Artifacts)
				installedName := w.skill.InstalledName
				if installedName == "" {
					installedName = w.skill.Name
				}
				rest, compiledSkillArtifacts, installDirRel := splitSkillArtifacts(w.target, installedName, result.Artifacts)
				installDirRel = machineSkillInstallDirRel(w.target, w.machineScope, installDirRel)
				for _, compiled := range compiledSkillArtifacts {
					compiled.Path = path.Join(installDirRel, installedName, compiled.Path)
					pluginPlan.artifacts = append(pluginPlan.artifacts, compiled)
				}
				result.Artifacts = rest
				if skill, ok := w.skill.Resource.(*ir.Skill); ok && len(skill.SupportFiles) > 0 {
					sourceDir := materializationSourcePath(w.skill)
					if info, err := os.Stat(sourceDir); err == nil && !info.IsDir() {
						sourceDir = filepath.Dir(sourceDir)
					}
					if sourceDir == "" {
						pluginBlocked[pluginInstanceIdentity(w.skill, w.target)] = true
						delete(pluginInstances, pluginPlan.ref.InstanceKey)
						blockResource(key, w)
						allDiags = append(allDiags, diag.Diagnostic{
							Code:     "materialize.support-source.missing",
							Severity: diag.SeverityError,
							Target:   string(w.target),
							Resource: installedName,
							Message:  fmt.Sprintf("plugin member %q declares support files but its generation source is unavailable", installedName),
							Source:   compilepkg.SourceOf(w.skill.Resource),
						})
						continue
					}
					link := skillLink{
						name: installedName, sourceDir: sourceDir, declaredSupport: skill.SupportFiles,
						artifacts: compiledSkillArtifacts,
					}
					supportErr := walkPortableSkillSupport(link, func(string) {}, func(root *fsboundary.Dir, sourceChild, childRel string) error {
						data, identity, err := readSupportSourceSnapshot(root, sourceChild)
						if err != nil {
							return err
						}
						pluginPlan.artifacts = append(pluginPlan.artifacts, artifact.Artifact{
							Path:  path.Join(installDirRel, installedName, childRel),
							Bytes: data,
							Mode:  identity.Mode.Perm(),
						})
						return nil
					})
					if supportErr != nil {
						pluginBlocked[pluginInstanceIdentity(w.skill, w.target)] = true
						delete(pluginInstances, pluginPlan.ref.InstanceKey)
						blockResource(key, w)
						allDiags = append(allDiags, diag.Diagnostic{
							Code:     "materialize.support-source.missing",
							Severity: diag.SeverityError,
							Target:   string(w.target),
							Resource: installedName,
							Message:  fmt.Sprintf("read plugin support files for %q: %v", installedName, supportErr),
							Source:   compilepkg.SourceOf(w.skill.Resource),
						})
						continue
					}
				}
				result.Diagnostics = slices.DeleteFunc(result.Diagnostics, func(d diag.Diagnostic) bool {
					return d.Code == diag.CodeSkillSupportFilesDeferred
				})
			}
			pluginPlan.artifacts = append(pluginPlan.artifacts, result.Artifacts...)
			allDiags = append(allDiags, result.Diagnostics...)
			continue
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
				if w.skill.IsMarketplace || w.skill.PluginPackage != nil {
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
			if resourceKind == ir.KindHook {
				if hookSupportPaths[key] == nil {
					hookSupportPaths[key] = make(map[string]bool)
				}
				if hookSupportOwners[key] == nil {
					hookSupportOwners[key] = make(map[string][]hookSupportOwner)
				}
				owner := hookSupportOwner{
					skill: w, resourceName: blockedResourceFor(key, w).Name, sourceDigest: plan.sourceDigest,
					sourceSnapshot: plan.sourceSnapshot, sourceExcludedPeers: append([]string(nil), plan.sourceExcludedPeers...),
				}
				for _, support := range plan.artifacts {
					hookSupportPaths[key][support.Path] = true
					hookSupportOwners[key][support.Path] = append(hookSupportOwners[key][support.Path], owner)
				}
				result.Artifacts = append(result.Artifacts, plan.artifacts...)
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
		blockedResources:     blockedResources,
		nativeHooks:          nativeHooks,
		nativeMCPEntries:     nativeMCPEntries,
		nativeMCPConflicts:   nativeMCPConflicts,
		nativeMCPIncomplete:  nativeMCPIncomplete,
		hookSupportPaths:     hookSupportPaths,
		hookSupportOwners:    hookSupportOwners,
		hookResources:        hookResources,
		selectedHookNames:    selectedHookNames,
		unknownHookSelection: unknownHookSelection,
		mcpResources:         mcpResources,
		artifactMeta:         artifactMeta,
		ownedState:           ownedState,
		plugins:              pluginInstances,
		pluginBlocked:        pluginBlocked,
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

func leafNativeMCPEntries(entries []ownedJSONEntry) []ownedJSONEntry {
	leaf := make([]ownedJSONEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.pluginInstanceKey == "" {
			leaf = append(leaf, entry)
		}
	}
	return leaf
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
	markOwnedAggregateKindIncomplete(handler, state, key, w.skill.Resource.Kind())
}

func markOwnedAggregateKindIncomplete(handler ownedAggregateHandler, state ownedAggregateState, key outputKey, kind ir.ResourceKind) {
	if handler != nil {
		handler.markIncomplete(state, key, kind)
	}
}

func sharedNativeHookAdapterRoot(key outputKey, repoRoot string, destinations []harness.Destination) (string, bool, error) {
	nativeDestination, _, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return "", false, err
	}
	nativeRoot, err := ownership.CanonicalRoot(nativeDestination.Root)
	if err != nil {
		return "", false, err
	}
	for _, destination := range destinations {
		root, err := ownership.CanonicalRoot(destination.Root)
		if err != nil {
			continue
		}
		if root == nativeRoot {
			return nativeRoot, true, nil
		}
	}
	return "", false, nil
}

func captureOwnedAggregateSessionSnapshot(session *ownedAggregateWriteSession, key outputKey, repoRoot string, handler ownedAggregateHandler, hasNativeWork bool) {
	if session == nil {
		return
	}
	if _, err := os.Lstat(session.store.StateFile()); !os.IsNotExist(err) {
		return
	}
	snapshot := &ownedAggregateSessionSnapshot{stateMissing: true, files: make(map[string]bool)}
	rels := make(map[string]bool)
	if hasNativeWork {
		if _, file, err := nativeHookJSONLayout(key, repoRoot); err == nil {
			rels[file.rel] = true
		}
	}
	if handler != nil {
		for _, kind := range []ir.ResourceKind{ir.KindHook, ir.KindMcpServer} {
			for _, rel := range handler.aggregateRels(kind, key.machineScope) {
				if harness.ValidateRelPath(rel) == nil {
					rels[filepath.ToSlash(rel)] = true
				}
			}
		}
	}
	for rel := range rels {
		if _, err := os.Lstat(filepath.Join(session.root, filepath.FromSlash(rel))); err == nil {
			snapshot.files[rel] = true
		}
	}
	session.initial = snapshot
}

func appendMissingOwnedAggregateStateDiagnostics(report *Report, key outputKey, session *ownedAggregateWriteSession) {
	if report == nil || session == nil || session.initial == nil || !session.initial.stateMissing {
		return
	}
	for _, rel := range sortedKeys(session.initial.files) {
		found := false
		for _, diagnostic := range report.Diags {
			if diagnostic.Code == diag.CodeOwnershipStateMissing && diagnostic.Target == string(key.target) && diagnostic.Source.File == rel {
				found = true
				break
			}
		}
		if !found {
			report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, session.root, rel, ownership.ErrStateUninitialized, diag.CodeOwnershipStateMissing))
		}
	}
}

// executeMaterializePlans writes compiled artifacts to disk and syncs skill links.
//
// ctx reaches the owned-aggregate handlers because their per-canonical-root
// critical sections must abandon a contended wait when the run is cancelled.
func executeMaterializePlans(ctx context.Context, compileResult compilePhaseResult, dryRun bool, repoRoot string, modes config.MaterializationModes, supportWriteFaults ...func(string, string) error) *Report {
	var supportWriteFault func(string, string) error
	if len(supportWriteFaults) > 0 {
		supportWriteFault = supportWriteFaults[0]
	}
	report := &Report{
		PerTarget:        make(map[hcc.Target]TargetReport),
		Diags:            compileResult.allDiags,
		BlockedResources: compileResult.blockedResources,
	}
	geminiCoalescence, err := coalesceGeminiNativeAggregates(&compileResult, repoRoot, report)
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

	aggregateState := compileResult.ownedAggregateState()
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
			if geminiCoalescence.isRepo(key) {
				artifacts = withoutNativeHookSupportArtifacts(artifacts, compileResult.hookSupportPaths[key])
			}
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
		boundary := destinationBoundary{machineScope: key.machineScope, repoRoot: repoRoot, homeDir: boundaryHome, linkPublicationHook: compileResult.linkPublicationHook, pruneClassificationHook: compileResult.pruneClassificationHook, pruneEnumerationHook: compileResult.pruneEnumerationHook, manifestPublicationHook: compileResult.manifestPublicationHook}
		directories := configureDestinationBoundary(&boundary, key, outDir, repoRoot, destinations)
		if _, hasNativeHookWork := compileResult.nativeHooks[key]; hasNativeHookWork {
			if hookDestination, _, hookErr := nativeHookJSONLayout(key, repoRoot); hookErr == nil && !slices.Contains(boundary.roots, hookDestination.Root) {
				boundary.roots = append(boundary.roots, hookDestination.Root)
			}
		}
		if !geminiCoalescence.includes(key) {
			var supportDiags []diag.Diagnostic
			var supportBlocked []BlockedResource
			artifacts, supportDiags, supportBlocked = preflightHookSupportDestinations(&compileResult, key, repoRoot, outDir, artifacts, compileResult.skillLinks[key], coalescedCodexPartitions)
			report.Diags = append(report.Diags, supportDiags...)
			for _, resource := range supportBlocked {
				if !slices.Contains(report.BlockedResources, resource) {
					report.BlockedResources = append(report.BlockedResources, resource)
				}
			}
		}
		for _, plan := range materializePlans(key.target, outDir, artifacts, linkedComponents(compileResult.skillLinks[key]), key.machineScope) {
			directories = append(directories, plan.outDir)
		}
		for _, group := range orderedSkillLinkGroups(compileResult.skillLinks[key]) {
			directories = append(directories, filepath.Join(outDir, filepath.FromSlash(group.installDirRel)))
		}
		failedPath, boundaryErr := boundary.validateDirectories(directories)
		if boundaryErr != nil {
			slog.Error("materialize destination refused", "target", key.target, "machine_scope", key.machineScope, "error", boundaryErr)
			report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: boundaryErr.Error()})
			if nativeHookLegacyBoundaryAffected(key, repoRoot, failedPath, compileResult.nativeHooks[key]) {
				report.Diags = append(report.Diags, nativeHookLegacyRefusal(key))
			}
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
		nativeWork, hasNativeWork := compileResult.nativeHooks[key]
		nativeProtectedPaths := nativeHookPortableProtectedPaths(key, repoRoot, outDir)
		binding, hasBinding := composition.bindings[key.target]
		var nativeHandler ownedAggregateHandler
		if hasBinding {
			nativeHandler = binding.handler
		}
		var sharedSession *ownedAggregateWriteSession
		if !dryRun && hasNativeWork && nativeHandler != nil {
			root, shared, err := sharedNativeHookAdapterRoot(key, repoRoot, adapterDestinations)
			if err != nil {
				options := nativeHookReconcileOptionsFor(nativeHandler)
				report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "", fmt.Sprintf("resolve shared native aggregate root: %v", err)))
				continue
			}
			if shared {
				lockReport := newOwnedAggregateLockReporter(key, root)
				store, err := ownership.Open(root, ownership.WithLockObserver(lockReport.observe))
				if err != nil {
					options := nativeHookReconcileOptionsFor(nativeHandler)
					report.Diags = append(report.Diags, ownedAggregateDiag(options.writeCode, key, "", fmt.Sprintf("open shared ownership store for %s: %v", root, err)))
					continue
				}
				sharedSession = &ownedAggregateWriteSession{root: root, store: store, lockReport: lockReport}
			}
		}
		runTarget := func() error {
			var supportPublication nativeHookSupportPublication
			if sharedSession != nil {
				outcomes, recoverErr := sharedSession.store.Recover()
				for _, outcome := range outcomes {
					report.Diags = append(report.Diags, ownedAggregateDiag(diag.CodeOwnershipRecovered, key, outcome.File,
						fmt.Sprintf("recovered an incomplete ownership generation %d for %q (phase %s, action %s)",
							outcome.Generation, ownedAggregateLabel(sharedSession.root, outcome.File), outcome.Phase, outcome.Action)))
				}
				if recoverErr != nil {
					options := nativeHookReconcileOptionsFor(nativeHandler)
					report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, sharedSession.root, "", recoverErr, options.writeCode))
					if info, ok := harness.TargetInfoFor(key.target); ok {
						compileResult.blocked.mark(key, hookBlockedComponents(key.target, info, key.machineScope)...)
					}
					for _, resource := range append(append([]BlockedResource(nil), compileResult.hookResources[key]...), compileResult.mcpResources[key]...) {
						if !slices.Contains(report.BlockedResources, resource) {
							report.BlockedResources = append(report.BlockedResources, resource)
						}
					}
					return nil
				}
				captureOwnedAggregateSessionSnapshot(sharedSession, key, repoRoot, nativeHandler, hasNativeWork)
			}
			if hasNativeWork {
				nativeOptions := nativeHookReconcileOptionsFor(nativeHandler)
				if selected, complete := nativeHookSelectionAuthority(compileResult, geminiCoalescence.supportKeys(key)); complete {
					nativeOptions.selectedHookResources = selected
				}
				if sharedSession != nil {
					nativeOptions.store = sharedSession.store
					nativeOptions.lockReport = sharedSession.lockReport
					nativeOptions.deferLockDiagnostics = true
					nativeOptions.storeSessionHeld = true
				}
				var mcpDiagnostics []diag.Diagnostic
				var supportInstallers []nativeHookSupportInstaller
				supportKeys := geminiCoalescence.supportKeys(key)
				for _, supportKey := range supportKeys {
					supportPaths := compileResult.hookSupportPaths[supportKey]
					if len(supportPaths) == 0 {
						continue
					}
					supportArtifacts := artifacts
					if supportKey != key {
						if supportWork := compileResult.byTarget[supportKey]; supportWork != nil {
							supportArtifacts = supportWork.artifacts
						} else {
							supportArtifacts = nil
						}
					}
					supportOutDir := outDir
					if geminiCoalescence.isOwner(key) && supportKey.machineScope != key.machineScope {
						if roots, ok := harness.MakeInstallRoots()[hcc.TargetGemini]; ok {
							supportOutDir = roots.For(supportKey.machineScope, repoRoot)
						}
					}
					installKey := supportKey
					installOutDir := supportOutDir
					installArtifacts := supportArtifacts
					installPaths := supportPaths
					installOwners := compileResult.hookSupportOwners[installKey]
					installLinks := compileResult.skillLinks[installKey]
					installMode := modes.For(string(installKey.target), installKey.machineScope)
					supportInstallers = append(supportInstallers, nativeHookSupportInstaller{
						key: installKey,
						install: func(ctx context.Context, dryRun bool, authority nativeHookSupportAuthority) (bool, *portableState, error) {
							publication, pending, installErr := executeNativeHookSupportPlan(ctx, installKey, repoRoot, installOutDir, installArtifacts,
								installPaths, installOwners, authority, installLinks, compileResult.artifactMeta,
								installMode, dryRun, coalescedCodexPartitions, supportWriteFault, boundary.manifestPublicationHook, &boundary)
							mergeNativeHookSupportPublication(&supportPublication, publication)
							state := publication.manifestStates[installKey]
							if installErr != nil {
								return pending, state, installErr
							}
							if installKey == key {
								artifacts = withoutNativeHookSupportArtifacts(artifacts, installPaths)
							}
							return pending, state, nil
						},
					})
				}
				nativeOptions.nativeHookFinalize = nativeHookActivationFinalizer(key, repoRoot, leafNativeMCPEntries(compileResult.nativeMCPEntries[key]), compileResult.nativeMCPIncomplete[key], func() {
					if compileResult.nativeMCPIncomplete == nil {
						compileResult.nativeMCPIncomplete = make(map[outputKey]bool)
					}
					compileResult.nativeMCPIncomplete[key] = true
				}, &mcpDiagnostics, supportInstallers...)
				diagnosticStart := len(report.Diags)
				report = reconcileNativeHookJSON(ctx, report, key, repoRoot, nativeWork, nativeOptions, dryRun)
				for _, resource := range compileResult.mcpResources[key] {
					if len(mcpDiagnostics) > 0 && !slices.Contains(report.BlockedResources, resource) {
						report.BlockedResources = append(report.BlockedResources, resource)
					}
				}
				nativeRefused := false
				for _, diagnostic := range report.Diags[diagnosticStart:] {
					if !diagnostic.IsError() || diagnosticInList(diagnostic, mcpDiagnostics) {
						continue
					}
					nativeRefused = true
					break
				}
				if nativeRefused {
					if info, ok := harness.TargetInfoFor(key.target); ok {
						compileResult.blocked.mark(key, hookBlockedComponents(key.target, info, key.machineScope)...)
					}
					for _, resource := range append(append([]BlockedResource(nil), compileResult.hookResources[key]...), compileResult.mcpResources[key]...) {
						if !slices.Contains(report.BlockedResources, resource) {
							report.BlockedResources = append(report.BlockedResources, resource)
						}
					}
					remaining := make([]artifact.Artifact, 0, len(artifacts))
					for _, a := range artifacts {
						if !compileResult.hookSupportPaths[key][a.Path] {
							remaining = append(remaining, a)
						}
					}
					artifacts = remaining
				}
			}
			if geminiCoalescence.isRepo(key) {
				artifacts = withoutNativeHookSupportArtifacts(artifacts, compileResult.hookSupportPaths[key])
			}
			if !geminiCoalescence.isRepo(key) && !nativeHookCanSkipGenericAggregateRoute(key, repoRoot) {
				artifacts = materializeNativeAggregates(ctx, report, key, repoRoot, artifacts, compileResult.blocked.componentsFor(key), dryRun, &boundary)
			}
			if tw != nil && tw.nativeOnly {
				slog.Debug("native-only aggregate work complete", "target", key.target, "machine_scope", key.machineScope)
				return nil
			}
			report = executeTargetPlans(report, key, outDir, destinations, artifacts, compileResult.skillLinks, compileResult.artifactMeta, compileResult.blocked.componentsFor(key), dryRun, mode, repoRoot, coalescedCodexPartitions, supportPublication, geminiCoalescence.supportPathsForKey(key, compileResult.hookSupportPaths), supportWriteFault, &boundary, geminiCoalescence, nativeProtectedPaths)
			if hasBinding {
				report = binding.handler.reconcile(ctx, report, aggregateState, key, outDir, adapterDestinations, dryRun)
			}
			appendMissingOwnedAggregateStateDiagnostics(report, key, sharedSession)
			return nil
		}
		if sharedSession == nil {
			_ = runTarget()
			continue
		}
		options := nativeHookReconcileOptionsFor(nativeHandler)
		sessionErr := withOwnedAggregateWriteSession(ctx, &aggregateState, *sharedSession, "materialize-native-adapter-root", runTarget)
		report.Diags = append(report.Diags, sharedSession.lockReport.diags()...)
		if sessionErr != nil {
			report.Diags = append(report.Diags, ownedAggregateLockSessionDiag(key, sharedSession.root, sessionErr, options.writeCode))
			if info, ok := harness.TargetInfoFor(key.target); ok {
				compileResult.blocked.mark(key, hookBlockedComponents(key.target, info, key.machineScope)...)
			}
			for _, resource := range append(append([]BlockedResource(nil), compileResult.hookResources[key]...), compileResult.mcpResources[key]...) {
				if !slices.Contains(report.BlockedResources, resource) {
					report.BlockedResources = append(report.BlockedResources, resource)
				}
			}

		}
	}

	return report
}

func materializePlanCollisionMetadata(key outputKey, plan materializePlan, artifacts []artifact.Artifact, artifactMeta map[outputKey]map[string]skillLink) map[string]skillLink {
	metadata := make(map[string]skillLink, len(artifacts))
	for _, artifact := range artifacts {
		if meta, ok := artifactMeta[key][artifact.Path]; ok {
			metadata[artifact.Path] = meta
			continue
		}
		if plan.reportPrefix != "" {
			fullPath := filepath.ToSlash(filepath.Join(plan.reportPrefix, artifact.Path))
			if meta, ok := artifactMeta[key][fullPath]; ok {
				metadata[artifact.Path] = meta
			}
		}
	}
	return metadata
}

func hookSupportArtifactKey(supportPaths map[string]bool, prefix, artifactPath string) (string, bool) {
	artifactPath = filepath.ToSlash(artifactPath)
	if supportPaths[artifactPath] {
		return artifactPath, true
	}
	if prefix != "" {
		fullPath := path.Join(filepath.ToSlash(prefix), artifactPath)
		if supportPaths[fullPath] {
			return fullPath, true
		}
	}
	return "", false
}

func isHookSupportArtifact(supportPaths map[string]bool, prefix, artifactPath string) bool {
	_, ok := hookSupportArtifactKey(supportPaths, prefix, artifactPath)
	return ok
}

func withoutNativeHookSupportArtifacts(artifacts []artifact.Artifact, supportPaths map[string]bool) []artifact.Artifact {
	remaining := make([]artifact.Artifact, 0, len(artifacts))
	for _, candidate := range artifacts {
		if !supportPaths[filepath.ToSlash(candidate.Path)] {
			remaining = append(remaining, candidate)
		}
	}
	return remaining
}

func loadNativeHookSupportState(repoRoot string, machineScope bool) (*portableState, error) {
	manifestPath, err := nativeHookPortableManifestPath(repoRoot, machineScope)
	if err != nil {
		return nil, portableManifestError(manifestPath, err)
	}
	state, err := loadPortableState(repoRoot, machineScope)
	if err != nil {
		return nil, portableManifestError(manifestPath, err)
	}
	data, generation, mode, err := dependencytxn.ReadSnapshot(state.path)
	if err != nil {
		return nil, portableManifestError(state.path, err)
	}
	if err := validatePortableManifestSnapshot(state, data, generation, mode); err != nil {
		return nil, portableManifestError(state.path, err)
	}
	return state, nil
}

func nativeHookSupportPublicationPlan(publication *nativeHookSupportPublication, plan materializePlan) *nativeHookSupportPlanPublication {
	if publication.plans == nil {
		publication.plans = make(map[nativeHookSupportPlanKey]*nativeHookSupportPlanPublication)
	}
	key := nativeHookSupportPlanKeyFor(plan)
	if published := publication.plans[key]; published != nil {
		return published
	}
	published := &nativeHookSupportPlanPublication{
		supportTargets: make(map[string]string),
		manifestPaths:  make(map[string]bool),
		writeReport:    &hcc.WriteReport{OutDir: plan.outDir},
	}
	publication.plans[key] = published
	return published
}

func mergeNativeHookSupportWriteReport(dst, src *hcc.WriteReport) *hcc.WriteReport {
	if src == nil {
		return dst
	}
	if dst == nil {
		dst = &hcc.WriteReport{OutDir: src.OutDir}
	}
	dst.Written = append(dst.Written, src.Written...)
	dst.Skipped = append(dst.Skipped, src.Skipped...)
	dst.Removed = append(dst.Removed, src.Removed...)
	sort.Strings(dst.Written)
	sort.Strings(dst.Skipped)
	sort.Strings(dst.Removed)
	return dst
}

func mergeNativeHookSupportPublication(dst *nativeHookSupportPublication, src nativeHookSupportPublication) {
	if dst.plans == nil {
		dst.plans = make(map[nativeHookSupportPlanKey]*nativeHookSupportPlanPublication)
	}
	if dst.manifestStates == nil {
		dst.manifestStates = make(map[outputKey]*portableState)
	}
	for key, state := range src.manifestStates {
		dst.manifestStates[key] = state
	}
	for key, sourcePlan := range src.plans {
		targetPlan := dst.plans[key]
		if targetPlan == nil {
			targetPlan = &nativeHookSupportPlanPublication{
				supportTargets: make(map[string]string),
				manifestPaths:  make(map[string]bool),
			}
			dst.plans[key] = targetPlan
		}
		for path, cacheTarget := range sourcePlan.supportTargets {
			targetPlan.supportTargets[path] = cacheTarget
		}
		for path := range sourcePlan.manifestPaths {
			targetPlan.manifestPaths[path] = true
		}
		targetPlan.writeReport = mergeNativeHookSupportWriteReport(targetPlan.writeReport, sourcePlan.writeReport)
		targetPlan.diagnostics = append(targetPlan.diagnostics, sourcePlan.diagnostics...)
	}
}

func sameNativeHookSupportDesiredFile(left, right dependencytxn.DesiredFile) bool {
	sameNamespace := left.Namespace == nil && right.Namespace == nil ||
		left.Namespace != nil && right.Namespace != nil && *left.Namespace == *right.Namespace
	return sameNamespace && left.Expected == right.Expected && left.ExpectedMode == right.ExpectedMode &&
		left.ExpectedLinkTarget == right.ExpectedLinkTarget && left.Mode == right.Mode && left.Delete == right.Delete &&
		bytes.Equal(left.Bytes, right.Bytes)
}

func dedupeNativeHookSupportParticipants(files []dependencytxn.DesiredFile) ([]dependencytxn.DesiredFile, error) {
	seen := make(map[string]dependencytxn.DesiredFile, len(files))
	participants := make([]dependencytxn.DesiredFile, 0, len(files))
	for _, file := range files {
		key := filepath.Clean(file.Path)
		if existing, ok := seen[key]; ok {
			if !sameNativeHookSupportDesiredFile(existing, file) {
				return nil, fmt.Errorf("%w: conflicting hook support transaction preconditions for %s", dependencytxn.ErrGenerationConflict, key)
			}
			continue
		}
		seen[key] = file
		participants = append(participants, file)
	}
	return participants, nil
}

func syncNativeRepositoryHookSupportCopies(ctx context.Context, outDir string, artifacts []artifact.Artifact, cacheDir string, dryRun bool, collisionMeta map[string]skillLink, state *portableState, target hcc.Target, reportPrefix string, desired map[string]bool, owners map[string][]hookSupportOwner, authority nativeHookSupportAuthority) (writeReport *hcc.WriteReport, writeDiags []diag.Diagnostic, retErr error) {
	report := &hcc.WriteReport{OutDir: outDir}
	var manifestParent *fsboundary.Dir
	var manifestDestination *fsboundary.Destination
	var manifestNamespace *dependencytxn.NamespaceExpectation
	defer func() {
		if manifestDestination != nil {
			if err := manifestDestination.Close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close captured manifest destination: %w", err))
			}
		}
		if manifestParent != nil {
			if err := manifestParent.Close(); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close captured manifest parent: %w", err))
			}
		}
	}()
	conflict := func(a artifact.Artifact, relative string, pathOwners []hookSupportOwner, cause error) (*hcc.WriteReport, []diag.Diagnostic, error) {
		resource := ""
		if meta, ok := collisionMeta[a.Path]; ok {
			resource = meta.resourceName
		}
		if resource == "" && len(pathOwners) > 0 {
			resource = pathOwners[0].resourceName
		}
		message := "hook support destination could not be verified"
		if resource != "" {
			message = fmt.Sprintf("hook support for %q could not be verified", resource)
		}
		if cause == nil {
			cause = errors.New(message)
		} else {
			cause = fmt.Errorf("%s: %w", message, cause)
		}
		return &hcc.WriteReport{OutDir: outDir}, nil, diag.NewCodedError(diag.CodeMaterializeHookSupportConflict, diag.SourceRef{File: relative}, cause)
	}
	if state == nil {
		return report, nil, portableManifestError("", errors.New("native hook support has no portable state"))
	}
	manifestConflict := func(cause error) (*hcc.WriteReport, []diag.Diagnostic, error) {
		if len(artifacts) == 0 {
			return report, nil, portableManifestError(state.path, cause)
		}
		a := artifacts[0]
		relative := filepath.ToSlash(path.Join(reportPrefix, a.Path))
		return conflict(a, relative, owners[a.Path], portableManifestError(state.path, cause))
	}
	manifestBytes, manifestGeneration, manifestMode, err := dependencytxn.ReadSnapshot(state.path)
	if err != nil {
		return report, nil, portableManifestError(state.path, err)
	}
	if err := validatePortableManifestSnapshot(state, manifestBytes, manifestGeneration, manifestMode); err != nil {
		return report, nil, portableManifestError(state.path, err)
	}
	nextState := *state
	nextState.entries = make(map[string]portableManifestEntry, len(state.entries))
	for key, entry := range state.entries {
		nextState.entries[key] = entry
	}
	participants := make([]dependencytxn.DesiredFile, 0, len(artifacts)+1)
	var manifestParticipant *dependencytxn.DesiredFile
	writeDirs := make(map[string]bool)
	changed := false
	for _, a := range artifacts {
		if err := a.Validate(); err != nil {
			return report, nil, err
		}
		relative := filepath.ToSlash(path.Join(reportPrefix, a.Path))
		desired[relative] = true
		pathOwners := owners[a.Path]
		if len(pathOwners) == 0 {
			return conflict(a, relative, pathOwners, errors.New("captured hook support has no selected source owner"))
		}
		for _, owner := range pathOwners {
			if owner.resourceName == "" {
				return conflict(a, relative, pathOwners, errors.New("captured hook support has no resource identity"))
			}
		}
		destination, err := portableOutputPath(outDir, a.Path)
		if err != nil {
			return conflict(a, relative, pathOwners, err)
		}
		mode := portableArtifactMode(a.Mode)
		digest := portableArtifactDigest(a)
		info, err := os.Lstat(destination)
		if os.IsNotExist(err) {
			participants = append(participants, dependencytxn.DesiredFile{
				Path: destination, Expected: dependencytxn.Generation{}, Bytes: append([]byte(nil), a.Bytes...), Mode: mode,
			})
			nextState.add(target, relative, digest)
			writeDirs[filepath.ToSlash(filepath.Dir(a.Path))] = true
			report.Written = append(report.Written, a.Path)
			changed = true
			continue
		}
		if err != nil {
			return conflict(a, relative, pathOwners, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, expected, err := dependencytxn.ReadSymlinkSnapshot(destination)
			if err != nil {
				return conflict(a, relative, pathOwners, err)
			}
			expectedCacheFile := filepath.Join(cacheDir, filepath.FromSlash(a.Path))
			if linkTarget != expectedCacheFile {
				return conflict(a, relative, pathOwners, errors.New("hook support link does not point to its authorized cache file"))
			}
			for _, owner := range pathOwners {
				if owner.sourceDigest == "" || !authority.cleanSources[hookSupportOwnerID(owner.resourceName, owner.sourceDigest)] {
					return conflict(a, relative, pathOwners, errors.New("hook support link has no complete clean native source proof"))
				}
			}
			if _, err := validatePortableOutputDir(cacheDir, filepath.ToSlash(filepath.Dir(a.Path))); err != nil {
				return conflict(a, relative, pathOwners, err)
			}
			cacheBytes, cacheGeneration, cacheMode, err := dependencytxn.ReadSnapshot(expectedCacheFile)
			if err != nil || !cacheGeneration.Exists || !bytes.Equal(cacheBytes, a.Bytes) || cacheMode != mode {
				if err == nil {
					err = errors.New("authorized hook support cache bytes or permissions changed")
				}
				return conflict(a, relative, pathOwners, err)
			}
			participants = append(participants,
				dependencytxn.DesiredFile{
					Path: expectedCacheFile, Expected: cacheGeneration, ExpectedMode: cacheMode,
					Bytes: append([]byte(nil), cacheBytes...), Mode: cacheMode,
				},
				dependencytxn.DesiredFile{
					Path: destination, Expected: expected, ExpectedMode: 0, ExpectedLinkTarget: linkTarget,
					Bytes: append([]byte(nil), a.Bytes...), Mode: mode,
				},
			)
			nextState.add(target, relative, digest)
			report.Written = append(report.Written, a.Path)
			changed = true
			continue
		}
		if !info.Mode().IsRegular() {
			return conflict(a, relative, pathOwners, errors.New("hook support destination is not a regular file"))
		}
		currentBytes, expected, currentMode, err := dependencytxn.ReadSnapshot(destination)
		if err != nil {
			return conflict(a, relative, pathOwners, err)
		}
		currentDigest := portableArtifactDigest(artifact.Artifact{Bytes: currentBytes, Mode: currentMode})
		entry, owned := state.entry(target, relative)
		if entry.PluginInstance != "" {
			owned = false
		}
		if !owned {
			if currentDigest != digest || !bytes.Equal(currentBytes, a.Bytes) || currentMode != mode {
				return conflict(a, relative, pathOwners, errors.New("foreign regular hook support file differs from captured bytes"))
			}
			participants = append(participants, dependencytxn.DesiredFile{
				Path: destination, Expected: expected, ExpectedMode: currentMode,
				Bytes: currentBytes, Mode: currentMode,
			})
			report.Skipped = append(report.Skipped, a.Path)
			continue
		}
		switch portableOwnershipDecision(entry, currentDigest, digest) {
		case portableOwnershipAdopt:
			nextState.add(target, relative, digest)
			participants = append(participants, dependencytxn.DesiredFile{
				Path: destination, Expected: expected, ExpectedMode: currentMode,
				Bytes: currentBytes, Mode: currentMode,
			})
			report.Skipped = append(report.Skipped, a.Path)
		case portableOwnershipReplace:
			participants = append(participants, dependencytxn.DesiredFile{
				Path: destination, Expected: expected, ExpectedMode: currentMode,
				Bytes: append([]byte(nil), a.Bytes...), Mode: mode,
			})
			nextState.add(target, relative, digest)
			report.Written = append(report.Written, a.Path)
			changed = true
		default:
			return conflict(a, relative, pathOwners, errors.New("owned hook support file changed outside materialization"))
		}
	}
	manifestNext, hasEntries, err := nextState.serialized()
	if err != nil {
		return report, nil, err
	}
	manifestModeNext := manifestMode
	if !manifestGeneration.Exists {
		manifestModeNext = 0o600
	}
	manifestChanged := hasEntries && (!manifestGeneration.Exists || !bytes.Equal(manifestNext, manifestBytes))
	if manifestChanged {
		participant := dependencytxn.DesiredFile{
			Path: state.path, Expected: manifestGeneration, ExpectedMode: manifestMode, Namespace: manifestNamespace,
			Bytes: manifestNext, Mode: manifestModeNext,
		}
		participants = append(participants, participant)
		changed = true
	} else if changed && manifestGeneration.Exists {
		participants = append(participants, dependencytxn.DesiredFile{
			Path: state.path, Expected: manifestGeneration, ExpectedMode: manifestMode, Namespace: manifestNamespace,
			Bytes: manifestBytes, Mode: manifestMode,
		})
	}
	participants = append(participants, authority.assertions...)
	participants, err = dedupeNativeHookSupportParticipants(participants)
	if err != nil {
		if len(artifacts) == 0 {
			return report, nil, err
		}
		a := artifacts[0]
		return conflict(a, filepath.ToSlash(path.Join(reportPrefix, a.Path)), owners[a.Path], err)
	}
	writeFault, hasWriteFault := ctx.Value(nativeRepositorySupportWriteFaultKey{}).(func(string, string) error)
	if hasWriteFault {
		for _, a := range artifacts {
			destination, err := portableOutputPath(outDir, a.Path)
			if err != nil {
				return &hcc.WriteReport{OutDir: outDir}, nil, fmt.Errorf("resolve hook support output: %w", err)
			}
			if err := writeFault("output", destination); err != nil {
				return &hcc.WriteReport{OutDir: outDir}, nil, fmt.Errorf("prepare hook support output: %w", err)
			}
		}
		currentManifest, currentGeneration, currentMode, err := dependencytxn.ReadSnapshot(state.path)
		if err != nil {
			return &hcc.WriteReport{OutDir: outDir}, nil, portableManifestError(state.path, err)
		}
		if err := validatePortableManifestSnapshot(state, currentManifest, currentGeneration, currentMode); err != nil {
			return &hcc.WriteReport{OutDir: outDir}, nil, portableManifestError(state.path, err)
		}
	}
	if !changed {
		if hasWriteFault {
			for _, a := range artifacts {
				destination, err := portableOutputPath(outDir, a.Path)
				if err != nil {
					return conflict(a, filepath.ToSlash(path.Join(reportPrefix, a.Path)), owners[a.Path], err)
				}
				for _, participant := range participants {
					if participant.Path != destination {
						continue
					}
					current, generation, mode, err := dependencytxn.ReadSnapshot(destination)
					if err != nil || generation != participant.Expected || mode != participant.ExpectedMode || !bytes.Equal(current, participant.Bytes) {
						if err == nil {
							err = dependencytxn.ErrGenerationConflict
						}
						return conflict(a, filepath.ToSlash(path.Join(reportPrefix, a.Path)), owners[a.Path], err)
					}
				}
			}
		}
		sort.Strings(report.Written)
		sort.Strings(report.Skipped)
		return report, nil, nil
	}
	if dryRun {
		sort.Strings(report.Written)
		sort.Strings(report.Skipped)
		return report, nil, nil
	}
	validatedSources := make(map[hookSourceSnapshotKey]bool)
	for _, a := range artifacts {
		for _, owner := range owners[a.Path] {
			if owner.sourceSnapshot == nil {
				continue
			}
			snapshotKey := hookSourceSnapshotKey{snapshot: owner.sourceSnapshot, excludedPeers: strings.Join(owner.sourceExcludedPeers, "\x00")}
			if validatedSources[snapshotKey] {
				continue
			}
			validatedSources[snapshotKey] = true
			if err := revalidateHookSourceSnapshot(owner.sourceSnapshot, owner.sourceExcludedPeers); err != nil {
				return conflict(a, filepath.ToSlash(path.Join(reportPrefix, a.Path)), owners[a.Path], err)
			}
		}
	}
	for rel := range writeDirs {
		if _, err := ensurePortableOutputDir(outDir, rel); err != nil {
			return report, nil, fmt.Errorf("prepare repository hook support directory: %w", err)
		}
	}
	if manifestChanged && !manifestGeneration.Exists {
		manifestRel, err := filepath.Rel(state.repoRoot, state.path)
		if err != nil || manifestRel == "." || manifestRel == ".." || strings.HasPrefix(manifestRel, ".."+string(filepath.Separator)) || filepath.IsAbs(manifestRel) {
			return report, nil, portableManifestError(state.path, errors.New("portable manifest path is outside its root"))
		}
		manifestParent, err = state.captureManifestParent(true)
		if err != nil {
			return manifestConflict(err)
		}
	}
	if manifestGeneration.Exists || manifestChanged {
		currentManifest, currentGeneration, currentMode, err := dependencytxn.ReadSnapshot(state.path)
		if err != nil {
			return manifestConflict(err)
		}
		if err := validatePortableManifestSnapshot(state, currentManifest, currentGeneration, currentMode); err != nil {
			return manifestConflict(err)
		}
		if manifestParent == nil {
			manifestParent, err = state.captureManifestParent(false)
			if err != nil {
				return manifestConflict(err)
			}
		}
		manifestDestination, err = state.captureManifestDestination(manifestParent)
		if err != nil {
			return manifestConflict(err)
		}
		if err := state.verifyManifestParent(manifestParent); err != nil {
			return manifestConflict(err)
		}
		manifestNamespace, err = state.manifestNamespaceExpectation(manifestParent)
		if err != nil {
			return manifestConflict(err)
		}
	}
	if manifestChanged || changed && manifestGeneration.Exists {
		found := false
		for i := range participants {
			if participants[i].Path != state.path {
				continue
			}
			participants[i].Namespace = manifestNamespace
			if manifestChanged {
				manifestParticipant = &participants[i]
			}
			found = true
			break
		}
		if !found || manifestNamespace == nil || manifestChanged && manifestParticipant == nil {
			return manifestConflict(fmt.Errorf("%w: native support plan has no captured manifest namespace", dependencytxn.ErrGenerationConflict))
		}
	}
	if manifestChanged && state.publicationHook != nil {
		state.publicationHook(state.path)
	}
	if manifestGeneration.Exists || manifestChanged {
		currentManifest, currentGeneration, currentMode, err := dependencytxn.ReadSnapshot(state.path)
		if err != nil {
			return manifestConflict(err)
		}
		if err := validatePortableManifestSnapshot(state, currentManifest, currentGeneration, currentMode); err != nil {
			return manifestConflict(err)
		}
		if manifestParent != nil {
			if err := state.verifyManifestParent(manifestParent); err != nil {
				return manifestConflict(err)
			}
			currentDestination, err := state.captureManifestDestination(manifestParent)
			if err != nil {
				return manifestConflict(err)
			}
			if err := currentDestination.Close(); err != nil {
				return manifestConflict(err)
			}
		}
	}
	var commitOptions []dependencytxn.Option
	if fault, ok := ctx.Value(nativeRepositorySupportCommitFaultKey{}).(dependencytxn.FaultHook); ok && fault != nil {
		commitOptions = append(commitOptions, dependencytxn.WithFaultHook(fault))
	}
	if _, err := commitNativeHookFiles(ctx, participants, commitOptions...); err != nil {
		if errors.Is(err, dependencytxn.ErrGenerationConflict) || errors.Is(err, dependencytxn.ErrRecoveryBlocked) || errors.Is(err, dependencytxn.ErrUnsafePath) {
			for _, a := range artifacts {
				return conflict(a, filepath.ToSlash(path.Join(reportPrefix, a.Path)), owners[a.Path], err)
			}
		}
		return report, nil, fmt.Errorf("commit repository hook support copies: %w", err)
	}
	if manifestChanged {
		if manifestParticipant == nil {
			return report, nil, portableManifestError(state.path, fmt.Errorf("%w: committed support plan has no manifest receipt", dependencytxn.ErrGenerationConflict))
		}
		if err := refreshNativeHookManifestCommit(nativeHookManifestCommit{state: state, participant: *manifestParticipant}); err != nil {
			return report, nil, portableManifestError(state.path, err)
		}
	}
	sort.Strings(report.Written)
	sort.Strings(report.Skipped)
	return report, nil, nil
}

func executeNativeHookSupportPlan(ctx context.Context, key outputKey, repoRoot, outDir string, artifacts []artifact.Artifact, supportPaths map[string]bool, owners map[string][]hookSupportOwner, authority nativeHookSupportAuthority, linkedSkills map[string]*skillLinkGroup, artifactMeta map[outputKey]map[string]skillLink, mode config.MaterializationMode, dryRun bool, coalescedCodexPartitions map[string]bool, beforeSupportWrite func(string, string) error, manifestPublicationHook func(string), boundary *destinationBoundary) (nativeHookSupportPublication, bool, error) {
	publication := nativeHookSupportPublication{plans: make(map[nativeHookSupportPlanKey]*nativeHookSupportPlanPublication)}
	if len(supportPaths) == 0 {
		return publication, false, nil
	}
	state, err := loadNativeHookSupportState(repoRoot, key.machineScope)
	if err != nil {
		return publication, false, err
	}
	if publication.manifestStates == nil {
		publication.manifestStates = make(map[outputKey]*portableState)
	}
	publication.manifestStates[key] = state
	state.publicationHook = manifestPublicationHook
	repositoryCopies := !key.machineScope
	effectiveMode := mode
	if repositoryCopies {
		effectiveMode = config.MaterializationModeFile
	}
	plans := materializePlans(key.target, outDir, artifacts, linkedComponents(linkedSkills), key.machineScope)
	seen := make(map[string]bool, len(supportPaths))
	var installedSupport []nativeHookInstalledSupport
	pending := false
	for _, plan := range plans {
		var supportArtifacts []artifact.Artifact
		artifactOwners := make(map[string][]hookSupportOwner)
		for _, candidate := range plan.artifacts {
			if supportKey, ok := hookSupportArtifactKey(supportPaths, plan.reportPrefix, candidate.Path); ok {
				seen[supportKey] = true
				supportArtifacts = append(supportArtifacts, candidate)
				artifactOwners[candidate.Path] = append(artifactOwners[candidate.Path], owners[supportKey]...)
			}
		}
		if len(supportArtifacts) == 0 {
			continue
		}
		regularArtifacts, mergeArtifacts := splitMergeArtifacts(supportArtifacts)
		if len(mergeArtifacts) > 0 {
			return publication, pending, fmt.Errorf("captured hook support contains an aggregate artifact")
		}
		planRoot := outDir
		if plan.root != "" {
			planRoot = plan.root
		}
		rel, err := filepath.Rel(planRoot, plan.outDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return publication, pending, fmt.Errorf("hook support output directory is outside its root")
		}
		validate := validatePortableOutputDir
		if effectiveMode == config.MaterializationModeFile && !dryRun && !repositoryCopies {
			validate = ensurePortableOutputDir
		}
		if _, err := validate(planRoot, filepath.ToSlash(rel)); err != nil {
			return publication, pending, fmt.Errorf("validate hook support output directory: %w", err)
		}
		if effectiveMode != config.MaterializationModeFile && !dryRun {
			if err := os.MkdirAll(plan.outDir, 0o755); err != nil {
				return publication, pending, fmt.Errorf("create hook support output directory: %w", err)
			}
		}
		cacheDir := cachedPlanDirectoryForOutput(key, outDir, plan, coalescedCodexPartitions)
		cacheChanged := false
		if !repositoryCopies {
			cacheReport, err := materializeArtifactLinkCache(cacheDir, regularArtifacts, dryRun, false)
			if err != nil {
				return publication, pending, fmt.Errorf("prepare hook support cache: %w", err)
			}
			cacheChanged = cacheReport != nil && len(cacheReport.Written) > 0
		}
		collisionMeta := materializePlanCollisionMetadata(key, plan, regularArtifacts, artifactMeta)
		if !dryRun && beforeSupportWrite != nil {
			if repositoryCopies {
				ctx = context.WithValue(ctx, nativeRepositorySupportWriteFaultKey{}, beforeSupportWrite)
			} else {
				for _, candidate := range regularArtifacts {
					destination, err := portableOutputPath(plan.outDir, candidate.Path)
					if err != nil {
						return publication, pending, fmt.Errorf("resolve hook support output: %w", err)
					}
					if err := beforeSupportWrite("output", destination); err != nil {
						return publication, pending, fmt.Errorf("prepare hook support output: %w", err)
					}
				}
			}
		}
		desired := make(map[string]bool, len(regularArtifacts))
		var writeReport *hcc.WriteReport
		var writeDiags []diag.Diagnostic
		if repositoryCopies {
			writeReport, writeDiags, err = syncNativeRepositoryHookSupportCopies(ctx, plan.outDir, regularArtifacts, cacheDir, dryRun, collisionMeta, state, key.target, plan.reportPrefix, desired, artifactOwners, authority)
		} else if mode == config.MaterializationModeFile {
			writeReport, writeDiags, err = syncPortableArtifactFiles(plan.outDir, regularArtifacts, cacheDir, dryRun, false, collisionMeta, state, key.target, plan.reportPrefix, desired, boundary)
		} else {
			writeReport, writeDiags, err = syncArtifactLinks(plan.outDir, regularArtifacts, cacheDir, dryRun, false, collisionMeta, portableSyncContext{state: state, target: key.target, reportPrefix: plan.reportPrefix, desired: desired, boundary: boundary})
		}
		if writeReport == nil {
			writeReport = &hcc.WriteReport{OutDir: plan.outDir}
		}
		published := nativeHookSupportPublicationPlan(&publication, plan)
		published.writeReport = mergeNativeHookSupportWriteReport(published.writeReport, writeReport)
		for i := range writeDiags {
			writeDiags[i].Target = string(key.target)
		}
		published.diagnostics = append(published.diagnostics, writeDiags...)
		if err != nil {
			return publication, pending, fmt.Errorf("install hook support files: %w", err)
		}
		for _, candidate := range regularArtifacts {
			outputPath, err := portableOutputPath(plan.outDir, candidate.Path)
			if err != nil {
				return publication, pending, fmt.Errorf("resolve installed hook support: %w", err)
			}
			if !dryRun {
				installed := nativeHookInstalledSupport{
					outputRoot:    plan.outDir,
					relativePath:  filepath.ToSlash(candidate.Path),
					outputPath:    outputPath,
					mode:          effectiveMode,
					capturedBytes: append([]byte(nil), candidate.Bytes...),
					capturedMode:  portableArtifactMode(candidate.Mode),
				}
				if effectiveMode == config.MaterializationModeSymlink {
					installed.cacheRoot = cacheDir
					installed.expectedSymlinkTarget = filepath.Join(cacheDir, filepath.FromSlash(candidate.Path))
				}
				if err := verifyNativeHookSupportOutput(installed); err != nil {
					return publication, pending, fmt.Errorf("verify installed hook support: %w", err)
				}
				installedSupport = append(installedSupport, installed)
			}
			manifestPath := filepath.ToSlash(path.Join(plan.reportPrefix, candidate.Path))
			published.manifestPaths[manifestPath] = true
			if effectiveMode == config.MaterializationModeSymlink {
				published.supportTargets[filepath.ToSlash(candidate.Path)] = filepath.Join(cacheDir, filepath.FromSlash(candidate.Path))
			}
		}
		pending = pending || cacheChanged || len(writeReport.Written) > 0 || len(writeReport.Removed) > 0
	}
	for supportPath := range supportPaths {
		if !seen[supportPath] {
			return publication, pending, fmt.Errorf("captured hook support could not be routed to a target output plan")
		}
	}
	if !dryRun {
		data, generation, mode, err := dependencytxn.ReadSnapshot(state.path)
		if err != nil {
			return publication, pending, portableManifestError(state.path, err)
		}
		if err := validatePortableManifestSnapshot(state, data, generation, mode); err != nil {
			return publication, pending, portableManifestError(state.path, err)
		}
		if !repositoryCopies {
			if err := state.save(false); err != nil {
				return publication, pending, portableManifestError(state.path, err)
			}
		}
		if beforeSupportWrite != nil {
			for _, output := range installedSupport {
				if err := beforeSupportWrite("before-native-commit", output.outputPath); err != nil {
					return publication, pending, fmt.Errorf("verify hook support before Native commit: %w", err)
				}
			}
		}
		for _, output := range installedSupport {
			if err := verifyNativeHookSupportOutput(output); err != nil {
				return publication, pending, fmt.Errorf("recheck hook support before Native commit: %w", err)
			}
		}
	}
	return publication, pending, nil
}

// executeTargetPlans writes artifact plans and skill links for a single target.
func executeTargetPlans(report *Report, key outputKey, outDir string, destinations []harness.Destination, artifacts []artifact.Artifact, skillLinks map[outputKey]map[string]*skillLinkGroup, artifactMeta map[outputKey]map[string]skillLink, blocked map[string]bool, dryRun bool, mode config.MaterializationMode, repoRoot string, coalescedCodexPartitions map[string]bool, supportPublication nativeHookSupportPublication, hookSupportPaths map[string]bool, beforeSupportWrite func(string, string) error, boundary *destinationBoundary, geminiCoalescence geminiAggregateCoalescence, protectedPathSets ...map[string]bool) *Report {
	protectedPaths := map[string]bool{}
	if len(protectedPathSets) > 0 && protectedPathSets[0] != nil {
		protectedPaths = protectedPathSets[0]
	}
	state := supportPublication.manifestStates[key]
	var stateErr error
	if state == nil {
		state, stateErr = loadPortableState(repoRoot, key.machineScope)
	}
	if stateErr != nil {
		slog.Error("materialize manifest load failed", "target", key.target, "machine_scope", key.machineScope, "error", stateErr)
		report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: stateErr.Error()})
		return report
	}
	state.publicationHook = boundary.manifestPublicationHook
	fileDesired := make(map[string]bool, len(hookSupportPaths))
	for supportPath := range hookSupportPaths {
		fileDesired[filepath.ToSlash(supportPath)] = true
	}
	// Track which plan output directories had merge artifacts, so we can
	// prune managed merge entries from directories NOT covered by any plan.
	dirsCoveredByMergePlans := make(map[string]bool)
	// File mode must also prune stale cache-backed symlinks from every regular
	// component directory, including components that have no desired artifacts.
	portableCleanupDirs := make(map[string]string)
	pruneOutDir := outDir
	pruneReportPrefix := ""
	if key.target == hcc.TargetWindsurf && !key.machineScope {
		pruneOutDir = filepath.Join(outDir, windsurfDir)
		pruneReportPrefix = windsurfDir
	}
	protected := nativeAggregatePruneFence(key.target, key.machineScope)
	protected = extendNativeHookProjectionPruneFence(key.target, key.machineScope, pruneOutDir, destinations, protected)
	protected, retiredFiles := extendRetiredPruneFence(key.target, protected)

	plans := materializePlans(key.target, outDir, artifacts, linkedComponents(skillLinks[key]), key.machineScope)
	if info, ok := harness.TargetInfoFor(key.target); ok {
		plans = slices.DeleteFunc(plans, func(plan materializePlan) bool {
			return info.PreservesLegacyPath(plan.reportPrefix, key.machineScope)
		})
	}
	for _, publication := range supportPublication.plans {
		for path := range publication.manifestPaths {
			fileDesired[path] = true
		}
	}
	// Prune suppression is per COMPONENT: only the components a skipped
	// resource would have written into may keep their stale files.
	for i := range plans {
		if componentBlocked(blocked, plans[i].reportPrefix) || componentProtectedByNativePath(protectedPaths, plans[i].reportPrefix) {
			plans[i].mode = hcc.WriteModeOverwrite
			plans[i].prune = false
			dirsCoveredByMergePlans[plans[i].outDir] = true
		}
	}
	for _, plan := range plans {
		supportPlan, hasSupportPublication := supportPublication.plans[nativeHookSupportPlanKeyFor(plan)]
		if hasSupportPublication {
			for i := range supportPlan.diagnostics {
				supportPlan.diagnostics[i].Target = string(key.target)
			}
			report.Diags = append(report.Diags, supportPlan.diagnostics...)
		}
		regularArtifacts, mergeArtifacts := splitMergeArtifacts(plan.artifacts)
		if mode == config.MaterializationModeFile && !dryRun && len(plan.artifacts) > 0 {
			if err := state.prepareManifestParentForOutput(plan.outDir); err != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
				continue
			}
		}
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
				if hasSupportPublication && supportPlan.writeReport != nil {
					appendMaterializePlanWriteReport(report, key, outDir, plan, supportPlan.writeReport)
				}
				continue
			}
			portableCleanupDirs[plan.outDir] = plan.reportPrefix
		}

		cacheDir := cachedPlanDirectoryForOutput(key, outDir, plan, coalescedCodexPartitions)
		pruneCache := !hasSupportPublication && !componentBlocked(blocked, plan.reportPrefix) && !componentProtectedByNativePath(protectedPaths, plan.reportPrefix)
		if matErr := prepareArtifactLinkCacheWithPrune(cacheDir, regularArtifacts, dryRun, pruneCache); matErr != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("prepare artifact cache for target %s: %v", key.target, matErr),
			})
			if hasSupportPublication && supportPlan.writeReport != nil {
				appendMaterializePlanWriteReport(report, key, outDir, plan, supportPlan.writeReport)
			}
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
		planCollisionMeta := materializePlanCollisionMetadata(key, plan, regularArtifacts, artifactMeta)
		var writeReport *hcc.WriteReport
		var writeDiags []diag.Diagnostic
		var matErr error
		if !dryRun && beforeSupportWrite != nil {
			for _, artifact := range regularArtifacts {
				if !isHookSupportArtifact(hookSupportPaths, plan.reportPrefix, artifact.Path) {
					continue
				}
				if err := beforeSupportWrite("output", filepath.Join(plan.outDir, filepath.FromSlash(artifact.Path))); err != nil {
					matErr = fmt.Errorf("install hook support %s: %w", artifact.Path, err)
					break
				}
			}
		}
		supportTargets := geminiCoalescence.supportTargetsForPlan(key, plan, supportPublication, hookSupportPaths)
		if matErr == nil {
			if mode == config.MaterializationModeFile {
				writeReport, writeDiags, matErr = syncPortableArtifactFiles(plan.outDir, regularArtifacts, cacheDir, dryRun, wantPrune, planCollisionMeta, state, key.target, plan.reportPrefix, fileDesired, boundary)
			} else {
				writeReport, writeDiags, matErr = syncArtifactLinks(plan.outDir, regularArtifacts, cacheDir, dryRun, wantPrune, planCollisionMeta, portableSyncContext{state: state, target: key.target, reportPrefix: plan.reportPrefix, desired: fileDesired, supportDesired: supportTargets, boundary: boundary})
			}
		}
		if hasSupportPublication && supportPlan.writeReport != nil {
			if writeReport == nil {
				writeReport = &hcc.WriteReport{OutDir: plan.outDir}
			}
			writeReport.Written = append(writeReport.Written, supportPlan.writeReport.Written...)
			writeReport.Skipped = append(writeReport.Skipped, supportPlan.writeReport.Skipped...)
			writeReport.Removed = append(writeReport.Removed, supportPlan.writeReport.Removed...)

		}
		if matErr != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{
				Code:     "materialize.write.error",
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Message:  fmt.Sprintf("materialize target %s to %s: %v", key.target, plan.outDir, matErr),
			})
			if hasSupportPublication && supportPlan.writeReport != nil {
				appendMaterializePlanWriteReport(report, key, outDir, plan, writeReport)
			}
			continue
		}
		if len(writeDiags) > 0 {
			for i := range writeDiags {
				writeDiags[i].Target = string(key.target)
			}
			report.Diags = append(report.Diags, writeDiags...)
		}
		if componentBlocked(blocked, plan.reportPrefix) && componentName(plan.reportPrefix) == hookAggregateComponent(key) {
			retained, err := retainBlockedHookArtifacts(plan.outDir, mergeArtifacts)
			if err != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("retain blocked hook entries in %s: %v", plan.outDir, err)})
				if hasSupportPublication && supportPlan.writeReport != nil {
					appendMaterializePlanWriteReport(report, key, outDir, plan, writeReport)
				}
				continue
			}
			mergeArtifacts = retained
		}
		mergePrune := wantPrune
		if len(mergeArtifacts) == 0 && genericMarkerPruneRootProtected(pruneOutDir, plan.outDir, protected) {
			mergePrune = false
		}
		mergeReport, mergeDiags, mergeErr := materializeMergeArtifacts(plan.outDir, mergeArtifacts, dryRun, mergePrune, boundary)
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
			if hasSupportPublication && supportPlan.writeReport != nil {
				appendMaterializePlanWriteReport(report, key, outDir, plan, writeReport)
			}
			continue
		}
		writeReport.Written = append(writeReport.Written, mergeReport.Written...)
		writeReport.Skipped = append(writeReport.Skipped, mergeReport.Skipped...)
		writeReport.Removed = append(writeReport.Removed, mergeReport.Removed...)

		sort.Strings(writeReport.Written)
		sort.Strings(writeReport.Skipped)
		sort.Strings(writeReport.Removed)
		appendMaterializePlanWriteReport(report, key, outDir, plan, writeReport)
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
	var pruneReport *hcc.WriteReport
	var pruneDiags []diag.Diagnostic
	// Root-level managed merge files belong to no component, so only the
	// wildcard block reaches them.
	if key.target == hcc.Target(targetmeta.TargetCodex) {
		pruneReport = &hcc.WriteReport{OutDir: pruneOutDir}
	} else if !componentBlocked(blocked, "") {
		pruneReport, pruneDiags = pruneUncoveredManagedMergeFiles(pruneOutDir, dirsCoveredByMergePlans, dryRun, protected)
		retiredReport, retiredDiags := reconcileRetiredManagedFiles(pruneOutDir, retiredFiles, dryRun)
		pruneReport.Written = append(pruneReport.Written, retiredReport.Written...)
		pruneReport.Removed = append(pruneReport.Removed, retiredReport.Removed...)
		pruneDiags = append(pruneDiags, retiredDiags...)
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
		symlinkLinks, copyLinks := partitionFileCopyLinks(key.target, group.links)
		if mode == config.MaterializationModeFile && !dryRun && len(symlinkLinks) > 0 {
			if err := state.prepareManifestParentForOutput(groupDir); err != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.manifest.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
				continue
			}
		}
		if err := boundary.directory(groupDir, !dryRun); err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.destination.error", Severity: diag.SeverityError, Target: string(key.target), Message: err.Error()})
			continue
		}
		if mode == config.MaterializationModeFile {
			portableCleanupDirs[groupDir] = group.reportPrefix
		}

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
				tr, err = syncPortableSkillTrees(groupDir, symlinkLinks, dryRun, state, key.target, group.reportPrefix, fileDesired, boundary)
			} else {
				tr, err = syncSkillLinks(groupDir, symlinkLinks, dryRun, !componentBlocked(blocked, group.installDirRel), portableSyncContext{state: state, target: key.target, reportPrefix: group.reportPrefix, desired: fileDesired, boundary: boundary})
			}
			report = appendGroupReport(report, key, outDir, group.reportPrefix, tr, err, "materialize target %s to %s")
			if err != nil {
				continue
			}
		}

		if len(copyLinks) > 0 {
			copyDirsCovered[groupDir] = true
			tr, err := syncCommandFiles(groupDir, copyLinks, dryRun, !componentBlocked(blocked, group.installDirRel), boundary)
			report = appendGroupReport(report, key, outDir, group.reportPrefix, tr, err, "copy command files for target %s to %s")
		}
	}

	// Prune managed file-copy resources (e.g. Factory commands) from their
	// install dirs when no group covered them this run — this handles the
	// removal case where the resolved set no longer contains the resource.
	// Regular files are never pruned by the symlink-oriented artifact plan,
	// so they must be cleaned up here.
	report = pruneUncoveredFileCopyDirs(report, key, outDir, copyDirsCovered, blocked, dryRun, boundary)

	// The two manifest-driven prune passes below walk the target's whole
	// recorded state rather than one component directory, so they cannot be
	// narrowed per component and stay suppressed by any skip.
	if mode == config.MaterializationModeFile && !anyBlocked(blocked) {
		report = prunePortableStaleSymlinks(report, key, outDir, portableCleanupDirs, fileDesired, dryRun, boundary, protectedPaths)
	}

	var portableReports []TargetReport
	var portableDiags []diag.Diagnostic
	var portableErr error
	if !anyBlocked(blocked) {
		portableReports, portableDiags, portableErr = prunePortableEntries(state, key, outDir, repoRoot, fileDesired, dryRun, boundary, protectedPaths)
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

func appendMaterializePlanWriteReport(report *Report, key outputKey, outDir string, plan materializePlan, writeReport *hcc.WriteReport) {
	if report == nil || writeReport == nil {
		return
	}
	written := append([]string(nil), writeReport.Written...)
	skipped := append([]string(nil), writeReport.Skipped...)
	removed := append([]string(nil), writeReport.Removed...)
	sort.Strings(written)
	sort.Strings(skipped)
	sort.Strings(removed)
	tr := TargetReport{
		Target:       key.target,
		MachineScope: key.machineScope,
		RootID:       plan.rootID,
		OutDir:       writeReport.OutDir,
		Written:      written,
		Skipped:      skipped,
		Removed:      removed,
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
	noPrune := noPruneComponents(target, linkedSkills)
	if info.PlanStrategy == harness.PlanWindsurf {
		support, rest := splitDeclaredHookSupportArtifacts(target, machineScope, artifacts)
		supportComponent := componentName(info.ResourceInstallDir(ir.KindHook))
		plans := subtreeMaterializePlans(outDir, support, []string{supportComponent}, "", noPrune)
		return append(plans, windsurfMaterializePlans(outDir, rest, machineScope, noPrune, supportComponent)...)
	}
	return subtreeMaterializePlans(outDir, artifacts, info.Components, info.ArtifactRootPrefix, noPrune)
}

func hookSupportOwnerID(resource, sourceDigest string) string {
	return resource + "\x00" + sourceDigest
}

func ownedJSONEntryHasBlockedHookSource(entry ownedJSONEntry, blocked map[string]bool) bool {
	if entry.kind != ownership.KindHook {
		return false
	}
	if blocked[hookSupportOwnerID(entry.resource, entry.sourceDigest)] {
		return true
	}
	for _, evidence := range entry.sourceEvidence {
		if blocked[hookSupportOwnerID(evidence.Resource, evidence.SourceDigest)] {
			return true
		}
	}
	return false
}

func preflightHookSupportDestinations(result *compilePhaseResult, key outputKey, repoRoot, outDir string, artifacts []artifact.Artifact, links map[string]*skillLinkGroup, coalescedCodexPartitions map[string]bool) ([]artifact.Artifact, []diag.Diagnostic, []BlockedResource) {
	ownersByPath := result.hookSupportOwners[key]
	if len(ownersByPath) == 0 {
		return artifacts, nil, nil
	}
	state, stateErr := loadPortableState(repoRoot, key.machineScope)
	owners := make(map[string]hookSupportOwner)
	ownerOrder := make([]string, 0)
	ownerDiags := make(map[string]diag.Diagnostic)
	blockedPaths := make(map[string]bool)
	addIssue := func(path string, affected []hookSupportOwner, code string) {
		blockedPaths[path] = true
		for _, owner := range affected {
			id := hookSupportOwnerID(owner.resourceName, owner.sourceDigest)
			if _, exists := owners[id]; exists {
				continue
			}
			owners[id] = owner
			ownerOrder = append(ownerOrder, id)
			message := "Hook support destination could not be validated safely; hook suppressed"
			hint := "Resolve the conflicting or unsafe support destination and retry sync"
			if code == "materialize.manifest.error" {
				message = "Hook support ownership could not be validated; hook suppressed"
				hint = "Repair the materialization manifest before syncing hooks"
			}
			ownerDiags[id] = diag.Diagnostic{
				Code:     code,
				Severity: diag.SeverityError,
				Target:   string(key.target),
				Resource: owner.resourceName,
				Message:  message,
				Hint:     hint,
				Source:   compilepkg.SourceOf(owner.skill.skill.Resource),
			}
		}
	}
	type preflightSnapshotOwner struct {
		path  string
		owner hookSupportOwner
	}
	snapshotOwners := make(map[hookSourceSnapshotKey][]preflightSnapshotOwner)
	var snapshotOrder []hookSourceSnapshotKey
	for _, supportPath := range sortedKeys(ownersByPath) {
		for _, owner := range ownersByPath[supportPath] {
			if owner.sourceSnapshot == nil {
				continue
			}
			snapshotKey := hookSourceSnapshotKey{snapshot: owner.sourceSnapshot, excludedPeers: strings.Join(owner.sourceExcludedPeers, "\x00")}
			if _, exists := snapshotOwners[snapshotKey]; !exists {
				snapshotOrder = append(snapshotOrder, snapshotKey)
			}
			snapshotOwners[snapshotKey] = append(snapshotOwners[snapshotKey], preflightSnapshotOwner{path: supportPath, owner: owner})
		}
	}
	for _, snapshotKey := range snapshotOrder {
		owners := snapshotOwners[snapshotKey]
		if len(owners) == 0 {
			continue
		}
		if err := revalidateHookSourceSnapshot(snapshotKey.snapshot, owners[0].owner.sourceExcludedPeers); err != nil {
			for _, item := range owners {
				addIssue(item.path, []hookSupportOwner{item.owner}, diag.CodeMaterializeHookSupportConflict)
			}
		}
	}
	var supportProofLoaded bool
	var cleanSupportSources map[string]bool
	var supportProofErr error
	loadSupportProof := func() {
		if supportProofLoaded {
			return
		}
		supportProofLoaded = true
		cleanSupportSources, supportProofErr = nativeHookSupportPreflightCleanSources(key, repoRoot)
	}
	if stateErr != nil {
		paths := make([]string, 0, len(ownersByPath))
		for path := range ownersByPath {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			addIssue(path, ownersByPath[path], "materialize.manifest.error")
		}
	} else {
		for _, plan := range materializePlans(key.target, outDir, artifacts, linkedComponents(links), key.machineScope) {
			planRoot := outDir
			if plan.root != "" {
				planRoot = plan.root
			}
			rel, relErr := filepath.Rel(planRoot, plan.outDir)
			planErr := relErr
			if planErr == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
				planErr = fmt.Errorf("materialization plan escapes its output root")
			}
			if planErr == nil {
				_, planErr = validatePortableOutputDir(planRoot, filepath.ToSlash(rel))
			}
			for _, candidate := range plan.artifacts {
				relative := path.Join(plan.reportPrefix, filepath.ToSlash(candidate.Path))
				affected := ownersByPath[relative]
				if len(affected) == 0 {
					continue
				}
				if planErr != nil {
					addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
					continue
				}
				destination, err := portableOutputPath(plan.outDir, candidate.Path)
				if err != nil {
					addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
					continue
				}
				info, err := os.Lstat(destination)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 && isManagedSymlink(destination) {
					if key.machineScope {
						continue
					}
					cacheDir := cachedPlanDirectoryForOutput(key, outDir, plan, coalescedCodexPartitions)
					expectedCacheFile := filepath.Join(cacheDir, filepath.FromSlash(candidate.Path))
					linkTarget, _, linkErr := dependencytxn.ReadSymlinkSnapshot(destination)
					if linkErr != nil || linkTarget != expectedCacheFile {
						addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
						continue
					}
					if _, err := validatePortableOutputDir(cacheDir, filepath.ToSlash(filepath.Dir(candidate.Path))); err != nil {
						addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
						continue
					}
					cacheBytes, cacheGeneration, cacheMode, err := dependencytxn.ReadSnapshot(expectedCacheFile)
					if err != nil || !cacheGeneration.Exists || !bytes.Equal(cacheBytes, candidate.Bytes) || cacheMode != portableArtifactMode(candidate.Mode) {
						addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
						continue
					}
					loadSupportProof()
					for _, owner := range affected {
						if supportProofErr != nil || owner.sourceDigest == "" || !cleanSupportSources[hookSupportOwnerID(owner.resourceName, owner.sourceDigest)] {
							addIssue(relative, []hookSupportOwner{owner}, diag.CodeMaterializeHookSupportConflict)
						}
					}
					continue
				}
				currentDigest, err := portablePathDigest(destination)
				if err != nil {
					addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
					continue
				}
				entry, _ := state.entry(key.target, relative)
				if portableOwnershipDecision(entry, currentDigest, portableArtifactDigest(candidate)) == portableOwnershipCollision {
					addIssue(relative, affected, diag.CodeMaterializeHookSupportConflict)
				}
			}
		}
	}
	if len(owners) == 0 {
		return artifacts, nil, nil
	}
	blockedSources := make(map[string]bool, len(owners))
	for id := range owners {
		blockedSources[id] = true
	}
	for path, affected := range ownersByPath {
		for _, owner := range affected {
			if blockedSources[hookSupportOwnerID(owner.resourceName, owner.sourceDigest)] {
				blockedPaths[path] = true
				break
			}
		}
	}
	blockedResources := make([]BlockedResource, 0, len(ownerOrder))
	blockedResourcesByValue := make(map[BlockedResource]bool)
	diagnostics := make([]diag.Diagnostic, 0, len(ownerOrder))
	for _, id := range ownerOrder {
		owner := owners[id]
		if result.unknownHookSelection == nil {
			result.unknownHookSelection = make(map[outputKey]bool)
		}
		result.unknownHookSelection[key] = true
		result.blocked.blockResource(key, owner.skill)
		resource := blockedResourceFor(key, owner.skill)
		if !blockedResourcesByValue[resource] {
			blockedResources = append(blockedResources, resource)
			blockedResourcesByValue[resource] = true
		}
		if !slices.Contains(result.blockedResources, resource) {
			result.blockedResources = append(result.blockedResources, resource)
		}
		diagnostics = append(diagnostics, ownerDiags[id])
	}
	result.hookResources[key] = slices.DeleteFunc(result.hookResources[key], func(resource BlockedResource) bool {
		return blockedResourcesByValue[resource]
	})
	for path := range blockedPaths {
		delete(result.hookSupportPaths[key], path)
		delete(result.hookSupportOwners[key], path)
	}
	if work := result.nativeHooks[key]; work != nil {
		work.entries = slices.DeleteFunc(work.entries, func(entry ownedJSONEntry) bool {
			return ownedJSONEntryHasBlockedHookSource(entry, blockedSources)
		})
		allNativeHooksBlocked := len(work.entries) == 0 && len(result.nativeMCPEntries[key]) == 0 &&
			!result.nativeMCPIncomplete[key] && len(work.incomplete) == 0
		if allNativeHooksBlocked && !key.machineScope && !nativeHookSharesMCPRoute(key, repoRoot) && len(result.hookSupportPaths[key]) == 0 {
			delete(result.nativeHooks, key)
		} else if allNativeHooksBlocked && !nativeHookOutputPresent(key, repoRoot) {
			delete(result.nativeHooks, key)
		} else if _, file, err := nativeHookJSONLayout(key, repoRoot); err == nil {
			work.markIncomplete(file.rel)
		}
	}
	remaining := make([]artifact.Artifact, 0, len(artifacts))
	for _, candidate := range artifacts {
		if !blockedPaths[filepath.ToSlash(candidate.Path)] {
			remaining = append(remaining, candidate)
		}
	}
	if targetWork := result.byTarget[key]; targetWork != nil {
		targetWork.artifacts = slices.DeleteFunc(targetWork.artifacts, func(candidate artifact.Artifact) bool {
			return blockedPaths[filepath.ToSlash(candidate.Path)]
		})
	}
	return remaining, diagnostics, blockedResources
}

func splitDeclaredHookSupportArtifacts(target hcc.Target, machineScope bool, artifacts []artifact.Artifact) (support, rest []artifact.Artifact) {
	root, _, err := hookSupportAggregateLayout(skillTarget{target: target, machineScope: machineScope})
	if err != nil {
		return nil, artifacts
	}
	prefix := path.Clean(root)
	for _, a := range artifacts {
		if strings.HasPrefix(a.Path, prefix+"/") {
			support = append(support, a)
		} else {
			rest = append(rest, a)
		}
	}
	return support, rest
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

func windsurfMaterializePlans(outDir string, artifacts []artifact.Artifact, machineScope bool, noPrune map[string]bool, supportComponent string) []materializePlan {
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

	nativeComponents := generatedComponents(hcc.TargetWindsurf)
	components := make([]string, 0, len(nativeComponents))
	for _, component := range nativeComponents {
		if component != supportComponent {
			components = append(components, component)
		}
	}
	delete(scoped, supportComponent)
	var plans []materializePlan
	for _, component := range orderedComponents(components, scoped) {
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

// hookAggregateComponent returns the component containing the target's hook
// aggregate file in this scope, or "" when it sits at the partition root.
func hookAggregateComponent(key outputKey) string {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.Target(key.target))
	if !ok {
		return ""
	}
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind != ir.KindHook {
				continue
			}
			file := aggregate.Project
			if key.machineScope {
				file = aggregate.User
			}
			installed := file.Path
			if len(file.Legacy) > 0 {
				installed = file.Legacy[0]
			}
			return componentName(path.Join(partition.Prefix, path.Dir(installed)))
		}
	}
	return ""
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
	if resolved.PluginPackage != nil {
		if resolved.PluginPackage.GenerationPath == "" || resolved.PluginPackage.MemberPath == "" {
			return ""
		}
		path := filepath.Join(resolved.PluginPackage.GenerationPath, filepath.FromSlash(resolved.PluginPackage.MemberPath))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return filepath.Dir(path)
		}
		return path
	}
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

func cachedPlanDirectoryForOutput(key outputKey, outDir string, plan materializePlan, coalescedCodexPartitions map[string]bool) string {
	cacheMachineScope, cacheOutDir := key.machineScope, outDir
	// A coalesced physical partition must also have one cache target;
	// otherwise identical repo/machine bytes produce different symlink
	// targets and rewrite the destination twice.
	if key.target == hcc.Target(targetmeta.TargetCodex) && coalescedCodexPartitions[codexPlanPartitionKey(plan)] {
		cacheMachineScope, cacheOutDir = false, plan.root
	}
	return cachedPlanDir(key.target, cacheMachineScope, cacheOutDir, plan.reportPrefix, plan.artifacts)
}

func prepareArtifactLinkCacheWithPrune(cacheDir string, artifacts []artifact.Artifact, dryRun, prune bool) error {
	if dryRun || len(artifacts) == 0 {
		return nil
	}
	_, err := materializeArtifactLinkCache(cacheDir, artifacts, dryRun, prune)
	return err
}

func materializeArtifactLinkCache(cacheDir string, artifacts []artifact.Artifact, dryRun, prune bool) (*hcc.WriteReport, error) {
	if len(artifacts) == 0 {
		return &hcc.WriteReport{OutDir: cacheDir}, nil
	}
	if !dryRun {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, err
		}
	}
	writeMode := hcc.WriteModeOverwrite
	if prune {
		writeMode = hcc.WriteModeSync
	}
	options := []hcc.WriteOption{hcc.WithWriteMode(writeMode), hcc.WithPruneStale(prune)}
	if dryRun {
		options = append(options, hcc.WithDryRun())
	}
	return hcc.Materialize(&hcc.Result{Artifacts: artifacts}, cacheDir, options...)
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
			aliasErr := materializeMarkerAlias(outDir, destPath, capturedBoundary, dryRun) // follows symlink
			// get target file info for permissions
			if aliasErr != nil {
				return nil, nil, fmt.Errorf("detach marker symlink %s: %w", destPath, aliasErr)
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

func genericMarkerPruneRootProtected(pruneRoot, candidate string, protected map[string]bool) bool {
	rel, err := filepath.Rel(pruneRoot, candidate)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	for protectedPath := range protected {
		if !strings.HasSuffix(protectedPath, "/") {
			continue
		}
		protectedDir := strings.TrimSuffix(protectedPath, "/")
		if rel == protectedDir || strings.HasPrefix(rel, protectedDir+"/") {
			return true
		}
	}
	return false
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

// extendRetiredPruneFence adds the target's retired locations to the prune
// walk's fence so the generic marker strip never touches them: retired
// directories (trailing slash) are left entirely alone and retired files
// are reconciled by reconcileRetiredManagedFiles. It returns the fence and
// the retired file list.
func extendRetiredPruneFence(t hcc.Target, protected map[string]bool) (map[string]bool, []string) {
	info, ok := harness.TargetInfoFor(t)
	if !ok || len(info.RetiredLocations) == 0 {
		return protected, nil
	}
	fence := make(map[string]bool, len(protected)+len(info.RetiredLocations))
	for k := range protected {
		fence[k] = true
	}
	var files []string
	for _, rel := range info.RetiredLocations {
		fence[rel] = true
		if !strings.HasSuffix(rel, "/") {
			files = append(files, rel)
		}
	}
	return fence, files
}

// reconcileRetiredManagedFiles strips managed entries from a target's
// retired shared files and deletes a file — and its then-empty directory —
// once only empty skills/agents members remain. dryRun reports the same
// outcome without writing.
func reconcileRetiredManagedFiles(outDir string, files []string, dryRun bool) (*hcc.WriteReport, []diag.Diagnostic) {
	report := &hcc.WriteReport{OutDir: outDir}
	var diags []diag.Diagnostic
	for _, rel := range files {
		if !retiredFileTargetSafe(outDir, rel) {
			continue
		}
		path := filepath.Join(outDir, filepath.FromSlash(rel))
		cleaned, removed, err := stripManagedEntriesFromFile(path, dryRun)
		if err != nil {
			diags = append(diags, managedMergePruneDiag(rel, path, err))
			continue
		}
		if len(removed) == 0 {
			continue
		}
		if retiredFileEmptied(cleaned) {
			if !dryRun {
				if removeErr := os.Remove(path); removeErr != nil {
					diags = append(diags, managedMergePruneDiag(rel, path, removeErr))
					continue
				}
				// The directory goes only when the deletion emptied it;
				// os.Remove fails on a directory that still holds files.
				_ = os.Remove(filepath.Dir(path))
			}
			report.Removed = append(report.Removed, rel)
			continue
		}
		report.Written = append(report.Written, rel)
	}
	return report, diags
}

// retiredFileTargetSafe reports whether every path element between outDir and
// the retired file is a real directory and the file itself is a regular file.
// A symlink anywhere in the chain — a symlinked .claude-plugin directory or a
// symlinked plugin.json — would let the reconcile read, rewrite, or delete
// files outside the harness root, so the location is left completely alone:
// no read, no write, no delete, no drift.
func retiredFileTargetSafe(outDir, rel string) bool {
	current := outDir
	parts := strings.Split(filepath.FromSlash(rel), string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if i == len(parts)-1 {
			return info.Mode().IsRegular()
		}
		if !info.IsDir() {
			return false
		}
	}
	return false
}

// retiredFileEmptied reports whether a stripped retired manifest carries
// nothing but empty skills/agents members, so the file itself can go.
func retiredFileEmptied(cleaned []byte) bool {
	doc, err := jsonc.Parse("retired", cleaned)
	if err != nil {
		return false
	}
	root := doc.Root()
	if root.Kind() != jsonc.KindObject {
		return false
	}
	for _, key := range root.Keys() {
		if key != "skills" && key != "agents" {
			return false
		}
		member, ok := root.Member(key)
		if !ok || member.Kind() != jsonc.KindArray || member.Len() != 0 {
			return false
		}
	}
	return true
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
		if entry.PluginInstance != "" {
			owned = false
		}
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
		if entry.PluginInstance != "" {
			owned = false
		}
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

func portablePathIsProtected(protectedPaths map[string]bool, rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	for protected := range protectedPaths {
		protected = filepath.ToSlash(filepath.Clean(filepath.FromSlash(protected)))
		if rel == protected || strings.HasPrefix(rel, strings.TrimSuffix(protected, "/")+"/") {
			return true
		}
	}
	return false
}

func componentProtectedByNativePath(protectedPaths map[string]bool, reportPrefix string) bool {
	component := componentName(reportPrefix)
	for protected := range protectedPaths {
		if strings.HasSuffix(protected, "/") && componentName(protected) == component {
			return true
		}
	}
	return false
}

func diagnosticInList(diagnostic diag.Diagnostic, candidates []diag.Diagnostic) bool {
	for _, candidate := range candidates {
		if diagnostic.Code == candidate.Code && diagnostic.Severity == candidate.Severity &&
			diagnostic.Target == candidate.Target && diagnostic.Resource == candidate.Resource &&
			diagnostic.Message == candidate.Message && diagnostic.Hint == candidate.Hint && diagnostic.Source == candidate.Source {
			return true
		}
	}
	return false
}

func prunePortableEntries(state *portableState, key outputKey, outDir, repoRoot string, desired map[string]bool, dryRun bool, boundary *destinationBoundary, protectedPathSets ...map[string]bool) ([]TargetReport, []diag.Diagnostic, error) {
	protectedPaths := map[string]bool{}
	if len(protectedPathSets) > 0 && protectedPathSets[0] != nil {
		protectedPaths = protectedPathSets[0]
	}

	reports := make(map[string]*TargetReport)
	var diags []diag.Diagnostic
	keys := make([]string, 0, len(state.entries))
	for key := range state.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, stateKey := range keys {
		entry := state.entries[stateKey]
		if entry.PluginInstance != "" {
			continue
		}
		if entry.Target != string(key.target) || desired[entry.Path] || portablePathIsProtected(protectedPaths, entry.Path) {
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
func prunePortableStaleSymlinks(report *Report, key outputKey, outDir string, cleanupDirs map[string]string, desired map[string]bool, dryRun bool, boundary *destinationBoundary, protectedPathSets ...map[string]bool) *Report {
	protectedPaths := map[string]bool{}
	if len(protectedPathSets) > 0 && protectedPathSets[0] != nil {
		protectedPaths = protectedPathSets[0]
	}

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
		stale, diags, err := staleArtifactLinks(dir, desiredInDir, snapshots, boundary)
		if err != nil {
			report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("prune stale portable symlinks in %s: %v", dir, err)})
			continue
		}
		protectedStale := make([]string, 0, len(stale))
		for _, rel := range stale {
			candidate := filepath.Join(dir, filepath.FromSlash(rel))
			outRel, relErr := filepath.Rel(outDir, candidate)
			if relErr == nil && portablePathIsProtected(protectedPaths, outRel) {
				continue
			}
			protectedStale = append(protectedStale, rel)
		}
		stale = protectedStale
		tr := TargetReport{OutDir: dir, Diags: diags}
		for _, rel := range stale {
			path, pathErr := portableOutputPath(dir, rel)
			if pathErr != nil {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: "materialize.write.error", Severity: diag.SeverityError, Target: string(key.target), Message: pathErr.Error()})
				continue
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
	var supportDesired map[string]string
	if len(portable) > 0 {
		state, target, reportPrefix, portableDesired = portable[0].state, portable[0].target, portable[0].reportPrefix, portable[0].desired
		supportDesired = portable[0].supportDesired
	}
	for path := range supportDesired {
		if portableDesired != nil {
			portableDesired[filepath.ToSlash(filepath.Join(reportPrefix, path))] = true
		}
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
			manifestOwned = manifestOwned && entry.PluginInstance == ""
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
		pruneDesired := desired
		if len(supportDesired) > 0 {
			pruneDesired = make(map[string]string, len(desired)+len(supportDesired))
			for path, target := range desired {
				pruneDesired[path] = target
			}
			for path, target := range supportDesired {
				pruneDesired[path] = target
			}
		}
		var readBoundary *destinationBoundary
		if len(portable) > 0 {
			readBoundary = portable[0].boundary
		}
		snapshots := make(map[string]managedLinkSnapshot)
		stale, staleDiags, err := staleArtifactLinks(outDir, pruneDesired, snapshots, readBoundary)
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

func staleArtifactLinks(outDir string, desired map[string]string, snapshots map[string]managedLinkSnapshot, boundaries ...*destinationBoundary) (stale []string, diags []diag.Diagnostic, err error) {
	var boundary *destinationBoundary
	if len(boundaries) > 0 {
		boundary = boundaries[0]
	}
	root, err := capturePruneDirectory(outDir, boundary)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := root.NativeRoot()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	err = fs.WalkDir(native.FS(), ".", func(relPath string, d fs.DirEntry, err error) error {
		path := filepath.Join(outDir, filepath.FromSlash(relPath))
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
		snapshot, err := observeManagedLink(filepath.FromSlash(relPath), native)
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
		if snapshots != nil {
			snapshots[relSlash] = snapshot
		}
		return nil
	})
	if err != nil {
		return nil, diags, err
	}
	if boundary != nil && boundary.pruneEnumerationHook != nil {
		boundary.pruneEnumerationHook(outDir)
	}
	// The walk read the retained directory; a replaced name must not turn an
	// obsolete enumeration into a successful discovery result.
	if err := verifyPruneDirectory(outDir, boundary, root); err != nil {
		return nil, diags, err
	}
	sort.Strings(stale)
	return stale, diags, nil
}

// cleanEmptyPortableArtifactDirs removes empty portable artifact directories.
// Each directory is validated before reading or
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

// supportStageCopyHook runs after the support stage is admitted and before
// support is copied into it. It is nil in production and set only by serial
// tests in this package.
var supportStageCopyHook func(parent string)

// supportStageCleanupHook runs after the support stage name is matched to
// the admitted stage and before removal. It is nil in production and set only
// by serial tests in this package.
var supportStageCleanupHook func(stage string)

// removeAdmittedSupportStage removes the attempt-created support stage only
// while its name still refers to the admitted directory. Contents are removed
// through the admitted descriptor, and the name itself only by an
// empty-directory removal, so a replacement swapped in during cleanup is never
// recursively deleted; a replaced name is left untouched as a generation
// conflict.
func removeAdmittedSupportStage(parent *fsboundary.Dir, native *os.Root, name string, stage *fsboundary.Dir, created fsboundary.Identity) error {
	sameStage := func() error {
		current, err := native.Lstat(name)
		if err != nil {
			return fmt.Errorf("%w: support stage unavailable for cleanup: %w", fsboundary.ErrGenerationConflict, err)
		}
		identity, err := fsboundary.FileInfoIdentity(current)
		if err != nil {
			return err
		}
		if !identity.SameObject(created) {
			return fmt.Errorf("%w: support stage replaced before cleanup", fsboundary.ErrGenerationConflict)
		}
		return nil
	}
	if err := sameStage(); err != nil {
		return err
	}
	if supportStageCleanupHook != nil {
		supportStageCleanupHook(filepath.Join(parent.Path(), name))
	}
	if err := removeCapturedDirectoryContents(stage); err != nil {
		return err
	}
	if err := sameStage(); err != nil {
		return err
	}
	if err := parent.Rmdir(name); err != nil {
		return fmt.Errorf("%w: support stage changed during cleanup: %w", fsboundary.ErrGenerationConflict, err)
	}
	return nil
}

// removeCapturedDirectoryContents empties a captured directory through
// descriptor-relative operations without following links.
func removeCapturedDirectoryContents(dir *fsboundary.Dir) (err error) {
	native, err := dir.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	entries, err := fs.ReadDir(native.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := native.Lstat(entry.Name())
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := dir.Unlink(entry.Name()); err != nil {
				return err
			}
			continue
		}
		child, err := dir.OpenPath(entry.Name())
		if err != nil {
			return err
		}
		removeErr := removeCapturedDirectoryContents(child)
		if closeErr := child.Close(); closeErr != nil {
			removeErr = errors.Join(removeErr, closeErr)
		}
		if removeErr != nil {
			return removeErr
		}
		if err := dir.Rmdir(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func prepareSkillLinkCache(link skillLink) (err error) {
	if len(link.artifacts) == 0 {
		return nil
	}
	cacheDir := link.cacheDir
	if cacheDir == "" {
		cacheDir = link.target
	}
	cacheDir, err = filepath.Abs(cacheDir)
	if err != nil {
		return err
	}
	parentPath := filepath.Dir(cacheDir)
	parent, err := harness.CaptureDestinationPath(true, "", parentPath, parentPath, ".", true)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	namespace := &destinationBoundary{capturedRoot: parent, capturedRootPath: parentPath}
	slog.Debug("cache namespace admitted", "path", parentPath, "phase", "cache-namespace")
	if err := namespace.directory(parentPath, false); err != nil {
		return err
	}
	compiled := make(map[string]bool, len(link.artifacts))
	for _, a := range link.artifacts {
		compiled[filepath.ToSlash(filepath.Clean(a.Path))] = true
	}
	supportStage := ""
	if link.sourceDir != "" {
		name := ".support-staging-" + rand.Text()
		if err := parent.Mkdir(name, 0o700); err != nil {
			return err
		}
		supportStage = filepath.Join(parentPath, name)
		// Capture the admitted stage directory itself: cleanup removes its
		// contents through this descriptor, never through the name.
		stage, stageErr := parent.OpenPath(name)
		if stageErr != nil {
			return stageErr
		}
		created := stage.Identity()
		defer func() {
			if cleanupErr := removeAdmittedSupportStage(parent, native, name, stage, created); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
			if closeErr := stage.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
		if supportStageCopyHook != nil {
			supportStageCopyHook(parentPath)
		}
		if err := namespace.directory(parentPath, false); err != nil {
			return err
		}
		// Stage and cache outputs are captured relative to the admitted
		// namespace descriptor, never re-resolved through its path.
		if err := copyScopedSupport(link.sourceDir, supportStage, compiled, "", len(link.declaredSupport) == 0, writeSupportStage, namespace); err != nil {
			return err
		}
		if err := verifyDeclaredSupportFiles(supportStage, link.declaredSupport); err != nil {
			return err
		}
	}
	if err := namespace.directory(cacheDir, true); err != nil {
		return err
	}
	// Write compiled artifacts (SKILL.md) without pruning, because
	// support files copied below are not in the artifact list and
	// would be incorrectly pruned.
	result := &hcc.Result{Artifacts: link.artifacts}
	_, err = hcc.Materialize(result, cacheDir, hcc.WithWriteMode(hcc.WriteModeOverwrite))
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
		if err := copyScopedSupport(supportStage, cacheDir, compiled, "", false, writeSupportSnapshot, namespace); err != nil {
			return err
		}
	}
	return nil
}

// pruneSkillCacheSupportFiles deletes every previous support-file entry while
// retaining current compiled artifacts and the directories required to contain
// them. The cache is fully managed; this makes its support subtree exactly
// reflect the current source tree on every real sync.
func pruneSkillCacheSupportFiles(cacheDir string, compiled map[string]bool) (err error) {
	root, err := fsboundary.OpenDir(cacheDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	slog.Debug("skill cache support pruning starting", "cache_dir", cacheDir, "phase", "support-prune")
	verify := func() error {
		info, err := os.Stat(cacheDir)
		if err != nil {
			return fmt.Errorf("%w: support-prune cache %s unavailable: %w", fsboundary.ErrGenerationConflict, cacheDir, err)
		}
		identity, err := fsboundary.FileInfoIdentity(info)
		if err != nil {
			return fmt.Errorf("support-prune cache %s identity: %w", cacheDir, err)
		}
		if !identity.SameObject(root.Identity()) {
			return fmt.Errorf("%w: support-prune cache %s replaced since capture", fsboundary.ErrGenerationConflict, cacheDir)
		}
		return nil
	}
	if err := verify(); err != nil {
		return err
	}
	native, err := root.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	var prune func(string) error
	prune = func(rel string) error {
		entries, err := readSupportSourceDirectory(root, rel)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childRel := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			if compiled[childRel] {
				continue
			}
			containsCompiled := false
			for artifactPath := range compiled {
				if strings.HasPrefix(artifactPath, childRel+"/") {
					containsCompiled = true
					break
				}
			}
			if containsCompiled && entry.IsDir() {
				if err := prune(childRel); err != nil {
					return err
				}
				continue
			}
			if err := verify(); err != nil {
				return err
			}
			if err := native.RemoveAll(childRel); err != nil {
				return err
			}
		}
		return nil
	}
	return prune(".")
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
			manifestOwned = manifestOwned && entry.PluginInstance == ""
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

	var readBoundary *destinationBoundary
	if len(portable) > 0 {
		readBoundary = portable[0].boundary
	}
	entries, err := readPruneDirectory(skillsDir, readBoundary, func(name string) bool { _, keep := desired[name]; return keep }, pruneStaleEntries)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
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
		observed := entry.observed
		snapshot := managedLinkSnapshot{observed: observed, text: entry.linkText}
		if skillPruneObservedHook != nil {
			skillPruneObservedHook(path)
		}
		if observed.Mode()&os.ModeSymlink == 0 {
			// Non-symlink directories are user-owned skill directories
			// and must be preserved. Mirror the principle from
			// staleArtifactLinks which also preserves user-owned content.
			if observed.IsDir() {
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
			if !entry.managedCommand {
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
			err = pruneNativeCommandFile(path, observed, boundary, dryRun)
			if err != nil {
				if missingPruneEntry(err) {
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
			managed, markerErr := managedCommandFileState(filePath, info, boundary)
			if markerErr != nil {
				return report, markerErr
			}
			if !managed {
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
	var readBoundary *destinationBoundary
	if len(boundaries) > 0 {
		readBoundary = boundaries[0]
	}
	entries, err := readPruneDirectory(dir, readBoundary, func(name string) bool { _, keep := desired[name]; return keep }, true)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
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
		info := entry.observed
		snapshot := managedLinkSnapshot{observed: info, text: entry.linkText}
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
			if !entry.managedCommand {
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
			cleanupErr = pruneNativeCommandFile(path, info, boundary, dryRun)
		}
		if cleanupErr != nil {
			if missingPruneEntry(cleanupErr) {
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
// commandMarkerReadHook runs before a desired command's marker is read. It is
// nil in production and set only by serial tests in this package.
var commandMarkerReadHook func(path string)

// managedCommandFileState classifies an observed command leaf. A non-regular
// or multiply linked leaf cannot be output this tool wrote and stays on the
// user-owned collision path, as do ordinary unreadable payloads. A leaf or
// command directory that vanished, was retargeted, or changed generation
// since it was observed is a refusal rather than a user-owned collision.
func managedCommandFileState(path string, observed os.FileInfo, boundary *destinationBoundary) (bool, error) {
	identity, err := fsboundary.FileInfoIdentity(observed)
	if err != nil {
		return false, err
	}
	if !identity.Mode.IsRegular() || identity.Links != 1 {
		return false, nil
	}
	if commandMarkerReadHook != nil {
		commandMarkerReadHook(path)
	}
	data, err := readNativeCommandFile(path, observed, boundary)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fsboundary.ErrUnsafePath) || errors.Is(err, fsboundary.ErrUnsafeFile) ||
			errors.Is(err, fsboundary.ErrGenerationConflict) || errors.Is(err, fsboundary.ErrTooLarge) {
			return false, fmt.Errorf("read command marker %s: %w", path, err)
		}
		return false, nil
	}
	return isManagedCommandContent(data), nil
}

func isManagedCommandFile(path string, boundaries ...*destinationBoundary) bool {
	var boundary *destinationBoundary
	if len(boundaries) > 0 {
		boundary = boundaries[0]
	}
	data, err := readNativeCommandFile(path, nil, boundary)
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
