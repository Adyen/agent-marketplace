package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

type ExistingMergeReport struct {
	Added     []string
	Removed   []string
	Unchanged []string
}

// MergeWithExistingJSON merges the desired aggregate document into the bytes
// that are already on disk and returns the bytes to write.
//
// The destination is a USER-OWNED harness config (.mcp.json, mcp.json,
// mcp_config.json, settings.json, hooks.json, plugin.json,
// gemini-extension.json): everything in it that the marketplace did not
// generate belongs to the user. The write therefore goes through
// pkg/harness-compilation/jsonc, the single concrete-syntax layer, exactly like
// the prune path does since bead agent-marketplace-5exj.18. Bead
// agent-marketplace-5exj.26 moved this WRITE path onto it too, which is the far
// more frequent one — a prune happens when a resource is removed, this runs on
// every ordinary sync that merges into an existing file. Two properties follow:
//
//   - Comments, key order, and handwritten formatting SURVIVE. The merge
//     decisions are still made on decoded values, but the RESULT is applied to
//     the parsed concrete syntax as a minimal patch: a member whose value is
//     already semantically what we want is not touched at all, so it keeps its
//     exact bytes, its position, and the comments written above it. The old
//     json.MarshalIndent tail re-serialized the whole document on every merge,
//     which deleted comments, sorted every key, and collapsed duplicates.
//   - Duplicate object keys FAIL CLOSED with the same stable
//     *diag.CodedError (json.duplicate-key) the read-only loaders return since
//     bead .16 and the prune path since bead .18. encoding/json collapses them
//     last-value-wins, and because this function's output is written back, the
//     collapse was persisted: a second "headers" or "env" block silently
//     disappeared from the user's file. Preserving them instead was rejected
//     for the same reason as in .18 — this function's own decision ("which
//     entries carry the marker") is undefined when a key appears twice, so we
//     would be persisting a document whose meaning is parser-dependent.
//     Nothing is written: the caller gets the error before any staging.
//
// The desired bytes are compiler output, never user-authored, so they keep the
// strict jsonutil decode: nothing about their formatting is preserved and they
// cannot contain comments.
func MergeWithExistingJSON(existingBytes, desiredBytes []byte, managedByField, managedByValue string) ([]byte, ExistingMergeReport, error) {
	// Both documents are validated and decoded before a single write to the
	// destination is prepared, so a malformed or duplicate-key document can
	// never partially mutate the existing configuration. The caller only has
	// bytes here; it supplies the file path when wrapping the returned error.
	existing := make(map[string]any)
	// doc is nil when there is no destination yet (a first-ever write) or the
	// destination is blank. There is then nothing to preserve, and the result is
	// rendered with json.MarshalIndent so a newly generated file keeps the
	// conventional two-space shape every golden tree records.
	var doc *jsonc.Document
	if len(bytes.TrimSpace(existingBytes)) > 0 {
		parsed, err := jsonc.Parse("", existingBytes)
		if err != nil {
			logJSONRefusal("existing", err)
			return nil, ExistingMergeReport{}, fmt.Errorf("invalid existing JSON: %w", err)
		}
		if err := parsed.Decode(&existing); err != nil {
			return nil, ExistingMergeReport{}, fmt.Errorf("invalid existing JSON: %w", err)
		}
		if existing == nil {
			// A literal `null` document decodes to a nil map. Treat it as an
			// empty object rather than panicking on the first assignment.
			existing = make(map[string]any)
		}
		doc = parsed
	}

	var desired map[string]any
	if err := jsonutil.Unmarshal("", desiredBytes, &desired); err != nil {
		logJSONRefusal("desired", err)
		return nil, ExistingMergeReport{}, fmt.Errorf("invalid desired JSON: %w", err)
	}

	report := ExistingMergeReport{}
	for containerKey, desiredValue := range desired {
		// Array-of-objects containers (e.g. plugin.json's "skills"/"droids"/
		// "agents") are keyed by each entry's "name" field. Detect these first
		// so they merge per-entry instead of being wholesale-replaced.
		if desiredArr, ok := asObjectArray(desiredValue); ok {
			existing[containerKey] = mergeArrayContainer(existing[containerKey], desiredArr, managedByField, managedByValue, &report)
			continue
		}

		// Map-of-arrays-of-objects containers (e.g. hooks.json's
		// {"PostToolUse": [{...}]}) are merged per-event: managed entries are
		// wholesale-replaced while user entries are preserved.
		//
		// An EMPTY desired container matches this shape vacuously and matches
		// the keyed-object shape equally well, so the destination decides. It
		// has to: sending `{"mcpServers": {}}` (every managed server removed)
		// down the map-of-arrays path used to walk the user's servers looking
		// for arrays, find objects, and drop every one of them — silent data
		// loss on the write path, which is exactly what bead
		// agent-marketplace-5exj.26 is about.
		if desiredMap, ok := asMapOfObjectArrays(desiredValue); ok &&
			(len(desiredMap) > 0 || isMapOfObjectArrays(existing[containerKey])) {
			existing[containerKey] = mergeMapOfObjectArrays(existing[containerKey], desiredMap, managedByField, managedByValue, &report)
			continue
		}

		desiredEntries, ok := asObjectMap(desiredValue)
		if !ok {
			existing[containerKey] = desiredValue
			continue
		}

		existingEntries, ok := asObjectMap(existing[containerKey])
		if !ok {
			existingEntries = make(map[string]any)
		}

		for name, existingValue := range existingEntries {
			if _, wanted := desiredEntries[name]; wanted {
				continue
			}
			if isManagedEntry(existingValue, managedByField, managedByValue) {
				delete(existingEntries, name)
				report.Removed = append(report.Removed, name)
			}
		}

		for name, desiredEntry := range desiredEntries {
			marked := markManagedEntry(desiredEntry, managedByField, managedByValue)
			if existingValue, ok := existingEntries[name]; !ok {
				report.Added = append(report.Added, name)
				existingEntries[name] = marked
			} else if isManagedEntry(existingValue, managedByField, managedByValue) {
				// Existing entry is already managed by us — update it.
				if equalJSON(existingValue, marked) {
					report.Unchanged = append(report.Unchanged, name)
				} else {
					report.Added = append(report.Added, name)
				}
				existingEntries[name] = marked
			} else {
				// Existing entry is a user entry (not managed by us) — preserve it.
				report.Unchanged = append(report.Unchanged, name)
			}
		}

		existing[containerKey] = existingEntries
	}

	// Strip managed entries from existing container keys that are no longer
	// in desired (e.g. a hook event that was excluded). Without this, the
	// merged file would retain stale managed entries for removed resources.
	for containerKey, existingValue := range existing {
		if _, inDesired := desired[containerKey]; inDesired {
			continue
		}
		switch existingArr := existingValue.(type) {
		case []any:
			kept := make([]any, 0, len(existingArr))
			for _, entry := range existingArr {
				if isManagedEntry(entry, managedByField, managedByValue) {
					continue
				}
				kept = append(kept, entry)
			}
			if len(kept) < len(existingArr) {
				if len(kept) == 0 {
					delete(existing, containerKey)
				} else {
					existing[containerKey] = kept
				}
				report.Removed = append(report.Removed, containerKey)
			}
		case map[string]any:
			for name, entry := range existingArr {
				if isManagedEntry(entry, managedByField, managedByValue) {
					delete(existingArr, name)
					report.Removed = append(report.Removed, name)
				}
			}
		}
	}

	sort.Strings(report.Added)
	sort.Strings(report.Removed)
	sort.Strings(report.Unchanged)

	if doc == nil {
		// First-ever write, or a destination that was blank: nothing to
		// preserve, so emit a conventionally formatted document.
		result, err := renderJSON(existing, "")
		if err != nil {
			return nil, ExistingMergeReport{}, err
		}
		slog.Debug("aggregate merge produced a new file",
			"added", len(report.Added), "removed", len(report.Removed), "unchanged", len(report.Unchanged))
		return append(result, '\n'), report, nil
	}

	if err := patchDocument(doc, existing); err != nil {
		return nil, ExistingMergeReport{}, err
	}
	slog.Debug("aggregate merge patched an existing file",
		"added", len(report.Added), "removed", len(report.Removed), "unchanged", len(report.Unchanged),
		"changed", doc.Changed())
	return doc.Bytes(), report, nil
}

