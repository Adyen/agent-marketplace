package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const CanonicalArtifactSchemaV2 = "v2"
const GeneratorV2 = "agent-marketplace canonical artifact v2"
const MediaTypeV2 = "application/vnd.agent-marketplace.artifact.v2.tar+gzip"

type ManifestV2 struct {
	SchemaVersion    string       `json:"schema_version"`
	ArtifactVersion  string       `json:"artifact_version"`
	Kind             string       `json:"kind"`
	Name             string       `json:"name"`
	ContentDigest    string       `json:"content_digest"`
	License          string       `json:"license,omitempty"`
	Capabilities     []string     `json:"capabilities"`
	Provenance       ProvenanceV2 `json:"provenance"`
	SupportedSchemas []string     `json:"supported_schema_versions"`
}

type ProvenanceV2 struct {
	Generator string `json:"generator"`
}

func (m ManifestV2) CanonicalBytes() ([]byte, error) {
	if m.SchemaVersion != CanonicalArtifactSchemaV2 || m.ArtifactVersion != CanonicalArtifactSchemaV2 || m.Provenance.Generator != GeneratorV2 || len(m.SupportedSchemas) != 1 || m.SupportedSchemas[0] != CanonicalArtifactSchemaV2 {
		return nil, fmt.Errorf("%w: version or generator", ErrManifestInvalid)
	}
	if _, ok := canonicalToIRKind(m.Kind); !ok {
		return nil, fmt.Errorf("%w: kind", ErrManifestInvalid)
	}
	if err := ir.ValidateName(m.Name); err != nil {
		return nil, fmt.Errorf("%w: name: %v", ErrManifestInvalid, err)
	}
	if !isDigestFormat(m.ContentDigest) {
		return nil, fmt.Errorf("%w: content digest", ErrManifestInvalid)
	}
	var b bytes.Buffer
	b.WriteByte('{')
	fields := []struct{ key, value string }{{"schema_version", m.SchemaVersion}, {"artifact_version", m.ArtifactVersion}, {"kind", m.Kind}, {"name", m.Name}, {"content_digest", m.ContentDigest}}
	if m.License != "" {
		fields = append(fields, struct{ key, value string }{"license", m.License})
	}
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + field.key + `":`)
		if err := appendCanonicalString(&b, field.value); err != nil {
			return nil, err
		}
	}
	b.WriteString(`,"capabilities":[`)
	for i, capability := range m.Capabilities {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := appendCanonicalString(&b, capability); err != nil {
			return nil, err
		}
	}
	b.WriteString(`],"provenance":{"generator":"agent-marketplace canonical artifact v2"},"supported_schema_versions":["v2"]}`)
	return b.Bytes(), nil
}

func appendCanonicalString(b *bytes.Buffer, value string) error {
	return appendJSONString(b, value, false)
}

func appendJSONString(b *bytes.Buffer, value string, legacy bool) error {
	if !legacy && !utf8.ValidString(value) {
		return fmt.Errorf("%w: invalid UTF-8", ErrManifestInvalid)
	}
	b.WriteByte('"')
	const hex = "0123456789abcdef"
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		if legacy && r == utf8.RuneError && size == 1 {
			b.WriteString(`\ufffd`)
			continue
		}
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		case '\u2028':
			b.WriteString(`\u2028`)
		case '\u2029':
			b.WriteString(`\u2029`)
		default:
			if r < 0x20 || legacy && (r == '<' || r == '>' || r == '&') {
				b.WriteString(`\u00`)
				b.WriteByte(hex[r>>4])
				b.WriteByte(hex[r&15])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return nil
}

func decodeManifestV2(raw []byte) (ManifestV2, error) {
	var m ManifestV2
	if !utf8.Valid(raw) {
		return m, fmt.Errorf("%w: invalid UTF-8", ErrManifestInvalid)
	}
	if err := jsonutil.Validate("", raw); err != nil {
		return ManifestV2{}, fmt.Errorf("%w: %w", ErrManifestInvalid, err)
	}
	if err := jsonutil.ValidateFieldAliases("", raw, &m); err != nil {
		return ManifestV2{}, fmt.Errorf("%w: %w", ErrManifestInvalid, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return ManifestV2{}, fmt.Errorf("%w: decode: %v", ErrManifestInvalid, err)
	}
	canonical, err := m.CanonicalBytes()
	if err != nil {
		return ManifestV2{}, err
	}
	if !bytes.Equal(canonical, raw) {
		return ManifestV2{}, fmt.Errorf("%w: non-canonical encoding", ErrManifestInvalid)
	}
	return m, nil
}
