// Devin shared-aggregate materialization (work package D10 of
// docs/plans/2026-08-03-devin-first-class-harness.md).
//
// The rest of internal/materialize treats a "merge artifact" as a JSON file
// whose managed entries are recognizable by an injected `_managed_by` marker
// (see materializeMergeArtifacts / stripManagedEntriesFromFile). Devin cannot
// use that mechanism, for three independent reasons the plan makes explicit:
//
//  1. "ownership is never inferred heuristically" and "Missing or corrupt
//     ownership state makes all existing aggregate entries unmanaged". A marker
//     INSIDE the file is exactly heuristic inference: any user (or any other
//     tool) can write `_managed_by` and have their entry deleted. Devin
//     ownership therefore comes from internal/ownership, whose Proof type
//     cannot be constructed outside that package.
//  2. "Structural patching must preserve unmanaged comments and key order."
//     Devin patches through pkg/harness-compilation/jsonc, which never
//     re-serializes an untouched value. (Bead agent-marketplace-5exj.18 later
//     moved stripManagedEntriesFromFile onto the same parser, and bead
//     agent-marketplace-5exj.26 moved the marker-merge WRITE path
//     — artifact.MergeWithExistingJSON — onto it too, so this is no longer a
//     difference in JSON handling at all. Reason 1 is unchanged and is on its
//     own sufficient: those paths still decide ownership from an in-file
//     marker, and Devin must not.)
//  3. hooks.v1.json "contains the hook object directly", so there is no
//     envelope to hang a marker off without inventing a member Devin would
//     forward to its hook runtime.
//
// The call sequence below is the one internal/ownership's package doc names for
// D10, in that order: Open, Recover (once, before any mutation), Proof,
// Classify, patch with jsonc, Apply. Nothing here decides ownership itself.
//
// No diagnostic, hint, or log attribute in this file carries a hook payload, a
// matcher, a timeout value, an MCP env value, a header, an OAuth secret, or
// aggregate file content. Only the aggregate's path (its install root plus its
// output-relative path — see ownedAggregateLabel, which is what makes two
// aggregates under one root distinguishable), the non-secret entry identity (an
// MCP server object key or a hook event name), resource names, and digests are
// reported.
package materialize

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// devinAggregateClaim resolves which declared Devin aggregate claims the
// output-relative path rel in the given scope, returning its kind and the
// declared envelope member — "" means the file's top level carries the
// entries directly (project-scope hooks.v1.json). The descriptor
// (pkg/harness-compilation/target) is the topology authority, so the member
// a file spec patches and a fragment decodes comes from the declaration
// rather than a second literal copy here.
func devinAggregateClaim(rel string, machineScope bool) (ir.ResourceKind, string, bool) {
	layout, ok := targetmeta.LookupNativeLayout(targetmeta.TargetDevin)
	if !ok {
		return 0, "", false
	}
	for _, partition := range layout.Partitions {
		for _, aggregate := range partition.Aggregates {
			file := aggregate.Project
			if machineScope {
				file = aggregate.User
			}
			if path.Join(partition.Prefix, file.Path) == rel {
				return aggregate.Kind, file.Member, true
			}
		}
	}
	return 0, "", false
}

// devinAggregateRels returns the aggregate files a Devin resource kind can
// contribute to in the given scope. It is used to attribute a SKIPPED resource
// to the files it would have written, since a skipped resource produces no
// artifacts to read the path from.
func devinAggregateRels(kind ir.ResourceKind, machineScope bool) []string {
	switch kind {
	case ir.KindHook:
		if machineScope {
			return []string{harness.DevinUserConfigFile}
		}
		return []string{harness.DevinProjectHooksFile}
	case ir.KindMcpServer:
		return []string{harness.DevinMcpConfigFile}
	default:
		return nil
	}
}

// devinOwnedJSONFile builds the reconcile spec for one declared Devin
// aggregate file. The container member comes from the descriptor; the keyed
// flag is entry-shape knowledge (MCP entries are a keyed object, hooks are
// event arrays) that the descriptor does not model.
func devinOwnedJSONFile(rel string, machineScope bool) ownedJSONFileSpec {
	kind, member, ok := devinAggregateClaim(rel, machineScope)
	if !ok {
		return ownedJSONFileSpec{rel: rel}
	}
	return ownedJSONFileSpec{rel: rel, container: member, keyed: kind == ir.KindMcpServer}
}

func devinOwnedJSONOptions() ownedJSONReconcileOptions {
	return ownedJSONReconcileOptions{
		sessionName:        "reconcile-devin-aggregates",
		writeCode:          diag.CodeMaterializeDevinAggregateWrite,
		malformedCode:      diag.CodeMaterializeDevinAggregateMalformed,
		shapeCode:          diag.CodeMaterializeDevinAggregateShape,
		satisfiedCode:      diag.CodeMaterializeDevinHookSatisfiedByUnmanaged,
		prunePostponedCode: diag.CodeMaterializeDevinAggregatePrunePostponed,
	}
}

type devinOwnedAggregateHandler struct{}

func (devinOwnedAggregateHandler) extract(state ownedAggregateState, key outputKey, artifacts []artifact.Artifact, resource string) ([]artifact.Artifact, error) {
	rest, entries, err := splitDevinAggregateArtifacts(artifacts, key.machineScope, resource)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return rest, nil
	}
	aggregate := state.devin[key]
	if aggregate == nil {
		aggregate = &ownedJSONAggregateWork{}
		state.devin[key] = aggregate
	}
	for _, entry := range entries {
		aggregate.add(entry)
	}
	return rest, nil
}

