package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/resourcediff"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"golang.org/x/sys/unix"
)

// preparedUpdateView carries the prepared prior/candidate state to the update
// preview inside lock preparation. It exists before any new publication so
// requested comparisons complete before new config or lock bytes are written.
type preparedUpdateView = lifecycle.PreparedUpdateView

// updatePreview is the prepared report model for a previewed or applied
// update. It describes proposed dependency changes and the effective
// selected-resource changes between the previously locked and candidate
// revisions.
type updatePreview struct {
	Locks            []previewGitLock
	ResourceChanges  []resourcediff.ResourceChange
	LiveSources      []previewLiveSource
	includeDiffs     bool
	computedDuration time.Duration
}

// previewGitLock is one proposed exact Git acquisition-lock change.
type previewGitLock struct {
	Scope     string
	URL       string
	Ref       string
	OldRef    string
	OldCommit string
	NewCommit string
	// Initial reports an acquisition with no previously locked commit for the
	// layer's lock at all.
	Initial bool
}

// previewLiveSource is a source that has no historical locked comparison:
// live local-directory registries and intentionally unlocked organization
// sources.
type previewLiveSource struct {
	Scope string
	URL   string
}

// buildUpdatePreview resolves the prior and candidate effective selections
// from the prepared view and computes the proposed changes. includeDiffs
// controls whether supporting-file bodies are retained for rendering; a
// summary comparison digests admitted files instead of holding their bytes.
func buildUpdatePreview(ctx context.Context, view preparedUpdateView, includeDiffs bool) (*updatePreview, error) {
	started := time.Now()
	sides, err := lifecycle.ResolvePriorAndDesired(ctx, view.Ws, view.Offline, view.PriorJobs, view.Overlays, view.LockDesired, view.DesiredSet, view.Lookup, true)
	if err != nil {
		return nil, err
	}
	// An unavailable recorded baseline refuses before any other comparison
	// result: name the lock and the load failure rather than the derived
	// lookup error, without substituting an empty or installed baseline.
	if len(sides.LoadErrors) > 0 {
		var messages []string
		for _, loadErr := range sides.LoadErrors {
			messages = append(messages, fmt.Sprintf("load previously locked revision for %s: %v", loadErr.LockPath, loadErr.Err))
		}
		return nil, errors.New(strings.Join(messages, "; "))
	}
	// A prior resolution that cannot produce comparable resources is an
	// unavailable recorded baseline: name it and refuse rather than
	// substituting an empty or installed baseline.
	if len(sides.Prior.Errors) > 0 {
		return nil, fmt.Errorf("prepare previously locked resources for comparison: %w", errors.Join(sides.Prior.Errors...))
	}
	if len(sides.Desired.Errors) > 0 {
		return nil, fmt.Errorf("prepare candidate resources for comparison: %w", errors.Join(sides.Desired.Errors...))
	}

	priorSides, err := previewSides(sides.Prior, sides.PriorSet, includeDiffs)
	if err != nil {
		return nil, err
	}
	// A source with no previously locked commit has no comparable baseline:
	// its prior-side occurrence came from the live-cache fallback, not from a
	// recorded revision. Drop it so the candidate appears as an initial
	// acquisition rather than as a fabricated comparison against current
	// upstream content.
	priorSides = dropUnlockedPriorSides(priorSides, sides.PriorSet, sides.DesiredSet)
	desiredSides, err := previewSides(sides.Desired, sides.DesiredSet, includeDiffs)
	if err != nil {
		return nil, err
	}
	changes, err := resourcediff.Compare(priorSides, desiredSides)
	if err != nil {
		return nil, err
	}
	locks, err := previewGitLockChanges(view)
	if err != nil {
		return nil, err
	}
	preview := &updatePreview{
		Locks:            locks,
		ResourceChanges:  changes,
		LiveSources:      previewLiveSources(sides.Desired, sides.DesiredSet),
		includeDiffs:     includeDiffs,
		computedDuration: time.Since(started),
	}
	slog.Debug("update preview computed", "changes", len(changes), "locks", len(locks), "duration_ms", preview.computedDuration.Milliseconds())
	return preview, nil
}

