package lockfile

func (a MarketplaceArtifactLock) Validate() error {
	switch a.Schema {
	case "v1":
		if a.ArtifactDigest != "" || a.BlobDigest != "" {
			return coded(CodeInvalidDocument, "v1 artifact forbids artifact_digest and blob_digest")
		}
		return validateDigest(a.ArchiveDigest, "artifact.archive_digest")
	case "v2":
		if a.ArchiveDigest != "" {
			return coded(CodeInvalidDocument, "v2 artifact forbids archive_digest")
		}
		if err := validateDigest(a.ArtifactDigest, "artifact.artifact_digest"); err != nil {
			return err
		}
		return validateDigest(a.BlobDigest, "artifact.blob_digest")
	default:
		return coded(CodeUnsupportedVersion, "unsupported artifact schema %q", a.Schema)
	}
}

func NormalizeArtifactSelections(doc Document) (Document, error) {
	doc = cloneDocument(doc)
	for _, mp := range doc.Marketplaces {
		for _, resource := range mp.Resources {
			if resource.Artifact != nil {
				doc.APIVersion = APIVersionV3
			}
		}
	}
	if doc.APIVersion == APIVersionV3 {
		for i := range doc.Marketplaces {
			for j := range doc.Marketplaces[i].Resources {
				r := &doc.Marketplaces[i].Resources[j]
				if r.Kind != "collection" && r.Acquisition == MarketplaceAcquisitionMirror && r.Artifact == nil {
					r.Artifact = &MarketplaceArtifactLock{Schema: "v1", ArchiveDigest: r.ArchiveDigest}
					r.ArchiveDigest = ""
				}
			}
		}
	}
	return Normalize(doc)
}

func UpgradeArtifactLockFormat(doc Document) (Document, error) {
	normalized, err := Normalize(doc)
	if err != nil {
		return Document{}, err
	}
	if len(normalized.Marketplaces) == 0 {
		return normalized, nil
	}
	normalized.APIVersion = APIVersionV3
	return NormalizeArtifactSelections(normalized)
}