func (devinOwnedAggregateHandler) markIncomplete(state ownedAggregateState, key outputKey, kind ir.ResourceKind) {
	rels := devinAggregateRels(kind, key.machineScope)
	if len(rels) == 0 {
		return
	}
	aggregate := state.devin[key]
	if aggregate == nil {
		aggregate = &ownedJSONAggregateWork{}
		state.devin[key] = aggregate
	}
	for _, rel := range rels {
		aggregate.markIncomplete(rel)
	}
}

func (devinOwnedAggregateHandler) aggregateRels(kind ir.ResourceKind, machineScope bool) []string {
	return devinAggregateRels(kind, machineScope)
}

func (devinOwnedAggregateHandler) malformedDiagnostic(key outputKey, resource string, err error) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeMaterializeDevinAggregateMalformed,
		Severity: diag.SeverityError,
		Target:   string(key.target),
		Resource: resource,
		Message:  fmt.Sprintf("compiled Devin aggregate fragment for resource %q could not be read: %v", resource, err),
	}
}

func (devinOwnedAggregateHandler) reconcile(ctx context.Context, report *Report, state ownedAggregateState, key outputKey, outDir string, _ []harness.Destination, dryRun bool) *Report {
	return reconcileDevinAggregates(ctx, report, key, outDir, state.devin[key], dryRun)
}

func reconcileDevinAggregates(ctx context.Context, report *Report, key outputKey, outDir string, work *ownedJSONAggregateWork, dryRun bool) *Report {
	if key.target != hcc.TargetDevin {
		return report
	}
	info, ok := harness.TargetInfoMap[key.target]
	if !ok {
		return report
	}
	rels := append([]string(nil), info.AggregateFilesForScope(key.machineScope)...)
	files := make([]ownedJSONFileSpec, 0, len(rels))
	for _, rel := range rels {
		files = append(files, devinOwnedJSONFile(rel, key.machineScope))
	}
	return reconcileOwnedJSONAggregates(ctx, report, key, outDir, files, work, devinOwnedJSONOptions(), dryRun)
}

func reconcileDevinAggregateFile(store *ownership.Store, key outputKey, outDir, rel string, desired []ownedJSONEntry, incomplete, dryRun bool) ownedJSONAggregateReport {
	return reconcileOwnedJSONAggregateFile(store, key, outDir, devinOwnedJSONFile(rel, key.machineScope), desired, incomplete, devinOwnedJSONOptions(), dryRun)
}

// isDevinAggregatePath reports whether an artifact path is one of the Devin
// shared aggregate files for the given scope.
func isDevinAggregatePath(path string, machineScope bool) bool {
	info, ok := harness.TargetInfoMap[hcc.TargetDevin]
	if !ok {
		return false
	}
	for _, rel := range info.AggregateFilesForScope(machineScope) {
		if rel == path {
			return true
		}
	}
	return false
}

// splitDevinAggregateArtifacts removes the Devin aggregate fragments from a
// compiled artifact list and converts them into desired entries.
//
// The fragments must not reach the generic plan path: hcc.Materialize would
// write them as whole files (clobbering handwritten content) or marker-merge
// them (inferring ownership from file content).
func splitDevinAggregateArtifacts(artifacts []artifact.Artifact, machineScope bool, resource string) ([]artifact.Artifact, []ownedJSONEntry, error) {
	var rest []artifact.Artifact
	var entries []ownedJSONEntry
	for _, a := range artifacts {
		if !isDevinAggregatePath(a.Path, machineScope) {
			rest = append(rest, a)
			continue
		}
		parsed, err := devinFragmentEntries(a.Path, a.Bytes, machineScope, resource)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, parsed...)
	}
	return rest, entries, nil
}

// devinFragmentEntries decodes one compiled aggregate fragment into desired
// entries. The claiming kind and envelope member come from the layout
// descriptor; the fragment shapes are fixed by the Devin compilers:
//
//	hooks.v1.json   {"<Event>": [ {rule} ]}             — hook, no member
//	config.json     {"<member>": {"<Event>": [ {rule} ]}} — hook envelope
//	mcp_config.json {"<member>": {"<key>": {server}}}   — keyed envelope
func devinFragmentEntries(rel string, data []byte, machineScope bool, resource string) ([]ownedJSONEntry, error) {
	kind, member, ok := devinAggregateClaim(rel, machineScope)
	if !ok {
		return nil, fmt.Errorf("unknown Devin aggregate %q", rel)
	}
	switch kind {
	case ir.KindMcpServer:
		var doc map[string]map[string]json.RawMessage
		if err := ownedJSONDecode(data, &doc); err != nil {
			return nil, fmt.Errorf("decode %s fragment: %w", rel, err)
		}
		var entries []ownedJSONEntry
		for _, key := range sortedKeys(doc[member]) {
			raw, err := canonicalOwnedJSON(doc[member][key])
			if err != nil {
				return nil, fmt.Errorf("canonicalize %s entry: %w", rel, err)
			}
			entries = append(entries, ownedJSONEntry{
				rel:       rel,
				kind:      ownership.KindMcpServer,
				container: member,
				key:       key,
				raw:       raw,
				identity:  ownership.McpIdentityDigest(key),
				resource:  resource,
			})
		}
		return entries, nil
	case ir.KindHook:
		if member == "" {
			var doc map[string][]json.RawMessage
			if err := ownedJSONDecode(data, &doc); err != nil {
				return nil, fmt.Errorf("decode %s fragment: %w", rel, err)
			}
			return ownedJSONHookEntries(rel, doc, resource)
		}
		var doc map[string]map[string][]json.RawMessage
		if err := ownedJSONDecode(data, &doc); err != nil {
			return nil, fmt.Errorf("decode %s fragment: %w", rel, err)
		}
		return ownedJSONHookEntries(rel, doc[member], resource)
	default:
		return nil, fmt.Errorf("unknown Devin aggregate %q", rel)
	}
}
