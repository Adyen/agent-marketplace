package materialize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func nativeHookLegacyMarkedFlatPatch(doc *jsonc.Document, file ownedJSONFileSpec, expected ...map[string]map[string]bool) ([]nativeHookLegacyRemoval, error) {
	container, _, err := ownedJSONContainerNode(doc, file)
	if err != nil {
		return nil, err
	}
	if container == nil {
		return nil, nil
	}
	var expectedEvents map[string]map[string]bool
	if len(expected) > 0 {
		expectedEvents = expected[0]
	}
	var removals []nativeHookLegacyRemoval
	for _, event := range container.Keys() {
		node, ok := container.Member(event)
		if !ok || node.Kind() != jsonc.KindArray {
			continue
		}
		var retained [][]byte
		eventChanged := false
		for i := 0; i < node.Len(); i++ {
			entry, _ := node.Element(i)
			if file.hookShape == targetmeta.HookShapeMatcherGroups && nativeHookLegacyGroupEntry(entry) {
				groupRemovals, rebuilt, remove, err := nativeHookLegacyGroup(file, entry, expectedEvents[event])
				if err != nil {
					return nil, err
				}
				for _, removal := range groupRemovals {
					removal.event = event
					removals = append(removals, removal)
				}
				switch {
				case remove:
					eventChanged = true
				case rebuilt != nil:
					retained = append(retained, rebuilt)
					eventChanged = true
				default:
					retained = append(retained, entry.Raw())
				}
				continue
			}
			remove, err := nativeHookLegacyFlatEntryRemoval(file, entry)
			if err != nil {
				return nil, err
			}
			if !remove {
				retained = append(retained, entry.Raw())
				continue
			}
			canonical, err := canonicalOwnedJSON(entry.Raw())
			if err != nil {
				return nil, nativeHookLegacyMarkedFlatError(file)
			}
			removals = append(removals, nativeHookLegacyRemoval{event: event, matcher: hookReviewMatcher(canonical), before: canonical})
			eventChanged = true
		}
		if !eventChanged {
			continue
		}
		if len(retained) == 0 {
			doc.DeleteMember(container, event)
		} else if !doc.SetElements(node, retained) {
			return nil, fmt.Errorf("aggregate %q could not remove marked legacy hook entries; it was left unchanged", file.rel)
		}
	}
	return removals, nil
}

func nativeHookLegacyMarkedFlatError(file ownedJSONFileSpec) error {
	return fmt.Errorf("aggregate %q holds a marked legacy hook entry outside the recognized v3.1.1 shape; it was left unchanged", file.rel)
}

func nativeHookLegacyFlatEntryRemoval(file ownedJSONFileSpec, entry *jsonc.Node) (bool, error) {
	if file.hookShape == targetmeta.HookShapeEventHandlers {
		return nativeHookLegacyWindsurfEntry(file, entry)
	}
	return nativeHookLegacyMarkedFlatEntry(file, entry)
}

func nativeHookLegacyMarkedFlatEntry(file ownedJSONFileSpec, entry *jsonc.Node) (bool, error) {
	if entry == nil || entry.Kind() != jsonc.KindObject {
		return false, nil
	}
	for _, key := range entry.Keys() {
		if strings.EqualFold(key, "hooks") {
			return false, nil
		}
	}
	if !isManagedNode(entry) {
		return false, nil
	}
	if err := nativeHookLegacyFlatShape(file, entry, true); err != nil {
		return false, err
	}
	return true, nil
}

func nativeHookLegacyFlatShape(file ownedJSONFileSpec, entry *jsonc.Node, allowMatcher bool) error {
	for _, key := range entry.Keys() {
		switch key {
		case managedByField, "type", "command", "timeout":
		case "matcher":
			if !allowMatcher {
				return nativeHookLegacyMarkedFlatError(file)
			}
		default:
			return nativeHookLegacyMarkedFlatError(file)
		}
	}
	if nodeStringMember(entry, "type") != "command" || nodeStringMember(entry, "command") == "" {
		return nativeHookLegacyMarkedFlatError(file)
	}
	if member, ok := entry.Member("matcher"); ok {
		raw := bytes.TrimSpace(member.Raw())
		if len(raw) == 0 || raw[0] != '"' {
			return nativeHookLegacyMarkedFlatError(file)
		}
		var matcher string
		if err := ownedJSONDecode(raw, &matcher); err != nil {
			return nativeHookLegacyMarkedFlatError(file)
		}
	}
	if member, ok := entry.Member("timeout"); ok {
		raw := bytes.TrimSpace(member.Raw())
		if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
			return nativeHookLegacyMarkedFlatError(file)
		}
		var timeout json.Number
		if err := ownedJSONDecode(raw, &timeout); err != nil {
			return nativeHookLegacyMarkedFlatError(file)
		}
		if value, err := timeout.Int64(); err != nil || value < 0 {
			return nativeHookLegacyMarkedFlatError(file)
		}
	}
	return nil
}

