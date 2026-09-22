package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

var (
	ErrUnsupportedSchema        = errors.New("unsupported artifact schema")
	ErrInvalidReference         = errors.New("invalid artifact reference")
	ErrBlobDigestMismatch       = errors.New("blob digest mismatch")
	ErrArchiveStructure         = errors.New("artifact archive structure violation")
	ErrManifestInvalid          = errors.New("artifact manifest schema or canonicality violation")
	ErrArtifactDigestMismatch   = errors.New("artifact digest mismatch")
	ErrContentDigestMismatch    = errors.New("content digest mismatch")
	ErrArtifactIdentityMismatch = errors.New("artifact identity mismatch")
	ErrUnsupportedConversion    = errors.New("unsupported artifact conversion")
	ErrProtectedEntryMismatch   = errors.New("protected-entry equivalence mismatch")
	ErrArchiveLimit             = errors.New("artifact archive limit exceeded")
)

type ArtifactDigest string
type BlobDigest string

type ArtifactReference struct {
	SchemaVersion  string         `json:"schema_version"`
	ArtifactDigest ArtifactDigest `json:"artifact_digest"`
	BlobDigest     BlobDigest     `json:"blob_digest"`
	MediaType      string         `json:"media_type"`
}

type EncodedArtifact struct {
	Bytes     []byte
	Reference ArtifactReference
}

type ArtifactV2 struct {
	Manifest ManifestV2
	Payload  []byte
	Assets   map[string][]byte
	Licenses map[string][]byte
}

type framedEntry struct {
	kind byte
	name string
	data []byte
}

func (r ArtifactReference) Validate() error {
	if r.SchemaVersion != CanonicalArtifactSchemaV2 {
		return fmt.Errorf("%w: %q", ErrUnsupportedSchema, r.SchemaVersion)
	}
	if !isDigestFormat(string(r.ArtifactDigest)) || !isDigestFormat(string(r.BlobDigest)) || r.MediaType != MediaTypeV2 {
		return ErrInvalidReference
	}
	return nil
}

func (a ArtifactV2) VerifyIdentity(kind, name, contentDigest string) error {
	if a.Manifest.Kind != kind || a.Manifest.Name != name || a.Manifest.ContentDigest != contentDigest {
		return ErrArtifactIdentityMismatch
	}
	return nil
}

func v2Limits(limits *ArchiveLimits) (ArchiveLimits, error) {
	if limits == nil {
		limits = DefaultArchiveLimits()
	}
	l := *limits
	if l.MaxPayloadBytes <= 0 || l.MaxTotalBytes <= 0 || l.MaxAssetFiles <= 0 || l.MaxTotalFiles <= 0 || l.MaxAssetBytes <= 0 || l.MaxTotalBytes > 1<<40 || l.MaxTotalFiles > 1<<24 {
		return l, fmt.Errorf("%w: invalid limits", ErrArchiveLimit)
	}
	return l, nil
}

func v2Path(name string) error {
	if !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00") || path.Clean(name) != name || name == "." {
		return ErrArchiveStructure
	}
	if err := validateArchiveMemberPath(name); err != nil {
		return fmt.Errorf("%w: %v", ErrArchiveStructure, err)
	}
	return nil
}

func (a ArtifactV2) entries(l ArchiveLimits) ([]framedEntry, error) {
	manifest, err := a.Manifest.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	return a.entriesWithManifest(l, manifest)
}

func (a ArtifactV2) entriesWithManifest(l ArchiveLimits, manifest []byte) ([]framedEntry, error) {
	if len(a.Payload) == 0 {
		return nil, fmt.Errorf("%w: empty payload", ErrArchiveStructure)
	}
	if ContentDigest(a.Payload) != a.Manifest.ContentDigest {
		return nil, ErrContentDigestMismatch
	}
	if int64(len(a.Payload)) > l.MaxPayloadBytes || int64(len(a.Assets)) > l.MaxAssetFiles || int64(2+len(a.Assets)+len(a.Licenses)) > l.MaxTotalFiles {
		return nil, ErrArchiveLimit
	}
	entries := []framedEntry{{1, CanonicalManifestPath, manifest}, {2, PayloadPath(a.Manifest.Kind), a.Payload}}
	total := int64(len(manifest)) + int64(len(a.Payload))
	for _, group := range []struct {
		kind   byte
		prefix string
		files  map[string][]byte
	}{{3, CanonicalAssetsDir, a.Assets}, {4, CanonicalLicensesDir, a.Licenses}} {
		for _, name := range sortedKeys(group.files) {
			if err := v2Path(name); err != nil {
				return nil, err
			}
			data := group.files[name]
			if int64(len(data)) > l.MaxAssetBytes {
				return nil, ErrArchiveLimit
			}
			total += int64(len(data))
			entries = append(entries, framedEntry{group.kind, group.prefix + "/" + name, data})
		}
	}
	if total > l.MaxTotalBytes {
		return nil, ErrArchiveLimit
	}
	for _, entry := range entries {
		if uint64(len(entry.name)) > uint64(^uint32(0)) || int64(len(entry.name)) > l.MaxTotalBytes {
			return nil, ErrArchiveLimit
		}
	}
	return entries, nil
}

