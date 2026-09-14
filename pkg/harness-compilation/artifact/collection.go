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
	"strings"
	"unicode"

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
	Leaves           []CollectionMemberEntry `json:"leaves,omitempty"`
	ContentDigest    string                  `json:"content_digest"`
	Provenance       CanonicalProvenance     `json:"provenance"`
	SupportedSchemas []string                `json:"supported_schema_versions"`
}

// CollectionMemberEntry describes a resource that is part of a collection.
type CollectionMemberEntry struct {
	Kind           string     `json:"kind"`
	Name           string     `json:"name"`
	Optional       bool       `json:"optional,omitempty"`
	RegistryID     string     `json:"registry_id,omitempty"`
	Revision       string     `json:"revision,omitempty"`
	InclusionPaths [][]string `json:"inclusion_paths,omitempty"`
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
	sortMembers := func(members []CollectionMemberEntry) []CollectionMemberEntry {
		if len(members) == 0 {
			return nil
		}
		out := append([]CollectionMemberEntry(nil), members...)
		for i := range out {
			if len(out[i].InclusionPaths) > 0 {
				out[i].InclusionPaths = append([][]string(nil), out[i].InclusionPaths...)
				sort.Slice(out[i].InclusionPaths, func(a, b int) bool {
					return strings.Join(out[i].InclusionPaths[a], "\x00") < strings.Join(out[i].InclusionPaths[b], "\x00")
				})
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Kind != out[j].Kind {
				return out[i].Kind < out[j].Kind
			}
			if out[i].Name != out[j].Name {
				return out[i].Name < out[j].Name
			}
			if out[i].RegistryID != out[j].RegistryID {
				return out[i].RegistryID < out[j].RegistryID
			}
			return out[i].Revision < out[j].Revision
		})
		return out
	}
	cm.Members = sortMembers(cm.Members)
	cm.Leaves = sortMembers(cm.Leaves)
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
	if err := m.Validate(); err != nil {
		return fmt.Errorf("validate collection manifest: %w", err)
	}
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
		if err := decoded.Validate(); err != nil {
			return fmt.Errorf("validate provided collection manifest: %w", err)
		}
		if decoded.ContentDigest != m.ContentDigest {
			return errors.New("provided payload content_digest does not match manifest")
		}
		canonical, err := canonicalCollectionPayload(m)
		if err != nil {
			return fmt.Errorf("canonical manifest payload: %w", err)
		}
		if !bytes.Equal(payload, canonical) {
			return errors.New("provided payload is not canonical")
		}
	}

	return nil
}

// ValidateFlatMirrorManifest verifies the subset published by the first
// Marketplace collection slice: a standalone manifest containing only exact,
// direct mirror leaves. Nested collections and mixed acquisition are deliberate
// follow-up features and must not be accepted accidentally here.
func ValidateFlatMirrorManifest(m CollectionManifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if len(m.Includes) != 0 {
		return errors.New("flat collection manifest cannot contain includes")
	}
	if len(m.Members) == 0 {
		return errors.New("flat collection manifest must contain direct leaves")
	}
	for i, member := range m.Members {
		if member.Kind == "collection" {
			return fmt.Errorf("members[%d] contains a nested collection", i)
		}
		if err := validateCollectionLeaf(member, i); err != nil {
			return errors.New(strings.Replace(err.Error(), "leaves[", "members[", 1))
		}
	}
	if len(m.Leaves) > 0 {
		if len(m.Leaves) != len(m.Members) {
			return errors.New("flat collection manifest leaves do not match members")
		}
		for i, leaf := range m.Leaves {
			if err := validateCollectionLeaf(leaf, i); err != nil {
				return err
			}
		}
	}
	return nil
}

