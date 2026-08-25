package materialize

import (
	"fmt"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func BenchmarkCodexDestinationOverlapPreflight(b *testing.B) {
	repoKey := outputKey{target: hcc.Target(targetmeta.TargetCodex)}
	machineKey := outputKey{target: hcc.Target(targetmeta.TargetCodex), machineScope: true}
	repoArtifacts := make([]artifact.Artifact, 100)
	machineArtifacts := make([]artifact.Artifact, 100)
	for i := range repoArtifacts {
		repoArtifacts[i] = artifact.Artifact{Path: fmt.Sprintf(".codex/agents/repo-%03d.toml", i), Bytes: []byte("repo")}
		machineArtifacts[i] = artifact.Artifact{Path: fmt.Sprintf(".codex/agents/machine-%03d.toml", i), Bytes: []byte("machine")}
	}
	repoRoot := b.TempDir()
	b.Setenv("HOME", repoRoot)
	b.Setenv("CODEX_HOME", "")
	modes := config.DefaultMaterializationModes()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result := compilePhaseResult{
			byTarget: map[outputKey]*targetWork{
				repoKey:    {artifacts: append([]artifact.Artifact(nil), repoArtifacts...)},
				machineKey: {artifacts: append([]artifact.Artifact(nil), machineArtifacts...)},
			},
			skillLinks:      make(map[outputKey]map[string]*skillLinkGroup),
			allOutputs:      map[outputKey]bool{repoKey: true, machineKey: true},
			blocked:         make(blockedOutputs),
			artifactMeta:    make(map[outputKey]map[string]skillLink),
			codexAggregates: make(map[outputKey]*ownedJSONAggregateWork),
			codexMCP:        make(map[outputKey][]codexMCPEntry),
			codexMCPBlocked: make(map[outputKey]bool),
		}
		if _, d := preflightCodexDestinations(&result, repoRoot, modes); d != nil {
			b.Fatal(d.Message)
		}
	}
}