// previewSides converts one resolution's effective selected set into
// comparison sides. Every admitted supporting file is read from the verified
// acquired representation — never from installed files or generated harness
// output. Selected collections are virtual roots that expand into members, so
// they join the sides from the display selection: their definition and
// membership changes must stay reviewable alongside their affected members.
func previewSides(result pipeline.ResolveResult, set *lifecycle.ExactLockSet, keepBodies bool) ([]resourcediff.Side, error) {
	artifacts := result.Resolved
	if len(result.SelectedCollections) > 0 {
		combined := make([]resolver.ResolvedArtifact, 0, len(artifacts)+len(result.SelectedCollections))
		combined = append(combined, artifacts...)
		combined = append(combined, result.SelectedCollections...)
		artifacts = combined
	}
	sides := make([]resourcediff.Side, 0, len(artifacts))
	for _, res := range artifacts {
		// Organization-layer selections are never update candidates: an
		// applying update does not check or rewrite organization-managed
		// pins, so a preview must not compare them against their cached
		// revisions either — that would propose changes no invocation makes.
		// Unlocked organization Git sources stay visible as live sources.
		if previewScopeLabel(res.Source) == "organization" {
			continue
		}
		if res.Unresolved || res.Resource == nil {
			return nil, fmt.Errorf("comparison side for %s %q has no resolved definition", res.Kind.String(), res.Name)
		}
		identity := resourcediff.Identity{
			Scope:      previewScopeLabel(res.Source),
			Kind:       res.Kind.String(),
			Name:       previewResourceName(res),
			SourceURL:  res.RegistryURL,
			InstanceID: res.MarketplaceInstanceID,
			Coordinate: res.MarketplaceRegistryID,
			Ref:        res.Ref,
			Path:       res.RegistryPath,
			Via:        res.Via,
		}
		revision := ""
		if res.IsMarketplace {
			revision = res.MarketplaceDigest
		} else if commit, ok := set.GitRevision(res.Source, res.RegistryURL, res.Ref); ok {
			revision = commit
		}
		support, err := previewSupportFiles(res, keepBodies)
		if err != nil {
			return nil, fmt.Errorf("read supporting files for %s %q: %w", res.Kind.String(), res.Name, err)
		}
		sides = append(sides, resourcediff.Side{
			Identity: identity,
			Revision: revision,
			Resource: res.Resource,
			Support:  support,
		})
	}
	return sides, nil
}

// dropUnlockedPriorSides removes prior-side occurrences whose source has no
// previously locked revision: the candidate acquisition is initial, so no
// historical comparison exists for that source. Sources recorded under any
// ref of the same canonical repository keep their prior side, because a ref
// bump rewrites the configured ref after the lock recorded the old one.
func dropUnlockedPriorSides(prior []resourcediff.Side, priorSet, desiredSet *lifecycle.ExactLockSet) []resourcediff.Side {
	priorCanonical := make(map[string]bool, len(priorSet.BySource))
	desiredCanonical := make(map[string]bool, len(desiredSet.BySource))
	for _, locked := range priorSet.BySource {
		if !locked.Unlocked {
			priorCanonical[locked.Canonical] = true
		}
	}
	for _, locked := range desiredSet.BySource {
		if !locked.Unlocked {
			desiredCanonical[locked.Canonical] = true
		}
	}
	kept := prior[:0]
	for _, side := range prior {
		canonical, err := lifecycle.CanonicalRegistryURL(side.Identity.SourceURL)
		if err != nil {
			kept = append(kept, side)
			continue
		}
		if !priorCanonical[canonical] && desiredCanonical[canonical] {
			// The candidate locks this source but the prior state did not:
			// initial acquisition, no comparable baseline.
			continue
		}
		kept = append(kept, side)
	}
	return kept
}

func previewScopeLabel(source config.Source) string {
	return source.LockScope()
}

func previewResourceName(res resolver.ResolvedArtifact) string {
	if res.InstalledName != "" {
		return res.InstalledName
	}
	return res.Name
}

