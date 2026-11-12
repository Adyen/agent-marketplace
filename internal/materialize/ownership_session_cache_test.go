package materialize

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func runDevinAggregateFixture(t *testing.T, repo string) {
	t.Helper()
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{
		devinResolved(t, "server", ir.NewMcpServer("server", "server-bin", nil, nil, nil, ir.SourceRef{}), false),
		additiveSafetyProvider(t, hcc.TargetDevin, false, "echo hook", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, report.Diags)
}

func TestDevinAggregateSessionLoadsOwnershipOnce(t *testing.T) {
	repo, _ := devinEnv(t)
	runDevinAggregateFixture(t, repo)
	root := devinProjectRoot(repo)
	loads := 0
	store, err := ownership.Open(root, ownership.WithStateLoadObserver(func() { loads++ }))
	if err != nil {
		t.Fatal(err)
	}
	mcpFile, err := devinOwnedJSONFile(harness.DevinMcpConfigFile, false)
	if err != nil {
		t.Fatal(err)
	}
	hookFile, err := devinOwnedJSONFile(harness.DevinProjectHooksFile, false)
	if err != nil {
		t.Fatal(err)
	}
	options := devinOwnedJSONOptions()
	options.store = store
	report := reconcileOwnedJSONAggregates(context.Background(), &Report{PerTarget: map[hcc.Target]TargetReport{}},
		outputKey{target: hcc.TargetDevin}, root, []ownedJSONFileSpec{mcpFile, hookFile},
		&ownedJSONAggregateWork{}, options, false)
	requireNoErrorDiags(t, report.Diags)
	if len(report.PerRoot) == 0 {
		t.Fatal("reconcile wrote no aggregate files")
	}
	if loads != 1 {
		t.Fatalf("state loads = %d, want 1 across two aggregate files", loads)
	}
}

func TestMaterializeRecoversOncePerHeldRootSession(t *testing.T) {
	recoveries := make(map[string]int)
	ownedAggregateRecoverHook = func(rootKey string) { recoveries[rootKey]++ }
	t.Cleanup(func() { ownedAggregateRecoverHook = nil })

	devinRepo, _ := devinEnv(t)
	runDevinAggregateFixture(t, devinRepo)
	if len(recoveries) != 1 {
		t.Fatalf("Devin Recover root keys = %v, want one", recoveries)
	}

	geminiRepo, _, _ := nativeHookJSONTestEnvironment(t)
	geminiReport, err := Run(context.Background(), geminiRepo, []resolver.ResolvedArtifact{
		additiveSafetyProvider(t, hcc.TargetGemini, false, "echo hook", ""),
		geminiMCPProvider(t, "server", "server-bin"),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoErrorDiags(t, geminiReport.Diags)
	if len(recoveries) != 2 {
		t.Fatalf("Recover root keys = %v, want Devin and shared Gemini roots", recoveries)
	}
	for rootKey, count := range recoveries {
		if count != 1 {
			t.Fatalf("Recover calls for root %s = %d, want 1", rootKey, count)
		}
	}
}
