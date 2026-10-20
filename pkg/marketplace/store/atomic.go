package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// BlobUpload is a digest-addressed blob that must be present in the blob store
// before a metadata transaction makes a revision visible.
type BlobUpload struct {
	Digest ArchiveDigest
	Data   []byte
}

// RegistryRevisionPublication groups the metadata and blobs for a registry revision.
// All blobs are uploaded before any metadata is written, so a failed publish
// can only leave orphaned blobs that are safe to garbage-collect.
type RegistryRevisionPublication struct {
	PublicationID PublicationID
	ManagedFence  *ManagedFence

	// Occurrence is the registry commit occurrence to publish.
	Occurrence RegistryOccurrence

	ExpectedHeadCommit string

	// Resources are the resource revisions to publish.
	Resources []ResourceRevision

	ResourceOccurrences []ResourceOccurrence

	Assessments []ResourceAssessment

	// Collection is retained for compatibility with callers publishing one
	// collection revision.
	Collection *CollectionRevision

	// Collections contains all collection revisions discovered in the registry
	// commit. They are committed with the registry occurrence as one publication.
	Collections []CollectionRevision

	// ManifestPayload is the canonical registry revision manifest JSON. When set,
	// it is uploaded to the blob store using Occurrence.RegistryRevisionManifestDigest
	// as the key before metadata is committed.
	ManifestPayload []byte

	// ExtraBlobs contains any additional blobs that must be uploaded first, such
	// as resource and collection archives.
	ExtraBlobs []BlobUpload

	InspectionBlobs []BlobUpload
}

// PublishRegistryRevision uploads all required blobs, then writes the metadata in
// a single transaction. It returns an error if any blob upload fails, without
// writing metadata.
func PublishRegistryRevision(ctx context.Context, meta MetadataStore, blobs BlobStore, req RegistryRevisionPublication) error {
	if err := validateRegistryRevisionPublication(req); err != nil {
		return err
	}
	uploads, err := prepareUploads(req)
	if err != nil {
		return err
	}
	if err := validateInspectionPublication(req, uploads); err != nil {
		return err
	}
	if err := validateCollectionPublication(req, uploads); err != nil {
		return err
	}

	for _, u := range uploads {
		if err := putImmutableBlob(ctx, blobs, u); err != nil {
			return fmt.Errorf("upload immutable blob %s: %w", u.Digest, err)
		}
	}

	return meta.WithTx(ctx, func(txCtx context.Context, tx MetadataStore) error {
		if req.PublicationID != "" || req.ManagedFence != nil {
			publisher, ok := tx.(PublicationStore)
			if !ok {
				return fmt.Errorf("%w: publication identities unsupported", ErrConflict)
			}
			if err := publisher.PublishRegistryPublication(txCtx, req); err != nil {
				return err
			}
			return recordPublicationBlobOwners(txCtx, tx, req, uploads)
		}
		alreadyCurrent, err := tx.FenceRegistryPublication(txCtx, req)
		if err != nil {
			return err
		}
		if alreadyCurrent {
			return nil
		}
		for _, rev := range req.Resources {
			if err := tx.PutResourceRevision(txCtx, rev); err != nil {
				return err
			}
		}
		if err := tx.PutRegistryOccurrence(txCtx, req.Occurrence); err != nil {
			return err
		}
		for _, occurrence := range req.ResourceOccurrences {
			if err := tx.PutResourceOccurrence(txCtx, occurrence); err != nil {
				return err
			}
		}
		for _, assessment := range req.Assessments {
			if err := tx.PutResourceAssessment(txCtx, assessment); err != nil {
				return err
			}
			if err := tx.SetCurrentResourceAssessment(txCtx, assessment); err != nil {
				return err
			}
		}
		if req.Collection != nil {
			if err := tx.PutCollectionRevision(txCtx, *req.Collection); err != nil {
				return err
			}
		}
		for _, collection := range req.Collections {
			if err := tx.PutCollectionRevision(txCtx, collection); err != nil {
				return err
			}
		}
		if err := tx.SetRegistryHead(txCtx, req.Occurrence.RegistryID, req.Occurrence.Commit, req.ExpectedHeadCommit); err != nil {
			return err
		}
		return recordPublicationBlobOwners(txCtx, tx, req, uploads)
	})
}

