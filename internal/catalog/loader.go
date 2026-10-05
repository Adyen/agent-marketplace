package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/skill"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type LoadMode int

const (
	LoadModeRefresh LoadMode = iota
	LoadModeCached
	LoadModeInspect
	// LoadModeCachedNoFetch loads only registries whose cache directories are
	// already present on disk. It never runs git clone/fetch or writes to the
	// cache, so it is safe for read-only/teardown paths such as deinit --dry-run.
	LoadModeCachedNoFetch
)

func (m LoadMode) String() string {
	switch m {
	case LoadModeRefresh:
		return "refresh"
	case LoadModeCached:
		return "cached"
	case LoadModeInspect:
		return "inspect"
	case LoadModeCachedNoFetch:
		return "cached_no_fetch"
	}
	return "unknown"
}

type LoadedCatalog struct {
	Entries     []Entry
	Malformed   []skill.MalformedSkill
	Collections []collection.Collection
	Resources   map[string]ir.Resource
	Diagnostics []diag.Diagnostic
	// SubBundles holds per-sub-bundle results when recursive discovery was
	// triggered for a meta-directory. Keyed by sub-bundle absolute path.
	// When non-empty, discoverAll splits them into separate catalogs (each
	// with its own catalog key) instead of storing a single merged catalog.
	SubBundles map[string]LoadedCatalog
	// Errors holds per-sub-bundle errors encountered during recursive
	// discovery. Non-nil when at least one sub-bundle failed to load.
	// The merged catalog still contains entries from successful sub-bundles.
	Errors []error
}

// EntriesByKey extracts the catalog entry slices from a LoadedCatalog map for
// consumption by resolver.Resolve.
func EntriesByKey(catalogs map[string]LoadedCatalog) map[string][]Entry {
	entries := make(map[string][]Entry, len(catalogs))
	for key, loaded := range catalogs {
		entries[key] = loaded.Entries
	}
	return entries
}

// MalformedByKey extracts the per-registry malformed sets from a LoadedCatalog
// map. Resolve needs it keyed the same way as the entries so a selection that is
// absent from one registry's entries can be correlated with that registry's
// validation failures — see resolver.WithMalformed.
func MalformedByKey(catalogs map[string]LoadedCatalog) map[string][]skill.MalformedSkill {
	malformed := make(map[string][]skill.MalformedSkill, len(catalogs))
	for key, loaded := range catalogs {
		if len(loaded.Malformed) == 0 {
			continue
		}
		malformed[key] = loaded.Malformed
	}
	return malformed
}

type LoadResult struct {
	Config           *config.Config
	Catalogs         map[string]LoadedCatalog
	ResourceCatalogs map[string]map[string]ir.Resource
	Collections      []collection.Collection
	Malformed        []skill.MalformedSkill
	MalformedCount   int
	RefreshAfterLoad bool
	Errors           []error
}

type CloneOrPullFunc func(url, ref string) (string, error)

type ExactSnapshotFunc func(entry config.RegistryEntry) (snapshotPath, snapshotIdentity string, err error)

type DiscoverFunc func(dir, sourceHarness string) (LoadedCatalog, error)

type LoadOption func(*loadConfig)

type loadConfig struct {
	cloneOrPull    CloneOrPullFunc
	exactSnapshot  ExactSnapshotFunc
	discover       DiscoverFunc
	localConfigDir string // base dir for resolving local-config entries' relative paths
}

type fetchJob struct {
	url       string
	ref       string
	cachePath string
	warmCache bool
	err       error
}

type loadRequest struct {
	entry           config.RegistryEntry
	catalogKey      string
	basePath        string
	catalogBasePath string
	job             *fetchJob
}

func WithCloneOrPull(fn CloneOrPullFunc) LoadOption {
	return func(c *loadConfig) { c.cloneOrPull = fn }
}

func WithExactSnapshots(fn ExactSnapshotFunc, discover ...DiscoverFunc) LoadOption {
	return func(c *loadConfig) {
		c.exactSnapshot = fn
		if len(discover) > 0 {
			c.discover = discover[0]
		}
	}
}

func WithVerifiedExactSnapshots(fn ExactSnapshotFunc, verified map[string]map[string]*Catalog) LoadOption {
	return func(c *loadConfig) {
		c.exactSnapshot = fn
		fallback := c.discover
		c.discover = func(path, sourceHarness string) (LoadedCatalog, error) {
			catalogs := verified[filepath.Clean(path)]
			loaded := catalogs[sourceHarness]
			if loaded == nil && sourceHarness == "" && len(catalogs) == 1 {
				for _, candidate := range catalogs {
					loaded = candidate
				}
			}
			if loaded != nil {
				slog.Debug("reusing verified source catalog", "path", path, "source_harness", loaded.SourceHarness)
				return loaded.Discover(path, sourceHarness)
			}
			return fallback(path, sourceHarness)
		}
	}
}

func WithDiscoverFunc(fn DiscoverFunc) LoadOption {
	return func(c *loadConfig) { c.discover = fn }
}

// WithLocalConfigDir sets the base directory for resolving relative paths
// in registry entries that originate from the local config layer
// (Source == RepoPersonal). Entries from other layers continue to use
// the repoRoot passed to LoadRegistries. This enables a monorepo
// subdirectory's local config to use relative paths that resolve against
// the subdirectory, not the parent repo root.
func WithLocalConfigDir(dir string) LoadOption {
	return func(c *loadConfig) { c.localConfigDir = dir }
}

func Discover(dir, sourceHarness string) (LoadedCatalog, error) {
	return DiscoverWithOptions(dir, sourceHarness, IngestOptions{})
}