// logJSONRefusal records a refused aggregate document. Only the stable code and
// the 1-based line are logged: these files hold MCP env values, headers, and
// OAuth secrets, and the coded error deliberately never carries the offending
// value either.
func logJSONRefusal(side string, err error) {
	coded, ok := diag.AsCodedError(err)
	if !ok {
		return
	}
	slog.Error("aggregate merge refused a JSON document",
		"side", side, "code", coded.Code, "line", coded.Source.Line)
}

// renderJSON serializes a value the marketplace owns, indented to sit at the
// given line prefix inside the destination document. It is the only place a
// value is serialized: everything already present and already correct keeps its
// original bytes instead.
func renderJSON(value any, indent string) ([]byte, error) {
	raw, err := json.MarshalIndent(value, indent, "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged JSON: %w", err)
	}
	return raw, nil
}

// patchDocument applies the merged view onto the parsed destination document as
// a minimal patch.
func patchDocument(doc *jsonc.Document, merged map[string]any) error {
	root := doc.Root()
	if root.Kind() == jsonc.KindObject && len(root.Keys()) > 0 {
		return patchObject(doc, root, merged)
	}
	if root.Kind() == jsonc.KindObject && len(merged) == 0 {
		return nil // an empty object that must stay empty: leave the user's bytes
	}
	// An empty object, or a root that is not an object at all (`null`, an array,
	// a scalar). There are no members to preserve and no interior shape to
	// patch into, so render the whole root. Trivia before and after the document
	// — a file-header comment, the trailing newline — is kept by ReplaceRoot.
	raw, err := renderJSON(merged, "")
	if err != nil {
		return err
	}
	doc.ReplaceRoot(raw)
	return nil
}

