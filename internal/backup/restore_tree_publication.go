package backup

import (
	"errors"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func restoreCapturedSymlink(parent *fsboundary.Dir, name, target string) (err error) {
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := native.RemoveAll(name); err != nil {
		return err
	}
	return native.Symlink(target, name)
}
