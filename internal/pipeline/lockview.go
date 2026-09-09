package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	unlocked bool
	err      error
}

type readOnlyExactView struct {
	catalogOption catalog.LoadOption
	exactSnapshot catalog.ExactSnapshotFunc
	gitMetadata   []LockMetadata
	errors        []error
	warnings      []error
	release       func()
}

func exactLockViewWithContext(ctx context.Context, ec config.EffectiveConfig, allowMissingLocks bool) (context.Context, readOnlyExactView) {
	bySource := make(map[string]readOnlyLockedGit)
	byIdentity := make(map[string][]readOnlyLockedGit)
	var metadata []LockMetadata
	var viewErrors []error
	var viewWarnings []error
	var releases []func()
	releaseAll := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}

	for _, layer := range ec.PresentLayers() {
		registries := readOnlyRemoteEntries(layer)
		if len(registries) == 0 {
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
		doc, layerErr := readOnlyLayerLock(layer.Path, lockPath, scope, registries)
		if layerErr != nil {
			var lockErr *LockViewError
			missing := errors.As(layerErr, &lockErr) && lockErr.Code == "lock.missing"
			if !allowMissingLocks || !missing {
				viewErrors = append(viewErrors, layerErr)
			}
			if missing && allowMissingLocks {
				viewWarnings = append(viewWarnings, layerErr)
			}
			for _, entry := range registries {
				canonical, canonicalErr := readOnlyCanonicalURL(entry.URL)
				if canonicalErr != nil {
					canonical = entry.URL
				}
				locked := readOnlyLockedGit{err: layerErr}
				if missing && allowMissingLocks {
					locked = readOnlyLockedGit{unlocked: true}
				}
				bySource[readOnlySourceKey(layer.Source, canonical, entry.Ref)] = locked
				identity := readOnlyGitIdentity(canonical, entry.Ref)
				byIdentity[identity] = append(byIdentity[identity], locked)
			}
			continue
		}
		if scope == lockfile.ScopeOrganization && doc.APIVersion == "" {
			// An absent organization lock is an explicit unlocked authority: read
			// commands may inspect it through the mutable cache, but a present lock
			// must always validate and can never silently downgrade to this path.
			for _, entry := range registries {
				canonical, canonicalErr := readOnlyCanonicalURL(entry.URL)
				if canonicalErr != nil {
					viewErrors = append(viewErrors, canonicalErr)
					continue
				}
				bySource[readOnlySourceKey(layer.Source, canonical, entry.Ref)] = readOnlyLockedGit{unlocked: true}
			}
			continue
		}
		for _, registry := range doc.Registries {
			oid, parseErr := cache.ParseGitOID(registry.Commit.String())
			snapshot := ""
			errorCode := "lock.snapshot-unavailable"
			openErr := parseErr
			if openErr == nil {
				var release func()
				snapshot, release, openErr = cache.LeaseExactGitReadOnlyWithContext(ctx, registry.URL, oid)
				if openErr == nil {
					releases = append(releases, release)
					ctx = cache.ContextWithExactGitNamespaceLease(ctx)
				}
			}
			if openErr == nil {
				for _, root := range registry.Roots {
					if _, verifyErr := sourcerevision.VerifyLockedGitRoot(snapshot, root); verifyErr != nil {
						errorCode = "lock.manifest-mismatch"
						openErr = fmt.Errorf("verify locked root %q: %w", root.Path, verifyErr)
						break
					}
				}
			}
			metadata = append(metadata, LockMetadata{Source: layer.Source, Scope: scope, Path: lockPath, URL: registry.URL, Ref: registry.Ref, Commit: registry.Commit.String(), SnapshotAvailable: openErr == nil})
			locked := readOnlyLockedGit{snapshot: snapshot, commit: registry.Commit.String()}
			if openErr != nil {
				locked.err = &LockViewError{Code: errorCode, Scope: scope, Path: lockPath, Err: openErr}
			}
			identity := readOnlyGitIdentity(registry.URL, registry.Ref)
			bySource[readOnlySourceKey(layer.Source, registry.URL, registry.Ref)] = locked
			byIdentity[identity] = append(byIdentity[identity], locked)
		}
	}

	sort.Slice(metadata, func(i, j int) bool {
		if metadata[i].Source != metadata[j].Source {
			return metadata[i].Source < metadata[j].Source
		}
		if metadata[i].URL != metadata[j].URL {
			return metadata[i].URL < metadata[j].URL
		}
		return metadata[i].Ref < metadata[j].Ref
	})
	exactSnapshot := catalog.ExactSnapshotFunc(func(entry config.RegistryEntry) (string, string, error) {
		canonical, err := readOnlyCanonicalURL(entry.URL)
		if err != nil {
			return "", "", err
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
		if locked.unlocked {
			return "", "", nil
		}
		return locked.snapshot, canonical + "@" + locked.commit + ":" + entry.Path, nil
	})
	return ctx, readOnlyExactView{catalogOption: catalog.WithExactSnapshots(exactSnapshot), exactSnapshot: exactSnapshot, gitMetadata: metadata, errors: viewErrors, warnings: viewWarnings, release: releaseAll}
}

func readOnlyLayerLock(configPath, path string, scope lockfile.Scope, registries []config.RegistryEntry) (lockfile.Document, error) {
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
	if !readOnlyGitDocumentMatches(doc, registries) {
		return doc, &LockViewError{Code: "lock.stale", Scope: scope, Path: path, Err: errors.New("lock does not match configuration")}
	}
	return doc, nil
}

func readOnlyGitDocumentMatches(doc lockfile.Document, entries []config.RegistryEntry) bool {
	wanted := make(map[string][]lockfile.GitRootRequirement)
	for _, entry := range entries {
		canonical, err := readOnlyCanonicalURL(entry.URL)
		if err != nil {
			return false
		}
		identity := readOnlyGitIdentity(canonical, entry.Ref)
		wanted[identity] = append(wanted[identity], lockfile.GitRootRequirement{Path: entry.Path, SourceHarness: entry.SourceHarness})
	}
	if len(doc.Registries) != len(wanted) {
		return false
	}
	for _, registry := range doc.Registries {
		roots, ok := wanted[readOnlyGitIdentity(registry.URL, registry.Ref)]
		if !ok {
			return false
		}
		projected, matches := lockfile.ProjectGitRoots(registry.Roots, roots)
		if !matches || len(projected) != len(registry.Roots) {
			return false
		}
	}
	return true
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
	return lockfile.CanonicalGitConfigURL(raw)
}
func readOnlySourceKey(source config.Source, canonical, ref string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", source, canonical, ref)
}
func readOnlyGitIdentity(canonical, ref string) string { return canonical + "\x00" + ref }
