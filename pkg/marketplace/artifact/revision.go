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
	// RegistryRevisionSchemaV1 is the current registry revision manifest schema.
	RegistryRevisionSchemaV1    = "v1"
	RegistryRevisionGeneratorV1 = "agent-marketplace registry-revision/v1"
)

// RegistryRevisionManifest lists all resources produced from a single registry
// revision. Its revision_digest is computed over the canonical JSON with the
// revision_digest field excluded, mirroring the collection manifest digest
// pattern.
type RegistryRevisionManifest struct {
	SchemaVersion  string             `json:"schema_version"`
	RegistryURL    string             `json:"-"`
	Ref            string             `json:"-"`
	Path           string             `json:"-"`
	Generator      string             `json:"generator"`
	Resources      []RevisionResource `json:"resources"`
	RevisionDigest string             `json:"revision_digest,omitempty"`
}

// RevisionResource identifies a single resource inside a registry revision.
type RevisionResource struct {
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	ContentDigest string            `json:"content_digest"`
	ArchiveDigest string            `json:"archive_digest,omitempty"`
	SupportFiles  map[string]string `json:"support_files,omitempty"`
}

// RevisionDigest returns the sha256 digest of a canonical JSON payload.
func RevisionDigest(payload []byte) string {
	h := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(h[:])
}

func isCanonicalDigest(value string) bool {
	if len(value) != len("sha256:")+64 || value[:len("sha256:")] != "sha256:" {
		return false
	}
	for i := len("sha256:"); i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// canonicalPayloadAndDigest returns the deterministic JSON for m with the
// self-referential revision_digest field excluded, together with its digest.
func canonicalPayloadAndDigest(m RegistryRevisionManifest) ([]byte, string, error) {
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
	return payload, digest, nil
}

// EncodeRegistryRevisionManifest serializes m to canonical JSON and returns the
// bytes together with the revision digest.
func EncodeRegistryRevisionManifest(m RegistryRevisionManifest) ([]byte, string, error) {
	_, digest, err := canonicalPayloadAndDigest(m)
	if err != nil {
		return nil, "", err
	}
	m.RevisionDigest = digest
	out, err := canonicalRevisionPayload(m)
	if err != nil {
		return nil, "", err
	}
	return out, digest, nil
}

// CanonicalRegistryRevisionPayload returns the deterministic JSON payload for m
// with the self-referential revision_digest field excluded, together with its
// SHA-256 content digest. This is the form stored as a content-addressed blob.
func CanonicalRegistryRevisionPayload(m RegistryRevisionManifest) ([]byte, string, error) {
	return canonicalPayloadAndDigest(m)
}

// canonicalRevisionPayload returns deterministic JSON for m.
func canonicalRevisionPayload(m RegistryRevisionManifest) ([]byte, error) {
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

// DecodeRegistryRevisionManifest reads a JSON revision manifest from r.
func DecodeRegistryRevisionManifest(r io.Reader) (RegistryRevisionManifest, []byte, error) {
	payload, err := io.ReadAll(r)
	if err != nil {
		return RegistryRevisionManifest{}, nil, fmt.Errorf("read revision manifest: %w", err)
	}
	var m RegistryRevisionManifest
	if err := json.Unmarshal(payload, &m); err != nil {
		return RegistryRevisionManifest{}, nil, fmt.Errorf("unmarshal revision manifest: %w", err)
	}
	return m, payload, nil
}

// VerifyRegistryRevisionManifest checks that m.RevisionDigest matches the
// canonical JSON payload with the digest field excluded. If payload is non-nil,
// it also checks that the payload round-trips to the same digest.
func VerifyRegistryRevisionManifest(m RegistryRevisionManifest, payload []byte) error {
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
		var decoded RegistryRevisionManifest
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
func (m RegistryRevisionManifest) Validate() error {
	if m.SchemaVersion != RegistryRevisionSchemaV1 {
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
		for path, digest := range r.SupportFiles {
			if path == "" {
				return fmt.Errorf("resources[%d].support_files contains an empty path", i)
			}
			if !isCanonicalDigest(digest) {
				return fmt.Errorf("resources[%d].support_files[%q] has invalid digest", i, path)
			}
		}
	}
	return nil
}
