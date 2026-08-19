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
// output-relative path — see devinAggregateLabel, which is what makes two
// aggregates under one root distinguishable), the non-secret entry identity (an
// MCP server object key or a hook event name), resource names, and digests are
// reported.
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
	"sort"
	"strings"
	"sync"
	"time"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Aggregate container member names. `mcpServers` is the object MCP entries are
// keyed in; `hooks` is the config.json member that carries the hook object at
// user scope. Both are fixed vocabulary, never user content.
const (
	devinMcpServersKey = "mcpServers"
	devinHooksKey      = "hooks"

	// devinEmptyDocument is the base document used when an aggregate does not
	// exist yet. An absent file has no unmanaged content to preserve, so
	// starting from `{}` cannot lose anything.
	devinEmptyDocument = "{}\n"
)

// ownedJSONEntry is one desired aggregate entry with its ownership identity.
//
// raw holds the CANONICAL bytes of the entry value (sorted object keys, no
// insignificant whitespace) so that the digest recorded in ownership state is
// reproducible from the file on a later run: an existing entry is canonicalized
// the same way before its digest is compared.
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

// ownedJSONAggregateWork is the desired aggregate state for one output key.
type ownedJSONAggregateWork struct {
	// entries is in insertion order, which is the deterministic bundle resource
	// order the plan requires ("Generated hooks use deterministic resource
	// ordering after handwritten hooks").
	entries []ownedJSONEntry
	// incomplete records, PER FILE, that a resource which would have
	// contributed to that file was skipped this run. Pruning is suppressed for
	// exactly those files, which is narrower than internal/materialize's
	// output-key-wide `blocked` map: an erroring hook no longer stops
	// mcp_config.json (or the skills/ directory) from pruning.
	incomplete map[string]bool
	// seen collapses duplicate desired identities; see add.
	seen map[string]bool
}

// add appends a desired entry unless an identical identity was already
// collected. "Duplicate desired hook tuples collapse before patching" (plan,
// entry identities); the same first-wins rule applies to a duplicated MCP key so
// two resources can never produce two members with one key.
func (w *ownedJSONAggregateWork) add(entry ownedJSONEntry) {
	if w.seen == nil {
		w.seen = make(map[string]bool)
	}
	id := entry.rel + "\x00" + devinIDKey(entry.id())
	if w.seen[id] {
		slog.Debug("devin aggregate desired entry collapsed as a duplicate identity",
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
		parsed, err := ownedJSONFragmentEntries(a.Path, a.Bytes, resource)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, parsed...)
	}
	return rest, entries, nil
}

// devinFragmentEntries decodes one compiled aggregate fragment into desired
// entries. The fragment shapes are fixed by the Devin compilers:
//
//	hooks.v1.json   {"<Event>": [ {rule} ]}
//	config.json     {"hooks": {"<Event>": [ {rule} ]}}
//	mcp_config.json {"mcpServers": {"<key>": {server}}}
func ownedJSONFragmentEntries(rel string, data []byte, resource string) ([]ownedJSONEntry, error) {
	switch rel {
	case harness.DevinMcpConfigFile:
		var doc map[string]map[string]json.RawMessage
		if err := devinDecode(data, &doc); err != nil {
			return nil, fmt.Errorf("decode %s fragment: %w", rel, err)
		}
		var entries []ownedJSONEntry
		for _, key := range sortedKeys(doc[devinMcpServersKey]) {
			raw, err := devinCanonicalJSON(doc[devinMcpServersKey][key])
			if err != nil {
				return nil, fmt.Errorf("canonicalize %s entry: %w", rel, err)
			}
			entries = append(entries, ownedJSONEntry{
				rel:       rel,
				kind:      ownership.KindMcpServer,
				container: devinMcpServersKey,
				key:       key,
				raw:       raw,
				identity:  ownership.McpIdentityDigest(key),
				resource:  resource,
			})
		}
		return entries, nil
	case harness.DevinProjectHooksFile:
		var doc map[string][]json.RawMessage
		if err := devinDecode(data, &doc); err != nil {
			return nil, fmt.Errorf("decode %s fragment: %w", rel, err)
		}
		return devinHookEntries(rel, doc, resource)
	case harness.DevinUserConfigFile:
		fallthrough
	case codexHooksFile:
		var doc map[string]map[string][]json.RawMessage
		if err := devinDecode(data, &doc); err != nil {
			return nil, fmt.Errorf("decode %s fragment: %w", rel, err)
		}
		return devinHookEntries(rel, doc[devinHooksKey], resource)
	default:
		return nil, fmt.Errorf("unknown Devin aggregate %q", rel)
	}
}

func devinHookEntries(rel string, events map[string][]json.RawMessage, resource string) ([]ownedJSONEntry, error) {
	var entries []ownedJSONEntry
	for _, event := range sortedKeys(events) {
		for _, rule := range events[event] {
			raw, err := devinCanonicalJSON(rule)
			if err != nil {
				return nil, fmt.Errorf("canonicalize %s entry: %w", rel, err)
			}
			identity, err := devinHookIdentity(event, raw)
			if err != nil {
				return nil, fmt.Errorf("identity for %s entry: %w", rel, err)
			}
			entries = append(entries, ownedJSONEntry{
				rel:       rel,
				kind:      ownership.KindHook,
				container: event,
				raw:       raw,
				identity:  identity,
				resource:  resource,
			})
		}
	}
	return entries, nil
}

// devinHookIdentity returns the total identity digest for a hook rule. The
// identity is derived from the hook event and the digest of the canonical JSON
// rule, so any difference in any byte of the canonical value or in the event
// produces a different identity. The canonical rule is the same bytes that are
// written for the entry and that become the ownership ValueDigest, so identity
// equality implies value equality.
//
// The rule must already be canonical JSON (sorted keys, no insignificant
// whitespace). Both devinHookEntries and devinExistingEntries canonicalize
// before calling this function.
func devinHookIdentity(event string, rule []byte) (string, error) {
	return ownership.HookIdentityDigest(event, ownership.Digest(rule)), nil
}

