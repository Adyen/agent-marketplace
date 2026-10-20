package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

var (
	codexHooksArtifactPath = mustCodexAggregatePath(ir.KindHook)
	codexMCPArtifactPath   = mustCodexAggregatePath(ir.KindMcpServer)
	codexHooksFile         = path.Base(codexHooksArtifactPath)
	codexMCPFile           = path.Base(codexMCPArtifactPath)
	// codexHooksContainer and codexMCPContainer are the declared envelope
	// members carrying each kind's entries — hooks.json's "hooks" member and
	// config.toml's "mcp_servers" table. mustCodexAggregatePath already
	// refuses a scope-variant aggregate, so the member beside it is
	// scope-invariant too.
	codexHooksContainer = mustCodexAggregateMember(ir.KindHook)
	codexMCPContainer   = mustCodexAggregateMember(ir.KindMcpServer)
)

type codexOwnedAggregateHandler struct{}

type codexOwnedAggregateState struct {
	hooks   *ownedJSONAggregateWork
	mcps    []codexMCPEntry
	blocked bool
}

func codexState(state ownedAggregateState, key outputKey) *codexOwnedAggregateState {
	current, _ := state.get(key).(*codexOwnedAggregateState)
	if current == nil {
		current = &codexOwnedAggregateState{}
		state.put(key, current)
	}
	return current
}

func (codexOwnedAggregateHandler) extract(state ownedAggregateState, key outputKey, artifacts []artifact.Artifact, resource, pluginInstanceKey string) ([]artifact.Artifact, error) {
	rest, hooks, mcps, err := splitCodexOwnedArtifacts(artifacts, resource)
	if err != nil {
		return nil, err
	}
	for i := range hooks {
		hooks[i].pluginInstanceKey = pluginInstanceKey
	}
	for i := range mcps {
		mcps[i].pluginInstanceKey = pluginInstanceKey
	}
	current := codexState(state, key)
	current.mcps = append(current.mcps, mcps...)
	if len(hooks) == 0 {
		return rest, nil
	}
	aggregate := current.hooks
	if aggregate == nil {
		aggregate = &ownedJSONAggregateWork{}
		current.hooks = aggregate
	}
	for _, entry := range hooks {
		aggregate.add(entry)
	}
	return rest, nil
}

func (codexOwnedAggregateHandler) markIncomplete(state ownedAggregateState, key outputKey, kind ir.ResourceKind) {
	current := codexState(state, key)
	switch kind {
	case ir.KindHook:
		aggregate := current.hooks
		if aggregate == nil {
			aggregate = &ownedJSONAggregateWork{}
			current.hooks = aggregate
		}
		aggregate.markIncomplete(codexHooksFile)
	case ir.KindMcpServer:
		current.blocked = true
	}
}

func (codexOwnedAggregateHandler) aggregateRels(kind ir.ResourceKind, _ bool) []string {
	switch kind {
	case ir.KindHook:
		return []string{codexHooksFile}
	case ir.KindMcpServer:
		return []string{codexMCPFile}
	default:
		return nil
	}
}

func (codexOwnedAggregateHandler) malformedDiagnostic(key outputKey, resource string, err error) diag.Diagnostic {
	return diag.Diagnostic{Code: diag.CodeMaterializeCodexAggregateMalformed, Severity: diag.SeverityError, Target: string(key.target), Resource: resource, Message: err.Error()}
}

