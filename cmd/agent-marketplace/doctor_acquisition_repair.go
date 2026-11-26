package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/lifecycle"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplacelock"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	repairplanner "github.com/Adyen/agent-marketplace/internal/repair"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type doctorAcquisitionDiagnostic struct {
	code  string
	scope lockfile.Scope
	path  string
}

type doctorDiagnosticCoder interface {
	DiagnosticCode() string
}

type doctorDiagnosticLocation interface {
	DiagnosticScope() string
	DiagnosticPath() string
}

func doctorAcquisitionDiagnostics(err error) []doctorAcquisitionDiagnostic {
	var diagnostics []doctorAcquisitionDiagnostic
	var visit func(error, lockfile.Scope, string)
	visit = func(current error, scope lockfile.Scope, path string) {
		if current == nil {
			return
		}
		if location, ok := current.(doctorDiagnosticLocation); ok {
			if location.DiagnosticScope() != "" {
				scope = lockfile.Scope(location.DiagnosticScope())
			}
			if location.DiagnosticPath() != "" {
				path = location.DiagnosticPath()
			}
		}
		if coded, ok := current.(doctorDiagnosticCoder); ok {
			code := coded.DiagnosticCode()
			if strings.HasPrefix(code, "lock.") {
				diagnostics = append(diagnostics, doctorAcquisitionDiagnostic{code: code, scope: scope, path: path})
			}
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range wrapped.Unwrap() {
				visit(child, scope, path)
			}
		case interface{ Unwrap() error }:
			visit(wrapped.Unwrap(), scope, path)
		}
	}
	visit(err, "", "")

	var selected []doctorAcquisitionDiagnostic
	for i, diagnostic := range diagnostics {
		if diagnostic.code == "lock.invalid" {
			for _, nested := range diagnostics[i+1:] {
				if sameDoctorAcquisitionLocation(diagnostic, nested) && isDoctorAcquisitionCode(nested.code) {
					diagnostic = nested
					break
				}
			}
		}
		duplicate := false
		for _, prior := range selected {
			if prior == diagnostic {
				duplicate = true
				break
			}
		}
		if !duplicate {
			selected = append(selected, diagnostic)
		}
	}
	return selected
}

func sameDoctorAcquisitionLocation(left, right doctorAcquisitionDiagnostic) bool {
	return left.scope == right.scope && filepath.Clean(left.path) == filepath.Clean(right.path)
}

func isDoctorAcquisitionCode(code string) bool {
	if isDoctorSafeAcquisitionCode(code) || isDoctorDestructiveAcquisitionCode(code) {
		return true
	}
	switch code {
	case "lock.invalid", "lock.manifest-mismatch", "lock.digest-invalid", "lock.trust":
		return true
	default:
		return false
	}
}

func doctorAcquisitionLayer(ec config.EffectiveConfig, diagnostic doctorAcquisitionDiagnostic) (config.LayerResult, string, bool) {
	var found config.LayerResult
	var path string
	matches := 0
	for _, layer := range ec.PresentLayers() {
		scope := lifecycle.LockScopeForSource(layer.Source)
		if diagnostic.scope != "" && diagnostic.scope != scope {
			continue
		}
		if len(layer.Sources) == 0 {
			continue
		}
		lockPath, err := lifecycle.LayerLockDestination(layer)
		if err != nil {
			continue
		}
		if diagnostic.path != "" && filepath.Clean(diagnostic.path) != filepath.Clean(lockPath) {
			continue
		}
		found, path = layer, lockPath
		matches++
	}
	return found, path, matches == 1
}

func doctorAcquisitionStep(
	repoRoot, localDir string,
	code string,
	scope lockfile.Scope,
	path string,
	reads *[]dependencytxn.ReadAssertion,
	revalidate func() error,
	authorizeLock func(string, dependencytxn.Generation) error,
) repairplanner.Step {
	stage := repairplanner.StageLocks
	action := "reconcile acquisition lock without pin upgrades"
	files := []string{path}
	if code == "lock.offline-snapshot-missing" || code == "lock.snapshot-unavailable" {
		stage = repairplanner.StageCache
		action = fmt.Sprintf("fetch exact snapshot from its locked commit for %s scope", scope)
		files = []string{fmt.Sprintf("exact Git snapshot cache for %s acquisition lock %s", scope, path)}
	}
	step := repairplanner.Step{
		Key:   fmt.Sprintf("acquisition-%s-%s-%s", code, scope, filepath.Base(path)),
		Stage: stage, Class: repairplanner.Safe, Action: action, Code: code,
		Files: files,
	}
	step.Apply = func(ctx context.Context) (bool, error) {
		slog.Info("doctor acquisition repair step started", "code", code, "scope", scope, "lock", path)
		if err := revalidate(); err != nil {
			slog.Error("doctor acquisition repair step failed", "code", code, "scope", scope, "status", "error")
			return true, err
		}
		if stage == repairplanner.StageCache {
			_, _, err := lifecycle.PrepareExactLocks(ctx, repoRoot, localDir, lifecycle.LockMode{
				Locked: true, Selects: map[lockfile.Scope]bool{scope: true}, AllowTransientOrganization: true,
				CommitReads: append([]dependencytxn.ReadAssertion(nil), (*reads)...),
			})
			if err != nil {
				slog.Error("doctor acquisition repair step failed", "code", code, "scope", scope, "status", "error")
				return true, err
			}
			slog.Info("doctor acquisition repair step completed", "code", code, "scope", scope, "status", "success")
			return true, err
		}

		mode := lifecycle.LockMode{
			Selects:     map[lockfile.Scope]bool{scope: true},
			CommitReads: append([]dependencytxn.ReadAssertion(nil), (*reads)...),
		}
		if code == "lock.unsupported-version" {
			edit, err := doctorStageSupportedLockVersion(path, scope)
			if err != nil {
				slog.Error("doctor acquisition repair step failed", "code", code, "scope", scope, "status", "error")
				return true, err
			}
			mode.StagedLockEdits = []pipeline.StagedLockEdit{edit}
		} else {
			edits, err := lifecycle.PrepareMissingMarketplaceLockEdits(ctx, repoRoot, localDir, mode.Selects, nil)
			if err != nil {
				slog.Error("doctor acquisition repair step failed", "code", code, "scope", scope, "status", "error")
				return true, err
			}
			mode.StagedLockEdits = edits
		}
		prepared := map[string]dependencytxn.Generation{}
		mode.UpdatePreview = func(_ context.Context, view lifecycle.PreparedUpdateView) error {
			for desiredPath, data := range view.LockDesired {
				if filepath.Clean(desiredPath) != filepath.Clean(path) {
					return fmt.Errorf("%w: acquisition repair prepared an unexpected lock destination", dependencytxn.ErrGenerationConflict)
				}
				prepared[desiredPath] = dependencytxn.Generation{
					Exists: true, Digest: dependencytxn.Digest(data),
				}
			}
			if len(prepared) == 0 {
				return fmt.Errorf("acquisition repair prepared no lock update for %s", path)
			}
			return nil
		}
		_, _, err := lifecycle.PrepareExactLocks(ctx, repoRoot, localDir, mode)
		if err != nil {
			slog.Error("doctor acquisition repair step failed", "code", code, "scope", scope, "status", "error")
			return true, err
		}
		for preparedPath, generation := range prepared {
			if err := authorizeLock(preparedPath, generation); err != nil {
				slog.Error("doctor acquisition repair step failed", "code", code, "scope", scope, "status", "error")
				return true, err
			}
		}
		slog.Info("doctor acquisition repair step completed", "code", code, "scope", scope, "status", "success")
		return true, nil
	}
	return step
}

