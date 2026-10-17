package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	CanonicalManifestPath     = "manifest.json"
	CanonicalPayloadDir       = "resource"
	CanonicalAssetsDir        = "assets"
	CanonicalLicensesDir      = "licenses"
	CanonicalSupportDir       = "support"
	CanonicalArtifactSchemaV1 = "v1"
	ContentDigestPrefix       = "sha256"
	archiveDigestValueLen     = 71 // len("sha256:") + 64 hex characters
	archiveDigestPlaceholder  = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

type SupportFile struct {
	Data       []byte
	Executable bool
}

// SupportFileDigest binds both bytes and executable mode into a hook revision.
func SupportFileDigest(file SupportFile) string {
	mode := byte(0)
	if file.Executable {
		mode = 1
	}
	h := sha256.New()
	_, _ = h.Write([]byte("hook-support-v1\x00"))
	_, _ = h.Write([]byte{mode})
	_, _ = h.Write(file.Data)
	return ContentDigestPrefix + ":" + hex.EncodeToString(h.Sum(nil))
}

// CanonicalArtifact is the in-memory representation of a deterministic,
// self-describing Marketplace artifact. It is encoded as a tar.gz archive.
type CanonicalArtifact struct {
	Manifest   CanonicalManifest
	Payload    []byte
	Assets     map[string][]byte // relative path -> bytes
	Licenses   map[string][]byte // relative path -> bytes
	Support    map[string]SupportFile
	rawArchive []byte // optional: populated by Decode
}

// CanonicalManifest describes the artifact payload and provenance.
type CanonicalManifest struct {
	SchemaVersion    string              `json:"schema_version"`
	ArtifactVersion  string              `json:"artifact_version"`
	Kind             string              `json:"kind"`
	Name             string              `json:"name"`
	RegistryID       string              `json:"registry_id,omitempty"`
	RegistrySlug     string              `json:"registry_slug,omitempty"`
	Revision         string              `json:"revision"`
	ContentDigest    string              `json:"content_digest"`
	ArchiveDigest    string              `json:"archive_digest"`
	License          string              `json:"license,omitempty"`
	AnalyzerVersion  string              `json:"analyzer_version,omitempty"`
	Capabilities     []string            `json:"capabilities,omitempty"`
	Findings         []CanonicalFinding  `json:"findings,omitempty"`
	Provenance       CanonicalProvenance `json:"provenance"`
	SupportedSchemas []string            `json:"supported_schema_versions"`
}

// CanonicalFinding is a single analyzer finding attached to an artifact.
type CanonicalFinding struct {
	Code            string `json:"code"`
	Severity        string `json:"severity"`
	Message         string `json:"message,omitempty"`
	Line            int    `json:"line,omitempty"`
	Column          int    `json:"column,omitempty"`
	File            string `json:"file,omitempty"`
	PolicyOutcome   string `json:"policy_outcome,omitempty"`
	Analyzer        string `json:"analyzer,omitempty"`
	AnalyzerVersion string `json:"analyzer_version,omitempty"`
}

