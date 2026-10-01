package materialize

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type repairPolicyCrasher func(ownership.CrashPoint) error

// Crash delegates an ownership crash point to the injected callback.
func (c repairPolicyCrasher) Crash(point ownership.CrashPoint) error { return c(point) }

// TestRepairRefusesPolicyChangeAfterStaging verifies that adoption,
// replacement, and removal reject a policy edit after staging, preserve aggregate
// and ownership bytes, and clean temporary state before a successful retry.
// spec: owned-entry-reconciliation/policy-changes-after-repair-staging
func TestRepairRefusesPolicyChangeAfterStaging(t *testing.T) {
	for _, target := range []hcc.Target{hcc.TargetDevin, hcc.TargetCodex} {
		for _, action := range []string{"adopt", "replace", "remove"} {
			t.Run(string(target)+"/"+action, func(t *testing.T) {
				repoRoot, home := devinEnv(t)
				t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
				selection := devinMcpSelection("managed")
				selection[0].Harnesses = []hcc.Target{target}
				outDir := filepath.Join(repoRoot, "."+string(target))
				rel := "mcp_config.json"
				original := "{\"mcpServers\":{\"managed\":{\"command\":\"handwritten\"}}}\n"
				if target == hcc.TargetCodex {
					rel = "config.toml"
					original = "[mcp_servers.managed]\ncommand = 'handwritten'\n"
				}
				aggregate := filepath.Join(outDir, rel)
				if action == "replace" {
					if err := os.MkdirAll(outDir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(aggregate, []byte(original), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := Run(context.Background(), repoRoot, selection); err != nil {
					t.Fatal(err)
				}
				if action != "replace" {
					dropOwnershipState(t, outDir)
				}
				if action == "remove" {
					selection = nil
				}
				policy := filepath.Join(repoRoot, "policy.yaml")
				if err := os.WriteFile(policy, []byte("original policy\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				canonical, err := filepath.EvalSymlinks(policy)
				if err != nil {
					t.Fatal(err)
				}
				generation, err := dependencytxn.ReadGeneration(canonical)
				if err != nil {
					t.Fatal(err)
				}
				validate := func() error {
					return dependencytxn.ValidateReadAssertions([]dependencytxn.ReadAssertion{{Path: canonical, RequestedPath: policy, Expected: generation}})
				}
				previews, err := RepairPreviews(context.Background(), repoRoot, selection, validate)
				if err != nil {
					t.Fatal(err)
				}
				var file *RepairFile
				for _, preview := range previews {
					if preview.Target == target {
						file = repairFileFor(t, preview.Preview, rel)
					}
				}
				if file == nil {
					t.Fatal("repair did not preview the selected target")
				}
				changed := false
				file.store, err = ownership.Open(outDir, ownership.WithCrasher(repairPolicyCrasher(func(point ownership.CrashPoint) error {
					if point == ownership.CrashAfterStagedWrite && !changed {
						changed = true
						return os.WriteFile(policy, []byte("changed policy\n"), 0o600)
					}
					return nil
				})))
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(aggregate)
				if err != nil {
					t.Fatal(err)
				}
				record, recordErr := os.ReadFile(file.store.StateFile())
				if recordErr != nil && !os.IsNotExist(recordErr) {
					t.Fatal(recordErr)
				}
				backups, err := file.store.SafetyBackups(rel)
				if err != nil {
					t.Fatal(err)
				}
				apply := func() (ownership.Result, error) {
					switch action {
					case "adopt":
						return file.Adopt([]int{0})
					case "replace":
						return file.ReplaceConflict(0, true)
					default:
						return file.RemoveOrphan(0, true)
					}
				}
				if _, err := apply(); !errors.Is(err, dependencytxn.ErrGenerationConflict) || !changed {
					t.Fatalf("repair error = %v, policy changed = %t", err, changed)
				}
				if after, err := os.ReadFile(aggregate); err != nil || !bytes.Equal(before, after) {
					t.Fatalf("refused repair changed the aggregate: %v", err)
				}
				afterRecord, afterErr := os.ReadFile(file.store.StateFile())
				if afterErr != nil && !os.IsNotExist(afterErr) || os.IsNotExist(recordErr) != os.IsNotExist(afterErr) || !bytes.Equal(record, afterRecord) {
					t.Fatalf("refused repair changed ownership state: %v", afterErr)
				}
				if journal, err := file.store.ReadJournal(); err != nil || journal != nil {
					t.Fatalf("refused repair retained a journal: %v", err)
				}
				if staged, err := filepath.Glob(aggregate + ".am-staged-*"); err != nil || len(staged) != 0 {
					t.Fatalf("refused repair retained staged files: %v %v", staged, err)
				}
				if after, err := file.store.SafetyBackups(rel); err != nil || !reflect.DeepEqual(backups, after) {
					t.Fatalf("refused repair changed retained backups: %v", err)
				}
				if err := os.WriteFile(policy, []byte("original policy\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if result, err := apply(); err != nil || result.Generation == 0 || result.BackupPath == "" {
					t.Fatalf("unchanged policy did not authorize the repair: %+v %v", result, err)
				}
			})
		}
	}
}
