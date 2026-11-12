package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resourcediff"
	"github.com/Adyen/agent-marketplace/internal/updatepreview"
)

type preparedUpdateView = lifecycle.PreparedUpdateView
type updatePreview = updatepreview.Preview

func buildUpdatePreview(ctx context.Context, view preparedUpdateView, includeDiffs bool) (*updatePreview, error) {
	return updatepreview.Build(ctx, view, includeDiffs)
}

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

func readPreviewSupportFile(root, rel string, keepBodies bool) (resourcediff.SupportFile, error) {
	return updatepreview.ReadPreviewSupportFile(root, rel, keepBodies)
}

func previewPriorRegistries(doc lockfile.Document, url, ref string, bumps []pipeline.RefBump) []lockfile.GitRegistry {
	return updatepreview.PriorRegistries(doc, url, ref, bumps)
}

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

func previewByteSummary(file *resourcediff.SupportFile) string {
	if file == nil {
		return "absent"
	}
	digest := file.Digest
	if digest == "" && file.Data != nil {
		digest = updatepreview.ContentDigest(file.Data)
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

func previewDefinitionReviewPath(priorPath, candidatePath string) string {
	if candidatePath != "" {
		return candidatePath
	}
	return priorPath
}

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
