package collection

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// ExpandResult holds the outcome of expanding collection references.
type ExpandResult struct {
	// SyntheticEntries are additional RegistryEntry items to append to the
	// merged config before skill resolution. They carry the skills each
	// collection contributes, scoped to the correct registry.
	SyntheticEntries []config.RegistryEntry

	// ViaMap maps kind+name → collection label (e.g. "collection:name@ver").
	// Only resources contributed exclusively by a collection are present here;
	// resources that appear in both a collection and a direct config entry are
	// NOT annotated (direct config takes precedence). Keys are formatted as
	// "<kind>:<name>" (e.g. "skill:deploy", "command:build") to prevent
	// cross-kind collisions (a skill "build" and a command "build" are
	// distinct resources).
	ViaMap map[string]string
}

// ViaMapKey returns a kind-aware key for the ViaMap, formatted as
// "<kind>:<name>". This prevents cross-kind collisions where e.g. a
// skill "build" and a command "build" would otherwise share a key.
func ViaMapKey(kind ir.ResourceKind, name string) string {
	return kind.String() + ":" + strings.TrimSpace(name)
}

// Expand resolves each selected collection name against the available discovered
// collections. It returns synthetic RegistryEntry items (one per unique registry
// referenced by collection skills) and a Via annotation map.
//
// The selected collections come from the merged config's Collections KindSelector
// Include list and from per-registry-entry Collections selectors.
// Skills contributed by a collection that specifies no registry are assumed to
// come from the collection's source registry. Skills that specify a registry
// that is not present in existingEntries produce an error — those registries
// must be configured (or fetched) before expansion.
func Expand(selectedNames []string, available []Collection, existingEntries []config.RegistryEntry) (*ExpandResult, []error) {
	if len(selectedNames) == 0 {
		return &ExpandResult{ViaMap: map[string]string{}}, nil
	}

	result := &ExpandResult{ViaMap: map[string]string{}}

	// Index available collections by name for fast lookup.
	index := make(map[string]*Collection, len(available))
	for i := range available {
		c := &available[i]
		index[strings.TrimSpace(c.Name)] = c
	}

	// Build set of resources already directly configured (by kind+name) so we
	// can skip Via annotation for them. Keyed by kind:name to prevent
	// cross-kind collisions (a directly-configured command "build" should not
	// suppress the Via annotation for a collected skill "build").
	directNames := make(map[string]struct{})
	for _, entry := range existingEntries {
		for _, kind := range config.AllKinds {
			for _, s := range entry.SelectorForKind(kind).Include {
				directNames[ViaMapKey(kind, strings.TrimSpace(s))] = struct{}{}
			}
		}
	}

	// kindMap accumulates names per (registryURL, ref, path, kind) so we emit
	// at most one synthetic entry per registry+path combination with all kinds
	// populated on it.
	type registryKey struct{ url, ref, path string }
	type kindNames = map[ir.ResourceKind][]string
	syntheticMap := make(map[registryKey]kindNames)
	syntheticSource := make(map[registryKey]config.Source)
	syntheticCatalogKey := make(map[registryKey]string)
	syntheticURL := make(map[registryKey]string)
	syntheticHarnesses := make(map[registryKey][]string)
	syntheticSourceHarness := make(map[registryKey]string)

	var errs []error

	for _, refName := range selectedNames {
		refName = strings.TrimSpace(refName)
		if refName == "" {
			continue
		}
		col := Find(available, refName)
		if col == nil {
			errs = append(errs, fmt.Errorf("collection %q not found in configured registries", refName))
			continue
		}

		viaLabel := "collection:" + col.Label()
		visited := map[string]bool{}

		resByReg, authoredURLs, expandErrs := expandCollection(col, index, existingEntries, visited)
		errs = append(errs, expandErrs...)

		for rk, kinds := range resByReg {
			if syntheticMap[rk] == nil {
				syntheticMap[rk] = make(kindNames)
			}
			for kind, names := range kinds {
				syntheticMap[rk][kind] = appendUnique(syntheticMap[rk][kind], names)
			}
			// Determine source, catalog key, and exclude list from existing entries.
			if _, ok := syntheticSource[rk]; !ok {
				src := sourceForRegistry(rk.url, rk.ref, rk.path, existingEntries, col.SourceRegistryURL, col.SourceRef, col.SourcePath)
				syntheticSource[rk] = src
				syntheticURL[rk] = authoredURLs[rk]
				if syntheticURL[rk] == "" {
					syntheticURL[rk] = authoredURLForRegistry(rk.url, rk.ref, rk.path, existingEntries, col.SourceRegistryURL)
				}
				syntheticCatalogKey[rk] = catalogKeyForRegistry(rk.url, rk.ref, rk.path, existingEntries, col.SourceCatalogKey)
				syntheticHarnesses[rk] = harnessesForRegistry(rk.url, rk.ref, rk.path, existingEntries)
				syntheticSourceHarness[rk] = sourceHarnessForRegistry(rk.url, rk.ref, rk.path, existingEntries)
			}

			for kind, names := range kinds {
				for _, name := range names {
					if _, direct := directNames[ViaMapKey(kind, name)]; !direct {
						result.ViaMap[ViaMapKey(kind, name)] = viaLabel
					}
				}
			}
		}
	}

	// Emit synthetic entries in a deterministic order so CLI output is stable.
	keys := make([]registryKey, 0, len(syntheticMap))
	for rk := range syntheticMap {
		keys = append(keys, rk)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].url != keys[j].url {
			return keys[i].url < keys[j].url
		}
		if keys[i].ref != keys[j].ref {
			return keys[i].ref < keys[j].ref
		}
		return keys[i].path < keys[j].path
	})

	for _, rk := range keys {
		kinds := syntheticMap[rk]
		entry := config.RegistryEntry{
			URL:           syntheticURL[rk],
			Ref:           rk.ref,
			Path:          rk.path,
			Source:        syntheticSource[rk],
			CatalogKey:    syntheticCatalogKey[rk],
			Harnesses:     syntheticHarnesses[rk],
			SourceHarness: syntheticSourceHarness[rk],
		}
		// Populate per-kind selectors from accumulated names.
		for _, kind := range config.AllKinds {
			names := kinds[kind]
			excl := excludeForRegistryWithPath(rk.url, rk.ref, rk.path, existingEntries)
			if ptr := entry.SelectorForKindPtr(kind); ptr != nil {
				ptr.Include = names
				ptr.Exclude = excl
			}
		}
		result.SyntheticEntries = append(result.SyntheticEntries, entry)
	}

	return result, errs
}

