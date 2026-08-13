package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
)

// MergeFragments merges artifact fragments that share the same Path and
// FragmentKey. Non-fragment artifacts are passed through unchanged.
//
// JSON fragments: each fragment's Bytes are parsed as a JSON object; maps are
// merged (later entries override earlier ones for matching keys), and the
// result is re-serialized.
//
// Text fragments: fragments are concatenated with newlines.
//
// Returns an error if fragments with the same path have incompatible merge
// strategies (mixed JSON and text).
func MergeFragments(artifacts []Artifact) ([]Artifact, error) {
	fragmentGroups := make(map[string][]Artifact) // key = Path
	var result []Artifact

	for _, a := range artifacts {
		if !a.Fragment {
			result = append(result, a)
			continue
		}
		fragmentGroups[a.Path] = append(fragmentGroups[a.Path], a)
	}

	for path, fragments := range fragmentGroups {
		fragmentKey := fragments[0].FragmentKey
		for _, fragment := range fragments[1:] {
			if fragment.FragmentKey != fragmentKey {
				return nil, fmt.Errorf("merging fragments for %q: fragments have different FragmentKey values %q and %q", path, fragmentKey, fragment.FragmentKey)
			}
		}
		merged, err := mergeGroup(path, fragments)
		if err != nil {
			return nil, fmt.Errorf("merging fragments for %q: %w", path, err)
		}
		result = append(result, merged)
	}

	// Sort by path for deterministic output.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})

	return result, nil
}

// mergeGroup merges a set of fragments that share the same output Path.
func mergeGroup(path string, fragments []Artifact) (Artifact, error) {
	if len(fragments) == 0 {
		return Artifact{}, errors.New("empty fragment group")
	}

	// Every fragment must use the same strategy. Treat only JSON objects as
	// JSON fragments; malformed JSON, arrays, scalars, and empty fragments are
	// text. In particular, never silently concatenate JSON and text.
	//
	// Classification and duplicate-key validation share a single token pass:
	// fragments are produced internally by json.Marshal and cannot contain
	// duplicate keys, so the check is defensive only and must not cost an extra
	// full decode per fragment on the Materialize path.
	jsonFragments := 0
	textFragments := 0
	for _, f := range fragments {
		isObject, err := jsonutil.ObjectWithUniqueKeys(f.Bytes)
		if err != nil {
			return Artifact{}, jsonutil.Coded(path, f.Bytes, err)
		}
		if isObject {
			jsonFragments++
		} else {
			textFragments++
		}
	}
	if jsonFragments > 0 && textFragments > 0 {
		return Artifact{}, errors.New("cannot mix JSON and text fragments")
	}
	if jsonFragments > 0 {
		return mergeJSON(path, fragments)
	}
	return mergeText(path, fragments)
}

// mergeJSON merges JSON object fragments by deep-merging their maps.
// Nested maps are merged recursively; non-map values from later fragments
// override earlier ones.
func mergeJSON(path string, fragments []Artifact) (Artifact, error) {
	merged := make(map[string]any)
	// The flag is a property of the fragment group, not of one fragment: every
	// fragment for a Devin hook file sets it, and honoring it when any does
	// keeps the merge from depending on which fragment happened to be first.
	dedupe := anyDedupeFragmentEntries(fragments)

	for _, f := range fragments {
		if len(f.Bytes) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(f.Bytes, &m); err != nil {
			return Artifact{}, fmt.Errorf("invalid JSON fragment: %w", err)
		}
		if err := deepMerge(merged, m, dedupe, 0); err != nil {
			return Artifact{}, err
		}
	}

	bytes, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("marshal merged JSON: %w", err)
	}

	return Artifact{
		Path:                  path,
		Bytes:                 bytes,
		MergeWithExisting:     anyMergeWithExisting(fragments),
		ManagedByField:        firstManagedByField(fragments),
		ManagedByValue:        firstManagedByValue(fragments),
		DedupeFragmentEntries: dedupe,
	}, nil
}