// CanonicalProvenance records where the artifact came from.
type CanonicalProvenance struct {
	RegistryURL string `json:"registry_url,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Path        string `json:"path,omitempty"`
	Generator   string `json:"generator"`
}

// PayloadPath returns the path of the resource payload inside the archive.
func PayloadPath(kind string) string {
	return path.Join(CanonicalPayloadDir, kind+".json")
}

// ContentDigest returns the sha256 digest of a byte payload.
func ContentDigest(payload []byte) string {
	h := sha256.Sum256(payload)
	return ContentDigestPrefix + ":" + hex.EncodeToString(h[:])
}

// ArchiveDigest returns the sha256 digest of a raw archive.
func ArchiveDigest(raw []byte) string {
	h := sha256.Sum256(raw)
	return ContentDigestPrefix + ":" + hex.EncodeToString(h[:])
}

// isDigestFormat reports whether s is "sha256:<64 lower-case hex characters>".
func isDigestFormat(s string) bool {
	if len(s) != archiveDigestValueLen || !strings.HasPrefix(s, ContentDigestPrefix+":") {
		return false
	}
	for i := len(ContentDigestPrefix) + 1; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > 'f' || (c > '9' && c < 'a') {
			return false
		}
	}
	return true
}

// Encode serializes a CanonicalArtifact into a deterministic gzip-compressed
// tar archive and returns the archive bytes together with the archive digest.
//
// The archive digest is computed over the canonical form of the archive in which
// the manifest's archive_digest field is replaced by a fixed-length placeholder.
// This breaks the self-referential cycle: the final archive carries the real
// digest, but verification recomputes the digest over the placeholder form.
func Encode(a CanonicalArtifact) ([]byte, string, error) {
	final, _, digest, err := EncodeArchives(a)
	return final, digest, err
}

// EncodeArchives returns the final archive, the placeholder-form archive whose
// bytes hash to digest, and that digest. The placeholder form is the only
// encoding the digest is computed over, so it can be verified by hashing alone.
func EncodeArchives(a CanonicalArtifact) (final, placeholder []byte, digest string, err error) {
	if err := a.Manifest.Validate(); err != nil {
		return nil, nil, "", fmt.Errorf("invalid manifest: %w", err)
	}

	// 1. Encode with the placeholder so the digest field length stays constant.
	canon := a
	canon.Manifest.ArchiveDigest = archiveDigestPlaceholder
	placeholder, err = encodeArtifact(canon)
	if err != nil {
		return nil, nil, "", err
	}
	digest = ArchiveDigest(placeholder)

	// 2. Encode the final archive with the real digest in the manifest.
	finalArtifact := a
	finalArtifact.Manifest.ArchiveDigest = digest
	final, err = encodeArtifact(finalArtifact)
	if err != nil {
		return nil, nil, "", err
	}

	return final, placeholder, digest, nil
}

type archiveEntry struct {
	path string
	data []byte
	dir  bool
	mode int64
}

// encodeArtifact writes a deterministic gzip-compressed tar archive.
func encodeArtifact(a CanonicalArtifact) ([]byte, error) {
	if err := a.Manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	gw.Name = ""
	gw.Comment = ""
	gw.Extra = nil
	gw.ModTime = time.Time{}
	gw.OS = 0xff // unknown

	tw := tar.NewWriter(gw)

	entries, err := a.buildEntries()
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{
			Name:     e.path,
			Mode:     mode,
			Size:     int64(len(e.data)),
			ModTime:  time.Time{},
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
			Typeflag: tar.TypeReg,
		}
		if e.dir {
			hdr.Mode = 0o755
			hdr.Size = 0
			hdr.Typeflag = tar.TypeDir
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("write tar header for %s: %w", e.path, err)
		}
		if !e.dir && len(e.data) > 0 {
			if _, err := tw.Write(e.data); err != nil {
				return nil, fmt.Errorf("write tar body for %s: %w", e.path, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (a CanonicalArtifact) buildEntries() ([]archiveEntry, error) {
	manifestBytes, err := json.Marshal(a.Manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	// Canonical order: manifest, resource directory, payload, then assets and licenses.
	entries := []archiveEntry{
		{path: CanonicalManifestPath, data: manifestBytes},
		{path: CanonicalPayloadDir, dir: true},
		{path: PayloadPath(a.Manifest.Kind), data: a.Payload},
	}

	if len(a.Assets) > 0 {
		entries = append(entries, archiveEntry{path: CanonicalAssetsDir, dir: true})
		keys := sortedKeys(a.Assets)
		for _, k := range keys {
			if err := validateArchiveMemberPath(k); err != nil {
				return nil, fmt.Errorf("invalid asset path: %w", err)
			}
			entries = append(entries, archiveEntry{path: path.Join(CanonicalAssetsDir, k), data: a.Assets[k]})
		}
	}

	if len(a.Licenses) > 0 {
		entries = append(entries, archiveEntry{path: CanonicalLicensesDir, dir: true})
		keys := sortedKeys(a.Licenses)
		for _, k := range keys {
			if err := validateArchiveMemberPath(k); err != nil {
				return nil, fmt.Errorf("invalid license path: %w", err)
			}
			entries = append(entries, archiveEntry{path: path.Join(CanonicalLicensesDir, k), data: a.Licenses[k]})
		}
	}

	if len(a.Support) > 0 {
		entries = append(entries, archiveEntry{path: CanonicalSupportDir, dir: true})
		keys := sortedKeys(a.Support)
		for _, k := range keys {
			member, err := supportMemberName(k)
			if err != nil {
				return nil, fmt.Errorf("invalid support path: %w", err)
			}
			support := a.Support[k]
			mode := int64(0o644)
			if support.Executable {
				mode = 0o755
			}
			entries = append(entries, archiveEntry{path: path.Join(CanonicalSupportDir, member), data: support.Data, mode: mode})
		}
	}

	return entries, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateArchiveMemberPath checks that a relative archive member path is safe.
func validateArchiveMemberPath(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("absolute path: %s", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return fmt.Errorf("path traversal: %s", p)
		}
	}
	if strings.Contains(p, "/") {
		return fmt.Errorf("nested archive member path: %s", p)
	}
	return nil
}

func ValidateSupportPath(p string) error {
	if !filepath.IsLocal(p) || p == "." || strings.Contains(p, "\\") {
		return fmt.Errorf("invalid support path: %s", p)
	}
	if p == "SKILL.md" {
		return fmt.Errorf("support path %s collides with the compiled skill manifest", p)
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("absolute support path: %s", p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("unclean support path: %s", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return fmt.Errorf("support path traversal: %s", p)
		}
	}
	return nil
}

func supportMemberName(p string) (string, error) {
	if err := ValidateSupportPath(p); err != nil {
		return "", err
	}
	member := url.PathEscape(p)
	if strings.Contains(member, "/") {
		return "", fmt.Errorf("support member path %q is not flat", member)
	}
	return member, nil
}

func supportPathFromMember(member string) (string, error) {
	rel, err := url.PathUnescape(member)
	if err != nil {
		return "", fmt.Errorf("invalid support member %q: %w", member, err)
	}
	if err := ValidateSupportPath(rel); err != nil {
		return "", fmt.Errorf("invalid support member %q: %w", member, err)
	}
	return rel, nil
}

// Decode reads a canonical artifact archive and returns the parsed artifact plus
// the raw archive bytes.
func Decode(r io.Reader) (CanonicalArtifact, []byte, error) {
	var raw bytes.Buffer
	tr, err := gzipToTar(io.TeeReader(r, &raw))
	if err != nil {
		return CanonicalArtifact{}, nil, err
	}

	var a CanonicalArtifact
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return CanonicalArtifact{}, nil, fmt.Errorf("tar next: %w", err)
		}

		name, err := validateTarHeaderPath(hdr.Name)
		if err != nil {
			return CanonicalArtifact{}, nil, err
		}

		switch hdr.Typeflag {
		case tar.TypeReg:
			data, err := safeTarRead(tr, hdr.Size)
			if err != nil {
				return CanonicalArtifact{}, nil, fmt.Errorf("read tar body for %s: %w", hdr.Name, err)
			}
			switch name {
			case CanonicalManifestPath:
				if err := json.Unmarshal(data, &a.Manifest); err != nil {
					return CanonicalArtifact{}, nil, fmt.Errorf("unmarshal manifest: %w", err)
				}
			default:
				dir := path.Dir(name)
				base := path.Base(name)
				switch dir {
				case CanonicalPayloadDir:
					a.Payload = data
				case CanonicalAssetsDir:
					if a.Assets == nil {
						a.Assets = make(map[string][]byte)
					}
					a.Assets[base] = data
				case CanonicalLicensesDir:
					if a.Licenses == nil {
						a.Licenses = make(map[string][]byte)
					}
					a.Licenses[base] = data
				case CanonicalSupportDir:
					rel, err := supportPathFromMember(base)
					if err != nil {
						return CanonicalArtifact{}, nil, err
					}
					if a.Support == nil {
						a.Support = make(map[string]SupportFile)
					}
					if _, dup := a.Support[rel]; dup {
						return CanonicalArtifact{}, nil, fmt.Errorf("duplicate support member %q", rel)
					}
					a.Support[rel] = SupportFile{Data: data, Executable: hdr.Mode&0o111 != 0}
				}
			}
		case tar.TypeDir:
			// Directories carry no body; membership is implied by file paths.
		case tar.TypeLink, tar.TypeSymlink:
			return CanonicalArtifact{}, nil, fmt.Errorf("tar entry contains link: %s", hdr.Name)
		default:
			return CanonicalArtifact{}, nil, fmt.Errorf("tar entry type %q not allowed: %s", string(hdr.Typeflag), hdr.Name)
		}
	}

	a.rawArchive = raw.Bytes()
	return a, raw.Bytes(), nil
}

// validateTarHeaderPath rejects absolute paths, parent traversal, and empty names.
func validateTarHeaderPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("tar entry has empty name")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", errors.New("tar entry path traversal")
		}
	}
	if !filepath.IsLocal(p) {
		return "", errors.New("tar entry absolute path")
	}
	return path.Clean(p), nil
}

// safeTarRead reads a regular file body, capping the read to a generous ceiling
// so a malformed archive cannot exhaust memory. Callers validate sizes later.
func safeTarRead(r io.Reader, declared int64) ([]byte, error) {
	const ceiling = 64 * 1024 * 1024 // 64 MiB safety ceiling
	limit := declared
	if limit <= 0 || limit > ceiling {
		limit = ceiling
	}
	return io.ReadAll(io.LimitReader(r, limit+1))
}

func gzipToTar(r io.Reader) (*tar.Reader, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	return tar.NewReader(gr), nil
}

// VerifyContentDigest checks that the sha256 digest of the payload matches the
// manifest's ContentDigest.
func (a CanonicalArtifact) VerifyContentDigest() error {
	got := ContentDigest(a.Payload)
	if got != a.Manifest.ContentDigest {
		return fmt.Errorf("content digest mismatch: got %s, want %s", got, a.Manifest.ContentDigest)
	}
	return nil
}

// VerifyArchiveDigest checks that the manifest's archive_digest matches the
// canonical archive. If raw is non-nil, it also verifies that raw is byte-identical
// to the re-encoded canonical archive with the real archive_digest value.
func (a CanonicalArtifact) VerifyArchiveDigest(raw []byte) error {
	if a.Manifest.ArchiveDigest == "" {
		return errors.New("manifest archive_digest is empty")
	}
	if !isDigestFormat(a.Manifest.ArchiveDigest) {
		return fmt.Errorf("manifest archive_digest has invalid format: %s", a.Manifest.ArchiveDigest)
	}

	// The digest is over the canonical form where archive_digest is the placeholder.
	canon := a
	canon.Manifest.ArchiveDigest = archiveDigestPlaceholder
	canon.rawArchive = nil
	canonRaw, err := encodeArtifact(canon)
	if err != nil {
		return fmt.Errorf("re-encode canonical artifact: %w", err)
	}
	got := ArchiveDigest(canonRaw)
	if got != a.Manifest.ArchiveDigest {
		return fmt.Errorf("archive digest mismatch: got %s, want %s", got, a.Manifest.ArchiveDigest)
	}

	// When raw bytes are supplied, confirm they are the canonical final archive.
	if len(raw) > 0 {
		actualRaw, err := encodeArtifact(a)
		if err != nil {
			return fmt.Errorf("re-encode artifact: %w", err)
		}
		if !bytes.Equal(actualRaw, raw) {
			return errors.New("raw archive bytes do not match canonical archive")
		}
	}

	return nil
}

// ErrPlaceholderNotReproducible reports that re-encoding a final archive does
// not reproduce the digest it is addressed by.
var ErrPlaceholderNotReproducible = errors.New("placeholder archive is not reproducible from the final archive")

// VerifyPlaceholderArchive verifies a placeholder-form archive by hashing raw
// against digest, so the bytes are checked as received and no compressor runs.
// The returned artifact carries digest as its archive digest.
func VerifyPlaceholderArchive(raw []byte, digest string) (CanonicalArtifact, error) {
	if !isDigestFormat(digest) {
		return CanonicalArtifact{}, fmt.Errorf("archive digest has invalid format: %s", digest)
	}
	if got := ArchiveDigest(raw); got != digest {
		return CanonicalArtifact{}, fmt.Errorf("placeholder archive digest mismatch: got %s, want %s", got, digest)
	}
	a, _, err := Decode(bytes.NewReader(raw))
	if err != nil {
		return CanonicalArtifact{}, err
	}
	if a.Manifest.ArchiveDigest != archiveDigestPlaceholder {
		return CanonicalArtifact{}, errors.New("placeholder archive manifest archive_digest is not the placeholder")
	}
	if err := a.validateFields(); err != nil {
		return CanonicalArtifact{}, err
	}
	if err := a.validateLimits(DefaultArchiveLimits()); err != nil {
		return CanonicalArtifact{}, err
	}
	a.Manifest.ArchiveDigest = digest
	a.rawArchive = nil
	return a, nil
}

// VerifyFinalAgainstPlaceholder verifies a final archive against a placeholder
// artifact already verified by VerifyPlaceholderArchive. The final bytes are
// accepted when they decode to the same content and carry the same archive
// digest; their compression is deliberately not reproduced.
func VerifyFinalAgainstPlaceholder(final []byte, placeholder CanonicalArtifact) (CanonicalArtifact, error) {
	a, _, err := Decode(bytes.NewReader(final))
	if err != nil {
		return CanonicalArtifact{}, err
	}
	if a.Manifest.ArchiveDigest != placeholder.Manifest.ArchiveDigest {
		return CanonicalArtifact{}, fmt.Errorf("final archive digest %q does not match placeholder digest %q", a.Manifest.ArchiveDigest, placeholder.Manifest.ArchiveDigest)
	}
	if err := a.validateFields(); err != nil {
		return CanonicalArtifact{}, err
	}
	if err := a.validateLimits(DefaultArchiveLimits()); err != nil {
		return CanonicalArtifact{}, err
	}
	if !EqualIgnoringArchiveDigest(a, placeholder) {
		return CanonicalArtifact{}, errors.New("final archive content does not match verified placeholder archive")
	}
	return a, nil
}

// EncodingMismatchError reports a final archive whose content verifies but which
// the running compressor does not reproduce to its archive digest.
type EncodingMismatchError struct {
	Want string
	Got  string
}

func (e *EncodingMismatchError) Error() string {
	return fmt.Sprintf("archive digest mismatch: got %s, want %s", e.Got, e.Want)
}

// VerifyArchiveBytes verifies archive bytes received for digest. Placeholder-form
// bytes are verified by hash. Final bytes are verified by re-encoding only as the
// legacy fallback for archives without a placeholder form; a digest the running
// compressor does not reproduce is reported as *EncodingMismatchError, while
// content failures stay integrity errors.
func VerifyArchiveBytes(raw []byte, digest string) (CanonicalArtifact, error) {
	if ArchiveDigest(raw) == digest {
		return VerifyPlaceholderArchive(raw, digest)
	}
	a, rawArchive, err := Decode(bytes.NewReader(raw))
	if err != nil {
		return CanonicalArtifact{}, err
	}
	if a.Manifest.ArchiveDigest != digest {
		return CanonicalArtifact{}, fmt.Errorf("manifest archive digest %q does not match requested %q", a.Manifest.ArchiveDigest, digest)
	}
	if err := a.validateFields(); err != nil {
		return CanonicalArtifact{}, err
	}
	if err := a.validateLimits(DefaultArchiveLimits()); err != nil {
		return CanonicalArtifact{}, err
	}
	canon := a
	canon.Manifest.ArchiveDigest = archiveDigestPlaceholder
	canon.rawArchive = nil
	canonRaw, err := encodeArtifact(canon)
	if err != nil {
		return CanonicalArtifact{}, fmt.Errorf("re-encode canonical artifact: %w", err)
	}
	if got := ArchiveDigest(canonRaw); got != digest {
		return CanonicalArtifact{}, &EncodingMismatchError{Want: digest, Got: got}
	}
	a.rawArchive = rawArchive
	return a, nil
}

// DecodeContentArchive decodes an archive and verifies its fields, limits, and
// content digest without binding it to any archive digest or re-encoding it.
func DecodeContentArchive(raw []byte) (CanonicalArtifact, error) {
	a, _, err := Decode(bytes.NewReader(raw))
	if err != nil {
		return CanonicalArtifact{}, err
	}
	if err := a.validateFields(); err != nil {
		return CanonicalArtifact{}, err
	}
	if err := a.validateLimits(DefaultArchiveLimits()); err != nil {
		return CanonicalArtifact{}, err
	}
	return a, nil
}

// DerivePlaceholderArchive rebuilds the placeholder-form archive of a final
// archive with the running compressor. This is the only intentionally
// compressor-dependent path, so its result is returned only when its hash equals
// digest; otherwise it returns ErrPlaceholderNotReproducible.
func DerivePlaceholderArchive(final []byte, digest string) ([]byte, error) {
	a, _, err := Decode(bytes.NewReader(final))
	if err != nil {
		return nil, err
	}
	a.Manifest.ArchiveDigest = archiveDigestPlaceholder
	placeholder, err := encodeArtifact(a)
	if err != nil {
		return nil, err
	}
	if ArchiveDigest(placeholder) != digest {
		return nil, ErrPlaceholderNotReproducible
	}
	return placeholder, nil
}

// EqualIgnoringArchiveDigest reports whether a and b have the same manifest, once
// each archive_digest is normalized to the placeholder, and the same content.
func EqualIgnoringArchiveDigest(a, b CanonicalArtifact) bool {
	am, errA := canonicalManifestJSON(a.Manifest)
	bm, errB := canonicalManifestJSON(b.Manifest)
	if errA != nil || errB != nil || !bytes.Equal(am, bm) {
		return false
	}
	return bytes.Equal(a.Payload, b.Payload) &&
		equalMaps(a.Assets, b.Assets, bytes.Equal) &&
		equalMaps(a.Licenses, b.Licenses, bytes.Equal) &&
		equalMaps(a.Support, b.Support, func(x, y SupportFile) bool {
			return x.Executable == y.Executable && bytes.Equal(x.Data, y.Data)
		})
}

func canonicalManifestJSON(m CanonicalManifest) ([]byte, error) {
	m.ArchiveDigest = archiveDigestPlaceholder
	return json.Marshal(m)
}

func equalMaps[V any](a, b map[string]V, equal func(x, y V) bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !equal(v, w) {
			return false
		}
	}
	return true
}

// Validate checks the manifest fields for well-formedness.
func (m CanonicalManifest) Validate() error {
	if m.SchemaVersion != CanonicalArtifactSchemaV1 {
		return fmt.Errorf("unsupported schema version %q", m.SchemaVersion)
	}
	if m.Name == "" {
		return errors.New("manifest name is required")
	}
	if m.Kind == "" {
		return errors.New("manifest kind is required")
	}
	if m.ContentDigest == "" {
		return errors.New("manifest content_digest is required")
	}
	if !isDigestFormat(m.ContentDigest) {
		return fmt.Errorf("manifest content_digest must be sha256:<64-hex>")
	}
	if m.ArchiveDigest != "" && !isDigestFormat(m.ArchiveDigest) {
		return fmt.Errorf("manifest archive_digest must be sha256:<64-hex>")
	}
	for i, f := range m.Findings {
		if f.Code == "" {
			return fmt.Errorf("findings[%d].code is required", i)
		}
		switch f.PolicyOutcome {
		case "reject", "warn", "allow":
		default:
			return fmt.Errorf("findings[%d].policy_outcome %q is invalid", i, f.PolicyOutcome)
		}
		switch f.Severity {
		case "reject", "warn", "info":
		default:
			return fmt.Errorf("findings[%d].severity %q is invalid", i, f.Severity)
		}
	}
	return nil
}

// ValidateArchive performs light structural checks on a decoded artifact. Use
// limits=nil to enforce defaults.
func (a CanonicalArtifact) ValidateArchive(limits *ArchiveLimits) error {
	if limits == nil {
		limits = DefaultArchiveLimits()
	}
	if err := a.validateFields(); err != nil {
		return err
	}
	if len(a.rawArchive) > 0 {
		if err := a.VerifyArchiveDigest(a.rawArchive); err != nil {
			return err
		}
	}
	return a.validateLimits(limits)
}

// validateFields checks manifest and payload identity without re-encoding the archive.
func (a CanonicalArtifact) validateFields() error {
	if err := a.Manifest.Validate(); err != nil {
		return err
	}
	if a.Manifest.ArchiveDigest == "" {
		return errors.New("manifest archive_digest is required")
	}
	if len(a.Payload) == 0 {
		return errors.New("artifact payload is empty")
	}
	return a.VerifyContentDigest()
}

// validateLimits enforces the archive limits on the decoded members.
func (a CanonicalArtifact) validateLimits(limits *ArchiveLimits) error {
	if int64(len(a.Payload)) > limits.MaxPayloadBytes {
		return fmt.Errorf("payload exceeds max bytes %d", limits.MaxPayloadBytes)
	}

	countAssets := int64(len(a.Assets))
	if countAssets > limits.MaxAssetFiles {
		return fmt.Errorf("too many asset files: %d > %d", countAssets, limits.MaxAssetFiles)
	}

	totalFiles := int64(1) // payload
	totalSize := int64(len(a.Payload))
	for name, data := range a.Assets {
		totalFiles++
		totalSize += int64(len(data))
		if err := validateArchiveMemberPath(name); err != nil {
			return fmt.Errorf("invalid asset path: %w", err)
		}
		if int64(len(data)) > limits.MaxAssetBytes {
			return fmt.Errorf("asset %s exceeds max bytes %d", name, limits.MaxAssetBytes)
		}
	}
	for name, data := range a.Licenses {
		totalFiles++
		totalSize += int64(len(data))
		if err := validateArchiveMemberPath(name); err != nil {
			return fmt.Errorf("invalid license path: %w", err)
		}
		if int64(len(data)) > limits.MaxAssetBytes {
			return fmt.Errorf("license %s exceeds max bytes %d", name, limits.MaxAssetBytes)
		}
	}

	countSupport := int64(len(a.Support))
	if countSupport > limits.MaxAssetFiles {
		return fmt.Errorf("too many support files: %d > %d", countSupport, limits.MaxAssetFiles)
	}
	for name, support := range a.Support {
		totalFiles++
		totalSize += int64(len(support.Data))
		if err := ValidateSupportPath(name); err != nil {
			return fmt.Errorf("invalid support path: %w", err)
		}
		if int64(len(support.Data)) > limits.MaxAssetBytes {
			return fmt.Errorf("support %s exceeds max bytes %d", name, limits.MaxAssetBytes)
		}
	}

	if limits.MaxTotalFiles > 0 && totalFiles > limits.MaxTotalFiles {
		return fmt.Errorf("total files %d exceeds max %d", totalFiles, limits.MaxTotalFiles)
	}
	if limits.MaxTotalBytes > 0 && totalSize > limits.MaxTotalBytes {
		return fmt.Errorf("total archive size %d exceeds max %d", totalSize, limits.MaxTotalBytes)
	}

	return nil
}

// ArchiveLimits caps decoded artifact resources.
type ArchiveLimits struct {
	MaxPayloadBytes int64
	MaxTotalBytes   int64
	MaxAssetFiles   int64
	MaxTotalFiles   int64
	MaxAssetBytes   int64
}

// DefaultArchiveLimits returns reasonable defaults.
func DefaultArchiveLimits() *ArchiveLimits {
	return &ArchiveLimits{
		MaxPayloadBytes: 8 * 1024 * 1024,
		MaxTotalBytes:   64 * 1024 * 1024,
		MaxAssetFiles:   256,
		MaxTotalFiles:   1024,
		MaxAssetBytes:   2 * 1024 * 1024,
	}
}

// FromIR builds a CanonicalArtifact from an IR resource. The payload is the
// canonical JSON serialization of the resource with the resource name injected.
func FromIR(r ir.Resource) (CanonicalArtifact, error) {
	if r == nil {
		return CanonicalArtifact{}, errors.New("nil resource")
	}

	kind := irToCanonicalKind(r.Kind())
	if kind == "" {
		return CanonicalArtifact{}, fmt.Errorf("unsupported resource kind %q", r.Kind())
	}

	payload, err := resourceJSON(r)
	if err != nil {
		return CanonicalArtifact{}, err
	}

	manifest := CanonicalManifest{
		SchemaVersion:   CanonicalArtifactSchemaV1,
		ArtifactVersion: CanonicalArtifactSchemaV1,
		Kind:            kind,
		Name:            r.Name(),
		Revision:        "",
		ContentDigest:   ContentDigest(payload),
		Provenance: CanonicalProvenance{
			Generator: "agent-marketplace canonical artifact v1",
		},
		SupportedSchemas: []string{CanonicalArtifactSchemaV1},
	}

	a := CanonicalArtifact{
		Manifest: manifest,
		Payload:  payload,
	}

	raw, digest, err := Encode(a)
	if err != nil {
		return CanonicalArtifact{}, err
	}
	a.Manifest.ArchiveDigest = digest
	a.rawArchive = raw

	return a, nil
}

func irToCanonicalKind(k ir.ResourceKind) string {
	switch k {
	case ir.KindSkill:
		return "skill"
	case ir.KindCommand:
		return "command"
	case ir.KindSubagent:
		return "subagent"
	case ir.KindHook:
		return "hook"
	case ir.KindMcpServer:
		return "mcpserver"
	}
	return ""
}

func resourceJSON(r ir.Resource) ([]byte, error) {
	// Marshal a copy of r with the Extra field zeroed. Extra may hold
	// map[interface{}]interface{} values from YAML parsers, which
	// json.Marshal cannot serialize, and should be merged manually below.
	copyVal, err := copyResourceWithoutExtra(r)
	if err != nil {
		return nil, fmt.Errorf("copy resource without extra: %w", err)
	}

	base, err := json.Marshal(copyVal)
	if err != nil {
		return nil, fmt.Errorf("marshal resource: %w", err)
	}

	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, fmt.Errorf("unmarshal resource: %w", err)
	}

	extra, err := extractExtra(r)
	if err != nil {
		return nil, fmt.Errorf("extract resource extra: %w", err)
	}
	modelBearing := r.Kind() == ir.KindSkill || r.Kind() == ir.KindSubagent
	for k, v := range extra {
		if modelBearing && (strings.EqualFold(k, "ModelSource") || strings.EqualFold(k, "ToolSource")) {
			continue
		}
		if r.Kind() == ir.KindCommand && strings.EqualFold(k, "ToolSource") {
			continue
		}
		if r.Kind() == ir.KindHook && strings.EqualFold(k, "HookSource") {
			continue
		}
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}

	m["name"] = r.Name()

	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical payload: %w", err)
	}
	if err := jsonutil.ValidateFieldAliases("", out, r); err != nil {
		return nil, err
	}
	if err := jsonutil.ValidateFieldAliases("", out, &struct {
		Name string `json:"name"`
	}{}); err != nil {
		return nil, err
	}
	return out, nil
}

// copyResourceWithoutExtra returns a shallow copy of r with its Extra field
// set to nil so json.Marshal does not attempt to serialize unknown frontmatter
// values (which may contain map[interface{}]interface{} from YAML parsers).
func copyResourceWithoutExtra(r ir.Resource) (any, error) {
	if r == nil {
		return nil, errors.New("nil resource")
	}

	v := reflect.ValueOf(r)
	if v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, errors.New("nil resource pointer")
	}

	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("resource %T is not a struct", r)
	}

	cloned := reflect.New(v.Type()).Elem()
	cloned.Set(v)
	extra := cloned.FieldByName("Extra")
	if extra.IsValid() && !extra.IsZero() {
		extra.Set(reflect.Zero(extra.Type()))
	}
	return cloned.Addr().Interface(), nil
}

// extractExtra reads the Extra field from the concrete resource value and
// returns a copy with nested map[interface{}]interface{} values normalized
// to map[string]any so json.Marshal can serialize them.
func extractExtra(r ir.Resource) (map[string]any, error) {
	if r == nil {
		return nil, nil
	}

	v := reflect.ValueOf(r)
	if v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return nil, nil
	}

	f := v.FieldByName("Extra")
	if !f.IsValid() || f.IsNil() {
		return nil, nil
	}

	extra, ok := f.Interface().(map[string]any)
	if !ok {
		return nil, fmt.Errorf("resource %T Extra field is not map[string]any", r)
	}
	return normalizeExtraMap(extra), nil
}

func normalizeExtraMap(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		out[k] = normalizeExtra(v)
	}
	return out
}

func normalizeExtra(v any) any {
	switch x := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]any, len(x))
		for k, v := range x {
			ks, ok := k.(string)
			if !ok {
				ks = fmt.Sprintf("%v", k)
			}
			m[ks] = normalizeExtra(v)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, v := range x {
			m[k] = normalizeExtra(v)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = normalizeExtra(v)
		}
		return out
	default:
		return v
	}
}
