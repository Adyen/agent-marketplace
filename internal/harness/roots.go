// Package harness maps harness target names to their install-root paths
// for both repo-scoped and machine-scoped skill materialization.
//
// internal/materialize consumes TargetInfoMap, ResolveDestinations, the
// install roots, and the Devin aggregate file names to drive
// hcc.Materialize into the correct per-target output directories;
// internal/pipeline consumes the same metadata for managed-path and
// safety-backup coverage. The TUI and CLI surfaces (status, doctor,
// backup, init, migrate) consume install-root and detection info such as
// DetectInstalled for informational output.
package harness

import (
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Roots holds the install-root paths for a single harness target.
// Repo is a relative path joined with the repository root;
// Machine is an absolute path under the user's home directory.
type Roots struct {
	Repo    string // relative path within the repo (e.g. ".claude")
	Machine string // absolute machine-wide path (e.g. /home/user/.claude)
}

// For returns the concrete output directory for the given scope.
// If machineScope is true, it returns the Machine path; otherwise it
// joins Repo with repoRoot.
func (r Roots) For(machineScope bool, repoRoot string) string {
	if machineScope {
		return r.Machine
	}
	return filepath.Join(repoRoot, r.Repo)
}

// PlanStrategy determines how materializePlans builds the write plan
// for a target.
type PlanStrategy int

const (
	// PlanSubtree organizes artifacts by component subdirectories.
	PlanSubtree PlanStrategy = iota
	// PlanWindsurf uses Windsurf-specific flat layout.
	PlanWindsurf
)

// Canonical Devin 3000.3 aggregate file names, relative to a Devin install
// root. They are read from the target's registered native-layout descriptor
// (pkg/harness-compilation/target), which is the topology authority:
// TestDevinAggregateNamesMatchCompiledArtifactPaths pins them against the
// paths the compiler really emits so the two cannot drift.
var (
	// DevinProjectHooksFile holds the hook object directly at project scope.
	DevinProjectHooksFile = mustNativeAggregatePath(targetmeta.TargetDevin, ir.KindHook, false)
	// DevinUserConfigFile carries hooks under its `hooks` member at user scope.
	DevinUserConfigFile = mustNativeAggregatePath(targetmeta.TargetDevin, ir.KindHook, true)
	// DevinMcpConfigFile holds mcpServers at the same relative path in both scopes.
	DevinMcpConfigFile = mustNativeAggregatePath(targetmeta.TargetDevin, ir.KindMcpServer, false)
)

// mustNativeAggregatePath resolves an aggregate file from the target's
// registered native-layout descriptor. A missing declaration is a programming
// error in the registry, not a runtime condition, so it panics the way
// mustCodexAggregatePath does in internal/materialize.
func mustNativeAggregatePath(t targetmeta.Target, kind ir.ResourceKind, machineScope bool) string {
	file, ok := targetmeta.NativeAggregateFileForScope(t, kind, machineScope)
	if !ok || file.Path == "" {
		panic(fmt.Sprintf("%s native aggregate for %s at machine_scope=%v is not registered", t, kind, machineScope))
	}
	return file.Path
}

// mustNativeResourcePattern resolves a resource path pattern from the
// target's registered native-layout descriptor.
func mustNativeResourcePattern(t targetmeta.Target, kind ir.ResourceKind) string {
	pattern, ok := targetmeta.NativeResourcePattern(t, kind)
	if !ok {
		panic(fmt.Sprintf("%s native resource path for %s is not registered", t, kind))
	}
	return pattern
}

// AggregateFileSet holds a target's shared aggregate configuration files per
// scope. The two scopes are separate lists rather than one list because Devin's
// hook aggregate genuinely differs by scope: project scope writes
// hooks.v1.json, while user scope patches the `hooks` member of config.json
// (plan, resource mapping: "Hook | Project: hooks.v1.json; user:
// config.json#hooks").
type AggregateFileSet struct {
	// Project lists output-root-relative paths written at repository scope.
	Project []string
	// User lists output-root-relative paths written at machine (user) scope.
	User []string
}

// TargetInfo holds per-target metadata needed for materialization.
// Consolidating this in one place means adding a new harness target
// only requires updating the TargetInfo registry, not scattering
// switch statements across the materialize package.
type TargetInfo struct {
	Roots              Roots
	Components         []string                   // subdirectory names within the install root (e.g. "skills", "commands")
	ArtifactRootPrefix string                     // prefix to strip from artifact paths (e.g. ".gemini")
	PlanStrategy       PlanStrategy               // how to build materialize plans
	ResourceDirs       map[ir.ResourceKind]string // per-kind install subdirectory (relative to install root)
	SkillPathPattern   string                     // format for skill SKILL.md path (e.g. "skills/%s/SKILL.md")

	// SkillDirKinds lists the resource kinds whose compiled artifacts live in a
	// skill DIRECTORY (skills/<name>/SKILL.md plus support files) rather than in
	// the flat `<dir>/<name>.<ext>` shape ResourceDirs models. A kind listed here
	// is materialized as a directory symlink through the same code path as
	// ir.KindSkill, which is what lets a Devin command compile to
	// skills/<name>/SKILL.md (plan, resource mapping: "Command |
	// skills/<name>/SKILL.md | User-triggered skill with
	// x-agent-marketplace-kind: command"). ir.KindSkill is always implied and
	// need not be listed.
	SkillDirKinds map[ir.ResourceKind]bool

	// AggregateFiles names the shared aggregate configuration FILES the target
	// merges into, per scope, as paths relative to that scope's install root.
	// They are real files that coexist with handwritten content, so they are
	// neither components (which are directories) nor ResourceDirs entries (which
	// are per-resource files). A path may name a file inside a component
	// directory (Claude's "hooks/hooks.json") or under a nested prefix
	// (Windsurf's repo-scope ".windsurf/mcp_config.json").
	//
	// This is the single source of truth for the set: materialization reads it to
	// route aggregate reconciliation, and internal/pipeline's managed-path
	// collection reads it so safety backups cover exactly the files that are
	// mutated. Before this field, pipeline.CollectManagedPaths carried a
	// hand-maintained rootConfigFiles map that had no Devin entry, which left
	// hooks.v1.json, config.json, and mcp_config.json outside backup coverage.
	//
	// The authoritative definition of the set is behavioural, not editorial:
	// "every compiled artifact with MergeWithExisting == true, at the
	// output-root-relative path materialization writes it to, in that scope".
	// The declaration below is not the derivation of that set, because the
	// artifact-path-to-output-path mapping lives in internal/materialize's
	// planner and the compilers live in pkg/harness-compilation; a copy of
	// either here would be the parallel implementation this field exists to
	// avoid. For targets with a registered native-layout descriptor (Codex,
	// Devin, Claude) the set is DERIVED from that descriptor instead of being
	// declared here at all. Either way the declaration is VERIFIED against
	// reality by
	// materialize.TestAggregateFilesMatchMaterializedMergeArtifacts, which
	// compiles one resource of every ir.ResourceKind for every target and scope,
	// runs the production planner, and fails when this list and the artifacts
	// actually produced differ in either direction. Bead
	// agent-marketplace-5exj.27 introduced that test after four of six
	// declarations here had drifted from what the compilers emit.
	AggregateFiles AggregateFileSet

	// OwnershipReconciledAggregates declares that this target's AggregateFiles
	// are reconciled through internal/ownership — durable, journaled proof of
	// which entries this tool wrote — instead of through the in-file
	// `_managed_by` marker that materialize's marker-merge path infers ownership
	// from. It is the property internal/materialize's prune fence
	// (aggregateProtectedPaths) keys off: an ownership-reconciled target's
	// aggregates are kept out of the marker-strip walk, because that walk decodes
	// and rewrites the whole file and would destroy user-owned entries the
	// ownership path is responsible for preserving. A target that does NOT set it
	// keeps the marker-merge path, and excluding its aggregates from the walk
	// would silently stop pruning stale managed entries.
	//
	// Ownership is independent of whether generated resources are copied or
	// symlinked. Before bead agent-marketplace-5exj.29, aggregateProtectedPaths
	// incorrectly used the former SymlinkOnly flag as a proxy for this field.
	// Flip only this field when a target moves to internal/ownership (see
	// docs/plans/2026-08-04-marker-to-ownership-migration.md).
	//
	// The former symlink-only restriction reflected the initial Devin delivery
	// scope, not a runtime limitation. Installation mode belongs to the generic
	// materializer and is selected by configuration for every harness target.
	OwnershipReconciledAggregates bool

	// MachineExcludedComponents lists native component names that are part of
	// the project layout but are not consumer-managed in machine scope. Keeping
	// this policy beside the target topology lets planning, cleanup, and backup
	// coverage agree without changing the scope-neutral compiler metadata.
	MachineExcludedComponents map[string]bool

	// MachineComponentDirs renames component directories for machine (user)
	// scope, for targets whose machine layout diverges from their repo layout.
	// Keys are repo-scope component/install-dir names, values are the directory
	// name used under the machine root. It is the single source of truth shared
	// by materialization (which writes those directories) and by backup managed
	// path collection (which must cover exactly what materialization writes).
	MachineComponentDirs map[string]string

	// MachineSkillDirRel was declared and validated but never read by any code
	// path: machineSkillInstallDirRel derives the Windsurf machine skill
	// directory from the install root instead, and the recorded value
	// (".codeium/windsurf/skills") is relative to HOME rather than to the
	// Windsurf machine root, so using it as-is would have written to
	// ~/.codeium/windsurf/.codeium/windsurf/skills. Work package D10 deleted the
	// field rather than wiring up a value that was wrong for its only plausible
	// use; its path validator survives as ValidateRelPath, which now guards the
	// AggregateFiles paths that ARE joined onto an output root.

	// FileCopyKinds lists resource kinds materialized as real files (with an
	// injected `_managed_by` marker) instead of symlinks.
	// TEMPORARY WORKAROUND: Factory's droid CLI does not follow symlinked
	// command files. Remove this field's Factory entry AND the copy path in
	// internal/materialize (syncCommandFiles + injectManagedByFrontmatter +
	// isManagedCommandFile) once Factory resolves symlink loading for commands.
	FileCopyKinds map[ir.ResourceKind]bool
}

// UsesFileCopy reports whether the given resource kind must be materialized as
// a real file (copy) instead of a symlink for this target.
func (t TargetInfo) UsesFileCopy(kind ir.ResourceKind) bool {
	if t.FileCopyKinds == nil {
		return false
	}
	return t.FileCopyKinds[kind]
}

// ComponentDirForScope returns the directory name a component is written to in
// the given scope. Repo scope always uses the component name as-is; machine
// scope applies the MachineComponentDirs rename when one is declared (e.g.
// Windsurf "workflows" -> "global_workflows").
func (t TargetInfo) ComponentDirForScope(component string, machineScope bool) string {
	if !machineScope {
		return component
	}
	if renamed, ok := t.MachineComponentDirs[component]; ok {
		return renamed
	}
	return component
}

// ComponentExcludedForScope reports whether a target component is outside the
// consumer-managed layout for the given scope.
func (t TargetInfo) ComponentExcludedForScope(component string, machineScope bool) bool {
	return machineScope && t.MachineExcludedComponents[component]
}

// ComponentsForScope returns the target's component directory names as they
// appear on disk in the given scope.
func (t TargetInfo) ComponentsForScope(machineScope bool) []string {
	components := make([]string, 0, len(t.Components))
	for _, component := range t.Components {
		if t.ComponentExcludedForScope(component, machineScope) {
			continue
		}
		components = append(components, t.ComponentDirForScope(component, machineScope))
	}
	return components
}

// ExcludedComponentsForScope returns excluded component directory names as
// they would appear on disk in the given scope.
func (t TargetInfo) ExcludedComponentsForScope(machineScope bool) []string {
	if !machineScope {
		return nil
	}
	excluded := make([]string, 0, len(t.MachineExcludedComponents))
	for _, component := range t.Components {
		if t.ComponentExcludedForScope(component, machineScope) {
			excluded = append(excluded, t.ComponentDirForScope(component, machineScope))
		}
	}
	return excluded
}

// UsesSkillDir reports whether a resource kind materializes as a skill
// DIRECTORY for this target. ir.KindSkill always does; other kinds only when
// declared in SkillDirKinds.
func (t TargetInfo) UsesSkillDir(kind ir.ResourceKind) bool {
	if kind == ir.KindSkill {
		return true
	}
	return t.SkillDirKinds[kind]
}

// AggregateFilesForScope returns the target's shared aggregate configuration
// files as they appear, relative to the install root, in the given scope.
func (t TargetInfo) AggregateFilesForScope(machineScope bool) []string {
	if machineScope {
		return t.AggregateFiles.User
	}
	return t.AggregateFiles.Project
}

// ResourceInstallDir returns the install subdirectory for a given resource kind.
// Returns "" if the kind has no dedicated directory for this target.
func (t TargetInfo) ResourceInstallDir(kind ir.ResourceKind) string {
	if t.ResourceDirs == nil {
		return ""
	}
	return t.ResourceDirs[kind]
}

// ResourceSupportedForScope reports whether the target consumer manages the
// resource's directory in the given scope. ResourceInstallDir remains
// scope-neutral so project Rule metadata continues to describe `rules`; this
// method applies the consumer policy only where materialization needs it.
func (t TargetInfo) ResourceSupportedForScope(kind ir.ResourceKind, machineScope bool) bool {
	if !machineScope {
		return true
	}
	installDir := t.ResourceInstallDir(kind)
	if installDir == "" {
		return true
	}
	for _, component := range t.Components {
		if !t.ComponentExcludedForScope(component, machineScope) {
			continue
		}
		installDir = filepath.ToSlash(path.Clean(installDir))
		if installDir == component || strings.HasPrefix(installDir, component+"/") {
			return false
		}
	}
	return true
}

// PathExcludedForScope reports whether a target-relative artifact or manifest
// path belongs to a component excluded by the consumer policy.
func (t TargetInfo) PathExcludedForScope(rel string, machineScope bool) bool {
	if !machineScope {
		return false
	}
	rel = filepath.ToSlash(path.Clean(rel))
	for _, component := range t.ExcludedComponentsForScope(machineScope) {
		if rel == component || strings.HasPrefix(rel, component+"/") {
			return true
		}
	}
	return false
}

// ValidateRelPath checks that a target-metadata relative path is safe to join
// onto an output root. It rejects absolute paths, path-traversal segments,
// volume names, drive letters, and OS-specific separators that could escape the
// target root. Empty values are allowed and mean "not declared".
//
// It was introduced as ValidateMachineSkillDirRel for a field that was never
// read (see the note where TargetInfo.MachineSkillDirRel used to be). Work
// package D10 kept the validation and gave it the caller it needed:
// TargetInfo.AggregateFiles paths, which materialization really does join onto
// an install root before mutating the file there.
func ValidateRelPath(v string) error {
	if v == "" {
		return nil // empty means "not declared"
	}
	if filepath.IsAbs(v) {
		return fmt.Errorf("harness: relative path %q must be a relative path", v)
	}
	if vol := filepath.VolumeName(v); vol != "" {
		return fmt.Errorf("harness: relative path %q contains volume name %q", v, vol)
	}
	// Reject Windows-style drive-letter prefixes (e.g. "C:") on all platforms.
	// filepath.VolumeName only detects these on Windows; on Unix it returns "".
	if containsDriveLetter(v) {
		return fmt.Errorf("harness: relative path %q contains drive letter", v)
	}
	// Check raw value for traversal segments before filepath.Clean resolves them.
	for _, part := range strings.Split(v, "/") {
		if part == ".." {
			return fmt.Errorf("harness: relative path %q contains path traversal", v)
		}
	}
	if strings.Contains(v, "\\") {
		return fmt.Errorf("harness: relative path %q contains backslash separator", v)
	}
	// After cleaning, the path must still be relative and not escape upward.
	cleaned := filepath.Clean(v)
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("harness: relative path %q resolves to absolute path", v)
	}
	if vol := filepath.VolumeName(cleaned); vol != "" {
		return fmt.Errorf("harness: relative path %q resolves to volume-qualified path %q", v, vol)
	}
	// Reject Windows-style drive-letter prefixes after Clean on all platforms.
	if containsDriveLetter(cleaned) {
		return fmt.Errorf("harness: relative path %q resolves to drive-letter path", v)
	}
	return nil
}

