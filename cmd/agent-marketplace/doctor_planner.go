package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/processstart"
	"github.com/Adyen/agent-marketplace/internal/remedy"
	repairplanner "github.com/Adyen/agent-marketplace/internal/repair"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/tui"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func runDoctorRepairPlanner(out io.Writer, opts doctorOptions, repoRoot string, initial config.EffectiveConfig) error {
	slog.Info("doctor repair preview start", "repo_root", repoRoot)
	ctx := opts.ctx
	reads, policyErr := doctorRepairPolicyReadAssertions(initial)
	refreshReadSetAfterEdit := false
	revalidate := func() error {
		if policyErr != nil {
			return policyErr
		}
		return dependencytxn.ValidateReadAssertions(reads)
	}
	refreshEditedFile := func(path string) error {
		if policyErr != nil {
			return policyErr
		}
		canonical, err := dependencytxn.CanonicalConfigDestination(path)
		if err != nil {
			return err
		}
		otherReads := make([]dependencytxn.ReadAssertion, 0, len(reads))
		found := false
		for _, read := range reads {
			if read.Path == canonical {
				found = true
				continue
			}
			otherReads = append(otherReads, read)
		}
		if !found {
			return fmt.Errorf("%w: edited configuration was not captured at %s", dependencytxn.ErrGenerationConflict, canonical)
		}
		if err := dependencytxn.ValidateReadAssertions(otherReads); err != nil {
			return err
		}
		generation, err := dependencytxn.ReadGeneration(canonical)
		if err != nil {
			return err
		}
		for i := range reads {
			if reads[i].Path == canonical {
				reads[i].Expected = generation
			}
		}
		refreshReadSetAfterEdit = true
		return nil
	}
	if policyErr == nil {
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
			if refreshReadSetAfterEdit {
				if err := revalidate(); err != nil {
					return repairplanner.Diagnosis{}, err
				}
				refreshed, err := doctorRepairPolicyReadAssertions(current)
				if err != nil {
					return repairplanner.Diagnosis{}, err
				}
				if err := dependencytxn.ValidateReadAssertions(refreshed); err != nil {
					return repairplanner.Diagnosis{}, err
				}
				reads = refreshed
				refreshReadSetAfterEdit = false
			}
		}
		firstDiagnosis = false
		previews = nil
		corrupt = false
		ownershipActionable = false
		incompleteCause = nil
		diagnosis := repairplanner.Diagnosis{InvalidInputs: make(map[repairplanner.Input]repairplanner.Blocker)}
		addHuman := func(stage repairplanner.Stage, code string, issue error) string {
			action := tui.TerminalSafeLine(issue.Error())
			if hint := remedy.For(issue, "doctor"); hint != "" {
				action += "; " + tui.TerminalSafeLine(hint)
			}
			key := fmt.Sprintf("cause-%d", len(diagnosis.Steps)+1)
			diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
				Key: key, Stage: stage, Class: repairplanner.HumanOnly, Action: action, Code: code,
			})
			return key
		}
		blockSelection := func(key, reason string) {
			diagnosis.InvalidInputs[repairplanner.InputSelection] = repairplanner.Blocker{Key: key, Reason: reason}
			diagnosis.Steps = append(diagnosis.Steps,
				repairplanner.Step{
					Stage: repairplanner.StageDrift, Class: repairplanner.Safe,
					Action: "sync harness drift", Code: diag.CodeOwnershipEntryDrift,
					Files: []string{"selected harness outputs"}, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				},
				repairplanner.Step{
					Stage: repairplanner.StageOwnership, Class: repairplanner.Safe,
					Action: "reconcile shared-aggregate ownership", Code: diag.CodeOwnershipUnmanagedKey,
					Files: []string{"shared aggregate files"}, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				},
			)
		}
		if len(current.Errors) > 0 {
			incompleteCause = errors.Join(current.Errors...)
			var causeKey string
			seenLayerErrors := make(map[string]int)
			organizationPaths := doctorOrganizationConfigPaths(current)
			for _, layer := range current.LayerResults() {
				for _, issue := range layer.Errors {
					seenLayerErrors[issue.Error()]++
					key := addConfigurationIssue(&diagnosis, layer, issue, organizationPaths, addHuman, revalidate, refreshEditedFile, opts)
					if causeKey == "" {
						causeKey = key
					}
				}
			}
			for _, issue := range current.Errors {
				key := issue.Error()
				if seenLayerErrors[key] > 0 {
					seenLayerErrors[key]--
					continue
				}
				cause := addHuman(repairplanner.StageConfiguration, repairDiagnosticCode(issue), issue)
				if causeKey == "" {
					causeKey = cause
				}
			}
			blockSelection(causeKey, "configuration invalid")
			return diagnosis, nil
		}
		if policyErr != nil {
			incompleteCause = policyErr
			issue := fmt.Errorf("capture policy assertions: %w", policyErr)
			code := repairDiagnosticCode(issue)
			key := addHuman(repairplanner.StageConfiguration, code, issue)
			blockSelection(key, "policy assertions unavailable")
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
			code := repairDiagnosticCode(resolveErr)
			key := addHuman(repairplanner.StageLocks, code, resolveErr)
			blockSelection(key, "selection unresolved")
			return diagnosis, nil
		}

		if opts.resolve == nil && len(current.TargetHarnesses) > 0 {
			var checkOutput strings.Builder
			checkErr := runSyncCheck(ctx, &checkOutput, false)
			checkText := checkOutput.String()
			collisionFiles, otherCheckErrors := syncCheckCollisions(checkText, repoRoot)
			files := syncCheckFiles(checkText)
			files = slices.DeleteFunc(files, func(file string) bool {
				return slices.Contains(collisionFiles, filepath.Clean(file))
			})
			hasDrift := strings.Contains(checkText, "out of sync")
			if checkErr != nil && (otherCheckErrors ||
				len(collisionFiles) == 0 && (!hasDrift || len(files) == 0)) {
				incompleteCause = checkErr
				issue := fmt.Errorf("sync drift diagnosis: %w", checkErr)
				addHuman(repairplanner.StageDrift, repairDiagnosticCode(issue), issue)
			} else if hasDrift && len(files) > 0 {
				diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
					Stage: repairplanner.StageDrift, Class: repairplanner.Safe,
					Action: "sync --locked harness drift", Code: diag.CodeOwnershipEntryDrift,
					Files: files, Inputs: []repairplanner.Input{repairplanner.InputSelection},
					Apply: func(ctx context.Context) (bool, error) {
						err := runSyncWithLocks(ctx, out, false, false, false, false, false, lockRunOptions{locked: true, beforeMaterialize: revalidate})
						if err == nil || errors.Is(err, dependencytxn.ErrGenerationConflict) || len(collisionFiles) == 0 {
							return true, err
						}
						var checkOutput strings.Builder
						checkErr := runSyncCheck(ctx, &checkOutput, false)
						checkText := checkOutput.String()
						remainingCollisions, otherCheckErrors := syncCheckCollisions(checkText, repoRoot)
						if !strings.Contains(checkText, "out of sync") &&
							!otherCheckErrors &&
							len(remainingCollisions) > 0 &&
							!slices.ContainsFunc(remainingCollisions, func(file string) bool {
								return !slices.Contains(collisionFiles, file)
							}) {
							return true, nil
						}
						return true, errors.Join(err, checkErr)
					},
				})
			}
		}

		var err error
		previews, err = materialize.RepairPreviews(ctx, repoRoot, selection, revalidate)
		if err != nil {
			incompleteCause = err
			slog.Error("doctor repair preview failed", "repo_root", repoRoot, "status", "error", "code", diag.CodeDoctorRepairFailed)
			issue := fmt.Errorf("inspect shared-aggregate ownership: %w", err)
			addHuman(repairplanner.StageOwnership, repairDiagnosticCode(issue), issue)
			return diagnosis, nil
		}
		adoptions, adoptionFiles := repairCandidateGroup(previews, true, false, false)
		conflicts, conflictFiles := repairCandidateGroup(previews, false, true, false)
		orphans, orphanFiles := repairCandidateGroup(previews, false, false, true)
		ownershipActionable = len(adoptions)+len(conflicts)+len(orphans) > 0
		if len(adoptions) > 0 {
			diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
				Stage: repairplanner.StageOwnership, Class: repairplanner.Safe,
				Action: "adopt exact shared-aggregate matches", Code: diag.CodeOwnershipUnmanagedKey,
				Files: adoptionFiles, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				Apply: func(ctx context.Context) (bool, error) {
					return applyRepairGroup(out, opts, adoptions, revalidate)
				},
			})
		}
		if len(conflicts) > 0 {
			diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
				Stage: repairplanner.StageOwnership, Class: repairplanner.Destructive,
				Action: "replace conflicting shared-aggregate entries", Code: diag.CodeDoctorRepairConflictReplaceable,
				Files: conflictFiles, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				Apply: func(ctx context.Context) (bool, error) {
					return applyRepairGroup(out, opts, conflicts, revalidate)
				},
			})
		}
		if len(orphans) > 0 {
			diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
				Stage: repairplanner.StageOwnership, Class: repairplanner.Destructive,
				Action: "remove evidence-backed orphan entries", Code: diag.CodeOwnershipEntryDrift,
				Files: orphanFiles, Inputs: []repairplanner.Input{repairplanner.InputSelection},
				Apply: func(ctx context.Context) (bool, error) {
					return applyRepairGroup(out, opts, orphans, revalidate)
				},
			})
		}
		for _, target := range previews {
			if !target.Preview.HasErrors() {
				continue
			}
			corrupt = true
			addHuman(repairplanner.StageOwnership, repairDiagnosticCodeFromDiagnostics(target.Preview.Diags),
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
		for _, step := range steps {
			if step.Class == repairplanner.Guided && (opts.yes || !opts.isTTY) {
				if _, err := fmt.Fprintf(out, "  deferred: %s\n", tui.TerminalSafeLine(step.Action)); err != nil {
					return err
				}
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
			deferred, _ := repairCandidateGroup(previews, false, true, true)
			for _, target := range deferred {
				if failures := applyRepairActions(out, opts, target.Preview, revalidate); len(failures) > 0 {
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
		if repairplanner.HasBlockedSteps(steps) {
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
		AllowGuided:      opts.isTTY && !opts.yes,
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

func repairCandidateGroup(previews []materialize.TargetRepairPreview, adoptions, conflicts, orphans bool) ([]materialize.TargetRepairPreview, []string) {
	var targets []materialize.TargetRepairPreview
	var files []string
	for _, target := range previews {
		filtered := &materialize.RepairPreview{Diags: target.Preview.Diags}
		for _, file := range target.Preview.Files {
			selected := *file
			if !adoptions {
				selected.Adoptions = nil
			}
			if !conflicts {
				selected.Conflicts = nil
			}
			if !orphans {
				selected.Orphans = nil
			}
			if len(selected.Adoptions)+len(selected.Conflicts)+len(selected.Orphans) == 0 {
				continue
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
	var outputDir string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "→ ") {
			dir := strings.TrimPrefix(line, "→ ")
			if !filepath.IsAbs(dir) {
				if open := strings.Index(dir, " ("); open >= 0 && strings.HasSuffix(dir, ")") {
					dir = dir[open+2 : len(dir)-1]
				}
			}
			if strings.HasPrefix(dir, "~"+string(filepath.Separator)) {
				if home, err := os.UserHomeDir(); err == nil {
					dir = filepath.Join(home, dir[2:])
				}
			}
			if filepath.IsAbs(dir) {
				outputDir = dir
			}
			continue
		}
		if strings.HasPrefix(line, "+ ") || strings.HasPrefix(line, "- ") {
			file := strings.TrimSpace(line[2:])
			if !filepath.IsAbs(file) {
				file = filepath.Join(outputDir, file)
			}
			files = append(files, filepath.Clean(file))
		}
	}
	sort.Strings(files)
	return slices.Compact(files)
}

func syncCheckCollisions(output, repoRoot string) ([]string, bool) {
	var files []string
	otherErrors := false
	collisionError := false
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, diag.CodeOwnershipUnmanagedKey) ||
			strings.Contains(line, "materialize.collision.user-owned") {
			var collisionPath string
			for _, quoted := range strings.Split(line, `"`) {
				if filepath.IsAbs(quoted) {
					collisionPath = filepath.Clean(quoted)
					break
				}
			}
			if collisionPath == "" {
				var collisionFile string
				switch {
				case strings.Contains(line, "mcp_config.json"):
					collisionFile = "mcp_config.json"
				case strings.Contains(line, "config.toml"):
					collisionFile = "config.toml"
				case strings.Contains(line, "hooks.json"):
					collisionFile = "hooks.json"
				}
				if collisionFile != "" && (strings.Contains(line, "Codex") || strings.Contains(line, ".codex")) {
					collisionPath = filepath.Join(repoRoot, ".codex", collisionFile)
				} else if collisionFile != "" && (strings.Contains(line, "Devin") || strings.Contains(line, ".devin")) {
					collisionPath = filepath.Join(repoRoot, ".devin", collisionFile)
				}
			}
			if collisionPath != "" {
				files = append(files, collisionPath)
			}
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "error:") {
			fields := strings.Fields(strings.TrimSpace(line))
			code := ""
			if len(fields) > 1 {
				code = strings.TrimSuffix(fields[1], ":")
			}
			switch {
			case code == diag.CodeOwnershipUnmanagedKey || code == "materialize.collision.user-owned",
				strings.HasPrefix(code, "materialize.") && strings.HasSuffix(code, ".aggregate-collision"):
				collisionError = true
			default:
				otherErrors = true
			}
		}
	}
	if collisionError && len(files) == 0 {
		otherErrors = true
	}
	return slices.Compact(files), otherErrors
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

func repairDiagnosticCode(err error) string {
	var coded interface{ DiagnosticCode() string }
	if errors.As(err, &coded) && coded.DiagnosticCode() != "" {
		return coded.DiagnosticCode()
	}
	return diag.CodeDoctorRepairFailed
}

func repairDiagnosticCodeFromDiagnostics(diagnostics []diag.Diagnostic) string {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == diag.SeverityError && diagnostic.Code != "" {
			return diagnostic.Code
		}
	}
	return diag.CodeDoctorRepairFailed
}

func doctorRepairPolicyReadAssertions(ec config.EffectiveConfig) ([]dependencytxn.ReadAssertion, error) {
	reads, err := pipeline.EffectivePolicyReadAssertions(ec)
	if err != nil {
		return nil, err
	}
	byRequested := make(map[string]dependencytxn.ReadAssertion, len(reads))
	for _, read := range reads {
		byRequested[read.RequestedPath] = read
	}
	addPath := func(path string) error {
		if path == "" {
			return nil
		}
		canonical, err := dependencytxn.CanonicalConfigDestination(path)
		if err != nil {
			return err
		}
		generation, err := dependencytxn.ReadGeneration(canonical)
		if err != nil {
			return err
		}
		if prior, ok := byRequested[path]; ok {
			if prior.Path != canonical || (prior.Expected.Exists || prior.Expected.Digest != "") && prior.Expected != generation {
				return fmt.Errorf("%w: config read changed at %s", dependencytxn.ErrGenerationConflict, path)
			}
		}
		byRequested[path] = dependencytxn.ReadAssertion{Path: canonical, RequestedPath: path, Expected: generation}
		return nil
	}
	for _, layer := range ec.LayerResults() {
		if err := addPath(layer.Path); err != nil {
			return nil, err
		}
		if len(layer.Errors) == 0 {
			continue
		}
		for _, issue := range layer.Errors {
			var validation *config.V4ValidationError
			if !errors.As(issue, &validation) {
				continue
			}
			if err := addPath(repairConfigurationSourceFile(validation)); err != nil {
				return nil, err
			}
		}
	}
	for _, issue := range ec.Errors {
		var validation *config.V4ValidationError
		if !errors.As(issue, &validation) {
			continue
		}
		if err := addPath(repairConfigurationSourceFile(validation)); err != nil {
			return nil, err
		}
	}
	reads = reads[:0]
	for _, read := range byRequested {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].RequestedPath < reads[j].RequestedPath })
	return reads, nil
}

func addConfigurationIssue(
	diagnosis *repairplanner.Diagnosis,
	layer config.LayerResult,
	issue error,
	organizationPaths []string,
	addHuman func(repairplanner.Stage, string, error) string,
	revalidate func() error,
	refreshEditedFile func(string) error,
	opts doctorOptions,
) string {
	code := repairDiagnosticCode(issue)
	var validation *config.V4ValidationError
	if layer.Source == config.Organization || !errors.As(issue, &validation) || !guidedConfigurationError(validation.Code) {
		return addHuman(repairplanner.StageConfiguration, code, issue)
	}
	file := repairConfigurationSourceFile(validation)
	if file == "" {
		file = layer.Path
	}
	if aliases, err := configurationAliasesOrganization(file, organizationPaths); err != nil || aliases {
		return addHuman(repairplanner.StageConfiguration, code, issue)
	}
	line := repairDiagnosticLine(validation)
	source, err := config.ReadConfigFile(file)
	if err != nil {
		return addHuman(repairplanner.StageConfiguration, code, fmt.Errorf("read configuration source %s: %w", tui.TerminalSafeLine(file), err))
	}
	lines := strings.Split(string(source), "\n")
	if line < 1 || line > len(lines) {
		line = 1
	}
	action := tui.TerminalSafeLine(fmt.Sprintf("edit %s:%d %s; source: %s", file, line, issue.Error(), lines[line-1]))
	key := fmt.Sprintf("guided-%s-%s-%d-%s", layer.Source, file, line, code)
	diagnosis.Steps = append(diagnosis.Steps, repairplanner.Step{
		Key: key, Stage: repairplanner.StageConfiguration, Class: repairplanner.Guided,
		Action: action, Code: code, Files: []string{tui.TerminalSafeLine(file)},
		Apply: func(ctx context.Context) (bool, error) {
			return applyGuidedConfigurationEdit(ctx, opts, file, organizationPaths, revalidate, refreshEditedFile)
		},
	})
	return key
}

func doctorOrganizationConfigPaths(ec config.EffectiveConfig) []string {
	paths := make(map[string]struct{})
	add := func(path string) {
		if path != "" {
			paths[path] = struct{}{}
		}
	}
	add(ec.Organization.Path)
	for _, read := range ec.Organization.Reads {
		add(read.Path)
		add(read.RequestedPath)
	}
	for _, issue := range ec.Organization.Errors {
		var validation *config.V4ValidationError
		if errors.As(issue, &validation) {
			add(validation.File)
		}
		var trust *config.OrganizationTrustError
		if errors.As(issue, &trust) {
			add(trust.Path)
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func configurationAliasesOrganization(path string, organizationPaths []string) (bool, error) {
	canonical, err := dependencytxn.CanonicalConfigDestination(path)
	if err != nil {
		return false, err
	}
	info, infoErr := os.Stat(canonical)
	if infoErr != nil && !errors.Is(infoErr, os.ErrNotExist) {
		return false, infoErr
	}
	for _, organizationPath := range organizationPaths {
		organizationCanonical, err := dependencytxn.CanonicalConfigDestination(organizationPath)
		if err != nil {
			return false, err
		}
		if canonical == organizationCanonical {
			return true, nil
		}
		if infoErr == nil {
			organizationInfo, err := os.Stat(organizationCanonical)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return false, err
			}
			if os.SameFile(info, organizationInfo) {
				return true, nil
			}
		}
	}
	return false, nil
}

func guidedConfigurationError(code string) bool {
	switch code {
	case "v4.parse", "v4.layer-field", "v4.unknown-field", "v4.duplicate-key",
		"v4.alias-forbidden", "v4.auxiliary", "v4.duplicate", "v4.duplicate-request",
		"v4.exclusion", "v4.git-origin", "v4.git-path", "v4.git-ref", "v4.git-ref-required",
		"v4.directory-path", "v4.harness", "v4.include", "v4.installed-name",
		"v4.key-type", "v4.locator", "v4.marketplace-url",
		"v4.merge-key-forbidden", "v4.multiple-documents", "v4.option-combination",
		"v4.registry-slug", "v4.resource-item", "v4.resource-options", "v4.revision",
		"v4.root-type", "v4.runtime-option", "v4.source-conflict", "v4.source-discriminator",
		"v4.source-field", "v4.source-kind", "v4.source-name", "v4.source-type",
		"v4.source-value", "v4.type", "v4.undefined-source":
		return true
	default:
		return false
	}
}

func repairConfigurationSourceFile(validation *config.V4ValidationError) string {
	if validation.File != "" {
		return validation.File
	}
	if filepath.IsAbs(validation.Path) {
		return validation.Path
	}
	return ""
}

func repairDiagnosticLine(validation *config.V4ValidationError) int {
	if validation.Line > 0 {
		return validation.Line
	}
	message := validation.Msg
	if offset := strings.Index(message, "line "); offset >= 0 {
		fields := strings.Fields(message[offset+len("line "):])
		if len(fields) > 0 {
			if line, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":")); err == nil && line > 0 {
				return line
			}
		}
	}
	return 1
}

func applyGuidedConfigurationEdit(
	ctx context.Context,
	opts doctorOptions,
	path string,
	organizationPaths []string,
	revalidate func() error,
	refreshEditedFile func(string) error,
) (bool, error) {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	safePath := tui.TerminalSafeLine(path)
	if editor == "" {
		_, err := fmt.Fprintf(opts.out, "  deferred: no $VISUAL or $EDITOR is set; edit %s manually, then run `agent-marketplace doctor --repair` again\n", safePath)
		return false, err
	}
	accepted, err := confirmPrompt(opts.out, opts.repairReader,
		fmt.Sprintf("Edit %s with %s? [y/N] ", safePath, tui.TerminalSafeLine(editor)))
	if err != nil {
		return false, err
	}
	if !accepted {
		_, err := fmt.Fprintf(opts.out, "  deferred: editor offer declined for %s\n", safePath)
		return false, err
	}
	if err := revalidate(); err != nil {
		return false, err
	}
	aliases, err := configurationAliasesOrganization(path, organizationPaths)
	if err != nil {
		return false, fmt.Errorf("check organization editor boundary: %w", err)
	}
	if aliases {
		return false, fmt.Errorf("%w: configuration editor target aliases organization configuration %s", dependencytxn.ErrGenerationConflict, safePath)
	}
	command := processstart.Command(ctx, "sh", "-c", `exec `+editor+` "$@"`, "doctor-editor", path)
	command.Stdin = opts.in
	command.Stdout = opts.out
	command.Stderr = opts.err
	runErr := command.Run()
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("run configuration editor: %w", err)
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return false, fmt.Errorf("run configuration editor: %w", runErr)
		}
		if _, err := fmt.Fprintf(opts.out, "  editor exited with status %d; diagnosing configuration again\n", exitErr.ExitCode()); err != nil {
			return false, err
		}
	}
	if err := refreshEditedFile(path); err != nil {
		return false, err
	}
	return true, nil
}
