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

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const SchemaVersion = "v1"

// MaxManifestBytes bounds the serialized inspection projection independently
// from the repository and individual-file limits. A manifest contains
// attacker-controlled paths, so content limits alone are not sufficient to
// bound its response size.
const MaxManifestBytes int64 = 1 * 1024 * 1024

const (
	ClassText  = "text"
	ClassImage = "image"
	// ClassMetadata is used for bytes whose identity is inspectable but whose
	// body must not be rendered (for example executables and SVG).
	ClassMetadata = "metadata"

	MediaMarkdown = "text/markdown"
	MediaPlain    = "text/plain"
	MediaJSON     = "application/json"
	MediaYAML     = "application/yaml"
	MediaPNG      = "image/png"
	MediaJPEG     = "image/jpeg"
	MediaGIF      = "image/gif"
	MediaWEBP     = "image/webp"
	MediaMetadata = "application/octet-stream"
)

var PublicMediaTypes = []string{
	MediaMarkdown, MediaPlain, MediaJSON, MediaYAML,
	MediaPNG, MediaJPEG, MediaGIF, MediaWEBP,
}

// Canonical primary paths are stable projection paths, not source-layout paths.
// Keeping them independent of the source prevents path disclosure and makes an
// inspection digest portable across acquisition modes.
const (
	PrimarySkillPath      = "SKILL.md"
	PrimaryCommandPath    = "COMMAND.md"
	PrimarySubagentPath   = "AGENT.md"
	PrimaryHookPath       = "HOOK.md"
	PrimaryMCPServerPath  = ".mcp.json"
	PrimaryCollectionPath = "COLLECTION.json"
)

func PrimaryPath(kind string) string {
	switch kind {
	case "skill":
		return PrimarySkillPath
	case "command":
		return PrimaryCommandPath
	case "subagent":
		return PrimarySubagentPath
	case "hook":
		return PrimaryHookPath
	case "mcpserver":
		return PrimaryMCPServerPath
	case "collection":
		return PrimaryCollectionPath
	default:
		return ""
	}
}

type File struct {
	Path            string `json:"path"`
	ContentDigest   string `json:"content_digest"`
	MediaType       string `json:"media_type"`
	Size            int64  `json:"size"`
	InspectionClass string `json:"inspection_class"`
}

