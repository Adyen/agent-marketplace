package skill

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func readSourceMetadata(rootPath, filePath string, read func(io.Reader) error) (err error) {
	rootPath, err = filepath.Abs(rootPath)
	if err != nil {
		return err
	}
	filePath, err = filepath.Abs(filePath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootPath, filePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: metadata path is outside its source root", fsboundary.ErrUnsafePath)
	}
	root, err := fsboundary.OpenDir(rootPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	source, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return source.ReadTargetWith(read)
}