// previewSupportFiles collects the admitted supporting-file inventory for one
// resolved resource with exact bytes (keepBodies) or digests only. Admission
// follows the acquisition contracts: skills expose their declared support
// files, Git hooks expose the adjacent-file inventory, and Marketplace hook
// support comes from the verified mirror artifact.
func previewSupportFiles(res resolver.ResolvedArtifact, keepBodies bool) (map[string]resourcediff.SupportFile, error) {
	var relPaths []string
	var root string
	switch typed := res.Resource.(type) {
	case *ir.Skill:
		relPaths = typed.SupportFiles
		if len(relPaths) == 0 {
			return nil, nil
		}
		base, err := previewResourceRoot(res)
		if err != nil {
			return nil, err
		}
		root = base
	case *ir.Hook:
		if res.IsMarketplace {
			return previewMarketplaceHookSupport(res, keepBodies), nil
		}
		definition, err := previewDefinitionPath(res)
		if err != nil {
			return nil, err
		}
		adjacent, err := catalog.HookSupportFiles(definition)
		if err != nil {
			return nil, err
		}
		if len(adjacent) == 0 {
			return nil, nil
		}
		relPaths = adjacent
		root = filepath.Dir(definition)
	default:
		return nil, nil
	}
	support := make(map[string]resourcediff.SupportFile, len(relPaths))
	for _, rel := range relPaths {
		normalized, err := normalizeSupportRelPath(rel)
		if err != nil {
			return nil, err
		}
		file, err := readPreviewSupportFile(root, normalized, keepBodies)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", normalized, err)
		}
		support[normalized] = file
	}
	return support, nil
}

// previewMarketplaceHookSupport reads the verified mirror artifact's hook
// support inventory, which was admitted during acquisition.
func previewMarketplaceHookSupport(res resolver.ResolvedArtifact, keepBodies bool) map[string]resourcediff.SupportFile {
	if res.MarketplaceHookSupport == nil {
		return nil
	}
	support := make(map[string]resourcediff.SupportFile, len(res.MarketplaceHookSupport))
	for _, file := range res.MarketplaceHookSupport {
		entry := resourcediff.SupportFile{Executable: file.Mode&0o111 != 0}
		if keepBodies {
			entry.Data = append([]byte(nil), file.Bytes...)
		} else {
			entry.Digest = contentDigest(file.Bytes)
		}
		support[file.Path] = entry
	}
	return support
}

// previewResourceRoot returns the directory holding a resource's supporting
// files: the staged Marketplace mirror directory or the exact Git snapshot
// directory of the definition file.
func previewResourceRoot(res resolver.ResolvedArtifact) (string, error) {
	if res.IsMarketplace {
		if res.StagedSourceDir != "" {
			return res.StagedSourceDir, nil
		}
		if res.Path != "" {
			if info, err := os.Stat(res.Path); err == nil && info.IsDir() {
				return res.Path, nil
			}
		}
		return "", fmt.Errorf("marketplace resource has no staged source directory")
	}
	definition, err := previewDefinitionPath(res)
	if err != nil {
		return "", err
	}
	return filepath.Dir(definition), nil
}

// previewDefinitionPath returns the absolute path of a Git resource's
// definition file inside its verified exact snapshot, preferring the
// preserved source reference the acquisition recorded over the catalog's
// live-cache path mapping.
func previewDefinitionPath(res resolver.ResolvedArtifact) (string, error) {
	if source := previewSourceFile(res.Resource); source != "" && filepath.IsAbs(source) {
		return source, nil
	}
	if res.Path != "" && filepath.IsAbs(res.Path) {
		return res.Path, nil
	}
	return "", fmt.Errorf("resource definition path is unavailable")
}

func previewSourceFile(resource ir.Resource) string {
	switch typed := resource.(type) {
	case *ir.Skill:
		return typed.Source.File
	case *ir.Command:
		return typed.Source.File
	case *ir.Subagent:
		return typed.Source.File
	case *ir.Hook:
		return typed.Source.File
	case *ir.McpServer:
		return typed.Source.File
	case *ir.Rule:
		return typed.Source.File
	case *ir.Collection:
		return typed.Source.File
	default:
		return ""
	}
}

func normalizeSupportRelPath(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty supporting-file path")
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "\x00") {
		return "", fmt.Errorf("unsafe supporting-file path")
	}
	return clean, nil
}

