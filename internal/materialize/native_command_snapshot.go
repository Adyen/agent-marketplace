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

func missingNativeCommandPruneEntry(err error) bool {
	return errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrGenerationConflict) &&
		!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile)
}

func removeNativeCommandFile(path string, observed os.FileInfo, boundary *destinationBoundary) (err error) {
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
	return removePortableOutput(path, observed, digest, boundary)
}

func readNativeCommandFile(path string, observed os.FileInfo, boundary *destinationBoundary) (data []byte, err error) {
	parent, err := capturePortableParent(path, boundary, false)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, fs.ErrNotExist
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return readNativeCommandSnapshot(parent, filepath.Base(path), observed)
}

func readNativeCommandSnapshot(parent *fsboundary.Dir, name string, observed os.FileInfo) (data []byte, err error) {
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
