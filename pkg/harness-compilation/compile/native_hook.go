package compile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func NativeHookArtifact(target Target, scope Scope, event string, rule any) (artifact.Artifact, error) {
	if event == "" {
		return artifact.Artifact{}, fmt.Errorf("native hook event is empty")
	}
	machineScope := false
	switch scope {
	case "", ScopeProject:
	case ScopeUser:
		machineScope = true
	default:
		return artifact.Artifact{}, fmt.Errorf("invalid native hook scope %q", scope)
	}
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.Target(target))
	if !ok {
		return artifact.Artifact{}, fmt.Errorf("target %q has no native layout", target)
	}
	var shape targetmeta.HookShape
	found := false
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			if aggregate.Kind != ir.KindHook {
				continue
			}
			if found {
				return artifact.Artifact{}, fmt.Errorf("target %q has multiple native hook aggregates", target)
			}
			shape = aggregate.Shape
			found = true
		}
	}
	if !found {
		return artifact.Artifact{}, fmt.Errorf("target %q has no native hook aggregate", target)
	}
	file, ok := targetmeta.NativeAggregateFileForScope(targetmeta.Target(target), ir.KindHook, machineScope)
	if !ok || file.Path == "" {
		return artifact.Artifact{}, fmt.Errorf("target %q has no native hook file for scope %q", target, scope)
	}
	entry, err := nativeHookEntry(shape, rule)
	if err != nil {
		return artifact.Artifact{}, err
	}
	eventMap, err := json.Marshal(map[string][]json.RawMessage{event: {entry}})
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("encode native hook event: %w", err)
	}
	document := make(map[string]json.RawMessage, len(file.Envelope)+1)
	for key, value := range file.Envelope {
		raw, err := json.Marshal(value)
		if err != nil {
			return artifact.Artifact{}, fmt.Errorf("encode native hook envelope: %w", err)
		}
		document[key] = raw
	}
	if file.Member != "" {
		if _, exists := document[file.Member]; exists {
			return artifact.Artifact{}, fmt.Errorf("native hook member %q conflicts with its envelope", file.Member)
		}
		document[file.Member] = eventMap
	} else {
		var events map[string]json.RawMessage
		if err := json.Unmarshal(eventMap, &events); err != nil {
			return artifact.Artifact{}, fmt.Errorf("decode native hook event map: %w", err)
		}
		for key, value := range events {
			if _, exists := document[key]; exists {
				return artifact.Artifact{}, fmt.Errorf("native hook event %q conflicts with its envelope", key)
			}
			document[key] = value
		}
	}
	data, err := json.Marshal(document)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("encode native hook artifact: %w", err)
	}
	return artifact.Artifact{
		Path:                           file.Path,
		Bytes:                          data,
		Mode:                           0o600,
		Fragment:                       true,
		FragmentKey:                    file.Path,
		Reconciliation:                 artifact.ReconciliationPolicy{Mode: artifact.ReconciliationExternal},
		ExternalReconciliationRequired: true,
	}, nil
}

func nativeHookEntry(shape targetmeta.HookShape, rule any) (json.RawMessage, error) {
	raw, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("encode native hook rule: %w", err)
	}
	if err := validateNativeHookOwnershipFields(raw); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("native hook rule must be an object")
	}
	switch shape {
	case targetmeta.HookShapeMatcherGroups:
		if hooks, ok := fields["hooks"]; ok {
			var handlers []json.RawMessage
			if err := json.Unmarshal(hooks, &handlers); err != nil || len(handlers) == 0 {
				return nil, fmt.Errorf("native hook matcher group has no handlers")
			}
			return json.Marshal(fields)
		}
		matcher, hasMatcher := fields["matcher"]
		delete(fields, "matcher")
		handler, err := json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("encode native hook handler: %w", err)
		}
		handlers, err := json.Marshal([]json.RawMessage{handler})
		if err != nil {
			return nil, fmt.Errorf("encode native hook handlers: %w", err)
		}
		group := map[string]json.RawMessage{"hooks": handlers}
		if hasMatcher {
			group["matcher"] = matcher
		}
		return json.Marshal(group)
	case targetmeta.HookShapeEventHandlers:
		delete(fields, "event")
		return json.Marshal(fields)
	default:
		return nil, fmt.Errorf("unknown native hook shape %q", shape)
	}
}

func validateNativeHookOwnershipFields(raw json.RawMessage) error {
	pending := []json.RawMessage{raw}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		var object map[string]json.RawMessage
		if err := json.Unmarshal(current, &object); err == nil && object != nil {
			for key, value := range object {
				if strings.EqualFold(key, "_managed_by") {
					return fmt.Errorf("native hook entry contains reserved ownership metadata")
				}
				pending = append(pending, value)
			}
			continue
		}
		var values []json.RawMessage
		if err := json.Unmarshal(current, &values); err == nil {
			pending = append(pending, values...)
		}
	}
	return nil
}

func nativeHookOutputArtifacts(target Target, scope Scope, artifacts []artifact.Artifact) ([]artifact.Artifact, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}
	var out []artifact.Artifact
	for _, source := range artifacts {
		events, err := nativeHookEvents(source.Bytes)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(events))
		for event := range events {
			names = append(names, event)
		}
		sort.Strings(names)
		for _, event := range names {
			for _, rule := range events[event] {
				native, err := NativeHookArtifact(target, scope, event, rule)
				if err != nil {
					return nil, err
				}
				native.FragmentOrderKey = source.FragmentOrderKey
				native.DedupeFragmentEntries = source.DedupeFragmentEntries
				out = append(out, native)
			}
		}
	}
	return out, nil
}

func nativeHookEvents(raw []byte) (map[string][]json.RawMessage, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil || document == nil {
		return nil, fmt.Errorf("compiled hook fragment is not a JSON object")
	}
	eventDocument := document
	if hooks, ok := document["hooks"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(hooks, &nested); err == nil && nested != nil {
			eventDocument = nested
		}
	}
	events := make(map[string][]json.RawMessage)
	for event, value := range eventDocument {
		var rules []json.RawMessage
		if err := json.Unmarshal(value, &rules); err == nil && len(rules) > 0 {
			events[event] = rules
		}
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("compiled hook fragment contains no event entries")
	}
	return events, nil
}
