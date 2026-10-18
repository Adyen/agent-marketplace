package sourcerevision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// PluginExternal is the owning-catalog binding of an external-entry lock
// record: the catalog source identity and the selected entry name. The entry
// overlay digest is recomputed from the owning catalog at verification.
type PluginExternal struct {
	CatalogKind   string // "git" or "directory"
	CatalogURL    string
	CatalogRef    string
	CatalogCommit string
	CatalogRoot   string
	EntryName     string
	EntryDigest   string
}

// PluginVerifyContext carries the plugin-v1 inputs a locked root's
// verification needs beyond the snapshot: the declared selection for format
// roots, or the owning-catalog binding for external-entry roots.
type PluginVerifyContext struct {
	Spec     *catalog.PluginSelectionSpec
	External *PluginExternal
}

// PluginExternalEntry is one selected catalog entry whose origin is an
// external repository; the caller acquires it through the exact Git path.
type PluginExternalEntry struct {
	Name             string
	Source           plugindecode.CatalogSource
	DefinitionDigest string
}

type pluginManifestEntry struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

type pluginManifestEdge struct {
	Type       string `json:"type"` // "direct" or "collection"
	Entry      string `json:"entry"`
	Collection string `json:"collection,omitempty"`
}

type pluginManifestMembers struct {
	Entry string   `json:"entry"`
	Keys  []string `json:"keys"`
}

type pluginManifestExternal struct {
	CatalogKind   string `json:"catalog_kind"`
	CatalogURL    string `json:"catalog_url"`
	CatalogRef    string `json:"catalog_ref,omitempty"`
	CatalogCommit string `json:"catalog_commit,omitempty"`
	CatalogRoot   string `json:"catalog_root"`
	EntryName     string `json:"entry_name"`
	EntryDigest   string `json:"entry_digest"`
}

type pluginSourceManifest struct {
	SchemaVersion      string                  `json:"schema_version"`
	Generator          string                  `json:"generator"`
	Format             string                  `json:"format"`
	FormatVersion      string                  `json:"format_version,omitempty"`
	CatalogIndexDigest string                  `json:"catalog_index_digest,omitempty"`
	Entries            []pluginManifestEntry   `json:"entries,omitempty"`
	Edges              []pluginManifestEdge    `json:"edges,omitempty"`
	Members            []pluginManifestMembers `json:"members,omitempty"`
	External           *pluginManifestExternal `json:"external,omitempty"`
	Resources          []revisionResource      `json:"resources"`
	RevisionDigest     string                  `json:"revision_digest,omitempty"`
}

