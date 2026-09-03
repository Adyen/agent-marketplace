package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

type LockMetadata struct {
	Source            config.Source
	Scope             lockfile.Scope
	Path              string
	URL               string
	Ref               string
	Commit            string
	SnapshotAvailable bool
}

type MarketplaceLockMetadata struct {
	Source     config.Source
	Scope      lockfile.Scope
	Path       string
	URL        string
	InstanceID string
	Resources  int
	Blocked    bool
	Code       string
}

type LockViewError struct {
	Code  string
	Scope lockfile.Scope
	Path  string
	Err   error
}

func (e *LockViewError) Error() string {
	return fmt.Sprintf("%s: %s lock %q: %v", e.Code, e.Scope, e.Path, e.Err)
}
func (e *LockViewError) Unwrap() error { return e.Err }

type readOnlyLockedGit struct {
	snapshot string
	commit   string
	err      error
}

type readOnlyExactView struct {
	catalogOption       catalog.LoadOption
	exactSnapshot       catalog.ExactSnapshotFunc
	marketplaceView     func(*config.Config) *config.Config
	gitMetadata         []LockMetadata
	marketplaceMetadata []MarketplaceLockMetadata
	errors              []error
}

func exactLockView(ec config.EffectiveConfig) readOnlyExactView {
	return exactLockViewWithContext(context.Background(), ec)
}

