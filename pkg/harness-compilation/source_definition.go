package harnesscompile

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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

func IngestBundleWithLoader(srcFS fs.FS, load func(fs.FS) (*ingest.Bundle, error)) (*ingest.Bundle, error) {
	observed := newSourceDigestFS(srcFS)
	bundle, err := load(observed)
	if err != nil {
		return nil, err
	}
	for _, resource := range bundle.Resources {
		if hook, ok := resource.(*ir.Hook); ok {
			hook.SetSourceDefinitionDigest(observed.digestFor(hook.Source.File))
		}
	}
	return bundle, nil
}