func DiscoverWithOptions(dir, sourceHarness string, opts IngestOptions) (loaded LoadedCatalog, err error) {
	source, err := fsboundary.OpenSourceFS(dir)
	if err != nil {
		return LoadedCatalog{}, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	return DiscoverSourceWithOptions(source, dir, sourceHarness, opts)
}

func DiscoverSourceWithOptions(source *fsboundary.SourceFS, dir, sourceHarness string, opts IngestOptions) (LoadedCatalog, error) {
	cat, err := LoadSourceWithOptions(source, dir, sourceHarness, opts)
	if fsboundary.IsSourceRefusal(err) {
		return LoadedCatalog{}, fmt.Errorf("catalog source refused: %w", err)
	}
	if err != nil {
		// Load failed — likely no recognizable layout at this directory.
		// Before falling back to DiscoverSkills, check for sub-bundles.
		// If subdirectories contain bundle roots, load them recursively
		// instead of relying on the legacy skill-only discovery.
		subBundles := scanSourceSubBundles(source, dir, sourceHarness, opts)
		if len(subBundles) > 0 {
			return discoverSubBundles(subBundles)
		}

		// No sub-bundles and no recognizable bundle format: return the
		// original load error so the caller knows discovery failed.
		return LoadedCatalog{}, err
	}
	return cat.discoverSourceWithOptions(source, dir, sourceHarness, opts)
}

func (cat *Catalog) Discover(dir, sourceHarness string) (loaded LoadedCatalog, err error) {
	source, err := fsboundary.OpenSourceFS(dir)
	if err != nil {
		return LoadedCatalog{}, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	return cat.discoverSourceWithOptions(source, dir, sourceHarness, IngestOptions{})
}

func (cat *Catalog) discoverSourceWithOptions(source *fsboundary.SourceFS, dir, sourceHarness string, opts IngestOptions) (LoadedCatalog, error) {
	if cat.sourceRefusal != nil {
		return LoadedCatalog{}, fmt.Errorf("catalog source refused: %w", cat.sourceRefusal)
	}
	// Vacant-lot condition: zero entries and no warning/error diagnostics.
	// HCC scanned the directory, found nothing, and had nothing to complain
	// about. This can happen when the directory is a meta-directory containing
	// multiple independent bundle subdirectories. Scan for sub-bundles and
	// recurse into them.
	if len(cat.Entries) == 0 && !hasDiscoveryBoundaryDiagnostics(cat.Diagnostics) {
		subBundles := scanSourceSubBundles(source, dir, sourceHarness, opts)
		if len(subBundles) > 0 {
			if hasWarningOrHigher(cat.Diagnostics) {
				subBundles = append([]subBundle{{path: dir, catalog: cat, malformed: normalizeMalformedPaths(source, dir, malformedFromDiagnostics(cat.Diagnostics))}}, subBundles...)
			}
			return discoverSubBundles(subBundles)
		}
	}

	malformed := malformedFromDiagnostics(cat.Diagnostics)

	// Make malformed paths absolute so the TUI can read the SKILL.md
	// file when the user opens the detail pane. Diagnostics use paths
	// relative to the bundle root (e.g. "skills/broken-skill"), but the
	// TUI's ReadSkillMarkdown needs an absolute path to stat/read.
	// Resolve dir to an absolute, canonical root and verify each
	// malformed path stays within that root to prevent ../ escapes.
	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return LoadedCatalog{}, fmt.Errorf("resolve discovery root %q: %w", dir, err)
	}
	if _, err := source.Stat("."); err != nil {
		return LoadedCatalog{}, fmt.Errorf("inspect discovery root %q: %w", rootAbs, err)
	}
	for i := range malformed {
		if malformed[i].Path == "" {
			continue
		}
		p := malformed[i].Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(rootAbs, p)
		}
		p = filepath.Clean(p)

		// Fast lexical containment check against the configured root.
		relAbs, err := filepath.Rel(rootAbs, p)
		if err != nil || relAbs == ".." || strings.HasPrefix(relAbs, ".."+string(filepath.Separator)) {
			malformed[i].Path = ""
			continue
		}

		// Resolve symlinks and verify the real path stays inside the real root.
		// This catches symlinks that lexical cleaning cannot see.
		if _, ok := resolveMalformedPath(source, rootAbs, p); !ok {
			malformed[i].Path = ""
			continue
		}

		// Store the unresolved absolute path: it is safe because both lexical
		// and resolved containment passed, and tests/callers expect paths
		// relative to the originally supplied root (which may itself be a
		// symlink, e.g. /var on macOS).
		malformed[i].Path = p
		// Recover the authored frontmatter name while the file is still
		// reachable. A resource that failed to load is keyed by its directory
		// here, but bundle.parseSkillFromFS keys loaded resources by their
		// frontmatter `name:`, so a config selecting the authored name can only
		// be correlated with this failure through the recovered name. Without
		// it, a name/directory mismatch looks exactly like an upstream rename.
		if declared := skill.DeclaredNameFromFS(source, filepath.ToSlash(relAbs)); declared != "" && declared != malformed[i].DirName {
			malformed[i].DeclaredName = declared
		}
	}

	// When the directory is itself a bundle (has root-level resources) but
	// also contains subdirectories that look like independent bundles, the
	// sub-bundles are NOT discovered — the directory is treated as a single
	// bundle. Emit a warning so the user knows to restructure: move root
	// resources into a sub-bundle (e.g. common/) so the parent becomes a
	// pure container and recursive discovery can find all sub-bundles.
	//
	// However, suppress the warning when the loaded catalog already covers
	// the sub-bundle directories. This happens when a recognized harness
	// workspace (e.g. .factory/skills/) has organizational groupings
	// (e.g. go/, python/) that contain skill subdirectories. HCC already
	// discovers all skills within the workspace, so the sub-bundles don't
	// contain any missed resources — the warning would be a false positive.
	if len(cat.Entries) > 0 {
		var ignored []string
		for _, subBundle := range scanSourceSubBundles(source, dir, sourceHarness, opts) {
			if !subBundle.coveredBy(cat.Entries, dir) {
				ignored = append(ignored, subBundle.path)
			}
		}
		if len(ignored) > 0 {
			malformed = append(malformed, skill.MalformedSkill{
				DirName: "catalog.discover.sub-bundles-ignored",
				Path:    dir,
				Error:   fmt.Errorf("directory has both root-level resources and %d sub-bundle(s) (e.g. %s); sub-bundles are ignored. Move root resources into a sub-bundle (e.g. common/) so the parent is a pure container for recursive discovery", len(ignored), filepath.Base(ignored[0])),
			})
		}
	}

	if refusal := source.Refusal(); refusal != nil {
		return LoadedCatalog{}, fmt.Errorf("catalog source refused: %w", refusal)
	}
	return LoadedCatalog{
		Entries:     slices.Clone(cat.Entries),
		Malformed:   malformed,
		Resources:   Resources(cat),
		Diagnostics: cat.Diagnostics,
	}, nil
}

// hasWarningOrHigher reports whether any diagnostic has severity Warning or
// Error. Severity values are ordered: Error < Warning < Info.
func hasWarningOrHigher(diagnostics []diag.Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity <= diag.SeverityWarning {
			return true
		}
	}
	return false
}

func hasDiscoveryBoundaryDiagnostics(diagnostics []diag.Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == diag.SeverityWarning {
			switch d.Code {
			case diag.CodeIngestClaudeUnconsumedConfig, diag.CodeIngestFactoryUnconsumedConfig,
				diag.CodeIngestCursorUnconsumedConfig, diag.CodeIngestGeminiUnconsumedConfig,
				diag.CodeIngestWindsurfUnconsumedConfig, diag.CodeIngestDevinUnconsumedConfig,
				diag.CodeIngestCodexUnconsumedConfig, CodeRequiresInvalid:
				continue
			}
		}
		if d.Severity.AtLeastAsSevereAs(diag.SeverityWarning) {
			return true
		}
	}
	return false
}

// catalogCoversSubBundles reports whether the loaded catalog entries already
// cover the supplied sub-bundle paths. Each path may identify a directory
// or an individual resource file. Warning suppression supplies every source
// file from a candidate, so partial coverage cannot hide missed resources.
// When every supplied path is covered by the loaded catalog, the
// sub-bundles-ignored warning is a false positive and should be suppressed.
func catalogCoversSubBundles(entries []Entry, dir string, subBundles []string) bool {
	root, err := filepath.Abs(dir)
	if err != nil {
		return false // Can't resolve the discovery root; be conservative.
	}
	root = filepath.Clean(root)

	// Normalize each sub-bundle against the discovery root and reject paths
	// outside it. This keeps coverage checks from treating an unrelated path as
	// a sub-bundle, including when callers mix absolute and relative forms.
	normalizedSubBundles := make([]string, 0, len(subBundles))
	for _, subBundle := range subBundles {
		normalized, ok := normalizeCatalogPath(root, subBundle)
		if !ok {
			return false
		}
		normalizedSubBundles = append(normalizedSubBundles, normalized)
	}

	// For each sub-bundle, check if at least one entry has a source path
	// within it. Entry.Path comes from ir.SourceRef.File and may be relative
	// to the bundle root (dir) or absolute (as returned by Load).
	covered := make(map[int]bool, len(normalizedSubBundles))
	for _, entry := range entries {
		p, ok := normalizeCatalogPath(root, entry.Path)
		if !ok {
			continue // An entry outside the root cannot prove coverage.
		}
		for i, subBundle := range normalizedSubBundles {
			if !covered[i] && catalogPathWithin(subBundle, p) {
				covered[i] = true
			}
		}
	}
	for _, c := range covered {
		if !c {
			return false // At least one sub-bundle has no entries from it.
		}
	}
	return len(covered) == len(normalizedSubBundles)
}

