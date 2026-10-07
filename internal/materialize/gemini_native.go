package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func geminiNativeHasOwnedMCP(destination harness.Destination) bool {
	store, err := ownership.Open(destination.Root)
	if err != nil {
		return false
	}
	proof, err := store.Proof(destination.AggregateFile)
	if err != nil {
		return false
	}
	for _, id := range proof.OwnedIDs() {
		if id.Kind == ownership.KindMcpServer && id.Container == "mcpServers" {
			return true
		}
	}
	return false
}

func nativeHookSharedMCPEntries(key outputKey, repoRoot string, artifacts []artifact.Artifact) ([]ownedJSONEntry, []artifact.Artifact, error) {
	routes, shared := nativeHookSharesAggregateRoute(key, repoRoot)
	if !shared {
		return nil, artifacts, nil
	}
	_, file, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		return nil, nil, err
	}
	var mcpRoute *harness.AggregateRoute
	for i := range routes {
		if routes[i].Container != "mcpServers" {
			continue
		}
		if mcpRoute != nil {
			return nil, nil, fmt.Errorf("shared hook output has multiple MCP routes")
		}
		mcpRoute = &routes[i]
	}
	if mcpRoute == nil {
		return nil, artifacts, nil
	}
	var entries []ownedJSONEntry
	remaining := make([]artifact.Artifact, 0, len(artifacts))
	for _, compiled := range artifacts {
		if compiled.Path != mcpRoute.ArtifactPath {
			remaining = append(remaining, compiled)
			continue
		}
		info, known := harness.TargetInfoFor(key.target)
		wantsMarker := !known || !info.OwnershipReconciledAggregates
		if !compiled.Fragment || wantsMarker && (compiled.ManagedByField != managedByField || compiled.ManagedByValue != "agent-marketplace" || !compiled.MergeWithExisting) ||
			!wantsMarker && (compiled.ManagedByField != "" || compiled.ManagedByValue != "" || compiled.MergeWithExisting) {
			return nil, nil, fmt.Errorf("shared MCP fragment does not match its reconciliation policy")
		}
		var fragment map[string]json.RawMessage
		if err := json.Unmarshal(compiled.Bytes, &fragment); err != nil || len(fragment) != 1 {
			return nil, nil, fmt.Errorf("decode shared MCP fragment")
		}
		container, ok := fragment[mcpRoute.Container]
		if !ok {
			return nil, nil, fmt.Errorf("shared MCP fragment omits its declared container")
		}
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(container, &servers); err != nil || servers == nil {
			return nil, nil, fmt.Errorf("shared MCP container is not an object")
		}
		for _, name := range sortedKeys(servers) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(servers[name], &fields); err != nil || fields == nil {
				return nil, nil, fmt.Errorf("shared MCP entry is not an object")
			}
			if _, marked := fields[managedByField]; marked {
				return nil, nil, fmt.Errorf("shared MCP definition contains an in-file ownership marker")
			}
			raw, err := canonicalOwnedJSON(servers[name])
			if err != nil {
				return nil, nil, fmt.Errorf("canonicalize shared MCP entry")
			}
			entries = append(entries, ownedJSONEntry{
				rel: file.rel, kind: ownership.KindMcpServer, container: mcpRoute.Container,
				key: name, raw: raw, identity: ownership.McpIdentityDigest(name), resource: name,
			})
		}
	}
	return entries, remaining, nil
}