// patchObject reconciles an object node with the merged view of it. Members are
// visited in SOURCE order and only touched when they actually differ, so key
// order and the exact bytes of everything untouched survive. Keys that are new
// are appended in sorted order, which is the only ordering decision left to
// make and keeps the output deterministic.
func patchObject(doc *jsonc.Document, node *jsonc.Node, merged map[string]any) error {
	for _, key := range node.Keys() {
		if _, keep := merged[key]; !keep {
			doc.DeleteMember(node, key)
		}
	}
	keys := node.Keys()
	present := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		present[key] = struct{}{}
		child, ok := node.Member(key)
		if !ok {
			continue
		}
		if err := patchMember(doc, node, child, key, merged[key]); err != nil {
			return err
		}
	}
	added := make([]string, 0, len(merged))
	for key := range merged {
		if _, ok := present[key]; !ok {
			added = append(added, key)
		}
	}
	sort.Strings(added)
	for _, key := range added {
		raw, err := renderJSON(merged[key], node.AppendIndent())
		if err != nil {
			return err
		}
		doc.SetMember(node, key, raw)
	}
	return nil
}

// patchMember reconciles one member value. Containers that still have contents
// are patched in place so their interior is preserved; anything else is
// replaced only when it is not already semantically what we want.
//
// An EMPTY container is replaced rather than patched: appending into `{}` or
// `[]` would put the closing delimiter on the last member's line, because the
// trivia that should precede it never existed. There is nothing inside an empty
// container to preserve, so rendering it is strictly better output.
func patchMember(doc *jsonc.Document, parent, child *jsonc.Node, key string, want any) error {
	switch wanted := want.(type) {
	case map[string]any:
		if child.Kind() == jsonc.KindObject && len(child.Keys()) > 0 {
			return patchObject(doc, child, wanted)
		}
	case []any:
		if child.Kind() == jsonc.KindArray && child.Len() > 0 {
			return patchArray(doc, child, wanted)
		}
	}
	wantBytes, err := json.Marshal(want)
	if err != nil {
		return fmt.Errorf("marshal merged JSON: %w", err)
	}
	if nodeMatches(child, wantBytes) {
		return nil
	}
	raw, err := renderJSON(want, parent.AppendIndent())
	if err != nil {
		return err
	}
	doc.SetMember(parent, key, raw)
	return nil
}

