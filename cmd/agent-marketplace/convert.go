package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/convertsync"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var (
	conversionHarnesses = targetmeta.ConversionNames()
)

const (
	// convertSyncCollisionCode is reported when sync would replace an output file
	// that no ownership record covers.
	convertSyncCollisionCode = "convert.sync.unmanaged-collision"
	// convertSyncPreservedCode is reported for each output file sync left alone
	// because it cannot prove it wrote it.
	convertSyncPreservedCode = "convert.sync.preserved-unmanaged"
	// convertSyncUnrecordedCode is reported when a conversion landed but its
	// ownership record could not be written, so the next sync will own nothing.
	convertSyncUnrecordedCode = "convert.sync.state-not-recorded"
	// convertSyncAuthorizedCode is reported when --replace-unmanaged is about to
	// destroy output ownership cannot account for.
	convertSyncAuthorizedCode = "convert.sync.replace-unmanaged-authorized"
	// convertSyncUnreplaceableCode is reported when an authorized replacement
	// cannot clear the destination it was authorized to replace.
	convertSyncUnreplaceableCode = "convert.sync.destination-unreplaceable"
	// convertSyncPathListLimit caps how many paths a single refusal message
	// names before summarizing the rest.
	convertSyncPathListLimit = 10
)

func newConvertCmd() *cobra.Command {
	var from string
	var to string
	var outDir string
	var autodetect bool
	var syncMode bool
	var dryRun bool
	var replaceUnmanaged bool

	cmd := &cobra.Command{
		Use:   "convert --from <harness> --to <harness> <path>",
		Short: "Convert a harness-specific plugin directory to another harness layout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConvert(cmd.OutOrStdout(), args[0], from, to, outDir, autodetect, syncMode, dryRun, replaceUnmanaged)
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "Source harness: "+strings.Join(conversionHarnesses, ", "))
	cmd.Flags().StringVar(&to, "to", "", "Target harness: "+strings.Join(conversionHarnesses, ", "))
	cmd.Flags().StringVar(&outDir, "out", "", "Output directory (default: <path>-<to>)")
	cmd.Flags().BoolVar(&autodetect, "autodetect", false, "Autodetect source harness instead of using --from")
	cmd.Flags().BoolVar(&syncMode, "sync", false, "Use sync mode and prune stale output files this command previously wrote")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report planned writes without changing files")
	cmd.Flags().BoolVar(&replaceUnmanaged, "replace-unmanaged", false, "With --sync, authorize destructive replacement and pruning of output files agent-marketplace cannot prove it wrote")
	return cmd
}

func runConvert(out io.Writer, srcPath, from, to, outDir string, autodetect, syncMode, dryRun, replaceUnmanaged bool) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("--to is required")
	}
	if !targetmeta.IsConversionTarget(to) {
		return fmt.Errorf("unknown or unsupported target harness %q; must be one of: %v", to, conversionHarnesses)
	}
	if replaceUnmanaged && !syncMode {
		return fmt.Errorf("--replace-unmanaged requires --sync; without --sync nothing is pruned")
	}

	srcHarness := hcc.Harness(from)
	srcFS := os.DirFS(srcPath)
	if autodetect {
		detected, ok := hcc.Autodetect(srcFS)
		if !ok {
			return fmt.Errorf("autodetect: could not identify source harness")
		}
		srcHarness = detected
	} else if from == "" {
		return fmt.Errorf("--from is required")
	} else if !targetmeta.IsConversionTarget(from) {
		return fmt.Errorf("unknown or unsupported source harness %q; must be one of: %v", from, conversionHarnesses)
	}
	if !targetmeta.IsConversionTarget(string(srcHarness)) {
		return fmt.Errorf("unknown or unsupported source harness %q; must be one of: %v", srcHarness, conversionHarnesses)
	}

	if outDir == "" {
		outDir = srcPath + "-" + to
	}

	result, err := hcc.Convert(srcFS, srcHarness, hcc.Harness(to), hcc.WithPluginRoot(srcPath))
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}

	if syncMode {
		return runConvertSync(out, result, srcHarness, to, outDir, dryRun, replaceUnmanaged)
	}
	return runConvertOverwrite(out, result, srcHarness, to, outDir, dryRun)
}