type Manifest struct {
	SchemaVersion  string                   `json:"schema_version"`
	Coordinate     store.ResourceCoordinate `json:"coordinate"`
	RevisionDigest string                   `json:"revision_digest"`
	PrimaryPath    string                   `json:"primary_path"`
	Files          []File                   `json:"files"`
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
func Build(coordinate store.ResourceCoordinate, revision string, body []byte, support map[string][]byte, maxFiles int, maxFileBytes, maxTotalBytes int64, manifestLimits ...int64) (Projection, error) {
	primaryPath := PrimaryPath(coordinate.Kind)
	if primaryPath == "" {
		return Projection{}, fmt.Errorf("unsupported inspection resource kind %q", coordinate.Kind)
	}
	if coordinate.Name == "" {
		return Projection{}, fmt.Errorf("inspection resource name must not be empty")
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
	seenPaths := map[string]struct{}{primaryPath: {}}
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

	if maxTotalBytes > 0 && int64(len(body)) > maxTotalBytes {
		return Projection{}, fmt.Errorf("inspection content exceeds total limit %d", maxTotalBytes)
	}
	primaryMedia, primaryClass, primaryExpose := mediaPolicy(primaryPath, body)
	expectedMedia, expectedClass := primaryMediaPolicy(coordinate.Kind)
	if primaryMedia != expectedMedia || primaryClass != expectedClass {
		return Projection{}, fmt.Errorf("primary content for %s does not match canonical media policy", coordinate.Kind)
	}
	primaryDigest := digest(body)
	files = append(files, File{Path: primaryPath, ContentDigest: primaryDigest, MediaType: primaryMedia, Size: int64(len(body)), InspectionClass: primaryClass})
	if primaryExpose {
		bodies[primaryDigest] = append([]byte(nil), body...)
	}

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

	out := Manifest{SchemaVersion: SchemaVersion, Coordinate: coordinate, RevisionDigest: revision, PrimaryPath: primaryPath, Files: files}
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
	switch name {
	case PrimarySkillPath, PrimaryCommandPath, PrimarySubagentPath, PrimaryHookPath, PrimaryMCPServerPath, PrimaryCollectionPath:
		return "", fmt.Errorf("support path %q shadows primary document", original)
	}
	return path.Join("support", name), nil
}

func mediaPolicy(name string, data []byte) (string, string, bool) {
	ext := strings.ToLower(path.Ext(name))
	if media := http.DetectContentType(data); media == MediaPNG || media == MediaJPEG || media == MediaGIF || media == MediaWEBP {
		return media, ClassImage, true
	}
	if isUnsafeExtension(ext) || (looksUnsafeText(data) && !isPrimaryMarkdownPath(name)) {
		return MediaMetadata, ClassMetadata, false
	}
	if isPrimaryMarkdownPath(name) && !IsValidText(data) {
		return MediaMetadata, ClassMetadata, false
	}
	if !utf8.Valid(data) {
		return MediaMetadata, ClassMetadata, false
	}
	switch ext {
	case ".md", ".markdown":
		return MediaMarkdown, ClassText, true
	case ".json":
		if json.Valid(data) {
			return MediaJSON, ClassText, true
		}
		return MediaMetadata, ClassMetadata, false
	case ".yaml", ".yml", ".txt":
		return map[string]string{".yaml": MediaYAML, ".yml": MediaYAML, ".txt": MediaPlain}[ext], ClassText, true
	default:
		// A textual file with an unrecognised extension is still safe to serve as
		// plain text, while binary and executable formats remain metadata-only.
		return MediaPlain, ClassText, true
	}
}

func primaryMediaPolicy(kind string) (string, string) {
	switch kind {
	case "skill", "command", "subagent", "hook":
		return MediaMarkdown, ClassText
	case "mcpserver", "collection":
		return MediaJSON, ClassText
	default:
		return "", ""
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

func isPrimaryMarkdownPath(name string) bool {
	switch name {
	case PrimarySkillPath, PrimaryCommandPath, PrimarySubagentPath, PrimaryHookPath:
		return true
	default:
		return false
	}
}

// IsValidText reports whether bytes are valid, non-control UTF-8 text. It is
// the narrow byte-safety policy for canonical Markdown, which is still passed
// through the Nuxt renderer and sanitizer before it is inserted into HTML.
func IsValidText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 || (b < 0x09) || (b > 0x0d && b < 0x20) || b == 0x7f {
			return false
		}
	}
	return true
}

// IsSafeText reports whether bytes are valid, non-control UTF-8 text that is
// not an active HTML, script, stylesheet, XML, or SVG payload.
func IsSafeText(data []byte) bool { return IsValidText(data) && !looksUnsafeTextLexical(data) }

// looksUnsafeText protects direct content consumers when a dangerous payload is
// given a harmless-looking filename or no filename at all.
func looksUnsafeText(data []byte) bool {
	return !IsValidText(data) || looksUnsafeTextLexical(data)
}

func looksUnsafeTextLexical(data []byte) bool {
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
		strings.HasPrefix(text, "data:") || strings.HasPrefix(text, "vbscript:") ||
		strings.HasPrefix(text, "function ") || strings.HasPrefix(text, "async function ") ||
		strings.HasPrefix(text, "const ") || strings.HasPrefix(text, "let ") ||
		strings.HasPrefix(text, "var ") || strings.HasPrefix(text, "import ") ||
		strings.HasPrefix(text, "export ") || strings.HasPrefix(text, "console.") ||
		strings.HasPrefix(text, "window.") || strings.HasPrefix(text, "document.") ||
		strings.HasPrefix(text, "alert(") ||
		(strings.Contains(text, "{") && strings.Contains(text, "}") && strings.Contains(text, ":") && !json.Valid(data))
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
	if manifest.SchemaVersion != SchemaVersion || PrimaryPath(manifest.Coordinate.Kind) == "" ||
		manifest.Coordinate.Marketplace == "" || manifest.Coordinate.Registry == "" || manifest.Coordinate.Name == "" ||
		!validDigest(manifest.RevisionDigest) || manifest.PrimaryPath != PrimaryPath(manifest.Coordinate.Kind) || len(manifest.Files) == 0 {
		return fmt.Errorf("invalid inspection manifest")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	primary := false
	for _, file := range manifest.Files {
		if file.Path == manifest.PrimaryPath {
			if primary || !AllowedMediaType(file.MediaType, file.InspectionClass) {
				return fmt.Errorf("invalid inspection primary file")
			}
			expectedMedia, expectedClass := primaryMediaPolicy(manifest.Coordinate.Kind)
			if file.MediaType != expectedMedia || file.InspectionClass != expectedClass {
				return fmt.Errorf("invalid inspection primary media policy")
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
		return mediaType == MediaMarkdown || mediaType == MediaPlain || mediaType == MediaJSON || mediaType == MediaYAML
	case ClassImage:
		return mediaType == MediaPNG || mediaType == MediaJPEG || mediaType == MediaGIF || mediaType == MediaWEBP
	case ClassMetadata:
		return mediaType == MediaMetadata
	default:
		return false
	}
}

func VerifyFileContent(file File, data []byte) error {
	if !AllowedMediaType(file.MediaType, file.InspectionClass) {
		return fmt.Errorf("media type %q is not allowed for class %q", file.MediaType, file.InspectionClass)
	}
	expectedMedia, expectedClass, _ := mediaPolicy(file.Path, data)
	if file.MediaType != expectedMedia || file.InspectionClass != expectedClass {
		return fmt.Errorf("content media policy does not match manifest")
	}
	if int64(len(data)) != file.Size {
		return fmt.Errorf("content size does not match manifest")
	}
	if digest(data) != file.ContentDigest {
		return fmt.Errorf("content digest does not match manifest")
	}
	switch file.InspectionClass {
	case ClassImage:
		if http.DetectContentType(data) != file.MediaType {
			return fmt.Errorf("content media type does not match manifest")
		}
	case ClassText:
		if file.MediaType == MediaMarkdown {
			if !IsValidText(data) {
				return fmt.Errorf("markdown content is not valid text")
			}
		} else {
			if !IsSafeText(data) || (file.MediaType == MediaJSON && !json.Valid(data)) {
				return fmt.Errorf("text content does not match manifest policy")
			}
		}
	}
	return nil
}
