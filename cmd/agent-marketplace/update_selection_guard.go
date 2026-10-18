package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// updateSelectionRemoveLine is the remediation line for a non-required miss,
// printed after its SelectedNotFoundError line.
const updateSelectionRemoveLine = "  → run 'agent-marketplace remove --kind %s %s' before rerunning update; install a renamed resource afterwards with 'agent-marketplace install <new-name>'"

// updateSelectionRequiredLine is the remediation line for a required miss,
// which remove cannot drop.
const updateSelectionRequiredLine = "  → required selection: 'agent-marketplace remove' cannot drop it; change the configuration entry that marks it required, or ask that configuration's owner"

// updateSelectionLoadNote reports a prior lock revision that could not be
// loaded, so its selections were compared as newly missing.
const updateSelectionLoadNote = "note: could not load the previously locked revision for %s (%v); its selections were compared as newly missing"

// updateSelectionRefusalHint is the fixed trailing line of every update
// selection refusal.
const updateSelectionRefusalHint = "update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'"

// selectionPriorLoadError is a prior lock revision that could not be loaded.
type selectionPriorLoadError struct {
	lockPath string
	err      error
}

// updateSelectionRefusedError is the typed pre-publication refusal returned
// when a selection that resolved at the previously locked revision no longer
// resolves at the new one. Its text is header-free: for each miss the existing
// SelectedNotFoundError line and its remediation line, then one note per prior
// revision that could not be loaded, then the fixed closing line.
type updateSelectionRefusedError struct {
	misses     []*resolver.SelectedNotFoundError
	loadErrors []selectionPriorLoadError
}

func (e *updateSelectionRefusedError) Error() string {
	var b strings.Builder
	for _, miss := range e.misses {
		b.WriteString(miss.Error())
		b.WriteByte('\n')
		b.WriteString(missRemediationLine(miss))
		b.WriteByte('\n')
	}
	for _, loadErr := range e.loadErrors {
		fmt.Fprintf(&b, updateSelectionLoadNote+"\n", loadErr.lockPath, loadErr.err)
	}
	b.WriteString(updateSelectionRefusalHint)
	return b.String()
}

func missRemediationLine(miss *resolver.SelectedNotFoundError) string {
	if miss.Required {
		return updateSelectionRequiredLine
	}
	return fmt.Sprintf(updateSelectionRemoveLine, skill.KindLabel(miss.Kind), terminalSafeText(miss.Name))
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
		if miss, ok := errors.AsType[*resolver.SelectedNotFoundError](err); ok {
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
	var loadErrors []selectionPriorLoadError
	for _, job := range priorJobs {
		// An unavailable prior snapshot leaves the layer's misses looking new,
		// which is the conservative direction.
		if err := addLayerSnapshots(ctx, priorSet, job.source, job.entries, job.doc, offline, false, nil); err != nil {
			loadErrors = append(loadErrors, selectionPriorLoadError{lockPath: job.lockPath, err: err})
		}
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
	return &updateSelectionRefusedError{misses: misses, loadErrors: loadErrors}
}