// runConvertOverwrite is the default one-shot conversion: it writes what the
// conversion produced and removes nothing, so an output directory can only gain
// files or have its own artifact paths rewritten.
func runConvertOverwrite(out io.Writer, result *hcc.Result, srcHarness hcc.Harness, to, outDir string, dryRun bool) error {
	matOpts := []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeOverwrite)}
	if dryRun {
		matOpts = append(matOpts, hcc.WithDryRun())
	}

	// A Result carrying error diagnostics is materialized, not refused: the
	// resources that compiled cleanly are complete and reach disk, while the
	// resources that failed contributed nothing. Report exactly what landed and
	// then fail, so a partial conversion can never read as a clean one.
	report, err := hcc.Materialize(result, outDir, matOpts...)
	if err != nil && !errors.Is(err, hcc.ErrPartialResult) {
		return fmt.Errorf("materialize: %w", err)
	}
	return reportConvert(out, convertReport{
		result:     result,
		report:     report,
		srcHarness: srcHarness,
		target:     to,
		outDir:     outDir,
		removed:    len(report.Removed),
		failure:    err,
	})
}

// runConvertSync reconciles an output directory instead of only writing into it.
//
// Reconciliation is scoped by ownership, never by directory scan. The stale-file
// scan cannot distinguish output of a resource that has since been deleted from
// a file the user wrote by hand, so a scan-driven prune deletes unrelated work —
// including work nested inside a directory this conversion also writes into.
// Two rules follow, and both fail closed:
//
//   - A prune candidate is removed only when the durable record proves this
//     command wrote it AND its bytes are still the bytes that were written.
//     Everything else is preserved and reported.
//   - An artifact whose destination already holds different, unowned content is
//     a collision: the run is refused before anything is written, because
//     replacing it would destroy content this command did not produce. A
//     destination whose content cannot be read at all is such a collision too,
//     and is settled before planning, since it would otherwise fail the
//     materialization from inside the compilation library.
//
// --replace-unmanaged is the explicit authorization that lifts those rules, and
// the scope of what it destroys is stated in full before any of it happens —
// both in the refusal that suggests the flag and in the run that uses it.
func runConvertSync(out io.Writer, result *hcc.Result, srcHarness hcc.Harness, to, outDir string, dryRun, replaceUnmanaged bool) error {
	record := loadConvertRecord(outDir, to)

	// Destinations whose current content cannot be read are settled before
	// planning. The compilation library decides what to write by comparing a
	// destination's bytes with the artifact's, so such a file fails the whole
	// materialization — planning included — and no later step gets a say.
	opaque, err := opaqueConvertDestinations(result, outDir)
	if err != nil {
		return err
	}
	if len(opaque) > 0 {
		cleared, err := settleOpaqueDestinations(out, outDir, opaque, dryRun, replaceUnmanaged)
		if err != nil {
			return err
		}
		opaque = cleared
	}

	decision, err := planConvertSync(result, outDir, record)
	if err != nil {
		return err
	}
	decision.cleared = opaque
	if !replaceUnmanaged && len(decision.conflicts) > 0 {
		// The refusal is what a user reads before deciding to authorize the
		// override, so it describes the whole scope of that override — every
		// destination that would be replaced AND every unowned file that would
		// additionally be deleted — rather than only the collisions that
		// triggered it. Naming half the damage would make the flag look narrower
		// than it is.
		if err := describeDestructiveScope(out, outDir, decision, fmt.Sprintf(
			"%s: refusing to reconcile %s: --replace-unmanaged would destroy %d output file(s) agent-marketplace cannot prove it wrote",
			convertSyncCollisionCode, filepath.Clean(outDir), decision.destructiveCount())); err != nil {
			return err
		}
		return fmt.Errorf("convert: %s: sync would replace %d output file(s) in %s that agent-marketplace cannot prove it wrote: %s; re-run with --replace-unmanaged to authorize destroying all %d unowned file(s) listed above",
			convertSyncCollisionCode, len(decision.conflicts), filepath.Clean(outDir),
			summarizeConvertPaths(decision.conflicts), decision.destructiveCount())
	}
	if replaceUnmanaged {
		decision.authorized = true
		if decision.destructiveCount() > 0 {
			// The same scope is stated before the destruction happens, so an
			// authorized run is auditable from its own output and a --dry-run
			// shows exactly what dropping --dry-run would destroy.
			if err := describeDestructiveScope(out, outDir, decision, fmt.Sprintf(
				"%s: destroying %d output file(s) in %s that agent-marketplace cannot prove it wrote",
				convertSyncAuthorizedCode, decision.destructiveCount(), filepath.Clean(outDir))); err != nil {
				return err
			}
		}
	}

	// Pruning is performed here rather than by the compilation library: the
	// library's prune mode removes every file absent from the result, which is
	// exactly the behavior ownership has to narrow.
	matOpts := []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeOverwrite)}
	if dryRun {
		matOpts = append(matOpts, hcc.WithDryRun())
	}
	report, materializeErr := hcc.Materialize(result, outDir, matOpts...)
	if materializeErr != nil && !errors.Is(materializeErr, hcc.ErrPartialResult) {
		return fmt.Errorf("materialize: %w", materializeErr)
	}

	prune := decision.prune()
	removed := len(prune)
	if !dryRun {
		removed, err = pruneOwnedConvertOutput(outDir, prune, record, !replaceUnmanaged)
		if err != nil {
			return err
		}
	}

	warnings := []string(nil)
	if !dryRun {
		if err := recordConvertOutput(outDir, to, report, record); err != nil {
			// The conversion already landed and failing to record it is not
			// destructive: an unrecorded file is unmanaged, so the next sync
			// preserves it. Report it, because pruning silently stops working.
			slog.Error("failed to record convert output ownership", "out_dir", outDir, "err", err)
			warnings = append(warnings, fmt.Sprintf("%s: %v", convertSyncUnrecordedCode, err))
		}
	}

	return reportConvert(out, convertReport{
		result:          result,
		report:          report,
		srcHarness:      srcHarness,
		target:          to,
		outDir:          outDir,
		removed:         removed,
		preserved:       decision.preserved(),
		pruneSuppressed: decision.pruneSuppressed,
		warnings:        warnings,
		failure:         materializeErr,
	})
}

