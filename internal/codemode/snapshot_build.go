package codemode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type MarketplaceOrigin struct {
	Entry      config.MarketplaceEntry
	RegistryID string
	Kind       string
	Name       string
	Revision   string
}

func (r *Resource) MarketplaceOrigin() *MarketplaceOrigin {
	if r == nil || r.origin == nil {
		return nil
	}
	return r.origin.Marketplace
}

type SnapshotBuilder struct {
	CatalogMode catalog.LoadMode
	RepoRoot    string

	Discovery func(ec config.EffectiveConfig) ([]marketplace.DiscoveryOutcome, error)

	Cache *marketplace.Cache
}

func (b *SnapshotBuilder) BuildSnapshot(ctx context.Context, ec config.EffectiveConfig) (*Snapshot, []error) {
	snap := &Snapshot{AsOf: time.Now()}
	var errs []error
	mode := b.CatalogMode

	cfg := ec.Merged
	if cfg == nil {
		return &Snapshot{Stale: true, AsOf: time.Now()}, nil
	}

	seenRefs := map[string]bool{}
	duplicateRefs := map[string]bool{}
	if len(cfg.Registries) > 0 {
		hasRemote := false
		for i := range cfg.Registries {
			if !cfg.Registries[i].IsLocal() {
				hasRemote = true
				break
			}
		}
		var leaseErr error
		if hasRemote {
			if release, lerr := cache.AcquireExactGitNamespaceLeaseWithContext(ctx); lerr != nil {
				leaseErr = fmt.Errorf("exact git namespace lease unavailable: %w", lerr)
				errs = append(errs, fmt.Errorf("acquire exact git lease: %w", lerr))
				snap.Stale = true
			} else {
				defer release()
				ctx = cache.ContextWithExactGitNamespaceLease(ctx)
			}
		}
		type exactRegistry struct {
			revision string
			root     string
		}
		exact := map[string]exactRegistry{}
		exactFailed := false
		pins := loadGitPins()
		pinDelta := map[string]string{}
		exactSnapshot := catalog.ExactSnapshotFunc(func(entry config.RegistryEntry) (string, string, error) {
			if leaseErr != nil {
				exactFailed = true
				return "", "", leaseErr
			}
			canonical, err := lockfile.CanonicalGitConfigURL(entry.URL)
			if err != nil {
				exactFailed = true
				return "", "", err
			}
			var oid cache.GitOID
			var path string
			if mode == catalog.LoadModeRefresh {
				oid, err = cache.ResolveExactGitRefWithContext(ctx, canonical, entry.URL, entry.Ref)
				if err == nil {
					pinDelta[pinKey(canonical, entry.Ref)] = oid.String()
					path, err = cache.AcquireExactGitWithContext(ctx, canonical, entry.URL, oid)
				}
			} else {
				var res exactResolution
				res, err = cachedExactGit(ctx, entry, canonical, pins)
				oid, path = res.oid, res.path
			}
			if err != nil {
				exactFailed = true
				return "", "", err
			}
			exact[cache.CatalogKeyForEntry(entry.URL, entry.Ref, entry.Path)] = exactRegistry{revision: oid.Hex(), root: path}
			return path, "", nil
		})
		result := catalog.LoadRegistriesWithContext(ctx, cfg, b.RepoRoot, mode, catalog.WithExactSnapshots(exactSnapshot))
		errs = append(errs, result.Errors...)
		if exactFailed || (hasRemote && mode == catalog.LoadModeCached) {
			snap.Stale = true
		}
		if len(pinDelta) > 0 {
			if err := mergeGitPins(ctx, pinDelta); err != nil {
				errs = append(errs, err)
			}
		}
		labels := registryLabels(cfg.Registries, b.RepoRoot)
		for i := range cfg.Registries {
			entry := cfg.Registries[i]
			if labels[i] == "" {
				errs = append(errs, fmt.Errorf("registry %q produced an ambiguous catalog label; omitting its resources", entry.URL))
				snap.Stale = true
				continue
			}
			key, kerr := cache.RegistryKey(entry, b.RepoRoot)
			if kerr != nil {
				continue
			}
			loaded, ok := result.Catalogs[key]
			if !ok {
				continue
			}
			revision := entry.Ref
			contentBase := cache.BaseCachePath(key)
			exactRoot := ""
			if info, ok := exact[key]; ok {
				revision = info.revision
				contentBase = entry.BundlePath(info.root)
				exactRoot = info.root
			}
			registryLabel := labels[i]
			for i := range loaded.Entries {
				e := loaded.Entries[i]
				res := Resource{
					Kind:        normalizeKind(e.Kind.String()),
					Name:        e.Name,
					Description: e.Description,
					Tags:        e.Tags,
					Registry:    registryLabel,
					Revision:    revision,
					Assessment:  "allow",
				}
				res.Ref = registryLabel + "/" + res.Kind + "/" + res.Name
				if seenRefs[res.Ref] {
					continue
				}
				seenRefs[res.Ref] = true
				res.origin = registryOrigin(contentBase, e, entry, exactRoot)
				if res.Kind == "mcp" {
					res.Tools = "unknown"
				}
				snap.Resources = append(snap.Resources, res)
			}
		}
	}

	if len(cfg.Marketplaces) > 0 {
		mc := b.Cache
		if mc == nil {
			mc = marketplace.DefaultCache()
		}
		discover := b.Discovery
		if discover == nil {
			discover = func(ec config.EffectiveConfig) ([]marketplace.DiscoveryOutcome, error) {
				outcomes, _, err := marketplace.CachedDiscovery(ec, "", marketplace.DiscoveryOptions{
					Limit: marketplace.DiscoveryPageLimit * 20,
					Cache: marketplace.DefaultDiscoveryCache(),
				})
				return outcomes, err
			}
		}
		outcomes, derr := discover(ec)
		if derr != nil {
			snap.Stale = true
			errs = append(errs, derr)
		}
		for _, oc := range outcomes {
			if oc.Err != nil || oc.CacheState == marketplace.DiscoveryCacheMiss {
				snap.Stale = true
				continue
			}
			if oc.CacheState == marketplace.DiscoveryCacheStale {
				snap.Stale = true
			}
			alias := oc.Endpoint.Alias
			if alias == "" {
				alias = oc.Endpoint.InstanceID
			}
			for _, item := range oc.Items {
				if excludedAvailability(item.Availability) {
					continue
				}
				slug := item.RegistrySlug
				if slug == "" {
					slug = item.RegistryID
				}
				kind := normalizeKind(item.Kind)
				res := Resource{
					Ref:         alias + ":" + slug + "/" + kind + "/" + item.Name,
					Kind:        kind,
					Name:        item.Name,
					Description: item.Description,
					Registry:    slug,
					Revision:    item.Revision,
					Assessment:  "allow",
				}
				res.origin = &Origin{Marketplace: &MarketplaceOrigin{
					Entry:      oc.Endpoint,
					RegistryID: item.RegistryID,
					Kind:       item.Kind,
					Name:       item.Name,
					Revision:   item.Revision,
				}}

				if !b.applyCachedGate(mc, oc.Endpoint.URL, item, &res) {
					continue
				}
				if item.Kind == "mcp" {
					res.Tools = "unknown"
				}
				if seenRefs[res.Ref] {
					if !duplicateRefs[res.Ref] {
						duplicateRefs[res.Ref] = true
						errs = append(errs, fmt.Errorf("ref %q is produced by more than one source; keeping the first source and omitting the rest", res.Ref))
						snap.Stale = true
					}
					continue
				}
				seenRefs[res.Ref] = true
				snap.Resources = append(snap.Resources, res)
			}
		}
	}
	return snap, errs
}

