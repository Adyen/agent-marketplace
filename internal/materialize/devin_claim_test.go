package materialize

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// devinClaimLayout builds a single-partition layout with the given aggregates
// so claim resolution can be exercised on shapes the registered Devin
// descriptor does not declare.
func devinClaimLayout(aggregates ...targetmeta.NativeAggregate) targetmeta.NativeLayout {
	return targetmeta.NativeLayout{Partitions: []targetmeta.NativePartition{{
		ID:         "default",
		Prefix:     ".",
		Aggregates: aggregates,
	}}}
}

// TestDevinAggregateClaimSharedEnvelope pins the loud-fail contract: the
// validator permits one path claimed by several kinds under different
// members, but the owned-JSON reconciler patches one container per file, so
// claim resolution must error rather than decode every kind under the first
// claim's member and silently drop the rest.
func TestDevinAggregateClaimSharedEnvelope(t *testing.T) {
	envelope := targetmeta.NativeAggregateFile{Path: "config.json", Member: "hooks"}
	envelopeOther := targetmeta.NativeAggregateFile{Path: "config.json", Member: "rules"}
	ambiguous := devinClaimLayout(
		targetmeta.NativeAggregate{Kind: ir.KindHook, Project: envelope, User: envelope},
		targetmeta.NativeAggregate{Kind: ir.KindRule, Project: envelopeOther, User: envelopeOther},
	)
	if _, _, err := devinAggregateClaimIn(ambiguous, "config.json", false); err == nil {
		t.Fatal("a shared envelope resolved to a single claim; the reconcile would drop every member but the first")
	}
	if _, _, err := devinAggregateClaimIn(ambiguous, "config.json", true); err == nil {
		t.Fatal("a shared envelope resolved to a single claim at user scope")
	}

	// A kind claiming the top level of a file another kind claims under a
	// member is the same ambiguity: one container cannot carry both.
	mixed := devinClaimLayout(
		targetmeta.NativeAggregate{
			Kind:    ir.KindHook,
			Project: targetmeta.NativeAggregateFile{Path: "config.json"},
			User:    targetmeta.NativeAggregateFile{Path: "config.json"},
		},
		targetmeta.NativeAggregate{Kind: ir.KindRule, Project: envelopeOther, User: envelopeOther},
	)
	if _, _, err := devinAggregateClaimIn(mixed, "config.json", false); err == nil {
		t.Fatal("a bare-path plus member claim on one file resolved to a single claim")
	}
}

// TestDevinAggregateClaimResolution covers the non-ambiguous resolution the
// loud fail protects: a unique claim resolves, an unclaimed path reports
// false, and a scope-variant claim answers per scope.
func TestDevinAggregateClaimResolution(t *testing.T) {
	hookProject := targetmeta.NativeAggregateFile{Path: "hooks.v1.json"}
	hookUser := targetmeta.NativeAggregateFile{Path: "config.json", Member: "hooks"}
	mcp := targetmeta.NativeAggregateFile{Path: "mcp_config.json", Member: "mcpServers"}
	layout := devinClaimLayout(
		targetmeta.NativeAggregate{Kind: ir.KindHook, Project: hookProject, User: hookUser},
		targetmeta.NativeAggregate{Kind: ir.KindMcpServer, Project: mcp, User: mcp},
	)

	claim, claimed, err := devinAggregateClaimIn(layout, "mcp_config.json", false)
	if err != nil || !claimed || claim.kind != ir.KindMcpServer || claim.member != "mcpServers" {
		t.Fatalf("mcp_config.json claim = %#v, %v, %v", claim, claimed, err)
	}
	claim, claimed, err = devinAggregateClaimIn(layout, "hooks.v1.json", false)
	if err != nil || !claimed || claim.kind != ir.KindHook || claim.member != "" {
		t.Fatalf("hooks.v1.json claim = %#v, %v, %v", claim, claimed, err)
	}
	// The scope-variant hook answers from the user-scope file at user scope.
	claim, claimed, err = devinAggregateClaimIn(layout, "config.json", true)
	if err != nil || !claimed || claim.kind != ir.KindHook || claim.member != "hooks" {
		t.Fatalf("user-scope config.json claim = %#v, %v, %v", claim, claimed, err)
	}
	// The project-scope file is not claimed at user scope.
	if _, claimed, err := devinAggregateClaimIn(layout, "hooks.v1.json", true); err != nil || claimed {
		t.Fatalf("user-scope hooks.v1.json claim = %v, %v; want unclaimed", claimed, err)
	}
	if _, claimed, err := devinAggregateClaimIn(layout, "other.json", false); err != nil || claimed {
		t.Fatalf("unclaimed path = %v, %v; want unclaimed", claimed, err)
	}
}
