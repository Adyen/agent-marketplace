package windsurf

import (
	"fmt"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
)

type Ingester struct{}

type LoadOptions struct {
	User                fs.FS
	LegacyHookInventory bool
}

func init() {
	ingest.Register(Ingester{})
}

func (Ingester) Source() ingest.Harness { return ingest.HarnessWindsurf }

func (Ingester) Detect(src fs.FS) bool {
	return hasWorkspaceLayout(src, ".windsurf") || hasWorkspaceLayout(src, ".")
}

func (Ingester) LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	return (Ingester{}).LoadBundleWithOptions(src, LoadOptions{})
}

func (Ingester) LoadBundleWithOptions(src fs.FS, opts LoadOptions) (*ingest.Bundle, error) {
	if hasWorkspaceLayout(src, ".windsurf") {
		return loadWorkspace(src, opts, ".windsurf")
	}
	if hasWorkspaceLayout(src, ".") {
		return loadWorkspace(src, opts, ".")
	}
	return nil, fmt.Errorf("ingest/windsurf: no recognizable Windsurf layout found")
}