// convertSyncDecision is the ownership-scoped reconciliation plan for one sync.
//
// It is computed once, independently of authorization, so the refusal a default
// run prints and the destruction an authorized run performs are derived from the
// same classification instead of two divergent passes.
type convertSyncDecision struct {
	// ownedPrune lists stale output paths the record proves this command wrote
	// and whose bytes are unchanged, so they can be removed unconditionally.
	ownedPrune []string
	// unownedPrune lists stale output paths without ownership proof. They are
	// preserved by default and destroyed only under --replace-unmanaged.
	unownedPrune []string
	// conflicts lists artifact destinations holding different, unowned content.
	conflicts []string
	// cleared lists unowned destinations an authorized run already removed
	// because their content could not be read (see settleOpaqueDestinations).
	// They are gone by the time the plan is computed, so they are carried here to
	// stay part of what this run reports having destroyed.
	cleared []string
	// pruneSuppressed mirrors the library's bundle-wide suppression for a
	// partial result, which yields no prune candidates at all.
	pruneSuppressed bool
	// authorized records that --replace-unmanaged widened the plan.
	authorized bool
}

// prune is the set of paths this run removes, which is only the proven-owned
// subset unless --replace-unmanaged authorized the rest.
func (d convertSyncDecision) prune() []string {
	if !d.authorized {
		return d.ownedPrune
	}
	all := append(append([]string(nil), d.ownedPrune...), d.unownedPrune...)
	sort.Strings(all)
	return all
}