func (codexOwnedAggregateHandler) reconcile(ctx context.Context, report *Report, state ownedAggregateState, key outputKey, _ string, destinations []harness.Destination, dryRun bool) *Report {
	current, _ := state.get(key).(*codexOwnedAggregateState)
	if current == nil {
		current = &codexOwnedAggregateState{}
	}
	batch := state.pluginBatch(key)
	for _, destination := range destinations {
		if !destination.Info.OwnershipReconciledAggregates {
			continue
		}
		session, hasSession := state.writeSession(destination.Root)
		if !state.reconcilesNativeHooks(key) {
			hookOptions := codexHookJSONOptions()
			hookOptions.rootID = destination.ID
			hookOptions.onWrite = appendCodexTrustReviewDiagnostic
			if batch != nil {
				hookOptions.batch = batch
				hookOptions.pluginInstanceKey = batch.instanceKey
				hookOptions.pluginInstanceKeys = batch.ownerInstances
				hookOptions.activePluginInstances = batch.activeInstances
			}
			if hasSession {
				hookOptions.store = session.store
				hookOptions.lockReport = session.lockReport
				hookOptions.deferLockDiagnostics = true
				hookOptions.storeSessionHeld = true
			}
			hookFile := ownedJSONFileSpec{rel: codexHooksFile, container: codexHooksContainer, rawValueDigest: true}
			report = reconcileOwnedJSONAggregates(ctx, report, key, destination.Root, []ownedJSONFileSpec{hookFile}, current.hooks, hookOptions, dryRun)
		}
		if hasSession {
			return reconcileCodexMCPWithSession(ctx, report, key, destination.ID, destination.Root, current.mcps, current.blocked, dryRun, &session, batch)
		}
		return reconcileCodexMCPWithSession(ctx, report, key, destination.ID, destination.Root, current.mcps, current.blocked, dryRun, nil, batch)
	}
	return report
}

func codexHookJSONOptions() ownedJSONReconcileOptions {
	return ownedJSONReconcileOptions{
		sessionName:        "reconcile-codex-hooks",
		writeCode:          diag.CodeMaterializeCodexAggregateMalformed,
		malformedCode:      diag.CodeMaterializeCodexAggregateMalformed,
		shapeCode:          diag.CodeMaterializeCodexAggregateMalformed,
		satisfiedCode:      diag.CodeMaterializeCodexHookSatisfiedByUnmanaged,
		prunePostponedCode: diag.CodeMaterializeCodexAggregatePrunePostponed,
	}
}

func (codexOwnedAggregateHandler) nativeHookReconcileOptions() ownedJSONReconcileOptions {
	options := codexHookJSONOptions()
	options.rawValueDigest = true
	options.onWrite = appendCodexTrustReviewDiagnostic
	return options
}

type codexMCPEntry struct {
	name, resource    string
	raw               []byte
	valueDigest       string
	pluginInstanceKey string
}

func (e codexMCPEntry) id() ownership.EntryID {
	return ownership.EntryID{Kind: ownership.KindMcpServer, Container: codexMCPContainer, Key: e.name, IdentityDigest: ownership.Digest([]byte(e.name))}
}

func (e codexMCPEntry) owned() ownership.Entry {
	id := e.id()
	return ownership.Entry{
		Kind:           id.Kind,
		Container:      id.Container,
		Key:            id.Key,
		IdentityDigest: id.IdentityDigest,
		ValueDigest:    e.valueDigest,
		Resource:       e.resource,
		PluginInstance: e.pluginInstanceKey,
	}
}

// splitCodexOwnedArtifacts keeps ownership-reconciled files out of the generic
// marker merge path.
func splitCodexOwnedArtifacts(artifacts []artifact.Artifact, resource string) ([]artifact.Artifact, []ownedJSONEntry, []codexMCPEntry, error) {
	var rest []artifact.Artifact
	var hooks []ownedJSONEntry
	var mcps []codexMCPEntry
	for _, candidate := range artifacts {
		if !candidate.ExternalReconciliationRequired {
			rest = append(rest, candidate)
			continue
		}
		switch candidate.Path {
		case codexHooksArtifactPath:
			parsed, err := codexHookFragmentEntries(candidate.Bytes, resource)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("decode Codex hooks fragment: %w", err)
			}
			hooks = append(hooks, parsed...)
		case codexMCPArtifactPath:
			parsed, err := codexDesiredMCP(candidate.Bytes, resource)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("decode Codex MCP fragment: %w", err)
			}
			mcps = append(mcps, parsed...)
		default:
			rest = append(rest, candidate)
		}
	}
	return rest, hooks, mcps, nil
}