// devinDecode is strict JSON decoding with number tokens preserved, so a large
// integer timeout cannot be reshaped by a float64 round trip.
func devinDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// devinCanonicalJSON renders a JSON value with object keys sorted and no
// insignificant whitespace. It is the single normalization used for BOTH desired
// entries and existing on-disk entries, which is what makes a value digest
// comparable across runs and across formatting.
func devinCanonicalJSON(data []byte) ([]byte, error) {
	var value any
	if err := devinDecode(data, &value); err != nil {
		return nil, err
	}
	// encoding/json sorts map keys and emits json.Number verbatim.
	return json.Marshal(value)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// devinAggregateReport is the per-file outcome, folded into the run Report.
type devinAggregateReport struct {
	written bool
	diags   []diag.Diagnostic
}

// reconcileDevinAggregates reconciles every Devin aggregate file for one output
// key. Each file is independent: "Aggregate updates are atomic per file, not per
// target ... Malformed destination JSON blocks that file only."
//
// # The critical section, and why it spans the whole of this function
//
// Everything from before store.Recover to after the last iteration of the rels
// loop runs inside ONE [ownership.Store.WithWriteSessionContext]. The boundary is
// forced, not chosen, and three separate facts pick it:
//
//   - The CALLER'S READ must be inside it. reconcileDevinAggregateFile reads the
//     aggregate bytes and then store.Proof, computes a patch from both, and only
//     then calls store.Apply. Apply's own acquisition cannot serialize a read its
//     caller already performed, so two processes could both read, then commit one
//     after the other, and the second patch — computed from bytes the first had
//     already replaced — silently DROPPED the first one's entry. That lost update
//     is the residual defect this section closes; it reproduced 60/60.
//   - Recover must be inside it. Recover is a WRITER: it rolls an incomplete
//     generation forward, rolls it back, or abandons it. Outside the section,
//     process B's Recover can abandon process A's genuinely in-flight generation.
//   - The loop must be inside it AS A WHOLE, not per file. The journal is one slot
//     per canonical root and Record.Generation is one counter covering all of the
//     root's files, so releasing between files would let another process interleave
//     a generation into the same counter — and would also let a contended root burn
//     the whole lock timeout once PER FILE (two Devin aggregates per scope ⇒ 60 s)
//     before reporting anything. One outer section collapses that to one wait.
//
// The inner Apply, Recover, and BackupOnly acquisitions become reentrant depth
// bumps on this Store rather than second descriptors. They are SEQUENTIAL, which
// is what keeps them clear of [ownership.ErrNestedMutation]: that guard sits inside
// withWriteLock and refuses a mutation that starts while another is half written,
// not a widened session that contains several one after another.
//
// # Dry run takes no lock at all
//
// `sync --check`, `status`, `list`, the TUI background refresh and `doctor`'s
// preview all reach this function only through WithDryRun. Dry run already skips
// Recover, BackupOnly and Apply, so it must also skip the session: acquiring would
// create locks/<rootKey>.lock under the state root — breaking Open's documented
// side-effect-free contract — and would make a read-only command block behind a
// long-running sync.
func reconcileDevinAggregates(ctx context.Context, report *Report, key outputKey, outDir string, work *ownedJSONAggregateWork, dryRun bool) *Report {
	if key.target != hcc.TargetDevin {
		return report
	}
	info, ok := harness.TargetInfoMap[key.target]
	if !ok {
		return report
	}
	rels := append([]string(nil), info.AggregateFilesForScope(key.machineScope)...)
	return reconcileOwnedJSONAggregates(ctx, report, key, outDir, rels, work, dryRun)
}

func reconcileCodexHooks(ctx context.Context, report *Report, key outputKey, outDir string, work *ownedJSONAggregateWork, dryRun bool) *Report {
	return reconcileOwnedJSONAggregates(ctx, report, key, outDir, []string{codexHooksFile}, work, dryRun)
}

func reconcileOwnedJSONAggregates(ctx context.Context, report *Report, key outputKey, outDir string, rels []string, work *ownedJSONAggregateWork, dryRun bool) *Report {
	rels = append([]string(nil), rels...)
	sort.Strings(rels)

	desiredByRel := make(map[string][]ownedJSONEntry, len(rels))
	if work != nil {
		for _, entry := range work.entries {
			desiredByRel[entry.rel] = append(desiredByRel[entry.rel], entry)
		}
	}
	// Nothing desired and no file present means there is no aggregate work at
	// all; do not open a store or create empty files for it.
	anyWork := len(desiredByRel) > 0
	for _, rel := range rels {
		if _, err := os.Lstat(filepath.Join(outDir, rel)); err == nil {
			anyWork = true
		}
	}
	if !anyWork {
		return report
	}
	if !dryRun {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			report.Diags = append(report.Diags, devinDiag(diag.CodeMaterializeDevinAggregateWrite, key, "", fmt.Sprintf("create aggregate root %s: %v", outDir, err)))
			return report
		}
	}

	// The observer is how a WAIT reaches the report. A contended acquisition that
	// eventually succeeds returns no error, so without it the user is never told
	// why their sync paused; and a degradation is a property of the root that must
	// be reported once, not once per file.
	lockReport := newDevinLockReporter(key, outDir)
	store, err := ownership.Open(outDir, ownership.WithLockObserver(lockReport.observe))
	if err != nil {
		report.Diags = append(report.Diags, devinDiag(diag.CodeMaterializeDevinAggregateWrite, key, "",
			fmt.Sprintf("open ownership store for %s: %v", outDir, err)))
		return report
	}

	// Dry run: no session, no lock, no state-root writes. See the doc comment.
	if dryRun {
		return appendDevinAggregateReports(report, store, key, outDir, rels, desiredByRel, work, true)
	}

	sessionErr := store.WithWriteSessionContext(ctx, "reconcile-devin-aggregates", func() error {
		// "On startup, incomplete generations are replayed or rolled back
		// according to the recorded phase" — once, before any mutation, and
		// INSIDE the section so exactly one process ever resolves a journal.
		outcomes, recoverErr := store.Recover()
		for _, outcome := range outcomes {
			report.Diags = append(report.Diags, devinDiag(diag.CodeOwnershipRecovered, key, outcome.File,
				fmt.Sprintf("recovered an incomplete ownership generation %d for %q (phase %s, action %s)",
					outcome.Generation, devinAggregateLabel(outDir, outcome.File), outcome.Phase, outcome.Action)))
		}
		if recoverErr != nil {
			report.Diags = append(report.Diags, devinAggregateCodedDiag(key, outDir, "", recoverErr, diag.CodeMaterializeDevinAggregateWrite))
			return nil
		}
		report = appendDevinAggregateReports(report, store, key, outDir, rels, desiredByRel, work, false)
		return nil
	})
	// The body never returns an error — every per-file failure is already a
	// diagnostic — so anything here is an ACQUISITION failure and the whole root
	// was skipped without a byte written. Report it against this root and let
	// every other root continue.
	if sessionErr != nil {
		report.Diags = append(report.Diags, devinLockSessionDiag(key, outDir, sessionErr))
		return report
	}
	report.Diags = append(report.Diags, lockReport.diags()...)
	return report
}

