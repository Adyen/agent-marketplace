package session

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
)

var ErrMarketplaceInspectionUnsupported = errors.New("inspection preview is unsupported for this file type")

type MarketplaceInspection struct {
	client   *marketplace.Client
	item     mpapi.ResourceListItem
	manifest inspection.Manifest
}

func MarketplaceInspectableSkill(row Row) bool {
	return row.Artifact.IsMarketplace &&
		row.Artifact.Path == "" &&
		row.Artifact.Kind == ir.KindSkill &&
		row.MarketplaceDiscovery != nil &&
		MarketplaceDiscoveryInstallable(row)
}

func OpenMarketplaceInspection(ctx context.Context, row Row) (*MarketplaceInspection, error) {
	if !MarketplaceInspectableSkill(row) {
		return nil, errors.New("resource is not an inspectable uninstalled Marketplace skill")
	}
	discovery := row.MarketplaceDiscovery
	item := discovery.Item
	if item.RegistryID == "" || item.Kind != ir.KindSkill.String() || item.Name == "" || item.Revision == "" || item.ContentDigest == "" {
		return nil, errors.New("marketplace discovery did not provide an exact revision")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, marketplace.DefaultSearchTimeout)
	defer cancel()
	client, err := marketplace.NewClient(discovery.Endpoint.URL)
	if err != nil {
		return nil, err
	}
	wellKnown, err := client.WellKnown(ctx)
	if err != nil {
		return nil, err
	}
	if discovery.Endpoint.InstanceID == "" || wellKnown.InstanceID != discovery.Endpoint.InstanceID {
		return nil, fmt.Errorf("marketplace instance %q does not match discovery endpoint %q", wellKnown.InstanceID, discovery.Endpoint.InstanceID)
	}
	manifest, err := client.GetResourceInspectionManifest(ctx, item.RegistryID, item.Kind, item.Name, item.Revision)
	if err != nil {
		return nil, err
	}
	coordinate := manifest.Coordinate
	if string(coordinate.Marketplace) != discovery.Endpoint.InstanceID || string(coordinate.Registry) != item.RegistryID || coordinate.Kind != item.Kind || coordinate.Name != item.Name || manifest.RevisionDigest != item.Revision {
		return nil, errors.New("inspection manifest identity does not match the discovery item")
	}
	if manifest.RevisionDigest != item.ContentDigest {
		return nil, errors.New("inspection manifest does not match the discovery content digest")
	}
	return &MarketplaceInspection{client: client, item: item, manifest: manifest}, nil
}

func (m *MarketplaceInspection) Manifest() inspection.Manifest {
	if m == nil {
		return inspection.Manifest{}
	}
	manifest := m.manifest
	manifest.Files = slices.Clone(m.manifest.Files)
	return manifest
}

func (m *MarketplaceInspection) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if m == nil {
		return nil, errors.New("inspection is unavailable")
	}
	var file *inspection.File
	for i := range m.manifest.Files {
		if m.manifest.Files[i].Path == path {
			file = &m.manifest.Files[i]
			break
		}
	}
	if file == nil {
		return nil, errors.New("inspection file is not in the verified manifest")
	}
	if file.InspectionClass != inspection.ClassText {
		return nil, ErrMarketplaceInspectionUnsupported
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, marketplace.DefaultSearchTimeout)
	defer cancel()
	var content marketplace.InspectionContent
	var err error
	if path == m.manifest.PrimaryPath {
		content, err = m.client.GetResourceInspectionPrimary(ctx, m.item.RegistryID, m.item.Kind, m.item.Name, m.item.Revision)
	} else {
		content, err = m.client.GetResourceInspectionFile(ctx, m.item.RegistryID, m.item.Kind, m.item.Name, m.item.Revision, file.ContentDigest)
	}
	if err != nil {
		return nil, err
	}
	if content.Manifest.ManifestDigest != m.manifest.ManifestDigest ||
		content.Manifest.Coordinate != m.manifest.Coordinate ||
		content.Manifest.RevisionDigest != m.item.Revision ||
		content.File.Path != file.Path ||
		content.File.ContentDigest != file.ContentDigest {
		return nil, errors.New("inspection response identity does not match the verified manifest")
	}
	return append([]byte(nil), content.Data...), nil
}
