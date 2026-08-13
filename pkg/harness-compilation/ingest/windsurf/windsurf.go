package windsurf

import (
	"fmt"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
)

type Ingester struct{}

type LoadOptions struct {
	User fs.FS
}

func init() {
	ingest.Register(Ingester{})
}

func (Ingester) Source() ingest.Harness { return ingest.HarnessWindsurf }

func (Ingester) Detect(src fs.FS) bool {
	return hasWorkspaceLayout(src, ".windsurf") || hasWorkspaceLayout(src, ".")
}

func (Ingester) LoadBundle(src fs.FS) (*ingest.Bundle, error) {
	if hasWorkspaceLayout(src, ".windsurf") {
		return loadWorkspace(src, LoadOptions{}, ".windsurf")
	}
	if hasWorkspaceLayout(src, ".") {
		return loadWorkspace(src, LoadOptions{}, ".")
	}
	return nil, fmt.Errorf("ingest/windsurf: no recognizable Windsurf layout found")
}
