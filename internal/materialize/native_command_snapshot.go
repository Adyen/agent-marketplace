package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func removeNativeCommandFile(path string, observed os.FileInfo, boundary *destinationBoundary) error {
	var digest string
	if observed.Mode().IsRegular() {
		var err error
		digest, err = portablePathDigest(path)
		if err != nil {
			return err
		}
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
