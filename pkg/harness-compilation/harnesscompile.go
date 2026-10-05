// Package harnesscompile converts AI-agent configuration resources from
// a canonical source format into vendor-specific layouts.
//
// It supports seven conversion targets (Claude Code, Factory Droid, Cursor,
// Windsurf, Gemini CLI, Devin CLI, and Codex), each with a registered compiler
// and ingester.
package harnesscompile

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// Target represents a compilation target harness. Its identity and declared
// metadata are owned by the dependency-free target registry.
type Target = targetmeta.Target

const (
	TargetClaude   = targetmeta.TargetClaude
	TargetFactory  = targetmeta.TargetFactory
	TargetCursor   = targetmeta.TargetCursor
	TargetWindsurf = targetmeta.TargetWindsurf
	TargetGemini   = targetmeta.TargetGemini
	TargetDevin    = targetmeta.TargetDevin
	TargetCodex    = targetmeta.TargetCodex
)

// Harness is a backward-compatible alias for Target. It allows callers to
// express source and target harness identity uniformly.
type Harness = Target

// Harness constants are backward-compatible aliases for the Target constants.
const (
	HarnessClaude   Harness = TargetClaude
	HarnessFactory  Harness = TargetFactory
	HarnessCursor   Harness = TargetCursor
	HarnessWindsurf Harness = TargetWindsurf
	HarnessGemini   Harness = TargetGemini
	HarnessDevin    Harness = TargetDevin
	HarnessCodex    Harness = TargetCodex
)

// TargetDefinition is the shared metadata declaration for a target.
type TargetDefinition = targetmeta.Definition

// TargetDefinitions returns the declared targets in stable display order.
func TargetDefinitions() []TargetDefinition { return targetmeta.Definitions() }

// TargetNames returns the declared target names in stable display order.
func TargetNames() []string { return targetmeta.Names() }

// Targets returns the declared target identifiers in stable display order.
func Targets() []Target { return targetmeta.Targets() }

// IsValidTarget reports whether name is a declared target.
func IsValidTarget(name string) bool { return targetmeta.IsValid(name) }

// ValidateTargetRegistrations checks the integration boundary between the
// dependency-free target declarations and the explicit compiler/ingester
// registrations. It validates both directions: every declared capability must
// have a registration, and every registration must belong to a target that
// declares that capability. It is intended for startup diagnostics and
// completeness tests, not for registering implementations implicitly.
func ValidateTargetRegistrations() error {
	definitions := targetmeta.Definitions()
	var errs []error
	seen := make(map[targetmeta.Target]bool, len(definitions))
	for _, definition := range definitions {
		t := Target(definition.Target)
		if seen[definition.Target] {
			errs = append(errs, fmt.Errorf("target %q is declared more than once", t))
		}
		seen[definition.Target] = true
		if err := targetmeta.ValidateDefinition(definition); err != nil {
			errs = append(errs, err)
		}
		if definition.Compiler {
			if _, ok := compile.Lookup(compile.Target(t)); !ok {
				errs = append(errs, fmt.Errorf("target %q declares a compiler but none is registered", t))
			}
		}
		if definition.Ingester {
			if _, ok := ingest.For(ingest.Harness(t)); !ok {
				errs = append(errs, fmt.Errorf("target %q declares an ingester but none is registered", t))
			}
		}
		if definition.Conversion && (!definition.Compiler || !definition.Ingester) {
			errs = append(errs, fmt.Errorf("target %q declares conversion without compiler and ingester capabilities", t))
		}
		if definition.UI && definition.Label == "" {
			errs = append(errs, fmt.Errorf("target %q has UI metadata without a label", t))
		}
		if definition.PartitionedRoots {
			if _, ok := targetmeta.LookupNativeLayout(definition.Target); !ok {
				errs = append(errs, fmt.Errorf("target %q declares partitioned roots but registers no native layout", t))
			}
		}
	}
	if len(definitions) == 0 {
		errs = append(errs, errors.New("target registry must not be empty"))
	}
	// Validate every declared native layout at the registration boundary so a
	// malformed descriptor fails here instead of surfacing as a mis-routed
	// artifact at materialization time.
	if err := targetmeta.ValidateNativeLayouts(); err != nil {
		errs = append(errs, err)
	}
	// Reverse direction: a registration for an undeclared target, or for a
	// target that does not declare the capability, is a wiring bug.
	for _, t := range compile.RegisteredTargets() {
		definition, ok := targetmeta.Lookup(string(t))
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("compiler registered for undeclared target %q", t))
		case !definition.Compiler:
			errs = append(errs, fmt.Errorf("compiler registered for target %q which does not declare a compiler", t))
		}
	}
	for _, h := range ingest.RegisteredSources() {
		definition, ok := targetmeta.Lookup(string(h))
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("ingester registered for undeclared target %q", h))
		case !definition.Ingester:
			errs = append(errs, fmt.Errorf("ingester registered for target %q which does not declare an ingester", h))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// errSourceNotImplemented is returned by Convert and IngestBundle when the
// source harness has no registered ingester.
var errSourceNotImplemented = errors.New("source harness not implemented — no ingester registered")

// Re-export types from sub-packages for convenience.
type (
	ResourceKind = ir.ResourceKind
	Resource     = ir.Resource

	Artifact             = artifact.Artifact
	Diagnostic           = diag.Diagnostic
	Severity             = diag.Severity
	ReconciliationMode   = artifact.ReconciliationMode
	ReconciliationPolicy = artifact.ReconciliationPolicy
)

// Severity constants re-exported from diag.
const (
	SeverityError   = diag.SeverityError
	SeverityWarning = diag.SeverityWarning
	SeverityInfo    = diag.SeverityInfo

	ReconciliationReplace  = artifact.ReconciliationReplace
	ReconciliationMarker   = artifact.ReconciliationMarker
	ReconciliationExternal = artifact.ReconciliationExternal
)

// ResourceKind constants re-exported from ir.
const (
	KindSkill     = ir.KindSkill
	KindCommand   = ir.KindCommand
	KindSubagent  = ir.KindSubagent
	KindHook      = ir.KindHook
	KindMcpServer = ir.KindMcpServer
	KindRule      = ir.KindRule
)

