package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	startupfixture "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/startup"
)

// spec: session-startup/provisional-layout-matches-verified-inventory
func TestGuiHostProvisionalLayoutMatchesVerifiedInventory(t *testing.T) {
	stages := work50HostRun(t, false, func(f *startupfixture.Fixture) []byte {
		return f.OrganizationConfig(false)
	}, func(f *startupfixture.Fixture, _ string) string {
		writeFile(t, config.UserPersonal.LockPath(filepath.Join(os.Getenv("HOME"), ".agents", "config.yaml")), string(f.UserCollectionLock(t)))
		return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  mp: {marketplace: %q}\nresources:\n  mp:\n    include:\n      - tools/collection/bundle: {revision: %q}\n    exclude: [tools/skill/review]\n", f.Server.URL, f.CollectionDigest())
	}, func(app *App) []work50HostEvent {
		return readWork50HostStages(t, app)
	})
	if len(stages) != 4 {
		t.Fatal("layout fixture did not deliver four startup stages")
	}
	generation := stages[0].Generation
	for i, stage := range stages {
		if stage.Type != "snapshot" || stage.Generation != generation || stage.Payload.Stage != []string{"provisional", "config", "inventory", "final"}[i] {
			t.Fatal("layout fixture stage order or generation changed")
		}
	}
	want := work50HostGroupedLayout(stages[0].Payload.Rows, stages[0].Payload.Groups)
	for i := 1; i < 3; i++ {
		if !reflect.DeepEqual(work50HostGroupedLayout(stages[i].Payload.Rows, stages[i].Payload.Groups), want) {
			t.Fatalf("provisional rendered layout differs from stage %d", i)
		}
		if !reflect.DeepEqual(stages[i].Payload.Groups, stages[0].Payload.Groups) {
			t.Fatalf("provisional group metadata differs from stage %d", i)
		}
	}
	if stages[2].Payload.Drift != "pending" {
		t.Fatal("layout fixture inventory did not keep pending drift")
	}
	for _, diagnostic := range stages[2].Payload.Diagnostics {
		if diagnostic.Severity == "error" {
			t.Fatal("layout fixture inventory published an error diagnostic")
		}
	}
	for _, stage := range stages {
		audit, gamma, review := work52RowNamed(t, stage.Payload.Rows, "audit"), work52RowNamed(t, stage.Payload.Rows, "gamma"), work52RowNamed(t, stage.Payload.Rows, "review")
		if !audit.Checked || !gamma.Checked || review.Checked || !strings.Contains(review.Status, "excluded") {
			t.Fatal("layout fixture selection or exclusion badge changed")
		}
	}
}
