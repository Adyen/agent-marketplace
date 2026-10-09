package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type LayerMutationTarget struct {
	RepoRoot       string
	LocalConfigDir string
	ConfigPath     string
	Source         config.Source
	Reconcile      DependencyReconciler
}

type LayerMutation struct {
	editor *config.LayerEditor
}

func (m *LayerMutation) EnsureRegistry(entry config.RegistryEntry) (config.SourceBinding, error) {
	return m.editor.EnsureRegistry(entry)
}

func (m *LayerMutation) Apply(binding config.SourceBinding, change config.SelectionChange) error {
	if change.Operation == config.SelectionInclude && change.Tag == "" && len(change.Locators) > 0 {
		group := m.editor.RenderedV4().Groups[binding.Name]
		selectedByAll := group.All != nil && *group.All
		for _, locator := range change.Locators {
			// Collections require explicit requests even in an all:true group.
			selectedByAll = selectedByAll && locator.Kind != ir.KindCollection
		}
		if selectedByAll {
			err := m.editor.Apply(binding, config.SelectionChange{Operation: config.SelectionClearExclude, Locators: change.Locators})
			var validation *config.V4ValidationError
			if !errors.As(err, &validation) || validation.Code != "v4.edit-exclusion" {
				return err
			}
			// An inherited exclusion may need a later explicit request. Let
			// the v4 editor retain its existing ordered-decision authority.
		}
	}
	return m.editor.Apply(binding, change)
}

func (m *LayerMutation) LookupRegistry(entry config.RegistryEntry) (config.SourceBinding, bool, error) {
	return m.editor.LookupRegistry(entry)
}

func (m *LayerMutation) RemoveSource(binding config.SourceBinding) error {
	return m.editor.RemoveSource(binding)
}

func (m *LayerMutation) RemoveMarketplaceRegistration(entry config.MarketplaceEntry) error {
	return m.editor.RemoveMarketplaceRegistration(entry)
}

func (m *LayerMutation) ClearInstallExclude(entry config.RegistryEntry, locator config.ResourceLocator) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.ClearInstallExclude(entry, locator)
}

func (m *LayerMutation) SetLayerExclude(locator config.ResourceLocator, entries []config.RegistryEntry) error {
	return m.editor.SetLayerExclude(locator, entries)
}

// EnsureMarketplaceSource binds a Marketplace endpoint source in the staged
// layer so pins can be included in the same transaction.
func (m *LayerMutation) EnsureMarketplaceSource(marketplaceURL string) (config.SourceBinding, error) {
	return m.editor.EnsureMarketplaceSource(marketplaceURL)
}

// LookupMarketplaceSource resolves a Marketplace endpoint source already
// declared in the staged layer.
func (m *LayerMutation) LookupMarketplaceSource(marketplaceURL string) (config.SourceBinding, bool, error) {
	return m.editor.LookupMarketplaceSource(marketplaceURL)
}

// IncludeMarketplacePins adds direct pins to a bound Marketplace source.
func (m *LayerMutation) IncludeMarketplacePins(binding config.SourceBinding, pins []config.MarketplaceResourcePin) error {
	return m.editor.IncludeMarketplacePins(binding, pins)
}

// RemoveMarketplacePins removes pins from a bound Marketplace source.
func (m *LayerMutation) RemoveMarketplacePins(binding config.SourceBinding, pins []config.MarketplaceResourcePin) error {
	return m.editor.RemoveMarketplacePins(binding, pins)
}

func (m *LayerMutation) ExcludeMarketplacePin(binding config.SourceBinding, pin config.MarketplaceResourcePin) error {
	return m.editor.ExcludeMarketplacePin(binding, pin)
}

func MutateLayer(ctx context.Context, target LayerMutationTarget, mutate func(*LayerMutation) error) error {
	if target.Reconcile == nil {
		return errors.New("layer mutation reconciler is required")
	}
	if target.RepoRoot == "" {
		return errors.New("layer mutation repo root is required")
	}
	if target.ConfigPath == "" {
		return errors.New("layer mutation config path is required")
	}
	if err := RecoverDependencyTransactions(ctx); err != nil {
		return err
	}
	if _, err := config.LoadV4ConfigIfPresent(target.ConfigPath, target.Source); err != nil {
		return err
	}
	return MutateConfigWithDependencies(ctx, target.RepoRoot, target.LocalConfigDir, func(mapper ConfigPathMapper) error {
		staged, err := mapper(target.ConfigPath)
		if err != nil {
			return err
		}
		return EditLayerAtPath(config.LayerTarget{Source: target.Source, StagedPath: staged, DeclaringPath: target.ConfigPath}, mutate)
	}, target.Reconcile)
}

func EditLayerAtPath(target config.LayerTarget, mutate func(*LayerMutation) error) error {
	if mutate == nil {
		return errors.New("layer mutation callback is required")
	}
	if target.StagedPath == target.DeclaringPath {
		ctx := context.Background()
		if err := RecoverDependencyTransactions(ctx); err != nil {
			return err
		}
		repoRoot, localDir := config.FindRepoRoot(), config.FindLocalConfigDir()
		if target.Source == config.RepoCommitted || target.Source == config.RepoPersonal {
			repoRoot = filepath.Dir(filepath.Dir(target.DeclaringPath))
			localDir = repoRoot
		}
		if err := ValidateConsumerIntent(ctx, repoRoot, localDir); err != nil {
			return err
		}
	}
	_, statErr := os.Stat(target.StagedPath)
	seeded := errors.Is(statErr, os.ErrNotExist)
	if err := seedMissingLayerRoot(target); err != nil {
		return err
	}
	editor, err := config.OpenLayerEditor(target)
	if err != nil {
		return err
	}
	if err := mutate(&LayerMutation{editor: editor}); err != nil {
		return err
	}
	candidate, err := editor.Render()
	if err != nil {
		return err
	}
	if !candidate.Changed {
		if seeded {
			if err := os.Remove(target.StagedPath); err != nil {
				return fmt.Errorf("discard unchanged staged config %s: %w", target.StagedPath, err)
			}
		}
		return nil
	}
	if err := os.WriteFile(target.StagedPath, candidate.Bytes, candidate.Mode); err != nil {
		return fmt.Errorf("write staged config %s: %w", target.StagedPath, err)
	}
	return nil
}

func seedMissingLayerRoot(target config.LayerTarget) error {
	info, err := os.Stat(target.StagedPath)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("config %s is not a regular file", target.StagedPath)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var body string
	switch target.Source {
	case config.UserPersonal:
		body = "apiVersion: agent-marketplace/v4\nharnesses: [" + strings.Join(config.DefaultHarnesses(), ", ") + "]\n"
	case config.RepoCommitted, config.RepoPersonal:
		body = "apiVersion: agent-marketplace/v4\n"
	default:
		return fmt.Errorf("cannot initialize missing config %s for source %d", target.StagedPath, target.Source)
	}
	if err := os.MkdirAll(filepath.Dir(target.StagedPath), 0o755); err != nil {
		return fmt.Errorf("initialize config %s: %w", target.StagedPath, err)
	}
	if err := os.WriteFile(target.StagedPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("initialize config %s: %w", target.StagedPath, err)
	}
	return nil
}
