package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/internal/logging/logvalue"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const (
	placeholderBackfillBatch   = 100
	placeholderBackfillSamples = 5
)

// PlaceholderBackfillReport counts the outcome of a placeholder archive backfill.
type PlaceholderBackfillReport struct {
	Examined       int
	Present        int
	Derived        int
	Unreproducible int
	// UnreproducibleSample holds the first few unreproducible archive digests.
	UnreproducibleSample []store.ArchiveDigest
	// Invalid counts stored archives whose final blob is missing or cannot be decoded.
	Invalid       int
	InvalidSample []store.ArchiveDigest
}

// StartPlaceholderBackfill runs BackfillPlaceholders in the background so that
// the backfill never delays readiness.
func StartPlaceholderBackfill(ctx context.Context, deps Deps) {
	go func() {
		_ = BackfillPlaceholders(ctx, deps)
	}()
}

// BackfillPlaceholders stores the placeholder-form archive of every stored archive
// that lacks one, deriving it from the final archive. Unreproducible archives are
// counted and logged; they do not fail the backfill.
func BackfillPlaceholders(ctx context.Context, deps Deps) error {
	if deps.Marketplace.ID == "" {
		if err := EnsureMarketplace(ctx, &deps); err != nil {
			logPlaceholderBackfill(PlaceholderBackfillReport{}, err)
			return err
		}
	}
	report, err := backfillPlaceholderArchives(ctx, deps.MetadataStore, deps.BlobStore, deps.Marketplace.ID)
	logPlaceholderBackfill(report, err)
	return err
}

func backfillPlaceholderArchives(ctx context.Context, meta store.MetadataStore, blobs store.BlobStore, mpID store.MarketplaceID) (PlaceholderBackfillReport, error) {
	var report PlaceholderBackfillReport
	var after store.ArchiveDigest
	for {
		digests, err := meta.ListResourceArchiveDigests(ctx, mpID, after, placeholderBackfillBatch)
		if err != nil {
			return report, fmt.Errorf("list archive digests: %w", err)
		}
		for _, digest := range digests {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			report.Examined++
			if err := backfillPlaceholderArchive(ctx, blobs, digest, &report); err != nil {
				return report, err
			}
		}
		if len(digests) < placeholderBackfillBatch {
			return report, nil
		}
		after = digests[len(digests)-1]
	}
}

func backfillPlaceholderArchive(ctx context.Context, blobs store.BlobStore, digest store.ArchiveDigest, report *PlaceholderBackfillReport) error {
	exists, err := blobs.Exists(ctx, store.PlaceholderArchiveKey(digest))
	if err != nil {
		return fmt.Errorf("check placeholder archive %s: %w", digest, err)
	}
	if exists {
		report.Present++
		return nil
	}
	final, err := blobs.Get(ctx, digest)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			recordSkipped(&report.Invalid, &report.InvalidSample, digest)
			return nil
		}
		return fmt.Errorf("load archive %s: %w", digest, err)
	}
	placeholder, err := hccartifact.DerivePlaceholderArchive(final, string(digest))
	if errors.Is(err, hccartifact.ErrPlaceholderNotReproducible) {
		recordSkipped(&report.Unreproducible, &report.UnreproducibleSample, digest)
		return nil
	}
	if err != nil {
		recordSkipped(&report.Invalid, &report.InvalidSample, digest)
		return nil
	}
	if err := store.PutImmutableBlob(ctx, blobs, store.PlaceholderArchiveKey(digest), placeholder); err != nil {
		return fmt.Errorf("store placeholder archive %s: %w", digest, err)
	}
	report.Derived++
	return nil
}

func recordSkipped(count *int, sample *[]store.ArchiveDigest, digest store.ArchiveDigest) {
	*count++
	if len(*sample) < placeholderBackfillSamples {
		*sample = append(*sample, digest)
	}
}

func logPlaceholderBackfill(report PlaceholderBackfillReport, err error) {
	if err != nil {
		slog.Error("placeholder archive backfill failed", "examined", report.Examined, "err", logvalue.Safe(err))
		return
	}
	if report.Unreproducible > 0 {
		slog.Error("placeholder archives are not reproducible from their stored archives",
			"count", report.Unreproducible, "digests", logvalue.Safe(report.UnreproducibleSample))
	}
	if report.Invalid > 0 {
		slog.Error("stored archives are missing or invalid",
			"count", report.Invalid, "digests", logvalue.Safe(report.InvalidSample))
	}
	slog.Info("placeholder archive backfill finished", "examined", report.Examined, "present", report.Present,
		"derived", report.Derived, "unreproducible", report.Unreproducible, "invalid", report.Invalid)
}
