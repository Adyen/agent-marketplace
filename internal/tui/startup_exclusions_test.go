package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
	startupfixture "github.com/Adyen/agent-marketplace/internal/testutil/marketplace/startup"
	"github.com/charmbracelet/x/ansi"
)

func work50SoloViewLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "solo") {
			return line
		}
	}
	return ""
}

// spec: session-startup/excluded-only-configuration-publishes-no-provisional-rows
func TestTuiExcludedOnlyDirectorySelectionsHaveNoProvisionalRows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		excluded bool
	}{{name: "control"}, {name: "excluded", excluded: true}} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newWork45Fixture(t, true, nil)
			if tc.excluded {
				root, err := os.ReadFile(fixture.userRoot)
				if err != nil {
					t.Fatal("read private user fixture root")
				}
				const existing = "resources:\n  local: {all: true}\n"
				if strings.Count(string(root), existing) != 1 {
					t.Fatal("private user fixture root did not contain one exact all-selection")
				}
				updated := strings.Replace(string(root), existing, "resources:\n  local: {all: true, exclude: [skill/solo]}\n", 1)
				if err := os.WriteFile(fixture.userRoot, []byte(updated), 0o644); err != nil {
					t.Fatal("add same-layer private skill exclusion")
				}
			}
			before := captureWork45Paths(t, fixture.protected)
			m := newProvisionalStageModel()
			generation := m.beginRequest(requestSnapshot)
			coreStages := session.ReadUserSnapshotStages(context.Background(), fixture.home, catalogLoadCached, generation)
			stages := collectSnapshotStages(bridgeSnapshotStages(coreStages))
			if len(stages) != 4 {
				t.Fatal("user snapshot did not deliver the four startup stages")
			}
			wantStages := []snapshotStage{snapshotStageConfig, snapshotStageConfig, snapshotStageInventory, snapshotStageFinal}
			for i, stage := range stages {
				if stage.generation != generation || stage.stage != wantStages[i] {
					t.Fatal("user snapshot stage order or generation changed")
				}
				if i == 0 && (stage.provisionalCfg == nil || stage.mergedCfg != nil) {
					t.Fatal("first user config stage was not provisional")
				}
				if i == 1 && stage.mergedCfg == nil {
					t.Fatal("second user config stage was not validated config")
				}
				updated, _ := m.Update(stage)
				var ok bool
				m, ok = updated.(Model)
				if !ok {
					t.Fatal("TUI did not accept an actual user snapshot stage")
				}
				if i < 2 {
					if tc.excluded {
						view := m.View().Content
						if len(m.rows) != 0 || strings.Contains(view, "solo") {
							t.Fatal("same-layer exclusion appeared in provisional TUI rows")
						}
					} else {
						if len(m.rows) != 1 {
							t.Fatalf("control stage %d: row-count condition failed", i)
						}
						row := m.rows[0]
						if row.Artifact.Name != "solo" {
							t.Fatalf("control stage %d: fixture-name condition failed", i)
						}
						if !row.PendingVerification {
							t.Fatalf("control stage %d: pending-state condition failed", i)
						}
						if row.Glyph() != "[x]" {
							t.Fatalf("control stage %d: checked-glyph condition failed", i)
						}
						if !strings.Contains(row.StatusText(), "verification pending") {
							t.Fatalf("control stage %d: row-status condition failed", i)
						}
						view := m.View().Content
						if !strings.Contains(view, "solo") {
							t.Fatalf("control stage %d: view-fixture-name condition failed", i)
						}
						if !strings.Contains(ansi.Strip(view), "verifying resources...") {
							t.Fatalf("control stage %d: view-startup-progress condition failed", i)
						}
					}
				} else if tc.excluded {
					if len(m.rows) != 1 || m.rows[0].Artifact.Name != "solo" || m.rows[0].PendingVerification || m.rows[0].Glyph() != "[ ]" {
						t.Fatal("excluded Directory catalog row was not unchecked and non-pending")
					}
					line := work50SoloViewLine(m.View().Content)
					if line == "" || strings.Contains(line, "[x]") {
						t.Fatal("excluded Directory row was not rendered as unchecked")
					}
				}
			}
			if stages[3].driftAssessment != driftReady || len(stages[3].errs) != 0 {
				t.Fatal("valid user-only Directory snapshot did not finish ready")
			}
			assertWork45PathsUnchanged(t, fixture.protected, before)
		})
	}
}

type work50TUIVisiblePosition struct {
	name, group, scopeBadge, harnessBadge string
}

func work50TUIVisiblePositions(rows []row) []work50TUIVisiblePosition {
	positions := make([]work50TUIVisiblePosition, len(rows))
	for i, item := range rows {
		positions[i] = work50TUIVisiblePosition{item.Artifact.Name, item.StatusGroup().ID(), originBadge(item), harnessBadge(item)}
	}
	return positions
}