// Compile compiles a single resource into target-specific artifacts.
// It delegates to the compile package's registry-driven dispatcher.
func Compile(r Resource, t Target, opts ...Option) (*Result, error) {
	cfg := &config{}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	res, err := compile.Compile(r, compile.Target(t), compileOptions(cfg)...)
	if err != nil {
		return nil, err
	}
	return &Result{
		Artifacts:   res.Artifacts,
		Diagnostics: res.Diagnostics,
	}, nil
}

// Convert compiles an entire harness-specific source directory from one harness
// format into target-specific artifacts for another harness. It uses the ingest
// registry to load the source bundle and then compiles each resource to the
// target format.
//
// Convert(srcFS, HarnessClaude, target, opts...) is equivalent to
// CompileBundle(srcFS, target, opts...).
func Convert(srcFS fs.FS, from, to Harness, opts ...Option) (*Result, error) {
	slog.Info("harness conversion start", "from", from, "to", to)
	if from == to {
		// Identity: still go through ingest→compile so that diagnostics
		// and artifact merging are consistent, but this is effectively a
		// round-trip. We do NOT short-circuit because the user may want
		// to see diagnostics about lossy fields.
		_ = from
	}

	ing, ok := ingest.For(ingest.Harness(from))
	if !ok {
		return nil, fmt.Errorf("harnesscompile.Convert: source %q: %w", from, errSourceNotImplemented)
	}

	slog.Info("harness ingest start", "from", from)
	b, err := ing.LoadBundle(srcFS)
	if err != nil {
		// If the ingester cannot find the expected layout, return an empty
		// result rather than an error. This preserves backward compatibility
		// with CompileBundle, which returned an empty result for an empty FS.
		if !ing.Detect(srcFS) {
			return &Result{}, nil
		}
		return nil, fmt.Errorf("harnesscompile.Convert: ingest %s: %w", from, err)
	}
	slog.Info("harness ingest end", "from", from, "resources", len(b.Resources), "diagnostics", len(b.Diagnostics))

	result, err := compileBundleFromIR(b, to, opts...)
	if err != nil {
		slog.Error("harness conversion failed", "from", from, "to", to, "error", err)
		return nil, err
	}
	slog.Info("harness conversion end", "from", from, "to", to, "resources", len(b.Resources), "artifacts", len(result.Artifacts), "diagnostics", len(result.Diagnostics))
	return result, nil
}

// IngestBundle reads all resources from a harness-specific source directory and
// returns them as an IR Bundle. The source harness is determined by the from
// parameter, which must have a registered ingester.
func IngestBundle(srcFS fs.FS, from Harness) (*ingest.Bundle, error) {
	slog.Info("harness ingest start", "from", from)
	ing, ok := ingest.For(ingest.Harness(from))
	if !ok {
		return nil, fmt.Errorf("harnesscompile.IngestBundle: source %q: %w", from, errSourceNotImplemented)
	}
	bundle, err := ing.LoadBundle(srcFS)
	if err != nil {
		slog.Error("harness ingest failed", "from", from, "error", err)
		return nil, err
	}
	slog.Info("harness ingest end", "from", from, "resources", len(bundle.Resources), "diagnostics", len(bundle.Diagnostics))
	return bundle, nil
}

// Autodetect probes the given filesystem for harness-specific layout markers
// and returns the detected source harness. Returns ("", false) if no known
// harness layout is detected.
//
// Detection delegates to the ingest package's registry-driven Autodetect.
func Autodetect(srcFS fs.FS) (Harness, bool) {
	h, ok := ingest.Autodetect(srcFS)
	return Harness(h), ok
}

// CompileBundle compiles an entire Claude-plugin directory into target-specific artifacts.
// It is a backward-compatible convenience wrapper around
// Convert(srcFS, HarnessClaude, t, opts...).
//
// It walks the Claude-plugin layout:
//   - .claude-plugin/plugin.json (manifest)
//   - skills/<name>/ → LoadSkill → Compile
//   - commands/<name>.md → LoadCommand → Compile
//   - agents/<name>.md → LoadSubagent → Compile
//   - hooks/hooks.json → LoadHooks → Compile (per hook)
//   - .mcp.json → LoadMcpServers → Compile (per server)
//
// All results are merged, fragments are coalesced, and duplicate artifact
// paths produce an error diagnostic (bundle.conflict.duplicate-artifact-path).
func CompileBundle(src fs.FS, t Target, opts ...Option) (*Result, error) {
	return Convert(src, HarnessClaude, Harness(t), opts...)
}

// resourceOutput records which bundle resource produced which artifacts so
// path conflicts can name the conflicting resources instead of reporting an
// anonymous duplicate path, and so a failing resource can be held responsible
// for exactly its own artifacts.
type resourceOutput struct {
	kind      ir.ResourceKind
	name      string
	artifacts []Artifact
}

// SkippedResource attributes suppressed bundle output to the resource that
// caused it. A skipped resource contributes NO artifacts to the bundle: not
// its own files and not its fragments of a shared aggregate.
//
// It carries identity and diagnostic codes only. It never carries a resource
// body, a hook payload, an MCP environment value, a header, an OAuth secret,
// or a permission value, so it is safe to log and to render in a UI.
type SkippedResource struct {
	Kind  ir.ResourceKind
	Name  string
	Codes []string
}

// logResourceSkip records a suppression decision. Only identity, diagnostic
// codes, and counts are logged — never resource bodies, payloads, credentials,
// or permission values.
func logResourceSkip(t Target, kind ir.ResourceKind, name string, codes []string, suppressedArtifacts int, reason string) {
	slog.Warn("bundle resource skipped",
		"target", t,
		"kind", kind,
		"resource", name,
		"codes", strings.Join(codes, ","),
		"suppressed_artifacts", suppressedArtifacts,
		"reason", reason)
}

// errorDiagnosticCodes returns the codes of the error-severity diagnostics in
// diagnostics, in emission order and without duplicates. An empty result means
// the resource compiled cleanly enough to publish.
func errorDiagnosticCodes(diagnostics []Diagnostic) []string {
	var codes []string
	for _, d := range diagnostics {
		if !d.IsError() {
			continue
		}
		if sliceContains(codes, d.Code) {
			continue
		}
		codes = append(codes, d.Code)
	}
	return codes
}

