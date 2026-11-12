package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/remedy"
	repairplanner "github.com/Adyen/agent-marketplace/internal/repair"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/tui"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func runDoctorRepairPlanner(out io.Writer, opts doctorOptions, repoRoot string, initial config.EffectiveConfig) error {
	slog.Info("doctor repair preview start", "repo_root", repoRoot)
	ctx := opts.ctx
	var reads []dependencytxn.ReadAssertion
	var policyErr error
	if len(initial.Errors) == 0 {
		reads, policyErr = pipeline.EffectivePolicyReadAssertions(initial)
	}
	revalidate := func() error {
		if policyErr != nil {
			return policyErr
		}
		return dependencytxn.ValidateReadAssertions(reads)
	}
	if len(initial.Errors) == 0 && policyErr == nil {
		if err := revalidate(); err != nil {
			return doctorRepairFailure(out, err)
		}
	}

	var previews []materialize.TargetRepairPreview
	previewsShown := false
	deferredShown := false
	firstDiagnosis := true
	corrupt := false
	ownershipActionable := false
	var incompleteCause error
	diagnose := func(ctx context.Context) (repairplanner.Diagnosis, error) {
		current := initial
		if !firstDiagnosis {
			ws, err := pipeline.DiscoverWorkspace()
			if err != nil {
				return repairplanner.Diagnosis{}, err
			}
			current = pipeline.LoadEffective(ctx, pipeline.ConfigLoadOnline, config.LoadEffectiveOptions{
				RepoRoot: ws.RepoRoot(), LocalConfigDir: ws.LocalConfigDir(),
			}, nil)
		}
		firstDiagnosis = false
		previews = nil
		corrupt = false
		ownershipActionable = false
		incompleteCause = nil
		diagnosis := repairplanner.Diagnosis{InvalidInputs: make(map[repairplanner.Input]repairplanner.Blocker)}
		addHuman := func(stage repairplanner.Stage, code string, issue error) {
			action := tui.TerminalSafeLine(issue.Error())
			if hint := remedy.For(issue, "doctor"); hint != "" {
				action += "; " + tui.TerminalSafeLine(hint)
			}
			diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
				Stage: stage, Class: repairplanner.HumanOnly, Action: action, Code: code,
			})
		}
		blockSelection := func(code, reason string) {
			diagnosis.InvalidInputs[repairplanner.InputSelection] = repairplanner.Blocker{Code: code, Reason: reason}
			diagnosis.Steps = append(diagnosis.Steps,
				repairplanner.Step{
					Stage: repairplanner.StageDrift, Class: repairplanner.Safe,
					Action: "sync harness drift", Code: "sync.drift",
					Files: []string{"selected harness outputs"}, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				},
				repairplanner.Step{
					Stage: repairplanner.StageOwnership, Class: repairplanner.Safe,
					Action: "reconcile shared-aggregate ownership", Code: "ownership.reconcile",
					Files: []string{"shared aggregate files"}, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				},
			)
		}
		if len(current.Errors) > 0 {
			incompleteCause = errors.Join(current.Errors...)
			for _, issue := range current.Errors {
				addHuman(repairplanner.StageConfiguration, "config.invalid", issue)
			}
			blockSelection("config.invalid", "configuration invalid")
			return diagnosis, nil
		}
		if policyErr != nil {
			incompleteCause = policyErr
			addHuman(repairplanner.StageConfiguration, "policy.unavailable", fmt.Errorf("capture policy assertions: %w", policyErr))
			blockSelection("policy.unavailable", "policy assertions unavailable")
			return diagnosis, nil
		}

		var selection []resolver.ResolvedArtifact
		var resolveErr error
		if opts.resolve != nil {
			selection, resolveErr = opts.resolve(ctx, repoRoot)
		} else {
			ws, err := pipeline.NewWorkspace(repoRoot, config.FindLocalConfigDir())
			if err != nil {
				resolveErr = fmt.Errorf("construct repair workspace: %w", err)
			} else {
				resolved, release := pipeline.ResolveWithExactLockLease(ctx, pipeline.ResolveOptions{
					Workspace: ws, CatalogMode: catalog.LoadModeCached, ReadOnlyExactLocks: true,
				})
				resolveErr = errors.Join(resolved.Errors...)
				if resolveErr == nil {
					selection = resolved.Resolved
				}
				release()
			}
		}
		if resolveErr != nil {
			incompleteCause = resolveErr
			addHuman(repairplanner.StageLocks, "selection.unavailable", resolveErr)
			blockSelection("selection.unavailable", "selection unresolved")
			return diagnosis, nil
		}

		if opts.resolve == nil && len(current.TargetHarnesses) > 0 {
			var checkOutput strings.Builder
			checkErr := runSyncCheck(ctx, &checkOutput, false)
			checkText := checkOutput.String()
			sharedCollision := strings.Contains(checkText, diag.CodeOwnershipUnmanagedKey) ||
				strings.Contains(checkText, "materialize.collision.user-owned")
			if strings.Contains(checkText, "out of sync") && !sharedCollision {
				diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
					Stage: repairplanner.StageDrift, Class: repairplanner.Safe,
					Action: "sync --locked harness drift", Code: "sync.drift",
					Files: syncCheckFiles(checkText), Inputs: []repairplanner.Input{repairplanner.InputSelection},
					Apply: func(ctx context.Context) (bool, error) {
						return true, runSyncWithLocks(ctx, out, false, false, false, false, false, lockRunOptions{locked: true})
					},
				})
			} else if checkErr != nil && !sharedCollision {
				incompleteCause = checkErr
				addHuman(repairplanner.StageLocks, "sync.check", fmt.Errorf("sync drift diagnosis: %w", checkErr))
				blockSelection("sync.check", "sync check could not resolve selection")
				return diagnosis, nil
			}
		}

		var err error
		previews, err = materialize.RepairPreviews(ctx, repoRoot, selection, revalidate)
		if err != nil {
			incompleteCause = err
			slog.Error("doctor repair preview failed", "repo_root", repoRoot, "status", "error", "code", diag.CodeDoctorRepairFailed)
			addHuman(repairplanner.StageOwnership, "ownership.preview", fmt.Errorf("inspect shared-aggregate ownership: %w", err))
			return diagnosis, nil
		}
		adoptions, adoptionFiles := repairCandidateGroup(previews, true)
		destructive, destructiveFiles := repairCandidateGroup(previews, false)
		if len(adoptions) > 0 {
			ownershipActionable = true
			diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
				Stage: repairplanner.StageOwnership, Class: repairplanner.Safe,
				Action: "adopt exact shared-aggregate matches", Code: "ownership.adopt",
				Files: adoptionFiles, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				Apply: func(ctx context.Context) (bool, error) {
					stepOpts := opts
					stepOpts.repairReader = opts.repairReader
					stepOpts.planAuthorized = true
					return applyRepairGroup(out, stepOpts, adoptions, revalidate)
				},
			})
		}
		if len(destructive) > 0 {
			ownershipActionable = true
			diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
				Stage: repairplanner.StageOwnership, Class: repairplanner.Destructive,
				Action: "replace conflicts or remove orphan entries", Code: "ownership.destructive",
				Files: destructiveFiles, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				Apply: func(ctx context.Context) (bool, error) {
					stepOpts := opts
					stepOpts.repairReader = opts.repairReader
					return applyRepairGroup(out, stepOpts, destructive, revalidate)
				},
			})
		}
		for _, target := range previews {
			if !target.Preview.HasErrors() {
				continue
			}
			corrupt = true
			addHuman(repairplanner.StageOwnership, diag.CodeDoctorRepairFailed,
				fmt.Errorf("corrupt ownership state or aggregate file: %s", repairDiagnosticSummary(target.Preview.Diags)))
		}
		slog.Info("doctor repair preview complete", "repo_root", repoRoot, "targets", len(previews),
			"actionable", ownershipActionable, "corrupt", corrupt)
		return diagnosis, nil
	}
	display := func(steps []repairplanner.Step) error {
		if len(steps) > 0 {
			if err := repairplanner.WritePlan(out, steps); err != nil {
				return err
			}
		}
		if !previewsShown && len(previews) > 0 {
			for _, target := range previews {
				if _, err := fmt.Fprintf(out, "repair: %s shared-aggregate ownership\n", targetDisplayName(string(target.Target))); err != nil {
					return err
				}
				if err := writeRepairDiagnostics(out, target.Preview); err != nil {
					return err
				}
				if err := writeRepairPreview(out, target.Preview); err != nil {
					return err
				}
			}
			previewsShown = true
		}
		if !ownershipActionable && len(previews) > 0 {
			if _, err := fmt.Fprintln(out, "  nothing to repair"); err != nil {
				return err
			}
		}
		if opts.yes && !opts.isTTY && !deferredShown {
			deferredShown = true
			deferred, _ := repairCandidateGroup(previews, false)
			for _, target := range deferred {
				noTerminal := opts
				noTerminal.isTTY = false
				if failures := applyRepairActions(out, noTerminal, target.Preview, revalidate); len(failures) > 0 {
					return errors.Join(failures...)
				}
			}
		}
		return nil
	}

	if !opts.isTTY && !opts.yes {
		diagnosis, err := diagnose(ctx)
		if err != nil {
			return doctorRepairFailure(out, err)
		}
		steps := repairplanner.Plan(diagnosis)
		if err := display(steps); err != nil {
			return doctorRepairFailure(out, err)
		}
		if len(steps) > 0 {
			if _, err := fmt.Fprintln(out, "  preview only: not a terminal; re-run on a terminal to confirm, or use --repair --yes to adopt exact matches"); err != nil {
				return err
			}
		}
		if hasBlockedSteps(steps) {
			return repairIncomplete(corrupt, incompleteCause)
		}
		return nil
	}

	if opts.in != nil {
		opts.repairReader = bufio.NewReader(opts.in)
	}
	var confirmErr error
	loop := repairplanner.Loop{
		Diagnose:         diagnose,
		Display:          display,
		Validate:         revalidate,
		AllowDestructive: opts.isTTY,
		Confirm: func([]repairplanner.Step) bool {
			if opts.yes {
				return true
			}
			accepted, err := confirmPrompt(out, opts.repairReader, "Repair the safe steps above? [y/N] ")
			confirmErr = err
			return accepted
		},
	}
	slog.Info("doctor repair execution start", "repo_root", repoRoot, "targets", len(previews), "automatic_adoption", opts.yes)
	err := loop.Run(ctx)
	if confirmErr != nil {
		err = confirmErr
	}
	if errors.Is(err, repairplanner.ErrIncomplete) {
		err = repairIncomplete(corrupt, incompleteCause)
		slog.Error("doctor repair execution failed", "repo_root", repoRoot, "status", "error", "corrupt", corrupt, "code", diag.CodeDoctorRepairFailed)
		return err
	}
	if err != nil {
		return doctorRepairFailure(out, err)
	}
	slog.Info("doctor repair execution complete", "repo_root", repoRoot, "status", "success")
	return nil
}

