package lockfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func Decode(data []byte) (Document, error) {
	if int64(len(data)) > MaxFileSize {
		return Document{}, fmt.Errorf("%w: lock input exceeds %d bytes", ErrFileTooLarge, MaxFileSize)
	}
	if containsConflictMarkers(data) {
		return Document{}, coded(CodeConflictMarkers, "Git conflict markers are forbidden")
	}
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil {
		return Document{}, coded(CodeMalformedYAML, "decode YAML: %v", err)
	}
	if len(root.Content) == 0 {
		return Document{}, coded(CodeMalformedYAML, "empty document")
	}
	nodes := 0
	if err := inspectYAML(root.Content[0], 1, &nodes); err != nil {
		return Document{}, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Document{}, coded(CodeMultipleDocuments, "exactly one YAML document is required")
	}

	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var doc Document
	if err := strict.Decode(&doc); err != nil {
		code := CodeMalformedYAML
		if strings.Contains(err.Error(), "field ") && strings.Contains(err.Error(), "not found") {
			code = CodeUnknownField
		}
		return Document{}, coded(code, "decode lock: %v", err)
	}
	return Normalize(doc)
}

func DecodeInto(data []byte, dst *Document) error {
	if dst == nil {
		return coded(CodeInvalidDocument, "nil decode destination")
	}
	doc, err := Decode(data)
	if err != nil {
		return err
	}
	*dst = doc
	return nil
}

func DecodeReader(r io.Reader) (Document, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return Document{}, fmt.Errorf("read lock: %w", err)
	}
	return Decode(data)
}