// resolveArtifactPathConflicts flattens per-resource artifacts and reports
// non-fragment output-path collisions.
//
// A collision is a BUNDLE-level failure: it is not attributable to one
// resource, because neither resource is wrong on its own. The plan requires
// that "Bundle compilation emits a stable error naming both resources; neither
// silently wins", so both producers are suppressed as whole resources — every
// artifact they emitted, fragments included.
//
// Suppressing the fragments too is what keeps a shared aggregate honest. A
// Claude subagent emits both agents/<name>.md and a .claude-plugin/plugin.json
// fragment; dropping only the colliding .md would leave the manifest
// advertising an agent whose file was never written.
//
// Ordering is the bundle resource order, so diagnostics, artifacts, and the
// returned skip list are deterministic.
func resolveArtifactPathConflicts(outputs []resourceOutput, t Target) ([]Artifact, []Diagnostic, []SkippedResource) {
	type producer struct {
		index int
		kind  ir.ResourceKind
		name  string
	}
	owners := make(map[string]producer)
	suppressed := make(map[int][]string)
	var diagnostics []Diagnostic

	suppress := func(index int, code string) {
		if sliceContains(suppressed[index], code) {
			return
		}
		suppressed[index] = append(suppressed[index], code)
	}

	for i, output := range outputs {
		for _, a := range output.artifacts {
			if a.Fragment {
				continue
			}
			current := producer{index: i, kind: output.kind, name: output.name}
			previous, exists := owners[a.Path]
			if !exists {
				owners[a.Path] = current
				continue
			}
			code := diag.CodeBundleConflictDuplicateArtifactPath
			message := fmt.Sprintf("duplicate artifact path %q produced by %s %q and %s %q; neither resource was emitted",
				a.Path, previous.kind, previous.name, current.kind, current.name)
			hint := "Rename one of the conflicting resources or adjust the output path"
			if previous.kind != current.kind {
				code = diag.CodeBundleConflictCrossKindArtifactPath
				message = fmt.Sprintf("%s %q and %s %q both compile to %q; neither artifact was emitted",
					previous.kind, previous.name, current.kind, current.name, a.Path)
				hint = "Rename one of the conflicting resources: on this target a skill and a command cannot share a name"
			}
			diagnostics = append(diagnostics, Diagnostic{
				Code:     code,
				Severity: SeverityError,
				Target:   string(t),
				Resource: current.name,
				Message:  message,
				Hint:     hint,
			})
			slog.Error("bundle artifact path conflict", "target", t, "code", code, "path", a.Path,
				"first_kind", previous.kind, "first_resource", previous.name,
				"second_kind", current.kind, "second_resource", current.name)
			suppress(previous.index, code)
			suppress(current.index, code)
		}
	}

	var artifacts []Artifact
	var skipped []SkippedResource
	for i, output := range outputs {
		if codes, dropped := suppressed[i]; dropped {
			skipped = append(skipped, SkippedResource{Kind: output.kind, Name: output.name, Codes: codes})
			logResourceSkip(t, output.kind, output.name, codes, len(output.artifacts), "artifact path conflict")
			continue
		}
		artifacts = append(artifacts, output.artifacts...)
	}
	return artifacts, diagnostics, skipped
}

// compileBundleFromIR compiles all resources in an ingest.Bundle to the
// target harness. It is the generalized N-to-N compilation path used by
// Convert for all source harnesses.
//
// # Bundle error contract
//
// Suppression is resource-attributable, never bundle-wide:
//
//   - An ERROR diagnostic or compiler error suppresses that resource's complete
//     output, including its fragments of a shared aggregate, and records it in
//     Result.Skipped. Warnings and info retain all of the resource's artifacts.
//   - A bundle-level diagnostic suppresses only the explicitly implicated
//     producers. Ingest failures refer to resources with no artifacts; artifact
//     path conflicts suppress both named producers. No unrelated resource is
//     suppressed.
//
// Result.Diagnostics retains every diagnostic and Result.HasErrors reports any
// error, so callers can fail the run while still publishing valid output.
func compileBundleFromIR(b *ingest.Bundle, t Target, opts ...Option) (*Result, error) {
	cfg := &config{}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}

	copts := compileOptions(cfg)

	var allDiagnostics []Diagnostic

	// Carry forward ingest-side diagnostics.
	allDiagnostics = append(allDiagnostics, b.Diagnostics...)

	// Compile each resource in the bundle. A compiler failure is scoped to the
	// resource that caused it: retain the diagnostic and continue so callers get
	// a complete report for the bundle instead of the first error only.
	outputs := make([]resourceOutput, 0, len(b.Resources))
	var skipped []SkippedResource
	for _, r := range b.Resources {
		res, err := compile.Compile(r, compile.Target(t), copts...)
		kind, name, _ := ir.ValidateResource(r)
		if err != nil {
			allDiagnostics = append(allDiagnostics, Diagnostic{
				Code:     diag.CodeBundleCompileResourceFailed,
				Severity: SeverityError,
				Target:   string(t),
				Resource: name,
				Message:  fmt.Sprintf("failed to compile resource: %v", err),
				Hint:     "Fix the resource compiler error before materializing the bundle",
			})
			slog.Error("bundle resource compilation failed", "target", t, "resource", name, "error", err)
			skipped = append(skipped, SkippedResource{Kind: kind, Name: name, Codes: []string{diag.CodeBundleCompileResourceFailed}})
			logResourceSkip(t, kind, name, []string{diag.CodeBundleCompileResourceFailed}, 0, "resource compiler failed")
			continue
		}
		allDiagnostics = append(allDiagnostics, res.Diagnostics...)

		// Per-resource suppression. The resource that emitted the error is the
		// only one that loses output; every artifact it produced is dropped,
		// fragments of a shared aggregate included, so no aggregate can end up
		// advertising an entry whose resource failed.
		if codes := errorDiagnosticCodes(res.Diagnostics); len(codes) > 0 {
			skipped = append(skipped, SkippedResource{Kind: kind, Name: name, Codes: codes})
			logResourceSkip(t, kind, name, codes, len(res.Artifacts), "resource emitted error diagnostics")
			continue
		}
		outputs = append(outputs, resourceOutput{kind: kind, name: name, artifacts: res.Artifacts})
	}

	allArtifacts, conflictDiagnostics, conflictSkips := resolveArtifactPathConflicts(outputs, t)
	allDiagnostics = append(allDiagnostics, conflictDiagnostics...)
	skipped = append(skipped, conflictSkips...)

	// Merge fragment artifacts. Only fragments from resources that survived the
	// rules above take part, so an aggregate is complete with respect to the
	// resources that actually compiled, and every missing entry has its own
	// error diagnostic in allDiagnostics naming the resource that lost it.
	merged, err := artifact.MergeFragments(allArtifacts)
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Convert: merge fragments: %w", err)
	}

	if len(skipped) > 0 {
		slog.Warn("bundle compiled with skipped resources", "target", t,
			"resources", len(b.Resources), "skipped", len(skipped), "artifacts", len(merged))
	}

	return &Result{
		Artifacts:   merged,
		Diagnostics: allDiagnostics,
		Skipped:     skipped,
	}, nil
}