func nativeHookLegacyWindsurfEntry(file ownedJSONFileSpec, entry *jsonc.Node) (bool, error) {
	if entry == nil || entry.Kind() != jsonc.KindObject || !isManagedNode(entry) {
		return false, nil
	}
	for _, key := range entry.Keys() {
		switch key {
		case managedByField, "command", "powershell", "show_output", "working_directory":
		default:
			return false, nativeHookLegacyMarkedFlatError(file)
		}
	}
	if nodeStringMember(entry, "command") == "" {
		return false, nativeHookLegacyMarkedFlatError(file)
	}
	for _, key := range []string{"powershell", "working_directory"} {
		if member, ok := entry.Member(key); ok && !isJSONString(member.Raw()) {
			return false, nativeHookLegacyMarkedFlatError(file)
		}
	}
	if member, ok := entry.Member("show_output"); ok {
		raw := bytes.TrimSpace(member.Raw())
		if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
			return false, nativeHookLegacyMarkedFlatError(file)
		}
	}
	return true, nil
}

func isJSONString(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	var value string
	return len(raw) > 0 && raw[0] == '"' && ownedJSONDecode(raw, &value) == nil
}

func nativeHookLegacyGroupEntry(entry *jsonc.Node) bool {
	if entry == nil || entry.Kind() != jsonc.KindObject {
		return false
	}
	for _, key := range entry.Keys() {
		if key == "hooks" {
			return true
		}
	}
	return false
}

func nativeHookLegacyGroup(file ownedJSONFileSpec, group *jsonc.Node, expected map[string]bool) ([]nativeHookLegacyRemoval, []byte, bool, error) {
	hooks, ok := group.Member("hooks")
	if !ok || hooks.Kind() != jsonc.KindArray {
		if isManagedNode(group) {
			return nil, nil, false, nativeHookLegacyMarkedFlatError(file)
		}
		return nil, nil, false, nil
	}
	groupMarked := isManagedNode(group)
	markedHandlers := 0
	for i := 0; i < hooks.Len(); i++ {
		handler, _ := hooks.Element(i)
		if isManagedNode(handler) {
			markedHandlers++
		}
	}
	if !groupMarked && markedHandlers == 0 {
		return nil, nil, false, nil
	}
	for _, key := range group.Keys() {
		switch key {
		case managedByField, "matcher", "hooks":
		default:
			return nil, nil, false, nativeHookLegacyMarkedFlatError(file)
		}
	}
	if member, ok := group.Member("matcher"); ok && !isJSONString(member.Raw()) {
		return nil, nil, false, nativeHookLegacyMarkedFlatError(file)
	}
	matcher := nodeStringMember(group, "matcher")
	if groupMarked {
		for i := 0; i < hooks.Len(); i++ {
			handler, _ := hooks.Element(i)
			if !expected[nodeStringMember(handler, "command")] {
				return nil, nil, false, nil
			}
		}
		for i := 0; i < hooks.Len(); i++ {
			handler, _ := hooks.Element(i)
			if err := nativeHookLegacyFlatShape(file, handler, false); err != nil {
				return nil, nil, false, err
			}
		}
		canonical, err := canonicalOwnedJSON(group.Raw())
		if err != nil {
			return nil, nil, false, nativeHookLegacyMarkedFlatError(file)
		}
		return []nativeHookLegacyRemoval{{matcher: matcher, before: canonical}}, nil, true, nil
	}
	var removals []nativeHookLegacyRemoval
	var retained [][]byte
	for i := 0; i < hooks.Len(); i++ {
		handler, _ := hooks.Element(i)
		if !isManagedNode(handler) || !expected[nodeStringMember(handler, "command")] {
			retained = append(retained, handler.Raw())
			continue
		}
		if err := nativeHookLegacyFlatShape(file, handler, false); err != nil {
			return nil, nil, false, err
		}
		canonical, err := canonicalOwnedJSON(handler.Raw())
		if err != nil {
			return nil, nil, false, nativeHookLegacyMarkedFlatError(file)
		}
		removals = append(removals, nativeHookLegacyRemoval{matcher: matcher, before: canonical})
	}
	if len(removals) == 0 {
		return nil, nil, false, nil
	}
	if len(retained) == 0 {
		return removals, nil, true, nil
	}
	groupDoc, err := jsonc.Parse(file.rel, group.Raw())
	if err != nil {
		return nil, nil, false, nativeHookLegacyMarkedFlatError(file)
	}
	groupHooks, ok := groupDoc.Root().Member("hooks")
	if !ok || !groupDoc.SetElements(groupHooks, retained) {
		return nil, nil, false, nativeHookLegacyMarkedFlatError(file)
	}
	return removals, groupDoc.Bytes(), false, nil
}

func nativeHookExpectedCommands(rel string, desired []ownedJSONEntry) (map[string]map[string]bool, error) {
	expected := make(map[string]map[string]bool)
	for _, entry := range desired {
		if entry.kind != ownership.KindHook || entry.sourceDigest == "" {
			continue
		}
		doc, err := jsonc.Parse(rel, entry.raw)
		if err != nil {
			return nil, err
		}
		hooks, ok := doc.Root().Member("hooks")
		if !ok || hooks.Kind() != jsonc.KindArray {
			return nil, fmt.Errorf("aggregate %q desired hook group has no handler array", rel)
		}
		for i := 0; i < hooks.Len(); i++ {
			handler, _ := hooks.Element(i)
			command := nodeStringMember(handler, "command")
			if nodeStringMember(handler, "type") != "command" || command == "" {
				continue
			}
			if expected[entry.container] == nil {
				expected[entry.container] = make(map[string]bool)
			}
			expected[entry.container][command] = true
		}
	}
	return expected, nil
}