// ResourceRevisionPublication groups a single resource revision with its archive blob.
type ResourceRevisionPublication struct {
	Revision     ResourceRevision
	ArchiveBytes []byte
}

// PublishResourceRevision uploads the resource archive and writes the resource
// revision in a single transaction.
func PublishResourceRevision(ctx context.Context, meta MetadataStore, blobs BlobStore, pub ResourceRevisionPublication) error {
	archive := string(pub.Revision.ArchiveDigest)
	if archive != "" && len(pub.ArchiveBytes) > 0 {
		if err := putImmutableBlob(ctx, blobs, BlobUpload{Digest: pub.Revision.ArchiveDigest, Data: pub.ArchiveBytes}); err != nil {
			return fmt.Errorf("upload resource archive %s: %w", pub.Revision.ArchiveDigest, err)
		}
	}

	return meta.WithTx(ctx, func(txCtx context.Context, tx MetadataStore) error {
		return tx.PutResourceRevision(txCtx, pub.Revision)
	})
}

func validateRegistryRevisionPublication(req RegistryRevisionPublication) error {
	for _, rev := range req.Resources {
		if rev.Coordinate.Kind == "collection" && rev.ArchiveDigest != "" {
			return fmt.Errorf("collection resource revision %s must not have an archive digest", rev.Coordinate)
		}
	}
	return nil
}

// validateInspectionPublication prevents a metadata transaction from exposing
// an assessment whose immutable inspection projection was not uploaded as part
// of the same publication.
func validateInspectionPublication(req RegistryRevisionPublication, uploads []BlobUpload) error {
	if len(req.InspectionBlobs) == 0 && len(req.Assessments) == 0 {
		return nil
	}
	byDigest := make(map[ArchiveDigest][]byte, len(uploads))
	for _, upload := range uploads {
		byDigest[upload.Digest] = upload.Data
	}
	for _, assessment := range req.Assessments {
		if err := assessment.Validate(); err != nil {
			return err
		}
		rev, ok := resourceRevisionFor(req.Resources, assessment.Coordinate, assessment.ContentDigest)
		if !ok {
			return fmt.Errorf("assessment %s has no resource revision", assessment.AssessmentDigest)
		}
		data, ok := byDigest[ArchiveDigest(assessment.InspectionManifestDigest)]
		if !ok || len(data) == 0 {
			return fmt.Errorf("resource revision %s inspection manifest is missing", rev.Coordinate)
		}
		if len(data) > 1*1024*1024 {
			return fmt.Errorf("resource revision %s inspection manifest exceeds serialized limit", rev.Coordinate)
		}
		if err := validateInspectionManifestBytes(data, rev, byDigest); err != nil {
			return fmt.Errorf("resource revision %s inspection manifest: %w", rev.Coordinate, err)
		}
	}
	return nil
}

type inspectionPublicationFile struct {
	Path            string `json:"path"`
	ContentDigest   string `json:"content_digest"`
	MediaType       string `json:"media_type"`
	Size            int64  `json:"size"`
	InspectionClass string `json:"inspection_class"`
}

type inspectionPublicationManifest struct {
	SchemaVersion  string                      `json:"schema_version"`
	Coordinate     ResourceCoordinate          `json:"coordinate"`
	RevisionDigest string                      `json:"revision_digest"`
	PrimaryPath    string                      `json:"primary_path"`
	Files          []inspectionPublicationFile `json:"files"`
}

// validateInspectionManifestBytes mirrors inspection.Verify's canonical
// identity, path, and media checks. The store package cannot
// call inspection.Verify directly because inspection's projection types use
// store coordinates; publication additionally verifies every exposed body
// before making the revision visible.
func validateInspectionManifestBytes(data []byte, rev ResourceRevision, blobs map[ArchiveDigest][]byte) error {
	return validateInspectionManifestOwnership(data, rev, blobs, true)
}