func (b *SnapshotBuilder) applyCachedGate(mc *marketplace.Cache, endpointURL string, item mpapi.ResourceListItem, res *Resource) bool {
	if mc == nil {
		return true
	}
	rev, ok, err := mc.ReadResourceRevision(endpointURL, item.RegistryID, item.Kind, item.Name, item.Revision)
	if err != nil || !ok {
		return true
	}
	if rev.Revocation != nil {
		return false
	}
	switch rev.Assessment.Decision {
	case store.AssessmentDecisionReject:
		return false
	case store.AssessmentDecisionWarn:
		res.Assessment = "warn"
	}
	for _, f := range rev.Assessment.Findings {
		res.Findings = append(res.Findings, Finding{Rule: f.Code, Severity: f.Severity, Message: f.Message})
	}
	return true
}

type exactResolution struct {
	path string
	oid  cache.GitOID
}

func cachedExactGit(ctx context.Context, entry config.RegistryEntry, canonical string, pins map[string]string) (exactResolution, error) {
	if oid, err := pinnedOID(pins, canonical, entry.Ref); err == nil {
		if path, oerr := cache.OpenExactGitReadOnlyWithContext(ctx, canonical, oid); oerr == nil {
			return exactResolution{path: path, oid: oid}, nil
		}
		return acquireExactGitFromClone(ctx, entry, canonical, oid)
	}
	return acquireExactGitFromClone(ctx, entry, canonical, cache.GitOID{})
}

