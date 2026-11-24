package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
)

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
