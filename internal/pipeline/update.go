package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

var dottedVersionRefPattern = regexp.MustCompile(`^(.*?)(\d+)$`)

// RefBump records a single registry ref update.
type RefBump struct {
	URL    string
	OldRef string
	NewRef string
}

// UpdateOptions holds the arguments for an update operation.
type UpdateOptions struct {
	RepoRoot            string
	MaterializeRoot     string
	LocalConfigDir      string
	Bump                bool
	BumpScopes          map[lockfile.Scope]bool
	BumpTarget          *UpdateTarget
	DependencyReconcile UpdateDependencyReconciler
}

type UpdateTarget struct {
	Registry string
	Ref      string
	Path     string
	Kind     string
	Names    []string
}

type UpdateDependencyReconciler func(context.Context, string, string, []RefBump, []StagedConfigEdit) (DependencyLockView, error)

// UpdateResult holds the outcome of an update operation.
type UpdateResult struct {
	UpdatedRegistries int
	Bumps             []RefBump
}

// Update refreshes selected Git dependency locks and syncs enabled resources.
func Update(ctx context.Context, opts UpdateOptions) (result *UpdateResult, retErr error) {
	tel := telemetry.TelemetryFromContext(ctx)
	var updateSpan telemetry.Span
	if tel != nil {
		ctx, updateSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.update", updateOptionAttrs(opts))
	}
	defer func() {
		if updateSpan != (telemetry.Span{}) {
			updateSpan.SetAttributes(updateResultAttrs(result, retErr))
			updateSpan.End()
		}
	}()

	repoRoot := opts.RepoRoot
	slog.Info("update start", "repo_root", repoRoot, "bump", opts.Bump)
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: opts.LocalConfigDir})
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}
	if opts.DependencyReconcile == nil {
		return nil, errors.New("update requires dependency lock reconciliation")
	}
	staging, err := newConfigStagingManager()
	if err != nil {
		return nil, err
	}
	defer closeConfigStaging(staging, "update", &retErr)

	updatedRegistries := 0
	if ec.Merged != nil {
		updatedRegistries = len(ec.Merged.Registries)
	}
	var bumps []RefBump
	if opts.Bump {
		for _, layer := range ec.PresentLayers() {
			if !layer.Source.IsWritable() {
				continue
			}
			scope := lockfile.Scope(layer.Source.LockScope())
			if !bumpScopeSelected(opts.BumpScopes, scope) {
				continue
			}
			registries := layerRegistriesForUpdate(layer)
			if len(registries) == 0 {
				continue
			}
			if err := config.ValidateRegistryURLs(registries, ec.Allowed); err != nil {
				return nil, err
			}
			configPath, err := staging.Path(layer.Path)
			if err != nil {
				return nil, err
			}
			layerBumps, err := bumpRepoRegistryRefs(ctx, configPath, &config.Config{Registries: registries}, opts.BumpTarget)
			if err != nil {
				return nil, err
			}
			bumps = append(bumps, layerBumps...)
		}
	}

	syncOpts := silentSyncOptsForPaths(repoRoot, opts.MaterializeRoot, opts.LocalConfigDir)
	syncOpts = append(syncOpts, withConfigStaging(staging), withDependencyReconciler(func(ctx context.Context, repoRoot, localDir string, _ bool, edits []StagedConfigEdit) (DependencyLockView, error) {
		return opts.DependencyReconcile(ctx, repoRoot, localDir, bumps, edits)
	}))
	if _, err := silentSyncCollect(ctx, repoRoot, syncOpts...); err != nil {
		return nil, err
	}
	slog.Info("update end", "updated_registries", updatedRegistries, "bumps", len(bumps))
	return &UpdateResult{UpdatedRegistries: updatedRegistries, Bumps: bumps}, nil
}

func layerRegistriesForUpdate(layer config.LayerResult) []config.RegistryEntry {
	if layer.Config != nil {
		return layer.Config.Registries
	}
	if layer.Local != nil {
		return layer.Local.Registries
	}
	return nil
}

func bumpScopeSelected(scopes map[lockfile.Scope]bool, scope lockfile.Scope) bool {
	if scopes == nil {
		return scope == lockfile.ScopeRepo
	}
	return scopes[scope]
}