// patchArray reconciles an array node with the merged view of it. An element
// that is semantically unchanged is reinstated with its ORIGINAL bytes, at its
// original index when it did not move and by content match when it did, so a
// handwritten entry keeps its spelling either way. Elements are matched
// wholesale rather than recursively: in every shape this function serves, an
// element is either a user entry the merge never rewrites or a managed entry
// the compiler regenerated in full.
func patchArray(doc *jsonc.Document, node *jsonc.Node, want []any) error {
	raws := make([][]byte, len(want))
	used := make([]bool, node.Len())
	wantBytes := make([][]byte, len(want))
	for i, wanted := range want {
		encoded, err := json.Marshal(wanted)
		if err != nil {
			return fmt.Errorf("marshal merged JSON: %w", err)
		}
		wantBytes[i] = encoded
	}

	// canonical caches each element's canonical bytes: the positional pass, the
	// content index, and the fallback decode all want them, and rendering them
	// three times over a large container is the difference between linear and
	// merely tolerable.
	canonical := make([][]byte, node.Len())
	canonicalAt := func(j int) []byte {
		if canonical[j] == nil {
			if element, ok := node.Element(j); ok {
				canonical[j] = element.Canonical()
			}
		}
		return canonical[j]
	}

	// Positional matches first: they keep the element AND the comments the
	// surrounding trivia associates with that position.
	unmatched := 0
	for i := range want {
		element, ok := node.Element(i)
		if !ok {
			unmatched += len(want) - i
			break
		}
		if bytesOrValueEqual(canonicalAt(i), wantBytes[i]) {
			raws[i] = element.Raw()
			used[i] = true
			continue
		}
		unmatched++
	}
	if unmatched == 0 && len(want) == node.Len() {
		// Nothing moved and nothing changed: leave the array completely alone
		// rather than round-tripping every element through SetElements.
		return nil
	}

	// Then content matches for elements that moved, so a moved element keeps
	// its handwritten spelling. The index is keyed on the canonical bytes
	// first, which costs no decode at all; only if something is still
	// unaccounted for after that do the remaining elements get decoded.
	byContent := make(map[string][]int)
	for j := range canonical {
		if used[j] {
			continue
		}
		byContent[string(canonicalAt(j))] = append(byContent[string(canonicalAt(j))], j)
	}
	if anyUnaccountedFor(raws, wantBytes, byContent) {
		for j := range canonical {
			if used[j] {
				continue
			}
			normalized, err := normalizeJSON(canonicalAt(j))
			if err != nil || bytes.Equal(normalized, canonicalAt(j)) {
				continue
			}
			byContent[string(normalized)] = append(byContent[string(normalized)], j)
		}
	}

	for i, wanted := range want {
		if raws[i] != nil {
			continue
		}
		if j, ok := takeMatch(byContent, wantBytes[i], used); ok {
			if element, found := node.Element(j); found {
				raws[i] = element.Raw()
				used[j] = true
				continue
			}
		}
		raw, err := renderJSON(wanted, node.AppendIndent())
		if err != nil {
			return err
		}
		raws[i] = raw
	}
	doc.SetElements(node, raws)
	return nil
}

// anyUnaccountedFor reports whether some wanted element has neither a
// positional match nor a canonical-bytes match, which is the only reason to pay
// for decoding the remaining elements.
func anyUnaccountedFor(raws, wantBytes [][]byte, byContent map[string][]int) bool {
	for i := range wantBytes {
		if raws[i] == nil && len(byContent[string(wantBytes[i])]) == 0 {
			return true
		}
	}
	return false
}

// takeMatch pops an unused element index recorded under key. An element can be
// indexed under both its canonical and its normalized bytes, so already-taken
// indices are skipped rather than handed out twice.
func takeMatch(byContent map[string][]int, key []byte, used []bool) (int, bool) {
	matches := byContent[string(key)]
	for len(matches) > 0 {
		j := matches[0]
		matches = matches[1:]
		if used[j] {
			continue
		}
		byContent[string(key)] = matches
		return j, true
	}
	byContent[string(key)] = nil
	return 0, false
}

