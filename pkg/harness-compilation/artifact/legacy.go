package artifact

import (
	"bytes"
	"fmt"
)

func VerifyV1(raw []byte, expected LegacyReference, limits *ArchiveLimits) (CanonicalArtifact, error) {
	if !isDigestFormat(expected.ArchiveDigest) || !isDigestFormat(expected.ContentDigest) || expected.Kind == "" || expected.Name == "" {
		return CanonicalArtifact{}, ErrInvalidReference
	}
	l, err := v2Limits(limits)
	if err != nil {
		return CanonicalArtifact{}, err
	}
	if _, err := unpackArchive(raw, l); err != nil {
		return CanonicalArtifact{}, err
	}
	legacy, _, err := Decode(bytes.NewReader(raw))
	if err != nil {
		return CanonicalArtifact{}, fmt.Errorf("decode legacy artifact: %w", err)
	}
	if legacy.Manifest.ArchiveDigest != expected.ArchiveDigest || legacy.Manifest.Kind != expected.Kind || legacy.Manifest.Name != expected.Name || legacy.Manifest.ContentDigest != expected.ContentDigest {
		return CanonicalArtifact{}, ErrArtifactIdentityMismatch
	}
	legacy.rawArchive = raw
	if err := legacy.ValidateArchive(&l); err != nil {
		return CanonicalArtifact{}, fmt.Errorf("verify legacy artifact: %w", err)
	}
	return legacy, nil
}
