package materialize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func nativeHookLegacyMarkedFlatPatch(doc *jsonc.Document, file ownedJSONFileSpec) ([]nativeHookLegacyRemoval, error) {
	container, _, err := ownedJSONContainerNode(doc, file)
	if err != nil {
		return nil, err
	}
	if container == nil {
		return nil, nil
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
			remove, err := nativeHookLegacyMarkedFlatEntry(file, entry)
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
	for _, key := range entry.Keys() {
		switch key {
		case managedByField, "type", "command", "matcher", "timeout":
		default:
			return false, nativeHookLegacyMarkedFlatError(file)
		}
	}
	if nodeStringMember(entry, "type") != "command" || nodeStringMember(entry, "command") == "" {
		return false, nativeHookLegacyMarkedFlatError(file)
	}
	if member, ok := entry.Member("matcher"); ok {
		raw := bytes.TrimSpace(member.Raw())
		if len(raw) == 0 || raw[0] != '"' {
			return false, nativeHookLegacyMarkedFlatError(file)
		}
		var matcher string
		if err := ownedJSONDecode(raw, &matcher); err != nil {
			return false, nativeHookLegacyMarkedFlatError(file)
		}
	}
	if member, ok := entry.Member("timeout"); ok {
		raw := bytes.TrimSpace(member.Raw())
		if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
			return false, nativeHookLegacyMarkedFlatError(file)
		}
		var timeout json.Number
		if err := ownedJSONDecode(raw, &timeout); err != nil {
			return false, nativeHookLegacyMarkedFlatError(file)
		}
		if value, err := timeout.Int64(); err != nil || value < 0 {
			return false, nativeHookLegacyMarkedFlatError(file)
		}
	}
	return true, nil
}