// bytesOrValueEqual compares canonical JSON bytes with encoding/json output,
// falling back to a decode only when the bytes differ. The fast path is the
// overwhelmingly common one: an entry the compiler itself wrote last time is
// already canonical.
func bytesOrValueEqual(canonical, wantBytes []byte) bool {
	if bytes.Equal(canonical, wantBytes) {
		return true
	}
	normalized, err := normalizeJSON(canonical)
	if err != nil {
		return false
	}
	return bytes.Equal(normalized, wantBytes)
}

// nodeMatches reports whether a node already denotes the same document as
// wantBytes, which must be encoding/json output (so its object keys are
// sorted).
func nodeMatches(node *jsonc.Node, wantBytes []byte) bool {
	return bytesOrValueEqual(node.Canonical(), wantBytes)
}

// normalizeJSON re-encodes JSON bytes with sorted object keys so two documents
// that differ only in key order or whitespace compare equal.
func normalizeJSON(data []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func asObjectMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

// asMapOfObjectArrays reports whether value is a JSON object whose values are
// all arrays of objects (e.g. hooks.json's {"PostToolUse": [{...}, ...]}).
// An empty object qualifies. If any value is not an array-of-objects, the
// function returns false so callers fall back to other merge strategies.
func asMapOfObjectArrays(value any) (map[string][]map[string]any, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string][]map[string]any, len(m))
	for k, v := range m {
		arr, ok := asObjectArray(v)
		if !ok {
			return nil, false
		}
		out[k] = arr
	}
	return out, true
}

// isMapOfObjectArrays reports whether an EXISTING container really has the
// map-of-arrays-of-objects shape, i.e. it has at least one member and every
// member is an array. It disambiguates an empty desired container, which
// matches every object shape vacuously.
func isMapOfObjectArrays(value any) bool {
	m, ok := value.(map[string]any)
	if !ok || len(m) == 0 {
		return false
	}
	for _, v := range m {
		if _, isArray := v.([]any); !isArray {
			return false
		}
	}
	return true
}

// mergeMapOfObjectArrays merges a desired map-of-arrays-of-objects (e.g.
// Claude/Cursor settings hooks, {"hooks": {"PostToolUse": [{...}]}}) into an
// existing map. Each event array is merged by mergeObjectArray with behavior-
// tuple identity: hook entries have no "name", and a managed entry whose
// behavior is unchanged keeps the slot it already occupies.
//
// Until bead agent-marketplace-5exj.28 this path did what the unnamed branch of
// the array container did — drop every managed entry and re-append the desired
// set after the user's entries — which moved managed entries to the end of each
// event array and shuffled the user's entries (and their comments) up. Both
// hook shapes now share one implementation and one ordering contract.
//
// Event keys only in existing have their managed entries stripped; if no user
// entries remain, the key is dropped.
func mergeMapOfObjectArrays(existingValue any, desired map[string][]map[string]any, field, marker string, report *ExistingMergeReport) map[string]any {
	existingMap, ok := existingValue.(map[string]any)
	if !ok {
		existingMap = make(map[string]any)
	}

	result := make(map[string]any)

	// Collect all event keys from both desired and existing.
	allKeys := make(map[string]bool)
	for k := range desired {
		allKeys[k] = true
	}
	for k := range existingMap {
		allKeys[k] = true
	}

	for key := range allKeys {
		desiredArr, inDesired := desired[key]

		if !inDesired {
			// Event only in existing: strip managed entries, keep user entries.
			existingArr, ok := existingMap[key].([]any)
			if !ok || len(existingArr) == 0 {
				result[key] = existingMap[key]
				continue
			}
			var userEntries []any
			managedCount := 0
			for _, el := range existingArr {
				if isManagedEntry(el, field, marker) {
					managedCount++
				} else {
					userEntries = append(userEntries, el)
				}
			}
			if managedCount > 0 {
				report.Removed = append(report.Removed, key)
			}
			if len(userEntries) > 0 {
				result[key] = userEntries
			}
			continue
		}

		// Event in desired: merge the array in place. Entries the user wrote
		// keep their slots, managed entries whose behavior is unchanged keep
		// theirs, stale managed entries go, and genuinely new ones are
		// appended.
		existingArr, _ := existingMap[key].([]any)
		// The event key, not the individual entry, is the unit this report
		// names, so the per-entry report the array merge would write is
		// discarded and the classification is made once per key below.
		var perEntry ExistingMergeReport
		merged := mergeObjectArray(existingArr, desiredArr, field, marker, false, &perEntry)

		// Classify Unchanged, Added, or Removed by comparing the prior array with
		// the newly merged array. On an idempotent re-sync the merged array is the
		// existing one, so report Unchanged. If the event is still desired with an
		// empty array and managed entries were removed, report Removed so the
		// caller sees the same user-visible change as the event-only-in-existing
		// branch above. Otherwise the event gained or changed a managed entry and
		// is reported as Added. Mirrors the keyed-object path, which also classifies
		// via equalJSON.
		switch {
		case equalJSON(existingMap[key], merged):
			report.Unchanged = append(report.Unchanged, key)
		case len(desiredArr) > 0:
			report.Added = append(report.Added, key)
		case existingMap[key] != nil:
			report.Removed = append(report.Removed, key)
		}
		if len(merged) == 0 {
			delete(result, key)
		} else {
			result[key] = merged
		}
	}

	return result
}

