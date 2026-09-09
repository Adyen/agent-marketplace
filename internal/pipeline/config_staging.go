package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

type StagedConfigEdit struct {
	RequestedPath string
	Path          string
	Expected      dependencytxn.Generation
	Bytes         []byte
	Mode          fs.FileMode
}

type ConfigPathMapper func(string) (string, error)

func MutateConfigWithDependencies(ctx context.Context, repoRoot, localDir string, mutation func(ConfigPathMapper) error, reconcile DependencyReconciler) (retErr error) {
	staging, err := newConfigStagingManager()
	if err != nil {
		return err
	}
	defer closeConfigStaging(staging, "config mutation", &retErr)
	if err := mutation(staging.Path); err != nil {
		return err
	}
	edits, err := staging.Edits()
	if err != nil {
		return err
	}
	_, err = reconcile(ctx, repoRoot, localDir, false, edits)
	return err
}

type stagedConfig struct {
	requested   string
	destination string
	expected    dependencytxn.Generation
	shadow      string
	mode        fs.FileMode
}

type configStagingManager struct {
	root  string
	files map[string]stagedConfig
}

func newConfigStagingManager() (*configStagingManager, error) {
	root, err := os.MkdirTemp("", ".agent-marketplace-config-*")
	if err != nil {
		return nil, fmt.Errorf("create config staging directory: %w", err)
	}
	return &configStagingManager{root: root, files: make(map[string]stagedConfig)}, nil
}

func (m *configStagingManager) Path(destination string) (string, error) {
	requested, err := filepath.Abs(destination)
	if err != nil {
		return "", fmt.Errorf("resolve requested config destination %s: %w", destination, err)
	}
	requested = filepath.Clean(requested)
	canonical, err := canonicalConfigDestination(requested)
	if err != nil {
		return "", err
	}
	if staged, ok := m.files[canonical]; ok {
		return staged.shadow, nil
	}
	expected, err := dependencytxn.ReadGeneration(canonical)
	if err != nil {
		return "", fmt.Errorf("snapshot config generation %s: %w", canonical, err)
	}
	mode := fs.FileMode(0o644)
	data, err := os.ReadFile(canonical)
	if err == nil {
		if !expected.Exists || dependencytxn.Digest(data) != expected.Digest {
			return "", fmt.Errorf("%w: config changed while staging %s", dependencytxn.ErrGenerationConflict, canonical)
		}
		info, statErr := os.Stat(canonical)
		if statErr != nil {
			return "", fmt.Errorf("stat config destination %s: %w", canonical, statErr)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("config destination is not regular: %s", canonical)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read config destination %s: %w", canonical, err)
	} else if expected.Exists {
		return "", fmt.Errorf("%w: config disappeared while staging %s", dependencytxn.ErrGenerationConflict, canonical)
	}
	stageDir, err := os.MkdirTemp(m.root, "layer-*")
	if err != nil {
		return "", fmt.Errorf("create config layer staging directory: %w", err)
	}
	shadow := filepath.Join(stageDir, filepath.Base(canonical))
	if global, globalErr := config.GlobalConfigPath(); globalErr == nil {
		if global, canonicalErr := canonicalConfigDestination(global); canonicalErr == nil && global == canonical {
			if err := os.WriteFile(filepath.Join(stageDir, config.UserConfigShadowMarker), nil, 0o600); err != nil {
				return "", fmt.Errorf("mark staged user config: %w", err)
			}
		}
	}
	if data != nil {
		if err := os.WriteFile(shadow, data, mode); err != nil {
			return "", fmt.Errorf("seed staged config %s: %w", canonical, err)
		}
	}
	m.files[canonical] = stagedConfig{requested: requested, destination: canonical, expected: expected, shadow: shadow, mode: mode}
	return shadow, nil
}

func (m *configStagingManager) Edits() ([]StagedConfigEdit, error) {
	paths := make([]string, 0, len(m.files))
	for path := range m.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	edits := make([]StagedConfigEdit, 0, len(paths))
	for _, path := range paths {
		staged := m.files[path]
		data, err := os.ReadFile(staged.shadow)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read staged config %s: %w", path, err)
		}
		info, err := os.Stat(staged.shadow)
		if err != nil {
			return nil, fmt.Errorf("inspect staged config %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("staged config is not regular: %s", path)
		}
		edits = append(edits, StagedConfigEdit{RequestedPath: staged.requested, Path: path, Expected: staged.expected, Bytes: data, Mode: staged.mode})
	}
	return edits, nil
}

func (m *configStagingManager) Overlays() (map[string][]byte, error) {
	edits, err := m.Edits()
	if err != nil {
		return nil, err
	}
	overlays := make(map[string][]byte, len(edits))
	for _, edit := range edits {
		overlays[edit.Path] = edit.Bytes
	}
	return overlays, nil
}

func (m *configStagingManager) Close() error {
	if m == nil || m.root == "" {
		return nil
	}
	return os.RemoveAll(m.root)
}

func closeConfigStaging(m *configStagingManager, operation string, retErr *error) {
	if err := m.Close(); err != nil {
		slog.Error("config staging cleanup failed", "operation", operation, "error", err)
		*retErr = errors.Join(*retErr, fmt.Errorf("clean up staged config: %w", err))
	}
}

func canonicalConfigDestination(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config destination %s: %w", path, err)
	}
	return dependencytxn.CanonicalConfigDestination(filepath.Clean(absolute))
}
