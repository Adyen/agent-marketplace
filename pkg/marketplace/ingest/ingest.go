// Package ingest builds Marketplace registry revisions from cloned registry trees.
package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/collection"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	hccir "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/licensepolicy"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/google/licensecheck"
)

var ErrNoValidResources = errors.New("catalog error: no valid resources remain after skipping malformed resources; previous publication is preserved")

// Revision is the result of ingesting a single registry commit.
type Revision struct {
	Occurrence          store.RegistryOccurrence
	Resources           []store.ResourceRevision
	ResourceOccurrences []store.ResourceOccurrence
	Assessments         []store.ResourceAssessment
	ExtraBlobs          []store.BlobUpload
	InspectionBlobs     []store.BlobUpload
	ManifestPayload     []byte
	ManifestDigest      store.RevisionDigest
	// CollectionManifests contains standalone canonical collection manifests.
	// They are uploaded by the worker under their manifest content digest; the
	// collection remains virtual and its tar archive is never used for install.
	CollectionManifests []store.BlobUpload
	Collections         []store.CollectionRevision
	// ResourceArchives retains locally-built canonical archives for index-only
	// consumers. They are never published by the worker.
	ResourceArchives      map[string][]byte
	ResourceArchiveDigest map[string]string
	// Diagnostics are the non-fatal catalog diagnostics observed per root.
	// Fatal diagnostics abort the build and are returned as the error.
	Diagnostics []RootDiagnostic
}