func doctorStageSupportedLockVersion(path string, scope lockfile.Scope) (pipeline.StagedLockEdit, error) {
	raw, generation, mode, err := dependencytxn.ReadSnapshot(path)
	if err != nil {
		return pipeline.StagedLockEdit{}, err
	}
	if !generation.Exists {
		return pipeline.StagedLockEdit{}, fmt.Errorf("%w: acquisition lock disappeared at %s", dependencytxn.ErrGenerationConflict, path)
	}
	doc, err := lockfile.DecodeKnownShapeVersion(raw)
	if err == nil {
		err = lockfile.ValidateScope(doc, scope)
	}
	if err != nil {
		return pipeline.StagedLockEdit{}, err
	}
	normalized, err := lockfile.Encode(doc)
	if err != nil {
		return pipeline.StagedLockEdit{}, err
	}
	return pipeline.StagedLockEdit{
		Path: path, Expected: generation, ExpectedMode: mode, Bytes: normalized, Mode: mode,
	}, nil
}

func doctorHasNonLockDiagnostic(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*pipeline.LockViewError); ok {
		return false
	}
	if coded, ok := err.(doctorDiagnosticCoder); ok {
		code := coded.DiagnosticCode()
		return code != "" && !strings.HasPrefix(code, "lock.")
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			if doctorHasNonLockDiagnostic(child) {
				return true
			}
		}
		return false
	case interface{ Unwrap() error }:
		return doctorHasNonLockDiagnostic(wrapped.Unwrap())
	default:
		return len(doctorAcquisitionDiagnostics(err)) == 0
	}
}

func isDoctorSafeAcquisitionCode(code string) bool {
	switch code {
	case "lock.stale", "lock.missing", "lock.version-upgrade-required", "lock.version-deprecated",
		"lock.unsupported-version", "lock.change-required", "lock.offline-snapshot-missing",
		"lock.snapshot-unavailable":
		return true
	default:
		return false
	}
}

func isDoctorDestructiveAcquisitionCode(code string) bool {
	switch code {
	case "lock.conflicted", "lock.alias-forbidden", "lock.merge-forbidden", "lock.duplicate-field":
		return true
	default:
		return false
	}
}

func doctorDeprecatedWritableLockErrors(ec config.EffectiveConfig) []error {
	var diagnostics []error
	for _, layer := range ec.PresentLayers() {
		if !layer.Source.IsWritable() || len(layer.Sources) == 0 {
			continue
		}
		scope := lifecycle.LockScopeForSource(layer.Source)
		path, err := lifecycle.LayerLockDestination(layer)
		if err != nil {
			diagnostics = append(diagnostics, &pipeline.LockViewError{Code: "lock.read", Scope: scope, Err: err})
			continue
		}
		doc, _, exists, err := pipeline.ReadLayerLock(path, scope)
		if err != nil {
			diagnostics = append(diagnostics, err)
			continue
		}
		if exists && doc != nil && doc.APIVersion == lockfile.APIVersionV1 {
			diagnostics = append(diagnostics, &pipeline.LockViewError{
				Code: "lock.version-deprecated", Scope: scope, Path: path,
				Err: errors.New("writable acquisition lock uses the deprecated v1 schema"),
			})
		}
	}
	return diagnostics
}

type doctorGitPinChoice struct {
	url   string
	ref   string
	left  lockfile.GitOID
	right lockfile.GitOID
}

