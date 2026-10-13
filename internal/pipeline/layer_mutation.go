package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
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
	return m.editor.Apply(binding, change)
}

func (m *LayerMutation) LookupRegistry(entry config.RegistryEntry) (config.SourceBinding, bool, error) {
	return m.editor.LookupRegistry(entry)
}

func (m *LayerMutation) RemoveSource(binding config.SourceBinding) error {
	return m.editor.RemoveSource(binding)
}

func (m *LayerMutation) ClearInstallExclude(entry config.RegistryEntry, locator config.ResourceLocator) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.ClearInstallExclude(entry, locator)
}

func (m *LayerMutation) ClearUnboundExclude(locator config.ResourceLocator) error {
	return m.editor.ClearUnboundExclude(locator)
}

func (m *LayerMutation) UnboundIncludeInherited(locator config.ResourceLocator) (bool, error) {
	return m.editor.UnboundIncludeInherited(locator)
}

func (m *LayerMutation) RemoveUnboundInclude(locator config.ResourceLocator) error {
	return m.editor.RemoveUnboundInclude(locator)
}

func (m *LayerMutation) SetUnboundExclude(locator config.ResourceLocator) error {
	return m.editor.SetUnboundExclude(locator)
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
	if candidate.Legacy {
		warnLegacyLayerMutation(target.Source, target.DeclaringPath)
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
