package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/acquire"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/ingest"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// managedPreviewRetention bounds how long completed previews stay readable.
const managedPreviewRetention = 24 * time.Hour

// processManagedPreview acquires and discovers a proposed registry for an admin
// preview. It runs the same acquisition and ingestion as a refresh but never
// publishes, never touches registry heads or attempts, and stores neither
// credentials nor raw Git output.
func processManagedPreview(ctx context.Context, deps Deps, j store.Job, worker string, clone managedCloneFunc) error {
	previews, ok := deps.MetadataStore.(store.ManagedPreviewStore)
	if !ok {
		return finishManaged(ctx, deps, j, worker, store.ErrTopology)
	}
	var p admin.PreviewJobPayload
	if json.Unmarshal(j.Payload, &p) != nil || p.Marketplace != store.MarketplaceID(deps.Config.Instance.ID) || p.PreviewID == "" {
		return finishManaged(ctx, deps, j, worker, store.ErrManagedStateCorrupt)
	}
	preview, err := previews.GetManagedPreview(ctx, p.Marketplace, p.PreviewID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && (preview.Terminal() || preview.JobID != j.ID)) {
		// Pruned or already answered: there is nothing left to do for this job.
		return finishManaged(ctx, deps, j, worker, nil)
	}
	if err != nil {
		return finishManaged(ctx, deps, j, worker, err)
	}
	if err := managedLease(ctx, deps.MetadataStore, j, worker); err != nil {
		return finishManaged(ctx, deps, j, worker, err)
	}
	preview.Status = store.ManagedPreviewRunning
	if err := previews.UpdateManagedPreview(ctx, preview); err != nil {
		return finishManaged(ctx, deps, j, worker, err)
	}
	workCtx, stop := context.WithDeadline(ctx, j.LeaseUntil)
	defer stop()
	preview.Result, preview.Failure = runManagedPreview(workCtx, deps, preview, clone)
	preview.Status = store.ManagedPreviewSucceeded
	if preview.Failure != nil {
		preview.Status = store.ManagedPreviewFailed
	}
	preview.CompletedAt = time.Now().UTC()
	err = deps.MetadataStore.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if err := managedLease(ctx, tx, j, worker); err != nil {
			return err
		}
		txPreviews, ok := tx.(store.ManagedPreviewStore)
		if !ok {
			return store.ErrTopology
		}
		if err := txPreviews.UpdateManagedPreview(ctx, preview); err != nil {
			return err
		}
		return tx.CompleteJob(ctx, j.ID, worker)
	})
	if err != nil {
		return finishManaged(ctx, deps, j, worker, err)
	}
	if err := previews.PruneManagedPreviews(ctx, p.Marketplace, time.Now().UTC().Add(-managedPreviewRetention)); err != nil {
		slog.WarnContext(ctx, "managed preview prune failed", "job", j.ID)
	}
	slog.InfoContext(ctx, "managed preview completed", "preview", preview.ID, "status", preview.Status, "job", j.ID)
	return nil
}

func previewFailure(code, message string) (*store.ManagedPreviewResult, *store.ManagedPreviewFailure) {
	return nil, &store.ManagedPreviewFailure{Code: code, Message: message}
}

func runManagedPreview(ctx context.Context, deps Deps, preview store.ManagedPreview, clone managedCloneFunc) (*store.ManagedPreviewResult, *store.ManagedPreviewFailure) {
	var r admin.Registry
	if err := json.Unmarshal(preview.Registry, &r); err != nil || r.ID == "" {
		return previewFailure("invalid_proposal", "The stored proposal could not be decoded.")
	}
	view, err := managedView(ctx, deps)
	if err != nil {
		return previewFailure("configuration_unavailable", "The current managed configuration could not be read.")
	}
	creds, err := managedCredentials(deps.Config.Management, r)
	if err != nil {
		return previewFailure("profile_unavailable", "The origin or credential profile is not permitted by the deployment configuration.")
	}
	cfg, registry := managedInputs(deps, view, r)
	limits := acquire.Limits{RepositoryBytes: cfg.Policy.Limits.RepositoryBytes, FileCount: cfg.Policy.Limits.FileCount, FileBytes: cfg.Policy.Limits.FileBytes}
	roots := make([]string, len(r.Roots))
	for i := range r.Roots {
		roots[i] = r.Roots[i].Path
	}
	result, err := clone(ctx, r.Origin, r.Ref, creds, limits, cfg.Policy.Limits.CloneDuration.Duration(), roots...)
	if err != nil {
		// Git output may echo the origin with embedded credentials; only a
		// stable code and a generic message are recorded.
		return previewFailure("acquisition_failed", "Repository acquisition failed. Check the origin, ref, credential profile and that every root exists at that ref.")
	}
	defer func() { _ = result.Release() }()
	revision, err := ingest.BuildRegistryRevision(ctx, cfg, preview.Marketplace, registry, result.Dir, result.Commit)
	if err != nil {
		return previewFailure(previewIngestCode(err), previewMessage(err.Error(), result.Dir))
	}
	var current store.RegistryPublication
	if preview.Existing {
		if publications, ok := deps.MetadataStore.(store.PublicationStore); ok {
			if published, err := publications.CurrentRegistryPublication(ctx, r.ID); err == nil {
				current = published
			} else if !errors.Is(err, store.ErrNotFound) {
				return previewFailure("publication_unavailable", "The current publication could not be read for comparison.")
			}
		}
	}
	return summarizePreview(r, revision, result.Commit, view.Snapshot.PolicyRevision, current), nil
}

