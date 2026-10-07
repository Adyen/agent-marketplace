package materialize

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

type HookChange struct {
	Target       hcc.Target
	MachineScope bool
	File         string
	Event        string
	Matcher      string
	Before       json.RawMessage
	After        json.RawMessage
}

func nativeHookReviewDelta(key outputKey, file ownedJSONFileSpec, abs string, existing []ownedJSONExistingEntry, proof ownership.Proof, plan ownedJSONPatch) []HookChange {
	if file.hookShape == "" {
		return nil
	}
	authorized := make(map[string]string, len(plan.owned))
	for _, entry := range plan.owned {
		if entry.Kind == ownership.KindHook {
			authorized[ownedAggregateIDKey(entry.ID())] = entry.Resource
		}
	}
	type side struct {
		event    string
		matcher  string
		resource string
		raw      []byte
	}
	before := make(map[string]side, len(existing))
	for _, candidate := range existing {
		if candidate.value.ID.Kind != ownership.KindHook {
			continue
		}
		if proof.Classify(candidate.value.ID, candidate.value.ValueDigest) == ownership.StatusUnmanaged {
			continue
		}
		canonical, err := canonicalOwnedJSON(candidate.raw)
		if err != nil {
			continue
		}
		resource, _ := proof.Resource(candidate.value.ID)
		before[ownedAggregateIDKey(candidate.value.ID)] = side{
			event:    candidate.container,
			matcher:  hookReviewMatcher(canonical),
			resource: resource,
			raw:      canonical,
		}
	}
	after := make(map[string]side, len(authorized))
	for event, elements := range plan.hookElements {
		for _, element := range elements {
			canonical, err := canonicalOwnedJSON(element)
			if err != nil {
				continue
			}
			identity, err := ownedJSONHookIdentity(event, canonical)
			if err != nil {
				continue
			}
			idKey := ownedAggregateIDKey(ownership.EntryID{Kind: ownership.KindHook, Container: event, IdentityDigest: identity})
			resource, ok := authorized[idKey]
			if !ok {
				continue
			}
			after[idKey] = side{event: event, matcher: hookReviewMatcher(canonical), resource: resource, raw: canonical}
		}
	}
	var removed, added []side
	for idKey, entry := range before {
		if _, ok := after[idKey]; !ok {
			removed = append(removed, entry)
		}
	}
	for idKey, entry := range after {
		if _, ok := before[idKey]; !ok {
			added = append(added, entry)
		}
	}
	type pairKey struct {
		resource string
		event    string
		matcher  string
	}
	groups := make(map[pairKey]struct{ befores, afters []side })
	for _, entry := range removed {
		k := pairKey{entry.resource, entry.event, entry.matcher}
		g := groups[k]
		g.befores = append(g.befores, entry)
		groups[k] = g
	}
	for _, entry := range added {
		k := pairKey{entry.resource, entry.event, entry.matcher}
		g := groups[k]
		g.afters = append(g.afters, entry)
		groups[k] = g
	}
	changes := make([]HookChange, 0, len(removed)+len(added))
	row := func(event, matcher string, before, after []byte) {
		changes = append(changes, HookChange{
			Target:       key.target,
			MachineScope: key.machineScope,
			File:         abs,
			Event:        event,
			Matcher:      matcher,
			Before:       json.RawMessage(before),
			After:        json.RawMessage(after),
		})
	}
	for _, g := range groups {
		if len(g.befores) == 1 && len(g.afters) == 1 {
			row(g.befores[0].event, g.befores[0].matcher, g.befores[0].raw, g.afters[0].raw)
			continue
		}
		for _, entry := range g.befores {
			row(entry.event, entry.matcher, entry.raw, nil)
		}
		for _, entry := range g.afters {
			row(entry.event, entry.matcher, nil, entry.raw)
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Event != changes[j].Event {
			return changes[i].Event < changes[j].Event
		}
		if changes[i].Matcher != changes[j].Matcher {
			return changes[i].Matcher < changes[j].Matcher
		}
		if !bytes.Equal(changes[i].Before, changes[j].Before) {
			return bytes.Compare(changes[i].Before, changes[j].Before) < 0
		}
		return bytes.Compare(changes[i].After, changes[j].After) < 0
	})
	return changes
}

func hookReviewMatcher(raw []byte) string {
	var entry struct {
		Matcher string `json:"matcher"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return ""
	}
	return entry.Matcher
}

func nativeHookReviewDiagnostic(key outputKey, rel string, dryRun bool) diag.Diagnostic {
	code := diag.CodeMaterializeHookReview
	message := "owned hook entries changed for this configuration; review the hook changes before use"
	hint := "Review the Hook changes listing"
	if dryRun {
		code = diag.CodeMaterializeHookReviewPending
		message = "owned hook entries would change for this configuration; --check did not modify files"
		hint = "A later sync applies the pending hook change"
	}
	return diag.Diagnostic{
		Code:     code,
		Severity: diag.SeverityWarning,
		Target:   string(key.target),
		Message:  message,
		Hint:     hint,
		Source:   diag.SourceRef{File: rel},
	}
}