func exactLockViewWithContext(ctx context.Context, ec config.EffectiveConfig) readOnlyExactView {
	bySource := make(map[string]readOnlyLockedGit)
	byIdentity := make(map[string][]readOnlyLockedGit)
	documents := make(map[config.Source]lockfile.Document)
	blockedSources := make(map[config.Source]bool)
	unlockedSources := make(map[config.Source]bool)
	var gitMetadata []LockMetadata
	var marketplaceMetadata []MarketplaceLockMetadata
	var viewErrors []error

	for _, layer := range ec.PresentLayers() {
		registries := readOnlyRemoteEntries(layer)
		marketplaces := readOnlyMarketplaceEntries(layer)
		if len(registries) == 0 && len(marketplaces) == 0 {
			continue
		}
		scope := readOnlyLockScope(layer.Source)
		lockPath, err := lockfile.PathForConfig(layer.Path)
		if err != nil {
			name := "lock.yaml"
			if scope == lockfile.ScopeLocal {
				name = "lock.local.yaml"
			}
			lockPath = filepath.Join(filepath.Dir(layer.Path), name)
		}

		doc, layerErr := readOnlyLayerLock(layer.Path, lockPath, scope, registries, marketplaces)
		if layerErr != nil {
			blockedSources[layer.Source] = true
			viewErrors = append(viewErrors, layerErr)
		} else if doc.APIVersion == "" && scope == lockfile.ScopeOrganization {
			unlockedSources[layer.Source] = true
		} else {
			documents[layer.Source] = doc
		}
		for _, entry := range marketplaces {
			metadata := MarketplaceLockMetadata{Source: layer.Source, Scope: scope, Path: lockPath, URL: entry.URL, InstanceID: entry.InstanceID, Resources: len(entry.Resources), Blocked: layerErr != nil}
			if viewErr := new(LockViewError); errors.As(layerErr, &viewErr) {
				metadata.Code = viewErr.Code
			}
			marketplaceMetadata = append(marketplaceMetadata, metadata)
		}
		if layerErr != nil {
			for _, entry := range registries {
				canonical, canonicalErr := readOnlyCanonicalURL(entry.URL)
				if canonicalErr != nil {
					canonical = entry.URL
				}
				locked := readOnlyLockedGit{err: layerErr}
				bySource[readOnlySourceKey(layer.Source, canonical, entry.Ref)] = locked
				identity := readOnlyGitIdentity(canonical, entry.Ref)
				byIdentity[identity] = append(byIdentity[identity], locked)
			}
			continue
		}

		for _, registry := range doc.Registries {
			oid, parseErr := cache.ParseGitOID(registry.Commit.String())
			snapshot := ""
			openErr := parseErr
			if openErr == nil {
				openErr = cache.WithExactGitReadOnly(ctx, registry.URL, oid, func(path string) error {
					snapshot = path
					for _, root := range registry.Roots {
						if _, verifyErr := sourcerevision.VerifyLockedGitRoot(path, root); verifyErr != nil {
							return &LockViewError{Code: "lock.manifest-mismatch", Scope: scope, Path: lockPath, Err: fmt.Errorf("verify %s@%s root %q: %w", registry.URL, registry.Commit.String(), root.Path, verifyErr)}
						}
					}
					return nil
				})
				var manifestErr *LockViewError
				if errors.As(openErr, &manifestErr) && manifestErr.Code == "lock.manifest-mismatch" {
					viewErrors = append(viewErrors, openErr)
				}
			}
			gitMetadata = append(gitMetadata, LockMetadata{
				Source: layer.Source, Scope: scope, Path: lockPath, URL: registry.URL,
				Ref: registry.Ref, Commit: registry.Commit.String(), SnapshotAvailable: openErr == nil,
			})
			locked := readOnlyLockedGit{snapshot: snapshot, commit: registry.Commit.String()}
			if openErr != nil {
				locked.err = &LockViewError{Code: "lock.snapshot-unavailable", Scope: scope, Path: lockPath, Err: openErr}
			}
			identity := readOnlyGitIdentity(registry.URL, registry.Ref)
			bySource[readOnlySourceKey(layer.Source, registry.URL, registry.Ref)] = locked
			byIdentity[identity] = append(byIdentity[identity], locked)
		}
	}

	sort.Slice(gitMetadata, func(i, j int) bool {
		if gitMetadata[i].Source != gitMetadata[j].Source {
			return gitMetadata[i].Source < gitMetadata[j].Source
		}
		if gitMetadata[i].URL != gitMetadata[j].URL {
			return gitMetadata[i].URL < gitMetadata[j].URL
		}
		return gitMetadata[i].Ref < gitMetadata[j].Ref
	})
	sort.Slice(marketplaceMetadata, func(i, j int) bool {
		if marketplaceMetadata[i].Source != marketplaceMetadata[j].Source {
			return marketplaceMetadata[i].Source < marketplaceMetadata[j].Source
		}
		if marketplaceMetadata[i].URL != marketplaceMetadata[j].URL {
			return marketplaceMetadata[i].URL < marketplaceMetadata[j].URL
		}
		return marketplaceMetadata[i].InstanceID < marketplaceMetadata[j].InstanceID
	})

	exactSnapshot := func(entry config.RegistryEntry) (string, string, error) {
		canonical, err := readOnlyCanonicalURL(entry.URL)
		if err != nil {
			return "", "", err
		}
		if unlockedSources[entry.Source] {
			return "", "", nil
		}
		locked, ok := bySource[readOnlySourceKey(entry.Source, canonical, entry.Ref)]
		if !ok {
			candidates := byIdentity[readOnlyGitIdentity(canonical, entry.Ref)]
			if len(candidates) != 1 {
				return "", "", &LockViewError{Code: "lock.lookup", Scope: readOnlyLockScope(entry.Source), Err: fmt.Errorf("no unambiguous exact lock for %s@%s", canonical, entry.Ref)}
			}
			locked = candidates[0]
		}
		if locked.err != nil {
			return "", "", locked.err
		}
		return locked.snapshot, canonical + "@" + locked.commit + ":" + entry.Path, nil
	}
	option := catalog.WithExactSnapshots(exactSnapshot)
	return readOnlyExactView{
		catalogOption: option,
		exactSnapshot: exactSnapshot,
		marketplaceView: func(input *config.Config) *config.Config {
			return exactMarketplaceConfig(input, documents, blockedSources, unlockedSources)
		},
		gitMetadata: gitMetadata, marketplaceMetadata: marketplaceMetadata, errors: viewErrors,
	}
}

