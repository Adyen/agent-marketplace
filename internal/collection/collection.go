// Package collection provides types and helpers for the Collection abstraction:
// named, versioned bundles of skills discovered from a registry's collections/
// subdirectory and referenced from any config layer.
package collection

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"gopkg.in/yaml.v3"
)

const collectionFileName = "COLLECTION.yaml"

// Collection is a named bundle of agent resources discovered from a registry's
// collections/ subdirectory. The file path encodes the type; apiVersion
// and kind are not required and are silently ignored if present.
//
// Resources can be listed in two ways:
//   - Legacy: the skills: key accepts CollectionSkill items (backward compatible).
//   - General: the resources: key accepts CollectionEntry items with an explicit
//     Kind field (defaults to "skill" when omitted).
//
// Both keys may coexist; they are merged during expansion.
type Collection struct {
	Name        string              `yaml:"name"`
	Version     string              `yaml:"version,omitempty"`
	Description string              `yaml:"description,omitempty"`
	Skills      []CollectionSkill   `yaml:"skills,omitempty"`
	Resources   []CollectionEntry   `yaml:"resources,omitempty"`
	Includes    []CollectionInclude `yaml:"includes,omitempty"`

	// SourceRegistryURL and SourceRef identify the registry this collection
	// was discovered from. Not serialized in YAML; set during discovery.
	SourceRegistryURL string `yaml:"-"`
	SourceRef         string `yaml:"-"`
	// SourceCatalogKey is the cache/local path key for the source registry.
	SourceCatalogKey string `yaml:"-"`
	// SourcePath is the path (subdirectory) of the registry entry this
	// collection was discovered from. Not serialized in YAML; set during
	// discovery. Used as the default path for collection skills that do not
	// specify an explicit path.
	SourcePath string `yaml:"-"`
	// SourceFile is the path of this collection definition relative to
	// SourcePath. It preserves the exact file from which the collection was
	// indexed without exposing a workspace-local absolute path.
	SourceFile string `yaml:"-"`
}

// Label returns the human-readable label for this collection.
func (c Collection) Label() string {
	if c.Version != "" {
		return c.Name + "@" + c.Version
	}
	return c.Name
}

// CollectionSkill is a skill entry within a collection. Registry, Ref, and
// Path are optional — if omitted the skill is expected to come from the same
// registry (and subdirectory) the collection was discovered in.
//
// Deprecated: prefer CollectionEntry (with Kind field) for new collections.
// CollectionSkill is retained for backward compatibility with the legacy
// skills: YAML key.
type CollectionSkill struct {
	Name     string `yaml:"name"`
	Registry string `yaml:"registry,omitempty"`
	Ref      string `yaml:"ref,omitempty"`
	Path     string `yaml:"path,omitempty"`
}

// CollectionEntry is a generalized resource entry within a collection.
// Kind defaults to "skill" when empty. Registry, Ref, and Path are optional —
// if omitted the resource is expected to come from the same registry (and
// subdirectory) the collection was discovered in.
type CollectionEntry struct {
	Name     string `yaml:"name"`
	Kind     string `yaml:"kind,omitempty"` // defaults to "skill" when empty
	Registry string `yaml:"registry,omitempty"`
	Ref      string `yaml:"ref,omitempty"`
	Path     string `yaml:"path,omitempty"`
}

// CollectionInclude references another collection to nest inside this one.
type CollectionInclude struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version,omitempty"`
}

// DiscoverCollections walks the `collections/` subdirectory of skillDir and
// parses every COLLECTION.yaml file found. registryURL, ref, and path are
// stamped on each discovered collection for provenance tracking.
func DiscoverCollections(skillDir, catalogKey, registryURL, ref, path string) (results []Collection, err error) {
	source, err := fsboundary.OpenSourceFS(skillDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, nil
		}
		return nil, fmt.Errorf("capture collection source %q: %w", skillDir, err)
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	collectionsDir := filepath.Join(skillDir, "collections")
	entries, err := source.ReadDir("collections")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, nil
		}
		return nil, fmt.Errorf("read collections directory %q: %w", collectionsDir, err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() {
			// Flat .yaml files are silently skipped.
			// The correct format is collections/<name>/COLLECTION.yaml
			// (a subdirectory per collection, not a flat file).
			continue
		}

		yamlPath := filepath.Join(collectionsDir, entry.Name(), collectionFileName)
		col, err := parseCollectionFile(source, filepath.ToSlash(filepath.Join("collections", entry.Name(), collectionFileName)), yamlPath)
		if err != nil {
			return nil, err
		}
		if col == nil {
			continue
		}
		col.SourceRegistryURL = registryURL
		col.SourceRef = ref
		col.SourceCatalogKey = catalogKey
		col.SourcePath = path
		col.SourceFile = filepath.ToSlash(filepath.Join("collections", entry.Name(), collectionFileName))
		results = append(results, *col)
	}

	return results, nil
}

func parseCollectionFile(source *fsboundary.SourceFS, name, path string) (*Collection, error) {
	data, err := source.ReadFile(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, nil
		}
		return nil, fmt.Errorf("read collection file %q: %w", path, err)
	}

	var col Collection
	if err := yaml.Unmarshal(data, &col); err != nil {
		return nil, fmt.Errorf("parse collection file %q: %w", path, err)
	}

	if strings.TrimSpace(col.Name) == "" {
		return nil, fmt.Errorf("collection file %q: name is required", path)
	}

	return &col, nil
}

// Find returns the first collection with the matching name from the given slice.
// Version is display-only metadata on the collection; matching is by name only
// since the registry ref already pins which version is in use.
// Returns nil if no match is found.
func Find(collections []Collection, name string) *Collection {
	name = strings.TrimSpace(name)
	for i := range collections {
		c := &collections[i]
		if strings.TrimSpace(c.Name) == name {
			return c
		}
	}
	return nil
}
