package main

import (
	"context"
	"errors"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// updateSelectionRefusalHint is the fixed trailing line of every update
// selection refusal.
const updateSelectionRefusalHint = "update left the lock and harness outputs unchanged; run 'agent-marketplace sync --prune-stale' to drop missing selections, then 'agent-marketplace install <new-name>' for any renamed resource, and rerun 'agent-marketplace update'"

// updateSelectionRefusedError is the typed pre-publication refusal returned
// when a selection that resolved at the previously locked revision no longer
// resolves at the new one. Its text is header-free: one line per miss using
// the existing SelectedNotFoundError wording, then the fixed hint.
type updateSelectionRefusedError struct {
	misses []*resolver.SelectedNotFoundError
}

func (e *updateSelectionRefusedError) Error() string {
	var b strings.Builder
	for _, miss := range e.misses {
		b.WriteString(miss.Error())
		b.WriteByte('\n')
	}
	b.WriteString(updateSelectionRefusalHint)
	return b.String()
}

// selectionMissIdentity identifies a selected-not-found diagnostic across the
// prior and desired resolutions: kind, name, canonical source URL, source
// path, and the collection it came through (without the collection version).
// The ref is excluded so `update --bump` compares like with like.
type selectionMissIdentity struct {
	kind       string
	name       string
	url        string
	path       string
	collection string
}

func missIdentityOf(miss *resolver.SelectedNotFoundError) selectionMissIdentity {
	canonical, err := canonicalRegistryURL(miss.URL)
	if err != nil {
		canonical = miss.URL
	}
	return selectionMissIdentity{kind: skill.KindLabel(miss.Kind), name: miss.Name, url: canonical, path: miss.Path, collection: missCollectionName(miss.Via)}
}

func missCollectionName(via string) string {
	name, ok := strings.CutPrefix(via, "collection:")
	if !ok {
		return ""
	}
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}
	return name
}

func selectionMisses(result pipeline.ResolveResult) []*resolver.SelectedNotFoundError {
	var misses []*resolver.SelectedNotFoundError
	for _, err := range result.ResolveErrors {
		var miss *resolver.SelectedNotFoundError
		if errors.As(err, &miss) {
			misses = append(misses, miss)
		}
	}
	return misses
}

// guardNewlyMissingSelections resolves the effective configuration twice
// without recording inventory: once against the prior lock documents with the
// on-disk configuration, and once against the desired documents with staged
// config and lock overlays applied. It refuses when a selected-not-found
// diagnostic is new at the desired revision. Prior evidence that cannot be
// evaluated is conservative: such misses count as newly missing.
func guardNewlyMissingSelections(ctx context.Context, ws pipeline.Workspace, offline bool, priorJobs []snapshotJob, overlays map[string][]byte, lockDesired map[string][]byte, desiredSet *exactLockSet, lookup func(*exactLockSet, func() error) catalog.ExactSnapshotFunc) error {
	priorSet := &exactLockSet{bySource: map[string]lockedGit{}, byIdentity: map[string][]lockedGit{}}
	for _, job := range priorJobs {
		// An unavailable prior snapshot leaves the layer's misses looking new,
		// which is the conservative direction.
		_ = addLayerSnapshots(ctx, priorSet, job.source, job.entries, job.doc, offline, false, nil)
	}
	// Unlocked organization sources carry no lock document, so prior jobs never
	// include them. They resolve identically in both passes; copying the
	// desired set's unlocked markers keeps their stale selections from looking
	// newly missing.
	for key, locked := range desiredSet.bySource {
		if locked.unlocked {
			priorSet.bySource[key] = locked
		}
	}
	prior := pipeline.Resolve(ctx, pipeline.ResolveOptions{
		Workspace:     ws,
		SkipInventory: true,
		CatalogOption: []catalog.LoadOption{catalog.WithVerifiedExactSnapshots(lookup(priorSet, nil), priorSet.catalogs)},
	})
	priorMisses := make(map[selectionMissIdentity]bool)
	for _, miss := range selectionMisses(prior) {
		priorMisses[missIdentityOf(miss)] = true
	}

	desiredOverlays := make(map[string][]byte, len(overlays)+len(lockDesired))
	for path, data := range overlays {
		desiredOverlays[path] = data
	}
	for path, data := range lockDesired {
		desiredOverlays[path] = data
	}
	desired := pipeline.Resolve(ctx, pipeline.ResolveOptions{
		Workspace:     ws,
		SkipInventory: true,
		CatalogOption: []catalog.LoadOption{catalog.WithVerifiedExactSnapshots(lookup(desiredSet, nil), desiredSet.catalogs)},
		EffectiveOpts: config.LoadEffectiveOptions{RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir(), FileOverlays: desiredOverlays},
	})

	seen := make(map[selectionMissIdentity]bool)
	var misses []*resolver.SelectedNotFoundError
	for _, miss := range selectionMisses(desired) {
		identity := missIdentityOf(miss)
		if priorMisses[identity] || seen[identity] {
			continue
		}
		seen[identity] = true
		misses = append(misses, miss)
	}
	if len(misses) == 0 {
		return nil
	}
	return &updateSelectionRefusedError{misses: misses}
}
