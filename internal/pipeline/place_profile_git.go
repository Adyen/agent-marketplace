package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

type deployedGitPlaceProfileCandidate struct {
	layer  config.Source
	url    string
	origin string
	ref    string
	locked lockfile.GitRegistry
}

// LockedGitPlaceProfileReader returns a reader for one exact profile in a
// trusted, deployed Git lock record. CachedNoFetch uses only the exact
// snapshot already present in the local cache.
func LockedGitPlaceProfileReader(locked lockfile.GitRegistry, fetchURL string) PlaceProfileReader {
	return func(ctx context.Context, identity targetprofile.Identity, mode resolver.MarketplaceResolveMode) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if mode != resolver.MarketplaceResolveModeRefresh && mode != resolver.MarketplaceResolveModeCachedNoFetch {
			return nil, fmt.Errorf("unsupported Marketplace place profile read mode %d", mode)
		}
		if err := identity.Validate(); err != nil {
			return nil, err
		}
		source, err := targetprofile.ParseSourceID(identity.SourceID)
		if err != nil {
			return nil, err
		}
		if source.Kind != "git" || source.Origin != locked.URL || source.Ref != locked.Ref {
			return nil, fmt.Errorf("marketplace.profile.identity-mismatch: target %s source does not match locked Git origin and ref", identity.TargetID)
		}
		if !validGitPlaceProfileRoot(source.Root) {
			return nil, fmt.Errorf("marketplace.profile.source-invalid: target %s has an invalid profile path", identity.TargetID)
		}
		commit, err := lockfile.ParseGitOID(locked.Commit.String())
		if err != nil {
			return nil, fmt.Errorf("marketplace.profile.lock-invalid: target %s has an invalid locked commit: %w", identity.TargetID, err)
		}

		var raw []byte
		consume := func(snapshot string) (consumeErr error) {
			root, err := fsboundary.OpenDir(snapshot)
			if err != nil {
				return err
			}
			defer func() { consumeErr = errors.Join(consumeErr, root.Close()) }()
			raw, _, err = root.ReadFile(source.Root, targetprofile.MaxProfileBytes)
			if err != nil {
				return err
			}
			return nil
		}
		var cacheErr error
		switch mode {
		case resolver.MarketplaceResolveModeRefresh:
			fetchCanonical, err := gitidentity.CanonicalV1(fetchURL)
			if err != nil {
				return nil, fmt.Errorf("marketplace.profile.unavailable: target %s fetch URL: %w", identity.TargetID, err)
			}
			if fetchCanonical != source.Origin {
				return nil, fmt.Errorf("marketplace.profile.identity-mismatch: target %s fetch URL does not match locked Git origin", identity.TargetID)
			}
			cacheErr = cache.WithAcquiredExactGitRoot(ctx, source.Origin, fetchURL, commit, path.Dir(source.Root), consume)
		case resolver.MarketplaceResolveModeCachedNoFetch:
			cacheErr = cache.WithExactGitReadOnlyRoot(ctx, source.Origin, commit, path.Dir(source.Root), consume)
		}
		if cacheErr != nil {
			if errors.Is(cacheErr, context.Canceled) || errors.Is(cacheErr, context.DeadlineExceeded) {
				return nil, cacheErr
			}
			return nil, fmt.Errorf("marketplace.profile.unavailable: target %s: %w", identity.TargetID, cacheErr)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := targetprofile.Parse(raw, identity); err != nil {
			return nil, fmt.Errorf("target %s profile validation: %w", identity.TargetID, err)
		}
		return raw, nil
	}
}

// DeployedGitPlaceProfileReader routes profile reads only through Git sources
// and locks frozen from the deployed organization and user configuration layers.
func DeployedGitPlaceProfileReader(intent config.Intent, evidence config.Evidence) PlaceProfileReader {
	candidates := deployedGitPlaceProfileCandidates(intent, evidence)
	return func(ctx context.Context, identity targetprofile.Identity, mode resolver.MarketplaceResolveMode) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if mode != resolver.MarketplaceResolveModeRefresh && mode != resolver.MarketplaceResolveModeCachedNoFetch {
			return nil, fmt.Errorf("unsupported Marketplace place profile read mode %d", mode)
		}
		if err := identity.Validate(); err != nil {
			return nil, err
		}
		source, err := targetprofile.ParseSourceID(identity.SourceID)
		if err != nil {
			return nil, err
		}
		if source.Kind != "git" {
			return nil, fmt.Errorf("marketplace.profile.lock-missing: target %s has no deployed Git profile source", identity.TargetID)
		}
		var matches []deployedGitPlaceProfileCandidate
		for _, candidate := range candidates {
			if candidate.origin == source.Origin && candidate.ref == source.Ref &&
				candidate.locked.URL == source.Origin && candidate.locked.Ref == source.Ref {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("marketplace.profile.lock-missing: target %s has no matching deployed machine-scope Git lock", identity.TargetID)
		}
		commit := matches[0].locked.Commit.String()
		distinctCommit := false
		layers := make([]string, 0, len(matches))
		seenLayers := make(map[config.Source]struct{}, len(matches))
		for _, candidate := range matches {
			if candidate.locked.Commit.String() != commit {
				distinctCommit = true
			}
			if _, seen := seenLayers[candidate.layer]; !seen {
				seenLayers[candidate.layer] = struct{}{}
				layers = append(layers, candidate.layer.String())
			}
		}
		if distinctCommit {
			return nil, fmt.Errorf(
				"marketplace.profile.selection-ambiguous: target %s has distinct deployed Git commits in %s layers",
				identity.TargetID, strings.Join(layers, ", "),
			)
		}
		selected := matches[0]
		return LockedGitPlaceProfileReader(selected.locked, selected.url)(ctx, identity, mode)
	}
}

func deployedGitPlaceProfileCandidates(intent config.Intent, evidence config.Evidence) []deployedGitPlaceProfileCandidate {
	var candidates []deployedGitPlaceProfileCandidate
	for _, source := range []config.Source{config.Organization, config.UserPersonal} {
		layer := intent.Layers[source]
		if layer.Source != source || !layer.Present() || layer.Err() != nil {
			continue
		}
		doc := evidence.Locks[source]
		if doc == nil || doc.Scope != lockfile.Scope(source.LockScope()) {
			continue
		}
		for _, declaration := range layer.Sources() {
			if declaration.Kind != config.V4SourceGit {
				continue
			}
			origin, err := gitidentity.CanonicalV1(declaration.URL)
			if err != nil {
				continue
			}
			for _, registry := range doc.Registries {
				if registry.URL != origin || registry.Ref != declaration.Ref {
					continue
				}
				candidates = append(candidates, deployedGitPlaceProfileCandidate{
					layer: source, url: declaration.URL, origin: origin, ref: declaration.Ref,
					locked: lockfile.GitRegistry{URL: registry.URL, Ref: registry.Ref, Commit: registry.Commit},
				})
			}
		}
	}
	return candidates
}

func validGitPlaceProfileRoot(root string) bool {
	if root == "" || root == "." || !fs.ValidPath(root) || strings.Contains(root, `\`) {
		return false
	}
	for _, component := range strings.Split(root, "/") {
		if strings.EqualFold(component, ".git") {
			return false
		}
	}
	return true
}