func validateInspectionManifestOwnership(data []byte, rev ResourceRevision, blobs map[ArchiveDigest][]byte, verifyBodies bool) error {
	var manifest inspectionPublicationManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("inspection manifest has trailing JSON")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("canonical JSON: %w", err)
	}
	if !bytes.Equal(canonical, data) {
		return errors.New("inspection manifest is not canonical JSON")
	}
	if manifest.SchemaVersion != "v1" || manifest.Coordinate != rev.Coordinate || manifest.RevisionDigest != string(rev.ContentDigest) || manifest.PrimaryPath != inspectionPrimaryPathForKind(rev.Coordinate.Kind) || len(manifest.Files) == 0 {
		return errors.New("identity or schema is incomplete")
	}
	if manifest.Coordinate.Marketplace == "" || manifest.Coordinate.Registry == "" || manifest.Coordinate.Name == "" || !validInspectionDigest(manifest.RevisionDigest) {
		return errors.New("identity or revision digest is invalid")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	primary := false
	for _, file := range manifest.Files {
		if file.Path == "" || file.ContentDigest == "" || file.Size < 0 || !validInspectionDigest(file.ContentDigest) || !allowedInspectionFile(file.MediaType, file.InspectionClass) {
			return fmt.Errorf("invalid file metadata for %q", file.Path)
		}
		if _, exists := seen[file.Path]; exists {
			return fmt.Errorf("duplicate file path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
		if file.Path == manifest.PrimaryPath {
			if primary {
				return errors.New("duplicate primary file")
			}
			expectedMedia, expectedClass := inspectionPrimaryMediaPolicy(rev.Coordinate.Kind)
			if file.MediaType != expectedMedia || file.InspectionClass != expectedClass {
				return errors.New("primary file media policy is not canonical")
			}
			primary = true
		} else if !canonicalInspectionSupportPath(file.Path) {
			return fmt.Errorf("non-canonical file path %q", file.Path)
		}
		bodyRequired := file.InspectionClass == "text" || file.InspectionClass == "image"
		if bodyRequired && verifyBodies {
			body, ok := blobs[ArchiveDigest(file.ContentDigest)]
			if !ok {
				return fmt.Errorf("body for %q is missing", file.Path)
			}
			if err := verifyInspectionBody(file, body); err != nil {
				return fmt.Errorf("body for %q: %w", file.Path, err)
			}
		}
	}
	if !primary {
		return errors.New("primary file is missing")
	}
	return nil
}

func CheckInspectionBlobs(ctx context.Context, blobs BlobStore, digest RevisionDigest) error {
	data, err := blobs.Get(ctx, ArchiveDigest(digest))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrBlobMissing, digest)
		}
		return err
	}
	if contentDigest(data) != string(digest) {
		return fmt.Errorf("%w: inspection manifest %s has different bytes", ErrConflict, digest)
	}
	var manifest inspectionPublicationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode inspection manifest %s: %w", digest, err)
	}
	for _, file := range manifest.Files {
		if file.InspectionClass != "text" && file.InspectionClass != "image" {
			continue
		}
		body, err := blobs.Get(ctx, ArchiveDigest(file.ContentDigest))
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrBlobMissing, file.ContentDigest)
		}
		if err != nil {
			return err
		}
		if int64(len(body)) != file.Size || contentDigest(body) != file.ContentDigest {
			return fmt.Errorf("%w: inspection body %s has different bytes", ErrConflict, file.ContentDigest)
		}
	}
	return nil
}

func inspectionPrimaryPathForKind(kind string) string {
	switch kind {
	case "skill":
		return "SKILL.md"
	case "command":
		return "COMMAND.md"
	case "subagent":
		return "AGENT.md"
	case "hook":
		return "HOOK.md"
	case "mcpserver":
		return ".mcp.json"
	case "collection":
		return "COLLECTION.json"
	default:
		return ""
	}
}

func inspectionPrimaryMediaPolicy(kind string) (string, string) {
	switch kind {
	case "skill", "command", "subagent", "hook":
		return "text/markdown", "text"
	case "mcpserver", "collection":
		return "application/json", "text"
	default:
		return "", ""
	}
}

func validInspectionDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, c := range value[len("sha256:"):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func allowedInspectionFile(mediaType, class string) bool {
	switch class {
	case "text":
		return mediaType == "text/markdown" || mediaType == "text/plain" || mediaType == "application/json" || mediaType == "application/yaml"
	case "image":
		return mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/gif" || mediaType == "image/webp"
	case "metadata":
		return mediaType == "application/octet-stream"
	default:
		return false
	}
}

func verifyInspectionBody(file inspectionPublicationFile, body []byte) error {
	if int64(len(body)) != file.Size || contentDigest(body) != file.ContentDigest {
		return errors.New("digest or size does not match manifest")
	}
	expectedMedia, expectedClass := inspectionMediaPolicy(file.Path, body)
	if file.MediaType != expectedMedia || file.InspectionClass != expectedClass {
		return errors.New("media type or inspection class does not match manifest")
	}
	return nil
}

func inspectionMediaPolicy(name string, data []byte) (string, string) {
	ext := strings.ToLower(path.Ext(name))
	if media := http.DetectContentType(data); media == "image/png" || media == "image/jpeg" || media == "image/gif" || media == "image/webp" {
		return media, "image"
	}
	if isUnsafeInspectionExtension(ext) || (!isInspectionPrimaryPath(name) && !isSafeInspectionText(data)) {
		return "application/octet-stream", "metadata"
	}
	if isInspectionPrimaryPath(name) && !isValidInspectionText(data) {
		return "application/octet-stream", "metadata"
	}
	if !utf8.Valid(data) {
		return "application/octet-stream", "metadata"
	}
	switch ext {
	case ".md", ".markdown":
		return "text/markdown", "text"
	case ".json":
		if json.Valid(data) {
			return "application/json", "text"
		}
		return "application/octet-stream", "metadata"
	case ".yaml", ".yml":
		return "application/yaml", "text"
	case ".txt":
		return "text/plain", "text"
	default:
		return "text/plain", "text"
	}
}

func isUnsafeInspectionExtension(ext string) bool {
	switch ext {
	case ".svg", ".html", ".htm", ".xhtml", ".xml", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".css":
		return true
	default:
		return false
	}
}

func isInspectionPrimaryPath(value string) bool {
	switch value {
	case "SKILL.md", "COMMAND.md", "AGENT.md", "HOOK.md":
		return true
	default:
		return false
	}
}

func isValidInspectionText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 || b < 0x09 || (b > 0x0d && b < 0x20) || b == 0x7f {
			return false
		}
	}
	return true
}

func isSafeInspectionText(data []byte) bool {
	if !isValidInspectionText(data) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(string(data)))
	for _, marker := range []string{"<script", "<svg", "<html", "<!doctype", "<?xml", "<style", "<iframe", "onerror=", "onload="} {
		if strings.Contains(text, marker) {
			return false
		}
	}
	if start := strings.IndexByte(text, '<'); start >= 0 && start+1 < len(text) {
		next := text[start+1]
		if (next >= 'a' && next <= 'z') || next == '/' || next == '!' || next == '?' {
			if strings.IndexByte(text[start+1:], '>') >= 0 {
				return false
			}
		}
	}
	if strings.HasPrefix(text, "#!") || strings.HasPrefix(text, "javascript:") ||
		strings.HasPrefix(text, "data:") || strings.HasPrefix(text, "vbscript:") ||
		strings.HasPrefix(text, "function ") || strings.HasPrefix(text, "async function ") ||
		strings.HasPrefix(text, "const ") || strings.HasPrefix(text, "let ") ||
		strings.HasPrefix(text, "var ") || strings.HasPrefix(text, "import ") ||
		strings.HasPrefix(text, "export ") || strings.HasPrefix(text, "console.") ||
		strings.HasPrefix(text, "window.") || strings.HasPrefix(text, "document.") ||
		strings.HasPrefix(text, "alert(") {
		return false
	}
	if strings.Contains(text, "{") && strings.Contains(text, "}") && strings.Contains(text, ":") && !json.Valid(data) {
		return false
	}
	return true
}

