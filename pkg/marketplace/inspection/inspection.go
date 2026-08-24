package inspection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const SchemaVersion = "v1"

// MaxManifestBytes bounds the serialized inspection projection independently
// from the repository and individual-file limits. A manifest contains
// attacker-controlled paths and findings, so content limits alone are not
// sufficient to bound its response size.
const MaxManifestBytes int64 = 1 * 1024 * 1024

const (
	ClassText     = "text"
	ClassImage    = "image"
	ClassMetadata = "metadata"
	MediaMarkdown = "text/markdown"
)

type File struct {
	Path            string `json:"path"`
	ContentDigest   string `json:"content_digest"`
	MediaType       string `json:"media_type"`
	Size            int64  `json:"size"`
	InspectionClass string `json:"inspection_class"`
}

type Finding struct {
	Code      string `json:"code"`
	Severity  string `json:"severity"`
	Policy    string `json:"policy,omitempty"`
	Message   string `json:"message,omitempty"`
	Path      string `json:"path,omitempty"`
	StartByte int64  `json:"start_byte,omitempty"`
	EndByte   int64  `json:"end_byte,omitempty"`
}

type Manifest struct {
	SchemaVersion  string                   `json:"schema_version"`
	Coordinate     store.ResourceCoordinate `json:"coordinate"`
	RevisionDigest string                   `json:"revision_digest"`
	PrimaryPath    string                   `json:"primary_path"`
	Files          []File                   `json:"files"`
	Findings       []Finding                `json:"findings,omitempty"`
	ManifestDigest string                   `json:"-"`
}

type Projection struct {
	Manifest Manifest
	Payload  []byte
	Bodies   map[string][]byte
}

// Build creates a deterministic, bounded projection. The optional manifest
// limit is retained as an argument so callers with a tighter policy can avoid
// duplicating the projection builder; omitted or non-positive values use the
// package default.
func Build(coordinate store.ResourceCoordinate, revision string, body []byte, support map[string][]byte, findings []analyze.Finding, maxFiles int, maxFileBytes, maxTotalBytes int64, manifestLimits ...int64) (Projection, error) {
	if coordinate.Kind != "skill" {
		return Projection{}, fmt.Errorf("inspection projection only supports skills, got %q", coordinate.Kind)
	}
	if coordinate.Name == "" || !utf8.Valid(body) {
		return Projection{}, fmt.Errorf("skill primary content must be valid UTF-8")
	}
	manifestLimit := MaxManifestBytes
	if len(manifestLimits) > 0 && manifestLimits[0] > 0 {
		manifestLimit = manifestLimits[0]
	}
	if maxFileBytes > 0 && int64(len(body)) > maxFileBytes {
		return Projection{}, fmt.Errorf("primary content exceeds file limit %d", maxFileBytes)
	}
	if maxFiles > 0 && len(support)+1 > maxFiles {
		return Projection{}, fmt.Errorf("inspection file count exceeds limit %d", maxFiles)
	}

	files := make([]File, 0, len(support)+1)
	bodies := make(map[string][]byte, len(support)+1)
	seenPaths := map[string]struct{}{"SKILL.md": {}}
	add := func(name string, data []byte, mediaType, class string, expose bool) error {
		if maxFileBytes > 0 && int64(len(data)) > maxFileBytes {
			return fmt.Errorf("inspection file %q exceeds file limit %d", name, maxFileBytes)
		}
		if _, exists := seenPaths[name]; exists {
			return fmt.Errorf("duplicate inspection path %q", name)
		}
		seenPaths[name] = struct{}{}
		contentDigest := digest(data)
		files = append(files, File{Path: name, ContentDigest: contentDigest, MediaType: mediaType, Size: int64(len(data)), InspectionClass: class})
		if expose {
			bodies[contentDigest] = append([]byte(nil), data...)
		}
		return nil
	}
	// The primary path is intentionally added outside the support map, so a
	// support alias can never shadow the canonical Markdown document.
	files = append(files, File{Path: "SKILL.md", ContentDigest: digest(body), MediaType: MediaMarkdown, Size: int64(len(body)), InspectionClass: ClassText})
	bodies[digest(body)] = append([]byte(nil), body...)

	keys := make([]string, 0, len(support))
	for name := range support {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	total := int64(len(body))
	for _, name := range keys {
		canonical, err := canonicalSupportPath(name)
		if err != nil {
			return Projection{}, err
		}
		data := support[name]
		total += int64(len(data))
		if maxTotalBytes > 0 && total > maxTotalBytes {
			return Projection{}, fmt.Errorf("inspection content exceeds total limit %d", maxTotalBytes)
		}
		mediaType, class, expose := mediaPolicy(canonical, data)
		if err := add(canonical, data, mediaType, class, expose); err != nil {
			return Projection{}, err
		}
	}
	if maxTotalBytes > 0 && total > maxTotalBytes {
		return Projection{}, fmt.Errorf("inspection content exceeds total limit %d", maxTotalBytes)
	}

	out := Manifest{SchemaVersion: SchemaVersion, Coordinate: coordinate, RevisionDigest: revision, PrimaryPath: "SKILL.md", Files: files}
	for _, finding := range findings {
		out.Findings = append(out.Findings, mapFinding(finding, body, support))
	}
	sort.Slice(out.Findings, func(i, j int) bool {
		if out.Findings[i].Path != out.Findings[j].Path {
			return out.Findings[i].Path < out.Findings[j].Path
		}
		if out.Findings[i].StartByte != out.Findings[j].StartByte {
			return out.Findings[i].StartByte < out.Findings[j].StartByte
		}
		return out.Findings[i].Code < out.Findings[j].Code
	})
	payload, err := canonicalJSON(out)
	if err != nil {
		return Projection{}, err
	}
	if manifestLimit > 0 && int64(len(payload)) > manifestLimit {
		return Projection{}, fmt.Errorf("inspection manifest exceeds serialized limit %d", manifestLimit)
	}
	out.ManifestDigest = digest(payload)
	final, err := json.Marshal(out)
	if err != nil {
		return Projection{}, fmt.Errorf("encode inspection manifest: %w", err)
	}
	if manifestLimit > 0 && int64(len(final)) > manifestLimit {
		return Projection{}, fmt.Errorf("inspection manifest exceeds serialized limit %d", manifestLimit)
	}
	return Projection{Manifest: out, Payload: final, Bodies: bodies}, nil
}

func canonicalSupportPath(name string) (string, error) {
	original := name
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || path.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("invalid support path %q", original)
	}
	// Do not clean attacker-controlled paths: cleaning would turn traversal and
	// aliases such as a/../b into a seemingly safe duplicate.
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid support path %q", original)
		}
	}
	if name == "SKILL.md" {
		return "", fmt.Errorf("support path %q shadows primary document", original)
	}
	return path.Join("support", name), nil
}

