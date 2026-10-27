package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// updateSelectionRemoveLine is the remediation line for a non-required miss,
// printed after its SelectedNotFoundError line.
const updateSelectionRemoveLine = "  → run 'agent-marketplace remove --kind %s %s' before rerunning update; install a renamed resource afterwards with 'agent-marketplace install <new-name>'"

// UpdateSelectionRequiredLine is the remediation line for a required miss,
// which remove cannot drop.
const UpdateSelectionRequiredLine = "  → required selection: 'agent-marketplace remove' cannot drop it; change the configuration entry that marks it required, or ask that configuration's owner"

// updateSelectionLoadNote reports a prior lock revision that could not be
// loaded, so its selections were compared as newly missing.
const updateSelectionLoadNote = "note: could not load the previously locked revision for %s (%v); its selections were compared as newly missing"

// UpdateSelectionRefusalHint is the fixed trailing line of every update
// selection refusal.
const UpdateSelectionRefusalHint = "update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'"

// SelectionPriorLoadError is a prior lock revision that could not be loaded.
type SelectionPriorLoadError struct {
	LockPath string
	Err      error
}

// UpdateSelectionRefusedError is the typed pre-publication refusal returned
// when a selection that resolved at the previously locked revision no longer
// resolves at the new one. Its text is header-free: for each miss the existing
// SelectedNotFoundError line and its remediation line, then one note per prior
// revision that could not be loaded, then the fixed closing line.
type UpdateSelectionRefusedError struct {
	misses     []*resolver.SelectedNotFoundError
	LoadErrors []SelectionPriorLoadError
}

func (e *UpdateSelectionRefusedError) Error() string {
	var b strings.Builder
	for _, miss := range e.misses {
		b.WriteString(miss.Error())
		b.WriteByte('\n')
		b.WriteString(MissRemediationLine(miss))
		b.WriteByte('\n')
	}
	for _, loadErr := range e.LoadErrors {
		fmt.Fprintf(&b, updateSelectionLoadNote+"\n", loadErr.LockPath, loadErr.Err)
	}
	b.WriteString(UpdateSelectionRefusalHint)
	return b.String()
}

// DiagnosticCause is the refusal's cause on one line for the TUI errors
// screen: each missing selection and each prior revision that could not be
// loaded.
func (e *UpdateSelectionRefusedError) DiagnosticCause() string {
	parts := make([]string, 0, len(e.misses)+len(e.LoadErrors))
	for _, miss := range e.misses {
		parts = append(parts, miss.Error())
	}
	for _, loadErr := range e.LoadErrors {
		parts = append(parts, fmt.Sprintf(updateSelectionLoadNote, loadErr.LockPath, loadErr.Err))
	}
	return strings.Join(parts, "; ")
}

// DiagnosticRemediation is the refusal's remediation on one line for the TUI
// errors screen: each miss's remediation, then the closing line.
func (e *UpdateSelectionRefusedError) DiagnosticRemediation() string {
	parts := make([]string, 0, len(e.misses)+1)
	for _, miss := range e.misses {
		parts = append(parts, strings.TrimPrefix(MissRemediationLine(miss), "  → "))
	}
	parts = append(parts, UpdateSelectionRefusalHint)
	return strings.Join(parts, "; ")
}

