package materialize

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

type openCodeOwnedAggregateHandler struct{}

func openCodeJSONOptions() ownedJSONReconcileOptions {
	return ownedJSONReconcileOptions{parseOptions: jsonc.ParseOptions{AllowTrailingCommas: true}, rootID: "mcp", sessionName: "reconcile-opencode-mcp", writeCode: diag.CodeMaterializeOpenCodeAggregateWrite, malformedCode: diag.CodeMaterializeOpenCodeAggregateMalformed, shapeCode: diag.CodeMaterializeOpenCodeAggregateMalformed, prunePostponedCode: diag.CodeMaterializeOpenCodeAggregatePrunePostponed}
}

func openCodeWork(state ownedAggregateState, key outputKey) *ownedJSONAggregateWork {
	work, _ := state.get(key).(*ownedJSONAggregateWork)
	if work == nil {
		work = &ownedJSONAggregateWork{}
		state.put(key, work)
	}
	return work
}

func (openCodeOwnedAggregateHandler) extract(state ownedAggregateState, key outputKey, artifacts []artifact.Artifact, resource string) ([]artifact.Artifact, error) {
	var rest []artifact.Artifact
	for _, a := range artifacts {
		if a.Path != "opencode.json" {
			rest = append(rest, a)
			continue
		}
		var fragment map[string]map[string]json.RawMessage
		if err := jsonc.Unmarshal(a.Path, a.Bytes, &fragment); err != nil {
			return nil, err
		}
		if len(fragment) != 1 || fragment["mcp"] == nil {
			return nil, fmt.Errorf("invalid OpenCode MCP fragment")
		}
		for _, name := range sortedKeys(fragment["mcp"]) {
			raw, err := canonicalOwnedJSON(fragment["mcp"][name])
			if err != nil {
				return nil, err
			}
			openCodeWork(state, key).add(ownedJSONEntry{rel: "opencode.json", kind: ownership.KindMcpServer, container: "mcp", key: name, identity: ownership.McpIdentityDigest(name), raw: raw, resource: resource})
		}
	}
	return rest, nil
}

func (openCodeOwnedAggregateHandler) markIncomplete(state ownedAggregateState, key outputKey, kind ir.ResourceKind) {
	if kind == ir.KindMcpServer {
		openCodeWork(state, key).markIncomplete("opencode.json")
	}
}
func (openCodeOwnedAggregateHandler) aggregateRels(kind ir.ResourceKind, _ bool) []string {
	if kind == ir.KindMcpServer {
		return []string{"opencode.json"}
	}
	return nil
}
func (openCodeOwnedAggregateHandler) malformedDiagnostic(key outputKey, resource string, _ error) diag.Diagnostic {
	return diag.Diagnostic{Code: diag.CodeMaterializeOpenCodeAggregateMalformed, Severity: diag.SeverityError, Target: string(key.target), Resource: resource, Message: "Compiled OpenCode MCP fragment could not be decoded"}
}

func routeOpenCodeWork(work *ownedJSONAggregateWork, rel string) *ownedJSONAggregateWork {
	routed := &ownedJSONAggregateWork{}
	if work == nil {
		return routed
	}
	for _, entry := range work.entries {
		entry.rel = rel
		routed.add(entry)
	}
	if work.incomplete["opencode.json"] {
		routed.markIncomplete(rel)
	}
	return routed
}

func (openCodeOwnedAggregateHandler) reconcile(ctx context.Context, report *Report, state ownedAggregateState, key outputKey, _ string, destinations []harness.Destination, dryRun bool) *Report {
	work, _ := state.get(key).(*ownedJSONAggregateWork)
	for _, destination := range destinations {
		if destination.AggregateFile == "" {
			continue
		}
		rel := destination.AggregateFile
		files := []ownedJSONFileSpec{{rel: rel, container: "mcp", keyed: true}}
		report = reconcileOwnedJSONAggregates(ctx, report, key, destination.Root, files, routeOpenCodeWork(work, rel), openCodeJSONOptions(), dryRun)
		for _, diagnostic := range report.Diags {
			if diagnostic.Target == string(key.target) && diagnostic.Code == diag.CodeOwnershipUnmanagedKey {
				report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeMaterializeOpenCodeAggregateCollision, Severity: diag.SeverityError, Target: string(key.target), Message: "A handwritten MCP entry conflicts with the selected server; configuration preserved"})
				break
			}
		}
	}
	return report
}

// OpenCodeRepairPreview uses the desired entries produced by the same compile
// phase as sync. The shared repair engine retains confirmation and revalidation.
func OpenCodeRepairPreview(ctx context.Context, repo string, resolved []resolver.ResolvedArtifact) (*RepairPreview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	compiled, keys := compileRepairTarget(repo, resolved, hcc.TargetOpenCode)
	preview := &RepairPreview{Diags: compiled.allDiags}
	for _, key := range keys {
		if work := compiled.byTarget[key]; work != nil {
			preview.Diags = append(preview.Diags, work.diags...)
		}
		if compiled.openCodeAggregateScope != nil && key.machineScope != *compiled.openCodeAggregateScope {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destinations, err := harness.ResolveDestinations(key.target, key.machineScope, repo)
		if err != nil {
			return nil, err
		}
		work, _ := compiled.ownedState[key].(*ownedJSONAggregateWork)
		for _, destination := range destinations {
			rel := destination.AggregateFile
			if rel == "" {
				continue
			}
			if _, err := os.Lstat(filepath.Join(destination.Root, rel)); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			store, err := ownership.Open(destination.Root, ownership.WithLockObserver(observeRepairLock))
			if err != nil {
				return nil, err
			}
			routed := routeOpenCodeWork(work, rel)
			file, diagnostics := previewOwnedJSONAggregateFile(store, key, destination.Root, ownedJSONFileSpec{rel: rel, container: "mcp", keyed: true}, openCodeJSONOptions(), true, routed.entries)
			preview.Diags = append(preview.Diags, diagnostics...)
			if file != nil {
				preview.Files = append(preview.Files, file)
			}
		}
	}
	return preview, nil
}