// appendDevinAggregateReports runs the per-file loop and folds each file's outcome
// into the report. It is a function rather than an inline loop only so the dry-run
// path can reuse it verbatim without a session around it.
func appendDevinAggregateReports(report *Report, store *ownership.Store, key outputKey, outDir string, rels []string,
	desiredByRel map[string][]ownedJSONEntry, work *ownedJSONAggregateWork, dryRun bool) *Report {
	for _, rel := range rels {
		fileReport := reconcileDevinAggregateFile(store, key, outDir, rel, desiredByRel[rel], work != nil && work.incomplete[rel], dryRun)
		report.Diags = append(report.Diags, fileReport.diags...)
		if !fileReport.written {
			continue
		}
		appendCodexTrustReviewDiagnostic(report, key, rel, dryRun)
		tr := TargetReport{Target: key.target, MachineScope: key.machineScope, RootID: aggregateRootID(key), OutDir: outDir, Written: []string{rel}}
		report.PerRoot = append(report.PerRoot, tr)
		report.PerTarget[key.target] = mergeTargetReports(report.PerTarget[key.target], tr)
	}
	return report
}

func aggregateRootID(key outputKey) string {
	if key.target != hcc.Target(targetmeta.TargetCodex) {
		return ""
	}
	if key.machineScope {
		return "machine-codex"
	}
	return "repo-codex"
}

// devinLockReporter turns the per-root lock's observable transitions into report
// diagnostics, exactly once each per root.
//
// Two of the three lock outcomes are invisible to the caller otherwise. A WAIT
// that succeeds returns no error at all — it is the mechanism working, so it is
// Info — and a DEGRADATION proceeds successfully, so it too returns nothing. Only
// a timeout arrives as an error, and that one is reported from the session's return
// value instead (see devinLockSessionDiag) because it also decides that the root
// is skipped.
//
// It is called from acquisition, which happens on this goroutine inside
// WithWriteSessionContext, but a Store makes no promise about which goroutine
// observes: the mutex is cheap and keeps this safe under -race regardless.
//
// Where the Info diagnostic surfaces, stated rather than assumed: it reaches
// Report.Diags and the structured log (slog.Info in ownership's acquireLock).
// internal/pipeline forwards only warnings and errors to the CLI output, which is
// its pre-existing policy for EVERY Info code and not something this reconciler
// decides — so a waiting sync is visible in the log and in the report, and a
// consumer that wants to print it has it in Report.Diags.
type devinLockReporter struct {
	key    outputKey
	outDir string

	mu          sync.Mutex
	waited      time.Duration
	attempts    int
	contended   bool
	unsupported bool
}

func newDevinLockReporter(key outputKey, outDir string) *devinLockReporter {
	return &devinLockReporter{key: key, outDir: outDir}
}

// devinLockAcquisitionHook is nil in production and is the seam a test uses to
// count ACQUISITIONS from outside the reconciler.
//
// It exists because the section boundary is not observable any other way: a
// per-root session and a per-file one produce the same diagnostics and the same
// bytes on an uncontended root, and differ only in how many times the lock is
// taken (once, versus once for Recover plus once per aggregate file). That count is
// exactly what makes a contended root wait once instead of once per file, so it is
// the property worth pinning — and pinning it needs a hook, because installing an
// ownership.LockObserver from a test would mean plumbing one through
// materialize.Run's whole option set for a single assertion.
//
// A test that installs it must not run in parallel with another that does.
var devinLockAcquisitionHook func(ownership.LockEvent)

