package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

const blobOwnerRevisionColumns = `rr.marketplace_id,rr.registry_id,rr.kind,rr.name,rr.content_digest,rr.archive_digest`

func inspectionBlobOwnership(ctx context.Context, q ManagedSQLQueries, meta MetadataStore, source blobOwnerSource, payload []byte) (blobOwnershipPlan, error) {
	var plan blobOwnershipPlan
	if len(payload) > 1<<20 || contentDigest(payload) != string(source.digest) {
		return plan, ErrBlobOwnershipInvalid
	}
	assessment, err := meta.GetResourceAssessment(ctx, AssessmentDigest(source.id))
	if err != nil {
		return plan, err
	}
	if assessment.Coordinate.Registry != source.registry || ArchiveDigest(assessment.InspectionManifestDigest) != source.digest {
		return plan, ErrBlobOwnershipInvalid
	}
	revisions, err := readBlobOwnerRevisions(ctx, q, `SELECT `+blobOwnerRevisionColumns+` FROM resource_revisions rr WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5`, assessment.Coordinate.Marketplace, assessment.Coordinate.Registry, assessment.Coordinate.Kind, assessment.Coordinate.Name, assessment.ContentDigest)
	if err != nil {
		return plan, err
	}
	if len(revisions) != 1 {
		return plan, ErrBlobOwnersIncomplete
	}
	if err := validateInspectionManifestOwnership(payload, revisions[0], nil, false); err != nil {
		return plan, fmt.Errorf("%w: inspection metadata", ErrBlobOwnershipInvalid)
	}
	var manifest inspectionPublicationManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return plan, ErrBlobOwnershipInvalid
	}
	if len(manifest.Files) > 4096 {
		return plan, ErrBlobOwnersIncomplete
	}
	owner := blobResourceOwner{coordinate: assessment.Coordinate, digest: assessment.ContentDigest}
	plan.resources = append(plan.resources, blobOwnerEdge{kind: "inspection", digest: source.digest, owner: owner})
	for _, file := range manifest.Files {
		if file.InspectionClass == "text" || file.InspectionClass == "image" {
			plan.resources = append(plan.resources, blobOwnerEdge{kind: "content", digest: ArchiveDigest(file.ContentDigest), owner: owner})
		}
	}
	return plan, nil
}

type blobRegistryManifest struct {
	SchemaVersion  string                 `json:"schema_version"`
	Generator      string                 `json:"generator"`
	Resources      []blobRegistryResource `json:"resources"`
	RevisionDigest string                 `json:"revision_digest,omitempty"`
}

type blobRegistryResource struct {
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	ContentDigest string            `json:"content_digest"`
	ArchiveDigest string            `json:"archive_digest,omitempty"`
	SupportFiles  map[string]string `json:"support_files,omitempty"`
}

func decodeBlobRegistryManifest(payload []byte, digest ArchiveDigest) (blobRegistryManifest, error) {
	var manifest blobRegistryManifest
	if contentDigest(payload) != string(digest) {
		return manifest, ErrBlobOwnershipInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, ErrBlobOwnershipInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return manifest, ErrBlobOwnershipInvalid
	}
	if manifest.SchemaVersion != "v1" || manifest.Generator != "agent-marketplace registry-revision/v1" || manifest.RevisionDigest != "" || len(manifest.Resources) > 4096 {
		return manifest, ErrBlobOwnershipInvalid
	}
	seen := make(map[string]bool, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		key := resource.Kind + "/" + resource.Name
		if !IsMarketplaceResourceKind(resource.Kind) || resource.Name == "" || !validInspectionDigest(resource.ContentDigest) || seen[key] || (resource.ArchiveDigest != "" && !validInspectionDigest(resource.ArchiveDigest)) {
			return manifest, ErrBlobOwnershipInvalid
		}
		seen[key] = true
		for _, digest := range resource.SupportFiles {
			if !validInspectionDigest(digest) {
				return manifest, ErrBlobOwnershipInvalid
			}
		}
	}
	sort.Slice(manifest.Resources, func(i, j int) bool {
		if manifest.Resources[i].Kind != manifest.Resources[j].Kind {
			return manifest.Resources[i].Kind < manifest.Resources[j].Kind
		}
		return manifest.Resources[i].Name < manifest.Resources[j].Name
	})
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(manifest) != nil || !bytes.Equal(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")), payload) {
		return manifest, ErrBlobOwnershipInvalid
	}
	return manifest, nil
}

