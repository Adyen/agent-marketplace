package tui

import (
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func readPreviewSource(rootPath, filePath string, limit int) (data []byte, err error) {
	if limit < 0 || int64(limit) == math.MaxInt64 {
		return nil, fsboundary.ErrTooLarge
	}
	rootPath, err = filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	filePath, err = filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		return nil, err
	}
	root, err := fsboundary.OpenDir(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	source, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if source.Link() != nil {
		return nil, fmt.Errorf("%w: source preview is a symlink", fsboundary.ErrUnsafePath)
	}
	err = source.ReadTargetWith(func(reader io.Reader) error {
		var readErr error
		data, readErr = io.ReadAll(io.LimitReader(reader, int64(limit)+1))
		return readErr
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}
