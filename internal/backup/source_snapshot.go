package backup

import (
	"errors"
	"io"
	"io/fs"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func writeCapturedBackupSource(writer io.Writer, root *fsboundary.Dir, name string, observed fs.FileInfo) (err error) {
	file, err := fsboundary.CaptureDestination(root, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}
	}()
	if file.Link() != nil {
		return fsboundary.ErrUnsafeFile
	}
	identity, err := fsboundary.FileInfoIdentity(observed)
	if err != nil {
		return err
	}
	if !file.Identity().SameState(identity) {
		return fsboundary.ErrGenerationConflict
	}
	return file.ReadTargetWith(func(reader io.Reader) error { _, err := io.Copy(writer, reader); return err })
}