func codexHookFragmentEntries(data []byte, resource string) ([]ownedJSONEntry, error) {
	var document map[string]map[string][]json.RawMessage
	if err := ownedJSONDecode(data, &document); err != nil {
		return nil, fmt.Errorf("decode %s fragment: %w", codexHooksFile, err)
	}
	return ownedJSONHookEntries(codexHooksFile, document[codexHooksContainer], resource)
}

func mustCodexAggregatePath(kind ir.ResourceKind) string {
	artifactPath, ok := targetmeta.NativeAggregatePath(targetmeta.TargetCodex, kind)
	if !ok {
		panic(fmt.Sprintf("Codex native aggregate path for %s is not registered", kind))
	}
	return artifactPath
}

// mustCodexAggregateMember resolves the declared envelope member for a Codex
// aggregate kind. Like the path, a missing declaration is a registry
// programming error, not a runtime condition.
func mustCodexAggregateMember(kind ir.ResourceKind) string {
	file, ok := targetmeta.NativeAggregateFileForScope(targetmeta.TargetCodex, kind, false)
	if !ok {
		panic(fmt.Sprintf("Codex native aggregate member for %s is not registered", kind))
	}
	return file.Member
}

func codexDesiredMCP(raw []byte, resource string) ([]codexMCPEntry, error) {
	sections, _, err := codexMCPSections(raw)
	if err != nil {
		return nil, err
	}
	entries := make([]codexMCPEntry, 0, len(sections))
	for name, section := range sections {
		fragment := append([]byte(nil), raw[section.start:section.end]...)
		fragment, err = canonicalCodexMCPFragment(fragment, name)
		if err != nil {
			return nil, err
		}
		if len(fragment) > 0 && fragment[len(fragment)-1] != '\n' {
			fragment = append(fragment, '\n')
		}
		entries = append(entries, codexMCPEntry{name: name, resource: resource, raw: fragment, valueDigest: section.value.ValueDigest})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}

func canonicalCodexMCPFragment(raw []byte, name string) ([]byte, error) {
	quotedName, err := json.Marshal(name)
	if err != nil {
		return nil, err
	}
	type edit struct {
		start, end  int
		replacement []byte
	}
	var edits []edit
	var parser unstable.Parser
	parser.Reset(raw)
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.Table {
			continue
		}
		var keys []string
		it := node.Key()
		for it.Next() {
			keys = append(keys, string(it.Node().Data))
		}
		if len(keys) < 2 || keys[0] != codexMCPContainer || keys[1] != name {
			continue
		}
		var header bytes.Buffer
		header.WriteString("[" + codexMCPContainer + ".")
		header.Write(quotedName)
		for _, key := range keys[2:] {
			header.WriteByte('.')
			header.WriteString(key)
		}
		header.WriteString("]\n")
		first := int(node.Child().Raw.Offset)
		edits = append(edits, edit{start: bytes.LastIndexByte(raw[:first], '\n') + 1, end: lineEnd(raw, first), replacement: header.Bytes()})
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	next := append([]byte(nil), raw...)
	for _, candidate := range edits {
		next = slices.Concat(next[:candidate.start], candidate.replacement, next[candidate.end:])
	}
	return next, nil
}

func codexMCPValues(raw []byte) (map[string]any, error) {
	var document map[string]any
	if err := toml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	value, ok := document[codexMCPContainer]
	if !ok {
		return map[string]any{}, nil
	}
	servers, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is not a table", codexMCPContainer)
	}
	return servers, nil
}

type codexMCPSection struct {
	start, end int
	value      ownership.EntryValue
	alternate  bool
}

