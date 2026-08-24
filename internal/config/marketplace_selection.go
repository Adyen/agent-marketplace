package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func EnsureMarketplaceDirectPin(pin MarketplaceResourcePin) MarketplaceResourcePin {
	pin.Direct = true
	return pin
}

func EnsureMarketplaceCollectionWithSnapshotAndCommit(configPath, marketplaceURL, instanceID string, oldCollection MarketplaceResourcePin, replacement []MarketplaceResourcePin) (*FileSnapshot, ConfigFileCommit, error) {
	return mutateMarketplaceConfigWithSnapshot(configPath, func(doc *yaml.Node, mode os.FileMode) error {
		return replaceMarketplaceCollectionDocument(configPath, doc, mode, marketplaceURL, instanceID, oldCollection, replacement)
	})
}

func RemoveMarketplaceCollectionWithSnapshotAndCommit(configPath, marketplaceURL, instanceID string, collection MarketplaceResourcePin) (*FileSnapshot, ConfigFileCommit, error) {
	return mutateMarketplaceConfigWithSnapshot(configPath, func(doc *yaml.Node, mode os.FileMode) error {
		return replaceMarketplaceCollectionDocument(configPath, doc, mode, marketplaceURL, instanceID, collection, nil)
	})
}

func RemoveMarketplaceDirectPinWithSnapshotAndCommit(configPath, marketplaceURL, instanceID string, pin MarketplaceResourcePin) (*FileSnapshot, ConfigFileCommit, error) {
	return mutateMarketplaceConfigWithSnapshot(configPath, func(doc *yaml.Node, mode os.FileMode) error {
		return removeMarketplaceDirectPinDocument(configPath, doc, mode, marketplaceURL, instanceID, pin)
	})
}

func mutateMarketplaceConfigWithSnapshot(configPath string, mutate func(*yaml.Node, os.FileMode) error) (*FileSnapshot, ConfigFileCommit, error) {
	var snapshot *FileSnapshot
	var commit ConfigFileCommit
	err := withConfigPathLock(configPath, func() error {
		var err error
		snapshot, err = SnapshotConfigFile(configPath)
		if err != nil {
			return err
		}
		doc, mode, err := readOrInitConfig(configPath)
		if err != nil {
			return err
		}
		if err := mutate(doc, mode); err != nil {
			return err
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("read committed config %q: %w", configPath, err)
		}
		commit = NewConfigFileCommit(data)
		return nil
	})
	return snapshot, commit, err
}