func doctorDestructiveAcquisitionStep(
	opts doctorOptions,
	repoRoot, localDir string,
	layer config.LayerResult,
	diagnostic doctorAcquisitionDiagnostic,
	path string,
	reads []dependencytxn.ReadAssertion,
	revalidate func() error,
	authorizeLock func(string, dependencytxn.Generation) error,
) (repairplanner.Step, error) {
	raw, generation, _, err := dependencytxn.ReadSnapshot(path)
	if err != nil {
		return repairplanner.Step{}, err
	}
	expected, found := doctorAcquisitionReadGeneration(reads, path)
	if !found || !generation.Exists || generation != expected {
		return repairplanner.Step{}, fmt.Errorf("%w: acquisition lock changed before recovery diagnosis", dependencytxn.ErrGenerationConflict)
	}
	candidates, err := lockfile.RepairCandidates(raw, diagnostic.scope)
	if err != nil {
		return repairplanner.Step{}, err
	}
	hasGitConflict := bytes.Contains(raw, []byte("<<<<<<<"))
	choices, err := doctorDivergentGitPins(candidates, hasGitConflict)
	if err != nil {
		return repairplanner.Step{}, err
	}
	step := repairplanner.Step{
		Key:    fmt.Sprintf("acquisition-%s-%s-%s", diagnostic.code, diagnostic.scope, filepath.Base(path)),
		Stage:  repairplanner.StageLocks,
		Class:  repairplanner.Destructive,
		Action: "recover acquisition lock from validated views",
		Code:   diagnostic.code,
		Files:  []string{path},
	}
	if len(choices) > 0 {
		step.Action = "choose exact Git pins and recover acquisition lock"
		if !opts.isTTY || opts.yes {
			step.Class = repairplanner.Guided
			return step, nil
		}
	}
	step.Apply = func(ctx context.Context) (bool, error) {
		if !opts.isTTY || opts.yes {
			_, printErr := fmt.Fprintf(opts.out, "  acquisition lock %s left unchanged: destructive lock recovery requires a terminal without --yes\n", filepath.Base(path))
			return true, errors.Join(repairplanner.ErrIncomplete, printErr)
		}
		if err := revalidate(); err != nil {
			return true, fmt.Errorf("repair policy changed before lock recovery: %w", err)
		}
		selected := make(map[string]lockfile.GitOID, len(choices))
		for _, choice := range choices {
			answer, err := promptDoctorGitPin(opts.out, opts.repairReader, choice)
			if err != nil {
				return true, err
			}
			if answer.IsZero() {
				_, printErr := fmt.Fprintf(opts.out, "  no valid pin choice for %s; acquisition lock left unchanged\n", doctorSafeGitIdentity(choice.url))
				return true, errors.Join(repairplanner.ErrIncomplete, printErr)
			}
			selected[lifecycle.GitLockIdentity(choice.url, choice.ref)] = answer
		}
		seed, err := doctorMergeAcquisitionCandidates(candidates, selected)
		if err != nil {
			return true, err
		}
		intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localDir})
		if len(intent.Errors) > 0 {
			return true, errors.Join(intent.Errors...)
		}
		layerIntent := intent.Layers[layer.Source]
		if err := layerIntent.Err(); err != nil {
			return true, err
		}
		if err := doctorRequireExistingAcquisitionPins(layerIntent, seed); err != nil {
			return true, err
		}
		seedBytes, err := lockfile.Encode(seed)
		if err != nil {
			return true, err
		}
		if err := revalidate(); err != nil {
			return true, fmt.Errorf("repair policy changed before lock replacement confirmation: %w", err)
		}
		accepted, err := confirmPrompt(opts.out, opts.repairReader,
			fmt.Sprintf("  replace acquisition lock %s with the validated recovery? [y/N] ", filepath.Base(path)))
		if err != nil {
			return true, err
		}
		if !accepted {
			_, printErr := fmt.Fprintf(opts.out, "  declined: acquisition lock %s left unchanged\n", filepath.Base(path))
			return false, printErr
		}
		if err := revalidate(); err != nil {
			return true, fmt.Errorf("repair policy changed after lock replacement confirmation: %w", err)
		}
		original, originalGeneration, originalMode, err := dependencytxn.ReadSnapshot(path)
		if err != nil {
			return true, err
		}
		if !originalGeneration.Exists || originalGeneration != expected || !bytes.Equal(original, raw) {
			return true, fmt.Errorf("%w: acquisition lock changed before backup", dependencytxn.ErrGenerationConflict)
		}
		root, destination, parent, err := doctorCaptureAcquisitionDestination(repoRoot, path, diagnostic.scope)
		if err != nil {
			return true, err
		}
		defer func() {
			_ = parent.Close()
			_ = destination.Close()
			_ = root.Close()
		}()
		if err := destination.Verify(); err != nil {
			return true, err
		}
		if _, err := doctorCreateAcquisitionBackup(parent, path, original); err != nil {
			return true, err
		}
		if err := revalidate(); err != nil {
			return true, fmt.Errorf("repair policy changed after lock backup: %w", err)
		}
		if err := destination.Verify(); err != nil {
			return true, err
		}

		preparedGeneration := dependencytxn.Generation{}
		mode := lifecycle.LockMode{
			Selects:     map[lockfile.Scope]bool{diagnostic.scope: true},
			CommitReads: append([]dependencytxn.ReadAssertion(nil), reads...),
			StagedLockEdits: []pipeline.StagedLockEdit{{
				Path: path, Expected: originalGeneration, ExpectedMode: originalMode,
				Bytes: seedBytes, Mode: originalMode,
			}},
			UpdatePreview: func(previewCtx context.Context, view lifecycle.PreparedUpdateView) error {
				if err := revalidate(); err != nil {
					return err
				}
				if err := destination.Verify(); err != nil {
					return err
				}
				current, currentGeneration, _, err := dependencytxn.ReadSnapshot(path)
				if err != nil {
					return err
				}
				if currentGeneration != originalGeneration || !bytes.Equal(current, original) {
					return fmt.Errorf("%w: acquisition lock changed before publication", dependencytxn.ErrGenerationConflict)
				}
				prepared, ok := view.LockDesired[path]
				if !ok {
					return fmt.Errorf("lock recovery prepared no update for %s", filepath.Base(path))
				}
				document, err := lockfile.Decode(prepared)
				if err == nil {
					err = lockfile.ValidateScope(document, diagnostic.scope)
				}
				if err == nil {
					err = doctorValidatePreparedAcquisitionPins(seed, document)
				}
				if err != nil {
					return err
				}
				if len(choices) > 0 {
					if err := doctorGuardOneSidedAcquisitionPins(previewCtx, view, layer, candidates, document); err != nil {
						return err
					}
				}
				preparedGeneration = dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(prepared)}
				return nil
			},
		}
		_, _, err = lifecycle.PrepareExactLocks(ctx, repoRoot, localDir, mode)
		if err != nil {
			return true, err
		}
		if !preparedGeneration.Exists {
			return true, errors.New("lock recovery published without a prepared generation")
		}
		_, publishedGeneration, _, err := dependencytxn.ReadSnapshot(path)
		if err != nil {
			return true, err
		}
		if publishedGeneration != preparedGeneration {
			return true, fmt.Errorf("%w: published acquisition lock differs from its prepared generation", dependencytxn.ErrGenerationConflict)
		}
		if err := authorizeLock(path, preparedGeneration); err != nil {
			return true, err
		}
		_, err = fmt.Fprintf(opts.out, "  recovered acquisition lock %s; backup retained\n", filepath.Base(path))
		return true, err
	}
	return step, nil
}