func codexMCPSections(raw []byte) (map[string]codexMCPSection, []ownership.EntryValue, error) {
	values, err := codexMCPValues(raw)
	if err != nil {
		return nil, nil, err
	}
	sections := make(map[string]codexMCPSection, len(values))
	var parser unstable.Parser
	parser.Reset(raw)
	current := ""
	inMCPRoot := false
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table:
			var keys []string
			it := node.Key()
			for it.Next() {
				keys = append(keys, string(it.Node().Data))
			}
			current = ""
			inMCPRoot = len(keys) == 1 && keys[0] == codexMCPContainer
			if len(keys) < 2 || keys[0] != codexMCPContainer {
				continue
			}
			inMCPRoot = false
			current = keys[1]
			first := node.Child().Raw.Offset
			start := bytes.LastIndexByte(raw[:first], '\n') + 1
			section := sections[current]
			if section.end == 0 {
				section.start = start
			}
			section.end = lineEnd(raw, int(first))
			sections[current] = section
		case unstable.KeyValue:
			var keys []string
			it := node.Key()
			for it.Next() {
				keys = append(keys, string(it.Node().Data))
			}
			if inMCPRoot {
				if len(keys) != 1 {
					return nil, nil, fmt.Errorf("ambiguous dotted MCP server definition")
				}
				if _, duplicate := sections[keys[0]]; duplicate {
					return nil, nil, fmt.Errorf("duplicate MCP server definition %q", keys[0])
				}
				start := bytes.LastIndexByte(raw[:node.Raw.Offset], '\n') + 1
				sections[keys[0]] = codexMCPSection{start: start, end: lineEnd(raw, int(node.Raw.Offset+node.Raw.Length)), alternate: true}
				continue
			}
			if current == "" {
				if len(keys) >= 3 && keys[0] == codexMCPContainer {
					name := keys[1]
					section := sections[name]
					start := bytes.LastIndexByte(raw[:node.Raw.Offset], '\n') + 1
					if section.end == 0 || start < section.start {
						section.start = start
					}
					section.end = lineEnd(raw, int(node.Raw.Offset+node.Raw.Length))
					section.alternate = true
					sections[name] = section
				}
				continue
			}
			section := sections[current]
			section.end = lineEnd(raw, int(node.Raw.Offset+node.Raw.Length))
			sections[current] = section
		}
	}
	if err := parser.Error(); err != nil {
		return nil, nil, err
	}
	existing := make([]ownership.EntryValue, 0, len(values))
	for name, value := range values {
		section, ok := sections[name]
		if !ok {
			return nil, nil, fmt.Errorf("MCP server %q is not represented by a table section", name)
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return nil, nil, err
		}
		id := ownership.EntryID{Kind: ownership.KindMcpServer, Container: codexMCPContainer, Key: name, IdentityDigest: ownership.Digest([]byte(name))}
		section.value = ownership.EntryValue{ID: id, ValueDigest: ownership.Digest(canonical)}
		sections[name] = section
		existing = append(existing, section.value)
	}
	return sections, existing, nil
}

func lineEnd(raw []byte, offset int) int {
	if offset > len(raw) {
		offset = len(raw)
	}
	if next := bytes.IndexByte(raw[offset:], '\n'); next >= 0 {
		return offset + next + 1
	}
	return len(raw)
}

func reconcileCodexMCP(ctx context.Context, report *Report, key outputKey, rootID, root string, desired []codexMCPEntry, incomplete, dryRun bool) *Report {
	return reconcileCodexMCPWithSession(ctx, report, key, rootID, root, desired, incomplete, dryRun, nil, nil)
}