// preserved is the set of paths this run leaves alone for lack of ownership
// proof. An authorized run preserves nothing on those grounds.
func (d convertSyncDecision) preserved() []string {
	if d.authorized {
		return nil
	}
	return d.unownedPrune
}

// destructiveCount is how many unowned files --replace-unmanaged destroys: every
// unowned prune candidate plus every unowned destination it overwrites. It does
// not depend on authorization, so the refusal quotes the same number the
// authorized run then acts on.
func (d convertSyncDecision) destructiveCount() int {
	return len(d.conflicts) + len(d.cleared) + len(d.unownedPrune)
}

// replaced is every unowned destination whose content the conversion overwrites.
func (d convertSyncDecision) replaced() []string {
	all := append(append([]string(nil), d.conflicts...), d.cleared...)
	sort.Strings(all)
	return all
}

// loadConvertRecord reads the durable ownership record for this output root and
// target, degrading to a record that owns nothing. Absent state is the normal
// first run and is only worth a debug line; state that existed and cannot be
// used is warned about.
func loadConvertRecord(outDir, target string) convertsync.Record {
	record, err := convertsync.Load(outDir, target)
	switch {
	case err == nil:
	case errors.Is(err, convertsync.ErrRecordMissing):
		slog.Debug("no convert ownership record for output root; existing output is unmanaged", "out_dir", outDir)
	case errors.Is(err, convertsync.ErrRecordOtherTarget):
		slog.Warn("convert ownership record was written for another target harness; existing output is unmanaged",
			"out_dir", outDir, "target", target, "err", err)
	default:
		slog.Warn("convert ownership record unusable; existing output is unmanaged", "out_dir", outDir, "err", err)
	}
	return record
}

// planConvertSync classifies what a sync would remove and what it would replace.
// The candidate sets come from a dry-run materialization, so the plan is derived
// from the same code that performs the write rather than from a second, possibly
// divergent view of the output layout.
func planConvertSync(result *hcc.Result, outDir string, record convertsync.Record) (convertSyncDecision, error) {
	plan, err := hcc.Materialize(result, outDir,
		hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true), hcc.WithDryRun())
	if err != nil && !errors.Is(err, hcc.ErrPartialResult) {
		return convertSyncDecision{}, fmt.Errorf("plan sync: %w", err)
	}

	decision := convertSyncDecision{pruneSuppressed: plan.PruneSuppressed}
	for _, rel := range plan.Removed {
		state, err := inspectConvertOutput(outDir, rel, record)
		if err != nil {
			return convertSyncDecision{}, err
		}
		if state.status == convertsync.StatusOwned {
			decision.ownedPrune = append(decision.ownedPrune, rel)
			continue
		}
		decision.unownedPrune = append(decision.unownedPrune, rel)
	}
	for _, rel := range plan.Written {
		state, err := inspectConvertOutput(outDir, rel, record)
		if err != nil {
			return convertSyncDecision{}, err
		}
		// An identical destination is reported as skipped rather than written, so
		// anything here differs from what would be written. Conversion compiles
		// without a managed-by field, so no artifact merges with existing
		// content: every write is a whole-file replacement and an existing
		// differing file really does lose its content. A future conversion that
		// opts into managed-by merging would have to exempt plan.MergedFiles from
		// this check.
		if !state.exists || state.status == convertsync.StatusOwned {
			continue
		}
		decision.conflicts = append(decision.conflicts, rel)
	}
	sort.Strings(decision.ownedPrune)
	sort.Strings(decision.unownedPrune)
	sort.Strings(decision.conflicts)
	if len(decision.unownedPrune) > 0 || len(decision.conflicts) > 0 {
		slog.Info("convert sync scoped reconciliation by ownership", "out_dir", outDir,
			"owned_prune", len(decision.ownedPrune), "unowned_prune", len(decision.unownedPrune),
			"conflicts", len(decision.conflicts))
	}
	return decision, nil
}

