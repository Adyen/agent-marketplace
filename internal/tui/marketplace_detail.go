package tui

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
)

type marketplaceDetailMsg struct {
	generation requestGeneration
	key        string
	bundle     *bundleTree
	content    string
	width      int
	err        error
}

type marketplaceInspection struct {
	source   *session.MarketplaceInspection
	manifest inspection.Manifest
}

func marketplaceInspectableSkill(r row) bool {
	return session.MarketplaceInspectableSkill(r)
}

func loadMarketplaceSkillDetail(ctx context.Context, t theme, r row, generation requestGeneration, width int) tea.Cmd {
	key := rowKey(r)
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return marketplaceDetailMsg{generation: generation, key: key, err: err}
		}
		ctx, cancel := context.WithTimeout(ctxOrBackground(ctx), marketplace.DefaultSearchTimeout)
		defer cancel()
		inspectionSource, err := session.OpenMarketplaceInspection(ctx, r)
		if err != nil {
			return fail(err)
		}
		manifest := inspectionSource.Manifest()
		primary, err := inspectionSource.ReadFile(ctx, manifest.PrimaryPath)
		if err != nil {
			return fail(err)
		}
		tree := newInspectionTree(inspectionSource, manifest, primary)
		return marketplaceDetailMsg{
			generation: generation,
			key:        key,
			bundle:     tree,
			content:    renderBundleContent(t.dark, manifest.PrimaryPath, primary, len(primary) > maxDetailSourceBytes, width),
			width:      width,
		}
	}
}

func newInspectionTree(inspectionSource *session.MarketplaceInspection, manifest inspection.Manifest, primary []byte) *bundleTree {
	t := &bundleTree{
		files:      map[string][]byte{manifest.PrimaryPath: primary},
		inspection: &marketplaceInspection{source: inspectionSource, manifest: manifest},
	}
	t.rootNode = &bundleNode{name: manifest.Coordinate.Name, expanded: true, loaded: true}
	for _, file := range manifest.Files {
		t.insertArchivePath(file.Path)
	}
	t.expandArchiveDirs(t.rootNode)
	t.selected = manifest.PrimaryPath
	t.rebuild()
	return t
}

func (s *marketplaceInspection) inspectionFile(relative string) *inspection.File {
	for i := range s.manifest.Files {
		if s.manifest.Files[i].Path == relative {
			return &s.manifest.Files[i]
		}
	}
	return nil
}

func (s *marketplaceInspection) renderFile(ctx context.Context, dark bool, relative string, width int) string {
	file := s.inspectionFile(relative)
	if file == nil {
		return "Error reading file: not part of the verified inspection manifest"
	}
	content, err := s.source.ReadFile(ctxOrBackground(ctx), relative)
	if errors.Is(err, session.ErrMarketplaceInspectionUnsupported) {
		label := file.MediaType
		if label == "" {
			label = "unknown"
		}
		return fmt.Sprintf("%s\n\n%s file (%s, %d bytes). Preview is not supported for this file type.",
			detailFileName(relative), file.InspectionClass, sanitizeTerminalData(label, terminalSingleLine), file.Size)
	}
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	return renderBundleContent(dark, relative, content, len(content) > maxDetailSourceBytes, width)
}

func renderMarketplaceInspectionError(err error) string {
	return "Marketplace inspection unavailable: " + sanitizeTerminalData(err.Error(), terminalSingleLine) +
		"\n\nNo verified content was loaded. Press space to install this exact revision, or esc to go back."
}