// buildPluginRevision computes the plugin-v1 manifest of a format root from
// the acquired source and the layer's declared plugin selection. For
// external-entry roots (input.PluginExternal != nil) the root is the entry
// package itself and the manifest binds to the owning catalog.
func buildPluginRevision(input Input, contract lockfile.SourceRevisionContract) (Result, error) {
	root, err := secureRoot(input)
	if err != nil {
		return Result{}, err
	}
	source, err := fsboundary.OpenSourceFS(root)
	if err != nil {
		return Result{}, fmt.Errorf("%w: capture revision source: %w", ErrCatalog, err)
	}
	defer func() { _ = source.Close() }()
	if err := source.VerifyRoot(); err != nil {
		return Result{}, fmt.Errorf("%w: validate revision root: %w", ErrCatalog, err)
	}
	if refusal := source.Refusal(); refusal != nil {
		return Result{}, fmt.Errorf("%w: source refused: %w", ErrCatalog, refusal)
	}
	manifest := pluginSourceManifest{
		SchemaVersion: contract.Schema,
		Generator:     contract.Generator,
		Resources:     []revisionResource{},
	}
	var externals []PluginExternalEntry

	if input.PluginExternal != nil {
		ext := input.PluginExternal
		plugin, _, err := plugindecode.Decode(root, input.SourceHarness)
		if err != nil {
			return Result{}, fmt.Errorf("%w: decode plugin package at %q: %w", ErrCatalog, input.RootPath, err)
		}
		manifest.Format = plugin.Format
		manifest.FormatVersion = plugin.FormatVersion
		manifest.External = &pluginManifestExternal{
			CatalogKind:   ext.CatalogKind,
			CatalogURL:    ext.CatalogURL,
			CatalogRef:    ext.CatalogRef,
			CatalogCommit: ext.CatalogCommit,
			CatalogRoot:   ext.CatalogRoot,
			EntryName:     ext.EntryName,
			EntryDigest:   ext.EntryDigest,
		}
		manifest.Resources = append(manifest.Resources, pluginResource(ext.EntryName, plugin))
	} else {
		spec := input.PluginSpec
		if spec == nil {
			return Result{}, fmt.Errorf("%w: plugin-v1 build requires a plugin selection spec", ErrCatalog)
		}
		loaded, err := catalog.DiscoverPluginSource(root, input.SourceHarness)
		if err != nil {
			return Result{}, fmt.Errorf("%w: decode plugin source %q: %w", ErrCatalog, input.RootPath, err)
		}
		manifest.Format = input.SourceHarness
		manifest.FormatVersion = pluginFormatVersion(loaded)
		indexDigest, err := catalogIndexDigest(root, input.SourceHarness)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %w", ErrCatalog, err)
		}
		manifest.CatalogIndexDigest = indexDigest

		byName := catalogEntriesByName(loaded.Entries)
		for _, name := range pluginSelectedNames(loaded.Entries, spec) {
			entry, ok := byName[name]
			if !ok || entry.Plugin == nil {
				continue
			}
			manifest.Entries = append(manifest.Entries, pluginManifestEntry{Name: name, Digest: entry.Plugin.DefinitionDigest})
			if entry.Plugin.Source.Kind == "git" {
				externals = append(externals, PluginExternalEntry{Name: name, Source: entry.Plugin.Source, DefinitionDigest: entry.Plugin.DefinitionDigest})
				continue
			}
			if entry.Plugin.Source.Kind != "path" || entry.Plugin.Refusal != nil {
				continue
			}
			plugin, ok := entry.Resource.(*ir.Plugin)
			if !ok {
				continue
			}
			manifest.Resources = append(manifest.Resources, pluginResource(name, plugin))
			manifest.Members = append(manifest.Members, pluginManifestMembers{Entry: name, Keys: memberKeysAfterExclusions(plugin, name, spec)})
		}
		manifest.Edges = pluginSpecEdges(spec, byName)
	}

	payload, digest, err := encodePluginManifest(manifest)
	if err != nil {
		return Result{}, err
	}
	manifest.RevisionDigest = digest
	resources := make([]revisionResource, 0, len(manifest.Resources))
	resources = append(resources, manifest.Resources...)
	return Result{
		SourceHarness:   input.SourceHarness,
		Manifest:        sourceRevisionManifest{SchemaVersion: contract.Schema, Generator: contract.Generator, RevisionDigest: digest, Resources: resources},
		Payload:         payload,
		PluginManifest:  &manifest,
		PluginExternals: externals,
	}, nil
}

// pluginResource turns the decoded package into the lock resource record:
// content_digest covers the sorted contained file set (path, digest and
// executable bit); archive_digest follows the per-resource archive
// construction; support_files is the verified path->digest inventory.
func pluginResource(name string, plugin *ir.Plugin) revisionResource {
	var content bytes.Buffer
	for _, file := range plugin.Files {
		content.WriteString(file.Path)
		content.WriteByte(0)
		content.WriteString(file.Digest)
		content.WriteByte(0)
		if file.Executable {
			content.WriteString("x")
		}
		content.WriteByte(0)
	}
	contentSum := contentDigest(content.Bytes())
	archiveInput := append([]byte(contentSum), 0)
	for _, file := range plugin.Files {
		archiveInput = append(archiveInput, []byte(file.Path)...)
		archiveInput = append(archiveInput, 0)
		archiveInput = append(archiveInput, []byte(file.Digest)...)
		archiveInput = append(archiveInput, 0)
	}
	support := make(map[string]string, len(plugin.Files))
	for _, file := range plugin.Files {
		support[file.Path] = "sha256:" + file.Digest
	}
	return revisionResource{Kind: "plugin", Name: name, ContentDigest: contentSum, ArchiveDigest: contentDigest(archiveInput), SupportFiles: support}
}