// normalizeCatalogPath returns p as a clean absolute path under root. It
// rejects empty paths and paths that escape root lexically.
func normalizeCatalogPath(root, p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	if !catalogPathWithin(root, p) {
		return "", false
	}
	return p, true
}

// catalogPathWithin reports whether candidate is root itself or a descendant
// of root, using component-aware path containment.
func catalogPathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// subPathRel reports whether path starts with prefix as a component-wise
// prefix. Both are slash-separated relative paths. A path like
// "go/go-conventions/SKILL.md" has prefix "go" as a component prefix.
func subPathRel(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if len(path) == len(prefix) {
		return true
	}
	return path[len(prefix)] == '/'
}

// scanForSubBundles walks downward from dir, looking for subdirectories that
// contain standard bundle roots (skills/, commands/, agents/, hooks/, mcp.json,
// or any HCC Autodetect markers). Candidates are loaded once and retained only
// when they yield resources, warning/error diagnostics, or a load error.
// Retained candidates own their children; empty candidates are traversed.
// Results retain the load outcome and are sorted deterministically by path.
func scanForSubBundles(dir, sourceHarness string, options ...IngestOptions) []subBundle {
	source, err := fsboundary.OpenSourceFS(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = source.Close() }()
	return scanSourceSubBundles(source, dir, sourceHarness, options...)
}

func scanSourceSubBundles(source *fsboundary.SourceFS, dir, sourceHarness string, options ...IngestOptions) []subBundle {
	var bundles []subBundle
	var opts IngestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	scanSubBundlesWalk(source, dir, sourceHarness, &bundles, opts)
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].path < bundles[j].path })
	return bundles
}

type subBundle struct {
	path      string
	catalog   *Catalog
	err       error
	malformed []skill.MalformedSkill
}

func loadSubBundle(path, sourceHarness string, options ...IngestOptions) subBundle {
	var opts IngestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	cat, err := LoadWithOptions(path, sourceHarness, opts)
	return subBundle{path: path, catalog: cat, err: err}
}

func (b subBundle) coveredBy(entries []Entry, dir string) bool {
	if b.err != nil || b.catalog.SourceRefusal() != nil || hasDiscoveryBoundaryDiagnostics(b.catalog.Diagnostics) {
		return false
	}
	paths := make([]string, 0, len(b.catalog.Entries))
	for _, entry := range b.catalog.Entries {
		if entry.Path == "" {
			return false
		}
		paths = append(paths, entry.Path)
	}
	return catalogCoversSubBundles(entries, dir, paths)
}

func scanSubBundlesWalk(source *fsboundary.SourceFS, dir, sourceHarness string, bundles *[]subBundle, opts IngestOptions) {
	entries, err := source.ReadDir(".")
	if err != nil {
		*bundles = append(*bundles, subBundle{path: dir, err: err})
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || skippedScanDirs(entry.Name()) {
			continue
		}
		subDir := filepath.Join(dir, entry.Name())
		subSource, err := source.Sub(entry.Name())
		if err != nil {
			*bundles = append(*bundles, subBundle{path: subDir, err: err})
			continue
		}
		func() {
			defer func() {
				if err := subSource.Close(); err != nil {
					*bundles = append(*bundles, subBundle{path: subDir, err: err})
				}
			}()
			if hasBundleRootFS(subSource) || subSource.Refusal() != nil {
				cat, err := LoadSourceWithOptions(subSource, subDir, sourceHarness, opts)
				candidate := subBundle{path: subDir, catalog: cat, err: err}
				if cat != nil {
					candidate.malformed = normalizeMalformedPaths(subSource, subDir, malformedFromDiagnostics(cat.Diagnostics))
					cat.sourceRefusal = subSource.Refusal()
				}
				if candidate.err != nil || candidate.catalog.SourceRefusal() != nil || len(candidate.catalog.Entries) > 0 || hasDiscoveryBoundaryDiagnostics(candidate.catalog.Diagnostics) {
					*bundles = append(*bundles, candidate)
					return
				}
				if hasWarningOrHigher(candidate.catalog.Diagnostics) {
					*bundles = append(*bundles, candidate)
					slog.Debug("traversing diagnostic-only bundle container", "path", subDir)
				} else {
					slog.Debug("ignoring empty bundle candidate", "path", subDir)
				}
			} else if hasRootSkill(subSource) {
				return
			}
			scanSubBundlesWalk(subSource, subDir, sourceHarness, bundles, opts)
		}()
	}
}

// skippedScanDirs identifies directory names that should never be treated as
// sub-bundles or descended into during recursive bundle scanning. Includes:
//   - VCS and dependency directories (same as skill.DiscoverSkills skip list)
//   - Harness workspace directories (.factory/, .claude/, .cursor/, etc.)
//     because these are internal bundle structure, not independent sub-bundles.
//   - Harness plugin manifest directories (.factory-plugin/, etc.) for the
//     same reason.
//   - Harness resource directories (skills/, commands/, agents/, hooks/)
//     because these are part of a recognized bundle's layout, not independent
//     sub-bundles. A bare skills/ directory that IS an independent bundle is
//     handled by hasMultiSkillSubdirs only when AutodetectStrict returns false.
func skippedScanDirs(name string) bool {
	_, repository := repositoryScanExclusions[name]
	_, layout := bundleLayoutDirs[name]
	return repository || layout
}

var repositoryScanExclusions = map[string]struct{}{
	".git":         {},
	".github":      {},
	"node_modules": {},
	".cache":       {},
	"test":         {},
	"tests":        {},
	"__tests__":    {},
}

var bundleLayoutDirs = map[string]struct{}{
	".factory":         {},
	".factory-plugin":  {},
	".claude":          {},
	".claude-plugin":   {},
	".cursor":          {},
	".cursor-plugin":   {},
	".agents":          {},
	".devin":           {},
	".windsurf":        {},
	".gemini":          {},
	"gemini-extension": {},
	"skills":           {},
	"commands":         {},
	"agents":           {},
	"hooks":            {},
}

func hasRootSkill(src fs.FS) bool {
	info, err := fs.Stat(src, "SKILL.md")
	return err == nil && !info.IsDir()
}

// hasBundleRoot reports whether dir contains any standard root directory or file
// that marks it as a harness bundle. Uses HCC's AutodetectStrict to cover all
// harness-specific markers (plugin manifests, workspace directories, and bare
// plugin form with skills/commands/agents/hooks/mcp.json). AutodetectStrict
// excludes the bare-skills-directory form to prevent individual skill dirs
// from being treated as sub-bundles, but we re-enable the multi-skill subdir
// case (dir/*/SKILL.md) so container directories with multiple skills are
// still recognized.
func hasBundleRoot(dir string) (bool, error) {
	srcFS, err := fsboundary.OpenSourceFS(dir)
	if err != nil {
		if errors.Is(err, fsboundary.ErrUnsafePath) || errors.Is(err, fsboundary.ErrUnsafeFile) {
			return false, fmt.Errorf("catalog source refused: %w", err)
		}
		return false, nil
	}
	defer func() { _ = srcFS.Close() }()
	matched := hasBundleRootFS(srcFS)
	// Marker probes that the captured source refused are not absent markers.
	if refusal := srcFS.Refusal(); refusal != nil {
		return false, fmt.Errorf("catalog source refused: %w", refusal)
	}
	return matched, nil
}

