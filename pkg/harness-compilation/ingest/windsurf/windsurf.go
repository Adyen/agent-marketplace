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

// devinHooksFile is the Devin Desktop workspace hook file. Windsurf ingests it
// only when the .devin directory carries no Devin CLI markers — the same fixed
// list the Devin ingester uses — so a real Devin workspace is never claimed.
const devinHooksFile = ".devin/hooks.json"

var devinMarkerFiles = []string{".devin/hooks.v1.json", ".devin/mcp_config.json", ".devin/config.json"}

var devinMarkerDirs = []string{".devin/skills", ".devin/agents"}

func devinHooksClaimable(src fs.FS) bool {
	if !fileExists(src, devinHooksFile) {
		return false
	}
	for _, marker := range devinMarkerFiles {
		if fileExists(src, marker) {
			return false
		}
	}
	for _, dir := range devinMarkerDirs {
		if dirHasEntries(src, dir) {
			return false
		}
	}
	return true
}

func (Ingester) Detect(src fs.FS) bool {
	return hasWorkspaceLayout(src, ".windsurf") || hasWorkspaceLayout(src, ".") || devinHooksClaimable(src)
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
	if devinHooksClaimable(src) {
		return loadWorkspace(src, opts, ".windsurf")
	}
	return nil, fmt.Errorf("ingest/windsurf: no recognizable Windsurf layout found")
}