func planGeminiMCPContainer(doc *jsonc.Document, key outputKey, outDir string, hookFile ownedJSONFileSpec, desired []ownedJSONEntry, proof ownership.Proof, proofErr error, incomplete bool) (ownedJSONPatch, []diag.Diagnostic, error) {
	if proofErr != nil && !errors.Is(proofErr, ownership.ErrStateMissing) {
		return ownedJSONPatch{}, nil, proofErr
	}
	spec := ownedJSONFileSpec{rel: hookFile.rel, container: "mcpServers", keyed: true}
	container, member, err := ownedJSONContainerNode(doc, spec)
	if err != nil {
		return ownedJSONPatch{}, nil, err
	}
	var existing []ownedJSONExistingEntry
	if container != nil {
		for _, name := range container.Keys() {
			entry, _ := container.Member(name)
			if entry.Kind() != jsonc.KindObject {
				return ownedJSONPatch{}, nil, fmt.Errorf("native MCP entry is not an object")
			}
		}
		existing, err = ownedJSONExistingEntries(spec, container)
		if err != nil {
			return ownedJSONPatch{}, nil, err
		}
	}
	desiredValues := make([]ownership.EntryValue, 0, len(desired))
	for _, entry := range desired {
		if entry.rel != spec.rel || entry.kind != ownership.KindMcpServer || entry.container != spec.container || entry.key == "" ||
			entry.identity != ownership.McpIdentityDigest(entry.key) {
			return ownedJSONPatch{}, nil, fmt.Errorf("shared MCP entry does not match its declaration")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry.raw, &fields); err != nil || fields == nil {
			return ownedJSONPatch{}, nil, fmt.Errorf("shared MCP entry is not an object")
		}
		if _, marked := fields[managedByField]; marked {
			return ownedJSONPatch{}, nil, fmt.Errorf("shared MCP definition contains an in-file ownership marker")
		}
		desiredValues = append(desiredValues, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest()})
	}
	existingValues := make([]ownership.EntryValue, 0, len(existing))
	for _, entry := range existing {
		existingValues = append(existingValues, entry.value)
		if container != nil {
			node, _ := container.Member(entry.value.ID.Key)
			if _, marked := node.Member(managedByField); marked && proof.Classify(entry.value.ID, entry.value.ValueDigest) == ownership.StatusUnmanaged {
				return ownedJSONPatch{}, nil, diag.NewCodedError("materialize.collision.user-owned", diag.SourceRef{}, fmt.Errorf("legacy Gemini MCP marker has no durable ownership proof; configuration preserved"))
			}
		}
	}
	drift := ownership.Classify(proof, existingValues, desiredValues)
	if len(drift.Collisions) > 0 {
		return ownedJSONPatch{}, nil, diag.NewCodedError("materialize.collision.user-owned", diag.SourceRef{}, fmt.Errorf("preserved unmanaged Gemini MCP server"))
	}
	patch := ownedJSONPatchPlan(outDir, spec, desired, existing, drift, proof, incomplete)
	if !incomplete {
		patch.reconciledKinds = map[ownership.EntryKind]bool{ownership.KindMcpServer: true}
	}
	if err := applyOwnedJSONPatch(doc, container, member, spec, patch); err != nil {
		return ownedJSONPatch{}, nil, err
	}
	var diagnostics []diag.Diagnostic
	for range patch.drifted {
		diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeOwnershipEntryDrift, Severity: diag.SeverityWarning, Target: string(key.target), Source: diag.SourceRef{File: spec.rel}, Message: "regenerated a drifted owned Gemini MCP server"})
	}
	return patch, diagnostics, nil
}

