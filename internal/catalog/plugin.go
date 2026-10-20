package catalog

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

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
	Invalid          []ir.PluginRecord
	DefinitionDigest string
	// Strict and Overlay carry the catalog entry's overlay input for
	// formats whose catalog declares an entry-overlay policy; they matter
	// only to decoders that apply that policy.
	Strict  bool
	Overlay map[string]any
	Refusal *diag.Diagnostic
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
	var loaded LoadedCatalog
	switch info.Role {
	case pluginpolicy.RolePackage:
		loaded, err = discoverPluginPackage(dir, format)
	case pluginpolicy.RoleCatalog:
		loaded, err = discoverPluginCatalog(dir, format)
	default:
		err = fmt.Errorf("plugin format %q is not a recognized package or catalog format", format)
	}
	if err != nil {
		return LoadedCatalog{}, err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return LoadedCatalog{}, fmt.Errorf("resolve plugin source root %q: %w", dir, err)
	}
	for i := range loaded.Entries {
		entry := &loaded.Entries[i]
		if entry.Kind != ir.KindPlugin || entry.Plugin == nil || entry.Plugin.Source.Kind != "path" {
			continue
		}
		packageRoot := root
		if entry.Plugin.Source.Path != "" && entry.Plugin.Source.Path != "." {
			packageRoot = filepath.Join(root, filepath.FromSlash(entry.Plugin.Source.Path))
		}
		rel, relErr := filepath.Rel(root, packageRoot)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return LoadedCatalog{}, fmt.Errorf("plugin package source %q escapes its catalog root", entry.Plugin.Source.Path)
		}
		entry.PackageRoot = filepath.Clean(packageRoot)
	}
	return loaded, nil
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
				Strict:           catEntry.Strict,
				Overlay:          catEntry.Overlay,
				Refusal:          catEntry.Diagnostic,
			},
		}
		if description, ok := catEntry.Overlay["description"].(string); ok {
			entry.Description = description
		}
		if catEntry.Installable && catEntry.Source.Kind == "path" {
			plugin, pluginDiags, decodeErr := plugindecode.DecodeEntry(filepath.Join(dir, filepath.FromSlash(catEntry.Source.Path)), format, catEntry)
			loaded.Diagnostics = append(loaded.Diagnostics, flattenDiagnostics(pluginDiags)...)
			switch {
			case decodeErr != nil:
				if coded, isCoded := diag.AsCodedError(decodeErr); isCoded && coded.Code == diag.CodePluginCatalogManifestConflict {
					refusal := coded.Diagnostic(catEntry.Name)
					entry.Plugin.Refusal = &refusal
				} else {
					entry.Plugin.Refusal = &diag.Diagnostic{
						Code:     diag.CodePluginFormatManifestInvalid,
						Severity: diag.SeverityWarning,
						Resource: catEntry.Name,
						Message:  fmt.Sprintf("catalog entry %q cannot be read as a %s package: %v", catEntry.Name, pluginFormat, decodeErr),
						Source:   diag.SourceRef{File: catEntry.Source.Path},
					}
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
	entry.Plugin.Invalid = append([]ir.PluginRecord(nil), plugin.Invalid...)
	if entry.Name == "" {
		entry.Name = plugin.Name()
	}
	entry.Plugin.Members = plugin.Members
	if entry.Description == "" {
		entry.Description = plugin.Manifest.Description
	}
}

func PluginInvalidMemberExclusionMatch(exclusion string, invalid []ir.PluginRecord) bool {
	kind, name, ok := strings.Cut(exclusion, "/")
	if !ok {
		return false
	}
	for _, record := range invalid {
		if record.Kind != "member" {
			continue
		}
		parts := strings.Split(filepath.ToSlash(filepath.Clean(record.Path)), "/")
		if len(parts) < 2 {
			continue
		}
		memberKind := ir.ResourceKind(-1)
		var memberName string
		switch parts[0] {
		case "skills":
			memberKind, memberName = ir.KindSkill, parts[1]
		case "commands":
			memberKind, memberName = ir.KindCommand, strings.TrimSuffix(path.Base(parts[len(parts)-1]), path.Ext(parts[len(parts)-1]))
		case "agents", "droids":
			memberKind, memberName = ir.KindSubagent, strings.TrimSuffix(path.Base(parts[len(parts)-1]), path.Ext(parts[len(parts)-1]))
		}
		if memberKind >= 0 && DecisionKind(kind) == memberKind && name == memberName {
			return true
		}
	}
	return false
}

// PluginMemberExclusionMatch reports whether an authored member exclusion
// names the member exactly, as a kind/name locator.
func PluginMemberExclusionMatch(exclusion string, member ir.PluginMember) bool {
	kind, name, ok := strings.Cut(exclusion, "/")
	if !ok || member.Resource == nil {
		return false
	}
	return DecisionKind(kind) == member.Resource.Kind() && name == member.Resource.Name()
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