func reconcileCodexMCPWithSession(ctx context.Context, report *Report, key outputKey, rootID, root string, desired []codexMCPEntry, incomplete, dryRun bool, sharedSession *ownedAggregateWriteSession, batch *pluginInstanceBatch) *Report {
	slog.Debug("Codex MCP reconciliation start", "target", key.target, "machine_scope", key.machineScope, "root", root, "desired_entries", len(desired), "incomplete", incomplete, "dry_run", dryRun)
	seen := make(map[string]string, len(desired))
	for _, entry := range desired {
		if first, duplicate := seen[entry.name]; duplicate {
			slog.Error("Codex MCP reconciliation refused duplicate desired identity", "target", key.target, "machine_scope", key.machineScope, "server", entry.name, "first_resource", first, "second_resource", entry.resource, "code", diag.CodeMaterializeCodexDuplicateIdentity)
			report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeMaterializeCodexDuplicateIdentity, Severity: diag.SeverityError, Target: string(key.target), Resource: entry.resource, Message: fmt.Sprintf("Codex MCP server %q is produced by both %q and %q; neither definition was written", entry.name, first, entry.resource)})
			return report
		}
		seen[entry.name] = entry.resource
	}
	abs := filepath.Join(root, codexMCPFile)
	if len(desired) == 0 && batch == nil {
		if _, err := os.Lstat(abs); os.IsNotExist(err) {
			return report
		}
	}
	if !dryRun {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return codexMCPDiag(report, key, fmt.Errorf("create Codex config root: %w", err))
		}
	}
	var store *ownership.Store
	var lockReport *ownedAggregateLockReporter
	var err error
	if sharedSession != nil {
		store = sharedSession.store
		lockReport = sharedSession.lockReport
	} else {
		lockReport = newOwnedAggregateLockReporter(key, root)
		store, err = ownership.Open(root, ownership.WithLockObserver(lockReport.observe))
		if err != nil {
			return codexMCPDiag(report, key, err)
		}
	}
	reconcile := func() error {
		if !dryRun && sharedSession == nil && batch == nil {
			if _, err := store.Recover(); err != nil {
				return err
			}
		}
		var raw []byte
		var readErr error
		var aggregateSnapshot pluginAggregateSnapshot
		if batch != nil {
			aggregateSnapshot, err = batch.readAggregate(ctx, store, codexMCPFile, dryRun)
			if err != nil {
				return err
			}
			raw, readErr = aggregateSnapshot.bytes, aggregateSnapshot.readErr
		} else {
			raw, readErr = store.ReadAggregate(codexMCPFile)
		}
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		if os.IsNotExist(readErr) {
			raw = nil
		}
		sections, existing, err := codexMCPSections(raw)
		if err != nil {
			if readErr == nil && !dryRun {
				if _, backupErr := store.BackupOnly(codexMCPFile, abs, ownership.ReasonSync); backupErr != nil {
					slog.Error("Codex MCP malformed-file safety backup failed", "target", key.target, "machine_scope", key.machineScope, "file", abs, "error", backupErr)
					report.Diags = append(report.Diags, ownedAggregateCodedDiag(key, root, codexMCPFile, backupErr, diag.CodeOwnershipSafetyBackup))
					return err
				}
			}
			return err
		}
		var proof ownership.Proof
		var proofErr error
		if batch != nil {
			proof, proofErr = batch.proof(ctx, store, codexMCPFile, dryRun)
		} else {
			proof, proofErr = store.Proof(codexMCPFile)
		}
		var retained []ownership.Entry
		instanceKey := ""
		var activeInstances map[string]bool
		if batch != nil {
			instanceKey, activeInstances = batch.instanceKey, batch.activeInstances
		}
		if batch != nil && len(batch.ownerInstances) > 0 {
			proof, retained, _ = proof.ForPluginInstances(batch.ownerInstances, activeInstances)
		} else {
			proof, retained, _ = proof.ForPluginInstance(instanceKey, activeInstances)
		}
		report.Diags = append(report.Diags, ownedAggregateProofDiags(key, root, codexMCPFile, proofErr)...)
		desiredValues := make([]ownership.EntryValue, 0, len(desired))
		byName := make(map[string]codexMCPEntry, len(desired))
		for _, entry := range desired {
			desiredValues = append(desiredValues, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest})
			byName[entry.name] = entry
		}
		drift := ownership.Classify(proof, existing, desiredValues)
		collisions := make(map[string]bool, len(drift.Collisions))
		for _, id := range drift.Collisions {
			collisions[id.Key] = true
			report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeOwnershipUnmanagedKey, Severity: diag.SeverityError, Target: string(key.target), Resource: byName[id.Key].resource, Message: fmt.Sprintf("Codex MCP server %q collides with an unmanaged config.toml entry; existing entry preserved", id.Key)})
		}
		type edit struct {
			start, end  int
			replacement []byte
		}
		var edits []edit
		owned := make([]ownership.Entry, 0, len(desired)+len(drift.Stale))
		if !incomplete {
			for _, id := range drift.Stale {
				if section, ok := sections[id.Key]; ok {
					if section.alternate {
						report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeMaterializeCodexAggregateUnpatchable, Severity: diag.SeverityError, Target: string(key.target), Resource: id.Key, Message: fmt.Sprintf("Codex MCP server %q uses an alternate TOML form that cannot be pruned safely; existing entry preserved", id.Key)})
						entry, _ := proof.Entry(id)
						entry.ValueDigest = section.value.ValueDigest
						owned = append(owned, entry)
						continue
					}
					edits = append(edits, edit{start: section.start, end: section.end})
				}
			}
		}
		for _, entry := range desired {
			if collisions[entry.name] {
				continue
			}
			section, exists := sections[entry.name]
			if exists && proof.Classify(entry.id(), section.value.ValueDigest) == ownership.StatusOwnedClean && section.value.ValueDigest == entry.valueDigest {
				owned = append(owned, entry.owned())
				continue
			}
			if exists {
				if section.alternate {
					report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeMaterializeCodexAggregateUnpatchable, Severity: diag.SeverityError, Target: string(key.target), Resource: entry.resource, Message: fmt.Sprintf("Codex MCP server %q uses an alternate TOML form that cannot be replaced safely; existing entry preserved", entry.name)})
					if existingEntry, ok := proof.Entry(entry.id()); ok {
						existingEntry.ValueDigest = section.value.ValueDigest
						owned = append(owned, existingEntry)
					}
					continue
				}
				owned = append(owned, entry.owned())
				edits = append(edits, edit{start: section.start, end: section.end, replacement: entry.raw})
			} else {
				owned = append(owned, entry.owned())
				prefix := []byte(nil)
				if len(raw) > 0 && raw[len(raw)-1] != '\n' {
					prefix = []byte("\n")
				}
				if len(raw) > 0 {
					prefix = append(prefix, '\n')
				}
				edits = append(edits, edit{start: len(raw), end: len(raw), replacement: append(prefix, entry.raw...)})
			}
		}
		if incomplete {
			for _, id := range drift.Stale {
				section, ok := sections[id.Key]
				if !ok {
					continue
				}
				resource, _ := proof.Resource(id)
				owned = append(owned, ownership.Entry{Kind: id.Kind, Container: id.Container, Key: id.Key, IdentityDigest: id.IdentityDigest, ValueDigest: section.value.ValueDigest, Resource: resource})
			}
		}
		sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		next := append([]byte(nil), raw...)
		for _, e := range edits {
			patched := make([]byte, 0, len(next)-(e.end-e.start)+len(e.replacement))
			patched = append(patched, next[:e.start]...)
			patched = append(patched, e.replacement...)
			patched = append(patched, next[e.end:]...)
			next = patched
		}
		owned = append(owned, retained...)
		if batch != nil {
			gen, digest := ownedAggregatePriorForApply(raw, readErr, proof)
			mutation := ownership.Mutation{
				Rel: codexMCPFile, AbsFile: abs, NextBytes: next, Entries: owned, Reason: ownership.ReasonSync,
				ExpectPriorGeneration: gen, ExpectPriorDigest: digest,
			}
			if !dryRun && !bytes.Equal(raw, next) {
				if err := batch.backupPreparedMutation(ctx, store, mutation, raw, readErr); err != nil {
					return err
				}
			}
			if err := batch.addMutation(ctx, store, mutation, nil); err != nil {
				return err
			}
			mode := aggregateSnapshot.mode
			if !aggregateSnapshot.generation.Exists {
				mode = 0o600
			}
			if err := batch.addParticipant(dependencytxn.DesiredFile{
				Path: abs, Expected: aggregateSnapshot.generation, ExpectedMode: aggregateSnapshot.mode,
				Bytes: next, Mode: mode,
			}); err != nil {
				return err
			}
			if dryRun && !bytes.Equal(raw, next) {
				recordOwnedAggregateWrite(report, key, rootID, root, codexMCPFile)
				appendCodexTrustReviewDiagnostic(report, key, codexMCPFile, true)
			} else if !dryRun && !bytes.Equal(raw, next) {
				batch.addAggregateReport(key, rootID, root, codexMCPFile, false)
			}
			return nil
		}
		if bytes.Equal(raw, next) {
			slog.Debug("Codex MCP configuration already in sync", "target", key.target, "machine_scope", key.machineScope, "file", abs, "owned_entries", len(owned))
			return nil
		}
		if dryRun {
			recordOwnedAggregateWrite(report, key, rootID, root, codexMCPFile)
			appendCodexTrustReviewDiagnostic(report, key, codexMCPFile, true)
			return nil
		}
		gen, digest := ownedAggregatePriorForApply(raw, readErr, proof)
		_, err = store.Apply(ownership.Mutation{Rel: codexMCPFile, AbsFile: abs, NextBytes: next, Entries: owned, Reason: ownership.ReasonSync, ExpectPriorGeneration: gen, ExpectPriorDigest: digest})
		if err == nil {
			slog.Info("Codex MCP configuration reconciled", "target", key.target, "machine_scope", key.machineScope, "file", abs, "owned_entries", len(owned))
			recordOwnedAggregateWrite(report, key, rootID, root, codexMCPFile)
			appendCodexTrustReviewDiagnostic(report, key, codexMCPFile, false)
		}
		return err
	}
	if dryRun || sharedSession != nil {
		err = reconcile()
	} else {
		err = store.WithWriteSessionContext(ctx, "reconcile-codex-mcp", reconcile)
	}
	if err != nil {
		return codexMCPDiag(report, key, err)
	}
	if sharedSession == nil {
		report.Diags = append(report.Diags, lockReport.diags()...)
	}
	slog.Debug("Codex MCP reconciliation complete", "target", key.target, "machine_scope", key.machineScope, "root", root, "dry_run", dryRun)
	return report
}

