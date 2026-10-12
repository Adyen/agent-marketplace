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
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var (
	ErrPathEscape       = errors.New("source revision path escapes snapshot")
	ErrCatalog          = errors.New("source revision catalog is invalid")
	ErrManifestMismatch = errors.New("source revision manifest mismatch")
)

type Input struct {
	SnapshotRoot   string
	RootPath       string
	SourceHarness  string
	RevisionSchema string
	beforeIngest   func(string)
	beforeHash     func(string)
}

type Result struct {
	SourceHarness string
	Manifest      sourceRevisionManifest
	Payload       []byte
	Warnings      []ResourceWarning
	Catalog       *catalog.Catalog
}

type ResourceWarning struct {
	Kind   string
	Name   string
	Reason string
}

func (w ResourceWarning) Error() string {
	return fmt.Sprintf("source revision omitted %s/%s: %s", w.Kind, w.Name, w.Reason)
}

func (w ResourceWarning) Resource() string        { return w.Kind + "/" + w.Name }
func (w ResourceWarning) PresentationKey() string { return "source-revision-omitted\x00" + w.Reason }
func (w ResourceWarning) PresentationText() string {
	return "source revision omitted resources: " + w.Reason
}
func (w ResourceWarning) DiagnosticCode() string    { return "" }
func (w ResourceWarning) DiagnosticMessage() string { return "" }
func (w ResourceWarning) DependencyWarning()        {}

type Projection struct {
	SourceHarness  string
	RevisionSchema string
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
	return Projection{SourceHarness: r.SourceHarness, RevisionSchema: r.Manifest.SchemaVersion, RevisionDigest: r.Manifest.RevisionDigest, Resources: resources}
}