func doctorAcquisitionReadGeneration(reads []dependencytxn.ReadAssertion, path string) (dependencytxn.Generation, bool) {
	for _, read := range reads {
		if filepath.Clean(read.Path) == filepath.Clean(path) {
			return read.Expected, true
		}
	}
	return dependencytxn.Generation{}, false
}

func doctorDivergentGitPins(candidates []lockfile.Document, hasGitConflict bool) ([]doctorGitPinChoice, error) {
	if len(candidates) < 2 {
		return nil, nil
	}
	left := make(map[string]lockfile.GitRegistry, len(candidates[0].Registries))
	for _, registry := range candidates[0].Registries {
		left[lifecycle.GitLockIdentity(registry.URL, registry.Ref)] = registry
	}
	var choices []doctorGitPinChoice
	for _, registry := range candidates[1].Registries {
		prior, exists := left[lifecycle.GitLockIdentity(registry.URL, registry.Ref)]
		if !exists || prior.Commit == registry.Commit {
			continue
		}
		if !hasGitConflict || len(candidates) != 2 {
			return nil, fmt.Errorf("divergent Git pins do not have two unambiguous conflict sides")
		}
		choices = append(choices, doctorGitPinChoice{
			url: registry.URL, ref: registry.Ref, left: prior.Commit, right: registry.Commit,
		})
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].url != choices[j].url {
			return choices[i].url < choices[j].url
		}
		return choices[i].ref < choices[j].ref
	})
	return choices, nil
}

func promptDoctorGitPin(out io.Writer, reader *bufio.Reader, choice doctorGitPinChoice) (lockfile.GitOID, error) {
	if reader == nil {
		return lockfile.GitOID{}, nil
	}
	prompt := fmt.Sprintf("  divergent pin for %s@%s: left %s, right %s; keep [left/right] ",
		doctorSafeGitIdentity(choice.url), session.TerminalSafeLine(choice.ref),
		choice.left.String(), choice.right.String())
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return lockfile.GitOID{}, err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			_, printErr := fmt.Fprintln(out)
			return lockfile.GitOID{}, printErr
		}
		return lockfile.GitOID{}, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "left":
		return choice.left, nil
	case "right":
		return choice.right, nil
	default:
		_, printErr := fmt.Fprintln(out, "  unknown pin choice")
		return lockfile.GitOID{}, printErr
	}
}

func doctorSafeGitIdentity(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "Git registry"
	}
	parsed.User = nil
	return session.TerminalSafeLine(parsed.Redacted())
}

func doctorCaptureAcquisitionDestination(repoRoot, path string, scope lockfile.Scope) (*fsboundary.Dir, *fsboundary.Destination, *fsboundary.Dir, error) {
	rootPath := repoRoot
	if scope == lockfile.ScopeUser {
		home := os.Getenv("HOME")
		if home == "" {
			return nil, nil, nil, errors.New("cannot locate user acquisition-lock root")
		}
		rootPath = home
	} else if scope != lockfile.ScopeRepo && scope != lockfile.ScopeLocal {
		return nil, nil, nil, fmt.Errorf("acquisition lock scope %q is not writable", scope)
	}
	rootPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, nil, nil, err
	}
	relative, err := filepath.Rel(rootPath, filepath.Clean(path))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, nil, fmt.Errorf("acquisition lock destination is outside its scope")
	}
	root, err := fsboundary.OpenDir(rootPath)
	if err != nil {
		return nil, nil, nil, err
	}
	destination, err := fsboundary.CaptureDestination(root, relative, fsboundary.LinkPolicyRepository)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, err
	}
	if destination.Link() != nil {
		_ = destination.Close()
		_ = root.Close()
		return nil, nil, nil, fmt.Errorf("%w: acquisition lock aliases are unsupported", fsboundary.ErrUnsafePath)
	}
	parent, err := root.OpenContainedRoot(filepath.Dir(relative))
	if err != nil {
		_ = destination.Close()
		_ = root.Close()
		return nil, nil, nil, err
	}
	return root, destination, parent, nil
}

