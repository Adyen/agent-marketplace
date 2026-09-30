package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// doctorOptions carries everything doctor needs that a test must be able to
// substitute: the streams, whether stdin is a terminal, and how the Devin CLI
// is probed. Nothing else about doctor is environment-dependent, so a test can
// pin every branch of the repair contract without a PTY and without a Devin
// binary.
type doctorOptions struct {
	out io.Writer
	in  io.Reader
	// isTTY decides the interactive half of the repair contract: "on a TTY it
	// then prompts for each authorized action" versus "off-TTY, `doctor
	// --repair` is preview-only".
	isTTY  bool
	repair bool
	yes    bool
	// ctx carries cancellation and request-scoped values from the command.
	// Tests that do not set it get context.Background().
	ctx context.Context
	// probe detects the installed Devin CLI. It never returns an error by
	// design; see internal/devincli.
	probe func(ctx context.Context) devincli.Probe
	// resolve produces the currently selected resources, which are what
	// "exact current desired matches" is measured against. It is a seam so a
	// test can pin the repair contract against a known selection instead of a
	// cloned registry. Its error reaches the caller, so a Workspace
	// construction failure keeps its wrapped cause instead of degrading to a
	// silently empty selection.
	resolve func(ctx context.Context, repoRoot string) ([]resolver.ResolvedArtifact, error)
}

