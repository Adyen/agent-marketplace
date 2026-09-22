package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type missingMarketplaceLockLayer struct {
	source   config.Source
	lockPath string
	v4       *config.V4Config
}

func hasMarketplaceLockRequiredError(errs []error) bool {
	for _, err := range errs {
		var validation *config.V4ValidationError
		if errors.As(err, &validation) && validation.Code == "v4.marketplace-lock-required" {
			return true
		}
	}
	return false
}

func bootstrapMissingMarketplaceLocks(ctx context.Context, repoRoot, localDir string, selected map[lockfile.Scope]bool) (int, error) {
	layers, overlays, err := missingMarketplaceLockLayers(repoRoot, localDir, selected)
	if err != nil || len(layers) == 0 {
		return 0, err
	}
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir, FileOverlays: overlays})
	if len(ec.Errors) > 0 {
		return 0, errors.Join(ec.Errors...)
	}
	resolver := marketplace.NewResolver()
	resolver.AllowedRegistries = ec.Allowed
	desired := make([]dependencytxn.DesiredFile, 0, len(layers))
	for _, layer := range layers {
		doc, err := resolveMarketplaceLock(ctx, resolver, layer, ec.TargetHarnesses)
		if err != nil {
			return 0, fmt.Errorf("create Marketplace lock %q: %w", layer.lockPath, err)
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(layer.lockPath), 0o755); err != nil {
			return 0, fmt.Errorf("create Marketplace lock directory: %w", err)
		}
		desired = append(desired, dependencytxn.DesiredFile{Path: layer.lockPath, Expected: dependencytxn.Generation{}, Bytes: data, Mode: 0o644})
	}
	reads, err := effectiveReadAssertions(ec, nil)
	if err != nil {
		return 0, err
	}
	stateDir, err := dependencyStateDir()
	if err != nil {
		return 0, err
	}
	if _, err := commitDependencyTransaction(ctx, stateDir, desired, dependencytxn.WithReadAssertions(reads)); err != nil {
		return 0, fmt.Errorf("commit missing Marketplace locks: %w", err)
	}
	slog.Info("missing Marketplace locks created", "count", len(desired))
	return len(desired), nil
}

func missingMarketplaceLockLayers(repoRoot, localDir string, selected map[lockfile.Scope]bool) ([]missingMarketplaceLockLayer, map[string][]byte, error) {
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		return nil, nil, err
	}
	paths := []struct {
		source config.Source
		path   string
	}{
		{config.UserPersonal, userPath},
		{config.RepoCommitted, config.ProjectConfigPath(repoRoot)},
		{config.RepoPersonal, config.LocalConfigPath(localDir)},
	}
	var layers []missingMarketplaceLockLayer
	overlays := map[string][]byte{}
	for _, candidate := range paths {
		if (candidate.source == config.RepoCommitted && config.IsHomeDir(repoRoot)) || (candidate.source == config.RepoPersonal && config.IsHomeDir(localDir)) {
			continue
		}
		scope := lockfile.Scope(candidate.source.LockScope())
		if selected != nil && !selected[scope] {
			continue
		}
		v4, err := config.LoadV4ConfigIfPresent(candidate.path, candidate.source)
		if err != nil {
			return nil, nil, err
		}
		if v4 == nil || !hasV4MarketplaceSource(v4) {
			continue
		}
		canonicalConfig, err := filepath.EvalSymlinks(candidate.path)
		if err != nil {
			return nil, nil, err
		}
		lockPath, err := lockfile.PathForConfig(canonicalConfig)
		if err != nil {
			return nil, nil, err
		}
		lockPath, err = rejectGeneratedLockSymlink(lockPath)
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Lstat(lockPath); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		doc, err := provisionalMarketplaceLock(v4, scope)
		if err != nil {
			return nil, nil, err
		}
		data, err := lockfile.Encode(doc)
		if err != nil {
			return nil, nil, err
		}
		overlays[lockPath] = data
		layers = append(layers, missingMarketplaceLockLayer{source: candidate.source, lockPath: lockPath, v4: v4})
	}
	return layers, overlays, nil
}

func hasV4MarketplaceSource(v4 *config.V4Config) bool {
	for _, source := range v4.Sources {
		if source.Kind == config.V4SourceMarketplace {
			return true
		}
	}
	return false
}

func provisionalMarketplaceLock(v4 *config.V4Config, scope lockfile.Scope) (lockfile.Document, error) {
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: scope}
	for _, sourceName := range sortedV4MarketplaceSources(v4) {
		source := v4.Sources[sourceName]
		entry := lockfile.MarketplaceLock{URL: source.CanonicalURL, InstanceID: "bootstrap"}
		bindings := map[string]string{}
		for _, request := range v4.Requests {
			if request.SourceName != sourceName {
				continue
			}
			bindings[request.Registry] = request.Registry
			revision := request.Options.Revision
			if revision == "" {
				revision = digest
			}
			resource := lockfile.MarketplaceResourceLock{RegistryName: request.Registry, RegistryID: request.Registry, Kind: request.Kind, Name: request.Name, Revision: revision, ContentDigest: lockfile.Digest(revision), Acquisition: lockfile.MarketplaceAcquisitionMirror}
			if request.Kind == "collection" {
				resource.CollectionManifestDigest = lockfile.Digest(revision)
			} else {
				resource.ArchiveDigest = lockfile.Digest(digest)
			}
			entry.Resources = append(entry.Resources, resource)
		}
		for name, id := range bindings {
			entry.RegistryBindings = append(entry.RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: name, RegistryID: id})
		}
		doc.Marketplaces = append(doc.Marketplaces, entry)
	}
	return lockfile.Normalize(doc)
}

