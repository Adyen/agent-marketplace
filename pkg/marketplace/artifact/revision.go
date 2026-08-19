package artifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	// SourceRevisionSchemaV1 is the current source revision manifest schema.
	SourceRevisionSchemaV1 = "v1"
)

// SourceRevisionManifest lists all resources produced from a single source
// revision. Its revision_digest is computed over the canonical JSON with the
// revision_digest field excluded, mirroring the collection manifest digest
// pattern.
type SourceRevisionManifest struct {
	SchemaVersion  string             `json:"schema_version"`
	SourceURL      string             `json:"source_url,omitempty"`
	Ref            string             `json:"ref,omitempty"`
	Path           string             `json:"path,omitempty"`
	Generator      string             `json:"generator"`
	Resources      []RevisionResource `json:"resources"`
	RevisionDigest string             `json:"revision_digest,omitempty"`
}

// RevisionResource identifies a single resource inside a source revision.
type RevisionResource struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	ContentDigest string `json:"content_digest"`
	ArchiveDigest string `json:"archive_digest,omitempty"`
	Rejected      bool   `json:"rejected,omitempty"`
}

// RevisionDigest returns the sha256 digest of a canonical JSON payload.
func RevisionDigest(payload []byte) string {
	h := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(h[:])
}

// EncodeSourceRevisionManifest serializes m to canonical JSON and returns the
// bytes together with the revision digest.
func EncodeSourceRevisionManifest(m SourceRevisionManifest) ([]byte, string, error) {
	if err := m.Validate(); err != nil {
		return nil, "", err
	}

	digestOnly := m
	digestOnly.RevisionDigest = ""
	payload, err := canonicalRevisionPayload(digestOnly)
	if err != nil {
		return nil, "", err
	}
	digest := RevisionDigest(payload)

	m.RevisionDigest = digest
	out, err := canonicalRevisionPayload(m)
	if err != nil {
		return nil, "", err
	}
	return out, digest, nil
}

// canonicalRevisionPayload returns deterministic JSON for m.
func canonicalRevisionPayload(m SourceRevisionManifest) ([]byte, error) {
	cm := m
	if len(cm.Resources) > 0 {
		cm.Resources = append([]RevisionResource(nil), cm.Resources...)
		sort.SliceStable(cm.Resources, func(i, j int) bool {
			if cm.Resources[i].Kind != cm.Resources[j].Kind {
				return cm.Resources[i].Kind < cm.Resources[j].Kind
			}
			return cm.Resources[i].Name < cm.Resources[j].Name
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "")
	if err := enc.Encode(cm); err != nil {
		return nil, fmt.Errorf("encode revision manifest: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// DecodeSourceRevisionManifest reads a JSON revision manifest from r.
func DecodeSourceRevisionManifest(r io.Reader) (SourceRevisionManifest, []byte, error) {
	payload, err := io.ReadAll(r)
	if err != nil {
		return SourceRevisionManifest{}, nil, fmt.Errorf("read revision manifest: %w", err)
	}
	var m SourceRevisionManifest
	if err := json.Unmarshal(payload, &m); err != nil {
		return SourceRevisionManifest{}, nil, fmt.Errorf("unmarshal revision manifest: %w", err)
	}
	return m, payload, nil
}

// VerifySourceRevisionManifest checks that m.RevisionDigest matches the
// canonical JSON payload with the digest field excluded. If payload is non-nil,
// it also checks that the payload round-trips to the same digest.
func VerifySourceRevisionManifest(m SourceRevisionManifest, payload []byte) error {
	if m.RevisionDigest == "" {
		return errors.New("revision manifest revision_digest is empty")
	}

	digestOnly := m
	digestOnly.RevisionDigest = ""
	wantPayload, err := canonicalRevisionPayload(digestOnly)
	if err != nil {
		return fmt.Errorf("canonical payload: %w", err)
	}
	got := RevisionDigest(wantPayload)
	if got != m.RevisionDigest {
		return fmt.Errorf("revision digest mismatch: got %s, want %s", got, m.RevisionDigest)
	}

	if len(payload) > 0 {
		var decoded SourceRevisionManifest
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return fmt.Errorf("unmarshal provided payload: %w", err)
		}
		canonical, err := canonicalRevisionPayload(decoded)
		if err != nil {
			return fmt.Errorf("canonical payload: %w", err)
		}
		if !bytes.Equal(canonical, payload) {
			return errors.New("provided payload is not canonical JSON")
		}
	}

	return nil
}

// Validate checks required fields and resource entries.
func (m SourceRevisionManifest) Validate() error {
	if m.SchemaVersion != SourceRevisionSchemaV1 {
		return fmt.Errorf("unsupported revision schema version %q", m.SchemaVersion)
	}
	if m.Generator == "" {
		return errors.New("revision manifest generator is required")
	}
	seen := make(map[string]bool)
	for i, r := range m.Resources {
		if r.Kind == "" {
			return fmt.Errorf("resources[%d].kind is required", i)
		}
		if r.Name == "" {
			return fmt.Errorf("resources[%d].name is required", i)
		}
		key := r.Kind + "/" + r.Name
		if seen[key] {
			return fmt.Errorf("duplicate revision resource %q", key)
		}
		seen[key] = true
	}
	return nil
}