func canonicalInspectionSupportPath(value string) bool {
	if !strings.HasPrefix(value, "support/") || value == "support/" || strings.Contains(value, "\\") {
		return false
	}
	name := strings.TrimPrefix(value, "support/")
	switch name {
	case "SKILL.md", "COMMAND.md", "AGENT.md", "HOOK.md", ".mcp.json", "COLLECTION.json":
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func prepareUploads(req RegistryRevisionPublication) ([]BlobUpload, error) {
	seen := make(map[ArchiveDigest]int)
	var uploads []BlobUpload

	collectionDigests := collectionManifestDigests(req)
	archiveDigests := make(map[ArchiveDigest]struct{}, len(req.Resources))
	for _, rev := range req.Resources {
		if rev.ArchiveDigest != "" {
			archiveDigests[rev.ArchiveDigest] = struct{}{}
		}
	}
	placeholders, err := verifiedPlaceholderArchives(archiveDigests, req.ExtraBlobs, req.InspectionBlobs)
	if err != nil {
		return nil, err
	}
	add := func(b BlobUpload) error {
		if b.Digest == "" {
			return errors.New("blob upload with empty digest")
		}
		// Placeholder archives are addressed by PlaceholderArchiveKey and were
		// verified by hash in verifiedPlaceholderArchives.
		if _, isPlaceholder := PlaceholderArchiveDigest(b.Digest); !isPlaceholder {
			// Collection manifest addresses use the manifest's content_digest,
			// which intentionally hashes the canonical payload with that field
			// cleared rather than the complete self-referential blob bytes.
			if _, isCollectionManifest := collectionDigests[b.Digest]; !isCollectionManifest {
				if got := contentDigest(b.Data); got != string(b.Digest) {
					// Canonical artifact archives use a placeholder digest to break
					// their self-reference, so their address is verified by the
					// artifact verifier rather than by hashing the final bytes.
					if _, isArchive := archiveDigests[b.Digest]; !isArchive || !verifyArchiveBlob(b, placeholders) {
						return fmt.Errorf("blob %s data digest mismatch: got %s", b.Digest, got)
					}
				}
			}
		}
		if i, ok := seen[b.Digest]; ok {
			if !bytes.Equal(uploads[i].Data, b.Data) {
				return fmt.Errorf("%w: blob %s supplied with different data", ErrConflict, b.Digest)
			}
			return nil
		}
		seen[b.Digest] = len(uploads)
		uploads = append(uploads, BlobUpload{Digest: b.Digest, Data: append([]byte(nil), b.Data...)})
		return nil
	}

	if len(req.ManifestPayload) > 0 {
		if req.Occurrence.RegistryRevisionManifestDigest == "" {
			return nil, errors.New("manifest payload provided but RegistryRevisionManifestDigest is empty")
		}
		got := contentDigest(req.ManifestPayload)
		want := string(req.Occurrence.RegistryRevisionManifestDigest)
		if got != want {
			return nil, fmt.Errorf("manifest payload digest mismatch: got %s, want %s", got, want)
		}
		if err := add(BlobUpload{Digest: ArchiveDigest(want), Data: req.ManifestPayload}); err != nil {
			return nil, err
		}
	}

	for _, b := range append(req.ExtraBlobs, req.InspectionBlobs...) {
		if err := add(b); err != nil {
			return nil, err
		}
	}

	return uploads, nil
}

// validateCollectionPublication validates the complete flat collection upload
// graph before any metadata transaction begins. A collection's manifest and
// each exact leaf archive must be present in the same publication request.
func validateCollectionPublication(req RegistryRevisionPublication, uploads []BlobUpload) error {
	byDigest := make(map[ArchiveDigest][]byte, len(uploads))
	for _, upload := range uploads {
		byDigest[upload.Digest] = upload.Data
	}

	collections := make(map[string]CollectionRevision, len(req.Collections)+1)
	addCollection := func(cr CollectionRevision) error {
		if cr.ID == "" {
			return errors.New("collection revision has empty ID")
		}
		if previous, ok := collections[cr.ID]; ok {
			if !previous.Equal(cr) {
				return fmt.Errorf("%w: collection revision %q supplied with different metadata", ErrConflict, cr.ID)
			}
			return nil
		}
		collections[cr.ID] = cr
		return nil
	}
	if req.Collection != nil {
		if err := addCollection(*req.Collection); err != nil {
			return err
		}
	}
	for _, cr := range req.Collections {
		if err := addCollection(cr); err != nil {
			return err
		}
	}

	for _, rev := range req.Resources {
		if rev.Coordinate.Kind == "collection" {
			cr, ok := collections[string(rev.ContentDigest)]
			if !ok {
				return fmt.Errorf("collection resource revision %s has no collection metadata", rev.Coordinate)
			}
			if cr.ID != string(rev.ContentDigest) || cr.ManifestDigest != rev.ContentDigest || cr.RegistryID != rev.Coordinate.Registry {
				return fmt.Errorf("%w: collection %s metadata does not agree with resource revision", ErrConflict, rev.Coordinate)
			}
			if err := validateCollectionManifestGraph(rev, cr, byDigest, req.Resources, collections); err != nil {
				return err
			}
		}
	}

	for id, cr := range collections {
		found := false
		for _, rev := range req.Resources {
			if rev.Coordinate.Kind == "collection" && string(rev.ContentDigest) == id {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("collection revision %q has no collection resource revision", id)
		}
		if _, ok := byDigest[ArchiveDigest(cr.ManifestDigest)]; !ok {
			return fmt.Errorf("collection revision %q manifest is missing from upload graph", id)
		}
	}
	return nil
}

func validateCollectionManifestGraph(rev ResourceRevision, cr CollectionRevision, byDigest map[ArchiveDigest][]byte, resources []ResourceRevision, collections map[string]CollectionRevision) error {
	// Decode every metadata-backed manifest first. The artifact helper then
	// validates the complete nested graph and compares every flattened Leaves
	// entry with the graph-derived result before metadata can be committed.
	manifests := make(map[string]hccartifact.CollectionManifest, len(collections))
	for id, collectionMeta := range collections {
		data, ok := byDigest[ArchiveDigest(collectionMeta.ManifestDigest)]
		if !ok {
			return fmt.Errorf("collection revision %q manifest is missing from upload graph", id)
		}
		manifest, payload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("collection revision %q manifest: %w", id, err)
		}
		if err := hccartifact.VerifyCollectionManifest(manifest, payload); err != nil {
			return fmt.Errorf("collection revision %q manifest: %w", id, err)
		}
		if manifest.ContentDigest != id || manifest.ContentDigest != string(collectionMeta.ManifestDigest) {
			return fmt.Errorf("%w: collection revision %q manifest digest disagrees with metadata", ErrConflict, id)
		}
		manifests[id] = manifest
	}
	rootManifest, ok := manifests[cr.ID]
	if !ok {
		return fmt.Errorf("collection revision %q manifest is missing from publication", cr.ID)
	}
	if rootManifest.Name != rev.Coordinate.Name {
		return fmt.Errorf("%w: collection %s manifest name disagrees with metadata", ErrConflict, rev.Coordinate)
	}
	if _, err := hccartifact.ValidateAndExpandCollectionManifestGraph(rootManifest, manifests); err != nil {
		return fmt.Errorf("collection %s manifest graph: %w", rev.Coordinate, err)
	}

	visiting := make(map[string]bool)
	leafRevisions := make(map[ResourceCoordinate]RevisionDigest)
	var walk func(ResourceRevision, CollectionRevision) error
	walk = func(collectionRev ResourceRevision, collectionMeta CollectionRevision) error {
		if visiting[collectionMeta.ID] {
			return fmt.Errorf("collection %s contains a cycle", collectionRev.Coordinate)
		}
		visiting[collectionMeta.ID] = true
		defer delete(visiting, collectionMeta.ID)
		manifest, ok := manifests[collectionMeta.ID]
		if !ok {
			return fmt.Errorf("collection %s manifest is missing from upload graph", collectionRev.Coordinate)
		}
		if manifest.Name != collectionRev.Coordinate.Name {
			return fmt.Errorf("%w: collection %s manifest name disagrees with metadata", ErrConflict, collectionRev.Coordinate)
		}

		metadataMembers := make(map[ResourceCoordinate]struct{}, len(collectionMeta.Members))
		for _, member := range collectionMeta.Members {
			if _, exists := metadataMembers[member]; exists {
				return fmt.Errorf("%w: collection %s metadata contains duplicate member %s", ErrConflict, collectionRev.Coordinate, member)
			}
			metadataMembers[member] = struct{}{}
		}
		if len(metadataMembers) != len(manifest.Members) {
			return fmt.Errorf("%w: collection %s metadata member count disagrees with manifest", ErrConflict, collectionRev.Coordinate)
		}
		for _, member := range manifest.Members {
			coordinate := ResourceCoordinate{Marketplace: collectionRev.Coordinate.Marketplace, Registry: RegistryID(member.RegistryID), Kind: member.Kind, Name: member.Name}
			if _, ok := metadataMembers[coordinate]; !ok {
				return fmt.Errorf("%w: collection %s metadata is missing member %s", ErrConflict, collectionRev.Coordinate, coordinate)
			}
			memberRevision, found := resourceRevisionFor(resources, coordinate, RevisionDigest(member.Revision))
			if !found {
				return fmt.Errorf("collection %s member %s revision is missing from publication", collectionRev.Coordinate, coordinate)
			}
			if member.Kind == "collection" {
				nested, ok := collections[string(memberRevision.ContentDigest)]
				if !ok {
					return fmt.Errorf("collection %s nested member %s metadata is missing", collectionRev.Coordinate, coordinate)
				}
				if nested.RegistryID != coordinate.Registry || nested.ID != member.Revision || nested.ManifestDigest != RevisionDigest(member.Revision) {
					return fmt.Errorf("%w: collection %s nested member %s metadata identity disagrees with manifest", ErrConflict, collectionRev.Coordinate, coordinate)
				}
				if err := walk(memberRevision, nested); err != nil {
					return err
				}
			}
		}

		// A legacy flat manifest may omit Leaves, in which case its direct
		// materialized members are the leaves. Nested manifests are fail-closed
		// by ValidateAndExpandCollectionManifestGraph and cannot use this fallback.
		leaves := manifest.Leaves
		if len(leaves) == 0 {
			for _, member := range manifest.Members {
				if member.Kind != "collection" {
					leaves = append(leaves, member)
				}
			}
		}
		for _, member := range leaves {
			coordinate := ResourceCoordinate{Marketplace: collectionRev.Coordinate.Marketplace, Registry: RegistryID(member.RegistryID), Kind: member.Kind, Name: member.Name}
			memberRevision, found := resourceRevisionFor(resources, coordinate, RevisionDigest(member.Revision))
			if !found {
				return fmt.Errorf("collection %s leaf %s revision is missing from publication", collectionRev.Coordinate, coordinate)
			}
			if previous, exists := leafRevisions[coordinate]; exists && previous != memberRevision.ContentDigest {
				return fmt.Errorf("%w: collection graph contains conflicting revisions for %s", ErrConflict, coordinate)
			}
			leafRevisions[coordinate] = memberRevision.ContentDigest
			if memberRevision.ArchiveDigest != "" {
				if _, ok := byDigest[memberRevision.ArchiveDigest]; !ok {
					return fmt.Errorf("collection %s leaf %s archive is missing from upload graph", collectionRev.Coordinate, coordinate)
				}
			}
		}
		return nil
	}
	return walk(rev, cr)
}

func resourceRevisionFor(resources []ResourceRevision, coordinate ResourceCoordinate, digest RevisionDigest) (ResourceRevision, bool) {
	for _, resource := range resources {
		if resource.Coordinate == coordinate && resource.ContentDigest == digest {
			return resource, true
		}
	}
	return ResourceRevision{}, false
}

func collectionManifestDigests(req RegistryRevisionPublication) map[ArchiveDigest]struct{} {
	out := make(map[ArchiveDigest]struct{}, len(req.Collections)+1)
	if req.Collection != nil && req.Collection.ManifestDigest != "" {
		out[ArchiveDigest(req.Collection.ManifestDigest)] = struct{}{}
	}
	for _, cr := range req.Collections {
		if cr.ManifestDigest != "" {
			out[ArchiveDigest(cr.ManifestDigest)] = struct{}{}
		}
	}
	return out
}

// immutableBlobStore is implemented by adapters that can atomically preserve a
// first writer. The read-before-write fallback protects adapters without this
// optional capability from replacing existing digest-addressed bytes.
type immutableBlobStore interface {
	PutImmutable(context.Context, ArchiveDigest, []byte) error
}

// PutImmutableBlob stores data under digest with the first-writer guarantee used
// by publication. Callers must have verified data against digest.
func PutImmutableBlob(ctx context.Context, blobs BlobStore, digest ArchiveDigest, data []byte) error {
	return putImmutableBlob(ctx, blobs, BlobUpload{Digest: digest, Data: data})
}

func putImmutableBlob(ctx context.Context, blobs BlobStore, upload BlobUpload) error {
	if existing, err := existingImmutableBlob(ctx, blobs, upload); err == nil {
		if !bytes.Equal(existing, upload.Data) {
			return fmt.Errorf("%w: blob %s already contains different data", ErrConflict, upload.Digest)
		}
		return nil
	} else if errors.Is(err, ErrBlobTooLarge) {
		return fmt.Errorf("%w: blob %s already contains different data", ErrConflict, upload.Digest)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if immutable, ok := blobs.(immutableBlobStore); ok {
		return immutable.PutImmutable(ctx, upload.Digest, upload.Data)
	}
	return blobs.Put(ctx, upload.Digest, upload.Data)
}

// existingImmutableBlob reads the blob already stored at the upload's digest.
// A bounded store reads at most one byte more than the upload, which is enough
// to tell an equal blob from a different one.
func existingImmutableBlob(ctx context.Context, blobs BlobStore, upload BlobUpload) ([]byte, error) {
	if bounded, ok := blobs.(BoundedBlobStore); ok && len(upload.Data) > 0 {
		return bounded.GetBounded(ctx, upload.Digest, int64(len(upload.Data)))
	}
	return blobs.Get(ctx, upload.Digest)
}

func contentDigest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// verifiedPlaceholderArchives hashes every placeholder archive upload against
// the archive digest it is keyed by and returns them by that digest. A
// placeholder is never re-encoded, so verification does not depend on the
// running compressor.
func verifiedPlaceholderArchives(archiveDigests map[ArchiveDigest]struct{}, batches ...[]BlobUpload) (map[ArchiveDigest]hccartifact.CanonicalArtifact, error) {
	placeholders := make(map[ArchiveDigest]hccartifact.CanonicalArtifact)
	for _, batch := range batches {
		for _, b := range batch {
			digest, isPlaceholder := PlaceholderArchiveDigest(b.Digest)
			if !isPlaceholder {
				continue
			}
			if _, ok := archiveDigests[digest]; !ok {
				return nil, fmt.Errorf("placeholder archive %s has no resource archive digest", b.Digest)
			}
			artifact, err := hccartifact.VerifyPlaceholderArchive(b.Data, string(digest))
			if err != nil {
				return nil, fmt.Errorf("placeholder archive %s: %w", b.Digest, err)
			}
			placeholders[digest] = artifact
		}
	}
	return placeholders, nil
}

// verifyArchiveBlob verifies a final archive. When its verified placeholder form
// is part of the same publication, the final bytes are checked by content
// equality with it; otherwise the archive digest is re-derived by re-encoding.
func verifyArchiveBlob(upload BlobUpload, placeholders map[ArchiveDigest]hccartifact.CanonicalArtifact) bool {
	if placeholder, ok := placeholders[upload.Digest]; ok {
		_, err := hccartifact.VerifyFinalAgainstPlaceholder(upload.Data, placeholder)
		return err == nil
	}
	canonical, raw, err := hccartifact.Decode(bytes.NewReader(upload.Data))
	if err != nil {
		return false
	}
	if err := canonical.VerifyArchiveDigest(raw); err != nil {
		return false
	}
	return canonical.Manifest.ArchiveDigest == string(upload.Digest)
}
