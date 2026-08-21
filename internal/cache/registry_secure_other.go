//go:build !darwin && !linux

package cache

import (
	"os"
	"path/filepath"
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
	parent, err := os.Open(filepath.Dir(cacheRoot))
	if err != nil {
		return nil, err
	}
	root, err := os.Open(filepath.Join(filepath.Dir(cacheRoot), filepath.Base(cacheRoot)))
	if err != nil {
		parent.Close()
		return nil, err
	}
	hash, err := os.Open(filepath.Join(cacheRoot, hashName))
	if err != nil {
		root.Close()
		parent.Close()
		return nil, err
	}
	return &cacheDirs{parent: parent, root: root, hash: hash, rootName: filepath.Base(cacheRoot), hashName: hashName}, nil
}

func createCacheDirs(cacheRoot, hashName string) (*cacheDirs, error) {
	parentPath := filepath.Dir(cacheRoot)
	if err := os.MkdirAll(parentPath, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return nil, err
	}
	hashPath := filepath.Join(cacheRoot, hashName)
	if err := os.MkdirAll(hashPath, 0o755); err != nil {
		return nil, err
	}
	return openCacheDirs(cacheRoot, hashName)
}

func (d *cacheDirs) close() {
	if d == nil {
		return
	}
	d.hash.Close()
	d.root.Close()
	d.parent.Close()
}

func (d *cacheDirs) cleanupNew() error { return nil }

func (d *cacheDirs) openEntry(name string) (*os.File, error) {
	return os.Open(filepath.Join(filepath.Dir(d.hash.Name()), name))
}

func (d *cacheDirs) makeTemp(prefix string) (string, error) {
	temp, err := os.MkdirTemp(d.hash.Name(), prefix)
	if err != nil {
		return "", err
	}
	return filepath.Base(temp), nil
}

func (d *cacheDirs) removeTemp(name string) error {
	return os.RemoveAll(filepath.Join(d.hash.Name(), name))
}

func (d *cacheDirs) publish(tempName, entryName string) error {
	return os.Rename(filepath.Join(d.hash.Name(), tempName), filepath.Join(d.hash.Name(), entryName))
}