func validCollectionToken(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || value == "." || value == ".." || strings.ContainsAny(value, "/\\%") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func ValidateCollectionManifest(m CollectionManifest) error { return m.Validate() }

// ValidateAndExpandCollectionManifestGraph validates a collection manifest and
// its nested manifests, then derives the exact flattened leaf set from the
// direct member/include graph. The nested map is keyed by collection manifest
// content digest. A flat manifest may omit Leaves for backwards compatibility;
// once a nested member or include exists, Leaves is required and must exactly
// match the graph-derived set and inclusion paths.
//
// The returned leaves are the graph-derived leaves, not the caller-provided
// flattened list. Callers should use them only after this function succeeds.
func ValidateAndExpandCollectionManifestGraph(root CollectionManifest, nested map[string]CollectionManifest) ([]CollectionMemberEntry, error) {
	if err := root.Validate(); err != nil {
		return nil, err
	}
	leaves, err := expandCollectionManifestGraph(root, nested, make(map[collectionManifestIdentity]bool))
	if err != nil {
		return nil, err
	}
	if err := validateFlattenedCollectionLeaves(root, leaves); err != nil {
		return nil, err
	}
	return leaves, nil
}

type collectionManifestIdentity struct {
	contentDigest string
	name          string
	registryURL   string
	ref           string
	path          string
}

// collectionManifestKey identifies one exact collection revision while walking
// a graph. Names are display labels, not revision identities: two revisions
// may intentionally use the same name. ContentDigest is the primary identity;
// provenance and name keep malformed or legacy manifests with an empty digest
// from collapsing distinct nodes.
func collectionManifestKey(manifest CollectionManifest) collectionManifestIdentity {
	return collectionManifestIdentity{
		contentDigest: manifest.ContentDigest,
		name:          manifest.Name,
		registryURL:   manifest.Provenance.RegistryURL,
		ref:           manifest.Provenance.Ref,
		path:          manifest.Provenance.Path,
	}
}

func expandCollectionManifestGraph(manifest CollectionManifest, nested map[string]CollectionManifest, visiting map[collectionManifestIdentity]bool) ([]CollectionMemberEntry, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	key := collectionManifestKey(manifest)
	if visiting[key] {
		return nil, fmt.Errorf("collection graph cycle detected at %q", manifest.Name)
	}
	visiting[key] = true
	defer delete(visiting, key)

	byKey := make(map[string]int)
	leaves := make([]CollectionMemberEntry, 0, len(manifest.Members))
	addLeaf := func(leaf CollectionMemberEntry, paths [][]string) error {
		key := collectionLeafKey(leaf)
		if index, ok := byKey[key]; ok {
			if leaves[index].Revision != leaf.Revision || leaves[index].Optional != leaf.Optional {
				return fmt.Errorf("collection %q contains conflicting leaves for %s/%q from registry %q", manifest.Name, leaf.Kind, leaf.Name, leaf.RegistryID)
			}
			leaves[index].InclusionPaths = appendUniqueCollectionPaths(leaves[index].InclusionPaths, paths...)
			return nil
		}
		leaf.InclusionPaths = appendUniqueCollectionPaths(nil, paths...)
		byKey[key] = len(leaves)
		leaves = append(leaves, leaf)
		return nil
	}

	for i, member := range manifest.Members {
		if member.Kind != "collection" {
			if err := validateCollectionLeaf(member, i); err != nil {
				return nil, fmt.Errorf("collection %q direct member: %s", manifest.Name, strings.Replace(err.Error(), "leaves[", "members[", 1))
			}
			if err := addLeaf(member, [][]string{{manifest.Name}}); err != nil {
				return nil, err
			}
			continue
		}
		if !validCollectionToken(member.Name) || !validCollectionToken(member.RegistryID) || !validCollectionDigest(member.Revision) {
			return nil, fmt.Errorf("collection %q nested member %q must have a valid registry_id and revision", manifest.Name, member.Name)
		}
		child, ok := nested[member.Revision]
		if !ok {
			return nil, fmt.Errorf("collection %q nested member %q manifest %q is missing", manifest.Name, member.Name, member.Revision)
		}
		if child.Name != member.Name {
			return nil, fmt.Errorf("collection %q nested member name mismatch: got %q, want %q", manifest.Name, child.Name, member.Name)
		}
		if child.ContentDigest != member.Revision {
			return nil, fmt.Errorf("collection %q nested member %q manifest digest mismatch: got %q, want %q", manifest.Name, member.Name, child.ContentDigest, member.Revision)
		}
		childLeaves, err := expandCollectionManifestGraph(child, nested, visiting)
		if err != nil {
			return nil, err
		}
		for _, childLeaf := range childLeaves {
			paths := make([][]string, 0, len(childLeaf.InclusionPaths))
			for _, path := range childLeaf.InclusionPaths {
				paths = append(paths, append([]string{manifest.Name}, path...))
			}
			if err := addLeaf(childLeaf, paths); err != nil {
				return nil, err
			}
		}
	}

	for _, include := range manifest.Includes {
		found := false
		for _, member := range manifest.Members {
			if member.Kind == "collection" && member.Name == include {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("collection %q include %q is not a direct nested member", manifest.Name, include)
		}
	}
	if err := validateFlattenedCollectionLeaves(manifest, leaves); err != nil {
		return nil, err
	}
	return leaves, nil
}

func validateFlattenedCollectionLeaves(manifest CollectionManifest, expected []CollectionMemberEntry) error {
	hasNestedGraph := len(manifest.Includes) > 0
	for _, member := range manifest.Members {
		if member.Kind == "collection" {
			hasNestedGraph = true
			break
		}
	}
	if hasNestedGraph && len(manifest.Leaves) == 0 {
		return fmt.Errorf("collection %q must contain flattened leaves when nested members or includes exist", manifest.Name)
	}
	if len(manifest.Leaves) == 0 {
		return nil
	}
	actual := make(map[string]CollectionMemberEntry, len(manifest.Leaves))
	for _, leaf := range manifest.Leaves {
		key := collectionLeafKey(leaf)
		if previous, ok := actual[key]; ok {
			if previous.Revision != leaf.Revision {
				return fmt.Errorf("collection %q flattened leaves contain conflicting revisions of %s/%q from registry %q", manifest.Name, leaf.Kind, leaf.Name, leaf.RegistryID)
			}
			return fmt.Errorf("collection %q flattened leaves contain duplicate %s/%q from registry %q", manifest.Name, leaf.Kind, leaf.Name, leaf.RegistryID)
		}
		actual[key] = leaf
	}
	expectedByKey := make(map[string]CollectionMemberEntry, len(expected))
	for _, leaf := range expected {
		expectedByKey[collectionLeafKey(leaf)] = leaf
	}
	for key, leaf := range expectedByKey {
		got, ok := actual[key]
		if !ok {
			return fmt.Errorf("collection %q flattened leaves are missing %s/%q from registry %q", manifest.Name, leaf.Kind, leaf.Name, leaf.RegistryID)
		}
		if got.Revision != leaf.Revision || got.Optional != leaf.Optional {
			return fmt.Errorf("collection %q flattened leaf %s/%q from registry %q has conflicting metadata", manifest.Name, leaf.Kind, leaf.Name, leaf.RegistryID)
		}
		if !sameCollectionPaths(got.InclusionPaths, leaf.InclusionPaths) {
			return fmt.Errorf("collection %q flattened leaf %s/%q has inclusion paths that do not follow the nested graph", manifest.Name, leaf.Kind, leaf.Name)
		}
		delete(actual, key)
	}
	for _, leaf := range actual {
		return fmt.Errorf("collection %q flattened leaves contain extra %s/%q from registry %q", manifest.Name, leaf.Kind, leaf.Name, leaf.RegistryID)
	}
	return nil
}

func collectionLeafKey(leaf CollectionMemberEntry) string {
	return leaf.Kind + "\x00" + leaf.Name + "\x00" + leaf.RegistryID
}

func appendUniqueCollectionPaths(paths [][]string, add ...[]string) [][]string {
	for _, candidate := range add {
		duplicate := false
		for _, existing := range paths {
			if slicesEqual(existing, candidate) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			paths = append(paths, append([]string(nil), candidate...))
		}
	}
	return paths
}

func sameCollectionPaths(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, path := range a {
		seen[strings.Join(path, "\x00")] = struct{}{}
	}
	for _, path := range b {
		if _, ok := seen[strings.Join(path, "\x00")]; !ok {
			return false
		}
	}
	return true
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m CollectionManifest) Validate() error {
	if m.SchemaVersion != CollectionSchemaV1 {
		return fmt.Errorf("unsupported collection schema version %q", m.SchemaVersion)
	}
	if !validCollectionToken(m.Name) {
		return errors.New("collection name is required and must be a route-safe token")
	}
	if len(m.Members) == 0 && len(m.Includes) == 0 && len(m.Leaves) == 0 {
		return errors.New("collection must have members, includes, or leaves")
	}
	seen := make(map[string]bool)
	for i, mem := range m.Members {
		if !isCollectionKind(mem.Kind) {
			return fmt.Errorf("members[%d].kind %q is not a valid collection member kind", i, mem.Kind)
		}
		if !validCollectionToken(mem.Name) {
			return fmt.Errorf("members[%d].name must be a route-safe token", i)
		}
		key := mem.Kind + "/" + mem.Name
		if seen[key] {
			return fmt.Errorf("duplicate collection member %q", key)
		}
		seen[key] = true
	}
	seenIncludes := make(map[string]bool)
	for i, name := range m.Includes {
		if !validCollectionToken(name) {
			return fmt.Errorf("includes[%d] must be a route-safe token", i)
		}
		if seenIncludes[name] {
			return fmt.Errorf("duplicate collection include %q", name)
		}
		seenIncludes[name] = true
	}
	seenLeaves := make(map[string]bool)
	for i, leaf := range m.Leaves {
		if err := validateCollectionLeaf(leaf, i); err != nil {
			return err
		}
		key := leaf.Kind + "/" + leaf.Name + "/" + leaf.RegistryID
		if seenLeaves[key] {
			return fmt.Errorf("duplicate collection leaf %q", key)
		}
		seenLeaves[key] = true
		if len(leaf.InclusionPaths) == 0 {
			return fmt.Errorf("leaves[%d].inclusion_paths is required", i)
		}
		seenPaths := make(map[string]bool)
		for pathIndex, path := range leaf.InclusionPaths {
			if len(path) == 0 {
				return fmt.Errorf("leaves[%d].inclusion_paths[%d] is empty", i, pathIndex)
			}
			if path[0] != m.Name {
				return fmt.Errorf("leaves[%d].inclusion_paths[%d] must start with collection %q", i, pathIndex, m.Name)
			}
			for segmentIndex, segment := range path {
				if !validCollectionToken(segment) {
					return fmt.Errorf("leaves[%d].inclusion_paths[%d][%d] is not a route-safe token", i, pathIndex, segmentIndex)
				}
			}
			pathKey := strings.Join(path, "\x00")
			if seenPaths[pathKey] {
				return fmt.Errorf("duplicate inclusion path for collection leaf %q", key)
			}
			seenPaths[pathKey] = true
		}
	}
	return nil
}

func validateCollectionLeaf(leaf CollectionMemberEntry, index int) error {
	if !isMaterializedCollectionKind(leaf.Kind) {
		return fmt.Errorf("leaves[%d].kind %q is not a materialized collection member kind", index, leaf.Kind)
	}
	if !validCollectionToken(leaf.Name) {
		return fmt.Errorf("leaves[%d].name must be a route-safe token", index)
	}
	if !validCollectionToken(leaf.RegistryID) {
		return fmt.Errorf("leaves[%d].registry_id must be a route-safe token", index)
	}
	if !validCollectionDigest(leaf.Revision) {
		return fmt.Errorf("leaves[%d].revision must be sha256:<64 lowercase hex>", index)
	}
	return nil
}

func validCollectionDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, ch := range value[len("sha256:"):] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func isMaterializedCollectionKind(kind string) bool {
	return kind == "skill" || kind == "command" || kind == "subagent" || kind == "hook" || kind == "mcpserver" || kind == "rule"
}

func isCollectionKind(kind string) bool {
	switch kind {
	case "skill", "command", "subagent", "hook", "mcpserver", "rule", "collection":
		return true
	}
	return false
}

// CollectionFromIR builds a CollectionManifest skeleton from an IR Collection.
// The caller is responsible for filling provenance and registry identifiers.
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
		case ir.KindRule:
			kind = "rule"
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