// ErrPartialResult reports that the materialized Result was not clean: at
// least one resource produced an error diagnostic and was skipped during
// compilation, so what reached disk is the output of the resources that
// compiled cleanly and nothing more.
//
// Materialize returns it WRAPPED, together with a fully populated non-nil
// *WriteReport whose Partial field is set. That pairing is deliberate: the plan
// requires that valid resources reach disk and that "sync exits non-zero if any
// error occurred", so the write must happen and the caller must still fail.
// A caller that ignores the error cannot mistake a partial run for a clean one,
// and a caller that wants to report what landed can read the report:
//
//	report, err := harnesscompile.Materialize(result, outDir)
//	if report != nil {
//		print(report) // partial or clean, this is what reached disk
//	}
//	if errors.Is(err, harnesscompile.ErrPartialResult) {
//		return err // wrote the valid resources, run is not clean, exit non-zero
//	}
//	if err != nil {
//		return err // non-partial failure; earlier renames or removals may have left partial disk changes
//	}
var ErrPartialResult = errors.New("result contains error diagnostics; only resources that compiled cleanly were materialized")

// ErrExternalReconciliationRequired reports that generic materialization was
// asked to write a shared file whose ownership must be reconciled externally.
var ErrExternalReconciliationRequired = errors.New("artifact requires external ownership reconciliation")

// materializeRootCaptureHook runs immediately before Materialize captures the
// output root for publication. It is nil in production and set only by serial
// tests in this package.
var materializeRootCaptureHook func(outDir string)

