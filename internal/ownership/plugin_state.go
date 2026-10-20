package ownership

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
)

type PluginGenerationRef struct {
	InstanceKey   string `json:"instance_key"`
	Name          string `json:"name"`
	Target        string `json:"target"`
	Scope         string `json:"scope"`
	GenerationKey string `json:"generation_key"`
}

type PluginState struct {
	Version  int                 `json:"version"`
	Instance PluginGenerationRef `json:"instance"`
}

var pluginDigestToken = regexp.MustCompile(`^[0-9a-f]{64}$`)

func PluginStatePath(instanceKey string) (string, error) {
	if !pluginDigestToken.MatchString(instanceKey) {
		return "", fmt.Errorf("ownership: invalid plugin instance key")
	}
	stateRoot, err := StateRoot()
	if err != nil {
		return "", err
	}
	stateRoot, err = CanonicalRoot(stateRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(stateRoot, ownershipDirName, "plugin-packages", instanceKey+".json"), nil
}

func ReadPluginState(instanceKey string) (PluginState, string, dependencytxn.Generation, os.FileMode, error) {
	statePath, err := PluginStatePath(instanceKey)
	if err != nil {
		return PluginState{}, "", dependencytxn.Generation{}, 0, err
	}
	data, generation, mode, err := dependencytxn.ReadSnapshot(statePath)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !generation.Exists) {
		return PluginState{Version: 1, Instance: PluginGenerationRef{InstanceKey: instanceKey}}, statePath, dependencytxn.Generation{}, 0, nil
	}
	if err != nil {
		return PluginState{}, statePath, generation, mode, err
	}
	var state PluginState
	if err := json.Unmarshal(data, &state); err != nil {
		return PluginState{}, statePath, generation, mode, fmt.Errorf("ownership: invalid plugin generation state")
	}
	if err := validatePluginState(&state); err != nil {
		return PluginState{}, statePath, generation, mode, err
	}
	if state.Instance.InstanceKey != instanceKey {
		return PluginState{}, statePath, generation, mode, fmt.Errorf("ownership: plugin generation state filename does not match its instance key")
	}
	return state, statePath, generation, mode, nil
}

func EncodePluginState(state PluginState) ([]byte, error) {
	if state.Version != 1 {
		return nil, fmt.Errorf("ownership: unsupported plugin state version %d", state.Version)
	}
	if err := validatePluginState(&state); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func validatePluginState(state *PluginState) error {
	instance := state.Instance
	validScope := instance.Scope == "machine"
	if strings.HasPrefix(instance.Scope, "repo:") {
		validScope = pluginDigestToken.MatchString(strings.TrimPrefix(instance.Scope, "repo:"))
	}
	if state.Version != 1 ||
		!pluginDigestToken.MatchString(instance.InstanceKey) ||
		!pluginDigestToken.MatchString(instance.GenerationKey) ||
		!safeToken.MatchString(instance.Name) ||
		!safeToken.MatchString(instance.Target) ||
		!validScope {
		return fmt.Errorf("ownership: invalid plugin generation reference")
	}
	return nil
}

func ReadPluginInstanceStates() ([]PluginGenerationRef, error) {
	stateRoot, err := StateRoot()
	if err != nil {
		return nil, err
	}
	stateRoot, err = CanonicalRoot(stateRoot)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(stateRoot, ownershipDirName, "plugin-packages")
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("ownership: unsafe plugin package state directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var refs []PluginGenerationRef
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		instanceKey := strings.TrimSuffix(entry.Name(), ".json")
		if !pluginDigestToken.MatchString(instanceKey) {
			return nil, fmt.Errorf("ownership: invalid plugin instance state filename")
		}
		state, _, _, _, err := ReadPluginState(instanceKey)
		if err != nil {
			return nil, err
		}
		refs = append(refs, state.Instance)
	}
	return refs, nil
}

func ReadAllPluginGenerationRefs() ([]PluginGenerationRef, error) {
	refs, err := ReadPluginInstanceStates()
	if err != nil {
		return nil, err
	}
	stateRoot, err := StateRoot()
	if err != nil {
		return nil, err
	}
	stateRoot, err = CanonicalRoot(stateRoot)
	if err != nil {
		return nil, err
	}
	recordDirs, err := os.ReadDir(filepath.Join(stateRoot, ownershipDirName))
	if errors.Is(err, os.ErrNotExist) {
		return refs, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range recordDirs {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("ownership: unsafe plugin generation ownership entry")
		}
		if !entry.IsDir() || entry.Name() == "plugin-packages" {
			continue
		}
		statePath := filepath.Join(stateRoot, ownershipDirName, entry.Name(), stateFileName)
		data, generation, _, err := dependencytxn.ReadSnapshot(statePath)
		if err != nil {
			return nil, err
		}
		if !generation.Exists {
			continue
		}
		var record Record
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("ownership: invalid aggregate ownership state %q", statePath)
		}
		if err := validateRecord(&record, record.CanonicalRoot, record.RootKey); err != nil {
			return nil, err
		}
		if record.RootKey != entry.Name() {
			return nil, fmt.Errorf("ownership: aggregate ownership filename does not match its root key")
		}
		for _, file := range record.Files {
			for _, owned := range file.Entries {
				refs = append(refs, owned.PluginGenerations...)
			}
		}
	}
	return refs, nil
}

