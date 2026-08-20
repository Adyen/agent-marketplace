package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
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
	RepoRoot string
	Bump     bool
}

// UpdateResult holds the outcome of an update operation.
type UpdateResult struct {
	UpdatedRegistries int
	Bumps             []RefBump
}

// Update refreshes pinned registries and syncs enabled skills.
// When bump is true, it also bumps repo registry refs to newer matching tags.
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

	// Load the effective configuration to obtain the authoritative allowlist
	// and validate all configured registries before any network access.
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot})
	if len(ec.Errors) > 0 {
		slog.Error("update aborted on effective config errors", "error_count", len(ec.Errors))
		return nil, errors.Join(ec.Errors...)
	}

	updatedRegistries := 0
	if ec.Merged != nil {
		updatedRegistries = len(ec.Merged.Registries)
	}

	var bumps []RefBump
	if opts.Bump {
		repoCfg := ec.Repo.Config
		if repoCfg != nil && len(repoCfg.Registries) > 0 {
			// Validate repo registries against the effective allowlist before
			// fetching tags from remote.
			if err := config.ValidateRegistryURLs(repoCfg.Registries, ec.Allowed); err != nil {
				slog.Error("update blocked by allowed_registries", "error", err)
				return nil, err
			}
			configPath := config.ProjectConfigPath(repoRoot)
			var err error
			bumps, err = bumpRepoRegistryRefs(ctx, configPath, repoCfg)
			if err != nil {
				return nil, err
			}
		} else {
			slog.Info("update skipping bump", "reason", "no repository registries")
		}
	}

	if err := silentSync(ctx, repoRoot); err != nil {
		return nil, err
	}

	slog.Info("update end", "updated_registries", updatedRegistries, "bumps", len(bumps))
	return &UpdateResult{UpdatedRegistries: updatedRegistries, Bumps: bumps}, nil
}

func bumpRepoRegistryRefs(ctx context.Context, configPath string, repoCfg *config.Config) (bumps []RefBump, retErr error) {
	if repoCfg == nil || len(repoCfg.Registries) == 0 {
		return nil, nil
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
		if entry.IsLocal() {
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
