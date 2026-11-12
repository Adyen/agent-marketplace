package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func enterMetadataDetail(t *testing.T, r row) Model {
	t.Helper()
	m := modelWithSelectedRow(t, r)
	m.width = 120
	m.height = 30
	m.enterDetailPane()
	if m.pane != paneDetail {
		t.Fatalf("row did not open the detail pane: pane %v", m.pane)
	}
	if m.bundle != nil {
		t.Fatal("recovery/metadata row opened a filesystem bundle")
	}
	return m
}

func TestUnavailableRecoveryDetailSpaceOpensToggleModal(t *testing.T) {
	r := mkRow("missing", withProject, withRegistry("https://example.com/reg.git", "reg"))
	r.UnavailableReason = session.UnavailableCatalogReason
	m := enterMetadataDetail(t, r)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(Model)
	if m.pane != paneToggleModal {
		t.Fatalf("unavailable detail Space did not open the toggle modal: pane %v", m.pane)
	}
	if m.modalSkill.Artifact.Name != r.Artifact.Name {
		t.Fatalf("modal captured %q, want %q", m.modalSkill.Artifact.Name, r.Artifact.Name)
	}
	if cmd != nil {
		t.Fatal("opening the toggle modal from detail must not emit a command")
	}
}

func TestMarketplaceDiscoveryDetailSpaceOpensScopeModal(t *testing.T) {
	for _, availability := range []mpapi.Availability{mpapi.AvailabilityAvailable, mpapi.AvailabilityOperatorExposed} {
		t.Run(string(availability), func(t *testing.T) {
			r := marketplaceDiscoveryRow("skill", "review", availability)
			m := enterMetadataDetail(t, r)

			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
			m = updated.(Model)
			if m.pane != paneToggleModal {
				t.Fatalf("%s detail Space did not open the scope modal: pane %v", availability, m.pane)
			}
			if m.modalSkill.Artifact.Name != r.Artifact.Name {
				t.Fatalf("modal captured %q, want %q", m.modalSkill.Artifact.Name, r.Artifact.Name)
			}
			if cmd != nil {
				t.Fatal("opening the scope modal from detail must not emit a command")
			}
		})
	}
}

func TestDetailSpaceEligibilityRestrictions(t *testing.T) {
	t.Run("config-invalid keeps editor guidance", func(t *testing.T) {
		r := marketplaceDiscoveryRow("skill", "review", mpapi.AvailabilityAvailable)
		m := enterMetadataDetail(t, r)
		m.configInvalid = true

		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneDetail {
			t.Fatalf("config-invalid detail Space moved the pane: %v", m.pane)
		}
		if cmd == nil {
			t.Fatal("config-invalid detail Space returned no editor guidance")
		}
		if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "edit config") {
			t.Fatalf("detail Space guidance = %#v, want an edit-config toast", cmd())
		}
	})

	t.Run("required recovery row keeps required restriction", func(t *testing.T) {
		r := mkRow("missing", withProject, withRequired)
		r.UnavailableReason = session.UnavailableCatalogReason
		m := enterMetadataDetail(t, r)

		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneDetail {
			t.Fatalf("required detail Space opened mutation UI: pane %v", m.pane)
		}
		if cmd == nil {
			t.Fatal("required detail Space returned no toast")
		}
		if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "required items cannot be toggled") {
			t.Fatalf("required detail Space = %#v, want the required toast", cmd())
		}
	})

	t.Run("collection-only recovery row keeps collection restriction", func(t *testing.T) {
		r := mkRow("missing", withGlobal)
		r.UnavailableReason = session.UnavailableCatalogReason
		r.Artifact.IsMarketplace = true
		r.MarketplaceCollectionOnly = true
		m := enterMetadataDetail(t, r)

		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneDetail {
			t.Fatalf("collection-only detail Space opened mutation UI: pane %v", m.pane)
		}
		if cmd == nil {
			t.Fatal("collection-only detail Space returned no toast")
		}
		if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "collection") {
			t.Fatalf("collection-only detail Space = %#v, want the collection toast", cmd())
		}
	})

	t.Run("syncing blocks the detail recovery action", func(t *testing.T) {
		r := mkRow("missing", withProject)
		r.UnavailableReason = session.UnavailableCatalogReason
		m := enterMetadataDetail(t, r)
		m.syncing = true

		updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneDetail {
			t.Fatalf("syncing detail Space opened mutation UI: pane %v", m.pane)
		}
		if cmd != nil {
			t.Fatal("syncing detail Space emitted a command")
		}
	})

	for _, availability := range []mpapi.Availability{mpapi.AvailabilityBlocked, mpapi.AvailabilityRevoked, "unknown"} {
		t.Run("non-installable discovery "+string(availability), func(t *testing.T) {
			r := marketplaceDiscoveryRow("skill", "review", availability)
			m := enterMetadataDetail(t, r)

			updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
			m = updated.(Model)
			if m.pane != paneDetail {
				t.Fatalf("%s detail Space opened mutation UI: pane %v", availability, m.pane)
			}
		})
	}

	t.Run("ordinary bundle detail Space stays viewport navigation", func(t *testing.T) {
		dir := t.TempDir()
		r := mkRow("verified", withProject)
		r.Artifact.Path = dir
		m := modelWithSelectedRow(t, r)
		m.width = 120
		m.height = 30
		m.enterDetailPane()
		if m.pane != paneDetail {
			t.Fatalf("ordinary row did not open the detail pane: pane %v", m.pane)
		}

		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneDetail {
			t.Fatalf("ordinary detail Space opened mutation UI: pane %v", m.pane)
		}
	})
}