// deepMerge recursively merges src into dst. For map values, keys are merged
// recursively. For arrays of objects (e.g. "skills": [{"name": ...}]),
// elements are appended so that fragments each contributing one entry to a
// shared array are combined rather than overwritten. MCP server names are
// keyed entries: identical duplicate definitions are deduplicated, while
// conflicting definitions are rejected. For arrays of scalars (e.g. MCP server
// "args": ["-y", "server"]), the later fragment's value replaces the earlier
// one (last-wins). For all other types, src values override dst values.
//
// dedupe opts the group into collapsing semantically identical object entries
// while accumulating (see Artifact.DedupeFragmentEntries).
func deepMerge(dst, src map[string]any, dedupe bool, depth int) error {
	for k, srcVal := range src {
		dstVal, exists := dst[k]
		if exists {
			dstMap, dstIsMap := dstVal.(map[string]any)
			srcMap, srcIsMap := srcVal.(map[string]any)
			if dstIsMap && srcIsMap {
				if depth == 0 && k == "mcpServers" {
					if err := mergeMCPServers(dstMap, srcMap); err != nil {
						return err
					}
					continue
				}
				if err := deepMerge(dstMap, srcMap, dedupe, depth+1); err != nil {
					return err
				}
				continue
			}
			// Concatenate arrays of objects so that fragment merging
			// accumulates entries (e.g. each skill fragment adds one
			// entry to the "skills" array, each hook fragment adds one
			// rule to the event array). Arrays of scalars (e.g. MCP
			// server "args") use last-wins to avoid incorrect
			// duplication when two fragments define the same resource.
			dstArr, dstIsArr := dstVal.([]any)
			srcArr, srcIsArr := srcVal.([]any)
			if dstIsArr && srcIsArr && allObjects(dstArr) && allObjects(srcArr) {
				if dedupe {
					dst[k] = appendUniqueObjects(dstArr, srcArr)
					continue
				}
				dst[k] = append(dstArr, srcArr...)
				continue
			}
		}
		dst[k] = srcVal
	}
	return nil
}

// appendUniqueObjects appends the entries of src to dst, skipping any entry
// that is semantically identical to one already present.
//
// "Semantically identical" is JSON equality of the whole entry object, which is
// key-order independent because encoding/json marshals object keys in sorted
// order. First occurrence wins, so the surviving entries keep the deterministic
// order in which distinct entries were contributed and the result of collapsing
// a duplicate never depends on which copy arrived first.
//
// The comparison is quadratic in the number of entries per array: each new entry
// is compared against every existing entry, and equalJSON marshals both
// operands. Aggregate hook and MCP files hold tens of entries at most, so the
// simple form is kept.
func appendUniqueObjects(dst, src []any) []any {
	for _, entry := range src {
		duplicate := false
		for _, existing := range dst {
			if equalJSON(existing, entry) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			dst = append(dst, entry)
		}
	}
	return dst
}

func mergeMCPServers(dst, src map[string]any) error {
	for name, srcServer := range src {
		if dstServer, exists := dst[name]; exists {
			if !equalJSON(dstServer, srcServer) {
				return fmt.Errorf("conflicting MCP server definition for %q", name)
			}
			continue
		}
		dst[name] = srcServer
	}
	return nil
}

// allObjects reports whether every element in arr is a map[string]any.
// Empty arrays return false so that last-wins applies (no entries to
// accumulate).
func allObjects(arr []any) bool {
	if len(arr) == 0 {
		return false
	}
	for _, el := range arr {
		if _, ok := el.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// mergeText concatenates text fragments with newlines.
func mergeText(path string, fragments []Artifact) (Artifact, error) {
	var sb strings.Builder
	for i, f := range fragments {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.Write(f.Bytes)
	}

	return Artifact{
		Path:              path,
		Bytes:             []byte(sb.String()),
		MergeWithExisting: anyMergeWithExisting(fragments),
		ManagedByField:    firstManagedByField(fragments),
		ManagedByValue:    firstManagedByValue(fragments),
	}, nil
}

func anyDedupeFragmentEntries(artifacts []Artifact) bool {
	for _, a := range artifacts {
		if a.DedupeFragmentEntries {
			return true
		}
	}
	return false
}

func anyMergeWithExisting(artifacts []Artifact) bool {
	for _, a := range artifacts {
		if a.MergeWithExisting {
			return true
		}
	}
	return false
}

func firstManagedByField(artifacts []Artifact) string {
	for _, a := range artifacts {
		if a.ManagedByField != "" {
			return a.ManagedByField
		}
	}
	return ""
}

func firstManagedByValue(artifacts []Artifact) string {
	for _, a := range artifacts {
		if a.ManagedByValue != "" {
			return a.ManagedByValue
		}
	}
	return ""
}
