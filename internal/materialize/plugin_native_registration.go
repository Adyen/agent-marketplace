package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/packagegen"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type nativePluginCatalogEntry struct {
	name, generationPath string
}

type nativePluginCatalog struct {
	name, path, scopeOwner string
	target                 hcc.Target
	entries                []nativePluginCatalogEntry
}

func nativePluginCatalogFor(target hcc.Target, scope string, entries []nativePluginCatalogEntry) (nativePluginCatalog, error) {
	stateRoot, err := packagegen.StateRoot()
	if err != nil {
		return nativePluginCatalog{}, err
	}
	canonicalRoot, err := ownership.CanonicalRoot(stateRoot)
	if err != nil {
		return nativePluginCatalog{}, err
	}
	sum := sha256.Sum256([]byte("agent-marketplace/native-plugin-catalog/v1\x00" + scope))
	name := "am-" + hex.EncodeToString(sum[:8])
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name != entries[j].name {
			return entries[i].name < entries[j].name
		}
		return entries[i].generationPath < entries[j].generationPath
	})
	unique := entries[:0]
	for _, entry := range entries {
		if len(unique) > 0 && unique[len(unique)-1].name == entry.name {
			continue
		}
		unique = append(unique, entry)
	}
	return nativePluginCatalog{
		name: name, path: filepath.Join(canonicalRoot, "native-plugins", string(target), name),
		scopeOwner: packagegen.InstanceKey("native-plugin-catalog", target, scope),
		target:     target,
		entries:    unique,
	}, nil
}

func (catalog nativePluginCatalog) manifest() ([]byte, error) {
	if catalog.target == target.TargetClaude || catalog.target == target.TargetFactory {
		type entry struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		}
		document := struct {
			Name  string `json:"name"`
			Owner struct {
				Name string `json:"name"`
			} `json:"owner"`
			Plugins []entry `json:"plugins"`
		}{Name: catalog.name}
		document.Owner.Name = "Agent Marketplace"
		for _, item := range catalog.entries {
			document.Plugins = append(document.Plugins, entry{
				Name: item.name, Source: nativePluginCatalogEntryPath(item.name),
			})
		}
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	}
	if catalog.target == target.TargetCodex {
		type entry struct {
			Name     string            `json:"name"`
			Source   map[string]string `json:"source"`
			Policy   map[string]string `json:"policy"`
			Category string            `json:"category"`
		}
		document := struct {
			Name    string  `json:"name"`
			Plugins []entry `json:"plugins"`
		}{Name: catalog.name}
		for _, item := range catalog.entries {
			document.Plugins = append(document.Plugins, entry{
				Name: item.name, Source: map[string]string{"source": "local", "path": nativePluginCatalogEntryPath(item.name)},
				Policy: map[string]string{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, Category: "Productivity",
			})
		}
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	}
	return nil, fmt.Errorf("native plugin catalog has no manifest shape for target %s", catalog.target)
}

func nativePluginCatalogEntryPath(name string) string {
	return "./plugins/" + name
}

func nativePluginCatalogParticipants(catalog nativePluginCatalog) ([]dependencytxn.DesiredFile, error) {
	if catalog.path == "" {
		return nil, nil
	}
	manifest, err := catalog.manifest()
	if err != nil {
		return nil, err
	}
	var manifestPath string
	switch catalog.target {
	case target.TargetClaude:
		manifestPath = filepath.Join(catalog.path, ".claude-plugin", "marketplace.json")
	case target.TargetFactory:
		manifestPath = filepath.Join(catalog.path, ".factory-plugin", "marketplace.json")
	case target.TargetCodex:
		manifestPath = filepath.Join(catalog.path, ".agents", "plugins", "marketplace.json")
	default:
		return nil, fmt.Errorf("native plugin catalog has no target manifest layout")
	}
	existing, generation, mode, err := dependencytxn.ReadSnapshot(manifestPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read native plugin catalog: %w", err)
	}
	if string(existing) == string(manifest) && generation.Exists {
		return nil, nil
	}
	return []dependencytxn.DesiredFile{{
		Path: manifestPath, Expected: generation, ExpectedMode: mode, Bytes: manifest, Mode: 0o600,
	}}, nil
}

