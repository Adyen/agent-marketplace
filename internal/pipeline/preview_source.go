package pipeline

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// SourcePreview is the resolved identity and read-only catalog for an unconfigured source.
type SourcePreview struct {
	URL         string
	Ref         string
	Path        string
	Commit      string
	Catalogs    map[string]catalog.LoadedCatalog
	Collections []collection.Collection
	BundlePaths []string
	Warnings    []error
}

// SourceRejectedError reports source policy failures that must be shown before fetching.
type SourceRejectedError struct {
	Err error
}

func (e *SourceRejectedError) Error() string {
	return e.Err.Error()
}

func (e *SourceRejectedError) Unwrap() error {
	return e.Err
}

// PreviewSource resolves and loads an unconfigured source without publishing configuration or locks.
func PreviewSource(ctx context.Context, ws Workspace, rawURL, ref, path string) (SourcePreview, error) {
	preview := SourcePreview{}
	if err := ws.validate(); err != nil {
		return preview, err
	}

	rawURL, ref, path, _ = ResolveFromURL(rawURL, ref, path)
	preview.URL, preview.Ref, preview.Path = rawURL, ref, path
	entry := config.RegistryEntry{URL: rawURL, Ref: ref, Path: path}

	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{
		RepoRoot:       ws.RepoRoot(),
		LocalConfigDir: ws.LocalConfigDir(),
	})
	preview.Warnings = append(preview.Warnings, intent.Warnings...)
	if len(intent.Errors) > 0 {
		return preview, errors.Join(intent.Errors...)
	}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{entry}, intent.AllowedRegistries()); err != nil {
		return preview, &SourceRejectedError{Err: err}
	}
	if err := config.ValidateRegistryPath(entry.Path); err != nil {
		return preview, &SourceRejectedError{Err: err}
	}

	if !entry.IsLocal() && ref == "" {
		defaultBranch, err := cache.DefaultBranch(rawURL)
		if err != nil {
			return preview, err
		}
		ref = defaultBranch
		entry.Ref = ref
		preview.Ref = ref
	}

	cfg := &config.Config{APIVersion: config.APIVersionV4, Registries: []config.RegistryEntry{entry}}
	result := catalog.LoadRegistriesWithContext(ctx, cfg, ws.RepoRoot(), catalog.LoadModeRefresh, catalog.WithLocalConfigDir(ws.LocalConfigDir()))
	var hardErrors []error
	for _, loadErr := range result.Errors {
		if catalog.IsOverlapWarning(loadErr) {
			preview.Warnings = append(preview.Warnings, loadErr)
			continue
		}
		hardErrors = append(hardErrors, loadErr)
	}
	if err := errors.Join(hardErrors...); err != nil {
		return preview, err
	}
	preview.Warnings = append(preview.Warnings, RequiresInvalidWarnings(result.Catalogs)...)
	preview.Catalogs = result.Catalogs
	preview.Collections = result.Collections
	if len(result.Catalogs) > 1 {
		for _, resolved := range result.Config.Registries {
			if sourceEntryMatches(resolved, entry) {
				preview.BundlePaths = append(preview.BundlePaths, resolved.Path)
			}
		}
		slices.Sort(preview.BundlePaths)
		preview.BundlePaths = slices.Compact(preview.BundlePaths)
	}
	if !entry.IsLocal() {
		commit, err := cache.ResolveRemoteRef(cache.RegistryCachePath(rawURL, ref), ref)
		if err != nil {
			return preview, fmt.Errorf("resolve preview commit for %s@%s: %w", rawURL, ref, err)
		}
		preview.Commit = commit
	}
	return preview, nil
}

// RequiresInvalidWarnings returns the deterministic soft warnings for invalid resource prerequisites.
func RequiresInvalidWarnings(catalogs map[string]catalog.LoadedCatalog) []error {
	var warnings []error
	for _, loaded := range catalogs {
		for _, diagnostic := range loaded.Diagnostics {
			if diagnostic.Code == catalog.CodeRequiresInvalid {
				warnings = append(warnings, PromoteDiagnostic(diagnostic))
			}
		}
	}
	warnings = DedupIssues(warnings)
	slices.SortFunc(warnings, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return warnings
}

func sourceEntryMatches(candidate, source config.RegistryEntry) bool {
	if candidate.IsLocal() != source.IsLocal() {
		return false
	}
	if source.IsLocal() {
		return candidate.CatalogKey != "" && strings.TrimSpace(candidate.URL) == strings.TrimSpace(source.URL)
	}
	return config.NormalizeGitURL(candidate.URL) == config.NormalizeGitURL(source.URL) && strings.TrimSpace(candidate.Ref) == strings.TrimSpace(source.Ref)
}
