//go:build unix

package fsboundary

import (
	"fmt"
	"io"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// ReadFile reads an opened file with a limit+1 bound and a post-read
// identity check. The opened object must be a regular file; content larger
// than limit bytes, including growth observed while reading, is
// ErrTooLarge; any identity change between the pre-read and post-read
// descriptor stats is ErrGenerationConflict.
func ReadFile(file *os.File, limit int64) ([]byte, Identity, error) {
	return readBounded(file, limit, nil)
}

// readBounded is ReadFile with a test barrier invoked between the pre-read
// identity capture and the read, so mutation-during-read regressions are
// deterministic instead of timing-dependent.
func readBounded(file *os.File, limit int64, barrier func(file *os.File)) ([]byte, Identity, error) {
	if limit < 0 || limit == math.MaxInt64 {
		return nil, Identity{}, fmt.Errorf("%w: invalid read limit %d", ErrTooLarge, limit)
	}
	before, err := identityOf(file)
	if err != nil {
		return nil, Identity{}, fmt.Errorf("inspect %s before read: %w", file.Name(), err)
	}
	if !before.Mode.IsRegular() {
		return nil, Identity{}, fmt.Errorf("%w: %s is not a regular file", ErrUnsafeFile, file.Name())
	}
	if before.Size > limit {
		return nil, Identity{}, fmt.Errorf("%w: %s is %d bytes, limit %d", ErrTooLarge, file.Name(), before.Size, limit)
	}
	if barrier != nil {
		barrier(file)
	}
	if err := unix.SetNonblock(int(file.Fd()), false); err != nil {
		return nil, Identity{}, fmt.Errorf("restore blocking reads on %s: %w", file.Name(), err)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, Identity{}, fmt.Errorf("read %s: %w", file.Name(), err)
	}
	if int64(len(data)) > limit {
		return nil, Identity{}, fmt.Errorf("%w: %s exceeds limit %d", ErrTooLarge, file.Name(), limit)
	}
	after, err := identityOf(file)
	if err != nil {
		return nil, Identity{}, fmt.Errorf("inspect %s after read: %w", file.Name(), err)
	}
	if !after.SameState(before) {
		return nil, Identity{}, fmt.Errorf("%w: %s changed while reading", ErrGenerationConflict, file.Name())
	}
	return data, before, nil
}

// ReadFile opens rel below the captured directory and reads it with the
// shared bounded-read contract.
func (d *Dir) ReadFile(rel string, limit int64) ([]byte, Identity, error) {
	file, _, err := d.OpenFile(rel)
	if err != nil {
		return nil, Identity{}, err
	}
	defer func() { _ = file.Close() }()
	return ReadFile(file, limit)
}