// RootDiagnostic is one catalog diagnostic attributed to the registry root
// under which it was observed.
type RootDiagnostic struct {
	Root    string `json:"root"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   bool   `json:"error,omitempty"`
}

// BuildRegistryRevision discovers resources under dir for the given registry and
// commit, builds artifacts, applies policy, and produces a publishable revision.
func BuildRegistryRevision(ctx context.Context, cfg config.Config, mpID store.MarketplaceID, registry store.Registry, dir, commit string) (Revision, error) {
	var rev Revision

	if mpID == "" {
		return rev, fmt.Errorf("marketplace id is required")
	}
	if registry.ID == "" {
		return rev, fmt.Errorf("registry id is required")
	}
	if dir == "" || commit == "" {
		return rev, fmt.Errorf("dir and commit are required")
	}
	if len(registry.Origins) == 0 {
		return rev, fmt.Errorf("registry %q has no origin", registry.ID)
	}
	origin := registry.Origins[len(registry.Origins)-1]

	policy := effectivePolicy(cfg, registry.PolicyOverrides)
	limits := effectiveLimits(cfg.Policy.Limits, registry.PolicyOverrides)
	license := effectiveLicensePolicy(cfg.Policy, registry.PolicyOverrides)
	policyDigest, err := resourcePolicyDigest(policy, limits, license)
	if err != nil {
		return rev, err
	}

	ctx, cancel := context.WithTimeout(ctx, limits.IngestionDuration.Duration())
	defer cancel()

	type processed struct {
		entry             catalog.Entry
		root              store.RegistryRoot
		sourcePath        string
		result            artifact.BuildResult
		supportFiles      map[string]string
		inspectionBody    []byte
		inspectionSupport analyze.FileSet
	}

	var processedResults []processed
	resourceResults := make(map[string]processed)
	resourceArchives := make(map[string][]byte)
	resourceArchiveDigests := make(map[string]string)
	inspectionDigests := make(map[string]string)
	var inspectionBlobs []store.BlobUpload
	var collectionEntries []catalog.Entry
	collectionPaths := make(map[string]string)
	seen := make(map[string]string)
	skippedResources := false

	for _, root := range registry.Roots {
		rootDir := filepath.Join(dir, filepath.Clean(root.Path))
		rel, err := filepath.Rel(dir, rootDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rev, fmt.Errorf("root %q escapes registry directory", root.Path)
		}
		if _, err := os.Stat(rootDir); err != nil {
			if os.IsNotExist(err) {
				return rev, fmt.Errorf("root path %q does not exist", root.Path)
			}
			return rev, fmt.Errorf("stat root %q: %w", root.Path, err)
		}

		entries, diagnostics, err := discoverRootEntries(rootDir, dir, root, registry, origin)
		if err != nil {
			return rev, fmt.Errorf("load catalog at %q: %w", root.Path, err)
		}
		for i, d := range diagnostics {
			if skippableResourceDiagnostic(d) {
				skippedResources = true
				d.Severity = diag.SeverityWarning
				d.Message = "Skipped malformed resource: " + d.Message
				diagnostics[i] = d
			}
			if d.Severity <= diag.SeverityWarning {
				slog.Warn("catalog diagnostic", "path", root.Path, "code", d.Code, "message", d.Message)
			} else {
				slog.Info("catalog diagnostic", "path", root.Path, "code", d.Code, "message", d.Message)
			}
			rev.Diagnostics = append(rev.Diagnostics, RootDiagnostic{Root: root.Path, Code: string(d.Code), Message: d.Message, Error: d.IsError()})
		}
		if diag.HasErrors(diagnostics) {
			d := firstErrorDiagnostic(diagnostics)
			return rev, fmt.Errorf("catalog error at %q: %s: %s", root.Path, d.Code, d.Message)
		}

		for _, entry := range entries {
			kind := api.FromIR(entry.Kind)
			if kind == "" {
				slog.Debug("skipping unsupported resource kind", "kind", entry.Kind, "name", entry.Name)
				continue
			}
			key := kind + "/" + entry.Name
			path, err := registryEntryPath(dir, root, entry)
			if err != nil {
				return rev, fmt.Errorf("source path for %s %q: %w", kind, entry.Name, err)
			}
			if keptPath, duplicate := seen[key]; duplicate {
				if keptPath == path {
					return rev, fmt.Errorf("duplicate resource %q at %q in registry %q", key, path, registry.ID)
				}
				slog.Warn("duplicate resource identity across registry paths; keeping first occurrence",
					"code", "marketplace.ingest.duplicate-resource",
					"registry", registry.ID,
					"resource", key,
					"kept_path", keptPath,
					"ignored_path", path,
					"action", "assign distinct registry-local names",
				)
				continue
			}
			seen[key] = path

			if entry.Kind == hccir.KindCollection {
				collectionEntries = append(collectionEntries, entry)
				collectionPaths[entry.Name] = path
				continue
			}

			files, modes, err := fileSetForResource(dir, entry)
			if err != nil {
				return rev, fmt.Errorf("support files for %s %q: %w", kind, entry.Name, err)
			}
			var archiveSupport map[string]hccartifact.SupportFile
			if registry.Acquisition == store.AcquisitionMirror {
				archiveSupport = archiveSupportFiles(files, modes)
			}
			r, err := buildResource(ctx, registry, root, entry, origin, commit, policy, limits, files, archiveSupport, nil)
			if err != nil {
				return rev, fmt.Errorf("build %s %q: %w", kind, entry.Name, err)
			}
			rawArchive, rawArchiveDigest := r.RawArchive, r.ArchiveDigest
			r = applyPolicyToResult(r, registry.ID, limits, license, registry.Acquisition)
			if registry.Acquisition == store.AcquisitionIndexOnly && len(rawArchive) > 0 {
				resourceArchives[key] = rawArchive
				resourceArchiveDigests[key] = rawArchiveDigest
			}
			p := processed{entry: entry, root: root, sourcePath: path, result: r, supportFiles: supportFileDigests(files)}
			primary, projectionSupport := inspectionInput(entry.Resource, r, files)
			p.inspectionBody, p.inspectionSupport = primary, projectionSupport
			projection, err := inspection.Build(
				store.ResourceCoordinate{Marketplace: mpID, Registry: registry.ID, Kind: kind, Name: entry.Name},
				r.ContentDigest, primary, projectionSupport,
				limits.FileCount, limits.FileBytes, limits.RepositoryBytes,
			)
			if err != nil {
				return rev, fmt.Errorf("inspection projection for %s %q: %w", kind, entry.Name, err)
			}
			inspectionDigests[key] = projection.Manifest.ManifestDigest
			inspectionBlobs = appendUpload(inspectionBlobs, store.ArchiveDigest(projection.Manifest.ManifestDigest), projection.Payload)
			for digest, data := range projection.Bodies {
				inspectionBlobs = appendUpload(inspectionBlobs, store.ArchiveDigest(digest), data)
			}
			processedResults = append(processedResults, p)
			resourceResults[key] = p
		}
	}

	collectionByName := make(map[string]catalog.Entry, len(collectionEntries))
	for _, e := range collectionEntries {
		collectionByName[e.Name] = e
	}

	type collectionLeaf struct {
		result artifact.BuildResult
		paths  [][]string
	}
	type resolvedCollection struct {
		result artifact.BuildResult
		leaves []collectionLeaf
	}
	collectionCache := make(map[string]resolvedCollection)

	var resolveCollection func(name string, visiting map[string]bool) (artifact.BuildResult, []collectionLeaf, error)
	resolveCollection = func(name string, visiting map[string]bool) (artifact.BuildResult, []collectionLeaf, error) {
		if visiting[name] {
			return artifact.BuildResult{}, nil, fmt.Errorf("cyclic collection reference: %q", name)
		}
		if cached, ok := collectionCache[name]; ok {
			return cached.result, cached.leaves, nil
		}
		entry, ok := collectionByName[name]
		if !ok {
			return artifact.BuildResult{}, nil, fmt.Errorf("collection %q not found", name)
		}
		c, ok := entry.Resource.(*hccir.Collection)
		if !ok {
			return artifact.BuildResult{}, nil, fmt.Errorf("resource %q is not a collection", name)
		}
		visiting[name] = true
		defer func() { delete(visiting, name) }()

		root := store.RegistryRoot{Path: c.SourcePath}
		var directResults []artifact.BuildResult
		var leaves []collectionLeaf
		addLeaf := func(result artifact.BuildResult, path []string) error {
			for i := range leaves {
				if leaves[i].result.Kind == result.Kind && leaves[i].result.Name == result.Name {
					if leaves[i].result.ContentDigest != result.ContentDigest {
						return fmt.Errorf("collection %q contains conflicting revisions of %s/%q", name, result.Kind, result.Name)
					}
					leaves[i].paths = appendUniquePath(leaves[i].paths, path)
					return nil
				}
			}
			leaves = append(leaves, collectionLeaf{result: result, paths: [][]string{path}})
			return nil
		}
		resolveChild := func(childName string, path []string) error {
			child, childLeaves, err := resolveCollection(childName, visiting)
			if err != nil {
				return err
			}
			directResults = append(directResults, child)
			for _, leaf := range childLeaves {
				for _, childPath := range leaf.paths {
					fullPath := append(append([]string{name}, path...), childPath...)
					if err := addLeaf(leaf.result, fullPath); err != nil {
						return err
					}
				}
			}
			return nil
		}

		for _, m := range c.Members {
			memberKind := m.Kind
			if memberKind == 0 {
				memberKind = hccir.KindSkill
			}
			effectiveRegistry := m.Registry
			if effectiveRegistry == "" {
				effectiveRegistry = c.SourceRegistryURL
			}
			effectiveRef := m.Ref
			if effectiveRef == "" {
				effectiveRef = c.SourceRef
			}
			if effectiveRegistry != origin || effectiveRef != registry.Ref {
				return artifact.BuildResult{}, nil, fmt.Errorf("collection %q member %q references external registry/ref which is not supported", name, m.Name)
			}
			if memberKind == hccir.KindCollection {
				if err := resolveChild(m.Name, nil); err != nil {
					return artifact.BuildResult{}, nil, err
				}
				continue
			}
			kind := api.FromIR(memberKind)
			key := kind + "/" + m.Name
			rp, ok := resourceResults[key]
			if !ok {
				return artifact.BuildResult{}, nil, fmt.Errorf("collection %q member %s %q not found in registry", name, kind, m.Name)
			}
			directResults = append(directResults, rp.result)
			if err := addLeaf(rp.result, []string{name}); err != nil {
				return artifact.BuildResult{}, nil, err
			}
		}
		for _, inc := range c.Includes {
			if err := resolveChild(inc, nil); err != nil {
				return artifact.BuildResult{}, nil, err
			}
		}

		savedURL, savedRef, savedKey, savedPath := c.SourceRegistryURL, c.SourceRef, c.SourceCatalogKey, c.SourcePath
		c.SourceRegistryURL, c.SourceRef, c.SourceCatalogKey, c.SourcePath = "", "", "", ""
		r, err := buildResource(ctx, registry, root, entry, origin, commit, policy, limits, nil, nil, directResults)
		c.SourceRegistryURL, c.SourceRef, c.SourceCatalogKey, c.SourcePath = savedURL, savedRef, savedKey, savedPath
		if err != nil {
			return artifact.BuildResult{}, nil, fmt.Errorf("build collection %q: %w", name, err)
		}
		r = applyPolicyToResult(r, registry.ID, limits, license, registry.Acquisition)
		if registry.Acquisition != store.AcquisitionMirror {
			return artifact.BuildResult{}, nil, fmt.Errorf("collection %q requires mirror acquisition", name)
		}
		manifest, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(r.ManifestPayload))
		if err != nil {
			return artifact.BuildResult{}, nil, fmt.Errorf("decode collection %q manifest: %w", name, err)
		}
		if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
			return artifact.BuildResult{}, nil, fmt.Errorf("verify collection %q manifest: %w", name, err)
		}
		manifest.Leaves = nil
		for _, leaf := range leaves {
			manifestLeaf := hccartifact.CollectionMemberEntry{Kind: leaf.result.Kind, Name: leaf.result.Name, RegistryID: string(registry.ID), Revision: leaf.result.ContentDigest, InclusionPaths: leaf.paths}
			manifest.Leaves = append(manifest.Leaves, manifestLeaf)
		}
		manifestPayload, manifestDigest, err := hccartifact.EncodeCollectionManifest(manifest)
		if err != nil {
			return artifact.BuildResult{}, nil, fmt.Errorf("encode collection %q graph: %w", name, err)
		}
		r.ManifestPayload, r.ManifestDigest, r.ContentDigest = manifestPayload, manifestDigest, manifestDigest
		r.Artifact.Manifest.ContentDigest = manifestDigest
		r.Artifact.Payload = manifestPayload
		primary, projectionSupport := inspectionInput(entry.Resource, r, nil)
		projection, err := inspection.Build(
			store.ResourceCoordinate{Marketplace: mpID, Registry: registry.ID, Kind: "collection", Name: entry.Name},
			r.ContentDigest, primary, projectionSupport,
			limits.FileCount, limits.FileBytes, limits.RepositoryBytes,
		)
		if err != nil {
			return artifact.BuildResult{}, nil, fmt.Errorf("inspection projection for collection %q: %w", name, err)
		}
		key := "collection/" + entry.Name
		inspectionDigests[key] = projection.Manifest.ManifestDigest
		inspectionBlobs = appendUpload(inspectionBlobs, store.ArchiveDigest(projection.Manifest.ManifestDigest), projection.Payload)
		for digest, data := range projection.Bodies {
			inspectionBlobs = appendUpload(inspectionBlobs, store.ArchiveDigest(digest), data)
		}
		collectionCache[name] = resolvedCollection{result: r, leaves: leaves}
		return r, leaves, nil
	}

	for _, entry := range collectionEntries {
		if registry.Acquisition != store.AcquisitionMirror {
			slog.Warn("skipping collection for non-mirror registry", "registry", registry.ID, "collection", entry.Name, "acquisition", registry.Acquisition)
			continue
		}
		r, _, err := resolveCollection(entry.Name, make(map[string]bool))
		if err != nil {
			if strings.Contains(err.Error(), "rejected") {
				slog.Warn("skipping collection without publishable leaves", "registry", registry.ID, "collection", entry.Name, "err", err)
				continue
			}
			return rev, fmt.Errorf("resolve collection %q: %w", entry.Name, err)
		}
		body, support := inspectionInput(entry.Resource, r, nil)
		collectionRoot := store.RegistryRoot{}
		if collection, ok := entry.Resource.(*hccir.Collection); ok {
			collectionRoot.Path = collection.SourcePath
		}
		processedResults = append(processedResults, processed{entry: entry, root: collectionRoot, sourcePath: collectionPaths[entry.Name], result: r, inspectionBody: body, inspectionSupport: support})
	}

	// Validate every published collection against the complete set of manifests
	// produced for this registry. This prevents a malformed or stale flattened
	// Leaves list from becoming part of the immutable publication.
	publishedCollectionManifests := make(map[string]hccartifact.CollectionManifest, len(collectionEntries))
	for _, p := range processedResults {
		if p.entry.Kind != hccir.KindCollection {
			continue
		}
		collection, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(p.result.ManifestPayload))
		if err != nil {
			return rev, fmt.Errorf("decode collection %q manifest for graph validation: %w", p.entry.Name, err)
		}
		if err := hccartifact.VerifyCollectionManifest(collection, payload); err != nil {
			return rev, fmt.Errorf("verify collection %q manifest for graph validation: %w", p.entry.Name, err)
		}
		publishedCollectionManifests[collection.ContentDigest] = collection
	}
	for _, collection := range publishedCollectionManifests {
		if _, err := hccartifact.ValidateAndExpandCollectionManifestGraph(collection, publishedCollectionManifests); err != nil {
			return rev, fmt.Errorf("validate published collection %q graph: %w", collection.Name, err)
		}
	}

	var manifest artifact.RegistryRevisionManifest
	manifest.SchemaVersion = artifact.RegistryRevisionSchemaV1
	manifest.Generator = artifact.RegistryRevisionGeneratorV1
	manifest.Resources = make([]artifact.RevisionResource, 0, len(processedResults))

	for _, p := range processedResults {
		kind := api.FromIR(p.entry.Kind)
		contentDigest := p.result.ContentDigest
		archiveDigest := p.result.ArchiveDigest
		if kind == "collection" {
			// A collection is virtual. Its revision points at the standalone
			// collection manifest and never at a tar archive.
			contentDigest = p.result.ManifestDigest
			archiveDigest = ""
		}
		manifest.Resources = append(manifest.Resources, artifact.RevisionResource{
			Kind:          kind,
			Name:          p.entry.Name,
			ContentDigest: contentDigest,
			ArchiveDigest: archiveDigest,
			SupportFiles:  p.supportFiles,
		})
	}

	payload, digest, err := artifact.CanonicalRegistryRevisionPayload(manifest)
	if err != nil {
		return rev, fmt.Errorf("canonical registry revision manifest: %w", err)
	}

	occ := store.NormalizeRegistryOccurrence(store.RegistryOccurrence{
		RegistryID:                     registry.ID,
		Ref:                            registry.Ref,
		Roots:                          append([]store.RegistryRoot(nil), registry.Roots...),
		Commit:                         commit,
		RegistryRevisionManifestDigest: store.RevisionDigest(digest),
		Acquisition:                    registry.Acquisition,
		SourceOrigin:                   origin,
		OccurredAt:                     time.Now().UTC(),
	})

	var resources []store.ResourceRevision
	var resourceOccurrences []store.ResourceOccurrence
	var assessments []store.ResourceAssessment
	var extraBlobs []store.BlobUpload
	var collectionManifests []store.BlobUpload
	var collections []store.CollectionRevision
	for _, p := range processedResults {
		kind := api.FromIR(p.entry.Kind)
		coord := store.ResourceCoordinate{
			Marketplace: mpID,
			Registry:    registry.ID,
			Kind:        kind,
			Name:        p.entry.Name,
		}

		contentDigest := p.result.ContentDigest
		archiveDigest := p.result.ArchiveDigest
		if kind == "collection" {
			contentDigest = p.result.ManifestDigest
			archiveDigest = ""
		}
		rev := store.ResourceRevision{
			Coordinate:    coord,
			ContentDigest: store.RevisionDigest(contentDigest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
			License:       p.result.License,
			Description:   p.result.Description,
		}
		assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
			Coordinate:               coord,
			ContentDigest:            rev.ContentDigest,
			AnalyzerVersion:          analyze.AnalyzerVersionV1,
			PolicyDigest:             policyDigest,
			Decision:                 assessmentDecision(p.result),
			Findings:                 toStoreFindings(p.result.Findings, inspection.PrimaryPath(kind), p.inspectionBody, p.inspectionSupport),
			InspectionManifestDigest: store.RevisionDigest(inspectionDigests[kind+"/"+p.entry.Name]),
			AssessedAt:               time.Now().UTC(),
		})
		if err != nil {
			return Revision{}, fmt.Errorf("finalize assessment for %s %q: %w", kind, p.entry.Name, err)
		}
		resources = append(resources, rev)
		resourceOccurrences = append(resourceOccurrences, store.ResourceOccurrence{
			Coordinate:     coord,
			ContentDigest:  rev.ContentDigest,
			Commit:         commit,
			ManifestDigest: store.RevisionDigest(digest),
			SourcePath:     p.sourcePath,
		})
		assessments = append(assessments, assessment)

		if kind == "collection" {
			if p.result.ManifestDigest == "" || len(p.result.ManifestPayload) == 0 {
				return Revision{}, fmt.Errorf("collection %q has no standalone manifest", p.entry.Name)
			}
			collection, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(p.result.ManifestPayload))
			if err != nil {
				return Revision{}, fmt.Errorf("decode collection %q manifest: %w", p.entry.Name, err)
			}
			if err := hccartifact.VerifyCollectionManifest(collection, payload); err != nil {
				return Revision{}, fmt.Errorf("verify collection %q manifest: %w", p.entry.Name, err)
			}
			if _, err := hccartifact.ValidateAndExpandCollectionManifestGraph(collection, publishedCollectionManifests); err != nil {
				return Revision{}, fmt.Errorf("collection %q manifest graph: %w", p.entry.Name, err)
			}
			if collection.ContentDigest != p.result.ManifestDigest {
				return Revision{}, fmt.Errorf("collection %q manifest digest changed during ingest", p.entry.Name)
			}
			collectionManifests = appendUpload(collectionManifests, store.ArchiveDigest(p.result.ManifestDigest), p.result.ManifestPayload)
			members := make([]store.ResourceCoordinate, 0, len(collection.Members))
			for _, member := range collection.Members {
				members = append(members, store.ResourceCoordinate{Marketplace: mpID, Registry: store.RegistryID(member.RegistryID), Kind: member.Kind, Name: member.Name})
			}
			includes := make([]store.ResourceCoordinate, 0, len(collection.Includes))
			for _, include := range collection.Includes {
				includes = append(includes, store.ResourceCoordinate{Marketplace: mpID, Registry: registry.ID, Kind: "collection", Name: include})
			}
			collections = append(collections, store.CollectionRevision{
				ID: p.result.ManifestDigest, RegistryID: registry.ID, ManifestDigest: store.RevisionDigest(p.result.ManifestDigest),
				Members: members, Includes: includes,
			})
		}

		if kind != "collection" && registry.Acquisition == store.AcquisitionMirror && p.result.RawArchive != nil {
			extraBlobs = appendUpload(extraBlobs, store.ArchiveDigest(p.result.ArchiveDigest), p.result.RawArchive)
			if p.result.PlaceholderArchive != nil {
				extraBlobs = appendUpload(extraBlobs, store.PlaceholderArchiveKey(store.ArchiveDigest(p.result.ArchiveDigest)), p.result.PlaceholderArchive)
			}
		}
	}

	if skippedResources && len(resources) == 0 {
		return rev, ErrNoValidResources
	}

	return Revision{
		Occurrence:            occ,
		Resources:             resources,
		ResourceOccurrences:   resourceOccurrences,
		Assessments:           assessments,
		ExtraBlobs:            append(extraBlobs, collectionManifests...),
		InspectionBlobs:       inspectionBlobs,
		CollectionManifests:   collectionManifests,
		Collections:           collections,
		ManifestPayload:       payload,
		ManifestDigest:        store.RevisionDigest(digest),
		ResourceArchives:      resourceArchives,
		ResourceArchiveDigest: resourceArchiveDigests,
		Diagnostics:           rev.Diagnostics,
	}, nil
}

func discoverRootEntries(rootDir, registryDir string, root store.RegistryRoot, registry store.Registry, origin string) ([]catalog.Entry, []diag.Diagnostic, error) {
	discovered, err := catalog.Discover(rootDir, root.SourceHarness)
	if err != nil {
		return nil, nil, err
	}
	if err := errors.Join(discovered.Errors...); err != nil {
		return nil, nil, err
	}

	bundleDirs := []string{rootDir}
	catalogs := map[string]catalog.LoadedCatalog{rootDir: discovered}
	if len(discovered.SubBundles) > 0 {
		bundleDirs = bundleDirs[:0]
		catalogs = discovered.SubBundles
		for bundleDir := range catalogs {
			bundleDirs = append(bundleDirs, bundleDir)
		}
		sort.Strings(bundleDirs)
	}

	var entries []catalog.Entry
	var diagnostics []diag.Diagnostic
	for _, bundleDir := range bundleDirs {
		bundle := catalogs[bundleDir]
		entries = append(entries, bundle.Entries...)
		diagnostics = append(diagnostics, bundle.Diagnostics...)

		rel, err := filepath.Rel(registryDir, bundleDir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, nil, fmt.Errorf("nested bundle %q escapes registry directory", bundleDir)
		}
		bundlePath := filepath.ToSlash(rel)
		cols, err := collection.DiscoverCollections(bundleDir, string(registry.Slug), origin, registry.Ref, bundlePath)
		if err != nil {
			return nil, nil, fmt.Errorf("discover collections at %q: %w", bundlePath, err)
		}
		colEntries, err := catalog.EntriesFromCollections(cols)
		if err != nil {
			return nil, nil, fmt.Errorf("collection entries at %q: %w", bundlePath, err)
		}
		entries = append(entries, colEntries...)
	}
	return entries, diagnostics, nil
}

func skippableResourceDiagnostic(d diag.Diagnostic) bool {
	if !d.IsError() || d.Resource == "" {
		return false
	}
	switch d.Code {
	case diag.CodeIngestClaudeLoadSkillFailed, diag.CodeIngestFactoryLoadSkillFailed,
		diag.CodeIngestCursorLoadSkillFailed, diag.CodeIngestGeminiLoadSkillFailed,
		diag.CodeIngestDevinLoadSkillFailed, diag.CodeIngestCodexLoadResourceFailed,
		diag.CodeIngestClaudeLoadCommandFailed, diag.CodeIngestClaudeLoadSubagentFailed,
		diag.CodeIngestFactoryLoadCommandFailed, diag.CodeIngestFactoryLoadSubagentFailed,
		diag.CodeIngestFactoryLoadMcpServersFailed, diag.CodeIngestFactoryLoadRuleFailed,
		diag.CodeIngestCursorLoadCommandFailed, diag.CodeIngestCursorLoadSubagentFailed,
		diag.CodeIngestCursorLoadRuleFailed, diag.CodeIngestGeminiLoadCommandFailed,
		diag.CodeIngestGeminiLoadSubagentFailed, diag.CodeIngestGeminiLoadHookFailed,
		diag.CodeIngestDevinLoadSubagentFailed, diag.CodeIngestDevinLoadHookFailed,
		diag.CodeIngestDevinLoadMcpServersFailed:
		return true
	}
	return false
}

func firstErrorDiagnostic(ds []diag.Diagnostic) diag.Diagnostic {
	for _, d := range ds {
		if d.IsError() {
			return d
		}
	}
	return diag.Diagnostic{}
}

func appendUniquePath(paths [][]string, candidate []string) [][]string {
	for _, path := range paths {
		if slices.Equal(path, candidate) {
			return paths
		}
	}
	return append(paths, append([]string(nil), candidate...))
}

func registryEntryPath(dir string, root store.RegistryRoot, entry catalog.Entry) (string, error) {
	sourcePath := entry.Path
	if sourcePath == "" {
		return "", fmt.Errorf("resource has no source file")
	}
	if !filepath.IsAbs(sourcePath) {
		base := root.Path
		if collection, ok := entry.Resource.(*hccir.Collection); ok && collection.SourcePath != "" {
			base = collection.SourcePath
		}
		sourcePath = filepath.Join(dir, base, sourcePath)
	}
	rel, err := filepath.Rel(dir, sourcePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source path escapes registry directory")
	}
	normalized, err := store.NormalizeSourcePath(filepath.ToSlash(rel))
	if err != nil {
		return "", fmt.Errorf("normalize source path: %w", err)
	}
	if strings.ContainsFunc(normalized, unicode.IsControl) {
		return "", fmt.Errorf("normalize source path: contains control characters")
	}
	return normalized, nil
}

// buildResource analyzes one catalog entry and builds its artifact. License
// policy is deliberately not an input here: analysis is independent of it, and
// the license decision is taken once, afterwards, in applyPolicyToResult.
func buildResource(ctx context.Context, registry store.Registry, root store.RegistryRoot, entry catalog.Entry, origin, commit string, policy analyze.Policy, limits config.Limits, files analyze.FileSet, archiveSupport map[string]hccartifact.SupportFile, memberResults []artifact.BuildResult) (artifact.BuildResult, error) {
	opts := analyze.DefaultOptions()
	opts.Policy = policy
	opts.Registry = analyze.Registry{
		RegistryURL: origin,
		Ref:         registry.Ref,
		Path:        root.Path,
		ID:          string(registry.ID),
		Slug:        registry.Slug,
		License:     resolvedResourceLicense(entry.Resource, registry.License, files),
	}
	opts.Limits = analyze.Limits{
		MaxFileBytes:  limits.FileBytes,
		MaxTotalBytes: limits.RepositoryBytes,
		MaxFiles:      limits.FileCount,
		MaxFindings:   defaultMaxFindings,
	}

	switch r := entry.Resource.(type) {
	case *hccir.Collection:
		if r.SourcePath != "" {
			opts.Registry.Path = r.SourcePath
		}
		return artifact.BuildCollectionManifest(ctx, r, memberResults, opts)
	case hccir.Resource:
		return artifact.BuildResourceArtifact(ctx, r, files, archiveSupport, opts)
	default:
		return artifact.BuildResult{}, fmt.Errorf("unknown resource type %T", entry.Resource)
	}
}

// inspectionInput turns the typed IR into the stable primary document used by
// the inspection projection. Registry paths are intentionally not used here:
// projections must be identical for mirror and index-only acquisition.
func inspectionInput(resource hccir.Resource, result artifact.BuildResult, files analyze.FileSet) ([]byte, analyze.FileSet) {
	support := files
	switch r := resource.(type) {
	case *hccir.Skill:
		return []byte(r.Body), support
	case *hccir.Command:
		return []byte(r.Body), support
	case *hccir.Subagent:
		return []byte(r.Body), support
	case *hccir.Hook:
		body := r.Command
		if body == "" {
			body = r.Prompt
		} else if r.Prompt != "" {
			if support == nil {
				support = make(analyze.FileSet)
			}
			support["prompt.txt"] = []byte(r.Prompt)
		}
		return []byte(body), support
	case *hccir.McpServer:
		return redactedMCPInspectionPrimary(r), nil
	case *hccir.Collection:
		return append([]byte(nil), result.ManifestPayload...), support
	default:
		return nil, support
	}
}

// redactedMCPInspectionPrimary is deliberately separate from the canonical
// artifact representation. The latter is used for installation and therefore
// preserves the complete MCP configuration, including secret values. An
// inspection projection is public, so it may contain identity and key names but
// must never contain credentials, environment values, header values, or argv
// values. Build applies the configured file/total limits to the resulting
// canonical JSON before it is published.
func redactedMCPInspectionPrimary(server *hccir.McpServer) []byte {
	if server == nil {
		return []byte(`{"name":""}`)
	}

	primary := struct {
		Name                string   `json:"name"`
		Command             string   `json:"command,omitempty"`
		ArgumentCount       int      `json:"argument_count,omitempty"`
		URLOrigin           string   `json:"url_origin,omitempty"`
		Transport           string   `json:"transport,omitempty"`
		EnvironmentKeys     []string `json:"environment_keys,omitempty"`
		HeaderKeys          []string `json:"header_keys,omitempty"`
		OAuthClientID       string   `json:"oauth_client_id,omitempty"`
		OAuthResourceOrigin string   `json:"oauth_resource_origin,omitempty"`
		Disabled            *bool    `json:"disabled,omitempty"`
	}{
		Name:                server.Name(),
		Command:             executableIdentity(server.Command),
		ArgumentCount:       len(server.Args),
		URLOrigin:           safeURLOrigin(server.URL),
		Transport:           server.Transport,
		EnvironmentKeys:     sortedStringKeys(server.Env),
		HeaderKeys:          sortedStringKeys(server.Headers),
		OAuthClientID:       server.OAuthClientID,
		OAuthResourceOrigin: safeURLOrigin(server.OAuthResource),
	}
	if server.DisabledSet {
		primary.Disabled = &server.Disabled
	}
	data, err := json.Marshal(primary)
	if err != nil {
		// All fields above are JSON primitives or strings, so this is not
		// expected. Keep a safe valid primary if a future field changes that.
		return []byte(`{"name":""}`)
	}
	return data
}

func sortedStringKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func executableIdentity(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	// Command lines may contain shell snippets or inline credentials. Only the
	// executable token is useful identity and it cannot expose trailing argv.
	return fields[0]
}

func safeURLOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	// Userinfo, path, query, and fragment can all carry bearer tokens or other
	// credentials. Scheme and authority are sufficient to identify the endpoint
	// without copying attacker-controlled URL values into a public projection.
	return parsed.Scheme + "://" + parsed.Host
}

func supportFileDigests(files analyze.FileSet) map[string]string {
	if len(files) == 0 {
		return nil
	}
	digests := make(map[string]string, len(files))
	for name, data := range files {
		digests[name] = artifact.RevisionDigest(data)
	}
	return digests
}

func fileSetForResource(dir string, entry catalog.Entry) (analyze.FileSet, map[string]bool, error) {
	files := make(analyze.FileSet)
	modes := make(map[string]bool)
	if entry.Path == "" {
		return files, modes, nil
	}

	resFile := entry.Path
	if !filepath.IsAbs(resFile) {
		resFile = filepath.Join(dir, filepath.Clean(resFile))
	}
	resRoot := filepath.Dir(resFile)
	relRoot, err := filepath.Rel(dir, resRoot)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return nil, nil, fmt.Errorf("resource registry escapes registry directory: %q", entry.Path)
	}

	supportFiles, err := supportFilesFor(entry.Resource)
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]struct{}, len(supportFiles))
	for _, rel := range supportFiles {
		normalized, err := validateSupportRelativePath(rel)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := seen[normalized]; exists {
			return nil, nil, fmt.Errorf("duplicate support file path: %q", rel)
		}
		seen[normalized] = struct{}{}
		filePath := filepath.Join(resRoot, filepath.FromSlash(normalized))
		if err := rejectSymlinkPath(resRoot, filePath); err != nil {
			return nil, nil, fmt.Errorf("support file %q: %w", rel, err)
		}
		data, err := readBoundedFile(filePath, 2*1024*1024)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil, fmt.Errorf("support file %q is missing", rel)
			}
			return nil, nil, fmt.Errorf("read support file %q: %w", rel, err)
		}
		info, err := os.Stat(filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("stat support file %q: %w", rel, err)
		}
		files[normalized] = data
		modes[normalized] = info.Mode().Perm()&0o111 != 0
	}
	return files, modes, nil
}

func archiveSupportFiles(files analyze.FileSet, modes map[string]bool) map[string]hccartifact.SupportFile {
	if len(files) == 0 {
		return nil
	}
	support := make(map[string]hccartifact.SupportFile, len(files))
	for name, data := range files {
		support[name] = hccartifact.SupportFile{Data: data, Executable: modes[name]}
	}
	return support
}

func resourceInfo(r hccir.Resource) (extra map[string]any, supportFiles []string, err error) {
	switch v := r.(type) {
	case *hccir.Skill:
		return v.Extra, v.SupportFiles, nil
	case *hccir.Command:
		return v.Extra, nil, nil
	case *hccir.Subagent:
		return v.Extra, nil, nil
	case *hccir.Rule:
		return v.Extra, nil, nil
	case *hccir.Hook:
		return v.Extra, nil, nil
	case *hccir.McpServer:
		return v.Extra, nil, nil
	case *hccir.Collection:
		return v.Extra, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported resource type %T", r)
	}
}

func supportFilesFor(r hccir.Resource) ([]string, error) {
	_, supportFiles, err := resourceInfo(r)
	return supportFiles, err
}

func validateSupportRelativePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("invalid support file path: %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsafe support file path: %q", name)
		}
	}
	return name, nil
}

func rejectSymlinkPath(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes resource root")
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink support files are not allowed")
		}
	}
	return nil
}

func readBoundedFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file %q size %d exceeds %d", path, info.Size(), maxBytes)
	}
	return os.ReadFile(path)
}

func resourceLicense(r hccir.Resource, fallback string) string {
	extra := resourceExtra(r)
	if extra == nil {
		return fallback
	}
	for _, key := range []string{"license", "License"} {
		if v, ok := extra[key].(string); ok && v != "" {
			return v
		}
	}
	if meta, ok := extra["metadata"].(map[string]any); ok {
		for _, key := range []string{"license", "License"} {
			if v, ok := meta[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return fallback
}

func resolvedResourceLicense(r hccir.Resource, fallback string, files analyze.FileSet) string {
	declared := resourceLicense(r, fallback)
	lower := strings.ToLower(declared)
	ids := make(map[string]struct{})
	for name, data := range files {
		path := strings.ToLower(filepath.ToSlash(name))
		base := filepath.Base(path)
		if (!strings.HasPrefix(base, "license") && !strings.HasPrefix(base, "copying")) ||
			(!strings.Contains(lower, path) && !strings.Contains(lower, base)) {
			continue
		}
		for _, match := range licensecheck.Scan(data).Match {
			ids[match.ID] = struct{}{}
		}
	}
	if len(ids) == 1 {
		for id := range ids {
			return id
		}
	}
	return declared
}

func resourceExtra(r hccir.Resource) map[string]any {
	extra, _, err := resourceInfo(r)
	if err != nil {
		return nil
	}
	return extra
}

func effectivePolicy(cfg config.Config, overrides map[string]any) analyze.Policy {
	hidden := cfg.Policy.HiddenContent
	if overrides != nil {
		if v, ok := overrides["hidden_content"].(string); ok && (v == "reject" || v == "warn") {
			hidden = v
		}
	}
	policy := analyze.PublicPolicy()
	if hidden == "reject" {
		policy.Outcomes = map[string]string{
			analyze.FindingHTMLComment: analyze.OutcomeReject,
			analyze.FindingZeroWidth:   analyze.OutcomeReject,
		}
	}
	return policy
}

func effectiveLimits(base config.Limits, overrides map[string]any) config.Limits {
	out := base
	if overrides == nil {
		return out
	}
	l, ok := overrides["limits"].(map[string]any)
	if !ok {
		return out
	}
	if v, ok := toInt64(l["repository_bytes"]); ok {
		out.RepositoryBytes = v
	}
	if v, ok := toInt(l["file_count"]); ok {
		out.FileCount = v
	}
	if v, ok := toInt64(l["file_bytes"]); ok {
		out.FileBytes = v
	}
	if v, ok := toInt64(l["artifact_bytes"]); ok {
		out.ArtifactBytes = v
	}
	if v, ok := parseDuration(l["clone_duration"]); ok {
		out.CloneDuration = config.Duration(v)
	}
	if v, ok := parseDuration(l["ingestion_duration"]); ok {
		out.IngestionDuration = config.Duration(v)
	}
	return out
}

// effectiveLicensePolicy resolves the global license policy against a registry's
// policy_overrides. An override that is absent, malformed, or unrecognised
// leaves the global value in place, so a typo cannot quietly relax a registry.
func effectiveLicensePolicy(base config.Policy, overrides map[string]any) licensePolicy {
	lp := licensePolicy{
		allowlist:  base.LicenseAllowlist,
		undeclared: base.UndeclaredLicenseOutcome(),
	}
	if overrides == nil {
		return lp
	}
	if v, ok := overrides["license_allowlist"].([]any); ok {
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			lp.allowlist = out
		}
	}
	if v, ok := overrides["license_undeclared"].(string); ok {
		resolved := config.ResolveUndeclaredLicenseOutcome(v)
		if resolved != v {
			slog.Warn("ignoring unrecognised license_undeclared policy override",
				"value", v, "using", lp.undeclared)
		} else {
			lp.undeclared = resolved
		}
	}
	return lp
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func parseDuration(v any) (time.Duration, bool) {
	switch d := v.(type) {
	case time.Duration:
		return d, true
	case string:
		pd, err := time.ParseDuration(d)
		if err != nil {
			return 0, false
		}
		return pd, true
	}
	return 0, false
}

const defaultMaxFindings = 1000

func stripArchive(r *artifact.BuildResult) {
	r.RawArchive = nil
	r.ArchiveDigest = ""
}

// licensePolicy is the effective license policy for one registry.
//
// The two license conditions it decides are distinct. A resource whose declared
// license is outside the allowlist is disallowed: the operator has seen that
// license and excluded it. A resource that declares no license at all is
// unknown, and what an unknown license means is the operator's call, so it has
// its own outcome. Collapsing the two would remove that choice and would also
// make the emitted message wrong for the far more common undeclared case.
type licensePolicy struct {
	// allowlist is empty when license policy is not enforced at all.
	allowlist []string
	// undeclared is reject, warn, or allow, already resolved by
	// config.ResolveUndeclaredLicenseOutcome, so it is never empty here.
	undeclared string
}

// enforced reports whether license policy applies. With no allowlist nothing is
// rejected or warned on license grounds, declared or not.
func (lp licensePolicy) enforced() bool { return len(lp.allowlist) > 0 }

func (lp licensePolicy) allowlistText() string { return strings.Join(lp.allowlist, ", ") }

// licenseDecisionAttrs describes a license decision the same way for every
// outcome, so an operator can grep one log shape and tell an undeclared license
// from a disallowed one by license_declared alone.
//
// It is a plain function rather than a closure because it runs once per resource
// per ingest: a closure would allocate on the accepted path too, where nothing
// is logged above debug.
func licenseDecisionAttrs(r artifact.BuildResult, registryID store.RegistryID, lp licensePolicy, code, outcome string) []any {
	return []any{
		"code", code,
		"registry", registryID,
		"kind", r.Kind,
		"resource", r.Name,
		"declared_license", r.License,
		"license_declared", strings.TrimSpace(r.License) != "",
		"outcome", outcome,
		"allowlist", lp.allowlist,
	}
}

// rejectOnLicense marks r rejected, records the reason, and logs the decision.
func rejectOnLicense(r *artifact.BuildResult, registryID store.RegistryID, lp licensePolicy, code, message string) {
	r.Rejected = true
	r.Findings = append(r.Findings, analyze.Finding{
		Code:     code,
		Severity: analyze.OutcomeReject,
		Message:  message,
	})
	slog.Warn("license policy rejected resource", licenseDecisionAttrs(*r, registryID, lp, code, analyze.OutcomeReject)...)
}

func debugEnabled() bool {
	return slog.Default().Enabled(context.Background(), slog.LevelDebug)
}

// applyLicensePolicy records the license decision for one built result. It
// returns the result and whether the decision stops further policy evaluation.
func applyLicensePolicy(r artifact.BuildResult, registryID store.RegistryID, lp licensePolicy) (artifact.BuildResult, bool) {
	if !lp.enforced() {
		return r, false
	}

	decision := licensepolicy.Evaluate(r.License, lp.allowlist, lp.undeclared)
	if decision.Code == analyze.PolicyLicenseUndeclared {
		switch decision.Outcome {
		case config.LicenseUndeclaredReject:
			rejectOnLicense(&r, registryID, lp, analyze.PolicyLicenseUndeclared, fmt.Sprintf(
				"resource declares no license, and policy.license_undeclared is %q: it is not installable while the license allowlist (%s) is configured",
				config.LicenseUndeclaredReject, lp.allowlistText()))
			return r, true
		case config.LicenseUndeclaredWarn:
			r.Findings = append(r.Findings, analyze.Finding{
				Code:     analyze.PolicyLicenseUndeclared,
				Severity: analyze.OutcomeWarn,
				Message: fmt.Sprintf(
					"resource declares no license, and policy.license_undeclared is %q: it stays installable but its license is unknown to the configured allowlist (%s)",
					config.LicenseUndeclaredWarn, lp.allowlistText()),
			})
			slog.Warn("license policy warned on undeclared license",
				licenseDecisionAttrs(r, registryID, lp, analyze.PolicyLicenseUndeclared, analyze.OutcomeWarn)...)
		default:
			// Allow: the operator has already decided that an unknown license
			// is acceptable here, so no finding is recorded. A finding would
			// otherwise be attached to the majority of resources in catalogs
			// that rarely declare licenses, and would be persisted into every
			// inspection projection for no decision the operator can act on.
			if debugEnabled() {
				slog.Debug("license policy allowed undeclared license",
					licenseDecisionAttrs(r, registryID, lp, analyze.PolicyLicenseUndeclared, analyze.OutcomeAllow)...)
			}
		}
		return r, false
	}

	// Matched against the declared value exactly as authored, not a trimmed
	// copy: trimming is only how "declares nothing" is recognised, and must not
	// widen which declared strings the allowlist accepts.
	if decision.Outcome == analyze.OutcomeReject {
		rejectOnLicense(&r, registryID, lp, analyze.PolicyLicenseNotAllowed, fmt.Sprintf(
			"declared license %q is not in the configured license allowlist (%s)",
			r.License, lp.allowlistText()))
		return r, true
	}

	if debugEnabled() {
		slog.Debug("license policy allowed declared license",
			licenseDecisionAttrs(r, registryID, lp, "", analyze.OutcomeAllow)...)
	}
	return r, false
}

func applyPolicyToResult(r artifact.BuildResult, registryID store.RegistryID, limits config.Limits, license licensePolicy, mode store.AcquisitionMode) artifact.BuildResult {
	if !r.Rejected {
		var stop bool
		r, stop = applyLicensePolicy(r, registryID, license)
		if !stop && limits.ArtifactBytes > 0 && len(r.RawArchive) > int(limits.ArtifactBytes) {
			artifactSize := len(r.RawArchive)
			r.Rejected = true
			r.Findings = append(r.Findings, analyze.Finding{
				Code:     analyze.PolicyArtifactTooLarge,
				Severity: analyze.OutcomeReject,
				Message:  fmt.Sprintf("artifact size %d exceeds limit %d", artifactSize, limits.ArtifactBytes),
			})
			slog.Warn("artifact policy decision",
				"code", analyze.PolicyArtifactTooLarge,
				"registry", registryID,
				"kind", r.Kind,
				"resource", r.Name,
				"artifact_bytes", artifactSize,
				"limit_bytes", limits.ArtifactBytes,
				"outcome", analyze.OutcomeReject,
			)
		}
	}
	if mode == store.AcquisitionIndexOnly {
		stripArchive(&r)
	}
	return r
}

func assessmentDecision(result artifact.BuildResult) store.AssessmentDecision {
	if result.Rejected {
		return store.AssessmentDecisionReject
	}
	for _, finding := range result.Findings {
		if finding.PolicyOutcome == analyze.OutcomeWarn || finding.Severity == analyze.OutcomeWarn {
			return store.AssessmentDecisionWarn
		}
	}
	return store.AssessmentDecisionAllow
}

func resourcePolicyDigest(policy analyze.Policy, limits config.Limits, license licensePolicy) (store.PolicyDigest, error) {
	allowlist := append([]string(nil), license.allowlist...)
	sort.Strings(allowlist)
	payload, err := json.Marshal(struct {
		Policy            analyze.Policy `json:"policy"`
		Limits            config.Limits  `json:"limits"`
		LicenseAllowlist  []string       `json:"license_allowlist"`
		LicenseUndeclared string         `json:"license_undeclared"`
	}{policy, limits, allowlist, license.undeclared})
	if err != nil {
		return "", fmt.Errorf("encode assessment policy: %w", err)
	}
	sum := sha256.Sum256(payload)
	return store.PolicyDigest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func toStoreFindings(findings []analyze.Finding, primaryPath string, body []byte, support analyze.FileSet) []store.Finding {
	out := make([]store.Finding, 0, len(findings))
	for _, f := range findings {
		finding := store.Finding{Code: f.Code, Severity: f.Severity, Policy: f.PolicyOutcome, Message: f.Message}
		if f.SourceRange != nil {
			location := &store.FindingLocation{Path: primaryPath, Line: f.SourceRange.Line, Column: f.SourceRange.Column}
			file := strings.ReplaceAll(f.SourceRange.File, "\\", "/")
			content := body
			if !strings.HasSuffix(file, "/body") && !strings.HasSuffix(file, "/name") && !strings.Contains(file, "/Description") && !strings.Contains(file, "/command") && !strings.Contains(file, "/prompt") && !strings.Contains(file, "/url") {
				name := strings.TrimPrefix(file, "support/")
				if data, ok := support[name]; ok {
					location.Path, content = filepath.ToSlash(filepath.Join("support", name)), data
				}
			}
			if f.SourceRange.Line > 0 {
				start, end := lineColumnBytes(content, f.SourceRange.Line, f.SourceRange.Column)
				location.StartByte, location.EndByte = int64(start), int64(end)
			}
			finding.Location = location
		}
		out = append(out, finding)
	}
	return out
}

func lineColumnBytes(content []byte, line, column int) (int, int) {
	if line < 1 {
		return 0, 0
	}
	start := 0
	for current := 1; current < line; current++ {
		n := bytes.IndexByte(content[start:], '\n')
		if n < 0 {
			return len(content), len(content)
		}
		start += n + 1
	}
	if column > 1 {
		for i, current := start, 1; i < len(content) && current < column; current++ {
			_, size := utf8.DecodeRune(content[i:])
			i += size
			start = i
		}
	}
	if start >= len(content) {
		return start, start
	}
	_, size := utf8.DecodeRune(content[start:])
	return start, start + size
}

func appendUpload(existing []store.BlobUpload, digest store.ArchiveDigest, data []byte) []store.BlobUpload {
	for _, b := range existing {
		if b.Digest == digest {
			return existing
		}
	}
	return append(existing, store.BlobUpload{Digest: digest, Data: data})
}