func frameDigest(entries []framedEntry) ArtifactDigest {
	h := sha256.New()
	h.Write([]byte("agent-marketplace-artifact-v2\x00"))
	var number [8]byte
	binary.BigEndian.PutUint32(number[:4], uint32(len(entries)))
	h.Write(number[:4])
	for _, entry := range entries {
		h.Write([]byte{entry.kind})
		binary.BigEndian.PutUint32(number[:4], uint32(len(entry.name)))
		h.Write(number[:4])
		h.Write([]byte(entry.name))
		binary.BigEndian.PutUint64(number[:], uint64(len(entry.data)))
		h.Write(number[:])
		h.Write(entry.data)
	}
	return ArtifactDigest("sha256:" + hex.EncodeToString(h.Sum(nil)))
}

func EncodeV2(a ArtifactV2, limits *ArchiveLimits) (EncodedArtifact, error) {
	l, err := v2Limits(limits)
	if err != nil {
		return EncodedArtifact{}, err
	}
	entries, err := a.entries(l)
	if err != nil {
		return EncodedArtifact{}, err
	}
	var raw bytes.Buffer
	gz, err := gzip.NewWriterLevel(&raw, gzip.BestSpeed)
	if err != nil {
		return EncodedArtifact{}, err
	}
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0644, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}); err != nil {
			return EncodedArtifact{}, fmt.Errorf("encode v2 header: %w", err)
		}
		if _, err := tw.Write(entry.data); err != nil {
			return EncodedArtifact{}, fmt.Errorf("encode v2 member: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return EncodedArtifact{}, err
	}
	if err := gz.Close(); err != nil {
		return EncodedArtifact{}, err
	}
	return EncodedArtifact{Bytes: raw.Bytes(), Reference: ArtifactReference{SchemaVersion: CanonicalArtifactSchemaV2, ArtifactDigest: frameDigest(entries), BlobDigest: BlobDigest(ContentDigest(raw.Bytes())), MediaType: MediaTypeV2}}, nil
}

func VerifyV2(raw []byte, ref ArtifactReference, limits *ArchiveLimits) (ArtifactV2, error) {
	if err := ref.Validate(); err != nil {
		return ArtifactV2{}, err
	}
	l, err := v2Limits(limits)
	if err != nil {
		return ArtifactV2{}, err
	}
	if int64(len(raw)) > transportLimitV2(l) {
		return ArtifactV2{}, ErrArchiveLimit
	}
	if BlobDigest(ContentDigest(raw)) != ref.BlobDigest {
		return ArtifactV2{}, ErrBlobDigestMismatch
	}
	unpacked, err := unpackArchive(raw, l)
	if err != nil {
		return ArtifactV2{}, err
	}
	a, manifest, err := decodeEntriesV2(unpacked, l)
	if err != nil {
		return ArtifactV2{}, err
	}
	entries, err := a.entriesWithManifest(l, manifest)
	if err != nil {
		return ArtifactV2{}, err
	}
	if frameDigest(entries) != ref.ArtifactDigest {
		return ArtifactV2{}, ErrArtifactDigestMismatch
	}
	return a, nil
}

func transportLimitV2(l ArchiveLimits) int64 {
	return l.MaxTotalBytes + l.MaxTotalFiles*1024 + 1<<20
}