func previewIngestCode(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "does not exist"), strings.Contains(message, "escapes registry directory"):
		return "root_missing"
	case strings.Contains(message, "catalog error"), strings.Contains(message, "load catalog"):
		return "catalog_error"
	case strings.Contains(message, "duplicate resource"):
		return "duplicate_resource"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "discovery_failed"
}

func previewMessage(message, dir string) string {
	if dir != "" {
		message = strings.ReplaceAll(message, dir, "<repository>")
	}
	message = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\x00' {
			return ' '
		}
		return r
	}, message)
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}

// summarizePreview groups discovered resources by the deepest configured root
// that contains their source path and compares them with the current
// publication when one exists.
func summarizePreview(r admin.Registry, revision ingest.Revision, commit string, policyRevision int64, current store.RegistryPublication) *store.ManagedPreviewResult {
	result := &store.ManagedPreviewResult{Commit: commit, Ref: r.Ref, ObservedAt: time.Now().UTC(), PolicyRevision: policyRevision, DiffBase: current.ID, Roots: make([]store.ManagedPreviewRoot, len(r.Roots))}
	published := make(map[string]store.RevisionDigest, len(current.Resources))
	for _, occurrence := range current.Resources {
		published[occurrence.Coordinate.Kind+"/"+occurrence.Coordinate.Name] = occurrence.ContentDigest
	}
	assessments := make(map[string]store.ResourceAssessment, len(revision.Assessments))
	for _, assessment := range revision.Assessments {
		assessments[assessment.Coordinate.Kind+"/"+assessment.Coordinate.Name] = assessment
	}
	descriptions := make(map[string]string, len(revision.Resources))
	for _, resource := range revision.Resources {
		descriptions[resource.Coordinate.Kind+"/"+resource.Coordinate.Name] = resource.Description
	}
	for i, root := range r.Roots {
		result.Roots[i] = store.ManagedPreviewRoot{Path: root.Path, SourceHarness: root.SourceHarness, Resources: []store.ManagedPreviewResource{}}
	}
	seen := make(map[string]bool, len(revision.ResourceOccurrences))
	for _, occurrence := range revision.ResourceOccurrences {
		key := occurrence.Coordinate.Kind + "/" + occurrence.Coordinate.Name
		seen[key] = true
		entry := store.ManagedPreviewResource{Kind: occurrence.Coordinate.Kind, Name: occurrence.Coordinate.Name, SourcePath: occurrence.SourcePath, Description: descriptions[key]}
		if assessment, ok := assessments[key]; ok {
			entry.Decision, entry.Findings = assessment.Decision, len(assessment.Findings)
		}
		if current.ID != "" {
			switch digest, ok := published[key]; {
			case !ok:
				entry.Change = "added"
			case digest != occurrence.ContentDigest:
				entry.Change = "changed"
			default:
				entry.Change = "unchanged"
			}
		}
		index := previewRootIndex(r.Roots, occurrence.SourcePath)
		root := &result.Roots[index]
		root.ResourceCount++
		result.TotalResources++
		if len(root.Resources) < store.ManagedPreviewResultLimit {
			root.Resources = append(root.Resources, entry)
		} else {
			root.Truncated = true
		}
	}
	for i := range result.Roots {
		slices.SortFunc(result.Roots[i].Resources, func(a, b store.ManagedPreviewResource) int {
			return strings.Compare(a.Kind+"/"+a.Name, b.Kind+"/"+b.Name)
		})
	}
	if current.ID != "" {
		for _, occurrence := range current.Resources {
			if key := occurrence.Coordinate.Kind + "/" + occurrence.Coordinate.Name; !seen[key] {
				result.Removed = append(result.Removed, store.ManagedPreviewResource{Kind: occurrence.Coordinate.Kind, Name: occurrence.Coordinate.Name, SourcePath: occurrence.SourcePath, Change: "removed"})
			}
		}
		slices.SortFunc(result.Removed, func(a, b store.ManagedPreviewResource) int {
			return strings.Compare(a.Kind+"/"+a.Name, b.Kind+"/"+b.Name)
		})
	}
	for _, d := range revision.Diagnostics {
		result.Diagnostics = append(result.Diagnostics, store.ManagedPreviewDiagnostic{Root: d.Root, Code: d.Code, Message: previewMessage(d.Message, ""), Error: d.Error})
	}
	return result
}

// previewRootIndex picks the deepest root whose normalized path contains the
// source path, falling back to the first root.
func previewRootIndex(roots []store.RegistryRoot, sourcePath string) int {
	best, bestLen := 0, -1
	for i, root := range roots {
		clean := path.Clean(strings.ReplaceAll(root.Path, "\\", "/"))
		if clean == "." {
			if bestLen < 0 {
				best, bestLen = i, 0
			}
			continue
		}
		clean = strings.TrimPrefix(clean, "./")
		if (sourcePath == clean || strings.HasPrefix(sourcePath, clean+"/")) && len(clean) > bestLen {
			best, bestLen = i, len(clean)
		}
	}
	return best
}