func syncGeminiNativeAggregate(ctx context.Context, key outputKey, destination harness.Destination, desired map[string]json.RawMessage, dryRun, prune bool) (TargetReport, []diag.Diagnostic, error) {
	rel := destination.AggregateFile
	report := TargetReport{Target: key.target, MachineScope: key.machineScope, RootID: destination.ID, OutDir: destination.Root}
	if !hasNativeDesiredEntries(desired) && (!prune || !nativeAggregateHasManagedEntries(key.target, destination)) {
		return report, nil, nil
	}
	if rel == "" || harness.ValidateRelPath(rel) != nil {
		return report, nil, fmt.Errorf("invalid Gemini native aggregate path")
	}
	if _, err := validatePortableOutputDir(destination.Root, filepath.Dir(rel)); err != nil {
		return report, nil, err
	}
	file := filepath.Join(destination.Root, rel)
	if info, err := os.Lstat(file); err == nil && !info.Mode().IsRegular() {
		return report, nil, fmt.Errorf("native aggregate must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return report, nil, err
	}
	lockReport := newOwnedAggregateLockReporter(key, destination.Root)
	store, err := ownership.Open(destination.Root, ownership.WithLockObserver(lockReport.observe))
	if err != nil {
		return report, nil, err
	}
	var diagnostics []diag.Diagnostic
	reconcile := func() error {
		if !dryRun {
			outcomes, err := store.Recover()
			if err != nil {
				return err
			}
			for _, outcome := range outcomes {
				diagnostics = append(diagnostics, ownedAggregateDiag(diag.CodeOwnershipRecovered, key, outcome.File, "recovered an incomplete Gemini native aggregate generation"))
			}
		}
		raw, readErr := ownership.ReadAggregateFile(file)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if os.IsNotExist(readErr) {
			raw = []byte(ownedJSONEmptyDocument)
		}
		hooked, err := mergeNativeAggregate(raw, map[string]json.RawMessage{"hooks": desired["hooks"]}, prune)
		if err != nil {
			if readErr == nil && !dryRun {
				if _, backupErr := store.BackupOnly(rel, file, ownership.ReasonSync); backupErr != nil {
					return backupErr
				}
			}
			return err
		}
		doc, err := jsonc.Parse(rel, hooked)
		if err != nil {
			return err
		}
		spec := ownedJSONFileSpec{rel: rel, container: "mcpServers", keyed: true}
		container, member, err := ownedJSONContainerNode(doc, spec)
		if err != nil {
			return err
		}
		if container != nil {
			for _, name := range container.Keys() {
				entry, _ := container.Member(name)
				if entry.Kind() != jsonc.KindObject {
					return fmt.Errorf("native MCP entry must be a JSON object")
				}
			}
		}
		var existing []ownedJSONExistingEntry
		if container != nil {
			existing, err = ownedJSONExistingEntries(spec, container)
			if err != nil {
				return err
			}
		}
		proof, proofErr := store.Proof(rel)
		if proofErr != nil && !errors.Is(proofErr, ownership.ErrStateMissing) {
			return proofErr
		}
		diagnostics = append(diagnostics, ownedAggregateProofDiags(key, destination.Root, rel, proofErr)...)
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(desired["mcpServers"], &servers); err != nil {
			return err
		}
		entries := make([]ownedJSONEntry, 0, len(servers))
		desiredValues := make([]ownership.EntryValue, 0, len(servers))
		for _, name := range sortedKeys(servers) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(servers[name], &fields); err != nil {
				return err
			}
			if _, marked := fields[managedByField]; marked {
				return fmt.Errorf("gemini MCP definitions cannot contain an in-file ownership marker")
			}
			canonical, err := canonicalOwnedJSON(servers[name])
			if err != nil {
				return err
			}
			entry := ownedJSONEntry{rel: rel, kind: ownership.KindMcpServer, container: "mcpServers", key: name, raw: canonical, identity: ownership.McpIdentityDigest(name), resource: name}
			entries = append(entries, entry)
			desiredValues = append(desiredValues, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest()})
		}
		existingValues := make([]ownership.EntryValue, 0, len(existing))
		for _, entry := range existing {
			existingValues = append(existingValues, entry.value)
			if container != nil {
				node, _ := container.Member(entry.value.ID.Key)
				if _, marked := node.Member(managedByField); marked && proof.Classify(entry.value.ID, entry.value.ValueDigest) == ownership.StatusUnmanaged {
					return diag.NewCodedError("materialize.collision.user-owned", diag.SourceRef{}, fmt.Errorf("legacy Gemini MCP marker has no durable ownership proof; configuration preserved"))
				}
			}
		}
		drift := ownership.Classify(proof, existingValues, desiredValues)
		if len(drift.Collisions) > 0 {
			return diag.NewCodedError("materialize.collision.user-owned", diag.SourceRef{}, fmt.Errorf("preserved unmanaged Gemini MCP server %q", drift.Collisions[0].Key))
		}
		plan := ownedJSONPatchPlan(destination.Root, spec, entries, existing, drift, proof, !prune)
		if err := applyOwnedJSONPatch(doc, container, member, spec, plan); err != nil {
			return err
		}
		next := doc.Bytes()
		if bytes.Equal(raw, next) {
			report.Skipped = []string{rel}
			return nil
		}
		if !dryRun {
			generation, digest := ownedAggregatePriorForApply(raw, readErr, proof)
			if _, err := store.Apply(ownership.Mutation{Rel: rel, AbsFile: file, NextBytes: next, Entries: plan.owned, Reason: ownership.ReasonSync, ExpectPriorGeneration: generation, ExpectPriorDigest: digest}); err != nil {
				return err
			}
		}
		for _, id := range plan.drifted {
			diagnostics = append(diagnostics, diag.Diagnostic{Code: diag.CodeOwnershipEntryDrift, Severity: diag.SeverityWarning, Target: string(key.target), Source: diag.SourceRef{File: rel}, Message: fmt.Sprintf("regenerated drifted owned Gemini MCP server %q", id.Key)})
		}
		slog.Debug("Gemini native aggregate reconciled", "file", rel, "mcp_entries", len(plan.owned), "prune", prune, "dry_run", dryRun)
		report.Written = []string{rel}
		return nil
	}
	if dryRun {
		err = reconcile()
	} else {
		err = store.WithWriteSessionContext(ctx, "gemini-native-aggregate", reconcile)
	}
	diagnostics = append(diagnostics, lockReport.diags()...)
	if errors.Is(err, ownership.ErrStateMissing) {
		diagnostics = append(diagnostics, ownedAggregateCodedDiag(key, destination.Root, rel, err, "materialize.write.error"))
		err = diag.NewCodedError("materialize.write.error", diag.SourceRef{File: rel}, fmt.Errorf("gemini native aggregate left unchanged because ownership state is unavailable: %w", err))
	}
	return report, diagnostics, err
}
