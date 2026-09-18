package resolver

import (
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func materializationArtifact(name, body string, kind ir.ResourceKind, source config.Source, targets []hcc.Target) ResolvedArtifact {
	var resource ir.Resource
	switch kind {
	case ir.KindSkill:
		resource = ir.NewSkill(name, "test", body, nil, nil, ir.SourceRef{})
	case ir.KindHook:
		resource = ir.NewHook(name, "SessionStart", body, "command", nil, ir.SourceRef{})
	case ir.KindMcpServer:
		resource = ir.NewMcpServer(name, body, nil, nil, nil, ir.SourceRef{})
	}
	return ResolvedArtifact{Entry: catalog.Entry{Kind: kind, Name: name, Resource: resource}, RegistryURL: "https://example.invalid/" + body, Source: source, Harnesses: targets}
}

func TestMaterializationWinnersPreservesInputAndPartialTargets(t *testing.T) {
	firstTargets := []hcc.Target{hcc.TargetFactory, hcc.TargetClaude}
	secondTargets := []hcc.Target{hcc.TargetFactory, hcc.TargetGemini}
	first := materializationArtifact("shared", "first", ir.KindSkill, config.RepoCommitted, firstTargets)
	first.FilePrecedence = 1
	second := materializationArtifact("shared", "second", ir.KindSkill, config.RepoCommitted, secondTargets)
	second.FilePrecedence = 2
	beforeFirst := append([]hcc.Target(nil), firstTargets...)
	beforeSecond := append([]hcc.Target(nil), secondTargets...)
	out := MaterializationWinners([]ResolvedArtifact{first, second})
	if !reflect.DeepEqual(firstTargets, beforeFirst) || !reflect.DeepEqual(secondTargets, beforeSecond) {
		t.Fatalf("input targets changed: %v / %v", firstTargets, secondTargets)
	}
	if len(out) != 2 || !reflect.DeepEqual(out[0].Harnesses, []hcc.Target{hcc.TargetClaude}) || !reflect.DeepEqual(out[1].Harnesses, []hcc.Target{hcc.TargetFactory, hcc.TargetGemini}) {
		t.Fatalf("partial target projection = %#v", out)
	}
}

func TestMaterializationWinnersLegacyEmptyPriorityAndOrdering(t *testing.T) {
	empty := materializationArtifact("empty", "empty", ir.KindSkill, config.RepoCommitted, []hcc.Target{})
	legacy := materializationArtifact("legacy", "legacy", ir.KindSkill, config.RepoCommitted, nil)
	lower := materializationArtifact("shared", "lower", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	higher := materializationArtifact("shared", "higher", ir.KindSkill, config.Organization, []hcc.Target{hcc.TargetFactory})
	higher.Required = true
	tiedFirst := materializationArtifact("tied", "first", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	tiedSecond := materializationArtifact("tied", "second", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	tiedSecond.RegistryURL = tiedFirst.RegistryURL
	out := MaterializationWinners([]ResolvedArtifact{empty, legacy, lower, higher, tiedFirst, tiedSecond})
	if len(out) != 3 || out[0].Name != "legacy" || !reflect.DeepEqual(out[0].Harnesses, []hcc.Target{hcc.TargetClaude}) || out[1].Resource.Name() != "shared" || out[1].RegistryURL != higher.RegistryURL || out[2].RegistryURL != tiedFirst.RegistryURL {
		t.Fatalf("projection = %#v", out)
	}
}

func TestMaterializationWinnersNilBlocksFallbackAndUnresolvedDoesNot(t *testing.T) {
	nilWinner := ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "blocked"}, RegistryURL: "https://example.invalid/high", Source: config.Organization, Required: true, Harnesses: []hcc.Target{hcc.TargetFactory}}
	fallback := materializationArtifact("blocked", "fallback", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	metadata := materializationArtifact("visible", "metadata", ir.KindSkill, config.Organization, []hcc.Target{hcc.TargetFactory})
	metadata.Unresolved = true
	valid := materializationArtifact("visible", "valid", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	out := MaterializationWinners([]ResolvedArtifact{fallback, nilWinner, metadata, valid})
	if len(out) != 2 || out[0].Resource != nil || out[0].Name != "blocked" || out[1].Resource == nil || out[1].Name != "visible" {
		t.Fatalf("nil/unresolved projection = %#v", out)
	}
}

func TestMaterializationWinnersPreservesScopesAndKinds(t *testing.T) {
	machine := materializationArtifact("shared", "machine", ir.KindSkill, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	project := materializationArtifact("shared", "project", ir.KindSkill, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	otherKind := materializationArtifact("shared", "mcp", ir.KindMcpServer, config.RepoCommitted, []hcc.Target{hcc.TargetFactory})
	out := MaterializationWinners([]ResolvedArtifact{machine, project, otherKind})
	if len(out) != 3 {
		t.Fatalf("scope/kind controls projected to %d", len(out))
	}
}

func TestMaterializationWinnersPreservesAdditiveHooks(t *testing.T) {
	first := materializationArtifact("startup", "first", ir.KindHook, config.UserPersonal, []hcc.Target{hcc.TargetFactory})
	second := materializationArtifact("startup", "second", ir.KindHook, config.Organization, []hcc.Target{hcc.TargetFactory})
	out := MaterializationWinners([]ResolvedArtifact{first, second})
	if len(out) != 2 {
		t.Fatalf("additive hooks projected to %d", len(out))
	}
}
