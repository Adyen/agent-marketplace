package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func pruneNativeCommandFile(path string, observed os.FileInfo, boundary *destinationBoundary, dryRun bool) (err error) {
	if dryRun && observed.Mode().IsRegular() {
		_, err := readNativeCommandFile(path, observed, boundary, true)
		return err
	}
	var digest string
	if observed.Mode().IsRegular() {
		var parent *fsboundary.Dir
		parent, err = capturePortableParent(path, boundary, false)
		if err != nil {
			return err
		}
		if parent == nil {
			return fsboundary.ErrGenerationConflict
		}
		defer func() {
			if closeErr := parent.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
		h := sha256.New()
		if err := writePortableDigestEntry(h, parent, filepath.Base(path), ""); err != nil {
			return err
		}
		digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	}
	return prunePortableOutput(path, observed, digest, "", boundary, dryRun)
}

func readNativeCommandFile(path string, observed os.FileInfo, boundary *destinationBoundary, allowMissingLeaf ...bool) (data []byte, err error) {
	parent, err := capturePortableParent(path, boundary, false)
	if err != nil {
		if observed != nil && missingPruneEntry(err) {
			return nil, fmt.Errorf("%w: observed native command parent disappeared: %w", fsboundary.ErrGenerationConflict, err)
		}
		return nil, err
	}
	if parent == nil {
		if observed != nil {
			return nil, fsboundary.ErrGenerationConflict
		}
		return nil, fs.ErrNotExist
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return readNativeCommandSnapshot(parent, filepath.Base(path), observed, allowMissingLeaf...)
}

func readNativeCommandSnapshot(parent *fsboundary.Dir, name string, observed os.FileInfo, allowMissingLeaf ...bool) (data []byte, err error) {
	file, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if !file.Exists() {
		if observed != nil && (len(allowMissingLeaf) == 0 || !allowMissingLeaf[0]) {
			return nil, fsboundary.ErrGenerationConflict
		}
		return nil, fs.ErrNotExist
	}
	if file.Link() != nil {
		return nil, fsboundary.ErrUnsafeFile
	}
	if observed != nil {
		expected, err := fsboundary.FileInfoIdentity(observed)
		if err != nil {
			return nil, err
		}
		if !file.Identity().SameState(expected) {
			return nil, fmt.Errorf("%w: native command snapshot changed", fsboundary.ErrGenerationConflict)
		}
	}
	data, _, err = file.ReadTarget(file.Identity().Size)
	return data, err
}