func ReadPendingPluginGenerationRefs() ([]PluginGenerationRef, error) {
	stateRoot, err := StateRoot()
	if err != nil {
		return nil, err
	}
	stateRoot, err = CanonicalRoot(stateRoot)
	if err != nil {
		return nil, err
	}
	pluginStateDir := filepath.Join(stateRoot, ownershipDirName, "plugin-packages")
	dependencyStateDir, err := dependencytxn.StateDir()
	if err != nil {
		return nil, err
	}
	journals, err := filepath.Glob(filepath.Join(dependencyStateDir, ".dependencytxn-*.json"))
	if err != nil {
		return nil, err
	}
	var refs []PluginGenerationRef
	for _, journalPath := range journals {
		journalBytes, _, _, err := dependencytxn.ReadSnapshot(journalPath)
		if err != nil {
			return nil, err
		}
		var journal struct {
			Files []struct {
				Path string `json:"path"`
				Temp string `json:"temp"`
			} `json:"files"`
		}
		if err := json.Unmarshal(journalBytes, &journal); err != nil {
			return nil, fmt.Errorf("ownership: invalid pending dependency journal")
		}
		for _, file := range journal.Files {
			isPluginState := filepath.Dir(file.Path) == pluginStateDir && filepath.Ext(file.Path) == ".json"
			isRootOwnershipState := filepath.Base(file.Path) == stateFileName &&
				filepath.Dir(filepath.Dir(file.Path)) == filepath.Join(stateRoot, ownershipDirName)
			if !isPluginState && !isRootOwnershipState {
				continue
			}
			candidates := []string{file.Path}
			if file.Temp != "" && filepath.Dir(file.Temp) == filepath.Dir(file.Path) && strings.HasPrefix(filepath.Base(file.Temp), ".dependencytxn-") {
				candidates = append(candidates, file.Temp)
			}
			for _, candidate := range candidates {
				data, generation, _, err := dependencytxn.ReadSnapshot(candidate)
				if err != nil {
					return nil, err
				}
				if !generation.Exists {
					continue
				}
				if isPluginState {
					var state PluginState
					if err := json.Unmarshal(data, &state); err != nil {
						return nil, fmt.Errorf("ownership: invalid pending plugin generation state")
					}
					if err := validatePluginState(&state); err != nil {
						return nil, err
					}
					if filepath.Base(file.Path) != state.Instance.InstanceKey+".json" {
						return nil, fmt.Errorf("ownership: pending plugin generation state filename does not match its instance key")
					}
					refs = append(refs, state.Instance)
					continue
				}
				var record Record
				if err := json.Unmarshal(data, &record); err != nil {
					return nil, fmt.Errorf("ownership: invalid pending aggregate ownership state")
				}
				if err := validateRecord(&record, record.CanonicalRoot, record.RootKey); err != nil {
					return nil, err
				}
				for _, file := range record.Files {
					for _, owned := range file.Entries {
						refs = append(refs, owned.PluginGenerations...)
					}
				}
			}
		}
	}
	return refs, nil
}
