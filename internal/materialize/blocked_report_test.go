package materialize

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestReportIdentifiesBlockedResource(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "repository", true: "machine"}[machine], func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			first := additiveSafetyProvider(t, hcc.TargetFactory, machine, "echo first", "first")
			second := additiveSafetyProvider(t, hcc.TargetFactory, machine, "echo second", "second")
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{first, second}, WithDryRun())
			if err != nil {
				t.Fatal(err)
			}
			if !diag.HasCode(report.Diags, diag.CodeMaterializeHookSupportConflict) {
				t.Fatalf("missing conflict: %v", report.Diags)
			}
			want := BlockedResource{Kind: ir.KindHook, Name: "PreToolUse-command", Target: hcc.TargetFactory, MachineScope: machine}
			found := false
			for _, got := range report.BlockedResources {
				if got == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("blocked resources = %+v, want %+v", report.BlockedResources, want)
			}
		})
	}
}
