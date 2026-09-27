package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func UpdateRegistryRefInConfig(configPath, url, oldRef, newRef string) (int, error) {
	url = strings.TrimSpace(url)
	oldRef = strings.TrimSpace(oldRef)
	newRef = strings.TrimSpace(newRef)
	switch {
	case url == "":
		return 0, fmt.Errorf("registry url is required")
	case oldRef == "":
		return 0, fmt.Errorf("old ref is required")
	case newRef == "":
		return 0, fmt.Errorf("new ref is required")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return 0, fmt.Errorf("read config %q: %w", configPath, err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		return 0, fmt.Errorf("stat config %q: %w", configPath, err)
	}

	doc, err := parseConfigDocument(configPath, data)
	if err != nil {
		return 0, err
	}

	root, err := ensureDocumentMapping(doc)
	if err != nil {
		return 0, fmt.Errorf("update config %q: %w", configPath, err)
	}

	_, registriesNode := findTopLevelSequence(root, "registries")
	if registriesNode == nil {
		return 0, nil
	}
	if registriesNode.Kind != yaml.SequenceNode {
		return 0, fmt.Errorf("update config %q: expected %q to be a sequence", configPath, "registries")
	}

	updated := 0
	for _, entryNode := range registriesNode.Content {
		if entryNode.Kind != yaml.MappingNode {
			continue
		}

		urlNode := findRegistryURLNode(entryNode)
		refNode := findMappingValue(entryNode, "ref")
		if urlNode == nil || refNode == nil || refNode.Kind != yaml.ScalarNode {
			continue
		}

		if NormalizeGitURL(strings.TrimSpace(urlNode.Value)) != NormalizeGitURL(url) || strings.TrimSpace(refNode.Value) != oldRef {
			continue
		}

		refNode.Value = newRef
		updated++
	}

	if updated == 0 {
		return 0, nil
	}

	if err := writeConfigDocument(configPath, info.Mode().Perm(), doc); err != nil {
		return 0, err
	}

	return updated, nil
}

// ValidateScopeBoundary returns an error if targetScope is repo-local
// (RepoCommitted or RepoPersonal) and skillSource is machine-scope
// (Organization or UserPersonal). Returns nil if the operation is allowed.
//
// This ensures repo/local layers cannot exclude org/user skills — those
// skills are installed at the user-wide scope and the exclude would be
// silently ineffective.
func ValidateScopeBoundary(skillSource Source, targetScope Source) error {
	if !targetScope.IsMachineScope() && skillSource.IsMachineScope() {
		return &ScopeBoundaryError{
			Source:     skillSource,
			Target:     targetScope,
			Suggestion: "Use --scope user to exclude at the user level.",
		}
	}
	return nil
}