func newDoctorCmd() *cobra.Command {
	var repair bool
	var assumeYes bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report local harness and install-root diagnostics",
		Long: strings.TrimSpace(`
Report local harness and install-root diagnostics.

doctor also detects a locally installed Devin CLI, reports whether it satisfies
the 3000.3 compatibility floor, and recommends selecting the devin harness when
a Devin CLI is installed but no harness list contains it. Sync
never requires a Devin binary, so nothing doctor reports here blocks it.

--repair is the recovery surface for Devin and Codex shared aggregate files
(hooks.v1.json, config.json#hooks, mcp_config.json, .codex/hooks.json, and
.codex/config.toml). It previews exact adoption
matches, evidence-backed orphan candidates, and existing unmanaged entries whose
key matches a selected resource but whose value differs, and, on a terminal,
prompts for each one. Off a terminal it is preview-only. Every aggregate it
inspected is listed, including the ones with nothing to repair. It never edits
harness lists.
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if assumeYes && !repair {
				return fmt.Errorf("--yes only applies to --repair")
			}
			return runDoctor(doctorOptions{
				ctx:    cmd.Context(),
				out:    cmd.OutOrStdout(),
				in:     cmd.InOrStdin(),
				isTTY:  term.IsTerminal(os.Stdin.Fd()),
				repair: repair,
				yes:    assumeYes,
			})
		},
	}

	cmd.Flags().BoolVar(&repair, "repair", false, "Preview and, on a terminal, confirm recovery of Devin and Codex shared-aggregate ownership state")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "With --repair, adopt exact current desired matches without prompting; removing an orphan or replacing a conflicting entry still requires an interactive per-entry confirmation")
	return cmd
}

func runDoctor(opts doctorOptions) error {
	if opts.ctx == nil {
		opts.ctx = context.Background()
	}
	if opts.out == nil {
		opts.out = os.Stdout
	}
	if opts.probe == nil {
		opts.probe = func(ctx context.Context) devincli.Probe { return devincli.Detect(ctx) }
	}
	if opts.resolve == nil {
		opts.resolve = func(ctx context.Context, repoRoot string) ([]resolver.ResolvedArtifact, error) {
			ws, err := pipeline.NewWorkspace(repoRoot, config.FindLocalConfigDir())
			if err != nil {
				return nil, err
			}
			return pipeline.Resolve(ctx, pipeline.ResolveOptions{Workspace: ws}).Resolved, nil
		}
	}
	out := opts.out

	repoRoot := config.FindRepoRoot()
	detected := "none"
	if h, ok := hcc.Autodetect(os.DirFS(repoRoot)); ok {
		detected = string(h)
	}

	if _, err := fmt.Fprintln(out, "agent-marketplace doctor"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "version: %s\n", version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "harness-compilation: %s\n", hccVersion()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "repo root: %s\n", repoRoot); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "detected harness: %s\n", detected); err != nil {
		return err
	}
	ec := pipeline.LoadEffective(opts.ctx, pipeline.ConfigLoadOnline, config.LoadEffectiveOptions{}, nil)
	config.LogManagedDiagnostics(ec.Diagnostics)

	managed := "false"
	if ec.Managed {
		managed = "true"
	}
	if _, err := fmt.Fprintf(out, "org managed: %s\n", managed); err != nil {
		return err
	}
	if err := writeDoctorManagedSection(out, ec); err != nil {
		return err
	}
	if err := writeDoctorConfigurationErrorsSection(out, ec); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "diagnostic codes: harness-compilation/diag/codes.md"); err != nil {
		return err
	}
	if err := writeDoctorInstallRoots(out, repoRoot); err != nil {
		return err
	}
	if err := writeDoctorDevinSection(out, opts, ec); err != nil {
		return err
	}
	if err := writeDoctorRequirementWarnings(out, opts.ctx, repoRoot); err != nil {
		return err
	}

	if !opts.repair {
		return nil
	}
	return runDoctorRepair(out, opts, repoRoot)
}

// writeDoctorInstallRoots lists every declared target with its capabilities and
// resolved roots. Partitioned layouts retain their stable destination names.
func writeDoctorInstallRoots(out io.Writer, repoRoot string) error {
	if _, err := fmt.Fprintln(out, "install roots:"); err != nil {
		return err
	}

	targets := make([]string, 0, len(targetmeta.Definitions()))
	for _, definition := range targetmeta.Definitions() {
		targets = append(targets, string(definition.Target))
	}
	sort.Strings(targets)
	legacyRoots := harness.MakeInstallRoots()

	for _, target := range targets {
		definition, _ := targetmeta.Lookup(target)
		repoRootSummary, machineRootSummary := "-", "-"
		if definition.PartitionedRoots || len(harness.NativeAggregateInstalls(hcc.Target(target))) > 0 {
			var err error
			repoRootSummary, machineRootSummary, err = harnessRootSummaries(hcc.Target(target), repoRoot)
			if err != nil {
				return err
			}
		} else if root, ok := legacyRoots[hcc.Target(target)]; ok {
			repoRootSummary = root.For(false, repoRoot)
			machineRootSummary = root.For(true, repoRoot)
		}
		if _, err := fmt.Fprintf(out, "  %s repo=%s machine=%s capabilities=%s\n",
			targetDisplayName(target), repoRootSummary, machineRootSummary, targetCapabilities(target)); err != nil {
			return err
		}
	}
	return nil
}

// targetCapabilities renders a target's declared capabilities from the
// authoritative registry, so a compile-only target and a bidirectional one are
// distinguishable in doctor output without a second hand-maintained list.
func targetCapabilities(name string) string {
	definition, ok := targetmeta.Lookup(name)
	if !ok {
		return "unknown"
	}
	var caps []string
	if definition.Compiler {
		caps = append(caps, "compile")
	}
	if definition.Ingester {
		caps = append(caps, "ingest")
	}
	if definition.Conversion {
		caps = append(caps, "convert")
	}
	if definition.UI {
		caps = append(caps, "tui")
	}
	if len(caps) == 0 {
		return "none"
	}
	return strings.Join(caps, ",")
}

// writeDoctorDevinSection implements the plan's detection and recommendation
// rules: "`doctor` detects `devin`, prints the installed version, reports
// compatibility with 3000.3+, and recommends selecting the harness when
// installed but unconfigured."
//
// Every branch here is non-blocking. A missing binary, a probe that fails, a
// probe that hangs, and an unparseable banner all print a line and return nil:
// "Sync does not require a Devin binary."
func writeDoctorDevinSection(out io.Writer, opts doctorOptions, ec config.EffectiveConfig) error {
	probe := opts.probe(opts.ctx)

	switch probe.Status {
	case devincli.StatusNotInstalled:
		if _, err := fmt.Fprintf(out, "devin cli: not installed (sync does not require it)\n"); err != nil {
			return err
		}
	case devincli.StatusSupported:
		if _, err := fmt.Fprintf(out, "devin cli: %s at %s (compatible with %s+)\n",
			probe.Version, probe.Path, devincli.FloorString); err != nil {
			return err
		}
	case devincli.StatusUnsupported:
		slog.Warn("installed devin cli is below the compatibility floor; sync is not blocked",
			"binary_path", probe.Path, "version", probe.Version.String(), "floor", devincli.FloorString,
			"code", diag.CodeDoctorDevinVersionUnsupported)
		if _, err := fmt.Fprintf(out, "devin cli: %s at %s (warning: below the %s compatibility floor; sync is not blocked) [%s]\n",
			probe.Version, probe.Path, devincli.FloorString, diag.CodeDoctorDevinVersionUnsupported); err != nil {
			return err
		}
	default:
		slog.Warn("devin cli version could not be established; sync is not blocked",
			"binary_path", probe.Path, "code", diag.CodeDoctorDevinVersionUnknown)
		if _, err := fmt.Fprintf(out, "devin cli: version unknown at %s (warning: %s compatibility could not be confirmed; sync is not blocked) [%s]\n",
			probe.Path, devincli.FloorString, diag.CodeDoctorDevinVersionUnknown); err != nil {
			return err
		}
	}

	selected, scopes := devinHarnessSelection(ec)
	switch {
	case selected:
		if _, err := fmt.Fprintf(out, "devin harness: selected (%s)\n", strings.Join(scopes, ", ")); err != nil {
			return err
		}
	case probe.Installed():
		// "recommends selecting the harness when installed but unconfigured".
		// A recommendation only: "`doctor --repair` owns aggregate-state
		// recovery only; it does not silently edit harness lists."
		slog.Info("recommending the devin harness: a devin cli is installed but no harness list selects it",
			"binary_path", probe.Path, "code", diag.CodeDoctorDevinRecommended)
		if _, err := fmt.Fprintf(out, "devin harness: not selected — a Devin CLI is installed; add \"devin\" to a harnesses list (TUI: press H) [%s]\n",
			diag.CodeDoctorDevinRecommended); err != nil {
			return err
		}
	default:
		if _, err := fmt.Fprintln(out, "devin harness: not selected"); err != nil {
			return err
		}
	}
	return nil
}

func writeDoctorRequirementWarnings(out io.Writer, ctx context.Context, repoRoot string) error {
	slog.Debug("doctor prerequisite resolve start")
	ws, err := pipeline.NewWorkspace(repoRoot, config.FindLocalConfigDir())
	if err != nil {
		return err
	}
	resolved, releaseExactLease := pipeline.ResolveWithExactLockLease(ctx, pipeline.ResolveOptions{
		Workspace:          ws,
		CatalogMode:        catalog.LoadModeInspect,
		ReadOnlyExactLocks: true,
	})
	defer releaseExactLease()
	var requirementDiagnostics []error
	for _, resolveErr := range resolved.ResolveErrors {
		var warning *resolver.UnmetRequirementWarning
		if !errors.As(resolveErr, &warning) {
			continue
		}
		requirementDiagnostics = append(requirementDiagnostics, warning)
	}
	requirementDiagnostics = append(requirementDiagnostics, requiresInvalidWarnings(resolved.CatalogLoad.Catalogs)...)
	slices.SortFunc(requirementDiagnostics, func(a, b error) int {
		return strings.Compare(a.Error(), b.Error())
	})
	slog.Debug("doctor prerequisite resolve end", "unmet_requirements", len(requirementDiagnostics))
	if err := writeGroupedWarnings(out, requirementDiagnostics, false); err != nil {
		slog.Error("doctor prerequisite warning output failed", "error", err)
		return fmt.Errorf("write doctor prerequisite warnings: %w", err)
	}

	return nil
}

// devinHarnessSelection reports whether any config layer selects the devin
// target, and which layers do.
//
// It reads the already-loaded EffectiveConfig rather than re-reading the files,
// so it reports the same layers status does, including the externally managed
// v3 organization layer. A layer that could not be loaded yields "not
// selected" rather than an error: doctor's job is to explain a broken
// environment, not to fail inside it.
func devinHarnessSelection(ec config.EffectiveConfig) (bool, []string) {
	var scopes []string
	add := func(scope string, layer config.LayerResult) {
		if layer.Config == nil {
			if len(layer.Errors) > 0 {
				slog.Debug("doctor could not read a config layer while checking harness selection",
					"layer", scope, "path", layer.Path, "errors", errors.Join(layer.Errors...))
			}
			return
		}
		if slices.Contains(layer.Config.Harnesses, string(hcc.TargetDevin)) {
			scopes = append(scopes, scope)
		}
	}
	add("org", ec.Organization)
	add("global", ec.User)
	add("project", ec.Repo)
	return len(scopes) > 0, scopes
}

// runDoctorRepair implements the plan's repair contract verbatim:
//
//	"`doctor --repair` previews exact adoption matches and evidence-backed
//	orphan candidates; on a TTY it then prompts for each authorized action"
//	"off-TTY, `doctor --repair` is preview-only"
//	"`doctor --repair --yes` adopts exact current desired matches without prompting"
//	"orphan-removal candidates require ... explicit per-entry confirmation and a fresh backup"
//	"exit 0 means clean, repaired, or declined without operational failure"
//	"exit 1 means corrupt state/file or partial repair failure"
//
// It also offers the third repair class (bead agent-marketplace-0w2g): an
// existing unmanaged entry whose key matches a desired entry but whose value
// differs. It is destructive, so it follows the orphan-removal half of the
// contract in every respect — TTY-only, per-entry confirmation, fresh backup,
// never under --yes.
//
// The exit code is produced by returning a non-nil error, which main turns into
// exit 1. Every other path returns nil, including "nothing to repair", "preview
// only", and "the user said no to everything".
func runDoctorRepair(out io.Writer, opts doctorOptions, repoRoot string) error {
	ctx := opts.ctx
	slog.Info("doctor repair preview start", "repo_root", repoRoot)
	selection, err := opts.resolve(ctx, repoRoot)
	if err != nil {
		return err
	}
	previews, err := materialize.RepairPreviews(ctx, repoRoot, selection)
	if err != nil {
		slog.Error("doctor repair preview failed", "repo_root", repoRoot, "status", "error", "code", diag.CodeDoctorRepairFailed)
		return fmt.Errorf("repair preview: %w", err)
	}

	actionable := false
	corrupt := false
	for _, targetPreview := range previews {
		if _, err := fmt.Fprintf(out, "repair: %s shared-aggregate ownership\n", targetDisplayName(string(targetPreview.Target))); err != nil {
			return err
		}
		if err := writeRepairDiagnostics(out, targetPreview.Preview); err != nil {
			return err
		}
		if err := writeRepairPreview(out, targetPreview.Preview); err != nil {
			return err
		}
		actionable = actionable || targetPreview.Preview.Actionable()
		corrupt = corrupt || targetPreview.Preview.HasErrors()
	}
	slog.Info("doctor repair preview complete", "repo_root", repoRoot, "targets", len(previews), "actionable", actionable, "corrupt", corrupt)

	// A corrupt ownership record or an unparseable aggregate is reported and
	// never repaired around: "exit 1 means corrupt state/file". Actions are
	// still offered for the files that ARE readable, because per-file
	// independence is the same rule sync follows.
	if !actionable {
		if _, err := fmt.Fprintln(out, "  nothing to repair"); err != nil {
			return err
		}
		if corrupt {
			slog.Error("doctor repair execution failed", "repo_root", repoRoot, "status", "error", "corrupt", true, "failures", 0, "code", diag.CodeDoctorRepairFailed)
			return errors.New("doctor --repair: corrupt ownership state or aggregate file")
		}
		slog.Info("doctor repair execution complete", "repo_root", repoRoot, "status", "success", "actions", 0)
		return nil
	}

	if !opts.isTTY && !opts.yes {
		// "off-TTY, `doctor --repair` is preview-only". Nothing below this point
		// runs, and every target preview has no mutation path, so this is a
		// structural guarantee rather than a promise.
		if _, err := fmt.Fprintln(out, "  preview only: not a terminal; re-run on a terminal to confirm, or use --repair --yes to adopt exact matches"); err != nil {
			return err
		}
		slog.Info("doctor repair preview-only: stdin is not a terminal", "repo_root", repoRoot)
		if corrupt {
			slog.Error("doctor repair execution failed", "repo_root", repoRoot, "status", "error", "corrupt", true, "failures", 0, "code", diag.CodeDoctorRepairFailed)
			return errors.New("doctor --repair: corrupt ownership state or aggregate file")
		}
		return nil
	}

	slog.Info("doctor repair execution start", "repo_root", repoRoot, "targets", len(previews), "automatic_adoption", opts.yes)
	var failures []error
	for _, targetPreview := range previews {
		failures = append(failures, applyRepairActions(out, opts, targetPreview.Preview)...)
	}
	if len(failures) > 0 {
		for _, failure := range failures {
			if _, err := fmt.Fprintf(out, "  repair failed: %v [%s]\n", failure, diag.CodeDoctorRepairFailed); err != nil {
				return err
			}
		}
		slog.Error("doctor repair execution failed", "repo_root", repoRoot, "status", "error", "corrupt", corrupt, "failures", len(failures), "code", diag.CodeDoctorRepairFailed)
		return fmt.Errorf("doctor --repair: %d repair action(s) failed", len(failures))
	}
	if corrupt {
		slog.Error("doctor repair execution failed", "repo_root", repoRoot, "status", "error", "corrupt", true, "failures", 0, "code", diag.CodeDoctorRepairFailed)
		return errors.New("doctor --repair: corrupt ownership state or aggregate file")
	}
	slog.Info("doctor repair execution complete", "repo_root", repoRoot, "status", "success")
	return nil
}

// writeRepairDiagnostics prints each preview diagnostic AT MOST ONCE.
//
// The preview derives its state per aggregate file, so one cause can legitimately
// produce several diagnostics — but a line that is byte-identical to one already
// printed tells the user nothing the first line did not, and printing it twice
// only makes them doubt the tool (a beta tester saw the same ownership warning
// twice for one root and could not tell whether two things were wrong). Any
// diagnostic that genuinely concerns a different aggregate still prints: the
// Devin reporter names the root and the file in its messages precisely so those
// lines differ.
//
// The key is the whole DIAGNOSTIC, not just the rendered line, because the line
// drops fields a diagnostic can differ by. Two rules compiled for Devin, for
// instance, both carry the constant rule.devin.unsupported message and differ
// only in Resource; suppressing the second would hide the fact that two
// resources are affected. Only a diagnostic that is identical in every field the
// reporter carries is dropped.
//
// Target is part of the key for the same reason, even though nothing under
// doctor --repair emits two targets today: a key that silently stops
// distinguishing a field the diagnostic carries is exactly how a per-file
// failure gets lost, and widening the key can only ever print MORE.
func writeRepairDiagnostics(out io.Writer, preview *materialize.RepairPreview) error {
	printed := make(map[string]struct{}, len(preview.Diags))
	for _, d := range preview.Diags {
		// diag.Severity counts DOWN (error 0, warning 1, info 2), so "at least
		// a warning" is <=, not >=. Info-level preview noise is suppressed.
		if d.Severity > diag.SeverityWarning {
			continue
		}
		message := d.Message
		if !strings.Contains(message, d.Code) {
			message = fmt.Sprintf("%s [%s]", message, d.Code)
		}
		line := fmt.Sprintf("  %s: %s\n", d.Severity, message)
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%s",
			d.Code, d.Target, d.Resource, d.Source.File, d.Source.Line, line)
		if _, seen := printed[key]; seen {
			slog.Debug("suppressed a duplicate repair diagnostic",
				"code", d.Code, "file", d.Source.File, "resource", d.Resource)
			continue
		}
		printed[key] = struct{}{}
		if _, err := fmt.Fprint(out, line); err != nil {
			return err
		}
	}
	return nil
}

// writeRepairPreview prints what could be repaired, and — for every aggregate it
// inspected — what could not.
//
// Orphan candidates are described by their EVIDENCE and never as owned: "orphan
// candidates are never described as currently owned". Conflicting entries are
// described by the FACT that the values differ plus both digests, never by
// either value: an MCP entry holds env values, headers, and OAuth secrets.
//
// A file with no candidates used to be skipped silently, which turned "nothing
// to repair" into an unexplained dead end: a beta tester whose every sync
// reported a collision could not tell whether repair had even looked at the file
// they were staring at. Naming the inspected file plus its entry counts makes
// "nothing to repair" a statement about known files.
func writeRepairPreview(out io.Writer, preview *materialize.RepairPreview) error {
	for _, file := range preview.Files {
		if len(file.Adoptions) == 0 && len(file.Orphans) == 0 && len(file.Conflicts) == 0 {
			if _, err := fmt.Fprintf(out, "  %s: no repair candidates (inspected %d existing, %d desired entr(y/ies))\n",
				file.AbsFile(), file.ExistingEntries, file.DesiredEntries); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, "  %s\n", file.AbsFile()); err != nil {
			return err
		}
		for _, candidate := range file.Adoptions {
			if _, err := fmt.Fprintf(out, "    adopt %s %q (exact match for resource %q)\n",
				candidate.EntryKind(), candidate.Identity(), candidate.Resource); err != nil {
				return err
			}
		}
		for _, candidate := range file.Conflicts {
			if _, err := fmt.Fprintf(out, "    replace %s %q (resource %q): the existing entry has the same key but a different value — %s [%s]\n",
				candidate.EntryKind(), candidate.Identity(), candidate.Resource, candidate.Difference(),
				diag.CodeDoctorRepairConflictReplaceable); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(out, "      replacing DELETES the existing value; it needs an interactive per-entry confirmation and is never done by --yes"); err != nil {
				return err
			}
		}
		for _, candidate := range file.Orphans {
			if _, err := fmt.Fprintf(out, "    orphan candidate %s %q (evidence: %s)\n",
				candidate.EntryKind(), candidate.Identity(), candidate.Evidence()); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyRepairActions performs the authorized actions and returns the failures.
//
// The three classes are authorized along ONE line: does acting destroy bytes the
// user wrote?
//
//   - Adoption changes no bytes of the user's file; it only records that an
//     entry which is already byte-identical to marketplace output is ours.
//     "`doctor --repair --yes` adopts exact current desired matches without
//     prompting" therefore covers it entirely.
//   - Orphan removal DELETES a user-visible entry, and the plan requires
//     "explicit per-entry confirmation" with no exception for --yes. So --yes
//     never removes anything: off a terminal, orphan candidates are reported
//     and left alone.
//   - Replacing a conflicting entry OVERWRITES a value the user wrote, so it
//     inherits orphan removal's rules exactly. opts.yes is deliberately not
//     consulted in that loop at all: there is no code path from --yes to a
//     replacement, and ownership.Apply would refuse an unconfirmed one anyway.
func applyRepairActions(out io.Writer, opts doctorOptions, preview *materialize.RepairPreview) []error {
	var failures []error
	// bufio.NewReader(nil) returns a NON-nil reader wrapping a nil io.Reader,
	// which panics on the first prompt. The nil reader is passed through
	// deliberately so confirmPrompt's nil guard is the live fail-closed answer:
	// no stdin is no consent, and a destructive class must decline rather than
	// crash halfway through a file's candidate list.
	var reader *bufio.Reader
	if opts.in != nil {
		reader = bufio.NewReader(opts.in)
	}

	for _, file := range preview.Files {
		var adopt []int
		for i, candidate := range file.Adoptions {
			if opts.yes {
				logDoctorRepairDecision(file, "adopt", "authorized", candidate.EntryKind(), candidate.Identity(), "--yes")
				adopt = append(adopt, i)
				continue
			}
			prompt := fmt.Sprintf("  adopt %s %q in %s? [y/N] ", candidate.EntryKind(), candidate.Identity(), file.Rel)
			ok, err := confirmPrompt(out, reader, prompt)
			if err != nil {
				slog.Error("doctor repair decision failed", "target", file.Target, "file", file.Rel, "action", "adopt", "kind", candidate.EntryKind(), "identity", candidate.Identity(), "code", diag.CodeDoctorRepairFailed)
				failures = append(failures, fmt.Errorf("read confirmation for %s %q: %w", candidate.EntryKind(), candidate.Identity(), err))
				break
			}
			if ok {
				logDoctorRepairDecision(file, "adopt", "authorized", candidate.EntryKind(), candidate.Identity(), "interactive")
				adopt = append(adopt, i)
			} else {
				logDoctorRepairDecision(file, "adopt", "declined", candidate.EntryKind(), candidate.Identity(), "interactive")
			}
		}
		if len(adopt) > 0 {
			if _, err := file.Adopt(adopt); err != nil {
				slog.Error("doctor repair action failed", "target", file.Target, "file", file.Rel, "action", "adopt", "entries", len(adopt), "code", diag.CodeDoctorRepairFailed)
				failures = append(failures, fmt.Errorf("adopt %d entr(y/ies) in %s: %w", len(adopt), file.Rel, err))
			} else if _, err := fmt.Fprintf(out, "  adopted %d entr(y/ies) in %s\n", len(adopt), file.Rel); err != nil {
				failures = append(failures, err)
			}
		}

		for i, candidate := range file.Conflicts {
			if !opts.isTTY {
				// Off a terminal — including under --yes — a conflict is
				// reported and left alone. There is no consent to be had from a
				// pipe, and the existing value is the user's.
				if _, err := fmt.Fprintf(out, "  conflicting %s %q in %s left unchanged: replacing it deletes user-written content and requires an interactive per-entry confirmation\n",
					candidate.EntryKind(), candidate.Identity(), file.Rel); err != nil {
					failures = append(failures, err)
				}
				logDoctorRepairDecision(file, "replace", "deferred", candidate.EntryKind(), candidate.Identity(), "non-interactive")
				continue
			}
			prompt := fmt.Sprintf("  replace %s %q in %s with the generated entry? this deletes the existing value [y/N] ",
				candidate.EntryKind(), candidate.Identity(), file.Rel)
			confirmed, err := confirmPrompt(out, reader, prompt)
			if err != nil {
				slog.Error("doctor repair decision failed", "target", file.Target, "file", file.Rel, "action", "replace", "kind", candidate.EntryKind(), "identity", candidate.Identity(), "code", diag.CodeDoctorRepairFailed)
				failures = append(failures, fmt.Errorf("read confirmation for %s %q: %w", candidate.EntryKind(), candidate.Identity(), err))
				break
			}
			if !confirmed {
				// A decline is a success, and it leaves the file byte-identical.
				if _, err := fmt.Fprintf(out, "  declined: %s %q in %s left unchanged (the existing value is kept)\n",
					candidate.EntryKind(), candidate.Identity(), file.Rel); err != nil {
					failures = append(failures, err)
				}
				logDoctorRepairDecision(file, "replace", "declined", candidate.EntryKind(), candidate.Identity(), "interactive")
				continue
			}
			logDoctorRepairDecision(file, "replace", "authorized", candidate.EntryKind(), candidate.Identity(), "interactive")
			if _, err := file.ReplaceConflict(i, true); err != nil {
				slog.Error("doctor repair action failed", "target", file.Target, "file", file.Rel, "action", "replace", "kind", candidate.EntryKind(), "identity", candidate.Identity(), "code", diag.CodeDoctorRepairFailed)
				failures = append(failures, fmt.Errorf("replace %s %q in %s: %w", candidate.EntryKind(), candidate.Identity(), file.Rel, err))
				continue
			}
			if _, err := fmt.Fprintf(out, "  replaced %s %q in %s with the generated entry\n",
				candidate.EntryKind(), candidate.Identity(), file.Rel); err != nil {
				failures = append(failures, err)
			}
		}

		for i, candidate := range file.Orphans {
			if !opts.isTTY {
				// --yes on a non-terminal: reported, never removed.
				if _, err := fmt.Fprintf(out, "  orphan candidate %s %q in %s left unchanged: removal requires an interactive per-entry confirmation\n",
					candidate.EntryKind(), candidate.Identity(), file.Rel); err != nil {
					failures = append(failures, err)
				}
				logDoctorRepairDecision(file, "remove", "deferred", candidate.EntryKind(), candidate.Identity(), "non-interactive")
				continue
			}
			prompt := fmt.Sprintf("  remove %s %q from %s? this deletes the entry [y/N] ", candidate.EntryKind(), candidate.Identity(), file.Rel)
			confirmed, err := confirmPrompt(out, reader, prompt)
			if err != nil {
				slog.Error("doctor repair decision failed", "target", file.Target, "file", file.Rel, "action", "remove", "kind", candidate.EntryKind(), "identity", candidate.Identity(), "code", diag.CodeDoctorRepairFailed)
				failures = append(failures, fmt.Errorf("read confirmation for %s %q: %w", candidate.EntryKind(), candidate.Identity(), err))
				break
			}
			if !confirmed {
				// "exit 0 means clean, repaired, or declined without
				// operational failure": a decline is a success.
				if _, err := fmt.Fprintf(out, "  declined: %s %q in %s left unchanged\n", candidate.EntryKind(), candidate.Identity(), file.Rel); err != nil {
					failures = append(failures, err)
				}
				logDoctorRepairDecision(file, "remove", "declined", candidate.EntryKind(), candidate.Identity(), "interactive")
				continue
			}
			logDoctorRepairDecision(file, "remove", "authorized", candidate.EntryKind(), candidate.Identity(), "interactive")
			if _, err := file.RemoveOrphan(i, true); err != nil {
				slog.Error("doctor repair action failed", "target", file.Target, "file", file.Rel, "action", "remove", "kind", candidate.EntryKind(), "identity", candidate.Identity(), "code", diag.CodeDoctorRepairFailed)
				failures = append(failures, fmt.Errorf("remove %s %q from %s: %w", candidate.EntryKind(), candidate.Identity(), file.Rel, err))
				continue
			}
			if _, err := fmt.Fprintf(out, "  removed %s %q from %s\n", candidate.EntryKind(), candidate.Identity(), file.Rel); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return failures
}

func logDoctorRepairDecision(file *materialize.RepairFile, action, decision, kind, identity, authorization string) {
	slog.Info("doctor repair decision",
		"target", file.Target, "file", file.Rel, "action", action, "decision", decision,
		"kind", kind, "identity", identity, "authorization", authorization)
}

// confirmPrompt asks one yes/no question. Anything that is not an explicit
// "y"/"yes" is a NO, and EOF is a NO rather than an error, so a closed stdin
// declines instead of deleting.
func confirmPrompt(out io.Writer, reader *bufio.Reader, prompt string) (bool, error) {
	if reader == nil {
		return false, nil
	}
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return false, err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if errors.Is(err, io.EOF) && answer == "" {
		if _, printErr := fmt.Fprintln(out); printErr != nil {
			return false, printErr
		}
		return false, nil
	}
	return answer == "y" || answer == "yes", nil
}

func hccVersion() string {
	return "embedded (pkg/harness-compilation)"
}

func writeDoctorManagedSection(out io.Writer, ec config.EffectiveConfig) error {
	d := ec.Diagnostics
	if _, err := fmt.Fprintln(out, "MANAGED INSTALLATION"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "state: %s\n", d.State); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "path: %s\n", d.Path); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "validation: %s\n", d.Validation); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "owner: %s\n", d.Owner); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "group: %s\n", d.Group); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "mode: 0%o\n", d.Mode.Perm()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "type: %s\n", d.FileType); err != nil {
		return err
	}
	if d.SymlinkTarget != "" {
		if _, err := fmt.Fprintf(out, "symlink target: %s\n", d.SymlinkTarget); err != nil {
			return err
		}
	}
	for _, w := range d.Warnings {
		if _, err := fmt.Fprintf(out, "warning: %v\n", w); err != nil {
			return err
		}
		if r, ok := w.(interface{ Remediation() string }); ok {
			if _, err := fmt.Fprintf(out, "  -> %s\n", r.Remediation()); err != nil {
				return err
			}
		}
	}
	if err := writeDoctorErrorList(out, ec.Organization.Errors); err != nil {
		return err
	}
	if d.State != config.ManagedInstallationManaged {
		if _, err := fmt.Fprintln(out, "run 'agent-marketplace status' to see the effective configuration"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out)
	return err
}

func writeDoctorConfigurationErrorsSection(out io.Writer, ec config.EffectiveConfig) error {
	configErrs := ec.Errors[len(ec.Organization.Errors):]
	if len(configErrs) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "CONFIGURATION ERRORS"); err != nil {
		return err
	}
	if err := writeDoctorErrorList(out, configErrs); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out)
	return err
}

func writeDoctorErrorList(out io.Writer, errs []error) error {
	for _, e := range errs {
		if _, err := fmt.Fprintf(out, "error: %v\n", e); err != nil {
			return err
		}
	}
	return nil
}