func readOnlyLayerLock(configPath, path string, scope lockfile.Scope, registries []config.RegistryEntry, marketplaces []config.MarketplaceEntry) (lockfile.Document, error) {
	if scope == lockfile.ScopeOrganization {
		if err := config.ValidateOrganizationTrustUnit(configPath, true, true); err != nil {
			return lockfile.Document{}, &LockViewError{Code: "lock.trust", Scope: scope, Path: path, Err: err}
		}
	}
	data, err := lockfile.ReadBytes(path)
	if err != nil {
		if scope == lockfile.ScopeOrganization && errors.Is(err, os.ErrNotExist) {
			return lockfile.Document{}, nil
		}
		code := "lock.read"
		if errors.Is(err, os.ErrNotExist) {
			code = "lock.missing"
		}
		return lockfile.Document{}, &LockViewError{Code: code, Scope: scope, Path: path, Err: err}
	}
	doc, err := lockfile.Decode(data)
	if err == nil {
		err = lockfile.ValidateScope(doc, scope)
	}
	if err != nil {
		return lockfile.Document{}, &LockViewError{Code: "lock.invalid", Scope: scope, Path: path, Err: err}
	}
	if !readOnlyDocumentMatches(doc, registries, marketplaces) {
		return doc, &LockViewError{Code: "lock.stale", Scope: scope, Path: path, Err: errors.New("lock does not match configuration")}
	}
	return doc, nil
}

func readOnlyDocumentMatches(doc lockfile.Document, registries []config.RegistryEntry, marketplaces []config.MarketplaceEntry) bool {
	if !readOnlyGitDocumentMatches(doc, registries) {
		return false
	}
	requirements := lockfile.Requirements{Scope: doc.Scope}
	for _, entry := range marketplaces {
		endpoint := lockfile.MarketplaceRequirements{URL: entry.URL, InstanceID: entry.InstanceID}
		for _, pin := range entry.Resources {
			if pin.SelectedBy != "" && !pin.Direct {
				continue
			}
			revision := pin.Revision
			if pin.Digest != "" {
				revision = pin.Digest
			}
			endpoint.Resources = append(endpoint.Resources, lockfile.MarketplaceRequirement{SourceID: pin.SourceID, Kind: pin.Kind, Name: pin.Name, Revision: lockfile.Digest(revision), Collection: pin.Kind == "collection"})
		}
		requirements.Marketplaces = append(requirements.Marketplaces, endpoint)
	}
	current := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: doc.Scope, Marketplaces: doc.Marketplaces}
	plan, err := lockfile.Plan(requirements, &current, lockfile.OperationSync)
	return err == nil && !plan.Changed()
}

func readOnlyGitDocumentMatches(doc lockfile.Document, entries []config.RegistryEntry) bool {
	wanted := make(map[string]map[string]bool)
	for _, entry := range entries {
		canonical, err := readOnlyCanonicalURL(entry.URL)
		if err != nil {
			return false
		}
		identity := readOnlyGitIdentity(canonical, entry.Ref)
		if wanted[identity] == nil {
			wanted[identity] = make(map[string]bool)
		}
		root := entry.Path
		if root == "" {
			root = "."
		}
		wanted[identity][root+"\x00"+entry.SourceHarness] = true
	}
	if len(doc.Registries) != len(wanted) {
		return false
	}
	for _, registry := range doc.Registries {
		roots, ok := wanted[readOnlyGitIdentity(registry.URL, registry.Ref)]
		if !ok || len(registry.Roots) != len(roots) {
			return false
		}
		for _, root := range registry.Roots {
			if !roots[root.Path+"\x00"+root.SourceHarness] && !roots[root.Path+"\x00"] {
				return false
			}
		}
	}
	return true
}

