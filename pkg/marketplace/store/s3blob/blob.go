package s3blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Store is an S3-compatible content-addressed [store.BlobStore].
// Blobs are stored under the content digest as the object key.
type Store struct {
	client *s3.Client
	bucket string
}

// Option configures a Store.
type Option func(*options)

type options struct {
	endpoint string
}

// WithEndpoint sets a custom S3-compatible endpoint (e.g. MinIO, Ceph, R2).
func WithEndpoint(endpoint string) Option {
	return func(o *options) {
		o.endpoint = endpoint
	}
}

// New returns an S3-compatible blob store for the given bucket and region.
// Credentials are loaded from the environment by default; callers may also
// use AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY to provide them.
func New(ctx context.Context, bucket, region string, opts ...Option) (*Store, error) {
	if bucket == "" {
		return nil, fmt.Errorf("%w: missing bucket", store.ErrTopology)
	}
	if region == "" {
		return nil, fmt.Errorf("%w: missing region", store.ErrTopology)
	}

	var cfgOpts []func(*config.LoadOptions) error
	cfgOpts = append(cfgOpts, config.WithRegion(region))

	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.endpoint != "" {
		cfgOpts = append(cfgOpts, config.WithBaseEndpoint(o.endpoint))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, cfgOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return &Store{client: client, bucket: bucket}, nil
}

// Get returns the blob bytes for digest. It returns [store.ErrNotFound] when
// the blob does not exist.
func (s *Store) Get(ctx context.Context, digest store.ArchiveDigest) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(string(digest)),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		var noSuchBucket *types.NoSuchBucket
		if errors.As(err, &noSuchKey) || errors.As(err, &noSuchBucket) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("get blob %s: %w", digest, err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", digest, err)
	}
	return data, nil
}

// Put stores data under the given digest, atomically replacing any existing
// content at that address.
func (s *Store) Put(ctx context.Context, digest store.ArchiveDigest, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(string(digest)),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		return fmt.Errorf("put blob %s: %w", digest, err)
	}
	return nil
}

// Exists reports whether a blob exists for the given digest.
func (s *Store) Exists(ctx context.Context, digest store.ArchiveDigest) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(string(digest)),
	})
	if err == nil {
		return true, nil
	}

	var notFound *types.NotFound
	var noSuchKey *types.NoSuchKey
	var noSuchBucket *types.NoSuchBucket
	if errors.As(err, &notFound) || errors.As(err, &noSuchKey) || errors.As(err, &noSuchBucket) {
		return false, nil
	}
	return false, fmt.Errorf("head blob %s: %w", digest, err)
}

// Delete removes the blob for the given digest.
func (s *Store) Delete(ctx context.Context, digest store.ArchiveDigest) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(string(digest)),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		var noSuchBucket *types.NoSuchBucket
		if errors.As(err, &noSuchKey) || errors.As(err, &noSuchBucket) {
			return store.ErrNotFound
		}
		return fmt.Errorf("delete blob %s: %w", digest, err)
	}
	return nil
}
