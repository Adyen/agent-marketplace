package sourcerevision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	sourceRevisionSchemaV1    = "v1"
	sourceRevisionGeneratorV1 = "agent-marketplace source-revision/v1"
)

type sourceRevisionManifest struct {
	SchemaVersion  string             `json:"schema_version"`
	Generator      string             `json:"generator"`
	Resources      []revisionResource `json:"resources"`
	RevisionDigest string             `json:"revision_digest,omitempty"`
}

type revisionResource struct {
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	ContentDigest string            `json:"content_digest"`
	ArchiveDigest string            `json:"archive_digest,omitempty"`
	SupportFiles  map[string]string `json:"support_files,omitempty"`
}

func encodeManifest(manifest sourceRevisionManifest) ([]byte, sourceRevisionManifest, error) {
	if err := validateManifest(manifest); err != nil {
		return nil, sourceRevisionManifest{}, err
	}
	withoutDigest := manifest
	withoutDigest.RevisionDigest = ""
	payload, err := canonicalManifestJSON(withoutDigest)
	if err != nil {
		return nil, sourceRevisionManifest{}, err
	}
	sum := sha256.Sum256(payload)
	manifest.RevisionDigest = "sha256:" + hex.EncodeToString(sum[:])
	encoded, err := canonicalManifestJSON(manifest)
	return encoded, manifest, err
}

func canonicalManifestJSON(manifest sourceRevisionManifest) ([]byte, error) {
	manifest.Resources = append([]revisionResource(nil), manifest.Resources...)
	sort.SliceStable(manifest.Resources, func(i, j int) bool {
		if manifest.Resources[i].Kind != manifest.Resources[j].Kind {
			return manifest.Resources[i].Kind < manifest.Resources[j].Kind
		}
		return manifest.Resources[i].Name < manifest.Resources[j].Name
	})
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(manifest); err != nil {
		return nil, fmt.Errorf("encode revision manifest: %w", err)
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

func verifyManifest(manifest sourceRevisionManifest, payload []byte) error {
	if manifest.RevisionDigest == "" {
		return errors.New("revision manifest revision_digest is empty")
	}
	encoded, canonical, err := encodeManifest(manifest)
	if err != nil {
		return err
	}
	if canonical.RevisionDigest != manifest.RevisionDigest {
		return fmt.Errorf("revision digest mismatch: got %s, want %s", canonical.RevisionDigest, manifest.RevisionDigest)
	}
	if len(payload) > 0 && !bytes.Equal(encoded, payload) {
		return errors.New("provided payload is not canonical JSON")
	}
	return nil
}

func validateManifest(manifest sourceRevisionManifest) error {
	if manifest.SchemaVersion != sourceRevisionSchemaV1 {
		return fmt.Errorf("unsupported revision schema version %q", manifest.SchemaVersion)
	}
	if manifest.Generator == "" {
		return errors.New("revision manifest generator is required")
	}
	seen := make(map[string]bool, len(manifest.Resources))
	for i, resource := range manifest.Resources {
		if resource.Kind == "" || resource.Name == "" || resource.ContentDigest == "" {
			return fmt.Errorf("resources[%d] has incomplete identity or digest", i)
		}
		key := resource.Kind + "/" + resource.Name
		if seen[key] {
			return fmt.Errorf("duplicate revision resource %q", key)
		}
		seen[key] = true
	}
	return nil
}