func acquireExactGitFromClone(ctx context.Context, entry config.RegistryEntry, canonical string, oid cache.GitOID) (exactResolution, error) {
	return cache.WithRegistryReadLockWithContext(ctx, []string{entry.URL}, func() (exactResolution, error) {
		local := cache.RegistryCachePath(entry.URL, entry.Ref)
		if oid.IsZero() {
			resolved, err := localCommitOID(local, entry.Ref)
			if err != nil {
				return exactResolution{}, err
			}
			oid = resolved
		}
		path, err := cache.AcquireExactGitWithContext(ctx, canonical, local, oid)
		if err != nil {
			return exactResolution{}, err
		}
		return exactResolution{path: path, oid: oid}, nil
	})
}

func localCommitOID(repoDir, ref string) (cache.GitOID, error) {
	hexID, err := cache.ResolveRemoteRef(repoDir, ref)
	if err != nil {
		return cache.GitOID{}, err
	}
	algorithm := ""
	switch len(hexID) {
	case 40:
		algorithm = "sha1"
	case 64:
		algorithm = "sha256"
	default:
		return cache.GitOID{}, fmt.Errorf("resolved ref %q to unexpected object id %q", ref, hexID)
	}
	return lockfile.ParseGitOID(algorithm + ":" + hexID)
}

func gitPinsPath() (string, error) {
	root := defaultStateRoot()
	if root == "" {
		return "", fmt.Errorf("state root unavailable")
	}
	return filepath.Join(root, "agent-marketplace", "codemode-git-pins.json"), nil
}

func pinKey(canonical, ref string) string {
	return canonical + "\x00" + ref
}

func loadGitPins() map[string]string {
	pins := map[string]string{}
	path, err := gitPinsPath()
	if err != nil {
		return pins
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return pins
	}
	_ = json.Unmarshal(raw, &pins)
	return pins
}

