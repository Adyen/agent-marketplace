package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

type marketplaceLockView func(*config.Config) (*config.Config, error)

func layerMarketplaces(layer config.LayerResult) []config.MarketplaceEntry {
	if layer.Config != nil {
		return layer.Config.Marketplaces
	}
	if layer.Local != nil {
		return layer.Local.Marketplaces
	}
	return nil
}

func marketplaceRequirements(entries []config.MarketplaceEntry) ([]lockfile.MarketplaceRequirements, error) {
	out := make([]lockfile.MarketplaceRequirements, 0, len(entries))
	for _, entry := range entries {
		endpoint := lockfile.MarketplaceRequirements{URL: entry.URL, InstanceID: entry.InstanceID}
		for _, pin := range entry.Resources {
			if pin.SelectedBy != "" && !pin.Direct {
				continue
			}
			revision := lockfile.Digest(pin.Revision)
			if pin.Digest != "" {
				revision = lockfile.Digest(pin.Digest)
			}
			endpoint.Resources = append(endpoint.Resources, lockfile.MarketplaceRequirement{
				SourceID: pin.SourceID, Kind: pin.Kind, Name: pin.Name, Revision: revision, Collection: pin.Kind == "collection",
			})
		}
		out = append(out, endpoint)
	}
	return out, nil
}

func reconcileMarketplaceDocument(ctx context.Context, scope lockfile.Scope, entries []config.MarketplaceEntry, current *lockfile.Document, operation lockfile.Operation, selection lockfile.UpgradeSelection, offline bool, resolverMP *marketplace.Resolver, stats *lockReconcileStats) (lockfile.Document, bool, error) {
	requirements, err := marketplaceRequirements(entries)
	if err != nil {
		return lockfile.Document{}, false, err
	}
	var marketplaceCurrent *lockfile.Document
	if current != nil {
		copy := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: scope, Marketplaces: current.Marketplaces}
		marketplaceCurrent = &copy
	}
	plan, err := lockfile.Plan(lockfile.Requirements{Scope: scope, Marketplaces: requirements, Upgrade: selection}, marketplaceCurrent, operation)
	if err != nil {
		return lockfile.Document{}, false, err
	}
	if stats != nil {
		for _, endpoint := range requirements {
			stats.retained += len(endpoint.Resources)
		}
		stats.acquired = len(plan.Acquisitions)
		stats.retained = max(0, stats.retained-stats.acquired)
	}
	if offline && len(plan.Acquisitions) > 0 {
		return lockfile.Document{}, false, &lockLifecycleError{Code: "lock.offline-resolution", Scope: scope, Err: errors.New("offline mode cannot resolve a stale Marketplace lock")}
	}
	for _, acquisition := range plan.Acquisitions {
		if acquisition.Marketplace == nil {
			continue
		}
		endpoint := acquisition.Marketplace.Endpoint
		requirement := acquisition.Marketplace.Requirement
		entry, ok := findMarketplaceEntry(entries, endpoint.URL, endpoint.InstanceID)
		if !ok {
			return lockfile.Document{}, false, fmt.Errorf("marketplace lock acquisition endpoint disappeared")
		}
		sourceLocator := requirement.SourceID
		for _, configured := range entry.Resources {
			if configured.SourceID == requirement.SourceID && configured.Kind == requirement.Kind && configured.Name == requirement.Name && configured.SourceSlug != "" {
				sourceLocator = configured.SourceSlug
				break
			}
		}
		locator := marketplace.MarketplaceLocator{Source: sourceLocator, Kind: requirement.Kind, Name: requirement.Name}
		if requirement.Revision != "" {
			locator.Revision = requirement.Revision.String()
			locator.Digest = requirement.Revision.String()
		}
		prepared, err := resolverMP.PrepareLocator(ctx, entry, locator, nil)
		if err != nil {
			return lockfile.Document{}, false, fmt.Errorf("resolve Marketplace lock %s/%s/%s: %w", requirement.SourceID, requirement.Kind, requirement.Name, err)
		}
		locked, err := marketplaceRecordsFromPrepared(resolverMP.Cache, prepared)
		if err != nil {
			return lockfile.Document{}, false, err
		}
		if err := mergeMarketplaceEndpoint(&plan.Desired, locked); err != nil {
			return lockfile.Document{}, false, err
		}
	}
	if err := lockfile.ReconcileMarketplaceGraph(&plan.Desired, requirements); err != nil {
		return lockfile.Document{}, false, err
	}
	normalized, err := lockfile.Normalize(plan.Desired)
	if err != nil {
		return lockfile.Document{}, false, err
	}
	fresh, err := marketplaceLockFresh(scope, current, normalized)
	if err != nil {
		return lockfile.Document{}, false, err
	}
	return normalized, fresh, nil
}