// Materialize writes a Result to disk using staged-then-rename for individual
// artifacts. It merges fragment artifacts before writing, validates paths, detects
// identical files via sha256, and supports dry-run and prune-stale modes.
//
// The overall operation is not atomic: a non-partial failure after earlier
// renames or stale-file removals may leave partial disk changes.
//
// # Partial results
//
// Materialize does not refuse a Result that carries error diagnostics. The
// promoted contract is per-resource: compilation already dropped every artifact
// of every failing resource (see compileBundleFromIR), so what remains is
// exactly the output of the resources that compiled cleanly, and refusing it
// would let one bad resource zero an entire materialization.
//
// Two things change for a partial result:
//
//   - WriteReport.Partial is true and WriteReport.SkippedResources lists the
//     suppressed resources, and the returned error wraps ErrPartialResult.
//   - Pruning is suppressed (WriteReport.PruneSuppressed) even when requested.
//     A skipped resource looks exactly like a removed resource to a stale-file
//     scan, so pruning a partial result would delete the still-good output of
//     the previous clean run. internal/materialize already disables pruning for
//     the same reason when a resource is blocked.
func Materialize(r *Result, outDir string, opts ...WriteOption) (report *WriteReport, err error) {
	if r == nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: result must not be nil")
	}
	partial := r.HasErrors()

	cfg := &writeConfig{}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}

	// Resolve ownership before fragment processing. Policy() translates old
	// Artifact literals, while an explicit typed policy remains authoritative;
	// refusing here guarantees no filesystem mutation for either representation.
	for _, candidate := range r.Artifacts {
		// Preserve the stable refusal for legacy external artifacts, including
		// old literals that omit fragment metadata. A non-zero typed policy is
		// authoritative and therefore intentionally ignores this stale field.
		if candidate.Reconciliation == (artifact.ReconciliationPolicy{}) && candidate.ExternalReconciliationRequired {
			return nil, fmt.Errorf("harnesscompile.Materialize: artifact %q: %w", candidate.Path, ErrExternalReconciliationRequired)
		}
		if candidate.Reconciliation != (artifact.ReconciliationPolicy{}) {
			policy, policyErr := candidate.Policy()
			if policyErr != nil {
				return nil, fmt.Errorf("harnesscompile.Materialize: artifact %q: invalid reconciliation policy: %w", candidate.Path, policyErr)
			}
			if policy.IsExternal() {
				return nil, fmt.Errorf("harnesscompile.Materialize: artifact %q: %w", candidate.Path, ErrExternalReconciliationRequired)
			}
		}
	}

	// Merge fragment artifacts before processing.
	artifacts, err := artifact.MergeFragments(r.Artifacts)
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: merge fragments: %w", err)
	}
	// Validate all artifact paths and reject symlinked destination parents
	// before any reads, writes, or stale-file discovery can follow them.
	if err := rejectSymlinkedPathComponents(outDir); err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: %w", err)
	}
	for _, a := range artifacts {
		if err := validateArtifactPath(a.Path, outDir); err != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: %w", err)
		}
		destPath := filepath.Join(outDir, filepath.FromSlash(a.Path))
		if err := rejectSymlinkedParentComponents(outDir, destPath); err != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: artifact %q: %w", a.Path, err)
		}
	}
	artifacts, err = coalesceDuplicateArtifacts(artifacts)
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: %w", err)
	}

	// Keep every output path for stale-file comparison, including a merge
	// artifact that is already up to date and will therefore be skipped.
	artifactPaths := make(map[string]struct{}, len(artifacts))
	for _, a := range artifacts {
		artifactPaths[a.Path] = struct{}{}
	}

	report = &WriteReport{OutDir: outDir, Partial: partial, SkippedResources: r.Skipped}
	regularArtifacts, mergeArtifacts, err := prepareMergeArtifacts(artifacts, outDir, report)
	if err != nil {
		return nil, err
	}
	// Merge artifacts are staged exactly like regular artifacts. Writing them
	// directly to outDir would leave a partially materialized result if a later
	// staged write or rename failed.
	artifacts = append(regularArtifacts, mergeArtifacts...)

	// Determine which artifacts to write vs skip.
	existingModes := make(map[string]fs.FileMode)
	originalFiles := make(map[string]*fsboundary.Identity, len(artifacts))
	for _, a := range artifacts {
		destPath := filepath.Join(outDir, filepath.FromSlash(a.Path))

		var original fsboundary.Identity
		matches, exists, err := fileMatchesBytes(destPath, a.Bytes, &original)
		if exists {
			originalFiles[a.Path] = &original
		}
		if err != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: stat %s: %w", a.Path, err)
		}
		if exists && a.MergeWithExisting {
			existingModes[a.Path] = original.Mode.Perm()
		}

		if exists && matches {
			report.Skipped = append(report.Skipped, a.Path)
			continue
		}

		if exists && cfg.mode == WriteModeFailIfExists {
			return nil, fmt.Errorf("harnesscompile.Materialize: file already exists: %s", a.Path)
		}

		report.Written = append(report.Written, a.Path)
	}

	// Compute stale files for prune mode. A partial result never prunes: the
	// output of a skipped resource is indistinguishable from the output of a
	// removed one, so pruning here would turn one unrepresentable resource into
	// the deletion of every sibling file the previous clean run wrote.
	pruneRequested := cfg.pruneStale || cfg.mode == WriteModeSync
	prune := pruneRequested && !partial
	if pruneRequested && partial {
		report.PruneSuppressed = true
		slog.Warn("materialize prune suppressed for partial result", "out_dir", outDir,
			"skipped_resources", len(r.Skipped))
	}
	if prune {
		stale, err := findStaleFiles(outDir, artifactPaths)
		if err != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: find stale: %w", err)
		}
		report.StaleCandidates = stale
		for _, candidate := range stale {
			report.Removed = append(report.Removed, candidate.Path)
		}
	}
	rootSnapshot, err := snapshotMaterializeRoot(outDir)
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: snapshot output root: %w", err)
	}
	if err := revalidateMaterializeRoot(outDir, rootSnapshot); err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: output root changed during planning: %w", err)
	}

	// Dry-run: return report without any I/O.
	if cfg.dryRun {
		return report, partialError(report)
	}

	if err := revalidateMaterializeRoot(outDir, rootSnapshot); err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: output root changed before publication: %w", err)
	}
	// Ensure outDir exists.
	if materializeRootCaptureHook != nil {
		materializeRootCaptureHook(outDir)
	}
	// Capture and validate the output root before creating the stage, so a
	// root setup failure never leaves an unregistered staging directory.
	absolute, err := filepath.Abs(outDir)
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: resolve outDir: %w", err)
	}
	ancestor := absolute
	base, err := fsboundary.OpenDir(ancestor)
	for errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && filepath.Dir(ancestor) != ancestor {
		ancestor = filepath.Dir(ancestor)
		base, err = fsboundary.OpenDir(ancestor)
	}
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: capture outDir: %w", err)
	}
	defer func() {
		if closeErr := base.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	rel, err := filepath.Rel(ancestor, absolute)
	if err != nil {
		return nil, err
	}
	root, err := base.EnsurePath(rel)
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: create outDir: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := root.NativeRoot()
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: capture outDir: %w", err)
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	rootInfo, err := native.Lstat(".")
	if err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: inspect outDir: %w", err)
	}
	if rootSnapshot.exists && !os.SameFile(rootSnapshot.info, rootInfo) {
		return nil, fmt.Errorf("%w: harnesscompile.Materialize: outDir replaced since it was checked", fsboundary.ErrGenerationConflict)
	}
	rootSnapshot = materializeRootSnapshot{exists: true, info: rootInfo}
	slog.Debug("SDK root admitted", "out_dir", outDir, "phase", "sdk-root-admitted")
	if rootErr := revalidateMaterializeRoot(outDir, rootSnapshot); rootErr != nil {
		return nil, errors.Join(fsboundary.ErrGenerationConflict, rootErr)
	}
	// Create staging directory.
	stageName := ".tmp-" + rand.Text()
	if err := root.Mkdir(stageName, 0o700); err != nil {
		return nil, fmt.Errorf("harnesscompile.Materialize: create staging dir: %w", err)
	}
	stagingDir := filepath.Join(outDir, stageName)
	created, err := native.Lstat(stageName)
	if err != nil {
		return nil, err
	}
	if !created.IsDir() {
		return nil, fsboundary.ErrUnsafeFile
	}
	cleanupStage := func() error {
		current, inspectErr := native.Lstat(stageName)
		if errors.Is(inspectErr, fs.ErrNotExist) {
			return nil
		}
		if inspectErr != nil {
			return errors.Join(fsboundary.ErrGenerationConflict, inspectErr)
		}
		if !os.SameFile(created, current) {
			return fsboundary.ErrGenerationConflict
		}
		return native.RemoveAll(stageName)
	}
	defer func() {
		if cleanupErr := cleanupStage(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	staged, err := root.OpenPath(stageName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := staged.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	createdID, err := fsboundary.FileInfoIdentity(created)
	if err != nil {
		return nil, err
	}
	if !staged.Identity().SameObject(createdID) {
		return nil, fsboundary.ErrGenerationConflict
	}
	stagedNative, err := staged.NativeRoot()
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := stagedNative.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	slog.Debug("SDK stage admitted", "out_dir", stagingDir, "phase", "sdk-stage-admitted")

	// Write artifacts to staging directory.
	for _, a := range artifacts {
		if sliceContains(report.Skipped, a.Path) {
			continue
		}
		parent, dirErr := staged.EnsurePath(filepath.Dir(filepath.FromSlash(a.Path)))
		if dirErr != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: mkdir %s: %w", filepath.Dir(a.Path), dirErr)
		}
		if closeErr := parent.Close(); closeErr != nil {
			return nil, closeErr
		}
		mode, existingMode := existingModes[a.Path]
		if !existingMode {
			mode = a.Mode
			if mode == 0 {
				mode = 0o644
			}
		}
		file, writeErr := stagedNative.OpenFile(filepath.FromSlash(a.Path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if writeErr != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: write %s: %w", a.Path, writeErr)
		}
		_, writeErr = file.Write(a.Bytes)
		if writeErr == nil && existingMode {
			writeErr = file.Chmod(mode)
		}
		if writeErr = errors.Join(writeErr, file.Close()); writeErr != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: write %s: %w", a.Path, writeErr)
		}
	}

	slog.Debug("SDK artifacts staged", "out_dir", outDir, "staged", stagingDir, "phase", "sdk-staged")
	// Move staged files onto outDir.
	for _, a := range artifacts {
		if sliceContains(report.Skipped, a.Path) {
			continue
		}
		if rootErr := revalidateMaterializeRoot(outDir, rootSnapshot); rootErr != nil {
			return nil, errors.Join(fsboundary.ErrGenerationConflict, rootErr)
		}
		currentStage, stageErr := native.Lstat(stageName)
		if stageErr != nil {
			return nil, errors.Join(fsboundary.ErrGenerationConflict, stageErr)
		}
		if !os.SameFile(created, currentStage) {
			return nil, fsboundary.ErrGenerationConflict
		}
		src := filepath.Join(stageName, filepath.FromSlash(a.Path))
		dst := filepath.Join(outDir, filepath.FromSlash(a.Path))
		if err := rejectSymlinkedParentComponents(outDir, dst); err != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: artifact %q: %w", a.Path, err)
		}
		parent, dirErr := root.EnsurePath(filepath.Dir(filepath.FromSlash(a.Path)))
		if dirErr != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: mkdir %s: %w", filepath.Dir(a.Path), dirErr)
		}
		if closeErr := parent.Close(); closeErr != nil {
			return nil, closeErr
		}
		currentFile, inspectErr := native.Lstat(filepath.FromSlash(a.Path))
		original := originalFiles[a.Path]
		if original == nil {
			if !errors.Is(inspectErr, fs.ErrNotExist) {
				return nil, errors.Join(fsboundary.ErrGenerationConflict, inspectErr)
			}
		} else {
			if inspectErr != nil {
				return nil, errors.Join(fsboundary.ErrGenerationConflict, inspectErr)
			}
			identity, identityErr := fsboundary.FileInfoIdentity(currentFile)
			if identityErr != nil {
				return nil, identityErr
			}
			if !identity.SameState(*original) {
				return nil, fsboundary.ErrGenerationConflict
			}
		}
		if err := native.Rename(src, filepath.FromSlash(a.Path)); err != nil {
			return nil, fmt.Errorf("harnesscompile.Materialize: rename %s: %w", a.Path, err)
		}
	}

	// Remove stale files only when the no-follow identity captured during
	// discovery still matches. A concurrent writer's replacement is preserved
	// and reported instead of being silently deleted.
	if prune {
		var pruneConflicts []string
		for _, candidate := range report.StaleCandidates {
			if rootErr := revalidateMaterializeRoot(outDir, rootSnapshot); rootErr != nil {
				return report, errors.Join(fsboundary.ErrGenerationConflict, rootErr)
			}
			stalePath := filepath.Join(outDir, filepath.FromSlash(candidate.Path))
			if err := rejectSymlinkedParentComponents(outDir, stalePath); err != nil {
				report.PruneConflicts = append(report.PruneConflicts, candidate.Path)
				return report, fmt.Errorf("harnesscompile.Materialize: stale file %q: %w: %v", candidate.Path, ErrStalePruneConflict, err)
			}
			if err := RemoveStaleFileCandidate(stalePath, candidate); err != nil {
				if errors.Is(err, ErrStalePruneConflict) {
					pruneConflicts = append(pruneConflicts, candidate.Path)
					slog.Warn("materialize stale prune preserved changed output", "out_dir", outDir, "path", candidate.Path)
					continue
				}
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("harnesscompile.Materialize: remove stale file %q: %w", candidate.Path, err)
			}
			cleanEmptyDirs(outDir, filepath.Dir(filepath.FromSlash(candidate.Path)), candidate.parents)
		}
		if len(pruneConflicts) > 0 {
			report.PruneConflicts = pruneConflicts
			return report, fmt.Errorf("harnesscompile.Materialize: %w: %s", ErrStalePruneConflict, strings.Join(pruneConflicts, ", "))
		}
	}

	// Remove staging directory.
	if cleanupErr := cleanupStage(); cleanupErr != nil {
		return report, cleanupErr
	}

	return report, partialError(report)
}