func hasBundleRootFS(srcFS fs.FS) bool {
	_, strictMatch := ingest.AutodetectStrict(srcFS)

	// A root-level SKILL.md identifies a skill directory. Preserve an explicit
	// plugin or canonical workspace marker when one is present, but do not let
	// a nested bare asset directory (for example, rules/) promote the skill to
	// a sub-bundle.
	if hasRootSkill(srcFS) {
		return strictMatch && hasExplicitBundleMarker(srcFS)
	}
	if strictMatch {
		return true
	}
	// AutodetectStrict skips bare-skills-directory form; check if this
	// directory contains immediate subdirectories with SKILL.md (multi-skill
	// container).
	return hasMultiSkillSubdirs(srcFS)
}

// hasExplicitBundleMarker recognizes the explicit plugin manifests and
// canonical workspace roots used by HCC. Bare root asset directories are
// intentionally omitted: they are ambiguous when SKILL.md is at the root.
func hasExplicitBundleMarker(src fs.FS) bool {
	manifestMarkers := []string{
		".claude-plugin/plugin.json",
		".factory-plugin/plugin.json",
		".cursor-plugin/plugin.json",
		"gemini-extension.json",
	}
	for _, marker := range manifestMarkers {
		if info, err := fs.Stat(src, marker); err == nil && !info.IsDir() {
			return true
		}
	}

	workspaceMarkers := []string{
		".claude",
		".factory",
		".cursor",
		".windsurf",
		".gemini",
		".agents",
		".devin",
	}
	for _, marker := range workspaceMarkers {
		if info, err := fs.Stat(src, marker); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// hasMultiSkillSubdirs reports whether the filesystem contains at least one
// immediate subdirectory with a SKILL.md file. This detects the
// "bare skills directory" container form (e.g. teams/dev/ containing
// review/SKILL.md, brainstorming/SKILL.md) without treating a single
// root-level SKILL.md as a bundle root.
//
// Known harness resource directories (skills/, commands/, agents/, etc.)
// are excluded because they are part of the bundle structure, not
// independent sub-bundles.
func hasMultiSkillSubdirs(src fs.FS) bool {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Skip harness resource directories — they're part of the bundle,
		// not independent sub-bundles containing skills.
		if skippedScanDirs(e.Name()) {
			continue
		}
		if _, err := fs.ReadFile(src, e.Name()+"/SKILL.md"); err == nil {
			return true
		}
	}
	return false
}

// resolveMalformedPath checks whether p resolves to a path inside rootReal and
// returns the resolved absolute path. It returns "" and false if p escapes the
// discovery root, does not exist, or cannot be resolved.
func resolveMalformedPath(source *fsboundary.SourceFS, rootReal, p string) (string, bool) {
	rel, err := filepath.Rel(rootReal, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if _, err := source.Stat(filepath.ToSlash(rel)); err != nil {
		return "", false
	}
	return p, true
}

// normalizeMalformedPaths makes malformed skill paths absolute relative to
// root, matching the normalization that Discover() applies for the
// non-sub-bundle path. Without this, recursively discovered broken skills
// keep relative paths (e.g. "skills/bad-skill") while the non-recursive
// path normalizes them for the TUI.
func normalizeMalformedPaths(source *fsboundary.SourceFS, root string, malformed []skill.MalformedSkill) []skill.MalformedSkill {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return malformed
	}
	if _, err := source.Stat("."); err != nil {
		return malformed
	}
	for i := range malformed {
		if malformed[i].Path == "" {
			continue
		}
		p := malformed[i].Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(rootAbs, p)
		}
		p = filepath.Clean(p)

		relAbs, err := filepath.Rel(rootAbs, p)
		if err != nil || relAbs == ".." || strings.HasPrefix(relAbs, ".."+string(filepath.Separator)) {
			malformed[i].Path = ""
			continue
		}

		if _, ok := resolveMalformedPath(source, rootAbs, p); !ok {
			malformed[i].Path = ""
			continue
		}

		malformed[i].Path = p
	}
	return malformed
}

// discoverSubBundles merges the retained sub-bundle load results. It populates
// SubBundles with per-sub-bundle results so discoverAll can create separate
// catalogs for each. Entries/Malformed/Resources contain the merged view
// for backward compatibility with callers that don't check SubBundles.
// Name collisions across sub-bundles are resolved by first-wins (sub-bundles
// are processed in sorted directory order).
func discoverSubBundles(subBundles []subBundle) (LoadedCatalog, error) {
	var merged LoadedCatalog
	var errs []error
	seen := make(map[string]struct{})
	subCats := make(map[string]LoadedCatalog, len(subBundles))

	for _, candidate := range subBundles {
		subDir, subCat, subErr := candidate.path, candidate.catalog, candidate.err
		if subCat != nil {
			subErr = errors.Join(subErr, subCat.SourceRefusal())
		}
		if subErr != nil {
			errs = append(errs, fmt.Errorf("sub-bundle %q: %w", subDir, subErr))
			continue
		}

		subLoaded := LoadedCatalog{
			Entries:     subCat.Entries,
			Malformed:   candidate.malformed,
			Resources:   Resources(subCat),
			Diagnostics: subCat.Diagnostics,
		}
		subCats[subDir] = subLoaded

		for _, e := range subCat.Entries {
			key := skill.ResourceKey(e.Kind, e.Name)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged.Entries = append(merged.Entries, e)
		}

		merged.Malformed = append(merged.Malformed, subLoaded.Malformed...)
		merged.Diagnostics = append(merged.Diagnostics, subLoaded.Diagnostics...)

		if merged.Resources == nil {
			merged.Resources = make(map[string]ir.Resource)
		}
		for k, v := range subLoaded.Resources {
			if _, exists := merged.Resources[k]; !exists {
				merged.Resources[k] = v
			}
		}
	}

	merged.SubBundles = subCats
	if len(errs) > 0 && len(subCats) == 0 {
		return LoadedCatalog{}, fmt.Errorf("all %d sub-bundles failed: %w", len(subBundles), errors.Join(errs...))
	}
	// Partial success: return what we found plus accumulated errors.
	// Callers that check SubBundles can inspect individual sub-bundles;
	// callers that only use Entries still get the successfully loaded ones.
	merged.Errors = errs
	return merged, nil
}

func LoadRegistries(cfg *config.Config, repoRoot string, mode LoadMode, opts ...LoadOption) LoadResult {
	return LoadRegistriesWithContext(context.Background(), cfg, repoRoot, mode, opts...)
}