// cloneVisited gives each include branch its own ancestry, so shared DAG nodes
// are valid while repeats on one ancestry path remain cycle errors.
func cloneVisited(visited map[string]bool) map[string]bool {
	branch := make(map[string]bool, len(visited))
	for seen := range visited {
		branch[seen] = true
	}
	return branch
}

// expandCollection recursively expands a single collection (following includes)
// and returns a map of registryKey → (kind → names).
func expandCollection(col *Collection, index map[string]*Collection, existingEntries []config.RegistryEntry, visited map[string]bool) (map[struct{ url, ref, path string }]map[ir.ResourceKind][]string, map[struct{ url, ref, path string }]string, []error) {
	key := strings.TrimSpace(col.Name)
	if visited[key] {
		return nil, nil, []error{fmt.Errorf("collection cycle detected at %q", col.Label())}
	}
	visited[key] = true

	type rk = struct{ url, ref, path string }
	type kindNames = map[ir.ResourceKind][]string
	result := make(map[rk]kindNames)
	authoredURLs := make(map[rk]string)
	var errs []error

	// processEntry adds a single resource entry to result, resolving its
	// registry/ref/path and populating the correct kind bucket.
	processEntry := func(name, kindStr, regURL, ref, skillPath string, pathProvided bool) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		kind, kindErr := parseKind(kindStr)
		if kindErr != nil {
			errs = append(errs, fmt.Errorf("collection %q: resource %q: %w", col.Label(), name, kindErr))
			return
		}

		regURL = strings.TrimSpace(regURL)
		ref = strings.TrimSpace(ref)
		// Normalize only the identity path. Keep pathProvided so an explicit
		// root ("." or "./") does not inherit the collection's source path.
		skillPath = config.NormalizeRegistryPath(skillPath)

		if regURL == "" {
			// Use the collection's source registry while preserving an explicit
			// member ref. A member may intentionally select another configured
			// ref of the same repository.
			regURL = strings.TrimSpace(col.SourceRegistryURL)
			if ref == "" {
				ref = strings.TrimSpace(col.SourceRef)
			}
			if !pathProvided {
				skillPath = config.NormalizeRegistryPath(col.SourcePath)
			}
		} else if ref == "" {
			configured := findConfiguredRef(regURL, existingEntries)
			isLocal := isLocalRegistry(regURL, existingEntries)
			if configured == "" && !isLocal {
				errs = append(errs, fmt.Errorf("collection %q: %s %q references registry %q which has no configured ref and none was provided", col.Label(), kind, name, regURL))
				return
			}
			ref = configured
		} else {
			if findConfiguredRef(regURL, existingEntries) == "" && !isLocalRegistry(regURL, existingEntries) {
				errs = append(errs, fmt.Errorf("collection %q: %s %q references registry %q@%q which is not present in any config layer; add it to your registry list first", col.Label(), kind, name, regURL, ref))
				return
			}
		}

		// Registry URL spelling is identity, not authored metadata: collapse
		// equivalent `repo`/`repo.git` references into one synthetic source.
		k := rk{url: config.NormalizeGitURL(regURL), ref: ref, path: skillPath}
		if result[k] == nil {
			result[k] = make(kindNames)
			authoredURLs[k] = regURL
		}
		result[k][kind] = appendUnique(result[k][kind], []string{name})
	}

	// Process legacy skills: key.
	for _, cs := range col.Skills {
		processEntry(cs.Name, "", cs.Registry, cs.Ref, cs.Path, strings.TrimSpace(cs.Path) != "")
	}

	// Process general resources: key.
	for _, ce := range col.Resources {
		processEntry(ce.Name, ce.Kind, ce.Registry, ce.Ref, ce.Path, strings.TrimSpace(ce.Path) != "")
	}

	// Expand includes recursively.
	for _, inc := range col.Includes {
		included := Find(allCollections(index), inc.Name)
		if included == nil {
			errs = append(errs, fmt.Errorf("collection %q includes %q which was not found in configured registries", col.Label(), inc.Name))
			continue
		}

		branchVisited := cloneVisited(visited)
		subResult, subAuthoredURLs, subErrs := expandCollection(included, index, existingEntries, branchVisited)
		errs = append(errs, subErrs...)
		for k, kinds := range subResult {
			if result[k] == nil {
				result[k] = make(kindNames)
				authoredURLs[k] = subAuthoredURLs[k]
			}
			for kind, names := range kinds {
				result[k][kind] = appendUnique(result[k][kind], names)
			}
		}
	}

	return result, authoredURLs, errs
}

