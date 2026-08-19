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

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	CollectionManifestPath = "manifest.json"
	CollectionSchemaV1     = "v1"
)

// CollectionManifest is the JSON manifest for a virtual collection. Unlike
// canonical artifacts, collection manifests are not tar archives because
// collections are virtual.
type CollectionManifest struct {
	SchemaVersion    string                  `json:"schema_version"`
	Name             string                  `json:"name"`
	Version          string                  `json:"version,omitempty"`
	Description      string                  `json:"description,omitempty"`
	Members          []CollectionMemberEntry `json:"members"`
	Includes         []string                `json:"includes,omitempty"`
	ContentDigest    string                  `json:"content_digest"`
	Provenance       CanonicalProvenance     `json:"provenance"`
	SupportedSchemas []string                `json:"supported_schema_versions"`
}

// CollectionMemberEntry describes a resource that is part of a collection.
type CollectionMemberEntry struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Optional bool   `json:"optional,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// CollectionContentDigest returns the digest of a collection manifest payload.
func CollectionContentDigest(payload []byte) string {
	h := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(h[:])
}

// EncodeCollectionManifest serializes a CollectionManifest to canonical,
// compact JSON and returns the manifest bytes together with its content digest.
//
// The content digest covers the manifest with members and includes sorted
// lexicographically and with the content_digest field set to the empty string.
func EncodeCollectionManifest(m CollectionManifest) ([]byte, string, error) {
	if err := m.Validate(); err != nil {
		return nil, "", err
	}

	// The digest is over the payload with content_digest excluded.
	digestOnly := m
	digestOnly.ContentDigest = ""
	payload, err := canonicalCollectionPayload(digestOnly)
	if err != nil {
		return nil, "", err
	}
	digest := CollectionContentDigest(payload)

	m.ContentDigest = digest
	out, err := canonicalCollectionPayload(m)
	if err != nil {
		return nil, "", err
	}
	return out, digest, nil
}

// canonicalCollectionPayload returns the canonical JSON representation of m.
// The content_digest field is taken as-is, so callers should clear it before
// computing a digest. Members and includes are sorted into a stable order.
func canonicalCollectionPayload(m CollectionManifest) ([]byte, error) {
	cm := m

	// Sort a copy of the members and includes slices so we never mutate the
	// caller's data while still producing a deterministic payload.
	if len(cm.Members) > 0 {
		cm.Members = append([]CollectionMemberEntry(nil), cm.Members...)
		sort.SliceStable(cm.Members, func(i, j int) bool {
			if cm.Members[i].Kind != cm.Members[j].Kind {
				return cm.Members[i].Kind < cm.Members[j].Kind
			}
			return cm.Members[i].Name < cm.Members[j].Name
		})
	}
	if len(cm.Includes) > 0 {
		cm.Includes = append([]string(nil), cm.Includes...)
		sort.Strings(cm.Includes)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "")
	if err := enc.Encode(cm); err != nil {
		return nil, fmt.Errorf("encode collection manifest: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// DecodeCollectionManifest reads a JSON collection manifest from r.
func DecodeCollectionManifest(r io.Reader) (CollectionManifest, []byte, error) {
	payload, err := io.ReadAll(r)
	if err != nil {
		return CollectionManifest{}, nil, fmt.Errorf("read collection manifest: %w", err)
	}
	var m CollectionManifest
	if err := json.Unmarshal(payload, &m); err != nil {
		return CollectionManifest{}, nil, fmt.Errorf("unmarshal collection manifest: %w", err)
	}
	return m, payload, nil
}

// VerifyCollectionManifest checks that the manifest's ContentDigest matches its
// JSON payload bytes.
func VerifyCollectionManifest(m CollectionManifest, payload []byte) error {
	if m.ContentDigest == "" {
		return errors.New("collection manifest content_digest is empty")
	}

	// The digest is over the payload with content_digest excluded.
	digestOnly := m
	digestOnly.ContentDigest = ""
	wantPayload, err := canonicalCollectionPayload(digestOnly)
	if err != nil {
		return fmt.Errorf("canonical payload: %w", err)
	}
	got := CollectionContentDigest(wantPayload)
	if got != m.ContentDigest {
		return fmt.Errorf("collection manifest digest mismatch: got %s, want %s", got, m.ContentDigest)
	}

	if payload != nil {
		var decoded CollectionManifest
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return fmt.Errorf("unmarshal provided payload: %w", err)
		}
		if decoded.ContentDigest != m.ContentDigest {
			return errors.New("provided payload content_digest does not match manifest")
		}
	}

	return nil
}

// Validate checks the collection manifest for required fields and valid members.
func (m CollectionManifest) Validate() error {
	if m.SchemaVersion != CollectionSchemaV1 {
		return fmt.Errorf("unsupported collection schema version %q", m.SchemaVersion)
	}
	if m.Name == "" {
		return errors.New("collection name is required")
	}
	if len(m.Members) == 0 && len(m.Includes) == 0 {
		return errors.New("collection must have members or includes")
	}
	seen := make(map[string]bool)
	for i, mem := range m.Members {
		if !isCollectionKind(mem.Kind) {
			return fmt.Errorf("members[%d].kind %q is not a valid collection member kind", i, mem.Kind)
		}
		if mem.Name == "" {
			return fmt.Errorf("members[%d].name is required", i)
		}
		key := mem.Kind + "/" + mem.Name
		if seen[key] {
			return fmt.Errorf("duplicate collection member %q", key)
		}
		seen[key] = true
	}
	return nil
}

func isCollectionKind(kind string) bool {
	switch kind {
	case "skill", "command", "subagent", "hook", "mcpserver", "collection":
		return true
	}
	return false
}

// CollectionFromIR builds a CollectionManifest skeleton from an IR Collection.
// The caller is responsible for filling provenance and source identifiers.
func CollectionFromIR(c *ir.Collection) (CollectionManifest, error) {
	if c == nil {
		return CollectionManifest{}, errors.New("nil collection")
	}
	members := make([]CollectionMemberEntry, 0, len(c.Members))
	for _, m := range c.Members {
		var kind string
		switch m.Kind {
		case ir.KindSkill:
			kind = "skill"
		case ir.KindCommand:
			kind = "command"
		case ir.KindSubagent:
			kind = "subagent"
		case ir.KindHook:
			kind = "hook"
		case ir.KindMcpServer:
			kind = "mcpserver"
		case ir.KindCollection:
			kind = "collection"
		default:
			continue
		}
		members = append(members, CollectionMemberEntry{
			Kind: kind,
			Name: m.Name,
		})
	}
	m := CollectionManifest{
		SchemaVersion:    CollectionSchemaV1,
		Name:             c.Name(),
		Members:          members,
		Includes:         append([]string(nil), c.Includes...),
		SupportedSchemas: []string{CollectionSchemaV1},
	}
	// Provenance is left to the caller.
	return m, nil
}