func LoadRegistriesWithContext(ctx context.Context, cfg *config.Config, repoRoot string, mode LoadMode, opts ...LoadOption) LoadResult {
	lc := loadConfig{cloneOrPull: cache.CloneOrPull, discover: Discover}
	for _, opt := range opts {
		opt(&lc)
	}

	result := LoadResult{
		Config:           &config.Config{},
		Catalogs:         make(map[string]LoadedCatalog),
		ResourceCatalogs: make(map[string]map[string]ir.Resource),
	}
	if cfg == nil {
		return result
	}

	// Preserve Marketplace entries in the loaded config so downstream resolver
	// and pipeline consumers can resolve configured Marketplace pins.
	result.Config.Marketplaces = cfg.Marketplaces
	result.Config.AllowedMarketplaces = cfg.AllowedMarketplaces
	result.Config.MarketplaceExclusions = cfg.MarketplaceExclusions
	// Marketplace resolution happens after registry loading. Preserve the
	// effective origin policy on the config returned by this loader; otherwise
	// the downstream resolver would see only the Marketplace pins and could
	// acquire an index-only source without the configured allowlist.
	result.Config.AllowedRegistries = cfg.AllowedRegistries

	// Phase 1: Prepare fetch jobs from registry entries.
	requests, jobs, err := prepareFetchJobs(cfg, repoRoot, mode, lc.localConfigDir, lc.exactSnapshot)
	result.Errors = append(result.Errors, err...)

	tel := telemetry.TelemetryFromContext(ctx)
	var cacheSpan telemetry.Span
	if tel != nil {
		_, cacheSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.registry_cache", registryCacheAttrs(cfg, jobs, mode))
	}

	// Phase 2: Execute fetch jobs (git clone/pull).
	runFetches(jobs, mode, lc.cloneOrPull)

	if cacheSpan != (telemetry.Span{}) {
		cacheSpan.SetAttributes(registryCacheResultAttrs(jobs))
		cacheSpan.End()
	}

	var ingestSpan telemetry.Span
	if tel != nil {
		_, ingestSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.catalog_ingest", nil)
	}

	// Phase 3: Discover skills in fetched registries. Keep the read leases for
	// the complete discovery pass so another process cannot collect a cache
	// directory after fetching but before its contents are consumed. Exact
	// snapshots use their own namespace lease because exact-git GC has a
	// separate lock from the mutable registry cache.
	var mutableURLs []string
	hasExactRequest := false
	for _, request := range requests {
		// Exact snapshots have no fetch job and are protected by the exact
		// namespace lease, not the mutable-cache lease.
		if request.job != nil {
			mutableURLs = append(mutableURLs, request.job.url)
		}
		if request.catalogBasePath != "" {
			hasExactRequest = true
		}
	}
	discover := func() {
		if len(mutableURLs) > 0 {
			if _, lockErr := cache.WithRegistryReadLockWithContext(ctx, mutableURLs, func() (struct{}, error) {
				discoverAll(&result, requests, lc.discover)
				return struct{}{}, nil
			}); lockErr != nil {
				result.Errors = append(result.Errors, fmt.Errorf("acquire registry cache read lease: %w", lockErr))
			}
			return
		}
		discoverAll(&result, requests, lc.discover)
	}
	if hasExactRequest {
		release, lockErr := cache.AcquireExactGitNamespaceReadOnlyLeaseWithContext(ctx)
		if lockErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("acquire exact git read lease: %w", lockErr))
		} else {
			discover()
			release()
		}
	} else {
		discover()
	}

	if ingestSpan != (telemetry.Span{}) {
		ingestSpan.SetAttributes(catalogIngestAttrs(result))
		ingestSpan.End()
	}

	return result
}

// prepareFetchJobs builds fetch jobs and load requests from registry entries.
// Local entries get a nil job; remote entries get deduplicated fetch jobs.
func prepareFetchJobs(cfg *config.Config, repoRoot string, mode LoadMode, localConfigDir string, exactSnapshot ...ExactSnapshotFunc) ([]loadRequest, []*fetchJob, []error) {
	jobsByKey := make(map[string]*fetchJob)
	requests := make([]loadRequest, 0, len(cfg.Registries))
	var errs []error
	var refreshAfterLoad bool

	for i := range cfg.Registries {
		entry := cfg.Registries[i]
		if entry.IsLocal() {
			// Resolve relative paths against the appropriate base directory:
			// entries from the local config layer use localConfigDir (the
			// subdirectory where config.local.yaml lives), all others use
			// repoRoot (the parent repo root).
			baseDir := repoRoot
			if entry.Source == config.RepoPersonal && localConfigDir != "" {
				baseDir = localConfigDir
			}
			localPath, err := entry.ResolveLocalPath(baseDir)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			// For local registries, BundlePath incorporates any path suffix,
			// giving a unique key per subdirectory.
			catalogKey := entry.BundlePath(localPath)
			entry.CatalogKey = catalogKey
			cfg.Registries[i].CatalogKey = catalogKey

			if mode == LoadModeInspect || mode == LoadModeCachedNoFetch {
				allowMissing := mode == LoadModeCachedNoFetch
				localExists, err := inspectLocalRegistryPath(localPath, catalogKey, allowMissing)
				if err != nil {
					errs = append(errs, fmt.Errorf("inspect local registry %q: %w", catalogKey, err))
					continue
				}
				if !localExists {
					// CachedNoFetch deliberately skips missing local registries;
					// Inspect treats them as hard errors via allowMissing=false.
					continue
				}
			} else if err := config.ValidateLocalRegistryPath(catalogKey); err != nil {
				// Stat the local registry path before attempting ingest so a missing
				// directory is reported clearly instead of falling through to the
				// autodetect/ingest failure path.
				slog.Warn("local registry validation failed", "path", catalogKey, "error", err)
				errs = append(errs, err)
				continue
			}

			requests = append(requests, loadRequest{entry: entry, catalogKey: catalogKey, basePath: localPath})
			continue
		}

		if len(exactSnapshot) > 0 && exactSnapshot[0] != nil {
			snapshotPath, snapshotIdentity, err := exactSnapshot[0](entry)
			if err != nil {
				errs = append(errs, fmt.Errorf("open exact snapshot for registry %q (ref %q): %w", entry.URL, entry.Ref, err))
				continue
			}
			if snapshotPath != "" {
				catalogKey := snapshotIdentity
				if catalogKey == "" {
					catalogKey = cache.CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path)
				}
				entry.CatalogKey = catalogKey
				cfg.Registries[i].CatalogKey = catalogKey
				requests = append(requests, loadRequest{
					entry: entry, catalogKey: catalogKey, basePath: snapshotPath,
					catalogBasePath: cache.RegistryCachePath(entry.URL, entry.Ref),
				})
				continue
			}
		}

		// cachePath is the filesystem location for git clone/pull (shared by
		// entries with the same URL+ref). catalogKey distinguishes entries by
		// path so different subdirectories produce distinct catalog keys.
		cachePath := cache.RegistryCachePath(entry.URL, entry.Ref)
		catalogKey := cache.CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path)
		entry.CatalogKey = catalogKey
		cfg.Registries[i].CatalogKey = catalogKey

		bundlePath := entry.BundlePath(cachePath)
		bundleExists := false
		if mode == LoadModeInspect || mode == LoadModeCachedNoFetch {
			allowCacheMissing := mode == LoadModeCachedNoFetch
			cacheExists, err := inspectRemoteGitCachePath(cachePath, allowCacheMissing)
			if err != nil {
				job, ok := jobsByKey[cachePath]
				if !ok {
					job = &fetchJob{url: entry.URL, ref: entry.Ref, cachePath: cachePath}
					jobsByKey[cachePath] = job
				}
				job.err = fmt.Errorf("inspect cache %q: %w", cachePath, err)
				requests = append(requests, loadRequest{entry: entry, catalogKey: catalogKey, job: job})
				continue
			}
			if !cacheExists && mode == LoadModeCachedNoFetch {
				// No cache on disk and we are forbidden from fetching; skip it so
				// teardown/dry-run paths remain read-only.
				continue
			}

			bundleExists, err = inspectRemoteGitCachePath(bundlePath, true)
			if err != nil {
				job, ok := jobsByKey[cachePath]
				if !ok {
					job = &fetchJob{url: entry.URL, ref: entry.Ref, cachePath: cachePath}
					jobsByKey[cachePath] = job
				}
				job.err = fmt.Errorf("inspect cache bundle %q: %w", bundlePath, err)
				requests = append(requests, loadRequest{entry: entry, catalogKey: catalogKey, job: job})
				continue
			}
		} else if info, err := os.Stat(bundlePath); err == nil && info.IsDir() {
			bundleExists = true
		}

		if mode == LoadModeCachedNoFetch && !bundleExists {
			// No cache on disk and we are forbidden from fetching; skip this
			// registry so teardown/dry-run paths remain read-only.
			continue
		}

		job, ok := jobsByKey[cachePath]
		if !ok {
			job = &fetchJob{url: entry.URL, ref: entry.Ref, cachePath: cachePath, warmCache: mode != LoadModeRefresh && bundleExists}
			if mode == LoadModeCached && bundleExists {
				refreshAfterLoad = true
			}
			jobsByKey[cachePath] = job
		}
		requests = append(requests, loadRequest{entry: entry, catalogKey: catalogKey, job: job})
	}

	// Stash refreshAfterLoad hint for the caller (we return it via the first job's field).
	if refreshAfterLoad && len(jobsByKey) > 0 {
		for _, job := range jobsByKey {
			if job.warmCache {
				// Signal to the result that a refresh is available.
				break
			}
		}
	}

	var jobs []*fetchJob
	for _, job := range jobsByKey {
		jobs = append(jobs, job)
	}

	return requests, jobs, errs
}

