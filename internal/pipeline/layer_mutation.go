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
	if m == nil || m.editor == nil {
		return config.SourceBinding{}, errors.New("layer mutation has no open editor")
	}
	return m.editor.EnsureRegistry(entry)
}

func (m *LayerMutation) Apply(binding config.SourceBinding, change config.SelectionChange) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.Apply(binding, change)
}

func (m *LayerMutation) LookupRegistry(entry config.RegistryEntry) (config.SourceBinding, bool, error) {
	if m == nil || m.editor == nil {
		return config.SourceBinding{}, false, errors.New("layer mutation has no open editor")
	}
	return m.editor.LookupRegistry(entry)
}

func (m *LayerMutation) RemoveSource(binding config.SourceBinding) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.RemoveSource(binding)
}

func (m *LayerMutation) ClearUnboundExclude(locator config.ResourceLocator) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.ClearUnboundExclude(locator)
}

func (m *LayerMutation) UnboundIncludeInherited(locator config.ResourceLocator) (bool, error) {
	if m == nil || m.editor == nil {
		return false, errors.New("layer mutation has no open editor")
	}
	return m.editor.UnboundIncludeInherited(locator)
}

func (m *LayerMutation) RemoveUnboundInclude(locator config.ResourceLocator) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.RemoveUnboundInclude(locator)
}

func (m *LayerMutation) SetUnboundExclude(locator config.ResourceLocator, excluded bool) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.SetUnboundExclude(locator, excluded)
}

func (m *LayerMutation) SetLayerExclude(locator config.ResourceLocator, entries []config.RegistryEntry, excluded bool) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.SetLayerExclude(locator, entries, excluded)
}

func MutateLayer(ctx context.Context, target LayerMutationTarget, mutate func(*LayerMutation) error) error {
	if mutate == nil {
		return errors.New("layer mutation callback is required")
	}
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
		return editLayerAtPath(config.LayerTarget{Source: target.Source, StagedPath: staged, DeclaringPath: target.ConfigPath}, mutate)
	}, target.Reconcile)
}

func EditLayerAtPath(target config.LayerTarget, mutate func(*LayerMutation) error) error {
	return editLayerAtPath(target, mutate)
}

func editLayerAtPath(target config.LayerTarget, mutate func(*LayerMutation) error) error {
	if mutate == nil {
		return errors.New("layer mutation callback is required")
	}
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