func doctorCreateAcquisitionBackup(parent *fsboundary.Dir, lockPath string, original []byte) (string, error) {
	name := filepath.Base(lockPath) + ".repair-*.bak"
	file, backupName, err := parent.CreateTemp(name)
	if err != nil {
		return "", err
	}
	n, writeErr := file.Write(original)
	if writeErr == nil && n != len(original) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	destination, captureErr := captureDoctorAcquisitionBackup(parent, backupName, file)
	closeErr := file.Close()
	cleanup := func(cause error) error {
		if destination == nil {
			return errors.Join(cause, fmt.Errorf("acquisition-lock backup cleanup skipped at %s because identity could not be established: %w", filepath.Join(parent.Path(), backupName), captureErr))
		}
		defer func() { _ = destination.Close() }()
		if err := destination.RemoveLeaf(); err != nil {
			return errors.Join(cause, fmt.Errorf("incomplete acquisition-lock backup retained at %s: %w", filepath.Join(parent.Path(), backupName), err))
		}
		if err := parent.Sync(); err != nil {
			return errors.Join(cause, fmt.Errorf("incomplete acquisition-lock backup cleanup durability is uncertain: %w", err))
		}
		return cause
	}
	if writeErr != nil || captureErr != nil || closeErr != nil {
		return "", cleanup(errors.Join(writeErr, captureErr, closeErr))
	}
	if err := parent.Sync(); err != nil {
		return "", cleanup(err)
	}
	_ = destination.Close()
	return filepath.Join(parent.Path(), backupName), nil
}

func captureDoctorAcquisitionBackup(parent *fsboundary.Dir, name string, file *os.File) (*fsboundary.Destination, error) {
	fileIdentity, err := fsboundary.FileIdentity(file)
	if err != nil {
		return nil, err
	}
	destination, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	if !destination.Identity().SameState(fileIdentity) {
		return nil, errors.Join(
			fmt.Errorf("%w: acquisition-lock backup destination no longer names its open file", fsboundary.ErrGenerationConflict),
			destination.Close(),
		)
	}
	return destination, nil
}

func doctorRequireExistingAcquisitionPins(layer config.LayerIntent, seed lockfile.Document) error {
	if err := layer.ValidateDeployedMarketplaceLock(&seed); err != nil {
		return fmt.Errorf("recovery seed does not retain the declared Marketplace identity: %w", err)
	}
	if lifecycle.SelectedMarketplaceResourcesMissing(layer, &seed) {
		return errors.New("recovery seed is missing a selected Marketplace pin; refusing to acquire a replacement")
	}
	if missing := lifecycle.UnrecordedRegistrations(layer, &seed); len(missing) > 0 {
		return errors.New("recovery seed is missing a Marketplace identity binding; refusing to rebind the endpoint")
	}
	registries := make(map[string]bool, len(seed.Registries))
	for _, registry := range seed.Registries {
		registries[lifecycle.GitLockIdentity(registry.URL, registry.Ref)] = true
	}
	for _, source := range layer.Sources() {
		if source.Kind != config.V4SourceGit || !source.All && !source.Required && len(source.Decisions) == 0 {
			continue
		}
		canonical, err := lockfile.CanonicalGitURL(source.URL)
		if err != nil {
			return err
		}
		if !registries[lifecycle.GitLockIdentity(canonical, source.Ref)] {
			return fmt.Errorf("recovery seed has no exact Git commit for selected source %s@%s", doctorSafeGitIdentity(canonical), session.TerminalSafeLine(source.Ref))
		}
	}
	return nil
}

func doctorMergeAcquisitionCandidates(candidates []lockfile.Document, choices map[string]lockfile.GitOID) (lockfile.Document, error) {
	if len(candidates) == 0 {
		return lockfile.Document{}, errors.New("lock recovery has no validated candidates")
	}
	merged := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: candidates[0].Scope}
	registryIndexes := make(map[string]int)
	marketplaceIndexes := make(map[string]int)
	for _, candidate := range candidates {
		owned, err := lockfile.Normalize(candidate)
		if err != nil {
			return lockfile.Document{}, err
		}
		if owned.Scope != merged.Scope {
			return lockfile.Document{}, errors.New("lock recovery candidates have different scopes")
		}
		for _, incoming := range owned.Registries {
			key := lifecycle.GitLockIdentity(incoming.URL, incoming.Ref)
			if chosen, exists := choices[key]; exists && incoming.Commit != chosen {
				continue
			}
			if index, exists := registryIndexes[key]; exists {
				current := &merged.Registries[index]
				if current.Commit != incoming.Commit {
					return lockfile.Document{}, fmt.Errorf("git registry %s@%s has divergent commits without an explicit choice", doctorSafeGitIdentity(incoming.URL), session.TerminalSafeLine(incoming.Ref))
				}
				if err := doctorMergeGitRegistry(current, incoming); err != nil {
					return lockfile.Document{}, err
				}
				continue
			}
			registryIndexes[key] = len(merged.Registries)
			merged.Registries = append(merged.Registries, incoming)
		}
		for _, incoming := range owned.Marketplaces {
			if index, exists := marketplaceIndexes[incoming.URL]; exists {
				if err := doctorMergeMarketplace(&merged.Marketplaces[index], incoming); err != nil {
					return lockfile.Document{}, err
				}
				continue
			}
			marketplaceIndexes[incoming.URL] = len(merged.Marketplaces)
			merged.Marketplaces = append(merged.Marketplaces, incoming)
		}
	}
	return lockfile.Normalize(merged)
}

