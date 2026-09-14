package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const ownedJSONEmptyDocument = "{}\n"

// ownedJSONEntry is one desired JSON aggregate entry with its ownership
// identity and canonical value bytes.
type ownedJSONEntry struct {
	rel       string
	kind      ownership.EntryKind
	container string
	key       string
	raw       []byte
	identity  string
	resource  string
}

func (e ownedJSONEntry) id() ownership.EntryID {
	return ownership.EntryID{Kind: e.kind, Container: e.container, Key: e.key, IdentityDigest: e.identity}
}

func (e ownedJSONEntry) valueDigest() string { return ownership.Digest(e.raw) }

func (e ownedJSONEntry) ownershipEntry() ownership.Entry {
	return ownership.Entry{
		Kind:           e.kind,
		Container:      e.container,
		Key:            e.key,
		IdentityDigest: e.identity,
		ValueDigest:    e.valueDigest(),
		Resource:       e.resource,
	}
}

// ownedJSONAggregateWork is the desired JSON aggregate state for one output
// key. Incomplete state is per file so one malformed aggregate cannot suppress
// pruning in an independent sibling file.
type ownedJSONAggregateWork struct {
	entries    []ownedJSONEntry
	incomplete map[string]bool
	seen       map[string]bool
}

func (w *ownedJSONAggregateWork) add(entry ownedJSONEntry) {
	if w.seen == nil {
		w.seen = make(map[string]bool)
	}
	id := entry.rel + "\x00" + ownedAggregateIDKey(entry.id())
	if w.seen[id] {
		slog.Debug("owned JSON aggregate desired entry collapsed as a duplicate identity",
			"file", entry.rel, "kind", string(entry.kind), "container", entry.container,
			"key", entry.key, "resource", entry.resource)
		return
	}
	w.seen[id] = true
	w.entries = append(w.entries, entry)
}

func (w *ownedJSONAggregateWork) markIncomplete(rel string) {
	if w.incomplete == nil {
		w.incomplete = make(map[string]bool, 2)
	}
	w.incomplete[rel] = true
}

// ownedJSONFileSpec describes the structural policy needed by the common JSON
// reconciler. Target adapters supply it; the reconciler does not infer target
// semantics from path strings.
type ownedJSONFileSpec struct {
	rel            string
	container      string
	keyed          bool
	rawValueDigest bool
}

func ownedJSONDecode(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(value)
}