// partialError returns the ErrPartialResult wrapper for a report that describes
// a partial run, and nil for a clean one. The report is always returned to the
// caller alongside it: the write already happened for every resource that
// compiled cleanly.
func partialError(report *WriteReport) error {
	if report == nil || !report.Partial {
		return nil
	}
	codes := make([]string, 0, len(report.SkippedResources))
	for _, s := range report.SkippedResources {
		codes = append(codes, s.Codes...)
	}
	slog.Warn("materialize completed with a partial result", "out_dir", report.OutDir,
		"written", len(report.Written), "skipped_resources", len(report.SkippedResources),
		"prune_suppressed", report.PruneSuppressed, "codes", strings.Join(codes, ","))
	return fmt.Errorf("harnesscompile.Materialize: %w (%d resource(s) skipped, %d file(s) written to %s)",
		ErrPartialResult, len(report.SkippedResources), len(report.Written), report.OutDir)
}

func coalesceDuplicateArtifacts(artifacts []Artifact) ([]Artifact, error) {
	seen := make(map[string]int, len(artifacts))
	out := make([]Artifact, 0, len(artifacts))
	for _, a := range artifacts {
		if idx, ok := seen[a.Path]; ok {
			if !sameArtifact(out[idx], a) {
				return nil, fmt.Errorf("duplicate artifact path with different contents: %s", a.Path)
			}
			continue
		}
		seen[a.Path] = len(out)
		out = append(out, a)
	}
	return out, nil
}

func sameArtifact(a, b Artifact) bool {
	aPolicy, aErr := a.Policy()
	bPolicy, bErr := b.Policy()
	if aErr != nil || bErr != nil {
		return false
	}
	return a.Path == b.Path &&
		bytes.Equal(a.Bytes, b.Bytes) &&
		a.Mode == b.Mode &&
		a.Fragment == b.Fragment &&
		a.FragmentKey == b.FragmentKey &&
		a.FragmentOrderKey == b.FragmentOrderKey &&
		aPolicy == bPolicy &&
		a.DedupeFragmentEntries == b.DedupeFragmentEntries
}

