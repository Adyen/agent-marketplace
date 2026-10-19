package catalog

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

var ErrHookSupportUnsafe = errors.New("marketplace.ingest.hook-support-unsafe")

// ReadHookSupportFile anchors every path component at the admitted root.
func ReadHookSupportFile(root, relative string) (data []byte, executable bool, err error) {
	defer func() {
		if err != nil {
			data, executable = nil, false
			err = fmt.Errorf("%w: %w", ErrHookSupportUnsafe, err)
		}
	}()
	if err := artifact.ValidateSupportPath(relative); err != nil {
		return nil, false, err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, false, err
	}
	directory, err := fsboundary.OpenDir(absolute)
	if err != nil {
		return nil, false, err
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	source, err := fsboundary.CaptureDestination(directory, filepath.FromSlash(relative), fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, false, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	if source.Link() != nil {
		return nil, false, fsboundary.ErrUnsafePath
	}
	data, identity, err := source.ReadTarget(2 * 1024 * 1024)
	return data, identity.Mode.Perm()&0o111 != 0, err
}