// convertOutputState is what one output path currently holds.
type convertOutputState struct {
	// exists reports that something — of any kind — occupies the path.
	exists bool
	// status is the ownership verdict. It is StatusUnmanaged whenever the current
	// bytes could not be obtained, since ownership is proved by comparing them.
	status convertsync.Status
}

// inspectConvertOutput reports what occupies one output path and whether the
// record proves this command wrote it.
func inspectConvertOutput(outDir, rel string, record convertsync.Record) (convertOutputState, error) {
	abs, err := resolveConvertOutputPath(outDir, rel)
	if err != nil {
		return convertOutputState{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return convertOutputState{}, nil
		}
		return convertOutputState{}, fmt.Errorf("convert: inspect output file %q: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		// A symlink, directory or special file is never something a conversion
		// wrote, and reading it would either follow it outside the output tree or
		// block on it, so its bytes are deliberately never obtained.
		return convertOutputState{exists: true}, nil
	}
	current, err := os.ReadFile(abs)
	if err != nil {
		// Ownership is proved by comparing bytes, so bytes that cannot be read
		// prove nothing: the file is unmanaged and therefore preserved. Failing
		// the run instead would let one unreadable file block every sync into
		// the directory, including the --replace-unmanaged run documented as the
		// way to take such a file over.
		slog.Warn("could not read an output file to check its ownership; treating it as unmanaged",
			"out_dir", outDir, "path", rel, "err", err)
		return convertOutputState{exists: true}, nil
	}
	return convertOutputState{exists: true, status: record.Classify(rel, current)}, nil
}

// opaqueConvertDestinations lists the artifact destinations that already hold
// content this command cannot compare against what it would write: an
// unreadable regular file, or anything that is not a regular file at all.
//
// The destinations come from the result rather than from a materialization plan
// because producing that plan is exactly what such a file prevents.
func opaqueConvertDestinations(result *hcc.Result, outDir string) ([]string, error) {
	opaque := make([]string, 0)
	seen := make(map[string]struct{}, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		rel := filepath.ToSlash(artifact.Path)
		if _, duplicate := seen[rel]; duplicate {
			continue
		}
		seen[rel] = struct{}{}
		unreadable, err := convertDestinationUnreadable(outDir, rel)
		if err != nil {
			return nil, err
		}
		if unreadable {
			opaque = append(opaque, rel)
		}
	}
	sort.Strings(opaque)
	return opaque, nil
}

// convertDestinationUnreadable reports whether something occupies rel whose
// current content cannot be read.
//
// It probes with an open instead of reading, because only readability is in
// question here: the content itself is compared later, and only for the
// destinations that survive this check, so reading every artifact destination
// twice on every sync would buy nothing.
func convertDestinationUnreadable(outDir, rel string) (bool, error) {
	abs, err := resolveConvertOutputPath(outDir, rel)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("convert: inspect output file %q: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		// A symlink, directory or special file holds no bytes this conversion
		// could have written, and opening it would either follow it out of the
		// output tree or block on it.
		return true, nil
	}
	file, err := os.Open(abs)
	if err != nil {
		slog.Warn("an output destination cannot be read to compare it with the conversion's output",
			"out_dir", outDir, "path", rel, "err", err)
		return true, nil
	}
	_ = file.Close()
	return false, nil
}

// settleOpaqueDestinations decides what to do about destinations whose content
// cannot be compared, and returns the ones it removed.
//
// Unlinking such a destination is the only way the conversion can write its own
// file there, and it destroys unowned content, so it happens only under
// --replace-unmanaged and only after the scope is stated. Without that
// authorization the run is refused with the same collision code as any other
// unowned destination, rather than failing deep inside the write with a bare
// permission error.
//
// A dry run cannot remove anything, and while these files exist no plan can be
// produced, so it reports them and stops instead of previewing a reconciliation
// it cannot compute.
func settleOpaqueDestinations(out io.Writer, outDir string, opaque []string, dryRun, replaceUnmanaged bool) ([]string, error) {
	scope := convertSyncDecision{conflicts: opaque}
	if !replaceUnmanaged {
		if err := describeDestructiveScope(out, outDir, scope, fmt.Sprintf(
			"%s: refusing to reconcile %s: %d output destination(s) hold content that cannot be read, so ownership of them cannot be proved",
			convertSyncCollisionCode, filepath.Clean(outDir), len(opaque))); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("convert: %s: sync would replace %d unreadable output file(s) in %s: %s; make them readable, remove them, or re-run with --replace-unmanaged to authorize replacing them",
			convertSyncCollisionCode, len(opaque), filepath.Clean(outDir), summarizeConvertPaths(opaque))
	}
	if err := describeDestructiveScope(out, outDir, scope, fmt.Sprintf(
		"%s: replacing %d output destination(s) in %s whose content cannot be read",
		convertSyncAuthorizedCode, len(opaque), filepath.Clean(outDir))); err != nil {
		return nil, err
	}
	if dryRun {
		return nil, fmt.Errorf("convert: %s: %d output destination(s) in %s cannot be read, so --dry-run cannot show what the conversion would write; re-run without --dry-run to let --replace-unmanaged replace them: %s",
			convertSyncUnreplaceableCode, len(opaque), filepath.Clean(outDir), summarizeConvertPaths(opaque))
	}
	cleared := make([]string, 0, len(opaque))
	for _, rel := range opaque {
		abs, err := resolveConvertOutputPath(outDir, rel)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(abs); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("convert: %s: cannot clear %q to replace it: %w", convertSyncUnreplaceableCode, rel, err)
		}
		slog.Warn("removed an output destination whose content could not be read, as authorized by --replace-unmanaged",
			"out_dir", outDir, "path", rel)
		cleared = append(cleared, rel)
	}
	return cleared, nil
}