func inspectLocalRegistryPath(root, path string, allowMissing bool) (bool, error) {
	exists, inspectErr := inspectDirectoryPath(root, path, allowMissing)
	if inspectErr == nil || (allowMissing && errors.Is(inspectErr, os.ErrNotExist)) {
		return exists, inspectErr
	}

	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return exists, inspectErr
	}
	if err == nil || errors.Is(err, os.ErrNotExist) {
		if localErr := config.ValidateLocalRegistryPath(path); localErr != nil {
			return false, fmt.Errorf("%w: %v", localErr, inspectErr)
		}
	}
	return exists, inspectErr
}

func inspectRemoteGitCachePath(path string, allowMissing bool) (bool, error) {
	cacheRoot := cache.CacheDir()
	cacheBoundary := cacheRoot
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		cacheBoundary = homeDir
	}
	if err := cache.ValidateDirectoryAncestry(cacheBoundary, cacheRoot); err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return inspectDirectoryPath(cacheRoot, path, allowMissing)
}

func inspectDirectoryPath(root, path string, allowMissing bool) (bool, error) {
	if err := cache.ValidateDirectoryAncestry(root, path); err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("path %q is a symlink", path)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("path %q is not a directory", path)
	}
	return true, nil
}

// runFetches executes fetch jobs according to the load mode.
func runFetches(jobs []*fetchJob, mode LoadMode, cloneOrPull CloneOrPullFunc) {
	switch mode {
	case LoadModeRefresh:
		runFetchJobs(jobs, cloneOrPull)
	case LoadModeCached:
		var cold []*fetchJob
		for _, job := range jobs {
			if !job.warmCache {
				cold = append(cold, job)
			}
		}
		runFetchJobs(cold, cloneOrPull)
	case LoadModeInspect:
		for _, job := range jobs {
			if _, err := inspectRemoteGitCachePath(job.cachePath, false); err != nil {
				job.err = fmt.Errorf("inspect cache %q: %w", job.cachePath, err)
			}
		}
	case LoadModeCachedNoFetch:
		// prepareFetchJobs only included registries whose cache already exists and
		// marked them warmCache=true. Nothing to fetch.
	}
}

// OverlappingEntryWarning is emitted when a registry entry's skill
// directory was already scanned by a previous entry (overlapping paths).
// The entry is still registered with its own policy, and the catalog is
// copied from the first scan, so the resolver receives the full config.
// This is a soft warning: no manual action is required, but the user
// should know their entries overlap.
type OverlappingEntryWarning struct {
	Dir        string // the overlapping skill directory
	FirstURL   string // URL of the first entry that scanned this directory
	FirstPath  string // path of the first entry
	SecondURL  string // URL of the second (overlapping) entry
	SecondPath string // path of the second entry
}

func (w *OverlappingEntryWarning) Error() string {
	return fmt.Sprintf("registry entries overlap at %q: first %q (path %q), second %q (path %q)",
		w.Dir, redact.URL(w.FirstURL), redact.URL(w.FirstPath), redact.URL(w.SecondURL), redact.URL(w.SecondPath))
}

func (w *OverlappingEntryWarning) Resource() string          { return w.SecondPath }
func (w *OverlappingEntryWarning) Source() string            { return redact.URL(w.SecondURL) }
func (w *OverlappingEntryWarning) PresentationKey() string   { return "catalog.registry-overlap" }
func (w *OverlappingEntryWarning) PresentationText() string  { return "registry entries overlap" }
func (w *OverlappingEntryWarning) DiagnosticCode() string    { return "" }
func (w *OverlappingEntryWarning) DiagnosticMessage() string { return "" }

// IsOverlapWarning reports whether an error is an OverlappingEntryWarning.
func IsOverlapWarning(err error) bool {
	var w *OverlappingEntryWarning
	return errors.As(err, &w)
}