// parseKind maps a kind string from a CollectionEntry to an ir.ResourceKind.
// Empty string defaults to KindSkill. Unknown kinds return an error so typos
// are surfaced early instead of silently defaulting to the wrong kind.
func parseKind(s string) (ir.ResourceKind, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "", "skill":
		return ir.KindSkill, nil
	case "command":
		return ir.KindCommand, nil
	case "subagent":
		return ir.KindSubagent, nil
	case "hook":
		return ir.KindHook, nil
	case "mcp", "mcpserver":
		return ir.KindMcpServer, nil
	default:
		return ir.KindSkill, fmt.Errorf("unsupported resource kind %q", s)
	}
}

func allCollections(index map[string]*Collection) []Collection {
	out := make([]Collection, 0, len(index))
	for _, c := range index {
		out = append(out, *c)
	}
	return out
}

func appendUnique(existing, add []string) []string {
	set := make(map[string]struct{}, len(existing))
	for _, v := range existing {
		set[v] = struct{}{}
	}
	result := append([]string(nil), existing...)
	for _, v := range add {
		if _, ok := set[v]; !ok {
			set[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// authoredURLForRegistry returns the URL spelling from the configured source
// while the caller's normalized URL remains the synthetic identity. This keeps
// generated config metadata deterministic and preserves the author's spelling.
func authoredURLForRegistry(url, ref, registryPath string, entries []config.RegistryEntry, fallbackURL string) string {
	norm := config.NormalizeGitURL(url)
	registryPath = config.NormalizeRegistryPath(registryPath)
	bestURL := ""
	bestSource := config.SourceUnset
	bestFile := 0
	for _, entry := range entries {
		if config.NormalizeGitURL(entry.URL) != norm || strings.TrimSpace(entry.Ref) != ref || config.NormalizeRegistryPath(entry.Path) != registryPath {
			continue
		}
		if bestURL == "" || config.CompareEntryPrecedence(entry.Source, entry.FilePrecedence, bestSource, bestFile) > 0 {
			bestURL = strings.TrimSpace(entry.URL)
			bestSource = entry.Source
			bestFile = entry.FilePrecedence
		}
	}
	if bestURL != "" {
		return bestURL
	}
	if config.NormalizeGitURL(fallbackURL) == norm {
		return strings.TrimSpace(fallbackURL)
	}
	return strings.TrimSpace(url)
}

func excludeForRegistry(url string, entries []config.RegistryEntry) []string {
	norm := config.NormalizeGitURL(url)
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm {
			return append([]string(nil), e.AllExcludes()...)
		}
	}
	return nil
}

// excludeForRegistryWithPath returns the exclude list from the first entry
// matching (url, ref, path). Falls back to (url, ref) matching when path is
// empty. Used by Expand for path-specific synthetic entries.
func excludeForRegistryWithPath(url, ref, path string, entries []config.RegistryEntry) []string {
	path = config.NormalizeRegistryPath(path)
	norm := config.NormalizeGitURL(url)
	// Prefer exact (url, ref, path) match first.
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref && config.NormalizeRegistryPath(e.Path) == config.NormalizeRegistryPath(path) {
			return append([]string(nil), e.AllExcludes()...)
		}
	}
	// Fallback: try (url, ref) only when path is empty.
	if path == "" {
		for _, e := range entries {
			if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref {
				return append([]string(nil), e.AllExcludes()...)
			}
		}
	}
	return nil
}

func findConfiguredRef(registryURL string, entries []config.RegistryEntry) string {
	norm := config.NormalizeGitURL(registryURL)
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm {
			return e.Ref
		}
	}
	return ""
}

// isLocalRegistry returns true if the given URL matches a configured local
// filesystem registry. Local registries have no ref, so the ref-matching
// logic in expandCollection must treat empty ref as valid for them.
func isLocalRegistry(registryURL string, entries []config.RegistryEntry) bool {
	norm := config.NormalizeGitURL(registryURL)
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm && e.IsLocal() {
			return true
		}
	}
	return false
}