func doctorMergeGitRegistry(current *lockfile.GitRegistry, incoming lockfile.GitRegistry) error {
	rootIndexes := make(map[string]int, len(current.Roots))
	for i, root := range current.Roots {
		rootIndexes[root.Path+"\x00"+root.SourceHarness] = i
	}
	for _, incomingRoot := range incoming.Roots {
		key := incomingRoot.Path + "\x00" + incomingRoot.SourceHarness
		index, exists := rootIndexes[key]
		if !exists {
			rootIndexes[key] = len(current.Roots)
			current.Roots = append(current.Roots, incomingRoot)
			continue
		}
		root := &current.Roots[index]
		if root.RevisionSchema != incomingRoot.RevisionSchema || root.RevisionDigest != incomingRoot.RevisionDigest {
			return fmt.Errorf("git root %q has incompatible pins at one exact commit", incomingRoot.Path)
		}
		resourceIndexes := make(map[string]int, len(root.Resources))
		for i, resource := range root.Resources {
			resourceIndexes[resource.Kind+"\x00"+resource.Name] = i
		}
		for _, incomingResource := range incomingRoot.Resources {
			resourceKey := incomingResource.Kind + "\x00" + incomingResource.Name
			if index, exists := resourceIndexes[resourceKey]; exists {
				if !reflect.DeepEqual(root.Resources[index], incomingResource) {
					return fmt.Errorf("git resource %s/%s has incompatible exact pins", incomingResource.Kind, incomingResource.Name)
				}
				continue
			}
			resourceIndexes[resourceKey] = len(root.Resources)
			root.Resources = append(root.Resources, incomingResource)
		}
	}
	return nil
}

func doctorMergeMarketplace(current *lockfile.MarketplaceLock, incoming lockfile.MarketplaceLock) error {
	if current.InstanceID != incoming.InstanceID {
		return fmt.Errorf("marketplace endpoint %s has differing instance identities", session.TerminalSafeLine(current.URL))
	}
	bindings := make(map[string]int, len(current.RegistryBindings))
	for i, binding := range current.RegistryBindings {
		bindings[binding.Name] = i
	}
	for _, incomingBinding := range incoming.RegistryBindings {
		if index, exists := bindings[incomingBinding.Name]; exists {
			if current.RegistryBindings[index].RegistryID != incomingBinding.RegistryID {
				return fmt.Errorf("marketplace registry %q has incompatible identity bindings", incomingBinding.Name)
			}
			continue
		}
		bindings[incomingBinding.Name] = len(current.RegistryBindings)
		current.RegistryBindings = append(current.RegistryBindings, incomingBinding)
	}
	resources := make(map[string]int, len(current.Resources))
	for i, resource := range current.Resources {
		resources[doctorMarketplaceResourceKey(resource)] = i
	}
	for _, incomingResource := range incoming.Resources {
		key := doctorMarketplaceResourceKey(incomingResource)
		if index, exists := resources[key]; exists {
			prior := &current.Resources[index]
			if !doctorMarketplaceResourcePinEqual(*prior, incomingResource) {
				return fmt.Errorf("marketplace resource %s has incompatible exact pins", key)
			}
			prior.SelectedBy = marketplacelock.MergeSelectedBy(prior.SelectedBy, incomingResource.SelectedBy)
			continue
		}
		resources[key] = len(current.Resources)
		current.Resources = append(current.Resources, incomingResource)
	}
	return nil
}

func doctorMarketplaceResourceKey(resource lockfile.MarketplaceResourceLock) string {
	return resource.RegistryID + "\x00" + resource.Kind + "\x00" + resource.Name
}

func doctorMarketplaceResourcePinEqual(left, right lockfile.MarketplaceResourceLock) bool {
	left.SelectedBy = nil
	right.SelectedBy = nil
	return reflect.DeepEqual(left, right)
}

type doctorGitResourceIdentity struct {
	url, ref, path, harness, kind, name string
}

type doctorOneSidedGitPin struct {
	identity  doctorGitResourceIdentity
	candidate int
	registry  lockfile.GitRegistry
	root      lockfile.GitRoot
	resource  lockfile.GitResource
}

