//go:build windows

package fsboundary

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// NativeRoot returns a standard-library root for the captured directory.
//
// os.Root cannot be built from a handle, so on Windows the directory is
// opened by the final path name of the captured handle and then verified to be
// the captured object. A directory that was replaced between the two steps is
// refused as ErrGenerationConflict; it is detected, not prevented.
func (d *Dir) NativeRoot() (*os.Root, error) {
	path, err := finalPath(d.file)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve captured directory %s: %w", ErrGenerationConflict, d.path, err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("%w: open captured directory %s: %w", ErrGenerationConflict, d.path, err)
	}
	opened, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("%w: open captured directory %s: %w", ErrGenerationConflict, d.path, err)
	}
	id, err := identityOf(opened)
	_ = opened.Close()
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("%w: inspect captured directory %s: %w", ErrGenerationConflict, d.path, err)
	}
	if !id.SameObject(d.id) {
		_ = root.Close()
		return nil, fmt.Errorf("%w: captured directory %s was replaced", ErrGenerationConflict, d.path)
	}
	return root, nil
}

// finalPath returns the normalized DOS path of the object behind file's
// handle, without the extended-length prefix.
func finalPath(file *os.File) (string, error) {
	handle := windows.Handle(file.Fd())
	buffer := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if int(n) < len(buffer) {
			path := windows.UTF16ToString(buffer[:n])
			switch {
			case strings.HasPrefix(path, `\\?\UNC\`):
				return `\\` + path[len(`\\?\UNC\`):], nil
			case strings.HasPrefix(path, `\\?\`):
				return path[len(`\\?\`):], nil
			}
			return path, nil
		}
		buffer = make([]uint16, n+1)
	}
}