func appendCodexTrustReviewDiagnostic(report *Report, key outputKey, rel string, dryRun bool) {
	if report == nil || key.target != targetmeta.TargetCodex {
		return
	}
	if key.machineScope {
		if rel == codexHooksFile {
			report.Diags = append(report.Diags, nativeHookReviewDiagnostic(key, rel, dryRun))
		}
		return
	}
	nativePath := path.Join(path.Dir(codexHooksArtifactPath), filepath.ToSlash(rel))
	code := diag.CodeMaterializeCodexTrustReview
	message := fmt.Sprintf("Codex repository configuration %s changed and may require trust review", nativePath)
	hint := "Review or re-approve this repository in Codex before using the changed configuration"
	if dryRun {
		code = diag.CodeMaterializeCodexTrustReviewPending
		message = fmt.Sprintf("Codex repository configuration %s would change; --check did not modify files or trust state", nativePath)
		hint = "A later sync may require reviewing or re-approving this repository in Codex"
	}
	report.Diags = append(report.Diags, diag.Diagnostic{
		Code:     code,
		Severity: diag.SeverityWarning,
		Target:   string(key.target),
		Message:  message,
		Hint:     hint,
	})
	slog.Warn("Codex repository configuration requires trust review",
		"target", key.target, "file", nativePath, "dry_run", dryRun, "code", code)
}

func codexMCPDiag(report *Report, key outputKey, err error) *Report {
	slog.Error("Codex MCP reconciliation failed", "target", key.target, "machine_scope", key.machineScope, "code", diag.CodeMaterializeCodexAggregateMalformed, "error", err)
	report.Diags = append(report.Diags, diag.Diagnostic{Code: diag.CodeMaterializeCodexAggregateMalformed, Severity: diag.SeverityError, Target: string(key.target), Message: fmt.Sprintf("reconcile Codex config.toml: %v", err)})
	return report
}