func prepareMergeArtifacts(artifacts []Artifact, outDir string, report *WriteReport) ([]Artifact, []Artifact, error) {
	var regular []Artifact
	var merged []Artifact
	for _, a := range artifacts {
		policy, err := a.Policy()
		if err != nil {
			return nil, nil, fmt.Errorf("harnesscompile.Materialize: artifact %q: invalid reconciliation policy: %w", a.Path, err)
		}
		if policy.Mode != artifact.ReconciliationMarker {
			regular = append(regular, a)
			continue
		}
		destPath := filepath.Join(outDir, filepath.FromSlash(a.Path))
		existing, err := os.ReadFile(destPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("harnesscompile.Materialize: read %s: %w", a.Path, err)
		}
		result, mergeReport, err := artifact.MergeWithExistingJSON(existing, a.Bytes, policy.MarkerField, policy.MarkerValue)
		if err != nil {
			// The merge layer only sees bytes, so the destination path is added
			// here — both to the error and to the log. The duplicated value is
			// never carried by the coded error and must never be logged: these
			// files hold MCP env values, headers, and OAuth secrets.
			if coded, ok := diag.AsCodedError(err); ok {
				slog.Error("materialize merge refused an existing aggregate",
					"path", a.Path, "code", coded.Code, "line", coded.Source.Line)
			}
			return nil, nil, fmt.Errorf("harnesscompile.Materialize: merge existing %s: %w", a.Path, err)
		}
		a.Bytes = result
		report.MergedFiles = append(report.MergedFiles, MergeFileReport{
			Path:      a.Path,
			Added:     mergeReport.Added,
			Removed:   mergeReport.Removed,
			Unchanged: mergeReport.Unchanged,
		})
		merged = append(merged, a)
	}
	return regular, merged, nil
}

// rejectSymlinkedPathComponents rejects an output root that is itself a
// symlink. System-level ancestors are intentionally not inspected: temporary
// directories on macOS commonly live below a symlink such as /var.
func rejectSymlinkedPathComponents(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	info, err := os.Lstat(filepath.Clean(absolute))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect output root %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path component %q is a symlink", absolute)
	}
	return nil
}

// rejectSymlinkedParentComponents checks every component leading from root to
// path but intentionally permits the final artifact/stale-file entry itself.
// Renaming over a symlink leaf is safe; following a symlinked parent is not.
func rejectSymlinkedParentComponents(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve output root %q: %w", root, err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	rootAbs = filepath.Clean(rootAbs)
	pathAbs = filepath.Clean(pathAbs)
	if rootAbs != pathAbs && !strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator)) {
		return fmt.Errorf("path %q is outside output root %q", path, root)
	}
	if err := rejectSymlinkedPathComponents(rootAbs); err != nil {
		return err
	}

	rel, err := filepath.Rel(rootAbs, filepath.Dir(pathAbs))
	if err != nil {
		return fmt.Errorf("resolve parent of %q: %w", path, err)
	}
	current := rootAbs
	if rel == "." {
		return nil
	}
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("inspect path component %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symlink", current)
		}
	}
	return nil
}

// validateArtifactPath rejects paths that try to escape outDir.
func validateArtifactPath(p, outDir string) error {
	if p == "" {
		return fmt.Errorf("artifact path must not be empty")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("artifact path must be relative, got absolute: %s", p)
	}
	// Check for ".." components.
	for _, component := range strings.Split(p, "/") {
		if component == ".." {
			return fmt.Errorf("artifact path must not contain '..': %s", p)
		}
	}
	// Double-check: the resolved path must stay within outDir.
	joined := filepath.Join(outDir, p)
	if !strings.HasPrefix(filepath.Clean(joined), filepath.Clean(outDir)+string(filepath.Separator)) &&
		filepath.Clean(joined) != filepath.Clean(outDir) {
		return fmt.Errorf("artifact path escapes output directory: %s", p)
	}
	return nil
}

// fileMatchesBytes reports whether path exists and has identical contents to
// expected.
func fileMatchesBytes(path string, expected []byte, observed *fsboundary.Identity) (matches, exists bool, err error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false, false, err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(absolute))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return false, false, nil
		}
		return false, false, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := root.NativeRoot()
	if err != nil {
		return false, false, err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := native.Lstat(filepath.Base(absolute))
	if errors.Is(err, fs.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	original, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return false, false, err
	}
	data, identity, err := root.ReadFile(filepath.Base(absolute), info.Size())
	if err != nil {
		return false, false, err
	}
	if !identity.SameState(original) {
		return false, false, fsboundary.ErrGenerationConflict
	}
	*observed = identity
	return bytes.Equal(data, expected), true, nil
}

type materializeRootSnapshot struct {
	exists bool
	info   fs.FileInfo
}

func snapshotMaterializeRoot(path string) (materializeRootSnapshot, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return materializeRootSnapshot{}, nil
	}
	if err != nil {
		return materializeRootSnapshot{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return materializeRootSnapshot{}, fmt.Errorf("output root is not a real directory")
	}
	return materializeRootSnapshot{exists: true, info: info}, nil
}

func revalidateMaterializeRoot(path string, snapshot materializeRootSnapshot) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if !snapshot.exists {
			return nil
		}
		return errors.New("output root disappeared")
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("output root is no longer a real directory")
	}
	if !snapshot.exists || !os.SameFile(snapshot.info, info) {
		return errors.New("output root identity changed")
	}
	return nil
}

func isMaterializeTransactionDir(name string) bool {
	for _, prefix := range []string{".tmp-", ".agent-marketplace-convert-", ".agent-marketplace-recovery-", ".agent-marketplace-stale-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isMaterializeTransactionFile(name string) bool {
	return isMaterializeTransactionDir(name) || strings.HasPrefix(name, ".agent-marketplace-stale-")
}

// findStaleFiles walks outDir and returns no-follow identity snapshots for
// paths not in the artifactPaths set.
func findStaleFiles(outDir string, artifactPaths map[string]struct{}) ([]StaleFileCandidate, error) {
	var stale []StaleFileCandidate
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != outDir && isMaterializeTransactionDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		if isMaterializeTransactionFile(filepath.Base(rel)) {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if _, ok := artifactPaths[relSlash]; !ok {
			candidate, snapshotErr := snapshotStaleFileCandidate(path, relSlash)
			if snapshotErr != nil {
				if os.IsNotExist(snapshotErr) {
					return nil
				}
				return snapshotErr
			}
			stale = append(stale, candidate)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].Path < stale[j].Path })
	return stale, nil
}

