package materialize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func TestDevinOwnedLegacyHooksUpgradeWithoutChangingHandwrittenGroups(t *testing.T) {
	for _, machine := range []bool{false, true} {
		name := "project"
		if machine {
			name = "user"
		}
		t.Run(name, func(t *testing.T) {
			repo, home := devinEnv(t)
			root, rel := devinProjectRoot(repo), harness.DevinProjectHooksFile
			legacy := `{"PostToolUse":[{"command":"echo managed","type":"command"}]}`
			if machine {
				root, rel = devinUserRoot(home), harness.DevinUserConfigFile
				legacy = `{"hooks":` + legacy + `}`
			}
			entries, err := devinFragmentEntries(rel, []byte(legacy), "managed-hook")
			if err != nil {
				t.Fatal(err)
			}
			work := &ownedJSONAggregateWork{}
			for _, entry := range entries {
				work.add(entry)
			}
			key := outputKey{target: hcc.TargetDevin, machineScope: machine}
			seed := reconcileDevinAggregates(context.Background(), &Report{PerTarget: map[hcc.Target]TargetReport{}}, key, root, work, false)
			requireNoErrorDiags(t, seed.Diags)
			const handwritten = `{ "hooks": [{ "type": "command", "command": "echo handwritten" }] }`
			mixed := "{\n  // handwritten group stays first\n  \"PostToolUse\": [\n    " + handwritten + ",\n    {\"command\":\"echo managed\",\"type\":\"command\"}\n  ]\n}\n"
			if machine {
				mixed = "{\n  \"auto_update\": false,\n  \"hooks\": " + mixed + "}\n"
			}
			path := filepath.Join(root, rel)
			if err := os.WriteFile(path, []byte(mixed), 0o600); err != nil {
				t.Fatal(err)
			}
			selection := []resolver.ResolvedArtifact{devinResolved("managed-hook", ir.NewHook("managed-hook", "PostToolUse", "echo managed", "command", nil, ir.SourceRef{}), machine)}
			report, err := Run(context.Background(), repo, selection)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, report.Diags)
			got := readFileString(t, path)
			if !strings.Contains(got, handwritten) || !strings.Contains(got, "// handwritten group stays first") || strings.Index(got, "echo handwritten") >= strings.Index(got, "echo managed") {
				t.Fatalf("legacy upgrade changed handwritten bytes or order: %s", got)
			}
			var document map[string]json.RawMessage
			if err := jsonc.Unmarshal(rel, []byte(got), &document); err != nil {
				t.Fatal(err)
			}
			if machine {
				if string(document["auto_update"]) != "false" {
					t.Fatal("unrelated user configuration changed")
				}
				if err := json.Unmarshal(document["hooks"], &document); err != nil {
					t.Fatal(err)
				}
			}
			var groups []map[string]json.RawMessage
			if err := json.Unmarshal(document["PostToolUse"], &groups); err != nil || len(groups) != 2 {
				t.Fatalf("expected handwritten and upgraded managed groups: %v: %s", err, got)
			}
			for _, group := range groups {
				var commands []struct{ Type, Command string }
				if err := json.Unmarshal(group["hooks"], &commands); err != nil || len(commands) != 1 || commands[0].Type != "command" || commands[0].Command == "" {
					t.Fatalf("a legacy flat hook survived the owned upgrade: %s", got)
				}
			}
			second, err := Run(context.Background(), repo, selection)
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, second.Diags)
			if again := readFileString(t, path); again != got {
				t.Fatal("upgraded native hook groups are not idempotent")
			}
		})
	}
}