func marketplaceLockFresh(scope lockfile.Scope, current *lockfile.Document, desired lockfile.Document) (bool, error) {
	if current == nil {
		return false, nil
	}
	currentMarketplace, err := lockfile.Normalize(lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: scope, Marketplaces: current.Marketplaces})
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(desired.Marketplaces, currentMarketplace.Marketplaces), nil
}

func findMarketplaceEntry(entries []config.MarketplaceEntry, rawURL, instanceID string) (config.MarketplaceEntry, bool) {
	want, err := urlutil.NormalizeEndpointURL(rawURL)
	if err != nil {
		return config.MarketplaceEntry{}, false
	}
	for _, entry := range entries {
		got, err := urlutil.NormalizeEndpointURL(entry.URL)
		if err == nil && got == want && entry.InstanceID == instanceID {
			return entry, true
		}
	}
	return config.MarketplaceEntry{}, false
}

func marketplaceRecordsFromPrepared(cache *marketplace.Cache, prepared *marketplace.PreparedInstall) (lockfile.Marketplace, error) {
	if prepared == nil {
		return lockfile.Marketplace{}, errors.New("nil prepared Marketplace lock")
	}
	endpoint := lockfile.Marketplace{URL: prepared.Entry.URL, InstanceID: prepared.Entry.InstanceID}
	if prepared.Pin.Kind != "collection" {
		record, err := marketplaceResourceRecord(prepared, true, nil)
		if err != nil {
			return endpoint, err
		}
		endpoint.Resources = append(endpoint.Resources, record)
		return endpoint, nil
	}
	rootEdge := lockfile.CollectionEdge{SourceID: prepared.Pin.SourceID, Name: prepared.Pin.Name, Revision: lockfile.Digest(prepared.Pin.Digest)}
	if cache == nil {
		cache = marketplace.DefaultCache()
	}
	seenCollections := map[string]bool{}
	var visit func(string, string, bool) error
	visit = func(sourceID, digest string, direct bool) error {
		key := sourceID + "\x00" + digest
		if seenCollections[key] {
			return nil
		}
		seenCollections[key] = true
		raw, verified, err := cache.ReadCollectionManifest(digest)
		if err != nil || raw == nil || !verified {
			return fmt.Errorf("read verified collection manifest %s: %w", digest, err)
		}
		manifest, payload, err := hccartifact.DecodeCollectionManifest(strings.NewReader(string(raw)))
		if err != nil {
			return fmt.Errorf("decode collection manifest %s: %w", digest, err)
		}
		if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
			return fmt.Errorf("verify collection manifest %s: %w", digest, err)
		}
		collection := lockfile.MarketplaceCollection{SourceID: sourceID, Name: manifest.Name, Revision: lockfile.Digest(digest), Direct: direct}
		for _, member := range manifest.Members {
			collection.Members = append(collection.Members, lockfile.CollectionEdge{SourceID: member.SourceID, Kind: member.Kind, Name: member.Name, Revision: lockfile.Digest(member.Revision)})
		}
		endpoint.Collections = append(endpoint.Collections, collection)
		for _, member := range manifest.Members {
			if member.Kind == "collection" {
				if err := visit(member.SourceID, member.Revision, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(prepared.Pin.SourceID, prepared.Pin.Digest, true); err != nil {
		return endpoint, err
	}
	for _, member := range prepared.CollectionMembers {
		record, err := marketplaceResourceRecord(member, false, []lockfile.CollectionEdge{rootEdge})
		if err != nil {
			return endpoint, err
		}
		endpoint.Resources = append(endpoint.Resources, record)
	}
	return endpoint, nil
}

func marketplaceResourceRecord(prepared *marketplace.PreparedInstall, direct bool, selectedBy []lockfile.CollectionEdge) (lockfile.MarketplaceResource, error) {
	record := lockfile.MarketplaceResource{
		SourceID: prepared.Pin.SourceID, Kind: prepared.Pin.Kind, Name: prepared.Pin.Name,
		Revision: lockfile.Digest(prepared.Pin.Digest), Direct: direct, SelectedBy: selectedBy,
	}
	switch prepared.Pin.Acquisition {
	case "", config.MarketplaceAcquisitionMirror:
		record.Acquisition = lockfile.AcquisitionMirror
		archive := prepared.ArchiveDigest
		if archive == "" {
			archive = string(prepared.Revision.ArchiveDigest)
		}
		if archive == "" {
			return record, errors.New("verified mirror metadata has no archive digest")
		}
		record.ArchiveDigest = lockfile.Digest(archive)
	case config.MarketplaceAcquisitionIndexOnly:
		record.Acquisition = lockfile.AcquisitionIndexOnly
		commit := prepared.Pin.SourceCommit
		if !strings.Contains(commit, ":") {
			commit = "sha1:" + commit
		}
		oid, err := lockfile.ParseGitOID(commit)
		if err != nil {
			return record, fmt.Errorf("convert index-only source commit: %w", err)
		}
		record.SourceCommit = oid
		record.SourceManifestDigest = lockfile.Digest(prepared.Pin.SourceManifestDigest)
	default:
		return record, fmt.Errorf("unsupported Marketplace acquisition %q", prepared.Pin.Acquisition)
	}
	return record, nil
}

func mergeMarketplaceEndpoint(doc *lockfile.Document, endpoint lockfile.Marketplace) error {
	canonicalURL, err := urlutil.NormalizeEndpointURL(endpoint.URL)
	if err != nil {
		return fmt.Errorf("normalize acquired Marketplace endpoint: %w", err)
	}
	endpoint.URL = canonicalURL
	for i := range doc.Marketplaces {
		if doc.Marketplaces[i].URL != endpoint.URL || doc.Marketplaces[i].InstanceID != endpoint.InstanceID {
			continue
		}
		target := &doc.Marketplaces[i]
		for _, incoming := range endpoint.Collections {
			merged := false
			for j := range target.Collections {
				existing := &target.Collections[j]
				if existing.SourceID != incoming.SourceID || existing.Name != incoming.Name {
					continue
				}
				if existing.Revision != incoming.Revision || !sameCollectionMembers(existing.Members, incoming.Members) {
					return fmt.Errorf("conflicting Marketplace collection records for %s/%s", incoming.SourceID, incoming.Name)
				}
				existing.Direct = existing.Direct || incoming.Direct
				merged = true
				break
			}
			if !merged {
				target.Collections = append(target.Collections, incoming)
			}
		}
		for _, incoming := range endpoint.Resources {
			merged := false
			for j := range target.Resources {
				existing := &target.Resources[j]
				if existing.SourceID != incoming.SourceID || existing.Kind != incoming.Kind || existing.Name != incoming.Name {
					continue
				}
				if existing.Revision != incoming.Revision || existing.Acquisition != incoming.Acquisition || existing.ArchiveDigest != incoming.ArchiveDigest || existing.SourceCommit.String() != incoming.SourceCommit.String() || existing.SourceManifestDigest != incoming.SourceManifestDigest {
					return fmt.Errorf("conflicting Marketplace lock records for %s/%s/%s", incoming.SourceID, incoming.Kind, incoming.Name)
				}
				existing.Direct = existing.Direct || incoming.Direct
				existing.SelectedBy = append(existing.SelectedBy, incoming.SelectedBy...)
				merged = true
				break
			}
			if !merged {
				target.Resources = append(target.Resources, incoming)
			}
		}
		return nil
	}
	doc.Marketplaces = append(doc.Marketplaces, endpoint)
	return nil
}

func sameCollectionMembers(left, right []lockfile.CollectionEdge) bool {
	left = append([]lockfile.CollectionEdge(nil), left...)
	right = append([]lockfile.CollectionEdge(nil), right...)
	less := func(edges []lockfile.CollectionEdge) {
		sort.Slice(edges, func(i, j int) bool {
			a, b := edges[i], edges[j]
			if a.SourceID != b.SourceID {
				return a.SourceID < b.SourceID
			}
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.Revision < b.Revision
		})
	}
	less(left)
	less(right)
	return reflect.DeepEqual(left, right)
}

func exactMarketplaceView(documents map[config.Source]lockfile.Document) marketplaceLockView {
	return func(input *config.Config) (*config.Config, error) {
		if input == nil {
			return nil, nil
		}
		out := *input
		out.Marketplaces = make([]config.MarketplaceEntry, 0, len(input.Marketplaces))
		for _, entry := range input.Marketplaces {
			doc, ok := documents[entry.Source]
			if !ok {
				return nil, fmt.Errorf("no lock for Marketplace owned by %s", entry.Source)
			}
			locked, ok := findLockedMarketplace(doc, entry)
			if !ok {
				return nil, fmt.Errorf("no Marketplace lock for %s (%s)", entry.URL, entry.InstanceID)
			}
			exact := entry
			exact.Resources = nil
			installedNames := map[string]string{}
			for _, requirement := range entry.Resources {
				installedNames[requirement.SourceID+"\x00"+requirement.Kind+"\x00"+requirement.Name] = requirement.InstalledName
			}
			for _, collection := range locked.Collections {
				if !collection.Direct {
					continue
				}
				exact.Resources = append(exact.Resources, config.MarketplaceResourcePin{SourceID: collection.SourceID, Kind: "collection", Name: collection.Name, Revision: collection.Revision.String(), Digest: collection.Revision.String()})
			}
			for _, resource := range locked.Resources {
				pin := config.MarketplaceResourcePin{SourceID: resource.SourceID, Kind: resource.Kind, Name: resource.Name, Revision: resource.Revision.String(), Digest: resource.Revision.String(), Acquisition: string(resource.Acquisition), ArchiveDigest: resource.ArchiveDigest.String(), Direct: resource.Direct}
				pin.InstalledName = installedNames[resource.SourceID+"\x00"+resource.Kind+"\x00"+resource.Name]
				if resource.Acquisition == lockfile.AcquisitionIndexOnly {
					pin.SourceCommit = resource.SourceCommit.Hex()
					pin.SourceManifestDigest = resource.SourceManifestDigest.String()
				}
				selectors := make([]config.MarketplaceSelection, len(resource.SelectedBy))
				for i, edge := range resource.SelectedBy {
					selectors[i] = config.MarketplaceSelection{SourceID: edge.SourceID, Name: edge.Name, Revision: edge.Revision.String()}
				}
				pin.SelectedBy = config.MarketplaceSelectionsFrom(selectors)
				exact.Resources = append(exact.Resources, pin)
			}
			out.Marketplaces = append(out.Marketplaces, exact)
		}
		return &out, nil
	}
}

func findLockedMarketplace(doc lockfile.Document, entry config.MarketplaceEntry) (lockfile.Marketplace, bool) {
	want, err := urlutil.NormalizeEndpointURL(entry.URL)
	if err != nil {
		return lockfile.Marketplace{}, false
	}
	for _, locked := range doc.Marketplaces {
		got, err := urlutil.NormalizeEndpointURL(locked.URL)
		if err == nil && got == want && locked.InstanceID == entry.InstanceID {
			return locked, true
		}
	}
	return lockfile.Marketplace{}, false
}