// cleanEmptyDirs removes empty parent directories of relPath from outDir
// upward, but only while each directory still has the inode captured during
// stale discovery. This prevents a root or parent replacement from turning
// post-prune cleanup into a deletion of a user's replacement tree.
func cleanEmptyDirs(outDir, relPath string, parents []staleParentSnapshot) {
	parts := strings.Split(relPath, "/")
	for i := len(parts); i >= 1; i-- {
		dirPath := filepath.Join(outDir, filepath.FromSlash(strings.Join(parts[:i], "/")))
		if dirPath == outDir {
			break
		}
		current, err := os.Lstat(dirPath)
		if err != nil {
			break
		}
		var expected fs.FileInfo
		for _, parent := range parents {
			if parent.path == dirPath {
				expected = parent.info
				break
			}
		}
		if expected == nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, current) {
			break
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(dirPath); err != nil {
			break
		}
	}
}

// sliceContains reports whether ss contains s.
func sliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// The per-resource loaders (LoadSkill, LoadCommand, LoadSubagent, LoadHooks,
// LoadMcpServers) live in load.go.

// Result holds the artifacts and diagnostics produced by compilation.
type Result struct {
	Artifacts   []Artifact
	Diagnostics []Diagnostic

	// Skipped lists the bundle resources whose artifacts were suppressed
	// because they produced error diagnostics. It is attribution for the
	// per-resource error contract (see compileBundleFromIR) and is empty for a
	// clean bundle and for single-resource Compile results.
	Skipped []SkippedResource
}

// HasErrors reports whether the result carries at least one error diagnostic.
//
// It is the single "was this run clean?" question. A Result with errors is by
// construction PARTIAL: the resources named in Skipped contributed nothing, so
// the caller must treat the run as failed (non-zero exit) even though
// Artifacts holds real, complete output for every resource that compiled.
func (r *Result) HasErrors() bool {
	if r == nil {
		return false
	}
	return diag.HasErrors(r.Diagnostics)
}

// WriteMode controls how Materialize handles existing files.
type WriteMode int

const (
	WriteModeFailIfExists WriteMode = iota
	WriteModeOverwrite
	WriteModeSync
)

// WriteOption configures Materialize behavior.
type WriteOption func(*writeConfig)

type writeConfig struct {
	mode       WriteMode
	pruneStale bool
	dryRun     bool
}

// WithWriteMode sets the write mode.
func WithWriteMode(m WriteMode) WriteOption {
	return func(c *writeConfig) { c.mode = m }
}

// WithPruneStale enables pruning of files not present in the result.
func WithPruneStale(enabled bool) WriteOption {
	return func(c *writeConfig) { c.pruneStale = enabled }
}

// WithDryRun causes Materialize to report what it would do without writing.
func WithDryRun() WriteOption {
	return func(c *writeConfig) { c.dryRun = true }
}

// WriteReport describes what Materialize did.
//
// Written, Skipped, and Removed describe FILES: Skipped means "already
// byte-identical on disk, so not rewritten". Partial and SkippedResources
// describe RESOURCES that never produced output at all.
type WriteReport struct {
	Written     []string
	Skipped     []string
	Removed     []string
	MergedFiles []MergeFileReport
	OutDir      string

	// Partial is true when the materialized Result carried error diagnostics,
	// so the write covers only the resources that compiled cleanly. Materialize
	// also returns an error wrapping ErrPartialResult in that case, so a caller
	// cannot mistake a partial write for a clean run by ignoring this field.
	Partial bool

	// SkippedResources lists the resources that contributed no artifacts. It
	// carries identity and diagnostic codes only, never resource content.
	SkippedResources []SkippedResource

	// PruneSuppressed is true when pruning was requested but not performed
	// because the result was partial. Removed is empty in that case.
	PruneSuppressed bool

	// StaleCandidates carries the no-follow identity snapshots captured during
	// stale discovery. A delayed publisher must revalidate these candidates
	// before removing them.
	StaleCandidates []StaleFileCandidate

	// PruneConflicts lists stale paths preserved because their identity or bytes
	// changed after discovery.
	PruneConflicts []string
}

type MergeFileReport struct {
	Path      string
	Added     []string
	Removed   []string
	Unchanged []string
}

// compileOptions translates the public option set into compile-package
// options. Compile and compileBundleFromIR share it so a newly added option can
// never be wired into one entry point and forgotten in the other.
func compileOptions(cfg *config) []compile.Option {
	copts := make([]compile.Option, 0, 5)
	if cfg.modelOverride != nil {
		copts = append(copts, compile.WithModelOverride(cfg.modelOverride))
	}
	if cfg.toolOverride != nil {
		copts = append(copts, compile.WithToolOverride(cfg.toolOverride))
	}
	if cfg.pluginRoot != "" {
		copts = append(copts, compile.WithPluginRoot(cfg.pluginRoot))
	}
	if cfg.managedByField != "" {
		copts = append(copts, compile.WithManagedBy(cfg.managedByField, cfg.managedByValue))
	}
	if cfg.scope != "" {
		copts = append(copts, compile.WithScope(cfg.scope))
	}
	return copts
}

// Option configures compilation behavior.
type Option func(*config)

type config struct {
	modelOverride  map[string]string
	toolOverride   map[string]string
	pluginRoot     string
	managedByField string
	managedByValue string
	scope          Scope
}

// Scope is the installation scope a caller compiles for. It is re-exported
// from the compile package so a consumer that imports only this package can
// name it.
type Scope = compile.Scope

// Scope constants re-exported from compile.
const (
	ScopeProject = compile.ScopeProject
	ScopeUser    = compile.ScopeUser
)

// WithScope selects the installation scope. It matters for targets whose
// canonical output path depends on the scope: Devin hooks compile to
// `hooks.v1.json` for a project root and to the `hooks` member of
// `config.json` for the user root. The default is ScopeProject.
func WithScope(scope Scope) Option {
	return func(c *config) { c.scope = scope }
}

// WithModelOverride sets a source-to-target mapping override for model IDs.
func WithModelOverride(m map[string]string) Option {
	return func(c *config) { c.modelOverride = m }
}

// WithToolOverride sets a source-to-target mapping override for tool IDs.
func WithToolOverride(m map[string]string) Option {
	return func(c *config) { c.toolOverride = m }
}

// WithPluginRoot sets the literal plugin root used by targets that need
// compile-time expansion of ${CLAUDE_PLUGIN_ROOT}.
func WithPluginRoot(path string) Option {
	return func(c *config) { c.pluginRoot = path }
}

func WithManagedBy(field, value string) Option {
	return func(c *config) {
		c.managedByField = field
		c.managedByValue = value
	}
}