// discoverAll runs skill discovery on all loaded requests and populates the result.
func discoverAll(result *LoadResult, requests []loadRequest, discover DiscoverFunc) {
	discoveredByKey := make(map[string]LoadedCatalog)
	scannedDirs := make(map[string]struct{})
	scannedDirEntries := make(map[string]config.RegistryEntry)
	// scannedDirKeys maps each scanned skill directory to the catalogKey
	// under which its catalog was stored. This allows overlapping entries
	// to copy the catalog from the first scan instead of getting an empty one.
	scannedDirKeys := make(map[string]string)

	for _, req := range requests {
		entry := req.entry
		basePath := req.basePath
		if req.job != nil {
			if req.job.err != nil {
				result.Errors = append(result.Errors, req.job.err)
				continue
			}
			basePath = req.job.cachePath
		}
		skillDir := entry.BundlePath(basePath)

		if _, scanned := scannedDirs[skillDir]; scanned {
			result.Config.Registries = append(result.Config.Registries, entry)
			// Copy the catalog from the first entry that scanned this directory,
			// so this entry's policy (default, required, selectors) can reach
			// the resolver with valid catalog data.
			firstKey := scannedDirKeys[skillDir]
			if existing, ok := discoveredByKey[firstKey]; ok {
				result.Catalogs[req.catalogKey] = existing
				discoveredByKey[req.catalogKey] = existing
				if len(existing.Resources) > 0 {
					result.ResourceCatalogs[req.catalogKey] = existing.Resources
				}
			}
			// Only emit an overlap warning when the paths genuinely overlap
			// (one is a component-wise prefix of the other). Same-path entries
			// from different URL forms (e.g. .git suffix) are URL dedup, not
			// path overlap — no warning needed.
			firstURL := findEntryURL(requests, firstKey)
			firstPath := findEntryPath(requests, firstKey)
			if firstURL == "" {
				firstURL = scannedDirEntries[skillDir].URL
				firstPath = scannedDirEntries[skillDir].Path
			}
			if isCatalogPathPrefix(firstPath, entry.Path) || isCatalogPathPrefix(entry.Path, firstPath) {
				result.Errors = append(result.Errors, &OverlappingEntryWarning{
					Dir:        skillDir,
					FirstURL:   firstURL,
					FirstPath:  firstPath,
					SecondURL:  entry.URL,
					SecondPath: entry.Path,
				})
			}
			continue
		}
		scannedDirs[skillDir] = struct{}{}
		scannedDirKeys[skillDir] = req.catalogKey
		scannedDirEntries[skillDir] = entry

		discovered, err := discover(skillDir, entry.SourceHarness)
		if err != nil {
			result.Config.Registries = append(result.Config.Registries, entry)
			result.Errors = append(result.Errors, fmt.Errorf("registry %q (ref %q, path %q): %w", entry.URL, entry.Ref, entry.Path, err))
			continue
		}
		if req.catalogBasePath != "" {
			rebaseCatalogEntryPaths(&discovered, basePath, req.catalogBasePath)
		}

		// When recursive discovery split the result into per-sub-bundle
		// catalogs, process each sub-bundle separately with its own catalog
		// key derived from the sub-bundle's path. This gives each sub-bundle
		// its own section in the TUI. The parent entry is NOT added to
		// Config.Registries because it has no catalog — processSubBundles
		// adds one entry per sub-bundle instead.
		//
		// The merged catalog is also stored under the parent entry's key
		// so that callers which only know the parent entry (e.g. the
		// --from install path) can still look up resources without needing
		// to know about the sub-bundle decomposition.
		if len(discovered.SubBundles) > 0 {
			processSubBundles(result, discoveredByKey, scannedDirs, scannedDirKeys, scannedDirEntries, requests, entry, req.catalogKey, basePath, discovered.SubBundles, discovered.Errors, discover)
			discoveredByKey[req.catalogKey] = discovered
			result.Catalogs[req.catalogKey] = discovered
			if len(discovered.Resources) > 0 {
				result.ResourceCatalogs[req.catalogKey] = discovered.Resources
			}
			continue
		}

		// Normal path: add the registry entry.
		result.Config.Registries = append(result.Config.Registries, entry)

		collections, colErr := collection.DiscoverCollections(skillDir, req.catalogKey, entry.URL, entry.Ref, entry.Path)
		if colErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("discover collections for registry %q (ref %q, path %q) in %q: %w", entry.URL, entry.Ref, entry.Path, skillDir, colErr))
		}
		discovered.Collections = collections

		// Add collection catalog entries so they are visible to the resolver,
		// collision detector, and disambiguation logic.
		collectionEntries, colEntryErr := EntriesFromCollections(collections)
		if colEntryErr != nil {
			result.Errors = append(result.Errors, colEntryErr)
		}
		discovered.Entries = append(discovered.Entries, collectionEntries...)
		for _, e := range collectionEntries {
			if discovered.Resources == nil {
				discovered.Resources = make(map[string]ir.Resource)
			}
			discovered.Resources[skill.ResourceKey(e.Kind, e.Name)] = e.Resource
		}

		// Merge with any existing catalog data for this key.
		existing := discoveredByKey[req.catalogKey]
		discovered.Entries = append(existing.Entries, discovered.Entries...)
		discovered.Malformed = append(existing.Malformed, discovered.Malformed...)
		discovered.Collections = appendUniqueCollections(existing.Collections, discovered.Collections)
		for k, v := range existing.Resources {
			if discovered.Resources == nil {
				discovered.Resources = make(map[string]ir.Resource)
			}
			if _, exists := discovered.Resources[k]; !exists {
				discovered.Resources[k] = v
			}
		}
		discoveredByKey[req.catalogKey] = discovered

		result.Catalogs[req.catalogKey] = discovered
		if len(discovered.Resources) > 0 {
			result.ResourceCatalogs[req.catalogKey] = discovered.Resources
		}
		result.Collections = appendUniqueCollections(result.Collections, discovered.Collections)
		result.Malformed = append(result.Malformed, discovered.Malformed...)
		result.MalformedCount += len(discovered.Malformed)
	}

	// Propagate RefreshAfterLoad from cached mode with warm caches.
	for _, req := range requests {
		if req.job != nil && req.job.warmCache {
			result.RefreshAfterLoad = true
			break
		}
	}
}