func (r *devinLockReporter) observe(ev ownership.LockEvent) {
	if devinLockAcquisitionHook != nil {
		devinLockAcquisitionHook(ev)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch ev.Kind {
	case ownership.LockAcquired:
		// Only a first acquisition carries a wait; reentries do not.
		if ev.Waited > r.waited {
			r.waited = ev.Waited
			r.attempts = ev.Attempts
		}
	case ownership.LockContended:
		r.contended = true
	case ownership.LockUnsupported:
		r.unsupported = true
	}
}

func (r *devinLockReporter) diags() []diag.Diagnostic {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []diag.Diagnostic
	if r.unsupported {
		out = append(out, devinDiag(diag.CodeOwnershipLockUnavailable, r.key, "",
			fmt.Sprintf("advisory locking is unavailable for %s, so ownership mutations for this root were not serialized against other processes",
				devinAggregateLabel(r.outDir, ""))))
	}
	// ownership.LockContendedReportThreshold, not a local copy: the same number
	// gates the slog.Info inside acquireLock, and two copies could drift into a
	// diagnostic with no log line behind it (or the reverse).
	if r.contended && r.waited >= ownership.LockContendedReportThreshold {
		out = append(out, devinDiag(diag.CodeOwnershipLockContended, r.key, "",
			fmt.Sprintf("waited %s (%d attempts) for another agent-marketplace process to release the ownership lock for %s",
				r.waited.Round(time.Millisecond), r.attempts, devinAggregateLabel(r.outDir, ""))))
	}
	return out
}

// devinLockSessionDiag reports a root that was skipped because its ownership lock
// could not be acquired.
//
// The code comes from the coded error itself — ownership.lock-timeout for a
// contended root, and every other acquisition failure keeps its own — so the
// diagnostic cannot mislabel the cause. It names the ROOT rather than a file,
// because no file was reached: the root's aggregates are byte-identical to what
// they were before this run.
//
// One case has no code of its own and falls back to
// materialize.devin.aggregate-write: a CANCELLED run, where the acquisition
// abandoned its wait because the user pressed Ctrl-C. The fallback's summary ("that
// file was left exactly as found") and hint ("nothing was partially written;
// re-run sync") are both true of it, and inventing a code is a codes.go plus
// codes.md pair that belongs with the bead that owns cancellation reporting rather
// than being smuggled in here. The message says plainly that the root was skipped,
// and the wrapped error says why.
func devinLockSessionDiag(key outputKey, outDir string, err error) diag.Diagnostic {
	d := devinAggregateCodedDiag(key, outDir, "", err, diag.CodeMaterializeDevinAggregateWrite)
	d.Message = fmt.Sprintf("%s: this target root was skipped and its aggregate files were left exactly as found: %s",
		devinAggregateLabel(outDir, ""), d.Message)
	return d
}

// devinPriorForApply returns the compare-and-swap fields for a Mutation built
// from the aggregate as it was read. The generation comes from the ownership
// proof; the digest comes from the live file bytes, and is empty when the file
// was absent when the caller read it.
func devinPriorForApply(raw []byte, readErr error, proof ownership.Proof) (gen uint64, digest string) {
	gen = proof.Generation()
	if readErr == nil {
		digest = ownership.Digest(raw)
	}
	return
}

// reconcileDevinAggregateFile patches ONE aggregate file.
func reconcileDevinAggregateFile(store *ownership.Store, key outputKey, outDir, rel string, desired []ownedJSONEntry, incomplete, dryRun bool) devinAggregateReport {
	var out devinAggregateReport
	abs := filepath.Join(outDir, rel)

	raw, readErr := os.ReadFile(abs)
	switch {
	case readErr == nil:
	case os.IsNotExist(readErr):
		if len(desired) == 0 {
			return out
		}
		raw = []byte(devinEmptyDocument)
	default:
		out.diags = append(out.diags, devinDiag(diag.CodeMaterializeDevinAggregateWrite, key, rel,
			fmt.Sprintf("read aggregate %q: %v", devinAggregateLabel(outDir, rel), readErr)))
		return out
	}

	doc, parseErr := jsonc.Parse(rel, raw)
	if parseErr != nil {
		// "Malformed JSON is safety-backed up before parsing, left unchanged, and
		// reported with a precise parse location."
		//
		// The backup is taken HERE rather than literally before jsonc.Parse
		// because parsing cannot mutate the file: the bytes backed up are
		// byte-identical either way, and the malformed branch is the only one
		// that needs a backup Store.Apply will not take. Backing up
		// unconditionally instead would fsync two files on every no-op sync and,
		// worse, churn the bounded five-backup retention window until the
		// genuinely valuable pre-first-merge backup was evicted.
		if readErr == nil && !dryRun {
			if _, err := store.BackupOnly(rel, abs, ownership.ReasonSync); err != nil {
				// The coded error's OWN code wins over ownership.safety-backup,
				// exactly as the Recover and Apply sites do it. Hardcoding the
				// backup code here mislabelled every acquisition failure on this
				// path — a lock timeout was reported as "the mandatory raw-byte
				// safety backup could not be written", which points a user at
				// state-directory permissions for a problem that is another
				// process holding the lock.
				out.diags = append(out.diags, devinAggregateCodedDiag(key, outDir, rel,
					fmt.Errorf("safety backup for %q failed: %w", devinAggregateLabel(outDir, rel), err),
					diag.CodeOwnershipSafetyBackup))
			}
		}
		out.diags = append(out.diags, devinAggregateCodedDiag(key, outDir, rel, parseErr, diag.CodeMaterializeDevinAggregateMalformed))
		return out
	}

	container, containerMember, shapeErr := ownedJSONContainerNode(doc, rel)
	if shapeErr != nil {
		out.diags = append(out.diags, devinShapeDiag(key, outDir, rel, shapeErr))
		return out
	}
	if container == nil && len(desired) == 0 {
		// The container does not exist and nothing is desired, so there is
		// neither anything to write nor anything that could be stale.
		return out
	}

	var existing []devinExistingEntry
	if container != nil {
		existing, shapeErr = devinExistingEntries(rel, container)
		if shapeErr != nil {
			out.diags = append(out.diags, devinShapeDiag(key, outDir, rel, shapeErr))
			return out
		}
	}

	proof, proofErr := store.Proof(rel)
	// Advisory: the zero Proof already classifies everything as unmanaged, so
	// reconciliation continues safely (preserve, refuse, never prune) whether or
	// not the situation is worth reporting.
	out.diags = append(out.diags, devinProofDiags(key, outDir, rel, proofErr)...)

	desiredValues := make([]ownership.EntryValue, 0, len(desired))
	for _, entry := range desired {
		desiredValues = append(desiredValues, ownership.EntryValue{ID: entry.id(), ValueDigest: entry.valueDigest()})
	}
	existingValues := make([]ownership.EntryValue, 0, len(existing))
	for _, entry := range existing {
		existingValues = append(existingValues, entry.value)
	}
	drift := ownership.Classify(proof, existingValues, desiredValues)

	plan := devinPatchPlan(outDir, rel, desired, existing, drift, proof, incomplete)
	out.diags = append(out.diags, plan.diags(key)...)

	if applyErr := devinApplyPatch(doc, container, containerMember, rel, plan); applyErr != nil {
		out.diags = append(out.diags, devinShapeDiag(key, outDir, rel, applyErr))
		return out
	}
	if !doc.Changed() {
		slog.Debug("devin aggregate already in sync", "target", key.target, "machine_scope", key.machineScope,
			"file", rel, "owned_entries", len(plan.owned))
		return out
	}
	next := doc.Bytes()
	if dryRun {
		out.written = true
		return out
	}

	priorGen, priorDigest := devinPriorForApply(raw, readErr, proof)
	result, applyErr := store.Apply(ownership.Mutation{
		Rel:                   rel,
		AbsFile:               abs,
		NextBytes:             next,
		Entries:               plan.owned,
		Reason:                ownership.ReasonSync,
		ExpectPriorGeneration: priorGen,
		ExpectPriorDigest:     priorDigest,
	})
	if applyErr != nil {
		out.diags = append(out.diags, devinAggregateCodedDiag(key, outDir, rel, applyErr, diag.CodeMaterializeDevinAggregateWrite))
		return out
	}
	slog.Info("devin aggregate reconciled", "target", key.target, "machine_scope", key.machineScope,
		"file", rel, "generation", result.Generation, "owned_entries", len(plan.owned),
		"pruned", len(plan.prune), "collisions", len(plan.collisions), "replaced_drifted", len(plan.drifted))
	out.written = true
	return out
}

// devinContainerNode returns the object node whose members are the aggregate's
// entries, plus the root member name it lives under:
//
//   - mcp_config.json: the `mcpServers` member of the root object.
//   - hooks.v1.json: the root object itself, which "contains the hook object
//     directly rather than nesting it under a top-level `hooks` key". Its
//     member name is "" because there is no enclosing member.
//   - config.json: the `hooks` member, so unrelated settings are untouched.
//
// A nil container with a nil error means the member does not exist yet. It is
// deliberately NOT created here: jsonc.SetMember stores raw JSON as an opaque
// literal node, so an empty object created that way could not then be patched
// member-by-member. An absent container holds no unmanaged content and no
// comments, so devinApplyPatch renders it whole instead.
func ownedJSONContainerNode(doc *jsonc.Document, rel string) (container *jsonc.Node, member string, err error) {
	root := doc.Root()
	if root.Kind() != jsonc.KindObject {
		return nil, "", fmt.Errorf("aggregate %q root is not a JSON object; it was left unchanged", rel)
	}
	switch rel {
	case harness.DevinProjectHooksFile:
		return root, "", nil
	case harness.DevinMcpConfigFile:
		member = devinMcpServersKey
	case harness.DevinUserConfigFile, codexHooksFile:
		member = devinHooksKey
	default:
		return nil, "", fmt.Errorf("unknown ownership-reconciled JSON aggregate %q", rel)
	}
	node, ok := root.Member(member)
	if !ok {
		return nil, member, nil
	}
	if node.Kind() != jsonc.KindObject {
		return nil, member, fmt.Errorf("aggregate %q member %q is not a JSON object; it was left unchanged", rel, member)
	}
	return node, member, nil
}

// devinExistingEntry pairs an on-disk entry's ownership identity with where it
// lives, so a patch can address it without re-deriving anything.
type devinExistingEntry struct {
	value ownership.EntryValue
	// container is the hook event name or "mcpServers".
	container string
	// raw is the entry's EXACT on-disk bytes. Preserving an unmanaged entry
	// means writing these bytes back, not a re-serialization of them.
	raw []byte
}

// devinExistingEntries reads the current entries out of the container node.
func devinExistingEntries(rel string, container *jsonc.Node) ([]devinExistingEntry, error) {
	if rel == harness.DevinMcpConfigFile {
		var out []devinExistingEntry
		for _, key := range container.Keys() {
			node, _ := container.Member(key)
			canonical, err := jsonc.Canonicalize(rel, node.Raw())
			if err != nil {
				return nil, fmt.Errorf("aggregate %q entry %q is not valid JSON; it was left unchanged", rel, key)
			}
			normalized, err := devinCanonicalJSON(canonical)
			if err != nil {
				return nil, fmt.Errorf("aggregate %q entry %q could not be normalized; it was left unchanged", rel, key)
			}
			out = append(out, devinExistingEntry{
				value: ownership.EntryValue{
					ID:          ownership.EntryID{Kind: ownership.KindMcpServer, Container: devinMcpServersKey, Key: key, IdentityDigest: ownership.McpIdentityDigest(key)},
					ValueDigest: ownership.Digest(normalized),
				},
				container: devinMcpServersKey,
				raw:       node.Raw(),
			})
		}
		return out, nil
	}
	var out []devinExistingEntry
	for _, event := range container.Keys() {
		node, _ := container.Member(event)
		if node.Kind() != jsonc.KindArray {
			// A non-array hook event is not a shape this target writes. It is
			// left alone rather than rewritten, and it contributes no entries,
			// so nothing about it can be pruned either.
			slog.Warn("devin hook event is not an array; it was left unchanged",
				"file", rel, "event", event)
			continue
		}
		for i := 0; i < node.Len(); i++ {
			element, _ := node.Element(i)
			canonical, err := jsonc.Canonicalize(rel, element.Raw())
			if err != nil {
				return nil, fmt.Errorf("aggregate %q event %q entry %d is not valid JSON; it was left unchanged", rel, event, i)
			}
			normalized, err := devinCanonicalJSON(canonical)
			if err != nil {
				return nil, fmt.Errorf("aggregate %q event %q entry %d could not be normalized; it was left unchanged", rel, event, i)
			}
			identity, err := devinHookIdentity(event, normalized)
			if err != nil {
				return nil, fmt.Errorf("aggregate %q event %q entry %d has no derivable identity; it was left unchanged", rel, event, i)
			}
			valueDigest := ownership.Digest(normalized)
			if rel == codexHooksFile {
				valueDigest = ownership.Digest(element.Raw())
			}
			out = append(out, devinExistingEntry{
				value: ownership.EntryValue{
					ID:          ownership.EntryID{Kind: ownership.KindHook, Container: event, IdentityDigest: identity},
					ValueDigest: valueDigest,
				},
				container: event,
				raw:       element.Raw(),
			})
		}
	}
	return out, nil
}

// devinPatch is the fully decided set of edits for one aggregate file. It is
// computed before any mutation so the decision logic is testable without a
// filesystem, and so a refused collision can never be half-applied.
type devinPatch struct {
	// outDir is the install root the aggregate lives under. It exists purely so
	// every reported decision can name WHICH aggregate it is about: Devin has two
	// roots and several aggregates per root, so "mcp_config.json" alone is
	// ambiguous. No patching decision reads it.
	outDir string
	rel    string
	// write maps a container key ("mcpServers" key, or hook event) to the entry
	// bytes to store there.
	writeMcp map[string][]byte
	// hookElements maps a hook event to its complete new element list:
	// preserved handwritten elements first, in their original order, then
	// generated ones in deterministic resource order.
	hookElements map[string][][]byte
	// deleteMcp / deleteHookEvent are keys whose entries are pruned entirely.
	deleteMcp       []string
	deleteHookEvent []string
	// owned is the entry set the new content claims: what ownership state
	// records for the next generation.
	owned []ownership.Entry

	collisions      []ownership.EntryID
	drifted         []ownership.EntryID
	prune           []ownership.EntryID
	satisfied       []ownedJSONEntry
	prunePostponed  []ownership.EntryID
	pruneSuppressed bool
}

// devinPatchPlan decides every edit for one file.
//
// The safety rules it implements, each quoted from the plan:
//
//   - "An unmanaged MCP key collision is preserved and causes only the
//     generated entry to fail" — a colliding desired key is dropped from the
//     patch and reported; the existing member is never written or deleted.
//   - "Manual edits to a proven owned entry are reported as drift and replaced
//     from marketplace configuration."
//   - "Stale proven owned entries are pruned" — and ONLY those: the prune list
//     is DriftReport.Stale, which ownership.Classify derives from Proof.OwnedIDs.
//     No filesystem or content heuristic contributes to it.
//   - "One semantically identical unmanaged hook may satisfy any number of
//     selected marketplace resources without duplication or adoption."
//   - "Handwritten hooks keep their relative order and execute before generated
//     hooks."
//   - "A partial run does not prune" — when incomplete is set, the prune list is
//     emptied for this file.
func devinPatchPlan(outDir, rel string, desired []ownedJSONEntry, existing []devinExistingEntry, drift ownership.DriftReport, proof ownership.Proof, incomplete bool) devinPatch {
	patch := devinPatch{outDir: outDir, rel: rel, writeMcp: map[string][]byte{}, hookElements: map[string][][]byte{}}

	collision := make(map[string]bool, len(drift.Collisions))
	for _, id := range drift.Collisions {
		collision[devinIDKey(id)] = true
	}
	drifted := make(map[string]bool, len(drift.Drifted))
	for _, id := range drift.Drifted {
		drifted[devinIDKey(id)] = true
	}
	stale := make(map[string]bool, len(drift.Stale))
	for _, id := range drift.Stale {
		stale[devinIDKey(id)] = true
	}
	if incomplete {
		patch.pruneSuppressed = true
		patch.prunePostponed = drift.Stale
		stale = map[string]bool{}
	} else {
		patch.prune = drift.Stale
	}
	patch.drifted = drift.Drifted
	if rel == harness.DevinMcpConfigFile {
		// Only a KEYED primitive can genuinely collide: "An unmanaged MCP key
		// collision is preserved and causes only the generated entry to fail."
		// A hook has no key, so an existing unmanaged entry with the same
		// identity IS the same behaviour and SATISFIES the desired entry rather
		// than conflicting with it (see the hook branch below).
		patch.collisions = drift.Collisions
	}

	if rel == harness.DevinMcpConfigFile {
		for _, entry := range desired {
			if collision[devinIDKey(entry.id())] {
				continue // preserved unmanaged entry wins; only ours fails
			}
			patch.writeMcp[entry.key] = entry.raw
			patch.owned = append(patch.owned, entry.ownershipEntry())
		}
		for _, id := range drift.Stale {
			if stale[devinIDKey(id)] {
				patch.deleteMcp = append(patch.deleteMcp, id.Key)
			}
		}
		retainPostponedOwnership(&patch, existing, drift.Stale, proof, incomplete)
		return patch
	}

	// Hooks. Identity is the event plus the digest of the canonical hook value,
	// so a desired entry whose identity already exists UNMANAGED is byte-identical
	// to that handwritten entry and is satisfied by it rather than being a
	// collision: adding ours would duplicate the behaviour.
	unmanagedIdentities := make(map[string]bool, len(existing))
	for _, id := range drift.Unmanaged {
		unmanagedIdentities[devinIDKey(id)] = true
	}

	desiredByEvent := make(map[string][]ownedJSONEntry)
	events := map[string]bool{}
	for _, entry := range desired {
		if unmanagedIdentities[devinIDKey(entry.id())] {
			patch.satisfied = append(patch.satisfied, entry)
			continue
		}
		desiredByEvent[entry.container] = append(desiredByEvent[entry.container], entry)
		events[entry.container] = true
	}
	existingByEvent := make(map[string][]devinExistingEntry)
	for _, entry := range existing {
		existingByEvent[entry.container] = append(existingByEvent[entry.container], entry)
		events[entry.container] = true
	}

	for _, event := range sortedKeys(events) {
		var elements [][]byte
		for _, entry := range existingByEvent[event] {
			id := devinIDKey(entry.value.ID)
			if stale[id] {
				continue // proven ours and no longer desired: pruned
			}
			if drifted[id] {
				continue // proven ours and hand-edited: replaced below
			}
			if devinDesires(desiredByEvent[event], entry.value.ID) {
				// Proven ours and still desired: the desired copy is appended
				// below, after every handwritten element, because "Handwritten
				// hooks keep their relative order and execute before generated
				// hooks".
				continue
			}
			// Unmanaged — preserve the EXACT source bytes, so handwritten
			// formatting and any comment attached to this element survive.
			elements = append(elements, entry.raw)
		}
		for _, entry := range desiredByEvent[event] {
			elements = append(elements, entry.raw)
			patch.owned = append(patch.owned, entry.ownershipEntry())
		}
		if len(elements) == 0 {
			patch.deleteHookEvent = append(patch.deleteHookEvent, event)
			continue
		}
		patch.hookElements[event] = elements
	}
	retainPostponedOwnership(&patch, existing, drift.Stale, proof, incomplete)
	return patch
}

func retainPostponedOwnership(patch *devinPatch, existing []devinExistingEntry, stale []ownership.EntryID, proof ownership.Proof, incomplete bool) {
	if !incomplete {
		return
	}
	for _, entry := range existing {
		if !containsOwnershipID(stale, entry.value.ID) {
			continue
		}
		resource, _ := proof.Resource(entry.value.ID)
		patch.owned = append(patch.owned, ownership.Entry{Kind: entry.value.ID.Kind, Container: entry.value.ID.Container, Key: entry.value.ID.Key, IdentityDigest: entry.value.ID.IdentityDigest, ValueDigest: entry.value.ValueDigest, Resource: resource})
	}
}

func containsOwnershipID(ids []ownership.EntryID, want ownership.EntryID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// devinDesires reports whether an identity is in a desired slice.
func devinDesires(desired []ownedJSONEntry, id ownership.EntryID) bool {
	want := devinIDKey(id)
	for _, entry := range desired {
		if devinIDKey(entry.id()) == want {
			return true
		}
	}
	return false
}

func devinIDKey(id ownership.EntryID) string {
	return string(id.Kind) + "\x00" + id.Container + "\x00" + id.Key + "\x00" + id.IdentityDigest
}

// devinApplyPatch applies a decided patch to the parsed document. Every edit
// goes through pkg/harness-compilation/jsonc, so untouched members keep their
// exact bytes, their comments, and their position.
func devinApplyPatch(doc *jsonc.Document, container *jsonc.Node, containerMember, rel string, patch devinPatch) error {
	if container == nil {
		// The container member does not exist yet, so the whole member is
		// rendered from the patch. Nothing handwritten can be lost: there was
		// nothing inside it to preserve.
		raw, err := devinFreshContainerRaw(rel, patch)
		if err != nil {
			return err
		}
		if raw == nil {
			return nil
		}
		if !doc.SetMember(doc.Root(), containerMember, raw) {
			return fmt.Errorf("aggregate %q could not create member %q", rel, containerMember)
		}
		return nil
	}
	if rel == harness.DevinMcpConfigFile {
		for _, key := range sortedKeys(patch.writeMcp) {
			if !doc.SetMember(container, key, patch.writeMcp[key]) {
				return fmt.Errorf("aggregate %q could not set member %q", rel, key)
			}
		}
		for _, key := range patch.deleteMcp {
			doc.DeleteMember(container, key)
		}
		return nil
	}
	for _, event := range sortedKeys(patch.hookElements) {
		elements := patch.hookElements[event]
		node, ok := container.Member(event)
		if !ok || node.Kind() != jsonc.KindArray {
			raw, err := devinHookArrayRaw(elements)
			if err != nil {
				return err
			}
			if !doc.SetMember(container, event, raw) {
				return fmt.Errorf("aggregate %q could not set hook event %q", rel, event)
			}
			continue
		}
		if !doc.SetElements(node, elements) {
			return fmt.Errorf("aggregate %q could not set hook event %q elements", rel, event)
		}
	}
	for _, event := range patch.deleteHookEvent {
		doc.DeleteMember(container, event)
	}
	return nil
}

// devinFreshContainerRaw renders a whole absent container member from a patch.
// Everything in the patch is generated content, so the result is canonical JSON.
func devinFreshContainerRaw(rel string, patch devinPatch) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	written := 0
	writeKey := func(key string) error {
		if written > 0 {
			buf.WriteByte(',')
		}
		written++
		keyRaw, err := json.Marshal(key)
		if err != nil {
			return err
		}
		buf.Write(keyRaw)
		buf.WriteByte(':')
		return nil
	}
	if rel == harness.DevinMcpConfigFile {
		for _, key := range sortedKeys(patch.writeMcp) {
			if err := writeKey(key); err != nil {
				return nil, err
			}
			buf.Write(patch.writeMcp[key])
		}
	} else {
		for _, event := range sortedKeys(patch.hookElements) {
			if err := writeKey(event); err != nil {
				return nil, err
			}
			raw, err := devinHookArrayRaw(patch.hookElements[event])
			if err != nil {
				return nil, err
			}
			buf.Write(raw)
		}
	}
	buf.WriteByte('}')
	if written == 0 {
		return nil, nil
	}
	if !json.Valid(buf.Bytes()) {
		return nil, fmt.Errorf("aggregate %q generated container is not valid JSON", rel)
	}
	return buf.Bytes(), nil
}

// devinHookArrayRaw renders a fresh hook event array. It is only used when the
// event member does not exist yet, so no handwritten formatting is at stake.
func devinHookArrayRaw(elements [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, element := range elements {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(element)
	}
	buf.WriteByte(']')
	if !json.Valid(buf.Bytes()) {
		return nil, errors.New("generated hook array is not valid JSON")
	}
	return buf.Bytes(), nil
}

// diags renders the patch's decisions as diagnostics. Exactly one code per
// decision class, and nothing but identities and file paths in the messages.
//
// Every message names the aggregate through [devinAggregateLabel], so a
// collision in ~/.config/devin/mcp_config.json is distinguishable from one in
// <repo>/.devin/mcp_config.json. A basename alone cannot tell a user which
// scope, which root, or even which of a root's aggregates to go and look at.
func (p devinPatch) diags(key outputKey) []diag.Diagnostic {
	var out []diag.Diagnostic
	file := devinAggregateLabel(p.outDir, p.rel)
	for _, id := range p.collisions {
		out = append(out, diag.Diagnostic{
			Code:     diag.CodeOwnershipUnmanagedKey,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("collision: %s entry %q in %q already exists and is not proven owned; the existing entry was preserved and only the generated entry failed",
				id.Kind, devinIdentityLabel(id), file),
			Hint: "Rename the marketplace resource, or run doctor --repair to adopt the existing entry when it matches exactly",
		})
	}
	for _, id := range p.drifted {
		out = append(out, diag.Diagnostic{
			Code:     diag.CodeOwnershipEntryDrift,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("drift: %s entry %q in %q was edited by hand and was replaced from marketplace configuration",
				id.Kind, devinIdentityLabel(id), file),
			Hint: "Edit the marketplace resource instead: an owned entry is regenerated on every sync",
		})
	}
	for _, entry := range p.satisfied {
		out = append(out, diag.Diagnostic{
			Code:     diag.CodeMaterializeDevinHookSatisfiedByUnmanaged,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Resource: entry.resource,
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("a handwritten %q hook in %q is semantically identical to this resource's hook; no generated entry was added",
				entry.container, file),
			Hint: "No action needed; the existing handwritten entry already provides the behaviour and is neither adopted nor duplicated",
		})
	}
	if p.pruneSuppressed && len(p.prunePostponed) > 0 {
		out = append(out, diag.Diagnostic{
			Code:     diag.CodeMaterializeDevinAggregatePrunePostponed,
			Severity: diag.SeverityWarning,
			Target:   string(key.target),
			Source:   diag.SourceRef{File: p.rel},
			Message: fmt.Sprintf("%d stale owned entries in %q were not pruned because a resource was skipped this run",
				len(p.prunePostponed), file),
			Hint: "Fix the reported resource errors and re-run sync; a skipped resource is indistinguishable from a removed one",
		})
	}
	return out
}

// devinIdentityLabel renders the non-secret identity of an entry: an MCP server
// object key, or a hook event name. It never renders a matcher or a payload.
func devinIdentityLabel(id ownership.EntryID) string {
	if id.Key != "" {
		return id.Key
	}
	return id.Container
}

// devinAggregateLabel names the AGGREGATE a diagnostic is about: its install
// root plus its output-relative path.
//
// The root is not decoration. Devin has two roots (<repo>/.devin and
// ~/.config/devin) and each holds several aggregates, so a bare
// "mcp_config.json" cannot tell a user which file — or even which scope — a
// warning concerns, and two warnings about two different files in one root
// render byte-identically. The joined absolute path is unique per aggregate, so
// it is both the identifying and the deduplicating form.
func devinAggregateLabel(outDir, rel string) string {
	switch {
	case rel == "":
		return outDir
	case outDir == "":
		return rel
	default:
		return filepath.Join(outDir, rel)
	}
}

// devinAggregateMessage makes the aggregate the SUBJECT of a message.
//
// It is the single place the aggregate identity is stitched into user-visible
// text, so every Devin aggregate diagnostic identifies the same way. A leading
// bare "<rel>: " is dropped first because the absolute label subsumes it.
func devinAggregateMessage(outDir, rel, message string) string {
	if rel != "" {
		message = strings.TrimPrefix(message, rel+": ")
	}
	label := devinAggregateLabel(outDir, rel)
	if label == "" || strings.Contains(message, label) {
		return message
	}
	return fmt.Sprintf("%s: %s", label, message)
}

// devinDiagSummary joins a slice of diagnostics into a compact, deterministic
// summary suitable for error messages. It uses only the stable code and the
// already-redaction-safe message of each diagnostic.
func devinDiagSummary(diags []diag.Diagnostic) string {
	if len(diags) == 0 {
		return "no diagnostics"
	}
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		if d.Code != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", d.Code, d.Message))
		} else {
			parts = append(parts, d.Message)
		}
	}
	return strings.Join(parts, "; ")
}

