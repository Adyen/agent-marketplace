package marketplace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	cachefs "github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"golang.org/x/sys/unix"
)

func (c *Cache) readV2File(path string, limit int64) ([]byte, error) {
	exists, err := validateCacheRegularFile(c.root, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	relative, err := filepath.Rel(c.root, path)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(relative, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return nil, fmt.Errorf("artifact cache file is not singly linked regular data")
	}
	if stat.Size > limit {
		return nil, artifact.ErrArchiveLimit
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, artifact.ErrArchiveLimit
	}
	return raw, nil
}

func (c *Cache) ReadArtifactV2(ref artifact.ArtifactReference) (artifact.ArtifactV2, []byte, error) {
	if err := ref.Validate(); err != nil {
		return artifact.ArtifactV2{}, nil, err
	}
	path := c.ArtifactPath(string(ref.BlobDigest))
	marker, err := c.readV2File(verifiedMarkerPath(filepath.Dir(path)), 4096)
	if err != nil {
		return artifact.ArtifactV2{}, nil, err
	}
	if marker == nil {
		return artifact.ArtifactV2{}, nil, nil
	}
	expected, err := json.Marshal(ref)
	if err != nil {
		return artifact.ArtifactV2{}, nil, err
	}
	if !bytes.Equal(marker, expected) {
		return artifact.ArtifactV2{}, nil, artifact.ErrInvalidReference
	}
	raw, err := c.readV2File(path, maxArtifactBytes)
	if err != nil {
		return artifact.ArtifactV2{}, nil, err
	}
	if raw == nil {
		return artifact.ArtifactV2{}, nil, nil
	}
	decoded, err := artifact.VerifyV2(raw, ref, nil)
	if err != nil {
		return artifact.ArtifactV2{}, nil, err
	}
	return decoded, raw, nil
}

func (c *Cache) WriteArtifactV2(ref artifact.ArtifactReference, raw []byte) error {
	if _, err := artifact.VerifyV2(raw, ref, nil); err != nil {
		return err
	}
	return cachefs.WithCacheMutation(func() error {
		path := c.ArtifactPath(string(ref.BlobDigest))
		evidence, err := json.Marshal(ref)
		if err != nil {
			return err
		}
		old, err := c.readV2File(path, maxArtifactBytes)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		marker := verifiedMarkerPath(filepath.Dir(path))
		if bytes.Equal(old, raw) {
			existing, err := c.readV2File(marker, 4096)
			if err != nil {
				return err
			}
			if bytes.Equal(existing, evidence) {
				return nil
			}
		}
		if err := ensureCacheDirectory(c.root, filepath.Dir(path)); err != nil {
			return err
		}
		if err := writeAtomic(c.root, path, raw, 0o644, marker); err != nil {
			return err
		}
		return writeAtomic(c.root, marker, evidence, 0o644, "")
	})
}