func sortedV4MarketplaceSources(v4 *config.V4Config) []string {
	names := make([]string, 0, len(v4.Sources))
	for name, source := range v4.Sources {
		if source.Kind == config.V4SourceMarketplace {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func resolveMarketplaceLock(ctx context.Context, resolver *marketplace.Resolver, layer missingMarketplaceLockLayer, harnesses []string) (lockfile.Document, error) {
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.Scope(layer.source.LockScope())}
	for _, sourceName := range sortedV4MarketplaceSources(layer.v4) {
		source := layer.v4.Sources[sourceName]
		entry := config.MarketplaceEntry{URL: source.CanonicalURL, Alias: sourceName, Source: layer.source}
		resources := map[string]lockfile.MarketplaceResourceLock{}
		bindings := map[string]string{}
		for _, request := range layer.v4.Requests {
			if request.SourceName != sourceName {
				continue
			}
			plan, err := resolver.PrepareLocator(ctx, entry, marketplace.MarketplaceLocator{URL: entry.URL, Registry: request.Registry, Kind: request.Kind, Name: request.Name, Revision: request.Options.Revision, Digest: request.Options.Revision}, marketplaceLockTargets(harnesses))
			if err != nil {
				return lockfile.Document{}, fmt.Errorf("resolve %s: %w", request.Locator, err)
			}
			entry = plan.Entry
			for _, pin := range marketplaceLockPins(plan) {
				record, err := resolver.Cache.VerifiedMarketplaceLockResource(entry, pin)
				if err != nil {
					return lockfile.Document{}, fmt.Errorf("verify %s/%s/%s lock evidence: %w", pin.RegistryID, pin.Kind, pin.Name, err)
				}
				if err := mergeMarketplaceLockResource(resources, record); err != nil {
					return lockfile.Document{}, err
				}
				if previous, exists := bindings[record.RegistryName]; exists && previous != record.RegistryID {
					return lockfile.Document{}, fmt.Errorf("registry binding %q resolves to both %q and %q", record.RegistryName, previous, record.RegistryID)
				}
				bindings[record.RegistryName] = record.RegistryID
			}
		}
		if entry.InstanceID == "" {
			client, err := marketplace.NewClient(entry.URL)
			if err != nil {
				return lockfile.Document{}, err
			}
			wellKnown, err := client.WellKnown(ctx)
			if err != nil {
				return lockfile.Document{}, fmt.Errorf("well-known document: %w", err)
			}
			entry.InstanceID = wellKnown.InstanceID
		}
		locked := lockfile.MarketplaceLock{URL: entry.URL, InstanceID: entry.InstanceID}
		for name, id := range bindings {
			locked.RegistryBindings = append(locked.RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: name, RegistryID: id})
		}
		for _, resource := range resources {
			locked.Resources = append(locked.Resources, resource)
		}
		doc.Marketplaces = append(doc.Marketplaces, locked)
	}
	return lockfile.Normalize(doc)
}

func marketplaceLockTargets(names []string) []hcc.Target {
	targets := make([]hcc.Target, len(names))
	for i, name := range names {
		targets[i] = hcc.Target(name)
	}
	return targets
}

func marketplaceLockPins(plan *marketplace.PreparedInstall) []config.MarketplaceResourcePin {
	pins := []config.MarketplaceResourcePin{plan.Pin}
	for _, member := range plan.CollectionMembers {
		pins = append(pins, member.Pin)
	}
	return pins
}

func mergeMarketplaceLockResource(resources map[string]lockfile.MarketplaceResourceLock, record lockfile.MarketplaceResourceLock) error {
	key := strings.Join([]string{record.RegistryID, record.Kind, record.Name}, "\x00")
	current, exists := resources[key]
	if !exists {
		resources[key] = record
		return nil
	}
	left, right := current, record
	left.SelectedBy, right.SelectedBy = nil, nil
	if !reflect.DeepEqual(left, right) {
		return fmt.Errorf("marketplace resource %s resolved to conflicting lock records", key)
	}
	seen := map[lockfile.MarketplaceSelectionLock]bool{}
	for _, selection := range current.SelectedBy {
		seen[selection] = true
	}
	for _, selection := range record.SelectedBy {
		if !seen[selection] {
			current.SelectedBy = append(current.SelectedBy, selection)
		}
	}
	resources[key] = current
	return nil
}