func work50TUIRows(t *testing.T, stage int, rows []row, required, excludeReview, pending, compareGroup bool) {
	t.Helper()
	if len(rows) != 2 {
		t.Fatal("mixed-pin TUI snapshot did not contain the two organization skills")
	}
	for i, name := range []string{"audit", "review"} {
		item := rows[i]
		if item.Artifact.Name != name || item.Artifact.Source != config.Organization {
			t.Fatal("mixed-pin TUI row did not match its organization skill coordinate")
		}
		if item.Artifact.Required != required {
			t.Fatal("mixed-pin TUI required state did not match the fixture")
		}
		if item.PendingVerification != pending {
			t.Fatal("mixed-pin TUI pending state did not match the stage")
		}
		wantGlyph := "[x]"
		if !pending && required {
			wantGlyph = "[R]"
		}
		if i == 1 && excludeReview && !required {
			wantGlyph = "[ ]"
		}
		if item.Glyph() != wantGlyph {
			t.Fatalf("mixed-pin TUI stage %d row %d: selection marker condition failed", stage, i)
		}
		if pending && !strings.Contains(item.StatusText(), "verification pending") {
			t.Fatal("mixed-pin TUI row omitted pending verification status")
		}
		if i == 1 && excludeReview && !required && (!item.IsAvailable || !item.IsExcluded || !item.ExcludedByUser || !strings.Contains(item.StatusText(), "excluded")) {
			t.Fatal("optional user exclusion was not retained on the organization row")
		}
		if compareGroup {
			wantGroup := "enabled"
			if required {
				wantGroup = "required"
			} else if i == 1 && excludeReview {
				wantGroup = "available"
			}
			if item.StatusGroup().ID() != wantGroup {
				t.Fatal("mixed-pin TUI row moved to an unexpected status group")
			}
		}
	}
}

func work50TUIStages(t *testing.T, required, excludeReview bool) ([][]row, []string) {
	t.Helper()
	fixture := newWork45Fixture(t, true, nil)
	market := startupfixture.New(t, marketplace.DefaultCache())
	if err := os.WriteFile(fixture.userRoot, market.UserConfig(excludeReview), 0o644); err != nil {
		t.Fatal("write private mixed-pin user config")
	}
	orgDir := t.TempDir()
	orgPath := filepath.Join(orgDir, "config.yaml")
	if err := os.WriteFile(orgPath, market.OrganizationConfig(required), 0o600); err != nil {
		t.Fatal("write private mixed-pin organization config")
	}
	config.SetTestOrganizationConfigPath(t, orgPath)
	orgLockPath := config.Organization.LockPath(orgPath)
	if err := os.WriteFile(orgLockPath, market.OrganizationLock(t), 0o600); err != nil {
		t.Fatal("write private mixed-pin organization lock")
	}
	protected := append([]string(nil), fixture.protected...)
	protected = append(protected, orgDir, orgPath, orgLockPath)
	before := captureWork45Paths(t, protected)
	model := newProvisionalStageModel()
	generation := model.beginRequest(requestSnapshot)
	core := session.ReadUserSnapshotStages(context.Background(), fixture.home, catalogLoadCached, generation)
	stages := collectSnapshotStages(bridgeSnapshotStages(core))
	if len(stages) != 4 {
		t.Fatal("mixed-pin user snapshot did not deliver four startup stages")
	}
	rows, views := make([][]row, len(stages)), make([]string, len(stages))
	wantStages := []snapshotStage{snapshotStageConfig, snapshotStageConfig, snapshotStageInventory, snapshotStageFinal}
	for i, stage := range stages {
		if stage.generation != generation || stage.stage != wantStages[i] {
			t.Fatal("mixed-pin user snapshot stage order or generation changed")
		}
		updated, _ := model.Update(stage)
		var ok bool
		model, ok = updated.(Model)
		if !ok {
			t.Fatal("TUI did not accept an actual mixed-pin user stage")
		}
		if i == 0 && excludeReview && !required {
			var review row
			for _, candidate := range model.rows {
				if candidate.Artifact.Name == "review" {
					review = candidate
					break
				}
			}
			if review.Artifact.Name == "" {
				t.Fatal("pending excluded organization row was absent from the actual TUI model")
			}
			guarded, _ := model.applyRowSpaceEligibility(review)
			guardedModel, ok := guarded.(Model)
			if !ok || guardedModel.pane == paneToggleModal {
				t.Fatal("pending excluded organization row opened the TUI toggle modal")
			}
			model = guardedModel
			assertWork45PathsUnchanged(t, protected, before)
		}
		rows[i], views[i] = slices.Clone(model.rows), ansi.Strip(model.View().Content)
	}
	market.Server.Close()
	assertWork45PathsUnchanged(t, protected, before)
	return rows, views
}

// spec: session-startup/excluded-configured-selections-render-unchecked-in-their-verified-position
func TestTuiExcludedOrganizationPinRetainsVerifiedPosition(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		excludeReview, required bool
	}{
		{name: "optional-visible"},
		{name: "optional-user-exclusion", excludeReview: true},
		{name: "required-user-exclusion-immunity", excludeReview: true, required: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, views := work50TUIStages(t, tc.required, tc.excludeReview)
			for i := 0; i < 3; i++ {
				work50TUIRows(t, i, rows[i], tc.required, tc.excludeReview, i < 2, true)
				if !strings.Contains(views[i], "audit") || !strings.Contains(views[i], "review") {
					t.Fatal("mixed-pin TUI stage did not render both organization skills")
				}
				if i > 0 && !slices.Equal(work50TUIVisiblePositions(rows[0]), work50TUIVisiblePositions(rows[i])) {
					t.Fatalf("mixed-pin TUI visible row metadata changed at stage %d", i)
				}
			}
			if !tc.required && !slices.Equal(statusLayout(rows[0]), statusLayout(rows[2])) {
				t.Fatal("optional mixed-pin inventory changed its verified TUI row positions")
			}
		})
	}
}
