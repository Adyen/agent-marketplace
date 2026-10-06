package backup

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func withBackupPayload(path string, read func(io.Reader) error) (err error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	file, err := fsboundary.CaptureDestination(root, filepath.Base(absolute), fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return fmt.Errorf("%w: backup archive %s is linked", fsboundary.ErrUnsafeFile, absolute)
	}
	return file.ReadTargetWith(read)
}
