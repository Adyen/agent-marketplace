package sourcerevision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var (
	ErrPathEscape       = errors.New("source revision path escapes snapshot")
	ErrCatalog          = errors.New("source revision catalog is invalid")
	ErrManifestMismatch = errors.New("source revision manifest mismatch")
)

type Input struct {
	SnapshotRoot  string
	RootPath      string
	SourceHarness string
}

type Result struct {
	SourceHarness string
	Manifest      sourceRevisionManifest
	Payload       []byte
	Warnings      []ResourceWarning
}

type ResourceWarning struct {
	Kind   string
	Name   string
	Reason string
}

func (w ResourceWarning) Error() string {
	return fmt.Sprintf("source revision omitted %s/%s: %s", w.Kind, w.Name, w.Reason)
}

func (w ResourceWarning) Resource() string   { return w.Kind + "/" + w.Name }
func (w ResourceWarning) DependencyWarning() {}

type Projection struct {
	SourceHarness  string
	RevisionDigest string
	Resources      []revisionResource
}

func (r Result) Projection() Projection {
	resources := make([]revisionResource, 0, len(r.Manifest.Resources))
	for _, resource := range r.Manifest.Resources {
		if resource.ArchiveDigest == "" {
			continue
		}
		resource.SupportFiles = cloneDigests(resource.SupportFiles)
		resources = append(resources, resource)
	}
	return Projection{SourceHarness: r.SourceHarness, RevisionDigest: r.Manifest.RevisionDigest, Resources: resources}
}

func Build(input Input) (Result, error) {
	root, err := secureRoot(input)
	if err != nil {
		return Result{}, err
	}

	cat, err := catalog.Load(root, input.SourceHarness)
	if err != nil {
		discovered, discoverErr := catalog.Discover(root, input.SourceHarness)
		if discoverErr != nil {
			return Result{}, fmt.Errorf("%w: load root %q: %w", ErrCatalog, input.RootPath, err)
		}
		sourceHarness := input.SourceHarness
		if sourceHarness == "" {
			sourceHarness = "auto"
		}
		cat = &catalog.Catalog{Entries: discovered.Entries, SourceHarness: sourceHarness}
	}
	diagnosticErrors := 0
	for _, diagnostic := range cat.Diagnostics {
		if diagnostic.IsError() {
			diagnosticErrors++
		}
	}

	collections, err := collection.DiscoverCollections(root, "", "", "", normalizedRootPath(input.RootPath))
	if err != nil {
		return Result{}, fmt.Errorf("%w: discover collections: %w", ErrCatalog, err)
	}
	collectionEntries, err := catalog.EntriesFromCollections(collections)
	if err != nil {
		return Result{}, fmt.Errorf("%w: collection entries: %w", ErrCatalog, err)
	}
	entries := make([]catalog.Entry, 0, len(cat.Entries)+len(collectionEntries))
	entries = append(entries, cat.Entries...)
	entries = append(entries, collectionEntries...)

	resources := make([]revisionResource, 0, len(entries))
	warnings := make([]ResourceWarning, 0)
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		key := entry.Kind.String() + "/" + entry.Name
		if _, exists := seen[key]; exists {
			return Result{}, fmt.Errorf("%w: duplicate resource %q", ErrCatalog, key)
		}
		seen[key] = struct{}{}
	}
	for _, entry := range entries {
		kind := entry.Kind.String()
		key := kind + "/" + entry.Name
		if kind == "unknown" || kind == "rule" {
			warnings = append(warnings, ResourceWarning{Kind: kind, Name: entry.Name, Reason: "unsupported resource kind"})
			slog.Warn("source revision resource omitted", "kind", kind, "name", entry.Name, "reason", "unsupported resource kind")
			continue
		}

		resource, err := buildResource(root, entry)
		if err != nil {
			if errors.Is(err, ErrPathEscape) {
				return Result{}, fmt.Errorf("%w: build %s: %w", ErrCatalog, key, err)
			}
			warnings = append(warnings, ResourceWarning{Kind: kind, Name: entry.Name, Reason: "resource could not be built: " + err.Error()})
			slog.Warn("source revision resource omitted", "kind", kind, "name", entry.Name, "reason", "resource build failed")
			continue
		}
		resources = append(resources, resource)
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Kind != resources[j].Kind {
			return resources[i].Kind < resources[j].Kind
		}
		return resources[i].Name < resources[j].Name
	})
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Kind != warnings[j].Kind {
			return warnings[i].Kind < warnings[j].Kind
		}
		if warnings[i].Name != warnings[j].Name {
			return warnings[i].Name < warnings[j].Name
		}
		return warnings[i].Reason < warnings[j].Reason
	})

	manifest := sourceRevisionManifest{
		SchemaVersion: sourceRevisionSchemaV1,
		Generator:     sourceRevisionGeneratorV1,
		Resources:     resources,
	}
	payload, manifest, err := encodeManifest(manifest)
	if err != nil {
		return Result{}, fmt.Errorf("encode source revision manifest: %w", err)
	}

	slog.Debug("built direct-Git source revision", "root", normalizedRootPath(input.RootPath), "source_harness", cat.SourceHarness, "resources", len(resources), "warnings", len(warnings), "deferred_diagnostic_errors", diagnosticErrors)
	return Result{SourceHarness: cat.SourceHarness, Manifest: manifest, Payload: payload, Warnings: warnings}, nil
}