func repairCandidateGroup(previews []materialize.TargetRepairPreview, adoptions bool) ([]materialize.TargetRepairPreview, []string) {
	var targets []materialize.TargetRepairPreview
	var files []string
	for _, target := range previews {
		filtered := &materialize.RepairPreview{Diags: target.Preview.Diags}
		for _, file := range target.Preview.Files {
			selected := *file
			if adoptions {
				selected.Conflicts = nil
				selected.Orphans = nil
				if len(selected.Adoptions) == 0 {
					continue
				}
			} else {
				selected.Adoptions = nil
				if len(selected.Conflicts) == 0 && len(selected.Orphans) == 0 {
					continue
				}
			}
			filtered.Files = append(filtered.Files, &selected)
			files = append(files, fmt.Sprintf("%s:%s", targetDisplayName(string(target.Target)), filepath.Clean(file.AbsFile())))
		}
		if len(filtered.Files) > 0 {
			targets = append(targets, materialize.TargetRepairPreview{Target: target.Target, Preview: filtered})
		}
	}
	sort.Strings(files)
	return targets, files
}

func applyRepairGroup(out io.Writer, opts doctorOptions, targets []materialize.TargetRepairPreview, revalidate func() error) (bool, error) {
	var failures []error
	for _, target := range targets {
		failures = append(failures, applyRepairActions(out, opts, target.Preview, revalidate)...)
	}
	return len(failures) == 0 && len(targets) > 0, errors.Join(failures...)
}

