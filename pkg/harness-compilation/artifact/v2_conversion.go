package artifact

import (
	"fmt"
	"slices"
)

type LegacyReference struct {
	ArchiveDigest string
	Kind          string
	Name          string
	ContentDigest string
}

func ConvertV1ToV2(raw []byte, expected LegacyReference, limits *ArchiveLimits) (ArtifactV2, ArtifactDigest, error) {
	legacy, err := VerifyV1(raw, expected, limits)
	if err != nil {
		return ArtifactV2{}, "", fmt.Errorf("verify conversion source: %w", err)
	}
	l, err := v2Limits(limits)
	if err != nil {
		return ArtifactV2{}, "", err
	}
	m := legacy.Manifest
	if m.SchemaVersion != CanonicalArtifactSchemaV1 || m.ArtifactVersion != CanonicalArtifactSchemaV1 {
		return ArtifactV2{}, "", ErrUnsupportedConversion
	}
	converted := ArtifactV2{
		Manifest: ManifestV2{SchemaVersion: CanonicalArtifactSchemaV2, ArtifactVersion: CanonicalArtifactSchemaV2, Kind: m.Kind, Name: m.Name, ContentDigest: m.ContentDigest, License: m.License, Capabilities: slices.Clone(m.Capabilities), Provenance: ProvenanceV2{Generator: GeneratorV2}, SupportedSchemas: []string{CanonicalArtifactSchemaV2}},
		Payload:  legacy.Payload, Assets: legacy.Assets, Licenses: legacy.Licenses,
	}
	entries, err := converted.entries(l)
	if err != nil {
		return ArtifactV2{}, "", fmt.Errorf("%w: %w", ErrUnsupportedConversion, err)
	}
	return converted, frameDigest(entries), nil
}

func VerifyV1V2Equivalence(legacyRaw []byte, legacyRef LegacyReference, candidateRaw []byte, candidateRef ArtifactReference, limits *ArchiveLimits) error {
	_, expected, err := ConvertV1ToV2(legacyRaw, legacyRef, limits)
	if err != nil {
		return err
	}
	candidate, err := VerifyV2(candidateRaw, candidateRef, limits)
	if err != nil {
		return err
	}
	if err := candidate.VerifyIdentity(legacyRef.Kind, legacyRef.Name, legacyRef.ContentDigest); err != nil {
		return err
	}
	if candidateRef.ArtifactDigest != expected {
		return ErrProtectedEntryMismatch
	}
	return nil
}