func VerifyLockedGitRoot(snapshot string, root lockfile.GitRoot) ([]ResourceWarning, error) {
	resources := make([]revisionResource, 0, len(root.Resources))
	for _, resource := range root.Resources {
		support := make(map[string]string, len(resource.SupportFiles))
		for name, digest := range resource.SupportFiles {
			support[name] = digest.String()
		}
		if len(support) == 0 {
			support = nil
		}
		resources = append(resources, revisionResource{
			Kind:          resource.Kind,
			Name:          resource.Name,
			ContentDigest: resource.ContentDigest.String(),
			ArchiveDigest: resource.ArchiveDigest.String(),
			SupportFiles:  support,
		})
	}
	return Verify(Input{SnapshotRoot: snapshot, RootPath: root.Path}, Projection{
		SourceHarness:  root.SourceHarness,
		RevisionDigest: root.RevisionDigest.String(),
		Resources:      resources,
	})
}

func Verify(input Input, locked Projection) ([]ResourceWarning, error) {
	if locked.SourceHarness == "" {
		return nil, fmt.Errorf("%w: locked source harness is empty", ErrManifestMismatch)
	}
	input.SourceHarness = locked.SourceHarness
	if input.SourceHarness == "auto" {
		input.SourceHarness = ""
	}
	built, err := Build(input)
	if err != nil {
		return nil, err
	}
	if built.SourceHarness != locked.SourceHarness {
		return nil, fmt.Errorf("%w: source harness got %q, want %q", ErrManifestMismatch, built.SourceHarness, locked.SourceHarness)
	}
	if built.Manifest.RevisionDigest != locked.RevisionDigest {
		return nil, fmt.Errorf("%w: revision digest got %q, want %q", ErrManifestMismatch, built.Manifest.RevisionDigest, locked.RevisionDigest)
	}
	if !reflect.DeepEqual(built.Projection().Resources, locked.Resources) {
		return nil, fmt.Errorf("%w: rebuilt resource inventory differs", ErrManifestMismatch)
	}
	return built.Warnings, nil
}

func cloneDigests(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for name, digest := range in {
		out[name] = digest
	}
	return out
}

func buildResource(root string, entry catalog.Entry) (revisionResource, error) {
	if entry.Resource == nil {
		return revisionResource{}, errors.New("resource is nil")
	}
	payload, err := resourceSourceBytes(root, entry)
	if err != nil {
		return revisionResource{}, err
	}
	digest := contentDigest(payload)
	if entry.Kind == ir.KindCollection {
		return revisionResource{Kind: "collection", Name: entry.Name, ContentDigest: digest}, nil
	}
	support, err := intrinsicSupportFileDigests(root, entry)
	if err != nil {
		return revisionResource{}, err
	}
	archiveInput := append([]byte(nil), payload...)
	names := make([]string, 0, len(support))
	for name := range support {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		archiveInput = append(archiveInput, 0)
		archiveInput = append(archiveInput, name...)
		archiveInput = append(archiveInput, 0)
		archiveInput = append(archiveInput, support[name]...)
	}
	return revisionResource{Kind: entry.Kind.String(), Name: entry.Name, ContentDigest: digest, ArchiveDigest: contentDigest(archiveInput), SupportFiles: support}, nil
}

func resourceSourceBytes(root string, entry catalog.Entry) ([]byte, error) {
	if entry.Path != "" {
		name, err := containedPath(root, entry.Path)
		if err != nil {
			return nil, err
		}
		if err := rejectSymlinkComponents(root, name); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(name)
		if err == nil {
			return data, nil
		}
	}
	data, err := json.Marshal(entry.Resource)
	if err != nil {
		return nil, fmt.Errorf("encode canonical resource: %w", err)
	}
	return data, nil
}

func contentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func intrinsicSupportFileDigests(root string, entry catalog.Entry) (map[string]string, error) {
	skill, ok := entry.Resource.(*ir.Skill)
	if !ok || len(skill.SupportFiles) == 0 {
		return nil, nil
	}
	if entry.Path == "" {
		return nil, errors.New("skill with support files has no source path")
	}
	resourceFile, err := containedPath(root, entry.Path)
	if err != nil {
		return nil, err
	}
	resourceRoot := filepath.Dir(resourceFile)
	digests := make(map[string]string, len(skill.SupportFiles))
	for _, supportPath := range skill.SupportFiles {
		normalized, err := safeArtifactPath(supportPath)
		if err != nil {
			return nil, err
		}
		if _, duplicate := digests[normalized]; duplicate {
			return nil, fmt.Errorf("duplicate support file %q", normalized)
		}
		fileName, err := containedPath(resourceRoot, filepath.FromSlash(normalized))
		if err != nil {
			return nil, err
		}
		if err := rejectSymlinkComponents(resourceRoot, fileName); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(fileName)
		if err != nil {
			return nil, fmt.Errorf("read support file %q: %w", normalized, err)
		}
		digests[normalized] = contentDigest(data)
	}
	return digests, nil
}

func secureRoot(input Input) (string, error) {
	if input.SnapshotRoot == "" {
		return "", fmt.Errorf("snapshot root is required")
	}
	snapshotAbs, err := filepath.Abs(input.SnapshotRoot)
	if err != nil {
		return "", fmt.Errorf("resolve snapshot root: %w", err)
	}
	snapshotReal, err := filepath.EvalSymlinks(snapshotAbs)
	if err != nil {
		return "", fmt.Errorf("canonicalize snapshot root: %w", err)
	}
	info, err := os.Stat(snapshotReal)
	if err != nil {
		return "", fmt.Errorf("stat snapshot root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("snapshot root is not a directory")
	}

	rootPath := normalizedRootPath(input.RootPath)
	if path.IsAbs(rootPath) || strings.Contains(rootPath, "\\") || escapesSlashRoot(rootPath) {
		return "", fmt.Errorf("%w: unsafe configured root %q", ErrPathEscape, input.RootPath)
	}
	rootLexical := filepath.Join(snapshotAbs, filepath.FromSlash(rootPath))
	if _, err := lexicallyContained(snapshotAbs, rootLexical); err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootLexical)
	if err != nil {
		return "", fmt.Errorf("resolve configured root %q: %w", input.RootPath, err)
	}
	if _, err := lexicallyContained(snapshotReal, rootReal); err != nil {
		return "", err
	}
	if err := validateTreeSymlinks(rootReal); err != nil {
		return "", err
	}
	return rootReal, nil
}

func normalizedRootPath(root string) string {
	if root == "" {
		return "."
	}
	return path.Clean(root)
}

func escapesSlashRoot(name string) bool {
	return name == ".." || strings.HasPrefix(name, "../")
}

func lexicallyContained(root, target string) (string, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q is outside %q", ErrPathEscape, target, root)
	}
	return rel, nil
}

func validateTreeSymlinks(rootReal string) error {
	return filepath.WalkDir(rootReal, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(name)
		if err != nil {
			return fmt.Errorf("%w: resolve symlink %q: %v", ErrPathEscape, name, err)
		}
		if _, err := lexicallyContained(rootReal, resolved); err != nil {
			return fmt.Errorf("%w: symlink %q", err, name)
		}
		return nil
	})
}

func containedPath(root, name string) (string, error) {
	candidate := name
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	if _, err := lexicallyContained(root, candidate); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", name, err)
	}
	if _, err := lexicallyContained(root, resolved); err != nil {
		return "", err
	}
	return candidate, nil
}

func rejectSymlinkComponents(root, target string) error {
	rel, err := lexicallyContained(root, target)
	if err != nil {
		return err
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: support file uses symlink %q", ErrPathEscape, current)
		}
	}
	return nil
}

func safeArtifactPath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", fmt.Errorf("%w: invalid support path %q", ErrPathEscape, name)
	}
	clean := path.Clean(name)
	if clean != name || escapesSlashRoot(clean) || clean == "." {
		return "", fmt.Errorf("%w: invalid support path %q", ErrPathEscape, name)
	}
	return clean, nil
}