// readPreviewSupportFile reads one admitted supporting file from a verified
// resource root with the existing containment and special-file contracts: it
// refuses final symlinks, non-regular files, and paths escaping the root.
// A diff must read and print the body, so an unreadable symlink target refuses
// the comparison rather than following it. A digest-only summary does not
// print bodies, so it records the file without its body — the change still
// surfaces as a content change against the previously readable body instead
// of blocking the summary of an update the applying invocation accepts.
func readPreviewSupportFile(root, rel string, keepBodies bool) (file resourcediff.SupportFile, retErr error) {
	target := filepath.FromSlash(rel)
	if !filepath.IsLocal(target) {
		return resourcediff.SupportFile{}, fmt.Errorf("supporting file escapes the resource root")
	}
	resourceRoot, err := os.OpenRoot(root)
	if err != nil {
		return resourcediff.SupportFile{}, fmt.Errorf("open resource root: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, resourceRoot.Close()) }()
	info, err := resourceRoot.Lstat(target)
	if err != nil {
		return resourcediff.SupportFile{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		if !keepBodies {
			return resourcediff.SupportFile{Executable: info.Mode()&0o111 != 0}, nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return resourcediff.SupportFile{}, fmt.Errorf("supporting file is a symlink")
		}
		return resourcediff.SupportFile{}, fmt.Errorf("supporting file is not a regular file")
	}
	opened, err := resourceRoot.OpenFile(target, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return resourcediff.SupportFile{}, err
	}
	defer func() { retErr = errors.Join(retErr, opened.Close()) }()
	info, err = opened.Stat()
	if err != nil {
		return resourcediff.SupportFile{}, err
	}
	if !info.Mode().IsRegular() {
		if !keepBodies {
			return resourcediff.SupportFile{Executable: info.Mode()&0o111 != 0}, nil
		}
		return resourcediff.SupportFile{}, fmt.Errorf("supporting file is not a regular file")
	}
	file = resourcediff.SupportFile{Executable: info.Mode()&0o111 != 0}
	data, err := io.ReadAll(opened)
	if err != nil {
		return resourcediff.SupportFile{}, err
	}
	if keepBodies {
		file.Data = data
	} else {
		file.Digest = contentDigest(data)
	}
	return file, nil
}

func contentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// previewGitLockChanges derives the proposed exact Git lock changes from the
// previously published lock documents and the prepared desired lock bytes.
// Lock documents record one exact commit per registry URL and ref.
func previewGitLockChanges(view preparedUpdateView) ([]previewGitLock, error) {
	priorDocs := make(map[string]lockfile.Document, len(view.PriorJobs))
	priorScopes := make(map[string]string, len(view.PriorJobs))
	for _, job := range view.PriorJobs {
		priorDocs[job.LockPath] = job.Doc
		priorScopes[job.LockPath] = job.Source.LockScope()
	}
	desiredPaths := make([]string, 0, len(view.LockDesired))
	for lockPath := range view.LockDesired {
		desiredPaths = append(desiredPaths, lockPath)
	}
	sort.Strings(desiredPaths)
	var changes []previewGitLock
	for _, lockPath := range desiredPaths {
		desired, err := lockfile.Decode(view.LockDesired[lockPath])
		if err != nil {
			return nil, fmt.Errorf("decode prepared lock %s: %w", lockPath, err)
		}
		prior, hasPrior := priorDocs[lockPath]
		scope := string(desired.Scope)
		if scope == "" && hasPrior {
			scope = string(prior.Scope)
		}
		if scope == "" {
			scope = priorScopes[lockPath]
		}
		for _, registry := range desired.Registries {
			var previous []lockfile.GitRegistry
			if hasPrior {
				previous = previewPriorRegistries(prior, registry.URL, registry.Ref, view.RefBumps)
			}
			newCommit := registry.Commit.String()
			if len(previous) == 0 {
				changes = append(changes, previewGitLock{Scope: scope, URL: registry.URL, Ref: registry.Ref, NewCommit: newCommit, Initial: true})
				continue
			}
			for _, priorRegistry := range previous {
				oldCommit := priorRegistry.Commit.String()
				if oldCommit == newCommit && priorRegistry.Ref == registry.Ref {
					continue
				}
				changes = append(changes, previewGitLock{
					Scope: scope, URL: registry.URL, Ref: registry.Ref, OldRef: priorRegistry.Ref,
					OldCommit: oldCommit, NewCommit: newCommit,
				})
			}
		}
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Scope != changes[j].Scope {
			return changes[i].Scope < changes[j].Scope
		}
		if changes[i].URL != changes[j].URL {
			return changes[i].URL < changes[j].URL
		}
		if changes[i].Ref != changes[j].Ref {
			return changes[i].Ref < changes[j].Ref
		}
		return changes[i].OldRef < changes[j].OldRef
	})
	return changes, nil
}

func previewPriorRegistries(doc lockfile.Document, url, ref string, bumps []pipeline.RefBump) []lockfile.GitRegistry {
	for _, registry := range doc.Registries {
		if registry.URL == url && registry.Ref == ref {
			return []lockfile.GitRegistry{registry}
		}
	}
	// Follow the actual ref edits, not an arbitrary same-URL baseline.
	// Several old refs can converge on one candidate; retain each baseline.
	var prior []lockfile.GitRegistry
	for _, registry := range doc.Registries {
		if registry.URL != url {
			continue
		}
		for _, bump := range bumps {
			canonical, err := lifecycle.CanonicalRegistryURL(bump.URL)
			if err == nil && canonical == url && bump.NewRef == ref && bump.OldRef == registry.Ref {
				prior = append(prior, registry)
				break
			}
		}
	}
	return prior
}

// previewLiveSources identifies sources with no historical locked comparison:
// selected Git sources absent from the exact lock set resolve live, whether
// they are local-directory registries or intentionally unlocked organization
// sources.
func previewLiveSources(result pipeline.ResolveResult, set *lifecycle.ExactLockSet) []previewLiveSource {
	seen := make(map[string]bool)
	var live []previewLiveSource
	for _, res := range result.Resolved {
		if res.IsMarketplace {
			continue
		}
		if _, ok := set.GitRevision(res.Source, res.RegistryURL, res.Ref); ok {
			continue
		}
		key := previewScopeLabel(res.Source) + "\x00" + res.RegistryURL
		if seen[key] {
			continue
		}
		seen[key] = true
		live = append(live, previewLiveSource{Scope: previewScopeLabel(res.Source), URL: res.RegistryURL})
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].Scope != live[j].Scope {
			return live[i].Scope < live[j].Scope
		}
		return live[i].URL < live[j].URL
	})
	return live
}