func bumpRepoRegistryRefs(ctx context.Context, configPath string, repoCfg *config.Config, target *UpdateTarget) (bumps []RefBump, retErr error) {
	if repoCfg == nil || len(repoCfg.Registries) == 0 {
		return nil, nil
	}
	wantedURL := ""
	if target != nil && strings.TrimSpace(target.Registry) != "" {
		var err error
		wantedURL, err = canonicalUpdateRegistryURL(target.Registry)
		if err != nil {
			return nil, fmt.Errorf("canonicalize update registry target: %w", err)
		}
	}

	tel := telemetry.TelemetryFromContext(ctx)
	var bumpSpan telemetry.Span
	if tel != nil {
		_, bumpSpan = tel.StartSpan(ctx, "agent_marketplace.pipeline.update_refs", updateRefsAttrs(repoCfg))
	}
	defer func() {
		if bumpSpan != (telemetry.Span{}) {
			bumpSpan.SetAttributes(updateRefsResultAttrs(bumps, retErr))
			bumpSpan.End()
		}
	}()

	bumps = make([]RefBump, 0)
	planned := make(map[string]string)

	for _, entry := range repoCfg.Registries {
		if entry.IsLocal() || !updateTargetMatchesEntry(entry, target, wantedURL) {
			continue
		}

		cachePath, err := cache.CloneOrPull(entry.URL, entry.Ref)
		if err != nil {
			return nil, err
		}

		newRef, ok, err := discoverTagBump(cachePath, entry.Ref)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		key := entry.URL + "\x00" + entry.Ref
		if existing, exists := planned[key]; exists {
			if existing != newRef {
				return nil, fmt.Errorf("conflicting bump targets for %s@%s: %s vs %s", entry.URL, entry.Ref, existing, newRef)
			}
			continue
		}

		planned[key] = newRef
		bumps = append(bumps, RefBump{
			URL:    entry.URL,
			OldRef: entry.Ref,
			NewRef: newRef,
		})
	}

	for _, bump := range bumps {
		if _, err := config.UpdateRegistryRefInConfig(configPath, bump.URL, bump.OldRef, bump.NewRef); err != nil {
			return nil, err
		}
	}

	return bumps, nil
}

func canonicalUpdateRegistryURL(raw string) (string, error) {
	return lockfile.CanonicalGitConfigURL(raw)
}

func updateTargetMatchesEntry(entry config.RegistryEntry, target *UpdateTarget, wantedURL string) bool {
	if target == nil {
		return true
	}
	if wantedURL != "" {
		entryURL, err := canonicalUpdateRegistryURL(entry.URL)
		if err != nil || entryURL != wantedURL {
			return false
		}
	}
	if target.Ref != "" && target.Ref != entry.Ref {
		return false
	}
	if target.Path != "" && normalizeUpdateRoot(target.Path) != normalizeUpdateRoot(entry.Path) {
		return false
	}
	return true
}

func normalizeUpdateRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "."
	}
	return pathpkg.Clean(root)
}

func discoverTagBump(repoDir, currentRef string) (string, bool, error) {
	currentRef = strings.TrimSpace(currentRef)
	matches := dottedVersionRefPattern.FindStringSubmatch(currentRef)
	if len(matches) != 3 {
		return "", false, nil
	}

	currentNumber, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", false, fmt.Errorf("parse current ref %q: %w", currentRef, err)
	}

	tags, err := listGitTags(repoDir, matches[1]+"*")
	if err != nil {
		return "", false, err
	}

	bestRef := ""
	bestNumber := currentNumber
	for _, tag := range tags {
		suffix := strings.TrimPrefix(tag, matches[1])
		if suffix == "" || strings.ContainsFunc(suffix, func(r rune) bool { return r < '0' || r > '9' }) {
			continue
		}

		tagNumber, err := strconv.Atoi(suffix)
		if err != nil {
			return "", false, fmt.Errorf("parse tag %q: %w", tag, err)
		}

		if tagNumber > bestNumber || (tagNumber == bestNumber && tag > bestRef) {
			bestRef = tag
			bestNumber = tagNumber
		}
	}

	if bestRef == "" {
		return "", false, nil
	}

	return bestRef, true, nil
}

func listGitTags(repoDir, pattern string) ([]string, error) {
	args := []string{"tag", "-l"}
	if strings.TrimSpace(pattern) != "" {
		args = append(args, pattern)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %q: %w", strings.Join(args, " "), repoDir, err)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}

	return strings.Split(trimmed, "\n"), nil
}