// devinShapeDiag reports an aggregate whose shape cannot be patched. The shape
// errors name the output-relative path only, so the aggregate label is added.
func devinShapeDiag(key outputKey, outDir, rel string, err error) diag.Diagnostic {
	return devinDiag(diag.CodeMaterializeDevinAggregateShape, key, rel,
		devinAggregateMessage(outDir, rel, err.Error()))
}

// devinProofDiags reports an advisory [ownership.Store.Proof] error.
//
// The Proof error is advisory in every case (the zero Proof already means
// "nothing is owned"), so this function only decides WHO hears about it:
//
//   - A first-ever use of a root has no record because nothing was ever
//     recorded for it. Nothing was lost, nothing is actionable, and the run can
//     succeed completely — so it is a debug fact, not a warning. Reporting it
//     would put a scary line in the report of every user's very first sync and
//     teach them to ignore the code when it later means something.
//   - Any other missing record (this root WAS recorded before) and any corrupt
//     record stay user-facing diagnostics, unchanged.
//
// What is classified never changes: both branches keep the zero Proof, so every
// existing entry is unmanaged, collisions are refused, and nothing is pruned.
func devinProofDiags(key outputKey, outDir, rel string, err error) []diag.Diagnostic {
	if err == nil {
		return nil
	}
	if errors.Is(err, ownership.ErrStateUninitialized) {
		slog.Debug("devin aggregate has no ownership record yet; first use of this root, so every existing entry is unmanaged",
			"target", key.target, "machine_scope", key.machineScope, "root", outDir, "file", rel,
			"aggregate", devinAggregateLabel(outDir, rel), "code", diag.CodeOwnershipStateMissing)
		return nil
	}
	return []diag.Diagnostic{devinAggregateCodedDiag(key, outDir, rel, err, diag.CodeOwnershipStateCorrupt)}
}