// writeUpdatePreviewOutput writes the prepared preview report. applied
// distinguishes an applied update with diffs from a proposed dry-run. The
// report identifies scopes, sources, kinds, names, and old/candidate revision
// identities, and only includeDiffs previews render content hunks.
func writeUpdatePreviewOutput(out io.Writer, preview *updatePreview, result *pipeline.UpdateResult, bump, verbose, applied, includeDiffs bool) error {
	if applied {
		if result != nil {
			if err := writeUpdateOutput(out, result, bump, verbose); err != nil {
				return err
			}
		}
	} else {
		if _, err := fmt.Fprintln(out, "Dry run: no updates were applied."); err != nil {
			return err
		}
		if result != nil {
			if err := writeUpdateProposalSummary(out, result, bump, verbose); err != nil {
				return err
			}
		}
		if preview != nil {
			if err := writePreviewLockChanges(out, preview); err != nil {
				return err
			}
			if err := writePreviewResourceSummary(out, preview); err != nil {
				return err
			}
			if err := writePreviewLiveSources(out, preview); err != nil {
				return err
			}
		}
		if preview == nil || (len(preview.ResourceChanges) == 0 && len(preview.Locks) == 0) {
			if _, err := fmt.Fprintln(out, "No versioned updates are available."); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out, "This preview does not enumerate every subsequent materialization change."); err != nil {
			return err
		}
	}
	if includeDiffs && preview != nil {
		if err := writePreviewContentDiffs(out, preview); err != nil {
			return err
		}
	}
	return nil
}