func Encode(doc Document) ([]byte, error) {
	normalized, err := Normalize(doc)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(normalized); err != nil {
		return nil, fmt.Errorf("encode lock: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("finish lock encoding: %w", err)
	}
	if int64(out.Len()) > MaxFileSize {
		return nil, fmt.Errorf("%w: encoded lock exceeds %d bytes", ErrFileTooLarge, MaxFileSize)
	}
	return out.Bytes(), nil
}

func Normalize(input Document) (Document, error) {
	doc := cloneDocument(input)
	switch doc.APIVersion {
	case APIVersionV1:
		if len(doc.Marketplaces) > 0 {
			return Document{}, coded(CodeUnsupportedVersion, "apiVersion %q cannot contain Marketplace records; use %q", doc.APIVersion, APIVersionV2)
		}
	case APIVersionV2, APIVersionV3:
		if len(doc.Marketplaces) == 0 {
			return Document{}, coded(CodeInvalidDocument, "apiVersion %q requires at least one Marketplace record", doc.APIVersion)
		}
	default:
		return Document{}, coded(CodeUnsupportedVersion, "unsupported apiVersion %q", doc.APIVersion)
	}
	if !doc.Scope.Valid() {
		return Document{}, coded(CodeInvalidScope, "invalid lock scope %q", doc.Scope)
	}
	for i := range doc.Registries {
		registry := &doc.Registries[i]
		canonical, err := CanonicalGitURL(registry.URL)
		if err != nil {
			return Document{}, fmt.Errorf("registries[%d].url: %w", i, err)
		}
		registry.URL = canonical
		if err := require(registry.Ref, fmt.Sprintf("registries[%d].ref", i)); err != nil {
			return Document{}, err
		}
		if _, err := ParseGitOID(registry.Commit.String()); err != nil {
			return Document{}, fmt.Errorf("registries[%d].commit: %w", i, err)
		}
		for j := range registry.Roots {
			root := &registry.Roots[j]
			root.Path, err = normalizeArtifactPath(root.Path, true)
			if err != nil {
				return Document{}, fmt.Errorf("registries[%d].roots[%d].path: %w", i, j, err)
			}
			if err := require(root.SourceHarness, "source_harness"); err != nil {
				return Document{}, err
			}
			if _, err := RevisionContract(root.RevisionSchema); err != nil {
				return Document{}, err
			}
			if err := validateDigest(root.RevisionDigest, "revision_digest"); err != nil {
				return Document{}, err
			}
			for k := range root.Resources {
				resource := &root.Resources[k]
				if err := require(resource.Kind, "resource kind"); err != nil {
					return Document{}, err
				}
				if err := require(resource.Name, "resource name"); err != nil {
					return Document{}, err
				}
				if err := validateDigest(resource.ContentDigest, "content_digest"); err != nil {
					return Document{}, err
				}
				if err := validateDigest(resource.ArchiveDigest, "archive_digest"); err != nil {
					return Document{}, err
				}
				cleanSupport := make(map[string]Digest, len(resource.SupportFiles))
				for filename, digest := range resource.SupportFiles {
					clean, err := normalizeArtifactPath(filename, false)
					if err != nil {
						return Document{}, err
					}
					if err := validateDigest(digest, "support file digest"); err != nil {
						return Document{}, err
					}
					if prior, exists := cleanSupport[clean]; exists && prior != digest {
						return Document{}, coded(CodeIdentityConflict, "support file %q has conflicting digests", clean)
					}
					cleanSupport[clean] = digest
				}
				resource.SupportFiles = cleanSupport
			}
			sort.Slice(root.Resources, func(a, b int) bool {
				x, y := root.Resources[a], root.Resources[b]
				if x.Kind != y.Kind {
					return x.Kind < y.Kind
				}
				return x.Name < y.Name
			})
			var err error
			root.Resources, err = dedupeGitResources(root.Resources)
			if err != nil {
				return Document{}, err
			}
		}
		sort.Slice(registry.Roots, func(a, b int) bool {
			x, y := registry.Roots[a], registry.Roots[b]
			if x.Path != y.Path {
				return x.Path < y.Path
			}
			return x.SourceHarness < y.SourceHarness
		})
		registry.Roots, err = dedupeRoots(registry.Roots)
		if err != nil {
			return Document{}, err
		}
	}
	sort.Slice(doc.Registries, func(i, j int) bool {
		a, b := doc.Registries[i], doc.Registries[j]
		if a.URL != b.URL {
			return a.URL < b.URL
		}
		return a.Ref < b.Ref
	})
	var err error
	doc.Registries, err = dedupeRegistries(doc.Registries)
	if err != nil {
		return Document{}, err
	}
	if err := normalizeMarketplaces(&doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func Validate(doc Document) error {
	_, err := Normalize(doc)
	return err
}

func ValidateScope(doc Document, expected Scope) error {
	if !expected.Valid() {
		return coded(CodeInvalidScope, "invalid expected scope %q", expected)
	}
	if doc.Scope != expected {
		return coded(CodeScopeMismatch, "lock scope %q does not match %q", doc.Scope, expected)
	}
	return nil
}

const (
	maxYAMLDepth = 100
	maxYAMLNodes = 100000
)

func inspectYAML(node *yaml.Node, depth int, nodes *int) error {
	*nodes++
	if depth > maxYAMLDepth || *nodes > maxYAMLNodes {
		return coded(CodeInvalidDocument, "lock YAML exceeds complexity limits")
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return coded(CodeYAMLAlias, "YAML aliases and anchors are forbidden")
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value == "<<" || key.Tag == "!!merge" {
				return coded(CodeYAMLMerge, "YAML merge keys are forbidden")
			}
			signature := key.Tag + "\x00" + key.Value
			if _, ok := seen[signature]; ok {
				return coded(CodeDuplicateField, "duplicate mapping key %q", key.Value)
			}
			seen[signature] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := inspectYAML(child, depth+1, nodes); err != nil {
			return err
		}
	}
	return nil
}

func containsConflictMarkers(data []byte) bool {
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("<<<<<<<")) || bytes.Equal(trimmed, []byte("=======")) || bytes.HasPrefix(trimmed, []byte(">>>>>>>")) {
			return true
		}
	}
	return false
}

func normalizeArtifactPath(value string, allowRoot bool) (string, error) {
	if value == "" || strings.Contains(value, `\`) || hasControl(value) || strings.HasPrefix(value, "/") {
		return "", coded(CodeInvalidPath, "invalid portable path %q", value)
	}
	for part := range strings.SplitSeq(value, "/") {
		if part == ".." {
			return "", coded(CodeInvalidPath, "path traversal in %q", value)
		}
	}
	clean := path.Clean(value)
	if clean == "." && !allowRoot {
		return "", coded(CodeInvalidPath, "empty artifact path")
	}
	return clean, nil
}
