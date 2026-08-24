// Package harness maps harness target names to their install-root paths
// for both repo-scoped and machine-scoped skill materialization.
//
// In Phase 0 this package is only used by the catalog layer and list/TUI
// for informational output. Phase 3 (internal/materialize) will consume
// it to drive hcc.Materialize into the correct per-target output directory.
package harness

import (
	"fmt"
	"os"
	"path/filepath"
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
// root. They mirror the unexported constants in
// pkg/harness-compilation/compile/devin (which are that package's internal path
// contract, not public API); materialization needs its own copy to route
// reconciliation, and TestDevinAggregateNamesMatchCompiledArtifactPaths pins
// them against the paths the compiler really emits so the two cannot drift.
const (
	// DevinProjectHooksFile holds the hook object directly at project scope.
	DevinProjectHooksFile = devinProjectHooksFile
	// DevinUserConfigFile carries hooks under its `hooks` member at user scope.
	DevinUserConfigFile = devinUserConfigFile
	// DevinMcpConfigFile holds mcpServers at the same relative path in both scopes.
	DevinMcpConfigFile = devinMcpConfigFile
)

const (
	devinProjectHooksFile = "hooks.v1.json"
	devinUserConfigFile   = "config.json"
	devinMcpConfigFile    = "mcp_config.json"
)

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
	// avoid. Instead the declaration is VERIFIED against reality by
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
	// This field and SymlinkOnly are INDEPENDENT properties that Devin happens to
	// be the only holder of today. "Aggregates are reconciled through proven
	// ownership" says nothing about how per-resource artifacts are materialized,
	// and "resources must be symlinks" says nothing about how shared files are
	// merged. Before bead agent-marketplace-5exj.29, aggregateProtectedPaths used
	// SymlinkOnly as a proxy for this field, which was correct only by
	// coincidence: the first target migrated to proven ownership without also
	// being symlink-only would have fallen back into the marker path it was
	// migrated away from. Do not re-conflate them — flip this field, and only
	// this field, when a target moves to internal/ownership (see
	// docs/plans/2026-08-04-marker-to-ownership-migration.md).
	OwnershipReconciledAggregates bool

	// SymlinkOnly forbids materializing this target's resources as real file
	// copies. Devin sets it because the promoted plan states "Skills use directory
	// symlinks and flat subagents use file symlinks. Both shapes are release
	// requirements ... There is no real-file fallback in this increment." A
	// SymlinkOnly target refuses config.MaterializationModeFile outright rather
	// than silently degrading to copies.
	//
	// This is about per-resource artifact SHAPE only. It is independent of
	// OwnershipReconciledAggregates (see that field): nothing about refusing file
	// copies implies anything about how shared aggregate files are merged, and
	// vice versa.
	SymlinkOnly bool

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

// ComponentsForScope returns the target's component directory names as they
// appear on disk in the given scope.
func (t TargetInfo) ComponentsForScope(machineScope bool) []string {
	components := make([]string, len(t.Components))
	for i, component := range t.Components {
		components[i] = t.ComponentDirForScope(component, machineScope)
	}
	return components
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

	roots := make(map[hcc.Target]Roots, len(targetmeta.Definitions()))
	for _, definition := range targetmeta.Definitions() {
		roots[hcc.Target(definition.Target)] = Roots{
			Repo:    definition.RepoRoot,
			Machine: filepath.Join(home, definition.UserRoot),
		}
	}
	return roots
}

func MachineRoot(target hcc.Target) (string, bool) {
	roots, ok := MakeInstallRoots()[target]
	return roots.Machine, ok
}

func MakeTargetInfo() (map[hcc.Target]TargetInfo, error) {
	roots := InstallRoots

	m := map[hcc.Target]TargetInfo{
		hcc.TargetClaude: {
			Roots:        roots[hcc.TargetClaude],
			Components:   []string{"skills", "commands", "agents", "hooks", ".claude-plugin"},
			PlanStrategy: PlanSubtree,
			ResourceDirs: map[ir.ResourceKind]string{
				ir.KindCommand:  "commands",
				ir.KindSubagent: "agents",
				ir.KindHook:     "hooks",
			},
			// Skills and subagents both contribute entries to
			// .claude-plugin/plugin.json, hooks merge into hooks/hooks.json, and
			// MCP servers merge into .mcp.json. The same three files are written
			// in both scopes.
			AggregateFiles: AggregateFileSet{
				Project: []string{".claude-plugin/plugin.json", ".mcp.json", "hooks/hooks.json"},
				User:    []string{".claude-plugin/plugin.json", ".mcp.json", "hooks/hooks.json"},
			},
		},
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
		hcc.TargetDevin: {
			Roots: roots[hcc.TargetDevin],
			// Devin project hooks are a single aggregate FILE (hooks.v1.json) at
			// the root of the target, not a "hooks" directory, so "hooks" must
			// not be listed as a prunable component.
			Components:       []string{"skills", "agents"},
			PlanStrategy:     PlanSubtree,
			SkillPathPattern: "skills/%s/SKILL.md",
			ResourceDirs: map[ir.ResourceKind]string{
				// Subagents are flat files (agents/<name>.md), which is exactly
				// what ResourceDirs models.
				ir.KindSubagent: "agents",
				// KindCommand is deliberately absent from ResourceDirs: a Devin
				// command compiles to a skill DIRECTORY (skills/<name>/SKILL.md),
				// while ResourceDirs means "<dir>/<name>.<ext>" (see
				// resourceArtifactMatches in internal/materialize, which rejects
				// nested artifact paths). D10 expresses it as a SkillDirKinds entry
				// instead, which routes it through the directory-symlink path that
				// already materializes ir.KindSkill.
				//
				// KindHook is deliberately absent: hooks are merged into the
				// aggregate hooks.v1.json / config.json#hooks files, not into a
				// per-hook install directory. D10 expresses those in AggregateFiles.
			},
			// A Devin command IS a skill directory carrying
			// `x-agent-marketplace-kind: command`, so it is materialized as a
			// directory symlink at skills/<name>/, exactly like a native skill.
			// That is also why "skills" needs prune coverage for entries owned by
			// commands: syncSkillLinks prunes any managed symlink under skills/
			// that is not desired this run, regardless of which kind created it.
			SkillDirKinds: map[ir.ResourceKind]bool{ir.KindCommand: true},
			// Plan, resource mapping: "Hook | Project: hooks.v1.json; user:
			// config.json#hooks" and "MCP server | mcp_config.json". MCP is at the
			// same relative path under BOTH roots.
			AggregateFiles: AggregateFileSet{
				Project: []string{devinProjectHooksFile, devinMcpConfigFile},
				User:    []string{devinUserConfigFile, devinMcpConfigFile},
			},
			// Devin's aggregates go through internal/ownership (see
			// internal/materialize/devin.go): no `_managed_by` marker is ever
			// written into a Devin file, so the marker-strip walk must not touch
			// them. Independent of SymlinkOnly below; Devin is currently the only
			// target holding either property, which is coincidence, not
			// implication.
			OwnershipReconciledAggregates: true,
			// "There is no real-file fallback in this increment" (plan, target
			// contract): a Devin resource is a symlink or it is a failure.
			SymlinkOnly: true,
		},
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
			return nil, fmt.Errorf("harness: InstallRoots missing entry for target %q present in TargetInfoMap", target)
		}
	}

	return m, nil
}