// asObjectArray reports whether value is a JSON array whose elements are all
// objects. Empty arrays qualify (len 0). Arrays containing any non-object
// element do not, so callers fall back to wholesale replacement for arrays of
// scalars.
func asObjectArray(value any) ([]map[string]any, bool) {
	arr, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

// entryName extracts a non-empty "name" field used as the identity key for
// array-of-objects containers (e.g. plugin.json skills/droids/agents).
func entryName(m map[string]any) (string, bool) {
	name, ok := m["name"].(string)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

// behaviorIdentity is the identity of an array element that has no "name": the
// canonical JSON of the element with the ownership marker removed.
//
// Factory's hooks/hooks.json is the shape that needs it. Its arrays hold rule
// GROUPS — `{"matcher": "Edit", "hooks": [{"type": "command", ...}]}` — and a
// rule group has no name, no id, and nothing else a compiler could stamp on it
// without inventing a member Factory does not read. Before bead
// agent-marketplace-5exj.28 an unnamed managed element was therefore
// unidentifiable: every one of them was dropped and the desired set was
// re-appended at the END of the array, which reordered the user's own entries
// around them and — because jsonc attaches a comment to the element that
// FOLLOWS it — handed the user's comments to our generated groups.
//
// The identity chosen is the same one compile/devin/hook.go gives a Devin hook:
// the BEHAVIOR TUPLE. For a Factory rule group that tuple is (event, matcher,
// hook type, payload, timeout, extras), and it is realized exactly by the pair
// of the event key the array hangs off and the canonical bytes of the group
// object — nothing in a rule group is anything other than behavior. Two groups
// with equal bytes are the same hook, so one may be reinstated in the other's
// slot; two groups with different bytes are different hooks, so the old one is
// removed and the new one is appended. The marker is excluded because it is our
// bookkeeping, not the harness's behavior, and a desired element never carries
// it yet.
//
// Marshalling cannot fail for a value decoded from JSON, so an error degrades
// to a never-matching identity rather than a panic: the element is then treated
// as new, which is the same conservative outcome as before this existed.
func behaviorIdentity(m map[string]any, field string) string {
	value := any(m)
	if _, marked := m[field]; field != "" && marked {
		stripped := make(map[string]any, len(m))
		for k, v := range m {
			if k == field {
				continue
			}
			stripped[k] = v
		}
		value = stripped
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

// arraySlot is one position in an existing array container, kept so the merged
// array can be rebuilt in the order the file already had.
type arraySlot struct {
	name    string // "" for unkeyable entries
	dropped bool
	// reusable marks a slot held by an unnamed managed entry. If no desired
	// entry has its behavior tuple the slot is still ours, so a desired entry
	// with a behavior we do not have yet takes it over instead of being
	// appended at the end — which would leave a hole where the old entry was
	// and slide every user entry after it up one index.
	reusable bool
	value    any
}

// takeSlot pops the first slot index recorded under an identity, so N existing
// copies of the same identity are consumed by at most N desired entries instead
// of all collapsing onto the first slot.
func takeSlot(index map[string][]int, identity string) (int, bool) {
	queue := index[identity]
	if len(queue) == 0 {
		return 0, false
	}
	index[identity] = queue[1:]
	return queue[0], true
}

// mergeArrayContainer merges a desired array-of-objects into an existing array
// container (e.g. plugin.json's "skills"/"droids"/"agents"), matching entries
// by their "name" field where they have one.
func mergeArrayContainer(existingValue any, desired []map[string]any, field, marker string, report *ExistingMergeReport) []any {
	arr, _ := existingValue.([]any)
	return mergeObjectArray(arr, desired, field, marker, true, report)
}

// mergeObjectArray merges a desired array-of-objects into an existing array.
// Our managed entries (carrying the _managed_by marker) are added/updated;
// existing managed entries no longer desired are removed; user-authored entries
// (unmanaged or non-object) are preserved verbatim.
//
// IDENTITY. An entry is matched to the entry it replaces by name when nameKeyed
// is set and the entry has a non-empty "name", and otherwise by its behavior
// tuple — see behaviorIdentity. nameKeyed is true for the named containers
// (plugin.json, gemini-extension.json) and false for hook event arrays, where
// no element has a name and a stray "name" member would be part of the
// behavior, not a key. Before bead agent-marketplace-5exj.28 an unnamed managed
// entry had NO identity: it was dropped unconditionally and the desired set was
// re-appended at the end, so Factory's hooks/hooks.json — whose elements are
// unnamed rule groups — reordered itself around the user's entries.
//
// ORDER. Every surviving existing entry stays at its original index, managed or
// not, and entries that are genuinely new are appended after them: named ones
// sorted by name, then unnamed ones in desired order, which is the compiler's
// own deterministic resource order. Keeping the destination's order is the
// contract bead agent-marketplace-5exj.26 established — jsonc associates a
// comment with the element that FOLLOWS it, so reordering elements reassigns
// the user's comments — and .28 extends it to unnamed elements, which is what
// makes a no-op sync a byte-identical no-op instead of a one-time shuffle.
//
// A first-ever write has no existing entries, so everything is new and the
// output is the sorted tail alone, which is why the golden trees do not move.
func mergeObjectArray(existingArr []any, desired []map[string]any, field, marker string, nameKeyed bool, report *ExistingMergeReport) []any {
	// Only our own managed entries are keyed for replace/remove. User-authored
	// entries (unmanaged or non-object) keep their slot verbatim, so a name
	// collision between a user entry and a stale managed entry never lets the
	// managed one overwrite or delete the user entry.
	var slots []arraySlot
	managedSlot := make(map[string]int)
	managedUnnamed := make(map[string][]int)
	userNames := make(map[string]bool)

	for _, el := range existingArr {
		m, ok := el.(map[string]any)
		if !ok {
			slots = append(slots, arraySlot{value: el})
			continue
		}
		name, named := entryName(m)
		if !nameKeyed {
			named = false
		}
		if !named {
			if isManagedEntry(m, field, marker) {
				// Managed but unnamed: hold the slot, keyed on the behavior
				// tuple. It stays dropped unless a desired entry claims it,
				// which reproduces the old "stale managed entries disappear"
				// behavior without the old "and so do the live ones".
				identity := behaviorIdentity(m, field)
				managedUnnamed[identity] = append(managedUnnamed[identity], len(slots))
				slots = append(slots, arraySlot{value: el, dropped: true, reusable: true})
				continue
			}
			slots = append(slots, arraySlot{value: el})
			continue
		}
		if isManagedEntry(m, field, marker) {
			// Collapsing our own duplicates is fine: the later one wins and
			// occupies the earlier slot, so the order stays stable.
			if idx, dup := managedSlot[name]; dup {
				slots[idx].value = m
				continue
			}
			managedSlot[name] = len(slots)
			slots = append(slots, arraySlot{name: name, value: m})
			continue
		}
		// User-authored named entry: preserve verbatim.
		slots = append(slots, arraySlot{name: name, value: m})
		userNames[name] = true
	}

	desiredByName := make(map[string]struct{}, len(desired))
	for _, d := range desired {
		if name, ok := entryName(d); ok && nameKeyed {
			desiredByName[name] = struct{}{}
		}
	}

	// Remove managed entries that are no longer desired.
	for name, idx := range managedSlot {
		if _, wanted := desiredByName[name]; wanted {
			continue
		}
		delete(managedSlot, name)
		slots[idx].dropped = true
		report.Removed = append(report.Removed, name)
	}

	// Add or update desired entries.
	var appendedNamed []arraySlot
	var pendingUnnamed []any
	for _, d := range desired {
		marked := markManagedEntry(d, field, marker)
		name, ok := entryName(d)
		if !nameKeyed {
			ok = false
		}
		if !ok {
			// Unnamed desired entry: reinstate the slot of the managed entry
			// with the same behavior tuple, so an unchanged hook does not move.
			// Only a behavior we do not already have is appended.
			// The identity is taken from the UNMARKED desired entry, which is
			// the same string behaviorIdentity derives from a marked existing
			// entry: the marker is stripped on both sides.
			if idx, reused := takeSlot(managedUnnamed, behaviorIdentity(d, field)); reused {
				slots[idx].dropped = false
				slots[idx].reusable = false
				slots[idx].value = marked
				continue
			}
			pendingUnnamed = append(pendingUnnamed, marked)
			continue
		}
		if userNames[name] {
			// A user entry with this name takes precedence; preserve it and
			// drop any managed duplicate that shares the name.
			report.Unchanged = append(report.Unchanged, name)
			if idx, hadManaged := managedSlot[name]; hadManaged {
				delete(managedSlot, name)
				slots[idx].dropped = true
				report.Removed = append(report.Removed, name)
			}
			continue
		}
		if idx, exists := managedSlot[name]; exists {
			if equalJSON(slots[idx].value, marked) {
				report.Unchanged = append(report.Unchanged, name)
			} else {
				report.Added = append(report.Added, name)
			}
			slots[idx].value = marked
			continue
		}
		report.Added = append(report.Added, name)
		appendedNamed = append(appendedNamed, arraySlot{name: name, value: marked})
	}

	sort.Slice(appendedNamed, func(i, j int) bool {
		return appendedNamed[i].name < appendedNamed[j].name
	})

	// A desired entry whose behavior we did not already have takes over the
	// lowest slot still held by a superseded managed entry, in desired order.
	// A hook whose command changed is therefore rewritten IN PLACE: nothing
	// else in the array moves, so no user entry slides under someone else's
	// comment. Only entries left over once our own slots are exhausted are
	// appended, which is also the whole of a first-ever write.
	next := 0
	for i := range slots {
		if next == len(pendingUnnamed) {
			break
		}
		if !slots[i].reusable || !slots[i].dropped {
			continue
		}
		slots[i].dropped = false
		slots[i].reusable = false
		slots[i].value = pendingUnnamed[next]
		next++
	}
	appendedUnnamed := pendingUnnamed[next:]

	result := make([]any, 0, int64(len(slots))+int64(len(appendedNamed))+int64(len(appendedUnnamed)))
	for _, slot := range slots {
		if slot.dropped {
			continue
		}
		result = append(result, slot.value)
	}
	for _, slot := range appendedNamed {
		result = append(result, slot.value)
	}
	result = append(result, appendedUnnamed...)
	return result
}

func markManagedEntry(value any, field, marker string) any {
	entry, ok := asObjectMap(value)
	if !ok || field == "" {
		return value
	}
	marked := make(map[string]any, int64(len(entry))+1)
	for k, v := range entry {
		marked[k] = v
	}
	marked[field] = marker
	return marked
}

func isManagedEntry(value any, field, marker string) bool {
	entry, ok := asObjectMap(value)
	if !ok || field == "" {
		return false
	}
	return entry[field] == marker
}

func equalJSON(a, b any) bool {
	ab, aerr := json.Marshal(a)
	bb, berr := json.Marshal(b)
	return aerr == nil && berr == nil && bytes.Equal(ab, bb)
}