func rebaseCatalogEntryPaths(loaded *LoadedCatalog, fromRoot, toRoot string) {
	for i := range loaded.Entries {
		path := loaded.Entries[i].Path
		if !filepath.IsAbs(path) {
			continue
		}
		rel, err := filepath.Rel(fromRoot, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		preserveResourceSourcePath(loaded.Entries[i].Resource, path)
		loaded.Entries[i].Path = filepath.Join(toRoot, rel)
	}
	for path, subBundle := range loaded.SubBundles {
		rebaseCatalogEntryPaths(&subBundle, fromRoot, toRoot)
		loaded.SubBundles[path] = subBundle
	}
}

// processSubBundles creates a separate catalog entry for each sub-bundle
// discovered from a meta-directory. Each sub-bundle gets its own catalog key
// derived from the sub-bundle's path relative to the repo root, so the TUI
// groups them under distinct sections. The parent registry entry's selectors
// (default, skills, tags, etc.) are inherited by each sub-bundle entry.
func processSubBundles(result *LoadResult, discoveredByKey map[string]LoadedCatalog, scannedDirs map[string]struct{}, scannedDirKeys map[string]string, scannedDirEntries map[string]config.RegistryEntry, requests []loadRequest, parentEntry config.RegistryEntry, parentCatalogKey string, basePath string, subBundles map[string]LoadedCatalog, subBundleErrs []error, discover DiscoverFunc) {
	// Sort sub-bundle directories for deterministic processing.
	subDirs := make([]string, 0, len(subBundles))
	for dir := range subBundles {
		subDirs = append(subDirs, dir)
	}
	sort.Strings(subDirs)

	// Propagate any sub-bundle errors from discovery into the result.
	for _, subErr := range subBundleErrs {
		result.Errors = append(result.Errors, fmt.Errorf("registry %q (ref %q): %w", parentEntry.URL, parentEntry.Ref, subErr))
	}

	var diagnosticOnlyDirs []string
	for _, subDir := range subDirs {
		subCat := subBundles[subDir]

		// Compute the sub-bundle's path relative to the repo root.
		relPath, err := filepath.Rel(basePath, subDir)
		if err != nil {
			relPath = filepath.Base(subDir)
		}

		subKey := cache.CatalogKeyForEntry(parentEntry.URL, parentEntry.Ref, relPath)
		if len(subCat.Entries) == 0 && !hasDiscoveryBoundaryDiagnostics(subCat.Diagnostics) {
			result.Catalogs[subKey] = subCat
			result.Malformed = append(result.Malformed, subCat.Malformed...)
			result.MalformedCount += len(subCat.Malformed)
			diagnosticOnlyDirs = append(diagnosticOnlyDirs, subDir)
			if !slices.ContainsFunc(subDirs, func(candidate string) bool {
				return candidate != subDir && catalogPathWithin(subDir, candidate) && len(subBundles[candidate].Entries) > 0
			}) {
				subEntry := parentEntry
				subEntry.Path = relPath
				subEntry.CatalogKey = subKey
				result.Config.Registries = append(result.Config.Registries, subEntry)
			}
			continue
		}

		// Avoid re-scanning if this sub-bundle directory was already
		// discovered (e.g. as an explicit registry entry). Create the
		// synthetic entry so the parent's policy (default, required, selectors)
		// reaches the resolver, copy the catalog from the first scan, and
		// prune the sub-bundle's entries from the parent catalog.
		subSkillDir := filepath.Join(basePath, relPath)
		if _, scanned := scannedDirs[subSkillDir]; scanned {
			// Create the synthetic entry so parent's policy (default, required,
			// selectors) is preserved even when the directory was already scanned
			// by an explicit entry.
			subEntry := parentEntry
			subEntry.Path = relPath
			subEntry.CatalogKey = subKey
			result.Config.Registries = append(result.Config.Registries, subEntry)

			// Copy the catalog from the first entry that scanned this directory.
			firstKey := scannedDirKeys[subSkillDir]
			if existing, ok := discoveredByKey[firstKey]; ok {
				result.Catalogs[subKey] = existing
				discoveredByKey[subKey] = existing
				if len(existing.Resources) > 0 {
					result.ResourceCatalogs[subKey] = existing.Resources
				}
			}

			// Only emit an overlap warning when the paths genuinely overlap
			// (one is a component-wise prefix of the other), consistent with
			// the same check in discoverAll's scannedDirs path.
			firstURL := findEntryURL(requests, firstKey)
			firstPath := findEntryPath(requests, firstKey)
			if firstURL == "" {
				firstURL = scannedDirEntries[subSkillDir].URL
				firstPath = scannedDirEntries[subSkillDir].Path
			}
			if isCatalogPathPrefix(firstPath, relPath) || isCatalogPathPrefix(relPath, firstPath) {
				result.Errors = append(result.Errors, &OverlappingEntryWarning{
					Dir:        subSkillDir,
					FirstURL:   firstURL,
					FirstPath:  firstPath,
					SecondURL:  parentEntry.URL,
					SecondPath: relPath,
				})
			}
			continue
		}
		scannedDirs[subSkillDir] = struct{}{}
		scannedDirKeys[subSkillDir] = subKey
		scannedEntry := parentEntry
		scannedEntry.Path = relPath
		scannedDirEntries[subSkillDir] = scannedEntry

		// Create a synthetic RegistryEntry for this sub-bundle.
		subEntry := parentEntry
		subEntry.Path = relPath
		subEntry.CatalogKey = subKey
		result.Config.Registries = append(result.Config.Registries, subEntry)

		// Discover collections for this sub-bundle.
		subCollections, subColErr := collection.DiscoverCollections(subDir, subKey, parentEntry.URL, parentEntry.Ref, subEntry.Path)
		if subColErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("discover collections for sub-bundle %q in %q: %w", relPath, subDir, subColErr))
		}
		subCat.Collections = subCollections

		// Add collection catalog entries for sub-bundles.
		subCollectionEntries, subColEntryErr := EntriesFromCollections(subCollections)
		if subColEntryErr != nil {
			result.Errors = append(result.Errors, subColEntryErr)
		}
		subCat.Entries = append(subCat.Entries, subCollectionEntries...)
		for _, e := range subCollectionEntries {
			if subCat.Resources == nil {
				subCat.Resources = make(map[string]ir.Resource)
			}
			subCat.Resources[skill.ResourceKey(e.Kind, e.Name)] = e.Resource
		}

		// Merge with any existing catalog data for this key.
		existing := discoveredByKey[subKey]
		subCat.Entries = append(existing.Entries, subCat.Entries...)
		subCat.Malformed = append(existing.Malformed, subCat.Malformed...)
		subCat.Collections = appendUniqueCollections(existing.Collections, subCat.Collections)
		for k, v := range existing.Resources {
			if subCat.Resources == nil {
				subCat.Resources = make(map[string]ir.Resource)
			}
			if _, exists := subCat.Resources[k]; !exists {
				subCat.Resources[k] = v
			}
		}
		discoveredByKey[subKey] = subCat

		result.Catalogs[subKey] = subCat
		if len(subCat.Resources) > 0 {
			result.ResourceCatalogs[subKey] = subCat.Resources
		}
		result.Collections = appendUniqueCollections(result.Collections, subCat.Collections)
		result.Malformed = append(result.Malformed, subCat.Malformed...)
		result.MalformedCount += len(subCat.Malformed)
	}

	for _, containerDir := range diagnosticOnlyDirs {
		for _, subDir := range subDirs {
			if subDir == containerDir || !catalogPathWithin(containerDir, subDir) || len(subBundles[subDir].Entries) == 0 {
				continue
			}
			relPath, err := filepath.Rel(basePath, subDir)
			if err != nil {
				continue
			}
			subKey := cache.CatalogKeyForEntry(parentEntry.URL, parentEntry.Ref, relPath)
			subCat, ok := result.Catalogs[subKey]
			if !ok {
				continue
			}
			for _, malformed := range subBundles[containerDir].Malformed {
				if !slices.ContainsFunc(subCat.Malformed, func(existing skill.MalformedSkill) bool {
					return existing.Path == malformed.Path && existing.DirName == malformed.DirName
				}) {
					subCat.Malformed = append(subCat.Malformed, malformed)
				}
			}
			result.Catalogs[subKey] = subCat
			discoveredByKey[subKey] = subCat
		}
	}
}

// isCatalogPathPrefix reports whether parent is a component-wise path prefix
// of child, using the same logic as resolver.isPathPrefix. Empty parent is
// a prefix of any child; same paths are not considered a prefix relationship.
func isCatalogPathPrefix(parent, child string) bool {
	if parent == child {
		return false
	}
	if parent == "" {
		return true
	}
	if child == "" {
		return false
	}
	if !strings.HasPrefix(child, parent) {
		return false
	}
	return len(child) > len(parent) && child[len(parent)] == '/'
}

// findEntryURL looks up the URL of the first request whose catalogKey
// matches the given key. Returns "" if not found.
func findEntryURL(requests []loadRequest, key string) string {
	for _, req := range requests {
		if req.catalogKey == key {
			return req.entry.URL
		}
	}
	return ""
}

// findEntryPath looks up the path of the first request whose catalogKey
// matches the given key. Returns "" if not found.
func findEntryPath(requests []loadRequest, key string) string {
	for _, req := range requests {
		if req.catalogKey == key {
			return req.entry.Path
		}
	}
	return ""
}

// appendUniqueCollections appends items from extra to base, skipping any
// whose Name already appears in base. This prevents duplicates when a
// collection exists at both the registry root and the skill subdirectory.
func appendUniqueCollections(base, extra []collection.Collection) []collection.Collection {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base))
	for _, c := range base {
		seen[c.Name] = struct{}{}
	}
	for _, c := range extra {
		if _, ok := seen[c.Name]; !ok {
			seen[c.Name] = struct{}{}
			base = append(base, c)
		}
	}
	return base
}

func runFetchJobs(jobs []*fetchJob, cloneOrPull CloneOrPullFunc) {
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(j *fetchJob) {
			defer wg.Done()
			j.cachePath, j.err = cloneOrPull(j.url, j.ref)
		}(job)
	}
	wg.Wait()
}