// writeUpdateProposalSummary mirrors the ordinary update report with proposal
// phrasing for registry counts, ref bumps, and Marketplace pin decisions.
func writeUpdateProposalSummary(out io.Writer, result *pipeline.UpdateResult, bump, verbose bool) error {
	if bump {
		if _, err := fmt.Fprintf(out, "Configured registry entries: %d, refs to bump: %d\n", result.UpdatedRegistries, len(result.Bumps)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, "Configured registry entries: %d\n", result.UpdatedRegistries); err != nil {
		return err
	}
	for _, skipped := range result.SkippedBumps {
		if _, err := fmt.Fprintf(out, "Ref bump skipped for %s: %s -> %s is declared in an included config file; update it there\n", marketplace.QuoteTerminal(skipped.URL), marketplace.QuoteTerminal(skipped.OldRef), marketplace.QuoteTerminal(skipped.NewRef)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "Marketplace pins proposed for update: %d\n", result.UpdatedMarketplacePins); err != nil {
		return err
	}
	if err := writeMarketplaceSkipSummary(out, result, verbose); err != nil {
		return err
	}
	for _, pin := range result.MarketplacePins {
		if pin.Source == config.Organization && !pin.Updated && !verbose {
			continue
		}
		identity := fmt.Sprintf("%s/%s/%s", marketplace.QuoteTerminal(pin.OldPin.RegistryID), marketplace.QuoteTerminal(pin.OldPin.Kind), marketplace.QuoteTerminal(pin.OldPin.Name))
		if pin.Updated {
			if pin.OldPin.Digest == "" {
				if _, err := fmt.Fprintf(out, "Marketplace %s %s: initial acquisition at %s\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, marketplace.QuoteTerminal(pin.NewPin.Digest)); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(out, "Marketplace %s %s: update %s -> %s\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, marketplace.QuoteTerminal(pin.OldPin.Digest), marketplace.QuoteTerminal(pin.NewPin.Digest)); err != nil {
				return err
			}
			continue
		}
		if pin.Source == config.Organization {
			if _, err := fmt.Fprintf(out, "Marketplace %s %s: not checked (organization-managed)\n", marketplace.QuoteTerminal(pin.Entry.URL), identity); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, "Marketplace %s %s: skipped (%s)\n", marketplace.QuoteTerminal(pin.Entry.URL), identity, terminalSafeText(pin.Reason)); err != nil {
			return err
		}
	}
	return nil
}

func writePreviewLockChanges(out io.Writer, preview *updatePreview) error {
	if len(preview.Locks) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "Proposed acquisition-lock changes:"); err != nil {
		return err
	}
	for _, lock := range preview.Locks {
		locator := marketplace.QuoteTerminal(lock.URL)
		if lock.Ref != "" {
			ref := marketplace.QuoteTerminal(lock.Ref)
			if lock.OldRef != "" && lock.OldRef != lock.Ref {
				ref = marketplace.QuoteTerminal(lock.OldRef) + " -> " + ref
			}
			locator += " (ref " + ref + ")"
		}
		if lock.Initial {
			if _, err := fmt.Fprintf(out, "  %s %s: initial acquisition at %s\n", lock.Scope, locator, marketplace.QuoteTerminal(lock.NewCommit)); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, "  %s %s: %s -> %s\n", lock.Scope, locator, marketplace.QuoteTerminal(lock.OldCommit), marketplace.QuoteTerminal(lock.NewCommit)); err != nil {
			return err
		}
	}
	return nil
}

func writePreviewResourceSummary(out io.Writer, preview *updatePreview) error {
	if len(preview.ResourceChanges) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "Proposed selected-resource changes:"); err != nil {
		return err
	}
	for _, change := range preview.ResourceChanges {
		line, err := previewResourceChangeLine(change)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "  %s\n", line); err != nil {
			return err
		}
	}
	return nil
}

func writePreviewLiveSources(out io.Writer, preview *updatePreview) error {
	if len(preview.LiveSources) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "Sources with no historical locked comparison (live; not compared as versioned content):"); err != nil {
		return err
	}
	for _, source := range preview.LiveSources {
		if _, err := fmt.Fprintf(out, "  %s %s\n", source.Scope, marketplace.QuoteTerminal(source.URL)); err != nil {
			return err
		}
	}
	return nil
}