func mediaPolicy(name string, data []byte) (string, string, bool) {
	ext := strings.ToLower(path.Ext(name))
	if isUnsafeExtension(ext) || looksUnsafeText(data) {
		return "application/octet-stream", ClassMetadata, false
	}
	if media := http.DetectContentType(data); media == "image/png" || media == "image/jpeg" || media == "image/gif" || media == "image/webp" {
		return media, ClassImage, true
	}
	if !utf8.Valid(data) {
		return "application/octet-stream", ClassMetadata, false
	}
	switch ext {
	case ".md", ".markdown":
		return MediaMarkdown, ClassText, true
	case ".json":
		if json.Valid(data) {
			return "application/json", ClassText, true
		}
		return "application/octet-stream", ClassMetadata, false
	case ".yaml", ".yml", ".txt":
		return map[string]string{".yaml": "application/yaml", ".yml": "application/yaml", ".txt": "text/plain"}[ext], ClassText, true
	default:
		// A textual file with an unrecognised extension is still safe to serve as
		// plain text, while binary and executable formats remain metadata-only.
		return "text/plain", ClassText, true
	}
}

func isUnsafeExtension(ext string) bool {
	switch ext {
	case ".svg", ".html", ".htm", ".xhtml", ".xml", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".css":
		return true
	default:
		return false
	}
}

// IsSafeText reports whether bytes are valid, non-control UTF-8 text that is
// not an active HTML, script, stylesheet, XML, or SVG payload.
func IsSafeText(data []byte) bool { return !looksUnsafeText(data) }

// looksUnsafeText protects direct content consumers when a dangerous payload is
// given a harmless-looking filename or no filename at all.
func looksUnsafeText(data []byte) bool {
	if !utf8.Valid(data) {
		return true
	}
	for _, b := range data {
		if b == 0 || (b < 0x09) || (b > 0x0d && b < 0x20) || b == 0x7f {
			return true
		}
	}
	text := strings.ToLower(strings.TrimSpace(string(data)))
	if text == "" {
		return false
	}
	for _, marker := range []string{"<script", "<svg", "<html", "<!doctype", "<?xml", "<style", "<iframe", "onerror=", "onload="} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if start := strings.IndexByte(text, '<'); start >= 0 && start+1 < len(text) {
		next := text[start+1]
		if (next >= 'a' && next <= 'z') || next == '/' || next == '!' || next == '?' {
			if strings.IndexByte(text[start+1:], '>') >= 0 {
				return true
			}
		}
	}
	return strings.HasPrefix(text, "#!") || strings.HasPrefix(text, "javascript:") ||
		strings.HasPrefix(text, "function ") || strings.HasPrefix(text, "async function ") ||
		strings.HasPrefix(text, "const ") || strings.HasPrefix(text, "let ") ||
		strings.HasPrefix(text, "var ") || strings.HasPrefix(text, "import ") ||
		strings.HasPrefix(text, "export ") || strings.HasPrefix(text, "console.") ||
		strings.HasPrefix(text, "window.") || strings.HasPrefix(text, "document.") ||
		strings.HasPrefix(text, "alert(") ||
		(strings.Contains(text, "{") && strings.Contains(text, "}") && strings.Contains(text, ":") && !json.Valid(data))
}

