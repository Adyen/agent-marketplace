package harnesscompile

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
)

type sourceDigestFS struct {
	fs.FS
	digests  map[string]string
	unstable map[string]bool
}

func newSourceDigestFS(src fs.FS) *sourceDigestFS {
	return &sourceDigestFS{
		FS:       src,
		digests:  make(map[string]string),
		unstable: make(map[string]bool),
	}
}

func (w *sourceDigestFS) ReadFile(name string) ([]byte, error) {
	data, err := fs.ReadFile(w.FS, name)
	if err != nil {
		return data, err
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if prev, ok := w.digests[name]; ok && prev != digest {
		w.unstable[name] = true
	}
	w.digests[name] = digest
	return data, nil
}

func (w *sourceDigestFS) digestFor(name string) string {
	if name == "" || w.unstable[name] {
		return ""
	}
	return w.digests[name]
}