func replaceMarketplaceCollectionDocument(configPath string, doc *yaml.Node, mode os.FileMode, marketplaceURL, instanceID string, oldCollection MarketplaceResourcePin, replacement []MarketplaceResourcePin) error {
	canonicalURL, err := NormalizeMarketplaceURL(marketplaceURL)
	if err != nil {
		return err
	}
	if err := ValidateMarketplaceResourcePin(oldCollection); err != nil {
		return fmt.Errorf("existing collection: %w", err)
	}
	if oldCollection.Kind != "collection" {
		return fmt.Errorf("expected Marketplace collection pin")
	}
	if len(replacement) > 0 {
		if err := validateIncomingMarketplacePins(replacement); err != nil {
			return err
		}
		for _, pin := range replacement {
			if err := ValidateMarketplaceResourcePin(pin); err != nil {
				return err
			}
		}
		if replacement[0].Kind != "collection" {
			return fmt.Errorf("replacement must begin with a collection pin")
		}
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	marketplaces := findMappingValue(root, "marketplaces")
	if marketplaces == nil || marketplaces.Kind != yaml.SequenceNode {
		return fmt.Errorf("update config %q: marketplaces must be a sequence", configPath)
	}
	if err := rejectMarketplaceYAMLAliases(doc); err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	entries, err := decodeMarketplaceEntries(marketplaces)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	var entryNode *yaml.Node
	for _, entry := range entries {
		if entry.canonicalURL != canonicalURL || entry.entry.InstanceID != instanceID {
			continue
		}
		if entryNode != nil {
			return fmt.Errorf("config %q contains duplicate Marketplace entry identity for URL %q and instance_id %q", configPath, canonicalURL, instanceID)
		}
		entryNode = entry.node
	}
	if entryNode == nil {
		return fmt.Errorf("marketplace entry %q/%q not found in config %q", canonicalURL, instanceID, configPath)
	}
	resources, err := ensureMarketplaceResources(entryNode)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	oldKey := marketplacePinKey(oldCollection)
	var oldNode *yaml.Node
	var oldPin MarketplaceResourcePin
	var collectionRootCount int
	originalNodes := append([]*yaml.Node(nil), resources.Content...)
	for _, node := range originalNodes {
		pin, err := decodeMarketplacePin(node)
		if err != nil {
			return err
		}
		if pin.Kind != "collection" || marketplacePinKey(pin) != oldKey {
			continue
		}
		collectionRootCount++
		if collectionRootCount > 1 {
			return fmt.Errorf("config %q contains duplicate Marketplace collection roots for %s/%s/%s", configPath, oldCollection.SourceID, oldCollection.Kind, oldCollection.Name)
		}
		oldNode = node
		oldPin = pin
	}
	if oldNode == nil {
		return fmt.Errorf("collection pin %s/%s/%s not found in config %q", oldCollection.SourceID, oldCollection.Kind, oldCollection.Name, configPath)
	}
	if !sameMarketplacePinCASValue(oldPin, oldCollection) {
		return fmt.Errorf("marketplace collection compare-and-swap conflict for %s/%s/%s", oldCollection.SourceID, oldCollection.Kind, oldCollection.Name)
	}
	if len(replacement) > 0 && !sameMarketplacePinIdentity(replacement[0], oldCollection) {
		return fmt.Errorf("replacement collection root identity does not match %s/%s/%s", oldCollection.SourceID, oldCollection.Kind, oldCollection.Name)
	}

	oldSelector := marketplaceCollectionSelection(oldCollection)
	newByKey := make(map[string]MarketplaceResourcePin, len(replacement))
	removed := make(map[*yaml.Node]bool)
	for _, pin := range replacement {
		key := marketplacePinKey(pin)
		if _, exists := newByKey[key]; exists {
			return fmt.Errorf("duplicate replacement Marketplace pin %s/%s/%s", pin.SourceID, pin.Kind, pin.Name)
		}
		newByKey[key] = pin
	}
	for _, node := range originalNodes {
		pin, err := decodeMarketplacePin(node)
		if err != nil {
			return err
		}
		if pin.Kind == "collection" {
			continue
		}
		key := marketplacePinKey(pin)
		newPin, inReplacement := newByKey[key]
		oldEdges := pin.SelectedBy.List()
		remaining := removeMarketplaceSelection(oldEdges, oldSelector)
		wasSelectedByOld := len(remaining) != len(oldEdges)
		if !wasSelectedByOld && !inReplacement {
			continue
		}
		independentlySelected := pin.Direct || len(remaining) > 0
		if inReplacement && independentlySelected && !sameMarketplacePinContent(pin, newPin) {
			return fmt.Errorf("collection replacement conflicts with another selection for %s/%s/%s", pin.SourceID, pin.Kind, pin.Name)
		}
		if wasSelectedByOld {
			if !inReplacement && len(remaining) == 0 && !pin.Direct {
				removed[node] = true
				continue
			}
			pin.SelectedBy = MarketplaceSelectionsFrom(remaining)
		}
		if inReplacement {
			// An independently selected leaf owns its existing content and local
			// identity. A replacement may add its collection edge only when the
			// content agrees; never overwrite the direct/other-collection pin.
			if independentlySelected {
				newPin = pin
			}
			if newPin.InstalledName == "" {
				newPin.InstalledName = pin.InstalledName
			}
			newPin.Direct = pin.Direct
			if len(oldEdges) == 0 && !newPin.Direct {
				newPin.Direct = true
			}
			newEdges := append([]MarketplaceSelection(nil), remaining...)
			newEdges = appendMarketplaceSelection(newEdges, marketplaceCollectionSelection(replacement[0]))
			newPin.SelectedBy = MarketplaceSelectionsFrom(newEdges)
			replaceMarketplacePinNode(node, newPin)
			delete(newByKey, key)
			continue
		}
		replaceMarketplacePinNode(node, pin)
	}
	removed[oldNode] = true
	retained := make([]*yaml.Node, 0, len(originalNodes)+len(replacement))
	for _, node := range originalNodes {
		if !removed[node] {
			retained = append(retained, node)
		}
	}
	resources.Content = retained
	if len(replacement) > 0 {
		resources.Content = append(resources.Content, marketplacePinNode(replacement[0]))
		for _, pin := range replacement[1:] {
			if _, exists := newByKey[marketplacePinKey(pin)]; exists {
				resources.Content = append(resources.Content, marketplacePinNode(pin))
			}
		}
	}
	return writeConfigDocument(configPath, mode, doc)
}

func removeMarketplaceDirectPinDocument(configPath string, doc *yaml.Node, mode os.FileMode, marketplaceURL, instanceID string, wanted MarketplaceResourcePin) error {
	canonicalURL, err := NormalizeMarketplaceURL(marketplaceURL)
	if err != nil {
		return err
	}
	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	if err := rejectMarketplaceYAMLAliases(doc); err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	marketplaces := findMappingValue(root, "marketplaces")
	if marketplaces == nil || marketplaces.Kind != yaml.SequenceNode {
		return fmt.Errorf("update config %q: marketplaces must be a sequence", configPath)
	}
	entries, err := decodeMarketplaceEntries(marketplaces)
	if err != nil {
		return fmt.Errorf("update config %q: %w", configPath, err)
	}
	var entryNode *yaml.Node
	for _, entry := range entries {
		if entry.canonicalURL == canonicalURL && entry.entry.InstanceID == instanceID {
			if entryNode != nil {
				return fmt.Errorf("config %q contains duplicate Marketplace entry identity", configPath)
			}
			entryNode = entry.node
		}
	}
	if entryNode == nil {
		return fmt.Errorf("marketplace entry %q/%q not found in config %q", canonicalURL, instanceID, configPath)
	}
	resources, err := ensureMarketplaceResources(entryNode)
	if err != nil {
		return err
	}
	node := findMarketplacePinNode(resources, marketplacePinKey(wanted))
	if node == nil {
		return fmt.Errorf("marketplace pin %s/%s/%s not found in config %q", wanted.SourceID, wanted.Kind, wanted.Name, configPath)
	}
	got, err := decodeMarketplacePin(node)
	if err != nil {
		return err
	}
	if !sameMarketplacePinCASValue(got, wanted) {
		return fmt.Errorf("marketplace pin compare-and-swap conflict for %s/%s/%s", wanted.SourceID, wanted.Kind, wanted.Name)
	}
	if len(got.SelectedBy.List()) == 0 {
		removeNodeFromSequence(resources, node)
	} else if got.Direct {
		got.Direct = false
		replaceMarketplacePinNode(node, got)
	} else {
		return nil
	}
	return writeConfigDocument(configPath, mode, doc)
}

func findMarketplacePinNode(resources *yaml.Node, key string) *yaml.Node {
	for _, node := range resources.Content {
		pin, err := decodeMarketplacePin(node)
		if err == nil && marketplacePinKey(pin) == key {
			return node
		}
	}
	return nil
}

func removeNodeFromSequence(sequence *yaml.Node, wanted *yaml.Node) {
	for i, node := range sequence.Content {
		if node == wanted {
			sequence.Content = append(sequence.Content[:i], sequence.Content[i+1:]...)
			return
		}
	}
}

func marketplacePinKey(pin MarketplaceResourcePin) string {
	return pin.SourceID + "\x00" + pin.Kind + "\x00" + pin.Name
}

func marketplaceCollectionSelection(pin MarketplaceResourcePin) MarketplaceSelection {
	return MarketplaceSelection{SourceID: pin.SourceID, Name: pin.Name, Revision: pin.Revision}
}

func removeMarketplaceSelection(values []MarketplaceSelection, wanted MarketplaceSelection) []MarketplaceSelection {
	out := make([]MarketplaceSelection, 0, len(values))
	for _, value := range values {
		if value == wanted {
			continue
		}
		out = append(out, value)
	}
	return out
}

func appendMarketplaceSelection(values []MarketplaceSelection, wanted MarketplaceSelection) []MarketplaceSelection {
	for _, value := range values {
		if value == wanted {
			return values
		}
	}
	return append(values, wanted)
}