var terminalPrintableSyntax = strings.NewReplacer(`\\`, `\`, `\"`, `"`)

// TerminalSafeText quotes value for terminal output without the surrounding
// quotes and with printable escapes undone.
func TerminalSafeText(value string) string {
	escaped := strings.TrimSuffix(strings.TrimPrefix(marketplace.QuoteTerminal(value), `"`), `"`)
	return terminalPrintableSyntax.Replace(escaped)
}

func MissRemediationLine(miss *resolver.SelectedNotFoundError) string {
	if miss.Required {
		return UpdateSelectionRequiredLine
	}
	return fmt.Sprintf(updateSelectionRemoveLine, skill.KindLabel(miss.Kind), TerminalSafeText(miss.Name))
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
	canonical, err := CanonicalRegistryURL(miss.URL)
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
// ResolvedUpdateSides carries the prior and candidate resolutions that both
// the selection guard and the update preview consume. loadErrors records
// prior snapshot documents that could not be loaded.
type ResolvedUpdateSides struct {
	Prior      pipeline.ResolveResult
	Desired    pipeline.ResolveResult
	PriorSet   *ExactLockSet
	DesiredSet *ExactLockSet
	LoadErrors []SelectionPriorLoadError
}

// ResolvePriorAndDesired resolves the prior (currently locked) and desired
// (staged overlay) effective selections over exact snapshots. priorJobs hold
// the previously published lock documents; overlays and lockDesired carry the
// staged config/lock bytes of the candidate state. When
// priorMarketplaceFromCache is set, the prior resolution reads Marketplace
// resources from the verified local cache only, the way read-only commands
// do, so a superseded hosted revision is still comparable from its installed
// evidence.
func ResolvePriorAndDesired(ctx context.Context, ws pipeline.Workspace, offline bool, priorJobs []SnapshotJob, overlays map[string][]byte, lockDesired map[string][]byte, desiredSet *ExactLockSet, lookup func(*ExactLockSet, func() error) catalog.ExactSnapshotFunc, priorMarketplaceFromCache bool) (ResolvedUpdateSides, error) {
	priorSet := &ExactLockSet{BySource: map[string]LockedGit{}, byIdentity: map[string][]LockedGit{}}
	var loadErrors []SelectionPriorLoadError
	// recorded identities name sources a previously published lock pinned, even
	// when that revision can no longer be loaded: they must keep their exact
	// refusals. Only never-locked sources (initial acquisition) fall back.
	recorded := make(map[string]bool)
	for _, job := range priorJobs {
		for _, registry := range job.Doc.Registries {
			recorded[gitLockIdentity(registry.URL, registry.Ref)] = true
		}
	}
	for _, job := range priorJobs {
		// An unavailable prior snapshot leaves the layer's misses looking new,
		// which is the conservative direction.
		if err := addLayerSnapshots(ctx, priorSet, job.Source, job.Entries, job.Doc, offline, false, nil); err != nil {
			loadErrors = append(loadErrors, SelectionPriorLoadError{LockPath: job.LockPath, Err: err})
		}
	}
	// Unlocked organization sources carry no lock document, so prior jobs never
	// include them. They resolve identically in both passes; copying the
	// desired set's unlocked markers keeps their stale selections from looking
	// newly missing.
	for key, locked := range desiredSet.BySource {
		if locked.Unlocked {
			priorSet.BySource[key] = locked
		}
	}
	// A source with no previously locked revision has no recorded baseline. A
	// preview comparison still needs the prior resolution to succeed without
	// fabricating history, so a never-locked source resolves from the desired
	// set's verified snapshot and its prior-side occurrences are dropped
	// afterward (dropUnlockedPriorSides), leaving an initial acquisition.
	// The newly-missing-selection guard keeps the raw exact-lock lookup: a
	// never-locked source with an unresolvable include must stay a refusal
	// exactly as it was before the preview feature, not silently pass because
	// a fallback made the prior miss observable.
	priorLookup := lookup(priorSet, nil)
	if priorMarketplaceFromCache {
		innerPriorLookup := priorLookup
		desiredLookup := lookup(desiredSet, nil)
		priorLookup = func(entry config.RegistryEntry) (string, string, error) {
			snapshot, catalogKey, err := innerPriorLookup(entry)
			if err == nil {
				return snapshot, catalogKey, nil
			}
			canonical, canonicalErr := CanonicalRegistryURL(entry.URL)
			if canonicalErr != nil || recorded[gitLockIdentity(canonical, entry.Ref)] {
				return "", "", err
			}
			if snapshot, catalogKey, desiredErr := desiredLookup(entry); desiredErr == nil {
				return snapshot, catalogKey, nil
			}
			return "", "", err
		}
	}
	priorOptions := pipeline.ResolveOptions{
		Workspace:     ws,
		SkipInventory: true,
		CatalogOption: []catalog.LoadOption{catalog.WithVerifiedExactSnapshots(priorLookup, priorSet.catalogs)},
	}
	if priorMarketplaceFromCache {
		// Read the previously pinned Marketplace revisions from the verified
		// local cache exactly as the read-only commands do: a hosted revision
		// superseded upstream stays comparable from its installed evidence.
		inspect := resolver.MarketplaceResolveModeInspect
		priorOptions.CatalogMode = catalog.LoadModeInspect
		priorOptions.MarketplaceMode = &inspect
		priorOptions.MarketplaceArtifactsFromCache = true
	}
	prior := pipeline.Resolve(ctx, priorOptions)

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
	return ResolvedUpdateSides{Prior: prior, Desired: desired, PriorSet: priorSet, DesiredSet: desiredSet, LoadErrors: loadErrors}, nil
}

func guardNewlyMissingSelections(ctx context.Context, ws pipeline.Workspace, offline bool, priorJobs []SnapshotJob, overlays map[string][]byte, lockDesired map[string][]byte, desiredSet *ExactLockSet, lookup func(*ExactLockSet, func() error) catalog.ExactSnapshotFunc) error {
	sides, err := ResolvePriorAndDesired(ctx, ws, offline, priorJobs, overlays, lockDesired, desiredSet, lookup, false)
	if err != nil {
		return err
	}
	priorMisses := make(map[selectionMissIdentity]bool)
	for _, miss := range selectionMisses(sides.Prior) {
		priorMisses[missIdentityOf(miss)] = true
	}

	seen := make(map[selectionMissIdentity]bool)
	var misses []*resolver.SelectedNotFoundError
	for _, miss := range selectionMisses(sides.Desired) {
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
	return &UpdateSelectionRefusedError{misses: misses, LoadErrors: sides.LoadErrors}
}