func Build(input Input) (result Result, err error) {
	if input.RevisionSchema == "" {
		input.RevisionSchema = lockfile.CurrentRevisionSchema
	}
	contract, err := lockfile.RevisionContract(input.RevisionSchema)
	if err != nil {
		return Result{}, err
	}
	root, err := secureRoot(input)
	if err != nil {
		return Result{}, err
	}

	source, err := fsboundary.OpenSourceFS(root)
	if err != nil {
		return Result{}, fmt.Errorf("capture revision source: %w", err)
	}
	defer func() {
		if refusal := source.Refusal(); refusal != nil {
			result = Result{}
			err = fmt.Errorf("%w: source refused: %w", ErrCatalog, errors.Join(err, refusal))
		}
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if input.beforeIngest != nil {
		input.beforeIngest(root)
	}
	options := catalog.IngestOptions{LegacyWindsurfHooks: !contract.AdditiveWindsurfHooks, LegacyBareMCP: !contract.BareBundleMCP}
	cat, err := catalog.LoadSourceWithOptions(source, root, input.SourceHarness, options)
	loaded := cat
	if fsboundary.IsSourceRefusal(err) {
		return Result{}, fmt.Errorf("%w: source refused: %w", ErrCatalog, err)
	}
	if cat != nil && cat.SourceRefusal() != nil {
		return Result{}, fmt.Errorf("%w: source refused: %w", ErrCatalog, cat.SourceRefusal())
	}
	if err != nil || len(cat.Entries) == 0 {
		discovered, discoverErr := catalog.DiscoverSourceWithOptions(source, root, input.SourceHarness, options)
		if discoverErr != nil {
			return Result{}, fmt.Errorf("%w: load root %q: %w", ErrCatalog, input.RootPath, errors.Join(err, discoverErr))
		}
		for _, discoveryErr := range discovered.Errors {
			if fsboundary.IsSourceRefusal(discoveryErr) {
				return Result{}, fmt.Errorf("%w: source refused: %w", ErrCatalog, discoveryErr)
			}
		}
		if err != nil || len(discovered.Entries) > 0 {
			sourceHarness := input.SourceHarness
			if sourceHarness == "" {
				sourceHarness = "auto"
			}
			cat = &catalog.Catalog{Entries: discovered.Entries, Diagnostics: discovered.Diagnostics, SourceHarness: sourceHarness}
			loaded = nil
		}
	}
	diagnosticErrors := 0
	for _, diagnostic := range cat.Diagnostics {
		if diagnostic.IsError() {
			diagnosticErrors++
		}
	}

	collections, err := collection.DiscoverCollectionsFS(source, root, "", "", "", normalizedRootPath(input.RootPath))
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
	if input.beforeHash != nil {
		input.beforeHash(root)
	}
	if err := source.VerifyRoot(); err != nil {
		return Result{}, fmt.Errorf("%w: validate revision root: %w", ErrCatalog, err)
	}
	for _, entry := range entries {
		kind := entry.Kind.String()
		key := kind + "/" + entry.Name
		if kind == "unknown" || kind == "rule" {
			warnings = append(warnings, ResourceWarning{Kind: kind, Name: entry.Name, Reason: "unsupported resource kind"})
			slog.Warn("source revision resource omitted", "kind", kind, "name", entry.Name, "reason", "unsupported resource kind")
			continue
		}

		resource, err := buildResource(root, source, entry, contract)
		if err != nil {
			if errors.Is(err, ErrPathEscape) || fsboundary.IsSourceRefusal(err) {
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
		SchemaVersion: contract.Schema,
		Generator:     contract.Generator,
		Resources:     resources,
	}
	payload, manifest, err := encodeManifest(manifest)
	if err != nil {
		return Result{}, fmt.Errorf("encode source revision manifest: %w", err)
	}

	if loaded != nil {
		catalogRoot, err := filepath.Abs(filepath.Join(input.SnapshotRoot, filepath.FromSlash(normalizedRootPath(input.RootPath))))
		if err != nil {
			return Result{}, fmt.Errorf("resolve catalog root: %w", err)
		}
		if catalogRoot != root {
			for i, entry := range loaded.Entries {
				if entry.Path == "" {
					continue
				}
				rel, err := lexicallyContained(root, entry.Path)
				if err != nil {
					loaded = nil
					break
				}
				loaded.Entries[i].Path = filepath.Join(catalogRoot, rel)
			}
		}
	}
	slog.Debug("built direct-Git source revision", "root", normalizedRootPath(input.RootPath), "source_harness", cat.SourceHarness, "revision_schema", contract.Schema, "resources", len(resources), "warnings", len(warnings), "deferred_diagnostic_errors", diagnosticErrors)
	return Result{SourceHarness: cat.SourceHarness, Manifest: manifest, Payload: payload, Warnings: warnings, Catalog: loaded}, nil
}

func VerifyLockedGitRoot(snapshot string, root lockfile.GitRoot) ([]ResourceWarning, error) {
	result, err := VerifyLockedGitRootWithCatalog(snapshot, root)
	return result.Warnings, err
}

func VerifyLockedGitRootWithCatalog(snapshot string, root lockfile.GitRoot) (Result, error) {
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
	return verify(Input{SnapshotRoot: snapshot, RootPath: root.Path}, Projection{
		SourceHarness:  root.SourceHarness,
		RevisionSchema: root.RevisionSchema,
		RevisionDigest: root.RevisionDigest.String(),
		Resources:      resources,
	})
}

func Verify(input Input, locked Projection) ([]ResourceWarning, error) {
	result, err := verify(input, locked)
	return result.Warnings, err
}

func verify(input Input, locked Projection) (Result, error) {
	if _, err := lockfile.RevisionContract(locked.RevisionSchema); err != nil {
		return Result{}, err
	}
	input.RevisionSchema = locked.RevisionSchema
	if locked.SourceHarness == "" {
		return Result{}, fmt.Errorf("%w: locked source harness is empty", ErrManifestMismatch)
	}
	input.SourceHarness = locked.SourceHarness
	if input.SourceHarness == "auto" {
		input.SourceHarness = ""
	}
	built, err := Build(input)
	if err != nil {
		return Result{}, err
	}
	if built.SourceHarness != locked.SourceHarness {
		return Result{}, fmt.Errorf("%w: source harness got %q, want %q", ErrManifestMismatch, built.SourceHarness, locked.SourceHarness)
	}
	if built.Manifest.RevisionDigest != locked.RevisionDigest {
		return Result{}, fmt.Errorf("%w: revision digest got %q, want %q", ErrManifestMismatch, built.Manifest.RevisionDigest, locked.RevisionDigest)
	}
	if !reflect.DeepEqual(built.Projection().Resources, locked.Resources) {
		return Result{}, fmt.Errorf("%w: rebuilt resource inventory differs", ErrManifestMismatch)
	}
	return built, nil
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

func buildResource(root string, source *fsboundary.SourceFS, entry catalog.Entry, contract lockfile.SourceRevisionContract) (revisionResource, error) {
	if entry.Resource == nil {
		return revisionResource{}, errors.New("resource is nil")
	}
	payload, err := resourceSourceBytes(root, source, entry, contract)
	if err != nil {
		return revisionResource{}, err
	}
	digest := contentDigest(payload)
	if entry.Kind == ir.KindCollection {
		return revisionResource{Kind: "collection", Name: entry.Name, ContentDigest: digest}, nil
	}
	support, err := intrinsicSupportFileDigests(root, source, entry, contract)
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

func resourceSourceBytes(root string, source *fsboundary.SourceFS, entry catalog.Entry, contract lockfile.SourceRevisionContract) (data []byte, err error) {
	defer func() {
		if entry.PrimarySourcePath != "" && entry.PrimarySourcePath == entry.Path && errors.Is(err, fs.ErrNotExist) && !fsboundary.IsSourceRefusal(err) {
			err = fmt.Errorf("%w: admitted primary %q disappeared: %w", fsboundary.ErrGenerationConflict, entry.Path, err)
		}
	}()
	if entry.Kind != ir.KindCollection && entry.PrimarySourcePath != "" && entry.PrimarySourcePath != entry.Path {
		rel, err := lexicallyContained(root, entry.PrimarySourcePath)
		if err != nil {
			return nil, err
		}
		if _, err := source.ReadFile(filepath.ToSlash(rel)); err != nil {
			if errors.Is(err, fs.ErrNotExist) && !fsboundary.IsSourceRefusal(err) {
				err = fmt.Errorf("%w: admitted primary %q disappeared: %w", fsboundary.ErrGenerationConflict, entry.PrimarySourcePath, err)
			}
			return nil, err
		}
	}
	// Collections digest over their canonical IR serialization, never the raw
	// collection definition file: entry.Path is populated for collections so
	// consumers can locate the definition file, but hashing that file would
	// change the source-revision compatibility basis.
	if entry.Path != "" && entry.Kind != ir.KindCollection {
		name, err := containedPath(root, entry.Path)
		if err != nil {
			return nil, err
		}
		if err := rejectSymlinkComponents(root, name); err != nil {
			return nil, err
		}
		rel, err := lexicallyContained(root, name)
		if err != nil {
			return nil, err
		}
		data, err := source.ReadFile(filepath.ToSlash(rel))
		if err == nil {
			return data, nil
		}
		if entry.PrimarySourcePath == entry.Path || !errors.Is(err, fs.ErrNotExist) || fsboundary.IsSourceRefusal(err) {
			return nil, err
		}
	}
	resource := entry.Resource
	resource = legacyHookToolProjection(resource)
	if !contract.ModelProvenance {
		resource = legacyModelProjection(resource)
	}
	if collection, ok := resource.(*ir.Collection); ok {
		copy := *collection
		copy.SourceRegistryURL = ""
		copy.SourceRef = ""
		copy.SourceCatalogKey = ""
		copy.SourcePath = ""
		copy.Source = ir.SourceRef{}
		resource = &copy
	}
	data, err = json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("encode canonical resource: %w", err)
	}
	return data, nil
}

func contentDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func intrinsicSupportFileDigests(root string, source *fsboundary.SourceFS, entry catalog.Entry, contract lockfile.SourceRevisionContract) (map[string]string, error) {
	var supportFiles []string
	if skill, ok := entry.Resource.(*ir.Skill); ok {
		supportFiles = skill.SupportFiles
	} else if entry.Kind == ir.KindHook && contract.HookSupport && entry.Path != "" {
		rel, err := lexicallyContained(root, entry.Path)
		if err != nil {
			return nil, err
		}
		supportFiles, err = catalog.HookSupportFilesFS(source, filepath.ToSlash(rel))
		if err != nil {
			return nil, err
		}
	}
	if len(supportFiles) == 0 {
		return nil, nil
	}
	if entry.Path == "" {
		return nil, errors.New("resource with support files has no source path")
	}
	resourceFile, err := containedPath(root, entry.Path)
	if err != nil {
		return nil, err
	}
	resourceRoot := filepath.Dir(resourceFile)
	digests := make(map[string]string, len(supportFiles))
	for _, supportPath := range supportFiles {
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
		rel, err := lexicallyContained(root, fileName)
		if err != nil {
			return nil, err
		}
		data, err := source.ReadFile(filepath.ToSlash(rel))
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
	if err := validateTreeSymlinks(snapshotReal, rootReal, rootPath); err != nil {
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

func validateTreeSymlinks(snapshotReal, rootReal, rootPath string) error {
	return filepath.WalkDir(rootReal, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(name)
		if err == nil {
			_, err = lexicallyContained(rootReal, resolved)
		}
		if err == nil {
			return nil
		}
		target, _ := os.Readlink(name)
		rel, relErr := filepath.Rel(snapshotReal, name)
		if relErr != nil {
			rel = name
		}
		return fmt.Errorf("%w: registry symlink %q -> %q does not resolve inside configured root %q; remove it from the registry or narrow the registry entry's path: %w",
			ErrPathEscape, filepath.ToSlash(rel), target, rootPath, err)
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