func saveGitPins(pins map[string]string) error {
	path, err := gitPinsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(pins)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pins-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func mergeGitPins(ctx context.Context, delta map[string]string) error {
	_, err := cache.WithCacheMutationLockWithContext(ctx, func() (struct{}, error) {
		merged := loadGitPins()
		for k, v := range delta {
			merged[k] = v
		}
		return struct{}{}, saveGitPins(merged)
	})
	return err
}

func pinnedOID(pins map[string]string, canonical, ref string) (cache.GitOID, error) {
	pinned, ok := pins[pinKey(canonical, ref)]
	if !ok {
		return cache.GitOID{}, fmt.Errorf("no resolved commit recorded for %s (ref %q); run a refresh", canonical, ref)
	}
	return lockfile.ParseGitOID(pinned)
}

func normalizeKind(kind string) string {
	if kind == "mcpserver" {
		return "mcp"
	}
	return kind
}

func excludedAvailability(a mpapi.Availability) bool {
	switch a {
	case "", mpapi.AvailabilityAvailable:
		return false
	default:
		return true
	}
}

func registryLabel(entry config.RegistryEntry) string {
	label := entry.URL
	if entry.IsLocal() {
		label = "local:" + filepath.Base(strings.TrimRight(entry.URL, "/"))
	}
	label = strings.TrimPrefix(label, "https://")
	label = strings.TrimPrefix(label, "http://")
	label = strings.TrimPrefix(label, "git@")
	label = strings.ReplaceAll(label, "/", "-")
	label = strings.TrimSuffix(label, ".git")
	return label
}

func registryLabels(entries []config.RegistryEntry, baseDir string) []string {
	labels := make([]string, len(entries))
	identities := make([]string, len(entries))
	used := map[string]bool{}
	counts := map[string]int{}
	for i := range entries {
		labels[i] = registryLabel(entries[i])
		counts[labels[i]]++
		used[labels[i]] = true
		identity, err := cache.RegistryKey(entries[i], baseDir)
		if err != nil {
			identity = entries[i].URL + "\x00" + entries[i].Ref + "\x00" + entries[i].Path
		}
		identities[i] = identity
	}
	groupIdentities := map[string]map[string]bool{}
	for i := range entries {
		if counts[labels[i]] < 2 {
			continue
		}
		set := groupIdentities[labels[i]]
		if set == nil {
			set = map[string]bool{}
			groupIdentities[labels[i]] = set
		}
		set[identities[i]] = true
	}
	assigned := map[string]string{}
	for i := range entries {
		if counts[labels[i]] < 2 || len(groupIdentities[labels[i]]) < 2 {
			continue
		}
		if prev, ok := assigned[identities[i]]; ok {
			labels[i] = prev
			continue
		}
		sum := sha256.Sum256([]byte(identities[i]))
		digest := hex.EncodeToString(sum[:])
		label := ""
		for off := 0; off+8 <= len(digest); off += 8 {
			candidate := labels[i] + "-" + digest[off:off+8]
			if !used[candidate] {
				label = candidate
				break
			}
		}
		labels[i] = label
		assigned[identities[i]] = label
		used[label] = true
	}
	return labels
}

func registryOrigin(contentBase string, e catalog.Entry, entry config.RegistryEntry, exactRoot string) *Origin {
	dir := contentBase
	file := ""
	if e.Path != "" {
		p := e.Path
		if exactRoot != "" && filepath.IsAbs(p) {
			if rel, err := filepath.Rel(cache.RegistryCachePath(entry.URL, entry.Ref), p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				p = filepath.Join(exactRoot, rel)
			}
		}
		if filepath.IsAbs(p) {
			dir = filepath.Dir(p)
		} else {
			dir = filepath.Join(contentBase, filepath.Dir(p))
		}
		file = filepath.Base(p)
	}
	o := &Origin{ContentDir: dir, File: file, SourceURL: entry.URL, SourcePath: entry.Path}
	if srv, ok := e.Resource.(*ir.McpServer); ok {
		o.Server = &ServerDef{
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
			URL:     srv.URL,
			Headers: srv.Headers,
			Digest:  serverDefDigest(srv),
		}
	}
	return o
}

func serverDefDigest(srv *ir.McpServer) string {
	raw, _ := json.Marshal(map[string]any{
		"command": srv.Command, "args": srv.Args, "env": srv.Env,
		"url": srv.URL, "headers": srv.Headers,
	})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
