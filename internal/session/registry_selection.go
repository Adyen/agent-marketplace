package session

import (
	"errors"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/skill"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var errRegistryResourceSelection = errors.New("registry resource selection is unavailable")

func RegistryResourceSelection(r Row) (config.RegistryEntry, config.ResourceLocator, error) {
	if r.Artifact.IsMarketplace || skill.ConfigKeyForKind(r.Artifact.Kind) == "" || strings.TrimSpace(r.Artifact.Name) == "" || strings.TrimSpace(r.RegistryURL) == "" {
		return config.RegistryEntry{}, config.ResourceLocator{}, errRegistryResourceSelection
	}
	entry := config.RegistryEntry{
		URL:           r.RegistryURL,
		Ref:           r.RegistryRef,
		Path:          r.RegistryPath,
		V4Kind:        r.Artifact.V4Kind,
		SourceHarness: r.Artifact.SourceHarness,
	}
	if entry.V4Kind != "" && entry.V4Kind != config.V4SourceGit && entry.V4Kind != config.V4SourceDirectory {
		return config.RegistryEntry{}, config.ResourceLocator{}, errRegistryResourceSelection
	}
	sourceHarness := strings.TrimSpace(entry.SourceHarness)
	if sourceHarness != "" && !targetmeta.IsIngesterTarget(sourceHarness) {
		return config.RegistryEntry{}, config.ResourceLocator{}, errRegistryResourceSelection
	}
	return entry, config.ResourceLocator{Kind: r.Artifact.Kind, Name: r.Artifact.Name}, nil
}

func EditRegistryResourceSelection(target config.LayerTarget, entry config.RegistryEntry, locator config.ResourceLocator, enable bool, rootOwnedOnly bool) error {
	if target.Source == config.UserPersonal && enable {
		// Repo-relative local paths are valid in repo/local configs but
		// meaningless machine-wide; refuse to write them to the user (global)
		// config when enabling. Disabling an existing broken entry is still
		// allowed so users can clean it up.
		if err := config.ValidateGlobalRegistryWrite(entry); err != nil {
			return err
		}
	}
	return pipeline.EditLayerAtPath(target, func(m *pipeline.LayerMutation) error {
		var binding config.SourceBinding
		found := false
		if !enable {
			var err error
			binding, found, err = m.LookupRegistry(entry)
			if err != nil {
				return err
			}
		}
		if !found {
			var err error
			binding, err = m.EnsureRegistry(entry)
			if err != nil {
				return err
			}
		}
		operation := config.SelectionExclude
		if enable {
			operation = config.SelectionInclude
		}
		change := config.SelectionChange{Operation: operation, Locators: []config.ResourceLocator{locator}}
		if rootOwnedOnly {
			return m.ApplyRootOwned(binding, change)
		}
		return m.Apply(binding, change)
	})
}
