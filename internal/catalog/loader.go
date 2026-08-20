package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
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

type DiscoverFunc func(dir, sourceHarness string) (LoadedCatalog, error)

type LoadOption func(*loadConfig)

type loadConfig struct {
	cloneOrPull    CloneOrPullFunc
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
	entry      config.RegistryEntry
	catalogKey string
	basePath   string
	job        *fetchJob
}

func WithCloneOrPull(fn CloneOrPullFunc) LoadOption {
	return func(c *loadConfig) { c.cloneOrPull = fn }
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
	cat, err := Load(dir, sourceHarness)
	if err != nil {
		// Load failed — likely no recognizable layout at this directory.
		// Before falling back to DiscoverSkills, check for sub-bundles.
		// If subdirectories contain bundle roots, load them recursively
		// instead of relying on the legacy skill-only discovery.
		subBundles := scanForSubBundles(dir)
		if len(subBundles) > 0 {
			return discoverSubBundles(subBundles, sourceHarness)
		}

		// No sub-bundles and no recognizable bundle format: return the
		// original load error so the caller knows discovery failed.
		return LoadedCatalog{}, err
	}

	// Vacant-lot condition: zero entries and no warning/error diagnostics.
	// HCC scanned the directory, found nothing, and had nothing to complain
	// about. This can happen when the directory is a meta-directory containing
	// multiple independent bundle subdirectories. Scan for sub-bundles and
	// recurse into them.
	if len(cat.Entries) == 0 && !hasWarningOrHigher(cat.Diagnostics) {
		subBundles := scanForSubBundles(dir)
		if len(subBundles) > 0 {
			return discoverSubBundles(subBundles, sourceHarness)
		}
	}

	malformed := malformedFromDiagnostics(cat.Diagnostics)

	// Make malformed paths absolute so the TUI can read the SKILL.md
	// file when the user opens the detail pane. Diagnostics use paths
	// relative to the bundle root (e.g. "skills/broken-skill"), but the
	// TUI's readDetailSource needs an absolute path to stat/read.
	// Resolve dir to an absolute, canonical root and verify each
	// malformed path stays within that root to prevent ../ escapes.
	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return LoadedCatalog{}, fmt.Errorf("resolve discovery root %q: %w", dir, err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return LoadedCatalog{}, fmt.Errorf("canonicalize discovery root %q: %w", rootAbs, err)
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
		pReal, ok := resolveMalformedPath(rootReal, p)
		if !ok {
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
		if declared := skill.DeclaredNameFromFile(pReal); declared != "" && declared != malformed[i].DirName {
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
		if subBundles := scanForSubBundles(dir); len(subBundles) > 0 {
			if !catalogCoversSubBundles(cat.Entries, dir, subBundles) {
				malformed = append(malformed, skill.MalformedSkill{
					DirName: "catalog.discover.sub-bundles-ignored",
					Path:    dir,
					Error:   fmt.Errorf("directory has both root-level resources and %d sub-bundle(s) (e.g. %s); sub-bundles are ignored. Move root resources into a sub-bundle (e.g. common/) so the parent is a pure container for recursive discovery", len(subBundles), filepath.Base(subBundles[0])),
				})
			}
		}
	}

	return LoadedCatalog{
		Entries:   cat.Entries,
		Malformed: malformed,
		Resources: Resources(cat),
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

// catalogCoversSubBundles reports whether the loaded catalog entries already
// cover the detected sub-bundle directories. When all sub-bundles have at
// least one entry whose source path falls within the sub-bundle, the
// sub-bundles are just organizational groupings within the loaded bundle —
// not independent bundles with missed resources. In that case the
// sub-bundles-ignored warning is a false positive and should be suppressed.
func catalogCoversSubBundles(entries []Entry, dir string, subBundles []string) bool {
	// Compute the relative path from dir to each sub-bundle.
	relPaths := make([]string, 0, len(subBundles))
	for _, sb := range subBundles {
		rel, err := filepath.Rel(dir, sb)
		if err != nil {
			return false // Can't compute relative path; be conservative.
		}
		relPaths = append(relPaths, rel)
	}

	// For each sub-bundle, check if at least one entry has a source path
	// within it. Entry.Path comes from ir.SourceRef.File and is relative
	// to the bundle root (dir).
	covered := make(map[int]bool, len(subBundles))
	for _, entry := range entries {
		p := filepath.ToSlash(entry.Path)
		for i, rel := range relPaths {
			if !covered[i] && subPathRel(p, rel) {
				covered[i] = true
			}
		}
	}
	for _, c := range covered {
		if !c {
			return false // At least one sub-bundle has no entries from it.
		}
	}
	return len(covered) == len(subBundles)
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
// or any HCC Autodetect markers). When a directory with roots is found, it is
// added to the results and its children are NOT descended into — the bundle's
// own Load call handles its internal structure. Directories without roots are
// recursed into. Results are sorted deterministically by path.
func scanForSubBundles(dir string) []string {
	var bundles []string
	scanSubBundlesWalk(dir, &bundles)
	sort.Strings(bundles)
	return bundles
}

func scanSubBundlesWalk(dir string, bundles *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, skip := skippedScanDirs[name]; skip {
			continue
		}
		subDir := filepath.Join(dir, entry.Name())
		if hasBundleRoot(subDir) {
			*bundles = append(*bundles, subDir)
		} else {
			scanSubBundlesWalk(subDir, bundles)
		}
	}
}

// skippedScanDirs lists directory names that should never be treated as
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
var skippedScanDirs = map[string]struct{}{
	".git":             {},
	"node_modules":     {},
	".cache":           {},
	"test":             {},
	"tests":            {},
	"__tests__":        {},
	".factory":         {},
	".factory-plugin":  {},
	".claude":          {},
	".claude-plugin":   {},
	".cursor":          {},
	".cursor-plugin":   {},
	".windsurf":        {},
	".gemini":          {},
	"gemini-extension": {},
	"skills":           {},
	"commands":         {},
	"agents":           {},
	"hooks":            {},
}

// hasBundleRoot reports whether dir contains any standard root directory or file
// that marks it as a harness bundle. Uses HCC's AutodetectStrict to cover all
// harness-specific markers (plugin manifests, workspace directories, and bare
// plugin form with skills/commands/agents/hooks/mcp.json). AutodetectStrict
// excludes the bare-skills-directory form to prevent individual skill dirs
// from being treated as sub-bundles, but we re-enable the multi-skill subdir
// case (dir/*/SKILL.md) so container directories with multiple skills are
// still recognized. A root SKILL.md (single-skill directory) remains excluded.
func hasBundleRoot(dir string) bool {
	srcFS := os.DirFS(dir)
	_, ok := ingest.AutodetectStrict(srcFS)
	if ok {
		return true
	}
	// AutodetectStrict skips bare-skills-directory form; check if this
	// directory contains immediate subdirectories with SKILL.md (multi-skill
	// container). A root SKILL.md (single skill) is NOT a sub-bundle root.
	return hasMultiSkillSubdirs(srcFS)
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
		if _, skip := skippedScanDirs[e.Name()]; skip {
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
func resolveMalformedPath(rootReal, p string) (string, bool) {
	pReal, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootReal, pReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return pReal, true
}

// normalizeMalformedPaths makes malformed skill paths absolute relative to
// root, matching the normalization that Discover() applies for the
// non-sub-bundle path. Without this, recursively discovered broken skills
// keep relative paths (e.g. "skills/bad-skill") while the non-recursive
// path normalizes them for the TUI.
func normalizeMalformedPaths(root string, malformed []skill.MalformedSkill) []skill.MalformedSkill {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return malformed
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
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

		if _, ok := resolveMalformedPath(rootReal, p); !ok {
			malformed[i].Path = ""
			continue
		}

		malformed[i].Path = p
	}
	return malformed
}

// discoverSubBundles loads each sub-bundle directory. It populates
// SubBundles with per-sub-bundle results so discoverAll can create separate
// catalogs for each. Entries/Malformed/Resources contain the merged view
// for backward compatibility with callers that don't check SubBundles.
// Name collisions across sub-bundles are resolved by first-wins (sub-bundles
// are processed in sorted directory order).
func discoverSubBundles(subBundles []string, sourceHarness string) (LoadedCatalog, error) {
	var merged LoadedCatalog
	var errs []error
	seen := make(map[string]struct{})
	subCats := make(map[string]LoadedCatalog, len(subBundles))

	for _, subDir := range subBundles {
		subCat, subErr := Load(subDir, sourceHarness)
		if subErr != nil {
			errs = append(errs, fmt.Errorf("sub-bundle %q: %w", subDir, subErr))
			continue
		}

		subLoaded := LoadedCatalog{
			Entries:   subCat.Entries,
			Malformed: normalizeMalformedPaths(subDir, malformedFromDiagnostics(subCat.Diagnostics)),
			Resources: Resources(subCat),
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

	// Phase 1: Prepare fetch jobs from registry entries.
	requests, jobs, err := prepareFetchJobs(cfg, repoRoot, mode, lc.localConfigDir)
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

	// Phase 3: Discover skills in fetched registries.
	discoverAll(&result, requests, lc.discover)

	if ingestSpan != (telemetry.Span{}) {
		ingestSpan.SetAttributes(catalogIngestAttrs(result))
		ingestSpan.End()
	}

	return result
}

// prepareFetchJobs builds fetch jobs and load requests from registry entries.
// Local entries get a nil job; remote entries get deduplicated fetch jobs.
func prepareFetchJobs(cfg *config.Config, repoRoot string, mode LoadMode, localConfigDir string) ([]loadRequest, []*fetchJob, []error) {
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

			if mode == LoadModeCachedNoFetch {
				if _, err := os.Stat(catalogKey); err != nil {
					// No local registry present and we are forbidden from fetching;
					// skip it so teardown/dry-run paths remain read-only.
					continue
				}
			}

			requests = append(requests, loadRequest{entry: entry, catalogKey: catalogKey, basePath: localPath})
			continue
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
		if info, err := os.Stat(bundlePath); err == nil && info.IsDir() {
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
			info, err := os.Stat(job.cachePath)
			if err != nil {
				job.err = fmt.Errorf("inspect cache %q: %w", job.cachePath, err)
				continue
			}
			if !info.IsDir() {
				job.err = fmt.Errorf("inspect cache %q: not a directory", job.cachePath)
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

// IsOverlapWarning reports whether an error is an OverlappingEntryWarning.
func IsOverlapWarning(err error) bool {
	var w *OverlappingEntryWarning
	return errors.As(err, &w)
}

// discoverAll runs skill discovery on all loaded requests and populates the result.
func discoverAll(result *LoadResult, requests []loadRequest, discover DiscoverFunc) {
	discoveredByKey := make(map[string]LoadedCatalog)
	scannedDirs := make(map[string]struct{})
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
			firstPath := findEntryPath(requests, firstKey)
			if isCatalogPathPrefix(firstPath, entry.Path) || isCatalogPathPrefix(entry.Path, firstPath) {
				result.Errors = append(result.Errors, &OverlappingEntryWarning{
					Dir:        skillDir,
					FirstURL:   findEntryURL(requests, firstKey),
					FirstPath:  firstPath,
					SecondURL:  entry.URL,
					SecondPath: entry.Path,
				})
			}
			continue
		}
		scannedDirs[skillDir] = struct{}{}
		scannedDirKeys[skillDir] = req.catalogKey

		discovered, err := discover(skillDir, entry.SourceHarness)
		if err != nil {
			result.Config.Registries = append(result.Config.Registries, entry)
			result.Errors = append(result.Errors, fmt.Errorf("registry %q (ref %q, path %q): %w", entry.URL, entry.Ref, entry.Path, err))
			continue
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
			processSubBundles(result, discoveredByKey, scannedDirs, scannedDirKeys, requests, entry, req.catalogKey, basePath, discovered.SubBundles, discovered.Errors, discover)
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

// processSubBundles creates a separate catalog entry for each sub-bundle
// discovered from a meta-directory. Each sub-bundle gets its own catalog key
// derived from the sub-bundle's path relative to the repo root, so the TUI
// groups them under distinct sections. The parent registry entry's selectors
// (default, skills, tags, etc.) are inherited by each sub-bundle entry.
func processSubBundles(result *LoadResult, discoveredByKey map[string]LoadedCatalog, scannedDirs map[string]struct{}, scannedDirKeys map[string]string, requests []loadRequest, parentEntry config.RegistryEntry, parentCatalogKey string, basePath string, subBundles map[string]LoadedCatalog, subBundleErrs []error, discover DiscoverFunc) {
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

	for _, subDir := range subDirs {
		subCat := subBundles[subDir]

		// Compute the sub-bundle's path relative to the repo root.
		relPath, err := filepath.Rel(basePath, subDir)
		if err != nil {
			relPath = filepath.Base(subDir)
		}

		subKey := cache.CatalogKeyForEntry(parentEntry.URL, parentEntry.Ref, relPath)

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
			firstPath := findEntryPath(requests, firstKey)
			if isCatalogPathPrefix(firstPath, relPath) || isCatalogPathPrefix(relPath, firstPath) {
				result.Errors = append(result.Errors, &OverlappingEntryWarning{
					Dir:        subSkillDir,
					FirstURL:   findEntryURL(requests, firstKey),
					FirstPath:  firstPath,
					SecondURL:  parentEntry.URL,
					SecondPath: relPath,
				})
			}
			continue
		}
		scannedDirs[subSkillDir] = struct{}{}
		scannedDirKeys[subSkillDir] = subKey

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