func doctorGuardOneSidedAcquisitionPins(ctx context.Context, view lifecycle.PreparedUpdateView, layer config.LayerResult, candidates []lockfile.Document, prepared lockfile.Document) error {
	occurrences := make(map[doctorGitResourceIdentity]map[int]doctorOneSidedGitPin)
	for candidateIndex, candidate := range candidates {
		for _, registry := range candidate.Registries {
			for _, root := range registry.Roots {
				for _, resource := range root.Resources {
					identity := doctorGitResourceIdentity{
						url: registry.URL, ref: registry.Ref, path: config.NormalizeRegistryPath(root.Path),
						harness: root.SourceHarness, kind: resource.Kind, name: resource.Name,
					}
					if occurrences[identity] == nil {
						occurrences[identity] = make(map[int]doctorOneSidedGitPin)
					}
					occurrences[identity][candidateIndex] = doctorOneSidedGitPin{
						identity: identity, candidate: candidateIndex, registry: registry, root: root, resource: resource,
					}
				}
			}
		}
	}
	preparedRegistries := make(map[string]lockfile.GitRegistry, len(prepared.Registries))
	for _, registry := range prepared.Registries {
		preparedRegistries[lifecycle.GitLockIdentity(registry.URL, registry.Ref)] = registry
	}
	var oneSided []doctorOneSidedGitPin
	candidatesToVerify := make(map[int]bool)
	for _, byCandidate := range occurrences {
		if len(byCandidate) != 1 {
			continue
		}
		for candidateIndex, pin := range byCandidate {
			oneSided = append(oneSided, pin)
			preparedRegistry, exists := preparedRegistries[lifecycle.GitLockIdentity(pin.identity.url, pin.identity.ref)]
			if !exists || preparedRegistry.Commit != pin.registry.Commit {
				candidatesToVerify[candidateIndex] = true
			}
		}
	}
	preparedExact := view.Lookup(view.DesiredSet, nil)
	actual := lifecycle.ResolvePreparedSelection(ctx, view, preparedExact, view.DesiredSet.Catalogs(), nil, false)
	if len(oneSided) == 0 {
		return doctorPreparedSelectionRefusal(actual, nil, false)
	}
	sort.Slice(oneSided, func(i, j int) bool {
		left, right := oneSided[i].identity, oneSided[j].identity
		if left.url != right.url {
			return left.url < right.url
		}
		if left.ref != right.ref {
			return left.ref < right.ref
		}
		if left.path != right.path {
			return left.path < right.path
		}
		if left.harness != right.harness {
			return left.harness < right.harness
		}
		if left.kind != right.kind {
			return left.kind < right.kind
		}
		if left.name != right.name {
			return left.name < right.name
		}
		return oneSided[i].candidate < oneSided[j].candidate
	})

	sideResults := make(map[int]pipeline.ResolveResult, len(candidatesToVerify))
	for candidateIndex := range candidates {
		if !candidatesToVerify[candidateIndex] {
			continue
		}
		candidate := candidates[candidateIndex]
		sideExact, sideCatalogs, err := view.VerifiedCandidateLayer(ctx, layer, candidate)
		if err != nil {
			return fmt.Errorf("verify original acquisition-lock view: %w", err)
		}
		routedExact := func(entry config.RegistryEntry) (string, string, error) {
			if entry.Source == layer.Source {
				recorded, err := doctorCandidateHasEntry(candidate, entry)
				if err != nil {
					return "", "", err
				}
				if recorded {
					return sideExact(entry)
				}
			}
			return preparedExact(entry)
		}
		catalogs := doctorUnionVerifiedCatalogs(view.DesiredSet.Catalogs(), sideCatalogs)
		comparison := lifecycle.ResolvePreparedSelection(ctx, view, routedExact, catalogs, nil, false)
		candidatePins := make([]doctorOneSidedGitPin, 0, len(oneSided))
		for _, pin := range oneSided {
			if pin.candidate == candidateIndex {
				candidatePins = append(candidatePins, pin)
			}
		}
		if err := doctorPreparedSelectionRefusal(comparison, candidatePins, true); err != nil {
			return err
		}
		sideResults[candidateIndex] = comparison
	}
	if err := doctorPreparedSelectionRefusal(actual, oneSided, false); err != nil {
		return err
	}
	for _, pin := range oneSided {
		result := actual
		if sideResult, exists := sideResults[pin.candidate]; exists {
			result = sideResult
		}
		if doctorSelectionContainsPin(result, layer.Source, pin.identity) && !doctorPreparedRetainsOneSidedPin(prepared, pin) {
			return doctorOneSidedPinRefusal(pin)
		}
	}
	return nil
}

func doctorCandidateHasEntry(candidate lockfile.Document, entry config.RegistryEntry) (bool, error) {
	canonical, err := lifecycle.CanonicalRegistryURL(entry.URL)
	if err != nil {
		return false, err
	}
	path := config.NormalizeRegistryPath(entry.Path)
	harness := strings.TrimSpace(entry.SourceHarness)
	for _, registry := range candidate.Registries {
		if registry.URL != canonical || registry.Ref != entry.Ref {
			continue
		}
		for _, root := range registry.Roots {
			if config.NormalizeRegistryPath(root.Path) == path && (harness == "" || root.SourceHarness == harness) {
				return true, nil
			}
		}
	}
	return false, nil
}

func doctorUnionVerifiedCatalogs(prepared, side map[string]map[string]*catalog.Catalog) map[string]map[string]*catalog.Catalog {
	union := make(map[string]map[string]*catalog.Catalog, len(prepared)+len(side))
	for path, catalogs := range prepared {
		union[path] = make(map[string]*catalog.Catalog, len(catalogs))
		for harness, loaded := range catalogs {
			union[path][harness] = loaded
		}
	}
	for path, catalogs := range side {
		if union[path] == nil {
			union[path] = make(map[string]*catalog.Catalog, len(catalogs))
		}
		for harness, loaded := range catalogs {
			union[path][harness] = loaded
		}
	}
	return union
}

func doctorPreparedSelectionRefusal(result pipeline.ResolveResult, pins []doctorOneSidedGitPin, comparison bool) error {
	if len(result.ConfigErrors) > 0 {
		return errors.Join(result.ConfigErrors...)
	}
	for _, err := range result.CatalogErrors {
		if !catalog.IsOverlapWarning(err) {
			return err
		}
	}
	check := func(err error) error {
		if miss, ok := errors.AsType[*resolver.SelectedNotFoundError](err); ok {
			for _, pin := range pins {
				if doctorSelectedNotFoundMatchesPin(miss, pin.identity) {
					return doctorOneSidedPinRefusal(pin)
				}
			}
			if comparison {
				return nil
			}
		}
		return err
	}
	for _, err := range result.ResolveErrors {
		if checked := check(err); checked != nil {
			if resolver.IsWarning(err) {
				continue
			}
			return checked
		}
	}
	for _, err := range result.Errors {
		if checked := check(err); checked != nil {
			return checked
		}
	}
	return nil
}

