package materialize

import (
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

const codexHooksFile = "hooks.json"

// splitCodexOwnedJSONArtifacts keeps ownership-reconciled JSON out of the
// generic marker merge path. TOML reconciliation is handled separately.
func splitCodexOwnedJSONArtifacts(artifacts []artifact.Artifact, resource string) ([]artifact.Artifact, []devinEntry, error) {
	var rest []artifact.Artifact
	var entries []devinEntry
	for _, candidate := range artifacts {
		if candidate.Path != ".codex/hooks.json" || !candidate.ExternalReconciliationRequired {
			rest = append(rest, candidate)
			continue
		}
		parsed, err := devinFragmentEntries(codexHooksFile, candidate.Bytes, resource)
		if err != nil {
			return nil, nil, fmt.Errorf("decode Codex hooks fragment: %w", err)
		}
		entries = append(entries, parsed...)
	}
	return rest, entries, nil
}