func registryBlobOwnership(ctx context.Context, q ManagedSQLQueries, source blobOwnerSource, payload []byte) (blobOwnershipPlan, error) {
	var plan blobOwnershipPlan
	manifest, err := decodeBlobRegistryManifest(payload, source.digest)
	if err != nil {
		return plan, err
	}
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM all_registry_occurrences WHERE registry_id=$1 AND manifest_digest=$2 LIMIT 1) occurrences`, source.registry, source.digest).Scan(&exists); err != nil {
		return plan, err
	}
	if exists != 1 {
		return plan, ErrBlobOwnersIncomplete
	}
	revisions, err := readBlobOwnerRevisions(ctx, q, `SELECT DISTINCT `+blobOwnerRevisionColumns+` FROM resource_revisions rr JOIN all_resource_occurrences ro ON ro.marketplace_id=rr.marketplace_id AND ro.registry_id=rr.registry_id AND ro.kind=rr.kind AND ro.name=rr.name AND ro.content_digest=rr.content_digest WHERE ro.registry_id=$1 AND ro.manifest_digest=$2 LIMIT 4097`, source.registry, source.digest)
	if err != nil {
		return plan, err
	}
	expected := make(map[string]blobRegistryResource, len(manifest.Resources))
	found := make(map[string]bool, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		expected[resource.Kind+"/"+resource.Name] = resource
	}
	for _, revision := range revisions {
		key := revision.Coordinate.Kind + "/" + revision.Coordinate.Name
		resource, ok := expected[key]
		if !ok || string(revision.ContentDigest) != resource.ContentDigest || string(revision.ArchiveDigest) != resource.ArchiveDigest {
			return plan, ErrBlobOwnershipInvalid
		}
		found[key] = true
		plan.resources = append(plan.resources, blobOwnerEdge{kind: "manifest", digest: source.digest, owner: blobResourceOwner{coordinate: revision.Coordinate, digest: revision.ContentDigest}})
	}
	if len(found) != len(expected) {
		return plan, ErrBlobOwnersIncomplete
	}
	plan.registries = []RegistryID{source.registry}
	return plan, nil
}

func collectionBlobOwnership(ctx context.Context, q ManagedSQLQueries, source blobOwnerSource, payload []byte) (blobOwnershipPlan, error) {
	var plan blobOwnershipPlan
	manifest, decoded, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(payload))
	if err != nil {
		return plan, ErrBlobOwnershipInvalid
	}
	if err := hccartifact.VerifyCollectionManifest(manifest, decoded); err != nil || manifest.ContentDigest != string(source.digest) || manifest.ContentDigest != source.id {
		return plan, ErrBlobOwnershipInvalid
	}
	owners, err := readBlobOwnerRevisions(ctx, q, `SELECT `+blobOwnerRevisionColumns+` FROM resource_revisions rr WHERE registry_id=$1 AND kind='collection' AND content_digest=$2 LIMIT 4097`, source.registry, source.id)
	if err != nil {
		return plan, err
	}
	if len(owners) == 0 {
		return plan, ErrBlobOwnersIncomplete
	}
	references := make([]hccartifact.CollectionMemberEntry, 0, len(manifest.Leaves)+len(manifest.Members))
	seen := make(map[string]bool)
	children := make(map[string]bool)
	for _, member := range manifest.Members {
		if member.Kind == "collection" {
			if member.Optional && member.Revision == "" {
				children[member.Name] = true
				continue
			}
			if len(manifest.Leaves) == 0 || !validInspectionDigest(member.Revision) {
				return plan, ErrBlobOwnersIncomplete
			}
			children[member.Name] = true
		}
	}
	for _, include := range manifest.Includes {
		if !children[include] {
			return plan, ErrBlobOwnersIncomplete
		}
	}
	for _, group := range [][]hccartifact.CollectionMemberEntry{manifest.Members, manifest.Leaves} {
		for _, member := range group {
			if member.Optional && member.Revision == "" {
				continue
			}
			if !IsMarketplaceResourceKind(member.Kind) || !validInspectionDigest(member.Revision) {
				return plan, ErrBlobOwnersIncomplete
			}
			if member.RegistryID == "" {
				member.RegistryID = string(source.registry)
			}
			key := member.RegistryID + "/" + member.Kind + "/" + member.Name + "@" + member.Revision
			if !seen[key] {
				references = append(references, member)
				seen[key] = true
			}
		}
	}
	if len(references) > 4096 || len(owners)*len(references) > 4096 {
		return plan, ErrBlobOwnersIncomplete
	}
	for _, owner := range owners {
		if owner.Coordinate.Name != manifest.Name {
			return plan, ErrBlobOwnershipInvalid
		}
		plan.resources = append(plan.resources, blobOwnerEdge{kind: "manifest", digest: source.digest, owner: blobResourceOwner{coordinate: owner.Coordinate, digest: owner.ContentDigest}})
		for _, leaf := range references {
			registry := RegistryID(leaf.RegistryID)
			if registry == "" {
				registry = source.registry
			}
			revisions, err := readBlobOwnerRevisions(ctx, q, `SELECT `+blobOwnerRevisionColumns+` FROM resource_revisions rr WHERE marketplace_id=$1 AND registry_id=$2 AND kind=$3 AND name=$4 AND content_digest=$5`, owner.Coordinate.Marketplace, registry, leaf.Kind, leaf.Name, leaf.Revision)
			if err != nil {
				return plan, err
			}
			if len(revisions) != 1 {
				return plan, ErrBlobOwnersIncomplete
			}
			plan.resources = append(plan.resources, blobOwnerEdge{kind: "manifest", digest: source.digest, owner: blobResourceOwner{coordinate: revisions[0].Coordinate, digest: revisions[0].ContentDigest}})
		}
	}
	plan.registries = []RegistryID{source.registry}
	return plan, nil
}
