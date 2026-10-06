package tui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func marketplaceDiscoveryRow(kind, name string, availability mpapi.Availability) row {
	irKind, ok := skill.ParseKind(kind)
	if !ok {
		irKind = ir.KindSkill
	}
	return row{
		Artifact: resolver.ResolvedArtifact{
			Entry:                 catalog.Entry{Kind: irKind, Name: name, Description: "discovered " + name},
			IsMarketplace:         true,
			MarketplaceURL:        "https://market.example",
			MarketplaceInstanceID: "instance-1",
			MarketplaceRegistryID: "registry-1",
		},
		IsAvailable: true,
		MarketplaceDiscovery: &marketplaceDiscovery{
			Endpoint: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "instance-1", Alias: "public"},
			Item: mpapi.ResourceListItem{
				RegistryID: "registry-1", RegistrySlug: "tools", Kind: kind, Name: name,
				Revision: "rev-9", ContentDigest: "sha256:" + strings.Repeat("a", 64),
				Availability: availability, Description: "discovered " + name,
			},
		},
	}
}

func enterDetail(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return updated.(Model), cmd
}

func TestMarketplaceDiscoveryDetailShowsOperatorExposedRejection(t *testing.T) {
	m := modelWithSelectedRow(t, marketplaceDiscoveryRow("skill", "review", mpapi.AvailabilityOperatorExposed))
	m.width = 100
	m.height = 30
	m, _ = enterDetail(t, m)
	selected, ok := m.selectedRow()
	if !ok {
		t.Fatal("no selected row")
	}
	content := renderDetailFullContent(newTheme(true), selected, 100)
	if !strings.Contains(content, "Operator-exposed rejection") || !strings.Contains(content, "Press space to install") {
		t.Fatalf("operator-exposed detail = %q", content)
	}
}

