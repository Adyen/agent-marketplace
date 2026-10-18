package catalog

import (
	"fmt"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// PluginEntry carries plugin-package metadata for Kind == ir.KindPlugin
// catalog entries. A non-nil Refusal marks the entry listed but not
// installable; a "git" Source names an exact external acquisition
// requirement the resolver refuses until external plugin acquisition lands.
type PluginEntry struct {
	Format           string
	Source           plugindecode.CatalogSource
	Members          []ir.PluginMember
	DefinitionDigest string
	Refusal          *diag.Diagnostic
}

// DiscoverPluginSource enumerates a format source: package formats decode
// the source root as one plugin; catalog formats read the native catalog and
// decode each in-repository entry. External entries stay listed with their
// exact requirement; refused and duplicate entries stay listed with their
// diagnostic.
func DiscoverPluginSource(dir, format string) (LoadedCatalog, error) {
	info, known, err := pluginpolicy.LookupPackageFormat(format)
	if err != nil {
		return LoadedCatalog{}, err
	}
	if !known {
		return LoadedCatalog{}, fmt.Errorf("plugin format %q is not a recognized package or catalog format", format)
	}
	switch info.Role {
	case pluginpolicy.RolePackage:
		return discoverPluginPackage(dir, format)
	case pluginpolicy.RoleCatalog:
		return discoverPluginCatalog(dir, format)
	}
	return LoadedCatalog{}, fmt.Errorf("plugin format %q is not a recognized package or catalog format", format)
}

func discoverPluginPackage(dir, format string) (LoadedCatalog, error) {
	plugin, diags, err := plugindecode.Decode(dir, format)
	loaded := LoadedCatalog{Diagnostics: flattenDiagnostics(diags)}
	if err != nil {
		return loaded, err
	}
	loaded.Entries = []Entry{pluginEntryFromPackage(plugin, plugindecode.CatalogSource{Kind: "path", Path: "."})}
	return loaded, nil
}

func discoverPluginCatalog(dir, format string) (LoadedCatalog, error) {
	decoded, diags, err := plugindecode.DecodeCatalog(dir, format)
	loaded := LoadedCatalog{Diagnostics: flattenDiagnostics(diags)}
	if err != nil {
		return loaded, err
	}
	pluginFormat := plugindecode.CatalogPluginFormat(format)
	for _, catEntry := range decoded.Entries {
		entry := Entry{
			Kind: ir.KindPlugin,
			Name: catEntry.Name,
			Plugin: &PluginEntry{
				Format:           pluginFormat,
				Source:           catEntry.Source,
				DefinitionDigest: catEntry.DefinitionDigest,
				Refusal:          catEntry.Diagnostic,
			},
		}
		if description, ok := catEntry.Overlay["description"].(string); ok {
			entry.Description = description
		}
		if catEntry.Installable && catEntry.Source.Kind == "path" {
			plugin, pluginDiags, decodeErr := plugindecode.Decode(filepath.Join(dir, filepath.FromSlash(catEntry.Source.Path)), pluginFormat)
			loaded.Diagnostics = append(loaded.Diagnostics, flattenDiagnostics(pluginDiags)...)
			switch {
			case decodeErr != nil:
				entry.Plugin.Refusal = &diag.Diagnostic{
					Code:     diag.CodePluginFormatManifestInvalid,
					Severity: diag.SeverityWarning,
					Resource: catEntry.Name,
					Message:  fmt.Sprintf("catalog entry %q cannot be read as a %s package: %v", catEntry.Name, pluginFormat, decodeErr),
					Source:   diag.SourceRef{File: catEntry.Source.Path},
				}
				loaded.Diagnostics = append(loaded.Diagnostics, *entry.Plugin.Refusal)
			default:
				applyPlugin(&entry, plugin)
				entry.Path = filepath.ToSlash(filepath.Join(catEntry.Source.Path, pluginManifestPath(pluginFormat)))
			}
		}
		loaded.Entries = append(loaded.Entries, entry)
	}
	return loaded, nil
}

// pluginEntryFromPackage builds the single catalog entry of a package-format
// source: the source root is the package root.
func pluginEntryFromPackage(plugin *ir.Plugin, source plugindecode.CatalogSource) Entry {
	entry := Entry{Kind: ir.KindPlugin, Description: plugin.Manifest.Description}
	applyPlugin(&entry, plugin)
	entry.Plugin.Source = source
	entry.Path = pluginManifestPath(plugin.Format)
	return entry
}

func applyPlugin(entry *Entry, plugin *ir.Plugin) {
	if entry.Plugin == nil {
		entry.Plugin = &PluginEntry{Format: plugin.Format}
	}
	entry.Resource = plugin
	if entry.Name == "" {
		entry.Name = plugin.Name()
	}
	entry.Plugin.Members = plugin.Members
	if entry.Description == "" {
		entry.Description = plugin.Manifest.Description
	}
}

func pluginManifestPath(format string) string {
	if layout, ok := target.LookupPackageLayout(format); ok {
		return layout.ManifestPath
	}
	return "plugin.json"
}

func flattenDiagnostics(diags []*diag.Diagnostic) []diag.Diagnostic {
	out := make([]diag.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d != nil {
			out = append(out, *d)
		}
	}
	return out
}