func canonicalOwnedJSON(data []byte) ([]byte, error) {
	var value any
	if err := ownedJSONDecode(data, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func ownedJSONHookEntries(rel string, events map[string][]json.RawMessage, resource string) ([]ownedJSONEntry, error) {
	var entries []ownedJSONEntry
	for _, event := range sortedKeys(events) {
		for _, rule := range events[event] {
			raw, err := canonicalOwnedJSON(rule)
			if err != nil {
				return nil, fmt.Errorf("canonicalize %s entry: %w", rel, err)
			}
			entries = append(entries, ownedJSONEntry{
				rel:       rel,
				kind:      ownership.KindHook,
				container: event,
				raw:       raw,
				identity:  ownership.HookIdentityDigest(event, ownership.Digest(raw)),
				resource:  resource,
			})
		}
	}
	return entries, nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ownedAggregateIDKey(id ownership.EntryID) string {
	return string(id.Kind) + "\x00" + id.Container + "\x00" + id.Key + "\x00" + id.IdentityDigest
}

// ownedAggregateState keeps format-specific desired state behind the common
// ownership-reconciliation boundary. JSON hook arrays and TOML MCP tables keep
// their native patch models.
type ownedAggregateState struct {
	devin           map[outputKey]*ownedJSONAggregateWork
	codex           map[outputKey]*ownedJSONAggregateWork
	codexMCP        map[outputKey][]codexMCPEntry
	codexMCPBlocked map[outputKey]bool
}

// ownedAggregateHandler lets the materializer orchestrate owned aggregate
// files without knowing a target's filenames, identities, or patch format.
type ownedAggregateHandler interface {
	extract(ownedAggregateState, outputKey, []artifact.Artifact, string) ([]artifact.Artifact, error)
	markIncomplete(ownedAggregateState, outputKey, ir.ResourceKind)
	aggregateRels(ir.ResourceKind, bool) []string
	malformedDiagnostic(outputKey, string, error) diag.Diagnostic
	reconcile(context.Context, *Report, ownedAggregateState, outputKey, string, []harness.Destination, bool) *Report
}

func ownedAggregateHandlerFor(target hcc.Target) ownedAggregateHandler {
	switch target {
	case hcc.TargetDevin:
		return devinOwnedAggregateHandler{}
	case hcc.TargetCodex:
		return codexOwnedAggregateHandler{}
	default:
		return nil
	}
}

type ownedJSONReconcileOptions struct {
	rootID             string
	sessionName        string
	writeCode          string
	malformedCode      string
	shapeCode          string
	satisfiedCode      string
	prunePostponedCode string
	onWrite            func(*Report, outputKey, string, bool)
}

func ownedJSONShapeDiag(code string, key outputKey, outDir, rel string, err error) diag.Diagnostic {
	return ownedAggregateDiag(code, key, rel, ownedAggregateMessage(outDir, rel, err.Error()))
}

func recordOwnedAggregateWrite(report *Report, key outputKey, rootID, outDir, rel string) {
	targetReport := TargetReport{
		Target:       key.target,
		MachineScope: key.machineScope,
		RootID:       rootID,
		OutDir:       outDir,
		Written:      []string{rel},
	}
	report.PerRoot = append(report.PerRoot, targetReport)
	report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], targetReport)
}

func ownedAggregatePriorForApply(raw []byte, readErr error, proof ownership.Proof) (generation uint64, digest string) {
	generation = proof.Generation()
	if readErr == nil {
		digest = ownership.Digest(raw)
	}
	return generation, digest
}

func ownedAggregateLabel(outDir, rel string) string {
	switch {
	case rel == "":
		return outDir
	case outDir == "":
		return rel
	default:
		return filepath.Join(outDir, rel)
	}
}

func ownedAggregateMessage(outDir, rel, message string) string {
	if rel != "" {
		message = strings.TrimPrefix(message, rel+": ")
	}
	label := ownedAggregateLabel(outDir, rel)
	if label == "" || strings.Contains(message, label) {
		return message
	}
	return fmt.Sprintf("%s: %s", label, message)
}

func ownedAggregateProofDiags(key outputKey, outDir, rel string, err error) []diag.Diagnostic {
	if err == nil {
		return nil
	}
	if errors.Is(err, ownership.ErrStateUninitialized) {
		slog.Debug("owned aggregate has no ownership record yet; first use of this root, so every existing entry is unmanaged",
			"target", key.target, "machine_scope", key.machineScope, "root", outDir, "file", rel,
			"aggregate", ownedAggregateLabel(outDir, rel), "code", diag.CodeOwnershipStateMissing)
		return nil
	}
	return []diag.Diagnostic{ownedAggregateCodedDiag(key, outDir, rel, err, diag.CodeOwnershipStateCorrupt)}
}

func ownedAggregateDiag(code string, key outputKey, rel, message string) diag.Diagnostic {
	severity := diag.SeverityError
	if doc, ok := diag.DocForCode(code); ok {
		severity = doc.Severity
	}
	diagnostic := diag.Diagnostic{Code: code, Severity: severity, Target: string(key.target), Message: message}
	if rel != "" {
		diagnostic.Source = diag.SourceRef{File: rel}
	}
	return diagnostic
}

func ownedAggregateCodedDiag(key outputKey, outDir, rel string, err error, fallback string) diag.Diagnostic {
	code := fallback
	if coded, ok := diag.AsCodedError(err); ok && coded.Code != "" {
		code = coded.Code
	}
	source := rel
	if source == "" {
		if coded, ok := diag.AsCodedError(err); ok {
			source = coded.Source.File
		}
		if source == "" {
			source = outDir
		}
	}
	return ownedAggregateDiag(code, key, source, ownedAggregateMessage(outDir, rel, err.Error()))
}

type ownedAggregateLockReporter struct {
	key    outputKey
	outDir string

	mu          sync.Mutex
	waited      time.Duration
	attempts    int
	contended   bool
	unsupported bool
}

func newOwnedAggregateLockReporter(key outputKey, outDir string) *ownedAggregateLockReporter {
	return &ownedAggregateLockReporter{key: key, outDir: outDir}
}

// ownedAggregateLockAcquisitionHook lets tests count acquisitions made by one
// root session. Tests that set it must not run in parallel.
var ownedAggregateLockAcquisitionHook func(ownership.LockEvent)

func (r *ownedAggregateLockReporter) observe(event ownership.LockEvent) {
	if ownedAggregateLockAcquisitionHook != nil {
		ownedAggregateLockAcquisitionHook(event)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch event.Kind {
	case ownership.LockAcquired:
		if event.Waited > r.waited {
			r.waited = event.Waited
			r.attempts = event.Attempts
		}
	case ownership.LockContended:
		r.contended = true
	case ownership.LockUnsupported:
		r.unsupported = true
	}
}

func (r *ownedAggregateLockReporter) diags() []diag.Diagnostic {
	r.mu.Lock()
	defer r.mu.Unlock()
	var diagnostics []diag.Diagnostic
	if r.unsupported {
		diagnostics = append(diagnostics, ownedAggregateDiag(diag.CodeOwnershipLockUnavailable, r.key, "",
			fmt.Sprintf("advisory locking is unavailable for %s, so ownership mutations for this root were not serialized against other processes",
				ownedAggregateLabel(r.outDir, ""))))
	}
	if r.contended && r.waited >= ownership.LockContendedReportThreshold {
		diagnostics = append(diagnostics, ownedAggregateDiag(diag.CodeOwnershipLockContended, r.key, "",
			fmt.Sprintf("waited %s (%d attempts) for another agent-marketplace process to release the ownership lock for %s",
				r.waited.Round(time.Millisecond), r.attempts, ownedAggregateLabel(r.outDir, ""))))
	}
	return diagnostics
}

func ownedAggregateLockSessionDiag(key outputKey, outDir string, err error, fallback string) diag.Diagnostic {
	diagnostic := ownedAggregateCodedDiag(key, outDir, "", err, fallback)
	diagnostic.Message = fmt.Sprintf("%s: this target root was skipped and its aggregate files were left exactly as found: %s",
		ownedAggregateLabel(outDir, ""), diagnostic.Message)
	return diagnostic
}