func nativeRegistrationEntries(key outputKey, repoRoot string, ref ownership.PluginGenerationRef, catalog nativePluginCatalog, destinations []harness.Destination) ([]ownedJSONEntry, error) {
	registration, ok := target.NativePluginRegistration(target.Target(key.target), key.machineScope)
	if !ok {
		return nil, nil
	}
	if len(destinations) == 0 {
		var err error
		destinations, err = harness.ResolveDestinations(key.target, key.machineScope, repoRoot)
		if err != nil {
			return nil, err
		}
	}
	var destination harness.Destination
	var rel string
	for _, candidate := range destinations {
		prefix := filepath.ToSlash(candidate.Prefix)
		registered := registration.Path
		if prefix == "." || prefix == "" {
			rel = registered
			destination = candidate
			break
		}
		if registered == prefix {
			rel = filepath.Base(registered)
			destination = candidate
			break
		}
		if strings.HasPrefix(registered, prefix+"/") {
			rel = strings.TrimPrefix(registered, prefix+"/")
			destination = candidate
			break
		}
	}
	if destination.Root == "" || rel == "" {
		return nil, fmt.Errorf("native plugin registration has no destination")
	}
	var entries []ownedJSONEntry
	add := func(container, entryKey string, value any, owner string) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		entries = append(entries, ownedJSONEntry{
			rel: rel, kind: ownership.KindPluginRegistration, container: container,
			key: entryKey, raw: raw, identity: ownership.PluginRegistrationIdentityDigest(container, entryKey),
			resource: ref.Name, pluginInstanceKey: owner,
		})
		return nil
	}
	if catalog.name == "" {
		return nil, nil
	}
	switch registration.Shape {
	case target.NativeRegistrationClaudeSettings:
		if len(catalog.entries) > 0 {
			if err := add("extraKnownMarketplaces", catalog.name, map[string]any{
				"source": map[string]string{"source": "directory", "path": catalog.path},
			}, catalog.scopeOwner); err != nil {
				return nil, err
			}
		}
		if ref.Delivery == "native" {
			if err := add("enabledPlugins", ref.Name+"@"+catalog.name, true, ref.InstanceKey); err != nil {
				return nil, err
			}
		}
	case target.NativeRegistrationFactorySettings:
		if len(catalog.entries) > 0 {
			if err := add("extraKnownMarketplaces", catalog.name, map[string]any{
				"source": map[string]string{"source": "local", "path": catalog.path},
			}, catalog.scopeOwner); err != nil {
				return nil, err
			}
		}
		if ref.Delivery == "native" {
			if err := add("enabledPlugins", ref.Name+"@"+catalog.name, true, ref.InstanceKey); err != nil {
				return nil, err
			}
		}
	case target.NativeRegistrationCodexConfig:
		if len(catalog.entries) > 0 {
			if err := add("marketplaces", catalog.name, map[string]string{
				"source_type": "local", "source": catalog.path,
			}, catalog.scopeOwner); err != nil {
				return nil, err
			}
		}
		if ref.Delivery == "native" {
			if err := add("plugins", ref.Name+"@"+catalog.name, map[string]bool{"enabled": true}, ref.InstanceKey); err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

func nativePluginCatalogSelections(resolved []resolver.ResolvedArtifact, repoRoot string) (map[string][]nativePluginCatalogEntry, error) {
	selections := make(map[string][]nativePluginCatalogEntry)
	for _, resource := range resolved {
		pkg := resource.PluginPackage
		if pkg == nil || pkg.Delivery != "native" || pkg.Package == nil {
			continue
		}
		scope := pluginGenerationScope(resource, repoRoot)
		for _, target := range resolver.HarnessTargets(resource.Harnesses) {
			generation := pkg.TargetGenerations[string(target)]
			if generation.GenerationPath == "" {
				continue
			}
			selections[nativePluginCatalogSelectionKey(target, scope)] = append(selections[nativePluginCatalogSelectionKey(target, scope)], nativePluginCatalogEntry{
				name: pkg.Name, generationPath: generation.GenerationPath,
			})
		}
	}
	return selections, nil
}

func nativePluginCatalogSelectionKey(target hcc.Target, scope string) string {
	return string(target) + "\x00" + scope
}