// pluginSelectedNames resolves the declared selection against the decoded
// entries: explicit names plus every installable entry when the source is
// selected wholesale, plus collection edges, minus group exclusions.
func pluginSelectedNames(entries []catalog.Entry, spec *catalog.PluginSelectionSpec) []string {
	names := map[string]bool{}
	for _, request := range spec.Explicit {
		names[request.Name] = true
	}
	if spec.All {
		for _, entry := range entries {
			if entry.Kind != ir.KindPlugin || entry.Plugin == nil || entry.Plugin.Refusal != nil {
				continue
			}
			names[entry.Name] = true
		}
	}
	for _, edge := range spec.Collections {
		for _, name := range edge.Entries {
			names[name] = true
		}
	}
	for _, name := range spec.Excluded {
		delete(names, name)
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func catalogEntriesByName(entries []catalog.Entry) map[string]catalog.Entry {
	byName := make(map[string]catalog.Entry, len(entries))
	for _, entry := range entries {
		if entry.Kind == ir.KindPlugin {
			byName[entry.Name] = entry
		}
	}
	return byName
}

// pluginSpecEdges renders the declared selection as manifest edges: one
// direct edge per explicitly or wholesale selected installable entry, and
// one edge per collection binding.
func pluginSpecEdges(spec *catalog.PluginSelectionSpec, entries map[string]catalog.Entry) []pluginManifestEdge {
	direct := map[string]bool{}
	for _, request := range spec.Explicit {
		direct[request.Name] = true
	}
	if spec.All {
		for name, entry := range entries {
			if entry.Plugin != nil && entry.Plugin.Refusal == nil {
				direct[name] = true
			}
		}
	}
	excluded := map[string]bool{}
	for _, name := range spec.Excluded {
		excluded[name] = true
		delete(direct, name)
	}
	var edges []pluginManifestEdge
	for name := range direct {
		if entry, ok := entries[name]; ok && entry.Plugin != nil && entry.Plugin.Refusal == nil {
			edges = append(edges, pluginManifestEdge{Type: "direct", Entry: name})
		}
	}
	for _, edge := range spec.Collections {
		for _, name := range edge.Entries {
			if excluded[name] {
				continue
			}
			if entry, ok := entries[name]; ok && entry.Plugin != nil && entry.Plugin.Refusal == nil {
				edges = append(edges, pluginManifestEdge{Type: "collection", Entry: name, Collection: edge.Collection})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Entry != edges[j].Entry {
			return edges[i].Entry < edges[j].Entry
		}
		return edges[i].Collection < edges[j].Collection
	})
	return edges
}

// memberKeysAfterExclusions lists the plugin's member keys minus the exact
// exclusions authored on the entry's direct requests.
func memberKeysAfterExclusions(plugin *ir.Plugin, name string, spec *catalog.PluginSelectionSpec) []string {
	var excluded []string
	for _, request := range spec.Explicit {
		if request.Name == name {
			excluded = append(excluded, request.Exclude...)
		}
	}
	keys := make([]string, 0, len(plugin.Members))
	for _, member := range plugin.Members {
		key := member.Key()
		ex := false
		for _, e := range excluded {
			if e == key {
				ex = true
				break
			}
		}
		if !ex {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// catalogIndexDigest digests the catalog manifest bytes of a catalog-format
// source; package-format roots have no index.
func catalogIndexDigest(root, format string) (string, error) {
	layout, ok := target.LookupPackageLayout(format)
	if !ok || layout.CatalogRoot == "" {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(layout.ManifestPath)))
	if err != nil {
		return "", fmt.Errorf("read catalog manifest %q: %w", layout.ManifestPath, err)
	}
	return contentDigest(data), nil
}

func pluginFormatVersion(loaded catalog.LoadedCatalog) string {
	for _, entry := range loaded.Entries {
		if plugin, ok := entry.Resource.(*ir.Plugin); ok {
			return plugin.FormatVersion
		}
	}
	return ""
}

// encodePluginManifest hashes the canonical JSON form of the plugin-v1
// manifest to its revision digest.
func encodePluginManifest(manifest pluginSourceManifest) ([]byte, string, error) {
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Name < manifest.Entries[j].Name })
	sort.Slice(manifest.Resources, func(i, j int) bool {
		if manifest.Resources[i].Kind != manifest.Resources[j].Kind {
			return manifest.Resources[i].Kind < manifest.Resources[j].Kind
		}
		return manifest.Resources[i].Name < manifest.Resources[j].Name
	})
	sort.Slice(manifest.Members, func(i, j int) bool { return manifest.Members[i].Entry < manifest.Members[j].Entry })
	if len(manifest.Entries) == 0 {
		manifest.Entries = nil
	}
	if len(manifest.Edges) == 0 {
		manifest.Edges = nil
	}
	if len(manifest.Members) == 0 {
		manifest.Members = nil
	}
	withoutDigest := manifest
	withoutDigest.RevisionDigest = ""
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(withoutDigest); err != nil {
		return nil, "", fmt.Errorf("encode plugin revision manifest: %w", err)
	}
	sum := sha256.Sum256(bytes.TrimRight(out.Bytes(), "\n"))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	manifest.RevisionDigest = digest
	out.Reset()
	if err := encoder.Encode(manifest); err != nil {
		return nil, "", fmt.Errorf("encode plugin revision manifest: %w", err)
	}
	return bytes.TrimRight(out.Bytes(), "\n"), digest, nil
}