func exactMarketplaceConfig(input *config.Config, documents map[config.Source]lockfile.Document, blocked, unlocked map[config.Source]bool) *config.Config {
	if input == nil {
		return nil
	}
	out := *input
	out.Marketplaces = make([]config.MarketplaceEntry, 0, len(input.Marketplaces))
	for _, entry := range input.Marketplaces {
		if len(entry.Resources) == 0 {
			// A registered endpoint without requirements has no exact resource
			// state to project from the adjacent lock.
			out.Marketplaces = append(out.Marketplaces, entry)
			continue
		}
		if blocked[entry.Source] {
			continue
		}
		if unlocked[entry.Source] {
			out.Marketplaces = append(out.Marketplaces, entry)
			continue
		}
		locked, ok := readOnlyLockedMarketplace(documents[entry.Source], entry)
		if !ok {
			continue
		}
		names := make(map[string]string, len(entry.Resources))
		for _, requirement := range entry.Resources {
			names[marketplaceCoordinate(requirement.SourceID, requirement.Kind, requirement.Name)] = requirement.InstalledName
		}
		exact := entry
		exact.Resources = nil
		for _, collection := range locked.Collections {
			if collection.Direct {
				exact.Resources = append(exact.Resources, config.MarketplaceResourcePin{SourceID: collection.SourceID, Kind: "collection", Name: collection.Name, Revision: collection.Revision.String(), Digest: collection.Revision.String(), InstalledName: names[marketplaceCoordinate(collection.SourceID, "collection", collection.Name)], Direct: true})
			}
		}
		for _, resource := range locked.Resources {
			pin := config.MarketplaceResourcePin{SourceID: resource.SourceID, Kind: resource.Kind, Name: resource.Name, Revision: resource.Revision.String(), Digest: resource.Revision.String(), Acquisition: string(resource.Acquisition), ArchiveDigest: resource.ArchiveDigest.String(), InstalledName: names[marketplaceCoordinate(resource.SourceID, resource.Kind, resource.Name)], Direct: resource.Direct}
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
	return &out
}

func readOnlyLockedMarketplace(doc lockfile.Document, entry config.MarketplaceEntry) (lockfile.Marketplace, bool) {
	want, err := config.NormalizeMarketplaceURL(entry.URL)
	if err != nil {
		return lockfile.Marketplace{}, false
	}
	for _, locked := range doc.Marketplaces {
		got, err := config.NormalizeMarketplaceURL(locked.URL)
		if err == nil && got == want && locked.InstanceID == entry.InstanceID {
			return locked, true
		}
	}
	return lockfile.Marketplace{}, false
}

func marketplaceCoordinate(sourceID, kind, name string) string {
	return sourceID + "\x00" + kind + "\x00" + name
}

func readOnlyRemoteEntries(layer config.LayerResult) []config.RegistryEntry {
	var entries []config.RegistryEntry
	if layer.Config != nil {
		entries = layer.Config.Registries
	} else if layer.Local != nil {
		entries = layer.Local.Registries
	}
	remote := make([]config.RegistryEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsLocal() {
			remote = append(remote, entry)
		}
	}
	return remote
}

func readOnlyMarketplaceEntries(layer config.LayerResult) []config.MarketplaceEntry {
	if layer.Config != nil {
		return layer.Config.Marketplaces
	}
	if layer.Local != nil {
		return layer.Local.Marketplaces
	}
	return nil
}

func readOnlyLockScope(source config.Source) lockfile.Scope {
	switch source {
	case config.Organization:
		return lockfile.ScopeOrganization
	case config.UserPersonal:
		return lockfile.ScopeUser
	case config.RepoCommitted:
		return lockfile.ScopeRepo
	default:
		return lockfile.ScopeLocal
	}
}

func readOnlyCanonicalURL(raw string) (string, error) {
	if filepath.IsAbs(raw) {
		raw = (&url.URL{Scheme: "file", Path: filepath.ToSlash(raw)}).String()
	}
	return lockfile.CanonicalGitURL(strings.TrimSpace(raw))
}
func readOnlySourceKey(source config.Source, canonical, ref string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", source, canonical, ref)
}
func readOnlyGitIdentity(canonical, ref string) string { return canonical + "\x00" + ref }