func sourceForRegistry(url, ref, path string, entries []config.RegistryEntry, fallbackURL, fallbackRef, fallbackPath string) config.Source {
	path = config.NormalizeRegistryPath(path)
	fallbackPath = config.NormalizeRegistryPath(fallbackPath)
	norm := config.NormalizeGitURL(url)
	best := config.SourceUnset
	// Prefer exact (url, ref, path) match first.
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref && config.NormalizeRegistryPath(e.Path) == config.NormalizeRegistryPath(path) {
			if e.Source.Precedence() > best.Precedence() {
				best = e.Source
			}
		}
	}
	// Fallback: try (url, ref) only when path is empty, matching entries
	// that have a path but the collection skill does not specify one.
	if best == config.SourceUnset && path == "" {
		for _, e := range entries {
			if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref {
				if e.Source.Precedence() > best.Precedence() {
					best = e.Source
				}
			}
		}
	}
	if best != config.SourceUnset {
		// Also consider the collection's source registry as a floor: if the
		// collection definition lives in a higher-precedence layer than the
		// target registry's configured scope, the synthetic entries must
		// inherit that scope. Without this, a RepoCommitted collection that
		// references a registry which is only configured at UserPersonal would
		// produce machine-scoped entries, causing an install→prune cycle every
		// time the user changes context to and from the project directory.
		// Match by (url, ref, path) first, then fall back to (url, ref) when
		// fallbackPath is empty.
		normFallback := config.NormalizeGitURL(fallbackURL)
		for _, e := range entries {
			if config.NormalizeGitURL(e.URL) == normFallback && e.Ref == fallbackRef && config.NormalizeRegistryPath(e.Path) == config.NormalizeRegistryPath(fallbackPath) {
				if e.Source.Precedence() > best.Precedence() {
					best = e.Source
				}
			}
		}
		if fallbackPath == "" {
			for _, e := range entries {
				if config.NormalizeGitURL(e.URL) == normFallback && e.Ref == fallbackRef {
					if e.Source.Precedence() > best.Precedence() {
						best = e.Source
					}
				}
			}
		}
		return best
	}
	if config.NormalizeGitURL(url) == config.NormalizeGitURL(fallbackURL) && ref == fallbackRef {
		// Target IS the source registry — find its highest-precedence source.
		// Match by path first, then fall back when fallbackPath is empty.
		normFallback := config.NormalizeGitURL(fallbackURL)
		for _, e := range entries {
			if config.NormalizeGitURL(e.URL) == normFallback && e.Ref == fallbackRef && config.NormalizeRegistryPath(e.Path) == config.NormalizeRegistryPath(fallbackPath) {
				if e.Source.Precedence() > best.Precedence() {
					best = e.Source
				}
			}
		}
		if fallbackPath == "" {
			for _, e := range entries {
				if config.NormalizeGitURL(e.URL) == normFallback && e.Ref == fallbackRef {
					if e.Source.Precedence() > best.Precedence() {
						best = e.Source
					}
				}
			}
		}
		return best
	}
	return config.SourceUnset
}