// previewResourceChangeLine renders one resource-level change with its full
// identity, change classification, and old/candidate revision identities.
func previewResourceChangeLine(change resourcediff.ResourceChange) (string, error) {
	id := change.Identity
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s", id.Scope, id.Kind, marketplace.QuoteTerminal(id.Name))
	if id.Via != "" {
		fmt.Fprintf(&b, " via %s", marketplace.QuoteTerminal(id.Via))
	}
	b.WriteString(" from ")
	writePreviewSourceIdentity(&b, id)
	switch change.Kind {
	case resourcediff.ChangeAdded:
		if change.OldRevision == "" {
			b.WriteString(": added (initial acquisition)")
		} else {
			b.WriteString(": added")
		}
	case resourcediff.ChangeRemoved:
		b.WriteString(": removed")
	case resourcediff.ChangeRevisionOnly:
		b.WriteString(": revision changed; reviewed resource content is unchanged")
	case resourcediff.ChangeSource:
		b.WriteString(": source changed (previous ")
		writePreviewSourceIdentity(&b, change.Prior.Identity)
		b.WriteByte(')')
	default:
		b.WriteString(": modified")
	}
	if change.DefinitionChanged {
		b.WriteString("; definition changed")
	}
	if len(change.Support) > 0 {
		fmt.Fprintf(&b, "; %d supporting file(s) changed", len(change.Support))
	}
	if change.OldRevision != "" || change.NewRevision != "" {
		fmt.Fprintf(&b, "; %s -> %s", marketplace.QuoteTerminal(change.OldRevision), marketplace.QuoteTerminal(change.NewRevision))
	}
	return b.String(), nil
}

func writePreviewSourceIdentity(out *strings.Builder, id resourcediff.Identity) {
	out.WriteString(marketplace.QuoteTerminal(id.SourceURL))
	if id.InstanceID != "" {
		fmt.Fprintf(out, " instance %s", marketplace.QuoteTerminal(id.InstanceID))
	}
	if id.Coordinate != "" {
		fmt.Fprintf(out, " registry %s", marketplace.QuoteTerminal(id.Coordinate))
	}
	if id.Path != "" && id.Path != "." {
		fmt.Fprintf(out, " path %s", marketplace.QuoteTerminal(id.Path))
	}
	if id.Ref != "" {
		fmt.Fprintf(out, " (ref %s)", marketplace.QuoteTerminal(id.Ref))
	}
}

// writePreviewContentDiffs renders the normalized review content for every
// changed resource: definition hunks and exact supporting-file changes. Added
// or removed resources label their full content as an addition or removal,
// never as a comparison against content that was never installed.
func writePreviewContentDiffs(out io.Writer, preview *updatePreview) error {
	if len(preview.ResourceChanges) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "Resource content diffs (normalized review content; not the original authored files; not a patch you can apply):"); err != nil {
		return err
	}
	for _, change := range preview.ResourceChanges {
		line, err := previewResourceChangeLine(change)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "\n%s\n", line); err != nil {
			return err
		}
		if err := writePreviewDefinitionDiff(out, change); err != nil {
			return err
		}
		if err := writePreviewSupportDiffs(out, change); err != nil {
			return err
		}
	}
	return nil
}

func writePreviewDefinitionDiff(out io.Writer, change resourcediff.ResourceChange) error {
	var priorPath, oldDoc string
	if change.Prior != nil {
		path, doc, err := resourcediff.Definition(change.Prior.Resource)
		if err != nil {
			return err
		}
		priorPath, oldDoc = path, doc
	}
	var candidatePath, newDoc string
	if change.Candidate != nil {
		path, doc, err := resourcediff.Definition(change.Candidate.Resource)
		if err != nil {
			return err
		}
		candidatePath, newDoc = path, doc
	}
	reviewPath := previewDefinitionReviewPath(priorPath, candidatePath)
	if oldDoc == newDoc {
		if _, err := fmt.Fprintf(out, "  Definition %s: reviewed content unchanged\n", reviewPath); err != nil {
			return err
		}
		return nil
	}
	hunks := resourcediff.Unified(reviewPath+" (locked revision)", reviewPath+" (candidate revision)", []byte(oldDoc), []byte(newDoc))
	if _, err := fmt.Fprintf(out, "  Definition %s: changed\n", reviewPath); err != nil {
		return err
	}
	_, err := io.WriteString(out, indentReviewHunks(hunks))
	return err
}