func containsDriveLetter(path string) bool {
	return len(path) >= 2 && path[1] == ':' &&
		((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z'))
}

// InstallRoots maps each harness target to its per-scope install-root paths.
// The Machine paths are computed at init time from the user's home directory.
var InstallRoots = MakeInstallRoots()

// TargetInfoMap maps each harness target to its consolidated metadata.
var TargetInfoMap map[hcc.Target]TargetInfo
var targetInfoErr error

func init() {
	TargetInfoMap, targetInfoErr = MakeTargetInfo()
	if targetInfoErr != nil {
		TargetInfoMap = make(map[hcc.Target]TargetInfo)
	}
}

// TargetInfoError reports a target metadata initialization failure, if any.
func TargetInfoError() error {
	return targetInfoErr
}

func MakeInstallRoots() map[hcc.Target]Roots {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}

	definitions := targetmeta.Definitions()
	roots := make(map[hcc.Target]Roots, len(definitions))
	for _, definition := range definitions {
		if definition.PartitionedRoots {
			continue
		}
		roots[hcc.Target(definition.Target)] = Roots{
			Repo:    definition.RepoRoot,
			Machine: filepath.Join(home, definition.UserRoot),
		}
	}
	return roots
}

// appendUnique appends rel to list unless already present, keeping declaration
// order deterministic while collapsing kinds that share one aggregate file
// (Claude skills and subagents both contribute to .claude-plugin/plugin.json).
func appendUnique(list []string, rel string) []string {
	if slices.Contains(list, rel) {
		return list
	}
	return append(list, rel)
}

// appendAggregateFiles collects every aggregate's per-scope file path into
// set, deduplicated via appendUnique and sorted for a stable,
// declaration-independent order. prefix roots each path under a partition
// sub-root (Codex's .agents/.codex split); an empty prefix records the path
// exactly as the descriptor declares it.
func appendAggregateFiles(set *AggregateFileSet, prefix string, aggregates []targetmeta.NativeAggregate) {
	for _, aggregate := range aggregates {
		project, user := aggregate.Project.Path, aggregate.User.Path
		if prefix != "" {
			project = path.Join(prefix, project)
			user = path.Join(prefix, user)
		}
		set.Project = appendUnique(set.Project, project)
		set.User = appendUnique(set.User, user)
	}
	slices.Sort(set.Project)
	slices.Sort(set.User)
}

// targetInfoFromLayout derives a TargetInfo's compiler-topology fields from
// the target's registered native-layout descriptor: Components, ResourceDirs,
// SkillDirKinds, and AggregateFiles. The consumer-side table then cannot
// drift from the layout the compilers emit. Fields that are consumer policy
// rather than compiler topology stay literal at the call site: Roots,
// OwnershipReconciledAggregates, FileCopyKinds, MachineExcludedComponents,
// MachineComponentDirs, ArtifactRootPrefix, and SkillPathPattern, which
// materialize reads to decide frontmatter injection. PlanStrategy defaults to
// PlanSubtree here.
func targetInfoFromLayout(t targetmeta.Target) (TargetInfo, error) {
	layout, ok := targetmeta.LookupNativeLayout(t)
	if !ok {
		return TargetInfo{}, fmt.Errorf("harness: %s native layout is not registered", t)
	}
	if len(layout.Partitions) != 1 {
		return TargetInfo{}, fmt.Errorf("harness: %s native layout has %d partitions; a legacy single-root target declares exactly one", t, len(layout.Partitions))
	}
	partition := layout.Partitions[0]
	if partition.Prefix != "." {
		return TargetInfo{}, fmt.Errorf("harness: %s native layout partition %q has prefix %q; a legacy single-root partition covers the whole root", t, partition.ID, partition.Prefix)
	}
	return targetInfoFromPartition(partition), nil
}

// targetInfoFromPartition derives a TargetInfo's compiler-topology fields from
// one whole-root native-layout partition.
func targetInfoFromPartition(partition targetmeta.NativePartition) TargetInfo {
	info := TargetInfo{
		Components:    slices.Clone(partition.Components),
		PlanStrategy:  PlanSubtree,
		SkillDirKinds: maps.Clone(partition.SkillDirKinds),
	}
	// A kind whose name placeholder is a directory element (skills/%s/SKILL.md)
	// materializes as a resource directory, not under a flat install dir, so it
	// contributes no ResourceDirs entry. The shape predicate is the
	// descriptor's own — validation guarantees it agrees with SkillDirKinds.
	for kind, pattern := range partition.ResourcePaths {
		if targetmeta.IsDirShapedPattern(pattern) {
			continue
		}
		if info.ResourceDirs == nil {
			info.ResourceDirs = make(map[ir.ResourceKind]string)
		}
		info.ResourceDirs[kind] = path.Dir(pattern)
	}
	// A kind with no per-resource path still has an install directory when its
	// aggregate file lives under a component (Claude's hooks/hooks.json keeps
	// hook resources under the hooks/ component). An aggregate at the
	// partition root, like Devin's hooks.v1.json, contributes none.
	// ResourceDirs is scope-neutral, so the fallback derives only when the
	// project and user paths share one directory: a scope-variant aggregate
	// whose directories diverge has no single install dir to describe, and
	// borrowing the project directory would mis-describe user scope.
	for _, aggregate := range partition.Aggregates {
		if _, ok := partition.ResourcePaths[aggregate.Kind]; ok {
			continue
		}
		dir := path.Dir(aggregate.Project.Path)
		if dir == "." || dir != path.Dir(aggregate.User.Path) {
			continue
		}
		if info.ResourceDirs == nil {
			info.ResourceDirs = make(map[ir.ResourceKind]string)
		}
		info.ResourceDirs[aggregate.Kind] = dir
	}
	appendAggregateFiles(&info.AggregateFiles, "", partition.Aggregates)
	return info
}

func MachineRoot(target hcc.Target) (string, bool) {
	roots, ok := MakeInstallRoots()[target]
	return roots.Machine, ok
}

func MakeTargetInfo() (map[hcc.Target]TargetInfo, error) {
	roots := InstallRoots
	codex := hcc.Target(targetmeta.TargetCodex)
	codexInfo, err := codexVirtualTargetInfo()
	if err != nil {
		return nil, err
	}
	claudeInfo, err := targetInfoFromLayout(targetmeta.TargetClaude)
	if err != nil {
		return nil, err
	}
	claudeInfo.Roots = roots[hcc.TargetClaude]
	// Claude deliberately leaves SkillPathPattern unset: materialize
	// activates frontmatter injection when a pattern is declared, and the
	// Claude compiler already renders it.
	devinInfo, err := targetInfoFromLayout(targetmeta.TargetDevin)
	if err != nil {
		return nil, err
	}
	devinInfo.Roots = roots[hcc.TargetDevin]
	devinInfo.SkillPathPattern = mustNativeResourcePattern(targetmeta.TargetDevin, ir.KindSkill)
	// Devin's aggregates go through internal/ownership (see
	// internal/materialize/devin.go): no `_managed_by` marker is ever
	// written into a Devin file, so the marker-strip walk must not touch
	// them. This applies in both file and symlink materialization modes.
	// The original plan's "no real-file fallback" was delivery scope,
	// not a limitation on Devin's ability to read regular files.
	devinInfo.OwnershipReconciledAggregates = true
	// Devin exposes Rules only in its project layout. Keep the compiler's
	// scope-neutral Rule topology intact for project metadata and installation,
	// while making every machine-scope consumer refuse the unsupported
	// component.
	devinInfo.MachineExcludedComponents = map[string]bool{"rules": true}

	m := map[hcc.Target]TargetInfo{
		codex:            codexInfo,
		hcc.TargetClaude: claudeInfo,
		hcc.TargetFactory: {
			Roots:            roots[hcc.TargetFactory],
			Components:       []string{"skills", "commands", "droids", "hooks", "rules", ".factory-plugin"},
			PlanStrategy:     PlanSubtree,
			SkillPathPattern: "skills/%s/SKILL.md",
			ResourceDirs: map[ir.ResourceKind]string{
				ir.KindCommand:  "commands",
				ir.KindSubagent: "droids",
				ir.KindHook:     "hooks",
				ir.KindRule:     "rules",
			},
			// TEMPORARY WORKAROUND: Factory's droid CLI does not follow
			// symlinked command files, so commands must be materialized as
			// real files. Remove this once Factory resolves symlink loading.
			FileCopyKinds: map[ir.ResourceKind]bool{
				ir.KindCommand: true,
			},
			// The Factory compiler emits no .factory-plugin/plugin.json and no
			// settings.json; hooks and MCP servers are its only merge artifacts.
			// settings.json was declared here until bead
			// agent-marketplace-5exj.27 and was backed up despite nothing ever
			// writing it.
			AggregateFiles: AggregateFileSet{
				Project: []string{"hooks/hooks.json", "mcp.json"},
				User:    []string{"hooks/hooks.json", "mcp.json"},
			},
		},
		hcc.TargetCursor: {
			Roots:            roots[hcc.TargetCursor],
			Components:       []string{"skills", "commands", "agents", "hooks", "rules"},
			PlanStrategy:     PlanSubtree,
			SkillPathPattern: "skills/%s/SKILL.md",
			ResourceDirs: map[ir.ResourceKind]string{
				ir.KindCommand:  "commands",
				ir.KindSubagent: "agents",
				ir.KindHook:     "hooks",
				ir.KindRule:     "rules",
			},
			AggregateFiles: AggregateFileSet{
				Project: []string{"hooks/hooks.json", "mcp.json"},
				User:    []string{"hooks/hooks.json", "mcp.json"},
			},
		},
		hcc.TargetWindsurf: {
			Roots:            roots[hcc.TargetWindsurf],
			Components:       []string{"skills", "workflows", "hooks", "rules"},
			PlanStrategy:     PlanWindsurf,
			SkillPathPattern: ".windsurf/skills/%s/SKILL.md",
			// Windsurf only discovers machine-wide slash commands under
			// ~/.codeium/windsurf/global_workflows; skills and hooks keep their
			// names at the Windsurf machine root.
			MachineComponentDirs: map[string]string{
				"workflows": "global_workflows",
			},
			ResourceDirs: map[ir.ResourceKind]string{
				ir.KindCommand: ".windsurf/workflows",
				ir.KindHook:    ".windsurf/hooks",
				ir.KindRule:    ".windsurf/rules",
			},
			// Windsurf's aggregate path is genuinely scope-dependent, like its
			// component directories: the repo-scope install root is the
			// repository itself, so the file lands under .windsurf/, while the
			// machine-scope install root already IS ~/.codeium/windsurf.
			AggregateFiles: AggregateFileSet{
				Project: []string{".windsurf/mcp_config.json"},
				User:    []string{"mcp_config.json"},
			},
		},
		hcc.TargetGemini: {
			Roots:              roots[hcc.TargetGemini],
			Components:         []string{"skills", "commands"},
			ArtifactRootPrefix: ".gemini",
			PlanStrategy:       PlanSubtree,
			SkillPathPattern:   "skills/%s/SKILL.md",
			ResourceDirs: map[ir.ResourceKind]string{
				ir.KindCommand: "commands",
			},
			// MCP servers merge into gemini-extension.json and hooks merge into
			// settings.json (compiled as ".gemini/settings.json", which the
			// ArtifactRootPrefix above strips). Gemini emits no mcp.json at all;
			// it was declared here until bead agent-marketplace-5exj.27, while
			// the settings.json that IS mutated had no backup coverage.
			AggregateFiles: AggregateFileSet{
				Project: []string{"gemini-extension.json", "settings.json"},
				User:    []string{"gemini-extension.json", "settings.json"},
			},
		},
		// Devin's topology is derived from its native-layout descriptor; its
		// aggregates are scope-variant, with project hooks in a root-level
		// hooks.v1.json file and user hooks under config.json's `hooks`
		// member.
		hcc.TargetDevin: devinInfo,
	}

	// Validate every declared aggregate path before it is ever joined onto an
	// output root. These paths name files this process MUTATES, so a traversal
	// segment here would let target metadata write outside the install root.
	for target, info := range m {
		for _, scope := range []bool{false, true} {
			for _, rel := range info.AggregateFilesForScope(scope) {
				if rel == "" {
					return nil, fmt.Errorf("target %q: empty aggregate file path", target)
				}
				if err := ValidateRelPath(rel); err != nil {
					return nil, fmt.Errorf("target %q: aggregate file: %w", target, err)
				}
			}
		}
	}

	// Verify every key in InstallRoots has a corresponding TargetInfo.
	// If a future target is added to InstallRoots but omitted here,
	// materialize would silently fall back to incomplete defaults.
	for target := range InstallRoots {
		if _, ok := m[target]; !ok {
			return nil, fmt.Errorf("harness: TargetInfoMap missing entry for target %q present in InstallRoots", target)
		}
	}

	// Verify every key in TargetInfoMap also exists in InstallRoots.
	// A target with no InstallRoots entry would have zero-value Roots,
	// causing materialize to write to empty or relative paths.
	for target := range m {
		if _, ok := InstallRoots[target]; !ok {
			definition, declared := targetmeta.Lookup(string(target))
			if declared && definition.PartitionedRoots {
				continue
			}
			return nil, fmt.Errorf("harness: InstallRoots missing entry for target %q present in TargetInfoMap", target)
		}
	}

	return m, nil
}