func devinDiag(code string, key outputKey, rel, message string) diag.Diagnostic {
	severity := diag.SeverityError
	if doc, ok := diag.DocForCode(code); ok {
		severity = doc.Severity
	}
	d := diag.Diagnostic{Code: code, Severity: severity, Target: string(key.target), Message: message}
	if rel != "" {
		d.Source = diag.SourceRef{File: rel}
	}
	return d
}

// devinAggregateCodedDiag converts an error about ONE aggregate (a file, or the
// root itself when rel is empty) into a diagnostic whose SUBJECT is that
// aggregate. It prefers the error's own stable code (and its catalogued
// severity) over the fallback, which is what makes an ownership failure surface
// as ownership.safety-backup or json.duplicate-key rather than being flattened
// into one generic code.
//
// What it deliberately does NOT do is let the error's own source path become the
// subject. An ownership error carries the durable state file as its source
// (<state>/state.json), which is shared by every aggregate under the root: as a
// subject it makes two diagnostics about two different files byte-identical and
// tells the user nothing about the file at stake. Here the aggregate leads and
// the error's own path follows inside the message, so both are available and the
// two diagnostics differ.
func devinAggregateCodedDiag(key outputKey, outDir, rel string, err error, fallback string) diag.Diagnostic {
	code := fallback
	if coded, ok := diag.AsCodedError(err); ok && coded.Code != "" {
		code = coded.Code
	}
	// A coded error already renders its own source path (for an ownership error
	// that is the state file, for a parse error the aggregate itself), so the
	// message is kept whole and the aggregate is prepended to it: both paths stay
	// available, and the aggregate is what makes two of these differ.
	//
	// The Source stays the output-relative aggregate path: it is what the rest of
	// the report keys files by, and the absolute label is already in the message.
	//
	// A ROOT-level error (rel == "") has no aggregate path to key by, and a
	// diagnostic with no Source at all is worse than one keyed by the error's own
	// path: it cannot be attributed to a file, a root, or anything else, and any
	// consumer that groups by Source collapses every root-level failure of a run
	// into one bucket. So the error's own source (for an ownership error, the
	// durable state file, which is per-root) is kept, and the install root is the
	// last resort.
	source := rel
	if source == "" {
		if coded, ok := diag.AsCodedError(err); ok {
			source = coded.Source.File
		}
		if source == "" {
			source = outDir
		}
	}
	return devinDiag(code, key, source, devinAggregateMessage(outDir, rel, err.Error()))
}