func TestListSpaceDiscoveryEligibility(t *testing.T) {
	for _, availability := range []mpapi.Availability{mpapi.AvailabilityAvailable, mpapi.AvailabilityOperatorExposed} {
		t.Run("installable "+string(availability), func(t *testing.T) {
			m := modelWithSelectedRow(t, marketplaceDiscoveryRow("skill", "review", availability))
			m.pane = paneList

			updated, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
			m = updated.(Model)
			if m.pane != paneToggleModal {
				t.Fatalf("list Space on %s discovery did not open the scope modal: pane %v", availability, m.pane)
			}
			if cmd != nil {
				t.Fatal("opening the scope modal must not emit a command")
			}
		})
	}
	for _, availability := range []mpapi.Availability{mpapi.AvailabilityBlocked, mpapi.AvailabilityRevoked, "unknown"} {
		t.Run("non-installable "+string(availability), func(t *testing.T) {
			m := modelWithSelectedRow(t, marketplaceDiscoveryRow("skill", "review", availability))
			m.pane = paneList

			updated, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
			m = updated.(Model)
			if m.pane != paneList {
				t.Fatalf("list Space on %s discovery opened mutation UI: pane %v", availability, m.pane)
			}
			if cmd == nil {
				t.Fatal("list Space on non-installable discovery returned no toast")
			}
			if msg, ok := cmd().(toastMsg); !ok || !strings.Contains(msg.text, "not installable") {
				t.Fatalf("list Space on %s discovery = %#v, want the not-installable toast", availability, cmd())
			}
		})
	}

	t.Run("ordinary row control", func(t *testing.T) {
		m := modelWithSelectedRow(t, mkRow("unsafe", withProject))
		m.pane = paneList

		updated, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneToggleModal {
			t.Fatalf("list Space on an ordinary row did not open the toggle modal: pane %v", m.pane)
		}
		if cmd != nil {
			t.Fatal("opening the toggle modal must not emit a command")
		}
	})

	t.Run("unavailable row control", func(t *testing.T) {
		r := mkRow("missing", withProject)
		r.UnavailableReason = session.UnavailableCatalogReason
		m := modelWithSelectedRow(t, r)
		m.pane = paneList

		updated, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneToggleModal {
			t.Fatalf("list Space on an unavailable row did not open the toggle modal: pane %v", m.pane)
		}
		if cmd != nil {
			t.Fatal("opening the toggle modal must not emit a command")
		}
	})

	t.Run("organization override opens scope modal", func(t *testing.T) {
		r := mkRow("org-item", withGlobal)
		r.Artifact.IsMarketplace = true
		r.Artifact.Source = config.Organization
		r.MarketplaceCollectionOnly = true
		m := modelWithSelectedRow(t, r)
		m.pane = paneList

		updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneToggleModal {
			t.Fatalf("list Space on an org-overridable collection row did not open the toggle modal: pane %v", m.pane)
		}
	})

	t.Run("kind preserved for non-skill recovery row", func(t *testing.T) {
		r := mkRow("missing-cmd", withProject, withKind(ir.KindCommand))
		r.UnavailableReason = session.UnavailableCatalogReason
		m := modelWithSelectedRow(t, r)
		m.pane = paneList

		updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace})
		m = updated.(Model)
		if m.pane != paneToggleModal || m.modalSkill.Artifact.Kind != ir.KindCommand {
			t.Fatalf("command recovery row modal = pane %v kind %v", m.pane, m.modalSkill.Artifact.Kind)
		}
	})
}
