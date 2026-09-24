package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"

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

func (m *LayerMutation) RemoveSource(binding config.SourceBinding) error {
	if m == nil || m.editor == nil {
		return errors.New("layer mutation has no open editor")
	}
	return m.editor.RemoveSource(binding)
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

func editLayerAtPath(target config.LayerTarget, mutate func(*LayerMutation) error) error {
	if mutate == nil {
		return errors.New("layer mutation callback is required")
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