func mapFinding(f analyze.Finding, body []byte, support map[string][]byte) Finding {
	result := Finding{Code: f.Code, Severity: f.Severity, Policy: f.PolicyOutcome, Message: f.Message}
	file := ""
	if f.SourceRange != nil {
		file = f.SourceRange.File
	}
	content := body
	switch {
	case strings.HasSuffix(file, "/body") || strings.HasSuffix(file, "/name") || strings.Contains(file, "/Description"):
		result.Path = "SKILL.md"
	case strings.HasPrefix(file, "skill/"):
		// Findings over structured fields are still anchored to the primary
		// canonical document rather than exposing analyzer implementation paths.
		result.Path = "SKILL.md"
	default:
		name := strings.TrimPrefix(strings.ReplaceAll(file, "\\", "/"), "support/")
		if data, ok := support[name]; ok {
			result.Path, content = path.Join("support", name), data
		} else {
			result.Path = "SKILL.md"
		}
	}
	if f.SourceRange != nil && f.SourceRange.Line > 0 {
		start, end := lineColumnBytes(content, f.SourceRange.Line, f.SourceRange.Column)
		result.StartByte, result.EndByte = int64(start), int64(end)
	}
	return result
}

func lineColumnBytes(content []byte, line, column int) (int, int) {
	if line < 1 {
		return 0, 0
	}
	start := 0
	for current := 1; current < line; current++ {
		n := bytes.IndexByte(content[start:], '\n')
		if n < 0 {
			return len(content), len(content)
		}
		start += n + 1
	}
	if column > 1 {
		for i, current := start, 1; i < len(content) && current < column; current++ {
			_, size := utf8.DecodeRune(content[i:])
			i += size
			start = i
		}
	}
	if start >= len(content) {
		return start, start
	}
	_, size := utf8.DecodeRune(content[start:])
	return start, start + size
}

func canonicalJSON(m Manifest) ([]byte, error) {
	copy := m
	copy.ManifestDigest = ""
	return json.Marshal(copy)
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digest(data []byte) string { return Digest(data) }

func Verify(manifest Manifest, payload []byte, expected string) error {
	if manifest.SchemaVersion != SchemaVersion || manifest.Coordinate.Kind != "skill" ||
		manifest.Coordinate.Marketplace == "" || manifest.Coordinate.Source == "" || manifest.Coordinate.Name == "" ||
		!validDigest(manifest.RevisionDigest) || manifest.PrimaryPath != "SKILL.md" || len(manifest.Files) == 0 {
		return fmt.Errorf("invalid inspection manifest")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	primary := false
	for _, file := range manifest.Files {
		if file.Path == "SKILL.md" {
			if primary || file.MediaType != MediaMarkdown || file.InspectionClass != ClassText {
				return fmt.Errorf("invalid inspection primary file")
			}
			primary = true
		} else {
			if !strings.HasPrefix(file.Path, "support/") || file.Path == "support/" {
				return fmt.Errorf("invalid inspection file path %q", file.Path)
			}
			if canonical, err := canonicalSupportPath(strings.TrimPrefix(file.Path, "support/")); err != nil || canonical != file.Path {
				return fmt.Errorf("invalid inspection file path %q", file.Path)
			}
		}
		if _, exists := seen[file.Path]; exists || !validDigest(file.ContentDigest) || file.Size < 0 || !AllowedMediaType(file.MediaType, file.InspectionClass) {
			return fmt.Errorf("invalid inspection file %q", file.Path)
		}
		seen[file.Path] = struct{}{}
	}
	if !primary {
		return fmt.Errorf("inspection manifest is missing primary file")
	}
	for _, finding := range manifest.Findings {
		if finding.Code == "" || finding.Severity == "" || finding.StartByte < 0 || finding.EndByte < 0 || finding.StartByte > finding.EndByte {
			return fmt.Errorf("invalid inspection finding")
		}
		if finding.Path != "" {
			if _, exists := seen[finding.Path]; !exists {
				return fmt.Errorf("inspection finding references unknown path %q", finding.Path)
			}
		}
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, payload) {
		return fmt.Errorf("inspection manifest is not canonical JSON")
	}
	got := digest(payload)
	if expected != "" && got != expected {
		return fmt.Errorf("inspection manifest digest mismatch")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, c := range value[len("sha256:"):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// AllowedMediaType is the single media policy shared by manifest validation
// and HTTP content serving.
func AllowedMediaType(mediaType, class string) bool {
	switch class {
	case ClassText:
		return mediaType == MediaMarkdown || mediaType == "text/plain" || mediaType == "application/json" || mediaType == "application/yaml"
	case ClassImage:
		return mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/gif" || mediaType == "image/webp"
	case ClassMetadata:
		return mediaType == "application/octet-stream"
	default:
		return false
	}
}