func previewDefinitionReviewPath(priorPath, candidatePath string) string {
	if candidatePath != "" {
		return candidatePath
	}
	return priorPath
}

func writePreviewSupportDiffs(out io.Writer, change resourcediff.ResourceChange) error {
	if len(change.Support) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "  Supporting files:"); err != nil {
		return err
	}
	for _, support := range change.Support {
		if err := writePreviewSupportDiff(out, support); err != nil {
			return err
		}
	}
	return nil
}

func writePreviewSupportDiff(out io.Writer, support resourcediff.SupportChange) error {
	if (support.Kind == resourcediff.SupportAdded && !resourcediff.RenderableText(support.New.Data)) ||
		(support.Kind == resourcediff.SupportRemoved && !resourcediff.RenderableText(support.Old.Data)) {
		_, err := fmt.Fprintf(out, "    %s: binary content %s (%s -> %s; executable mode %s -> %s)\n",
			marketplace.QuoteTerminal(support.Path), support.Kind, previewByteSummary(support.Old), previewByteSummary(support.New),
			previewModeLabel(support.Old), previewModeLabel(support.New))
		return err
	}
	switch support.Kind {
	case resourcediff.SupportText:
		hunks := resourcediff.Unified(support.Path+" (locked revision)", support.Path+" (candidate revision)", support.Old.Data, support.New.Data)
		if _, err := fmt.Fprintf(out, "    %s: text changed\n", marketplace.QuoteTerminal(support.Path)); err != nil {
			return err
		}
		_, err := io.WriteString(out, indentReviewHunks(hunks))
		return err
	case resourcediff.SupportAdded:
		hunks := resourcediff.Unified(support.Path, support.Path+" (added)", nil, support.New.Data)
		if _, err := fmt.Fprintf(out, "    %s: added\n", marketplace.QuoteTerminal(support.Path)); err != nil {
			return err
		}
		_, err := io.WriteString(out, indentReviewHunks(hunks))
		return err
	case resourcediff.SupportRemoved:
		hunks := resourcediff.Unified(support.Path, support.Path+" (removed)", support.Old.Data, nil)
		if _, err := fmt.Fprintf(out, "    %s: removed\n", marketplace.QuoteTerminal(support.Path)); err != nil {
			return err
		}
		_, err := io.WriteString(out, indentReviewHunks(hunks))
		return err
	case resourcediff.SupportBinary:
		_, err := fmt.Fprintf(out, "    %s: binary content changed (%s -> %s)\n", marketplace.QuoteTerminal(support.Path), previewByteSummary(support.Old), previewByteSummary(support.New))
		return err
	case resourcediff.SupportModeOnly:
		_, err := fmt.Fprintf(out, "    %s: executable mode changed (%s -> %s)\n", marketplace.QuoteTerminal(support.Path), previewModeLabel(support.Old), previewModeLabel(support.New))
		return err
	case resourcediff.SupportChanged:
		_, err := fmt.Fprintf(out, "    %s: content changed\n", marketplace.QuoteTerminal(support.Path))
		return err
	default:
		_, err := fmt.Fprintf(out, "    %s: changed\n", marketplace.QuoteTerminal(support.Path))
		return err
	}
}

func previewByteSummary(file *resourcediff.SupportFile) string {
	if file == nil {
		return "absent"
	}
	digest := file.Digest
	if digest == "" && file.Data != nil {
		digest = contentDigest(file.Data)
	}
	return strconv.Itoa(len(file.Data)) + " bytes, " + digest
}

func previewModeLabel(file *resourcediff.SupportFile) string {
	if file == nil {
		return "absent"
	}
	if file.Executable {
		return "executable"
	}
	return "not executable"
}

// indentReviewHunks indents rendered hunks so resource sections stay visually
// grouped while every hunk line remains a terminal-safe sanitized line.
func indentReviewHunks(hunks string) string {
	if hunks == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(hunks, "\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