func catalogKeyForRegistry(url, ref, path string, entries []config.RegistryEntry, fallbackCatalogKey string) string {
	path = config.NormalizeRegistryPath(path)
	norm := config.NormalizeGitURL(url)
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref && config.NormalizeRegistryPath(e.Path) == config.NormalizeRegistryPath(path) && e.CatalogKey != "" {
			return e.CatalogKey
		}
	}
	// Fallback: try matching on url+ref only when path is empty, to support
	// entries that have a path but the collection skill does not specify one.
	if path == "" {
		for _, e := range entries {
			if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref && e.CatalogKey != "" {
				return e.CatalogKey
			}
		}
	}
	return fallbackCatalogKey
}

func harnessesForRegistry(url, ref, path string, entries []config.RegistryEntry) []string {
	path = config.NormalizeRegistryPath(path)
	norm := config.NormalizeGitURL(url)
	// Prefer exact (url, ref, path) match first.
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref && config.NormalizeRegistryPath(e.Path) == config.NormalizeRegistryPath(path) {
			return append([]string(nil), e.Harnesses...)
		}
	}
	// Fallback: try (url, ref) only when path is empty.
	if path == "" {
		for _, e := range entries {
			if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref {
				return append([]string(nil), e.Harnesses...)
			}
		}
	}
	return nil
}

func sourceHarnessForRegistry(url, ref, path string, entries []config.RegistryEntry) string {
	path = config.NormalizeRegistryPath(path)
	norm := config.NormalizeGitURL(url)
	// Prefer exact (url, ref, path) match first.
	for _, e := range entries {
		if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref && config.NormalizeRegistryPath(e.Path) == config.NormalizeRegistryPath(path) {
			return e.SourceHarness
		}
	}
	// Fallback: try (url, ref) only when path is empty.
	if path == "" {
		for _, e := range entries {
			if config.NormalizeGitURL(e.URL) == norm && e.Ref == ref {
				return e.SourceHarness
			}
		}
	}
	return ""
}