func TestMarketplaceDiscoveryDetailShowsMetadataWithoutSourcePathError(t *testing.T) {
	for _, kind := range []string{"collection", "command"} {
		t.Run(kind, func(t *testing.T) {
			m := modelWithSelectedRow(t, marketplaceDiscoveryRow(kind, "review-"+kind, mpapi.AvailabilityAvailable))
			m.width = 100
			m.height = 30
			m, cmd := enterDetail(t, m)
			if m.pane != paneDetail {
				t.Fatalf("enter did not open the detail pane: %v", m.pane)
			}
			if cmd != nil {
				t.Fatal("discovery-only detail must not schedule content loading")
			}
			if m.bundle != nil || m.detailSelectedPath != "" {
				t.Fatalf("discovery-only detail must not open a filesystem bundle: %+v", m.bundle)
			}
			out := ansi.Strip(m.detailViewport.View())
			if strings.Contains(out, "no source path") || strings.Contains(out, "Error reading resource") {
				t.Fatalf("discovery detail produced a source-path error:\n%s", out)
			}
			for _, want := range []string{"review-" + kind, "rev-9", "install"} {
				if !contains(out, want) {
					t.Fatalf("discovery detail missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestMarketplaceUnavailableDiscoveryDetailIsActionable(t *testing.T) {
	m := modelWithSelectedRow(t, marketplaceDiscoveryRow("skill", "revoked-skill", mpapi.Availability("revoked")))
	m, _ = enterDetail(t, m)
	out := ansi.Strip(m.detailViewport.View())
	if !contains(out, "not currently installable") {
		t.Fatalf("unavailable discovery detail is not actionable:\n%s", out)
	}
}

// writeVerifiedArtifact encodes a canonical artifact into a cache-shaped
// directory (artifact.tar.gz + verified.marker) and returns the archive path.
func writeVerifiedArtifact(t *testing.T, kind, name string, payload []byte, assets map[string][]byte) string {
	t.Helper()
	ca := hccartifact.CanonicalArtifact{
		Manifest: hccartifact.CanonicalManifest{
			SchemaVersion:    hccartifact.CanonicalArtifactSchemaV1,
			ArtifactVersion:  hccartifact.CanonicalArtifactSchemaV1,
			Kind:             kind,
			Name:             name,
			Revision:         "rev-1",
			ContentDigest:    hccartifact.ContentDigest(payload),
			Provenance:       hccartifact.CanonicalProvenance{Generator: "test"},
			SupportedSchemas: []string{hccartifact.CanonicalArtifactSchemaV1},
		},
		Payload: payload,
		Assets:  assets,
	}
	raw, digest, err := hccartifact.Encode(ca)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "artifacts", strings.ReplaceAll(digest, ":", "_"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, marketplaceArtifactName)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, marketplaceVerifiedMarker), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func installedMarketplaceRow(kind ir.ResourceKind, name, path string) row {
	return row{Artifact: resolver.ResolvedArtifact{
		Entry:                 catalog.Entry{Kind: kind, Name: name, Path: path},
		IsMarketplace:         true,
		MarketplaceURL:        "https://market.example",
		MarketplaceInstanceID: "instance-1",
		MarketplaceRegistryID: "registry-1",
	}}
}

func TestInstalledMarketplaceArchiveOpensVerifiedBundle(t *testing.T) {
	payload := []byte(`{"name":"review","description":"review bundle"}`)
	artifactPath := writeVerifiedArtifact(t, "skill", "review", payload, map[string][]byte{"notes.md": []byte("# Notes\n")})
	m := modelWithSelectedRow(t, installedMarketplaceRow(ir.KindSkill, "review", artifactPath))
	m.width = 100
	m.height = 30

	m, cmd := enterDetail(t, m)
	if m.bundle == nil {
		t.Fatalf("installed archive did not open a bundle; viewport:\n%s", ansi.Strip(m.detailViewport.View()))
	}
	if m.detailSelectedPath != "SKILL.md" {
		t.Fatalf("initial archive selection = %q", m.detailSelectedPath)
	}
	var names []string
	for _, entry := range m.bundle.entries() {
		names = append(names, entry.RelPath)
	}
	for _, want := range []string{"manifest.json", "SKILL.md", "assets/notes.md"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("archive bundle missing %q in %v", want, names)
		}
	}
	if cmd == nil {
		t.Fatal("enter did not schedule an archive preview")
	}
	msg := cmd().(detailContentMsg)
	updated, _ := m.Update(msg)
	m = updated.(Model)
	out := ansi.Strip(m.detailViewport.View())
	if !contains(out, "review bundle") {
		t.Fatalf("archive preview did not render verified payload:\n%s", out)
	}
	// The cache container itself must never be browsable.
	if contains(out, "verified.marker") || contains(out, "artifact.tar.gz") {
		t.Fatalf("archive preview leaked cache container details:\n%s", out)
	}
}

func TestInstalledMarketplaceSkillRendersAsMarkdownWithFrontmatter(t *testing.T) {
	payload := []byte(`{"Agent":"","AllowedTools":["Read"],"ArgumentHint":"","Body":"\n# K8s App Onboarding\n\nUse this skill when onboarding an application.\n","Description":"Onboard applications onto Kubernetes","Model":"","Permissions":{},"PermissionsSet":false,"Subagent":"","SupportFiles":null,"Triggers":null,"name":"k8s-app-onboarding"}`)
	artifactPath := writeVerifiedArtifact(t, "skill", "k8s-app-onboarding", payload, nil)
	m := modelWithSelectedRow(t, installedMarketplaceRow(ir.KindSkill, "k8s-app-onboarding", artifactPath))
	m.width = 100
	m.height = 30

	m, cmd := enterDetail(t, m)
	if m.detailSelectedPath != "SKILL.md" {
		t.Fatalf("initial skill selection = %q, want SKILL.md", m.detailSelectedPath)
	}
	if cmd == nil {
		t.Fatal("enter did not schedule a skill preview")
	}
	msg := cmd().(detailContentMsg)
	updated, _ := m.Update(msg)
	out := ansi.Strip(updated.(Model).detailViewport.View())
	for _, want := range []string{"k8s-app-onboarding", "Onboard", "applications onto Kubernetes", "K8s App Onboarding", "Use this skill when onboarding an application."} {
		if !contains(out, want) {
			t.Fatalf("skill preview missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{`"Agent"`, `"AllowedTools"`, `"Body"`} {
		if contains(out, unwanted) {
			t.Fatalf("skill preview contains canonical JSON field %q:\n%s", unwanted, out)
		}
	}
}

func TestMarketplaceArchiveDetailFailsClosed(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(corrupt, []byte("not a gzip archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "verified.marker"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	valid := writeVerifiedArtifact(t, "skill", "review", []byte(`{"name":"review"}`), nil)
	unmarked := filepath.Join(t.TempDir(), "artifact.tar.gz")
	raw, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unmarked, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "real.tar.gz")
	if err := os.WriteFile(outside, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	linkedDir := t.TempDir()
	linked := filepath.Join(linkedDir, "artifact.tar.gz")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedDir, "verified.marker"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "artifact.tar.gz"),
		"corrupt":   corrupt,
		"unmarked":  unmarked,
		"symlinked": linked,
	} {
		t.Run(name, func(t *testing.T) {
			m := modelWithSelectedRow(t, installedMarketplaceRow(ir.KindSkill, "review", path))
			m.detailViewport.SetContent("stale preview content")
			m, cmd := enterDetail(t, m)
			if cmd != nil || m.bundle != nil || m.detailSelectedPath != "" {
				t.Fatalf("unsafe archive opened a bundle: cmd=%v bundle=%v", cmd, m.bundle != nil)
			}
			out := ansi.Strip(m.detailViewport.View())
			if contains(out, "stale preview content") {
				t.Fatalf("stale preview survived a failed archive open:\n%s", out)
			}
			if !contains(out, "Error reading resource") {
				t.Fatalf("failed archive did not produce an actionable state:\n%s", out)
			}
		})
	}
}

// spec: session-navigation/installed-skill-definition-and-supporting-files-remain-browsable
func TestInstalledMarketplaceStagedSkillShowsVirtualPrimary(t *testing.T) {
	staged := filepath.Join(t.TempDir(), "sha256_"+strings.Repeat("a", 64))
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	notes := []byte("# Notes\n\nSupporting instructions.")
	if err := os.WriteFile(filepath.Join(staged, "notes.md"), notes, 0o644); err != nil {
		t.Fatal(err)
	}
	skillResource := ir.NewSkill("grill", "Grill things", "# Grill with docs\n\nPrimary instructions.", nil, nil, ir.SourceRef{})
	skillResource.SupportFiles = []string{"notes.md"}
	r := installedMarketplaceRow(ir.KindSkill, "grill", staged)
	r.Artifact.Resource = skillResource

	m := modelWithSelectedRow(t, r)
	m.width = 100
	m.height = 30
	m, cmd := enterDetail(t, m)
	if m.bundle == nil {
		t.Fatalf("staged skill did not open a bundle; viewport:\n%s", ansi.Strip(m.detailViewport.View()))
	}
	if m.detailSelectedPath != "SKILL.md" {
		t.Fatalf("initial staged-skill selection = %q, want SKILL.md", m.detailSelectedPath)
	}
	var names []string
	for _, entry := range m.bundle.entries() {
		names = append(names, entry.RelPath)
	}
	for _, want := range []string{"SKILL.md", "notes.md"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("staged bundle missing %q in %v", want, names)
		}
	}
	if cmd == nil {
		t.Fatal("enter did not schedule a primary preview")
	}
	updated, _ := m.Update(cmd().(detailContentMsg))
	m = updated.(Model)
	if out := ansi.Strip(m.detailViewport.View()); !contains(out, "Primary instructions.") {
		t.Fatalf("virtual primary preview missing body:\n%s", out)
	}

	next, cmd := m.moveBundleCursor(1)
	m = next.(Model)
	if m.detailSelectedPath != "notes.md" {
		t.Fatalf("moving down selected %q, want notes.md", m.detailSelectedPath)
	}
	updated, _ = m.Update(cmd().(detailContentMsg))
	m = updated.(Model)
	if out := ansi.Strip(m.detailViewport.View()); !contains(out, "Supporting instructions.") {
		t.Fatalf("support file preview missing body:\n%s", out)
	}

	entries, err := os.ReadDir(staged)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "notes.md" {
		t.Fatalf("staging directory mutated: %v", entries)
	}
	data, err := os.ReadFile(filepath.Join(staged, "notes.md"))
	if err != nil || string(data) != string(notes) {
		t.Fatalf("support file changed: %v", err)
	}
}

// spec: session-navigation/installed-skill-definition-and-supporting-files-remain-browsable
func TestInstalledMarketplaceStagedSkillFailsVisibly(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  func(t *testing.T) row
	}{
		{name: "missing directory", row: func(t *testing.T) row {
			r := installedMarketplaceRow(ir.KindSkill, "grill", filepath.Join(t.TempDir(), "missing"))
			r.Artifact.Resource = ir.NewSkill("grill", "Grill", "# Grill", nil, nil, ir.SourceRef{})
			return r
		}},
		{name: "staged support without attached skill", row: func(t *testing.T) row {
			staged := t.TempDir()
			if err := os.WriteFile(filepath.Join(staged, "notes.md"), []byte("# Notes\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return installedMarketplaceRow(ir.KindSkill, "grill", staged)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := modelWithSelectedRow(t, tc.row(t))
			m.width = 100
			m.height = 30
			m.detailViewport.SetContent("stale preview content")
			m, cmd := enterDetail(t, m)
			if cmd != nil || m.bundle != nil || m.detailSelectedPath != "" {
				t.Fatalf("invalid staged skill opened a bundle: cmd=%v bundle=%v", cmd, m.bundle != nil)
			}
			out := ansi.Strip(m.detailViewport.View())
			if !contains(out, "Error reading resource") || contains(out, "stale preview content") {
				t.Fatalf("invalid staged skill did not fail visibly:\n%s", out)
			}
			if strings.Contains(out, ".cache") || strings.Contains(out, "sha256_") {
				t.Fatalf("error leaked cache internals:\n%s", out)
			}
		})
	}
}

func TestMarketplaceArchiveMemberTraversalRejected(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "verified.marker"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newMarketplaceArtifactTree(path); err == nil {
		t.Fatal("archive with traversal member was accepted")
	}
}

func TestMarketplaceDetailResizeKeepsMetadataAndRejectsStalePreview(t *testing.T) {
	m := modelWithSelectedRow(t, marketplaceDiscoveryRow("skill", "review", mpapi.AvailabilityAvailable))
	m.width = 100
	m.height = 30
	m, _ = enterDetail(t, m)

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("metadata-only detail scheduled a content reload on resize")
	}
	if m.detailViewport.View() == "" {
		t.Fatal("resize cleared the metadata detail")
	}

	// A stale preview response must not overwrite the metadata state.
	updated, _ = m.Update(detailContentMsg{generation: m.beginRequest(requestDetail) + 99, content: "stale"})
	m = updated.(Model)
	if contains(ansi.Strip(m.detailViewport.View()), "stale") {
		t.Fatal("stale detail response overwrote marketplace metadata")
	}
}

func TestRenderDetailFullContentMarketplaceRows(t *testing.T) {
	discovery := renderDetailFullContent(newTheme(true), marketplaceDiscoveryRow("collection", "bundle", mpapi.AvailabilityAvailable), 80)
	if strings.Contains(discovery, "no source path") || !contains(discovery, "bundle") {
		t.Fatalf("collection discovery full detail = %q", discovery)
	}

	artifactPath := writeVerifiedArtifact(t, "command", "deploy", []byte(`{"name":"deploy","run":"go deploy"}`), nil)
	out := ansi.Strip(renderDetailFullContent(newTheme(true), installedMarketplaceRow(ir.KindCommand, "deploy", artifactPath), 80))
	if !contains(out, "go deploy") {
		t.Fatalf("installed archive full detail did not render payload:\n%s", out)
	}

	missing := ansi.Strip(renderDetailFullContent(newTheme(true), installedMarketplaceRow(ir.KindSkill, "gone", filepath.Join(t.TempDir(), "artifact.tar.gz")), 80))
	if !contains(missing, "Error reading resource") {
		t.Fatalf("missing archive full detail = %q", missing)
	}
}
