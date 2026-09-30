package lockfile

import (
	"fmt"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

func normalizeMarketplaces(doc *Document) error {
	for i := range doc.Marketplaces {
		if err := normalizeMarketplace(&doc.Marketplaces[i], i); err != nil {
			return err
		}
	}
	sort.Slice(doc.Marketplaces, func(i, j int) bool {
		a, b := doc.Marketplaces[i], doc.Marketplaces[j]
		if a.URL != b.URL {
			return a.URL < b.URL
		}
		return a.InstanceID < b.InstanceID
	})
	var err error
	doc.Marketplaces, err = dedupeComparable(doc.Marketplaces,
		func(v MarketplaceLock) string { return v.URL }, "Marketplace endpoint")
	return err
}

func normalizeMarketplace(mp *MarketplaceLock, index int) error {
	canonical, err := urlutil.NormalizeEndpointURL(mp.URL)
	if err != nil {
		return fmt.Errorf("marketplaces[%d].url: %w", index, err)
	}
	mp.URL = canonical
	if err := validateSegment(mp.InstanceID, fmt.Sprintf("marketplaces[%d].instance_id", index)); err != nil {
		return err
	}

	for j := range mp.RegistryBindings {
		binding := &mp.RegistryBindings[j]
		if err := validateSegment(binding.Name, fmt.Sprintf("marketplaces[%d].registry_bindings[%d].name", index, j)); err != nil {
			return err
		}
		if err := validateSegment(binding.RegistryID, fmt.Sprintf("marketplaces[%d].registry_bindings[%d].registry_id", index, j)); err != nil {
			return err
		}
	}
	sort.Slice(mp.RegistryBindings, func(i, j int) bool {
		a, b := mp.RegistryBindings[i], mp.RegistryBindings[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.RegistryID < b.RegistryID
	})
	mp.RegistryBindings, err = dedupeComparable(mp.RegistryBindings,
		func(v MarketplaceRegistryBinding) string { return v.Name }, "Marketplace registry binding")
	if err != nil {
		return err
	}
	bindings := make(map[string]string, len(mp.RegistryBindings))
	bound := make(map[string]bool, len(mp.RegistryBindings))
	for _, binding := range mp.RegistryBindings {
		bindings[binding.Name] = binding.RegistryID
		bound[binding.RegistryID] = true
	}

	collections := make(map[string]MarketplaceResourceLock)
	for j := range mp.Resources {
		resource := &mp.Resources[j]
		if err := normalizeMarketplaceResource(resource, index, j); err != nil {
			return err
		}
		if boundID, exists := bindings[resource.RegistryName]; !exists {
			return coded(CodeInvalidDocument, "marketplaces[%d].resources[%d].registry_name %q has no registry binding", index, j, resource.RegistryName)
		} else if boundID != resource.RegistryID {
			return coded(CodeInvalidDocument, "marketplaces[%d].resources[%d].registry_name %q is bound to registry_id %q, not %q", index, j, resource.RegistryName, boundID, resource.RegistryID)
		}
		if !bound[resource.RegistryID] {
			return coded(CodeInvalidDocument, "marketplaces[%d].resources[%d].registry_id %q has no registry binding", index, j, resource.RegistryID)
		}
		if resource.Kind == api.KindCollection {
			collections[resource.RegistryID+"\x00"+resource.Name] = *resource
		}
	}
	sort.Slice(mp.Resources, func(i, j int) bool {
		return marketplaceResourceIdentity(mp.Resources[i]) < marketplaceResourceIdentity(mp.Resources[j])
	})
	mp.Resources, err = dedupeComparable(mp.Resources, marketplaceResourceIdentity, "Marketplace resource")
	if err != nil {
		return err
	}

	for j := range mp.Resources {
		resource := &mp.Resources[j]
		for k := range resource.SelectedBy {
			selection := &resource.SelectedBy[k]
			if err := validateSegment(selection.RegistryID, fmt.Sprintf("marketplaces[%d].resources[%d].selected_by[%d].registry_id", index, j, k)); err != nil {
				return err
			}
			if err := validateSegment(selection.Name, fmt.Sprintf("marketplaces[%d].resources[%d].selected_by[%d].name", index, j, k)); err != nil {
				return err
			}
			if err := validateDigest(selection.Revision, fmt.Sprintf("marketplaces[%d].resources[%d].selected_by[%d].revision", index, j, k)); err != nil {
				return err
			}
			if !bound[selection.RegistryID] {
				return coded(CodeInvalidDocument, "marketplaces[%d].resources[%d].selected_by[%d].registry_id %q has no registry binding", index, j, k, selection.RegistryID)
			}
			collection, exists := collections[selection.RegistryID+"\x00"+selection.Name]
			if !exists || collection.ContentDigest != selection.Revision {
				return coded(CodeInvalidDocument, "marketplaces[%d].resources[%d].selected_by[%d] does not resolve to a locked collection", index, j, k)
			}
		}
		sort.Slice(resource.SelectedBy, func(a, b int) bool {
			return marketplaceSelectionIdentity(resource.SelectedBy[a]) < marketplaceSelectionIdentity(resource.SelectedBy[b])
		})
		resource.SelectedBy, err = dedupeComparable(resource.SelectedBy, marketplaceSelectionIdentity, "Marketplace selection")
		if err != nil {
			return err
		}
	}
	return nil
}

func normalizeMarketplaceResource(resource *MarketplaceResourceLock, index, j int) error {
	field := func(name string) string {
		return fmt.Sprintf("marketplaces[%d].resources[%d].%s", index, j, name)
	}
	if err := validateSegment(resource.RegistryName, field("registry_name")); err != nil {
		return err
	}
	if err := validateSegment(resource.RegistryID, field("registry_id")); err != nil {
		return err
	}
	if !api.IsMarketplaceKind(resource.Kind) {
		return coded(CodeInvalidDocument, "%s %q is not a selectable Marketplace kind", field("kind"), resource.Kind)
	}
	if err := validateSegment(resource.Name, field("name")); err != nil {
		return err
	}
	if err := validateSegment(resource.Revision, field("revision")); err != nil {
		return err
	}
	if err := validateDigest(resource.ContentDigest, field("content_digest")); err != nil {
		return err
	}
	collection := resource.Kind == api.KindCollection
	switch resource.Acquisition {
	case MarketplaceAcquisitionMirror:
		if collection && resource.ArchiveDigest != "" {
			return coded(CodeInvalidDocument, "%s is forbidden for collection resources", field("archive_digest"))
		}
		if resource.RegistryCommit != "" || resource.RegistryManifestDigest != "" || resource.SourcePath != "" {
			return coded(CodeInvalidDocument, "%s acquisition forbids registry_commit, registry_manifest_digest, and source_path", resource.Acquisition)
		}
	case MarketplaceAcquisitionIndexOnly:
		if collection {
			return coded(CodeInvalidDocument, "collection %s must use mirror acquisition", field("name"))
		}
		if !api.IsCanonicalGitCommit(resource.RegistryCommit) {
			return coded(CodeInvalidDocument, "%s must be a canonical lowercase Git commit", field("registry_commit"))
		}
		if err := validateDigest(resource.RegistryManifestDigest, field("registry_manifest_digest")); err != nil {
			return err
		}
		clean, err := normalizeArtifactPath(resource.SourcePath, false)
		if err != nil {
			return fmt.Errorf("%s: %w", field("source_path"), err)
		}
		resource.SourcePath = clean
	default:
		return coded(CodeInvalidDocument, "%s %q is not a supported acquisition", field("acquisition"), resource.Acquisition)
	}
	if collection {
		if err := validateDigest(resource.CollectionManifestDigest, field("collection_manifest_digest")); err != nil {
			return err
		}
		if resource.CollectionManifestDigest != resource.ContentDigest {
			return coded(CodeInvalidDocument, "%s must equal content_digest for collection resources", field("collection_manifest_digest"))
		}
	} else {
		if resource.CollectionManifestDigest != "" {
			return coded(CodeInvalidDocument, "%s is forbidden for non-collection resources", field("collection_manifest_digest"))
		}
		if err := validateDigest(resource.ArchiveDigest, field("archive_digest")); err != nil {
			return err
		}
	}
	return nil
}

func validateSegment(value, field string) error {
	if err := nameutil.ValidateSegment(value); err != nil {
		return coded(CodeInvalidDocument, "%s %s", field, err)
	}
	return nil
}

func marketplaceResourceIdentity(resource MarketplaceResourceLock) string {
	return resource.RegistryID + "\x00" + resource.Kind + "\x00" + resource.Name
}

func marketplaceSelectionIdentity(selection MarketplaceSelectionLock) string {
	return selection.RegistryID + "\x00" + selection.Name + "\x00" + selection.Revision.String()
}
