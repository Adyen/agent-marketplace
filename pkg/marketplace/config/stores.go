package config

import (
	"context"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/s3blob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// OpenStores opens the metadata and blob stores described by cfg.
func OpenStores(ctx context.Context, cfg Config) (store.MetadataStore, store.BlobStore, error) {
	dsn, err := cfg.Storage.Metadata.ResolveDSN()
	if err != nil {
		return nil, nil, err
	}
	var meta store.MetadataStore
	switch cfg.Storage.Metadata.Driver {
	case "sqlite":
		meta = sqlite.New(dsn)
	case "postgres":
		meta = postgres.New(dsn)
	default:
		return nil, nil, fmt.Errorf("unsupported metadata driver %q", cfg.Storage.Metadata.Driver)
	}
	if err := meta.Open(ctx); err != nil {
		// Store adapters sanitize their own startup errors, but keep this
		// boundary defensive because this error is logged by serve/reindex.
		return nil, nil, fmt.Errorf("open metadata store: %w", store.SanitizeDSNError(err, dsn))
	}

	var blobs store.BlobStore
	switch cfg.Storage.Blobs.Driver {
	case "filesystem":
		blobs = fsblob.New(cfg.Storage.Blobs.Path)
	case "s3":
		var opts []s3blob.Option
		if cfg.Storage.Blobs.Endpoint != "" {
			opts = append(opts, s3blob.WithEndpoint(cfg.Storage.Blobs.Endpoint))
		}
		s, err := s3blob.New(ctx, cfg.Storage.Blobs.Bucket, cfg.Storage.Blobs.Region, opts...)
		if err != nil {
			_ = meta.Close()
			return nil, nil, fmt.Errorf("open s3 blob store: %w", err)
		}
		blobs = s
	default:
		_ = meta.Close()
		return nil, nil, fmt.Errorf("unsupported blob driver %q", cfg.Storage.Blobs.Driver)
	}

	return meta, blobs, nil
}
