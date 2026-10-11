package tui

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
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
	client   *marketplace.Client
	item     mpapi.ResourceListItem
	manifest inspection.Manifest
}

func marketplaceInspectableSkill(r row) bool {
	return isMarketplaceDetailOnly(r) &&
		r.Artifact.Kind == ir.KindSkill &&
		r.MarketplaceDiscovery != nil &&
		marketplaceDiscoveryInstallable(r)
}

func loadMarketplaceSkillDetail(ctx context.Context, t theme, r row, generation requestGeneration, width int) tea.Cmd {
	key := rowKey(r)
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return marketplaceDetailMsg{generation: generation, key: key, err: err}
		}
		d := r.MarketplaceDiscovery
		item := d.Item
		if item.RegistryID == "" || item.Kind != ir.KindSkill.String() || item.Name == "" || item.Revision == "" || item.ContentDigest == "" {
			return fail(errors.New("marketplace discovery did not provide an exact revision"))
		}
		ctx, cancel := context.WithTimeout(ctxOrBackground(ctx), marketplace.DefaultSearchTimeout)
		defer cancel()
		client, err := marketplace.NewClient(d.Endpoint.URL)
		if err != nil {
			return fail(err)
		}
		wellKnown, err := client.WellKnown(ctx)
		if err != nil {
			return fail(err)
		}
		if d.Endpoint.InstanceID == "" || wellKnown.InstanceID != d.Endpoint.InstanceID {
			return fail(fmt.Errorf("marketplace instance %q does not match discovery endpoint %q", wellKnown.InstanceID, d.Endpoint.InstanceID))
		}
		primary, err := client.GetResourceInspectionPrimary(ctx, item.RegistryID, item.Kind, item.Name, item.Revision)
		if err != nil {
			return fail(err)
		}
		manifest := primary.Manifest
		coord := manifest.Coordinate
		if string(coord.Registry) != item.RegistryID || coord.Kind != item.Kind || coord.Name != item.Name || manifest.RevisionDigest != item.Revision {
			return fail(errors.New("inspection manifest identity does not match the discovery item"))
		}
		if manifest.RevisionDigest != item.ContentDigest {
			return fail(errors.New("inspection manifest does not match the discovery content digest"))
		}
		tree := newInspectionTree(client, item, manifest, primary.Data)
		return marketplaceDetailMsg{
			generation: generation,
			key:        key,
			bundle:     tree,
			content:    renderBundleContent(t.dark, manifest.PrimaryPath, primary.Data, len(primary.Data) > maxDetailSourceBytes, width),
			width:      width,
		}
	}
}

func newInspectionTree(client *marketplace.Client, item mpapi.ResourceListItem, manifest inspection.Manifest, primary []byte) *bundleTree {
	t := &bundleTree{
		files:      map[string][]byte{manifest.PrimaryPath: primary},
		inspection: &marketplaceInspection{client: client, item: item, manifest: manifest},
	}
	t.rootNode = &bundleNode{name: item.Name, expanded: true, loaded: true}
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
	if file.InspectionClass != inspection.ClassText {
		label := file.MediaType
		if label == "" {
			label = "unknown"
		}
		return fmt.Sprintf("%s\n\n%s file (%s, %d bytes). Preview is not supported for this file type.",
			detailFileName(relative), file.InspectionClass, sanitizeTerminalData(label, terminalSingleLine), file.Size)
	}
	ctx, cancel := context.WithTimeout(ctxOrBackground(ctx), marketplace.DefaultSearchTimeout)
	defer cancel()
	item := s.item
	content, err := s.client.GetResourceInspectionFile(ctx, item.RegistryID, item.Kind, item.Name, item.Revision, file.ContentDigest)
	if err != nil {
		return "Error reading file: " + sanitizeTerminalData(err.Error(), terminalSingleLine)
	}
	if content.Manifest.ManifestDigest != s.manifest.ManifestDigest ||
		content.File.Path != file.Path || content.File.ContentDigest != file.ContentDigest ||
		content.Manifest.RevisionDigest != item.Revision {
		return "Error reading file: inspection response identity does not match the verified manifest"
	}
	return renderBundleContent(dark, relative, content.Data, len(content.Data) > maxDetailSourceBytes, width)
}

func renderMarketplaceInspectionError(err error) string {
	return "Marketplace inspection unavailable: " + sanitizeTerminalData(err.Error(), terminalSingleLine) +
		"\n\nNo verified content was loaded. Press space to install this exact revision, or esc to go back."
}