// describeDestructiveScope states, before anything is destroyed, the complete
// bounded set of unowned output files --replace-unmanaged affects: which are
// replaced by converted content and which are deleted outright. Long lists are
// truncated with a remainder count, so the scope stays readable while the totals
// stay exact.
func describeDestructiveScope(out io.Writer, outDir string, decision convertSyncDecision, header string) error {
	if _, err := fmt.Fprintln(out, header); err != nil {
		return err
	}
	sections := []struct {
		verb  string
		what  string
		paths []string
	}{
		{verb: "replace", what: "unowned file(s) a converted resource would be written over", paths: decision.replaced()},
		{verb: "delete", what: "unowned file(s) no longer produced by the conversion", paths: decision.unownedPrune},
	}
	for _, section := range sections {
		if len(section.paths) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "  %s %d %s:\n", section.verb, len(section.paths), section.what); err != nil {
			return err
		}
		named := section.paths
		if len(named) > convertSyncPathListLimit {
			named = named[:convertSyncPathListLimit]
		}
		for _, rel := range named {
			if _, err := fmt.Fprintf(out, "    %s %s\n", section.verb, rel); err != nil {
				return err
			}
		}
		if overflow := len(section.paths) - len(named); overflow > 0 {
			if _, err := fmt.Fprintf(out, "    %s %d more file(s)\n", section.verb, overflow); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveConvertOutputPath maps an output-relative path to an absolute one,
// refusing anything that leaves the output root or reaches it through a
// symlinked parent. It guards deletion, so containment is checked here rather
// than trusted from the plan or the record.
func resolveConvertOutputPath(outDir, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("convert: empty output path")
	}
	abs := filepath.Join(outDir, filepath.FromSlash(rel))
	inside, err := filepath.Rel(outDir, abs)
	if err != nil {
		return "", fmt.Errorf("convert: resolve output path %q: %w", rel, err)
	}
	if inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) || filepath.IsAbs(inside) {
		return "", fmt.Errorf("convert: output path %q escapes %s", rel, filepath.Clean(outDir))
	}
	current := filepath.Clean(outDir)
	for _, segment := range strings.Split(filepath.ToSlash(inside), "/") {
		current = filepath.Join(current, segment)
		if current == abs {
			break
		}
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				break
			}
			return "", fmt.Errorf("convert: inspect %q: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("convert: output path %q traverses symlink %s", rel, current)
		}
	}
	return abs, nil
}