func doctorSelectedNotFoundMatchesPin(miss *resolver.SelectedNotFoundError, identity doctorGitResourceIdentity) bool {
	canonical, err := lifecycle.CanonicalRegistryURL(miss.URL)
	if err != nil || canonical != identity.url || miss.Ref != identity.ref || miss.Kind.String() != identity.kind || miss.Name != identity.name {
		return false
	}
	if miss.Path != "" {
		return config.NormalizeRegistryPath(miss.Path) == identity.path
	}
	if miss.RequestedPath != "" && config.NormalizeRegistryPath(miss.RequestedPath) == identity.path {
		return true
	}
	for _, candidatePath := range miss.CandidatePaths {
		if config.NormalizeRegistryPath(candidatePath) == identity.path {
			return true
		}
	}
	return false
}

func doctorSelectionContainsPin(result pipeline.ResolveResult, source config.Source, identity doctorGitResourceIdentity) bool {
	selected := append(append([]resolver.ResolvedArtifact(nil), result.Resolved...), result.SelectedCollections...)
	for _, artifact := range selected {
		if artifact.Source != source || artifact.Ref != identity.ref || artifact.Kind.String() != identity.kind ||
			artifact.Name != identity.name || config.NormalizeRegistryPath(artifact.RegistryPath) != identity.path ||
			artifact.SourceHarness != identity.harness {
			continue
		}
		canonical, err := lifecycle.CanonicalRegistryURL(artifact.RegistryURL)
		if err == nil && canonical == identity.url {
			return true
		}
	}
	return false
}

func doctorPreparedRetainsOneSidedPin(prepared lockfile.Document, pin doctorOneSidedGitPin) bool {
	for _, registry := range prepared.Registries {
		if registry.URL != pin.identity.url || registry.Ref != pin.identity.ref {
			continue
		}
		if registry.Commit != pin.registry.Commit {
			return false
		}
		for _, root := range registry.Roots {
			if config.NormalizeRegistryPath(root.Path) != pin.identity.path || root.SourceHarness != pin.identity.harness {
				continue
			}
			if root.RevisionSchema != pin.root.RevisionSchema || root.RevisionDigest != pin.root.RevisionDigest {
				return false
			}
			for _, resource := range root.Resources {
				if resource.Kind == pin.identity.kind && resource.Name == pin.identity.name {
					return reflect.DeepEqual(resource, pin.resource)
				}
			}
			return false
		}
		return false
	}
	return false
}

func doctorOneSidedPinRefusal(pin doctorOneSidedGitPin) error {
	return fmt.Errorf("chosen commit cannot retain still-selected one-sided Git pin %s/%s from %s@%s; refusing acquisition-lock replacement",
		pin.identity.kind, pin.identity.name, doctorSafeGitIdentity(pin.identity.url), session.TerminalSafeLine(pin.identity.ref))
}

func doctorValidatePreparedAcquisitionPins(seed, prepared lockfile.Document) error {
	seedRegistries := make(map[string]lockfile.GitRegistry, len(seed.Registries))
	for _, registry := range seed.Registries {
		seedRegistries[lifecycle.GitLockIdentity(registry.URL, registry.Ref)] = registry
	}
	for _, registry := range prepared.Registries {
		pinned, exists := seedRegistries[lifecycle.GitLockIdentity(registry.URL, registry.Ref)]
		if !exists || pinned.Commit != registry.Commit {
			return fmt.Errorf("prepared Git lock has no matching recovered commit for %s@%s; refusing latest acquisition", doctorSafeGitIdentity(registry.URL), session.TerminalSafeLine(registry.Ref))
		}
		for _, root := range registry.Roots {
			for _, prior := range pinned.Roots {
				if prior.Path != root.Path || prior.SourceHarness != root.SourceHarness {
					continue
				}
				if prior.RevisionSchema != root.RevisionSchema || prior.RevisionDigest != root.RevisionDigest {
					return fmt.Errorf("prepared Git root %q no longer matches its recovered manifest", root.Path)
				}
				for _, resource := range root.Resources {
					for _, old := range prior.Resources {
						if old.Kind == resource.Kind && old.Name == resource.Name && !reflect.DeepEqual(old, resource) {
							return fmt.Errorf("prepared Git resource %s/%s no longer matches its recovered pin", resource.Kind, resource.Name)
						}
					}
				}
			}
		}
	}
	seedMarketplaces := make(map[string]lockfile.MarketplaceLock, len(seed.Marketplaces))
	for _, marketplace := range seed.Marketplaces {
		seedMarketplaces[marketplace.URL] = marketplace
	}
	for _, marketplace := range prepared.Marketplaces {
		prior, exists := seedMarketplaces[marketplace.URL]
		if !exists || prior.InstanceID != marketplace.InstanceID {
			return fmt.Errorf("prepared Marketplace lock changes the recovered endpoint identity for %s", session.TerminalSafeLine(marketplace.URL))
		}
		bindings := make(map[string]string, len(prior.RegistryBindings))
		for _, binding := range prior.RegistryBindings {
			bindings[binding.Name] = binding.RegistryID
		}
		for _, binding := range marketplace.RegistryBindings {
			if bindings[binding.Name] != binding.RegistryID {
				return fmt.Errorf("prepared Marketplace lock changes registry identity %q", binding.Name)
			}
		}
		resources := make(map[string]lockfile.MarketplaceResourceLock, len(prior.Resources))
		for _, resource := range prior.Resources {
			resources[doctorMarketplaceResourceKey(resource)] = resource
		}
		for _, resource := range marketplace.Resources {
			old, exists := resources[doctorMarketplaceResourceKey(resource)]
			if !exists || !doctorMarketplaceResourcePinEqual(old, resource) {
				return fmt.Errorf("prepared Marketplace lock changes a recovered resource pin")
			}
		}
	}
	return nil
}
