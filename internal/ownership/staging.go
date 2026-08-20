package ownership

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const stagedRandomBytes = 16

// stagedPathFor returns a fresh, unguessable staging path. The file is created
// separately, after the journal is durable, so the journal can describe the
// path before any replacement bytes are written.
func stagedPathFor(absFile string) (string, error) {
	suffix := make([]byte, stagedRandomBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("ownership: generate staging filename: %w", err)
	}
	return fmt.Sprintf("%s.am-staged-%s", absFile, hex.EncodeToString(suffix)), nil
}

// writeStaged creates, writes, and fsyncs a replacement. The returned
// descriptor stays open until the caller has published the path, so the
// descriptor that was exclusively created is held across the rename.
func writeStaged(path string, data []byte, mode os.FileMode) (*os.File, error) {
	f, err := openStagedFile(path, mode)
	if err != nil {
		return nil, fmt.Errorf("ownership: create staged file %s: %w", path, err)
	}

	cleanup := func(cause error) (*os.File, error) {
		_ = f.Close()
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("%w; remove staged file %s: %v", cause, path, removeErr)
		}
		return nil, cause
	}

	// O_CREATE honours umask, so set the mode explicitly.
	if err := f.Chmod(mode); err != nil {
		return cleanup(fmt.Errorf("ownership: chmod staged file %s: %w", path, err))
	}
	if n, err := f.Write(data); err != nil {
		return cleanup(fmt.Errorf("ownership: write staged file %s: %w", path, err))
	} else if n != len(data) {
		return cleanup(fmt.Errorf("ownership: short write to staged file %s: wrote %d of %d bytes", path, n, len(data)))
	}
	if err := f.Sync(); err != nil {
		return cleanup(fmt.Errorf("ownership: fsync staged file %s: %w", path, err))
	}
	if err := fsyncDir(filepath.Dir(path)); err != nil {
		return cleanup(fmt.Errorf("ownership: fsync staging directory %s: %w", filepath.Dir(path), err))
	}
	return f, nil
}