// pruneOwnedConvertOutput removes the approved paths and returns how many were
// actually removed. When verify is set each file is re-checked against the
// record immediately before deletion, so a file that changed between planning
// and writing is preserved rather than deleted on stale evidence.
func pruneOwnedConvertOutput(outDir string, rels []string, record convertsync.Record, verify bool) (int, error) {
	removed := 0
	for _, rel := range rels {
		abs, err := resolveConvertOutputPath(outDir, rel)
		if err != nil {
			return removed, err
		}
		if verify {
			state, statusErr := inspectConvertOutput(outDir, rel, record)
			if statusErr != nil {
				return removed, statusErr
			}
			if !state.exists {
				continue
			}
			if state.status != convertsync.StatusOwned {
				slog.Warn("preserved an output file that stopped matching its ownership record during sync",
					"out_dir", outDir, "path", rel, "status", state.status.String())
				continue
			}
		}
		if err := os.Remove(abs); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("convert: remove stale output %q: %w", rel, err)
		}
		removed++
		removeEmptyConvertDirs(outDir, filepath.Dir(filepath.FromSlash(rel)))
	}
	return removed, nil
}

// removeEmptyConvertDirs removes directories left empty by pruning, from relDir
// upward, and never touches the output root itself.
func removeEmptyConvertDirs(outDir, relDir string) {
	current := filepath.Join(outDir, relDir)
	root := filepath.Clean(outDir)
	for current != root {
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

// recordConvertOutput records every file this run materialized, digesting the
// bytes now on disk rather than the artifact bytes: a merged aggregate is
// written from a combination of existing and generated content, and what the
// next run must compare against is what is actually there.
//
// prior is the record this run started from. Entries it still holds that this
// run neither rewrote nor removed are carried over, so ownership survives a run
// that could not reconcile them (see carryOverPriorOwnership).
func recordConvertOutput(outDir, target string, report *hcc.WriteReport, prior convertsync.Record) error {
	// report.Skipped holds destinations that already contained exactly the bytes
	// this run would have written. Recording them adopts them, which is
	// byte-neutral: a later prune of such a file removes content identical to
	// what the conversion produces, never content unique to the user.
	paths := make([]string, 0, len(report.Written)+len(report.Skipped))
	paths = append(paths, report.Written...)
	paths = append(paths, report.Skipped...)

	files := make([]convertsync.OwnedFile, 0, len(paths))
	materialized := make(map[string]struct{}, len(paths))
	for _, rel := range paths {
		abs, err := resolveConvertOutputPath(outDir, rel)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		materialized[rel] = struct{}{}
		current, err := os.ReadFile(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// Recording nothing at all would drop ownership of every other file
			// this run wrote, so one unreadable output costs only its own entry:
			// it stays unmanaged, which preserves it and refuses to replace it.
			slog.Warn("could not read a materialized output file to record it; leaving it unmanaged",
				"out_dir", outDir, "path", rel, "err", err)
			continue
		}
		files = append(files, convertsync.OwnedFile{Path: rel, Digest: convertsync.DigestOf(current)})
	}
	files = append(files, carryOverPriorOwnership(outDir, prior, materialized)...)
	return convertsync.Save(outDir, target, files)
}

// carryOverPriorOwnership returns the prior record entries that are still on
// disk with exactly their recorded bytes and that this run did not materialize.
//
// Without this, a run that cannot prune forgets what it owns. A partial result
// suppresses pruning bundle-wide (harnesscompile.WriteReport.PruneSuppressed)
// and contributes no artifacts for the failing resource, so replacing the record
// with only what this run wrote would make the previous run's output unmanaged
// forever: the next clean run could no longer prune it, and would refuse to
// rewrite its own file as an unmanaged collision. Re-verifying the digest keeps
// the carry-over evidence-based — a file that was pruned is gone, and a file the
// user edited no longer matches, so neither is carried.
func carryOverPriorOwnership(outDir string, prior convertsync.Record, materialized map[string]struct{}) []convertsync.OwnedFile {
	carried := make([]convertsync.OwnedFile, 0, len(prior.Files))
	for _, f := range prior.Files {
		if _, rewritten := materialized[f.Path]; rewritten {
			continue
		}
		state, err := inspectConvertOutput(outDir, f.Path, prior)
		if err != nil || !state.exists || state.status != convertsync.StatusOwned {
			continue
		}
		carried = append(carried, f)
	}
	if len(carried) > 0 {
		slog.Debug("carried prior convert output ownership forward", "out_dir", outDir, "files", len(carried))
	}
	return carried
}

// convertReport is everything the command prints about one conversion.
type convertReport struct {
	result          *hcc.Result
	report          *hcc.WriteReport
	srcHarness      hcc.Harness
	target          string
	outDir          string
	removed         int
	preserved       []string
	pruneSuppressed bool
	warnings        []string
	failure         error
}

func reportConvert(out io.Writer, r convertReport) error {
	if _, err := fmt.Fprintf(out, "Converted %s -> %s into %s\n", r.srcHarness, r.target, filepath.Clean(r.outDir)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Written %d, Skipped %d, Removed %d, Diagnostics %d\n",
		len(r.report.Written), len(r.report.Skipped), r.removed, len(r.result.Diagnostics)); err != nil {
		return err
	}
	// A sync into a directory full of unrelated work can preserve thousands of
	// paths; the count is the actionable part, so the listing is bounded exactly
	// like the refusal message.
	named := r.preserved
	if len(named) > convertSyncPathListLimit {
		named = named[:convertSyncPathListLimit]
	}
	for _, rel := range named {
		if _, err := fmt.Fprintf(out, "preserved %s (%s)\n", rel, convertSyncPreservedCode); err != nil {
			return err
		}
	}
	if overflow := len(r.preserved) - len(named); overflow > 0 {
		if _, err := fmt.Fprintf(out, "preserved %d more file(s) (%s)\n", overflow, convertSyncPreservedCode); err != nil {
			return err
		}
	}
	for _, warning := range r.warnings {
		if _, err := fmt.Fprintf(out, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	if r.failure != nil {
		for _, s := range r.report.SkippedResources {
			if _, err := fmt.Fprintf(out, "skipped %s %q (%s)\n", s.Kind, s.Name, strings.Join(s.Codes, ", ")); err != nil {
				return err
			}
		}
		if r.report.PruneSuppressed || r.pruneSuppressed {
			if _, err := fmt.Fprintln(out, "pruning was skipped because the conversion was partial"); err != nil {
				return err
			}
		}
		return fmt.Errorf("materialize: %w", r.failure)
	}
	return nil
}

// summarizeConvertPaths renders a bounded path list for a user-facing message.
func summarizeConvertPaths(paths []string) string {
	if len(paths) <= convertSyncPathListLimit {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(paths[:convertSyncPathListLimit], ", "), len(paths)-convertSyncPathListLimit)
}
