package ownership

import (
	"fmt"
	"io"
)

// Keep ownership metadata bounded even when a hostile file grows between stat
// and read. The limit matches dependency transactions' durable metadata bound.
const maxOwnershipMetadataSize int64 = 4 << 20

// readOwnershipFile opens a metadata file without following a final symlink on
// supported Unix platforms, verifies that the opened object is a regular file,
// and reads at most Max+1 bytes so a size race cannot turn a bounded read into
// an unbounded allocation.
func readOwnershipFile(path string, maxSize int64) ([]byte, error) {
	file, err := openOwnershipFile(path)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, fmt.Errorf("ownership: inspect %s: %w", path, statErr)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("ownership: final path %s is not a regular file", path)
	}
	if maxSize > 0 && info.Size() > maxSize {
		_ = file.Close()
		return nil, fmt.Errorf("ownership: file %s exceeds %d bytes", path, maxSize)
	}

	reader := io.Reader(file)
	if maxSize > 0 {
		reader = io.LimitReader(file, maxSize+1)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("ownership: read %s: %w", path, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("ownership: close %s: %w", path, closeErr)
	}
	if maxSize > 0 && int64(len(data)) > maxSize {
		return nil, fmt.Errorf("ownership: file %s exceeds %d bytes", path, maxSize)
	}
	return data, nil
}
