//go:build darwin || linux

package cache

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type cacheDirs struct {
	parent      *os.File
	root        *os.File
	hash        *os.File
	rootName    string
	hashName    string
	rootCreated bool
	hashCreated bool
}

func openCacheDirs(cacheRoot, hashName string) (*cacheDirs, error) {
	parent, err := openCacheParent(cacheRoot, false)
	if err != nil {
		return nil, err
	}
	root, err := openDirAt(parent, filepath.Base(cacheRoot))
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	hash, err := openDirAt(root, hashName)
	if err != nil {
		_ = root.Close()
		_ = parent.Close()
		return nil, err
	}
	return &cacheDirs{parent: parent, root: root, hash: hash, rootName: filepath.Base(cacheRoot), hashName: hashName}, nil
}

func createCacheDirs(cacheRoot, hashName string) (*cacheDirs, error) {
	parent, err := openCacheParent(cacheRoot, true)
	if err != nil {
		return nil, err
	}

	root, rootCreated, err := openOrCreateDirAt(parent, filepath.Base(cacheRoot))
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	hash, hashCreated, err := openOrCreateDirAt(root, hashName)
	if err != nil {
		if rootCreated {
			_ = removeEmptyDirAt(parent, filepath.Base(cacheRoot))
		}
		_ = root.Close()
		_ = parent.Close()
		return nil, err
	}
	return &cacheDirs{
		parent:      parent,
		root:        root,
		hash:        hash,
		rootName:    filepath.Base(cacheRoot),
		hashName:    hashName,
		rootCreated: rootCreated,
		hashCreated: hashCreated,
	}, nil
}

func (d *cacheDirs) close() {
	if d == nil {
		return
	}
	if d.hash != nil {
		_ = d.hash.Close()
	}
	if d.root != nil {
		_ = d.root.Close()
	}
	if d.parent != nil {
		_ = d.parent.Close()
	}
}

func (d *cacheDirs) cleanupNew() error {
	if d == nil {
		return nil
	}
	var firstErr error
	if d.hashCreated {
		if err := removeEmptyDirAt(d.root, d.hashName); err != nil && !errors.Is(err, os.ErrNotExist) {
			firstErr = err
		}
	}
	if d.rootCreated {
		if err := removeEmptyDirAt(d.parent, d.rootName); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (d *cacheDirs) openEntry(name string) (*os.File, error) {
	return openDirAt(d.hash, name)
}

func (d *cacheDirs) makeTemp(prefix string) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		name := prefix + hex.EncodeToString(random[:])
		err := unix.Mkdirat(int(d.hash.Fd()), name, 0o755)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", pathError("mkdirat", name, err)
		}
		return name, nil
	}
	return "", errors.New("could not create unique temporary cache entry")
}

func (d *cacheDirs) removeTemp(name string) error {
	return removeTreeAt(d.hash, name)
}

func removeEmptyDirAt(parent *os.File, name string) error {
	child, err := openDirAt(parent, name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	entries, readErr := child.Readdirnames(-1)
	_ = child.Close()
	if readErr != nil {
		return readErr
	}
	if len(entries) > 0 {
		return nil
	}

	if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.ENOTEMPTY) {
		return pathError("unlinkat", name, err)
	}
	return nil
}

func (d *cacheDirs) publish(tempName, entryName string) error {
	if err := unix.Renameat(int(d.hash.Fd()), tempName, int(d.hash.Fd()), entryName); err != nil {
		return pathError("renameat", entryName, err)
	}
	return nil
}

func openCacheParent(cacheRoot string, create bool) (*os.File, error) {
	parentPath := filepath.Dir(cacheRoot)
	home, err := openDir(filepath.Dir(parentPath))
	if err != nil {
		return nil, err
	}
	parentName := filepath.Base(parentPath)
	if !create {
		parent, err := openDirAt(home, parentName)
		_ = home.Close()
		return parent, err
	}
	parent, _, err := openOrCreateDirAt(home, parentName)
	_ = home.Close()
	return parent, err
}

func openDir(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, pathError("open", path, err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openDirAt(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, pathError("openat", name, err)
	}
	return os.NewFile(uintptr(fd), filepath.Join(parent.Name(), name)), nil
}

func openOrCreateDirAt(parent *os.File, name string) (*os.File, bool, error) {
	if dir, err := openDirAt(parent, name); err == nil {
		return dir, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	created := false
	if err := unix.Mkdirat(int(parent.Fd()), name, 0o755); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return nil, false, pathError("mkdirat", name, err)
		}
	} else {
		created = true
	}
	dir, err := openDirAt(parent, name)
	if err != nil {
		return nil, false, err
	}
	return dir, created, nil
}

func removeTreeAt(parent *os.File, name string) error {
	child, err := openDirAt(parent, name)
	if err != nil {
		if errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
			if unlinkErr := unix.Unlinkat(int(parent.Fd()), name, 0); unlinkErr != nil && !errors.Is(unlinkErr, unix.ENOENT) {
				return pathError("unlinkat", name, unlinkErr)
			}
			return nil
		}
		return err
	}

	entries, readErr := child.Readdirnames(-1)
	if readErr != nil {
		_ = child.Close()
		return readErr
	}
	for _, entry := range entries {
		if err := removeTreeAt(child, entry); err != nil {
			_ = child.Close()
			return err
		}
	}
	_ = child.Close()
	if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
		return pathError("unlinkat", name, err)
	}
	return nil
}

func pathError(op, path string, err error) error {
	return &os.PathError{Op: op, Path: path, Err: fmt.Errorf("%w", err)}
}