func syncCheckFiles(output string) []string {
	var files []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "+ ") || strings.HasPrefix(line, "- ") {
			files = append(files, strings.TrimSpace(line[2:]))
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return []string{"selected harness outputs"}
	}
	return slices.Compact(files)
}

func hasBlockedSteps(steps []repairplanner.Step) bool {
	for _, step := range steps {
		if step.BlockedBy != nil || step.Class == repairplanner.HumanOnly || step.Class == repairplanner.Guided {
			return true
		}
	}
	return false
}

func repairIncomplete(corrupt bool, cause error) error {
	if corrupt {
		return errors.New("doctor --repair: corrupt ownership state or aggregate file")
	}
	if cause != nil {
		return fmt.Errorf("doctor --repair: %w", cause)
	}
	return errors.New("doctor --repair: blocked repair steps remain")
}

func doctorRepairFailure(out io.Writer, err error) error {
	slog.Error("doctor repair execution failed", "status", "error", "error", err, "code", diag.CodeDoctorRepairFailed)
	if errors.Is(err, dependencytxn.ErrGenerationConflict) {
		_, _ = fmt.Fprintf(out, "  repair failed: policy changed: %v [%s]\n", err, diag.CodeDoctorRepairFailed)
		return fmt.Errorf("doctor --repair: policy changed: %w", dependencytxn.ErrGenerationConflict)
	}
	_, _ = fmt.Fprintf(out, "  repair failed: %v [%s]\n", err, diag.CodeDoctorRepairFailed)
	return fmt.Errorf("doctor --repair: %w", err)
}

func repairDiagnosticSummary(diagnostics []diag.Diagnostic) string {
	var messages []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "" {
			messages = append(messages, diagnostic.Code+": "+diagnostic.Message)
		} else {
			messages = append(messages, diagnostic.Message)
		}
	}
	return strings.Join(messages, "; ")
}