func unpackArchive(raw []byte, l ArchiveLimits) ([]byte, error) {
	limit := transportLimitV2(l)
	if int64(len(raw)) > limit {
		return nil, ErrArchiveLimit
	}
	input := bytes.NewReader(raw)
	gz, err := gzip.NewReader(input)
	if err != nil {
		return nil, fmt.Errorf("%w: gzip: %v", ErrArchiveStructure, err)
	}
	defer func() { _ = gz.Close() }()
	gz.Multistream(false)
	unpacked, err := io.ReadAll(io.LimitReader(gz, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: decompress: %v", ErrArchiveStructure, err)
	}
	if int64(len(unpacked)) > limit {
		return nil, ErrArchiveLimit
	}
	if input.Len() != 0 {
		return nil, fmt.Errorf("%w: trailing gzip data", ErrArchiveStructure)
	}
	return unpacked, nil
}

func decodeEntriesV2(raw []byte, l ArchiveLimits) (ArtifactV2, []byte, error) {
	a := ArtifactV2{Assets: make(map[string][]byte), Licenses: make(map[string][]byte)}
	input := bytes.NewReader(raw)
	tr := tar.NewReader(input)
	seen := make(map[string]bool)
	var manifest []byte
	var payloadPath string
	var count, total int64
	for {
		before := input.Len()
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if before-input.Len() < 1024 {
				return ArtifactV2{}, nil, fmt.Errorf("%w: missing tar terminator", ErrArchiveStructure)
			}
			break
		}
		if err != nil {
			return ArtifactV2{}, nil, fmt.Errorf("%w: tar: %v", ErrArchiveStructure, err)
		}
		count++
		if count > l.MaxTotalFiles+3 {
			return ArtifactV2{}, nil, ErrArchiveLimit
		}
		name := hdr.Name
		if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00") || path.IsAbs(name) || path.Clean(name) != name || name == "." || strings.HasPrefix(name, "../") || seen[name] {
			return ArtifactV2{}, nil, ErrArchiveStructure
		}
		seen[name] = true
		if hdr.Typeflag == tar.TypeDir {
			if hdr.Size != 0 || (name != CanonicalPayloadDir && name != CanonicalAssetsDir && name != CanonicalLicensesDir) {
				return ArtifactV2{}, nil, ErrArchiveStructure
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Linkname != "" {
			return ArtifactV2{}, nil, ErrArchiveStructure
		}
		memberLimit := l.MaxAssetBytes
		switch {
		case name == CanonicalManifestPath:
			memberLimit = l.MaxTotalBytes
		case path.Dir(name) == CanonicalPayloadDir:
			if payloadPath != "" {
				return ArtifactV2{}, nil, ErrArchiveStructure
			}
			memberLimit = l.MaxPayloadBytes
		case path.Dir(name) == CanonicalAssetsDir || path.Dir(name) == CanonicalLicensesDir:
			if err := v2Path(path.Base(name)); err != nil {
				return ArtifactV2{}, nil, err
			}
		default:
			return ArtifactV2{}, nil, ErrArchiveStructure
		}
		if hdr.Size < 0 || hdr.Size > memberLimit || hdr.Size > l.MaxTotalBytes-total {
			return ArtifactV2{}, nil, ErrArchiveLimit
		}
		total += hdr.Size
		data, err := io.ReadAll(io.LimitReader(tr, hdr.Size+1))
		if err != nil || int64(len(data)) != hdr.Size {
			return ArtifactV2{}, nil, ErrArchiveStructure
		}
		switch {
		case name == CanonicalManifestPath:
			manifest = data
		case path.Dir(name) == CanonicalPayloadDir:
			a.Payload = data
			payloadPath = name
		case path.Dir(name) == CanonicalAssetsDir:
			a.Assets[path.Base(name)] = data
		case path.Dir(name) == CanonicalLicensesDir:
			a.Licenses[path.Base(name)] = data
		}
	}
	if input.Len() != 0 {
		for _, b := range raw[len(raw)-input.Len():] {
			if b != 0 {
				return ArtifactV2{}, nil, ErrArchiveStructure
			}
		}
	}
	var err error
	a.Manifest, err = decodeManifestV2(manifest)
	if err != nil {
		return ArtifactV2{}, nil, err
	}
	if payloadPath != PayloadPath(a.Manifest.Kind) {
		return ArtifactV2{}, nil, ErrArchiveStructure
	}
	return a, manifest, nil
}
