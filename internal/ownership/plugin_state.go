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
	InstanceKey    string `json:"instance_key"`
	Name           string `json:"name"`
	Target         string `json:"target"`
	Scope          string `json:"scope"`
	GenerationKey  string `json:"generation_key"`
	Delivery       string `json:"delivery,omitempty"`
	Refresh        string `json:"refresh,omitempty"`
	ReloadRequired bool   `json:"reload_required,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
}

type PluginState struct {
	Version  int                 `json:"version"`
	Instance PluginGenerationRef `json:"instance"`
	Outputs  []PluginOutputRef   `json:"outputs,omitempty"`
	Members  []PluginMemberRef   `json:"members,omitempty"`
}

type PluginOutputRef struct {
	Path         string   `json:"path"`
	ManifestPath string   `json:"manifest_path,omitempty"`
	Digest       string   `json:"digest"`
	Mode         uint32   `json:"mode"`
	LinkTarget   string   `json:"link_target,omitempty"`
	MarkerField  string   `json:"marker_field,omitempty"`
	MarkerValue  string   `json:"marker_value,omitempty"`
	Containers   []string `json:"containers,omitempty"`
	MarkerShapes []string `json:"marker_shapes,omitempty"`
}

type PluginMemberRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

var pluginDigestToken = regexp.MustCompile(`^[0-9a-f]{64}$`)
var pluginSourceRevisionToken = regexp.MustCompile(`^(sha1:[0-9a-f]{40}|sha256:[0-9a-f]{64})$`)
var pluginMarkerFieldToken = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

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
		instance.Delivery != "" && instance.Delivery != "native" && instance.Delivery != "decompose" ||
		instance.Refresh != "" && instance.Refresh != "restart-required" ||
		instance.ReloadRequired && instance.Delivery != "native" ||
		instance.SourceRevision != "" && !pluginSourceRevisionToken.MatchString(instance.SourceRevision) ||
		!validScope {
		return fmt.Errorf("ownership: invalid plugin generation reference")
	}
	seenOutputs := make(map[string]bool, len(state.Outputs))
	for _, output := range state.Outputs {
		if !filepath.IsAbs(output.Path) || filepath.Clean(output.Path) != output.Path ||
			!digestPattern.MatchString(output.Digest) || output.Mode&^0o777 != 0 || seenOutputs[output.Path] {
			return fmt.Errorf("ownership: invalid plugin output reference")
		}
		if output.ManifestPath != "" {
			clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(output.ManifestPath)))
			if clean != output.ManifestPath || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(output.ManifestPath) {
				return fmt.Errorf("ownership: invalid plugin output manifest path")
			}
		}
		if output.MarkerField != "" || output.MarkerValue != "" || len(output.Containers) > 0 || len(output.MarkerShapes) > 0 {
			if !pluginMarkerFieldToken.MatchString(output.MarkerField) ||
				output.MarkerField != "_managed_by" && output.MarkerField != "managed-by" ||
				output.MarkerValue != "agent-marketplace-plugin-"+instance.InstanceKey ||
				len(output.Containers) == 0 || len(output.Containers) != len(output.MarkerShapes) {
				return fmt.Errorf("ownership: invalid plugin aggregate output reference")
			}
			seenContainers := make(map[string]bool, len(output.Containers))
			for i, container := range output.Containers {
				shape := output.MarkerShapes[i]
				if !safeToken.MatchString(container) || seenContainers[container] ||
					shape != "object" && shape != "array" && shape != "map-arrays" {
					return fmt.Errorf("ownership: invalid plugin aggregate container")
				}
				seenContainers[container] = true
			}
		}
		if output.LinkTarget != "" && (!filepath.IsAbs(output.LinkTarget) || filepath.Clean(output.LinkTarget) != output.LinkTarget) {
			return fmt.Errorf("ownership: invalid plugin output link target")
		}
		seenOutputs[output.Path] = true
	}
	seenMembers := make(map[string]bool, len(state.Members))
	for _, member := range state.Members {
		key := member.Kind + "\x00" + member.Name
		if !safeToken.MatchString(member.Kind) || !safeToken.MatchString(member.Name) || seenMembers[key] {
			return fmt.Errorf("ownership: invalid plugin member reference")
		}
		seenMembers[key] = true
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
			if !isPluginState {
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
			}
		}
	}
	return refs, nil
}
