package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// conversionPublisher contains the target-independent transaction used by
// one-shot conversions. Targets provide only their ownership policy, syntax
// validator, and shared-file preflight; publication itself never adopts a
// destination into durable ownership state.
type conversionPublisher struct {
	target          string
	rootGuard       func() error
	shared          []string
	clone           func(*hcc.Result) (*hcc.Result, error)
	preflight       func(string) error
	validate        func(string, []string) error
	pruneCandidates func(string, []hcc.StaleFileCandidate) error
}

func (p conversionPublisher) materialize(result *hcc.Result, outDir string, matOpts []hcc.WriteOption, dryRun bool) (*hcc.WriteReport, error) {
	converted, err := p.clone(result)
	if err != nil {
		return nil, err
	}
	if dryRun {
		if err := p.preflight(outDir); err != nil {
			return nil, err
		}
		report, err := hcc.Materialize(converted, outDir, matOpts...)
		protectConversionSharedCandidates(report, p.shared)
		return report, err
	}
	store, err := ownership.Open(outDir)
	if err != nil {
		return nil, fmt.Errorf("open %s conversion destination lock: %w", p.target, err)
	}
	release, err := store.Lock()
	if err != nil {
		return nil, fmt.Errorf("lock %s conversion destination: %w", p.target, err)
	}
	defer release()
	slog.Debug("conversion publication started", "target", p.target, "out_dir", filepath.Clean(outDir), "root_key", store.RootKey())
	if err := p.preflight(outDir); err != nil {
		return nil, err
	}
	planOpts := append([]hcc.WriteOption(nil), matOpts...)
	planOpts = append(planOpts, hcc.WithDryRun())
	report, resultErr := hcc.Materialize(converted, outDir, planOpts...)
	if resultErr != nil && !errors.Is(resultErr, hcc.ErrPartialResult) {
		return nil, resultErr
	}
	protectConversionSharedCandidates(report, p.shared)
	if p.rootGuard != nil {
		if err := p.rootGuard(); err != nil {
			return nil, err
		}
	}
	stageParent, err := codexConversionStageParent(outDir)
	if err != nil {
		return nil, err
	}
	stageDir, err := os.MkdirTemp(stageParent, ".agent-marketplace-convert-")
	if err != nil {
		return nil, fmt.Errorf("create %s conversion staging directory: %w", p.target, err)
	}
	defer func() {
		if removeErr := os.RemoveAll(stageDir); removeErr != nil {
			slog.Warn("conversion staging cleanup failed", "target", p.target, "stage_dir", stageDir, "error", removeErr)
		}
	}()
	if _, stageErr := hcc.Materialize(converted, stageDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); stageErr != nil && !errors.Is(stageErr, hcc.ErrPartialResult) {
		return nil, fmt.Errorf("stage %s conversion: %w", p.target, stageErr)
	}
	if err := p.validate(stageDir, report.Written); err != nil {
		return nil, err
	}
	if p.rootGuard != nil {
		if err := p.rootGuard(); err != nil {
			return nil, err
		}
	}
	if err := publishConversionWith(stageDir, outDir, report.Written, p.shared, p.target, nil); err != nil {
		return nil, err
	}
	return finishConversionWithCandidates(outDir, report, resultErr, p.pruneCandidates)
}

func protectConversionSharedCandidates(report *hcc.WriteReport, shared []string) {
	if report == nil || len(shared) == 0 {
		return
	}
	protected := make(map[string]struct{}, len(shared))
	for _, path := range shared {
		protected[path] = struct{}{}
	}
	filteredCandidates := report.StaleCandidates[:0]
	filteredRemoved := report.Removed[:0]
	for _, candidate := range report.StaleCandidates {
		if _, ok := protected[candidate.Path]; ok {
			continue
		}
		filteredCandidates = append(filteredCandidates, candidate)
		filteredRemoved = append(filteredRemoved, candidate.Path)
	}
	report.StaleCandidates = filteredCandidates
	report.Removed = filteredRemoved
}

func finishConversion(outDir string, report *hcc.WriteReport, resultErr error, prune func(string, []string) error) (*hcc.WriteReport, error) {
	if pruneErr := prune(outDir, report.Removed); pruneErr != nil {
		return report, fmt.Errorf("conversion output was published, but stale output pruning failed: %w", pruneErr)
	}
	slog.Info("conversion published", "out_dir", filepath.Clean(outDir), "written", len(report.Written), "removed", len(report.Removed), "partial", report.Partial)
	return report, resultErr
}

func finishConversionWithCandidates(outDir string, report *hcc.WriteReport, resultErr error, pruneCandidates func(string, []hcc.StaleFileCandidate) error) (*hcc.WriteReport, error) {
	if pruneCandidates != nil {
		if pruneErr := pruneCandidates(outDir, report.StaleCandidates); pruneErr != nil {
			return report, fmt.Errorf("conversion output was published, but stale output pruning failed: %w", pruneErr)
		}
	}
	slog.Info("conversion published", "out_dir", filepath.Clean(outDir), "written", len(report.Written), "removed", len(report.Removed), "partial", report.Partial)
	return report, resultErr
}

// publishConversionWith is the guarded publication phase shared by Devin and
// Codex. Existing ordinary files are moved into a unique recovery directory;
// no fixed <destination>.backup name is ever used, so a user's backup cannot
// be overwritten. Every rename is verified by inode and digest before a new
// hard link is exposed, and rollback restores only paths still owned by this
// transaction.
func publishConversionWith(stageDir, outDir string, written, shared []string, target string, publishFile func(codexConversionPublication) error) error {
	publications, err := planConversionPublication(stageDir, outDir, written, shared, target)
	if err != nil {
		return err
	}
	defer removeEmptyCodexConversionRecoveryDirs(publications)
	if publishFile == nil {
		publishFile = publishCodexConversionFile
	}
	createdDirs := make([]string, 0, 4)
	published := make([]codexConversionPublication, 0, len(publications))
	for _, publication := range publications {
		if err := ensureCodexConversionDirs(outDir, filepath.Dir(publication.dst), &createdDirs); err != nil {
			return rollbackCodexConversion(fmt.Errorf("prepare destination for %q: %w", publication.rel, err), published, createdDirs)
		}
		if err := publishFile(publication); err != nil {
			if publication.shared && os.IsExist(err) {
				err = fmt.Errorf("%s shared file %q already exists; in-place conversion is unsupported; choose an empty destination", target, publication.rel)
			} else {
				err = fmt.Errorf("publish %s artifact %q: %w", target, publication.rel, err)
			}
			return rollbackCodexConversion(err, published, createdDirs)
		}
		published = append(published, publication)
	}
	return discardCodexConversionRecovery(publications)
}

func planConversionPublication(stageDir, outDir string, written, shared []string, target string) ([]codexConversionPublication, error) {
	sharedSet := make(map[string]struct{}, len(shared))
	for _, path := range shared {
		sharedSet[path] = struct{}{}
	}
	ordered := append([]string(nil), written...)
	sort.Strings(ordered)
	publications := make([]codexConversionPublication, 0, len(ordered))
	for _, rel := range ordered {
		publication := codexConversionPublication{rel: rel, src: filepath.Join(stageDir, filepath.FromSlash(rel)), dst: filepath.Join(outDir, filepath.FromSlash(rel))}
		_, publication.shared = sharedSet[rel]
		info, digest, err := snapshotCodexConversionFile(publication.src)
		if err != nil {
			return nil, fmt.Errorf("inspect staged %s artifact %q: %w", target, rel, err)
		}
		publication.sourceInfo, publication.sourceDigest = info, digest
		info, err = os.Lstat(publication.dst)
		switch {
		case err == nil && publication.shared:
			return nil, fmt.Errorf("%s shared file %q already exists; in-place conversion is unsupported; choose an empty destination", target, rel)
		case err == nil && !info.Mode().IsRegular():
			return nil, fmt.Errorf("%s conversion destination %q is not a regular file", target, rel)
		case err == nil:
			publication.existed = true
			info, digest, err = snapshotCodexConversionFile(publication.dst)
			if err != nil {
				return nil, fmt.Errorf("inspect %s conversion destination %q: %w", target, rel, err)
			}
			publication.originalInfo, publication.originalDigest = info, digest
		case !os.IsNotExist(err):
			return nil, fmt.Errorf("inspect %s conversion destination %q: %w", target, rel, err)
		}
		publications = append(publications, publication)
	}
	if len(publications) == 0 {
		return publications, nil
	}
	recoveryParent, err := codexConversionStageParent(outDir)
	if err != nil {
		return nil, err
	}
	recoveryDir, err := os.MkdirTemp(recoveryParent, ".agent-marketplace-recovery-")
	if err != nil {
		return nil, fmt.Errorf("create %s conversion recovery directory: %w", target, err)
	}
	for i := range publications {
		publications[i].recoveryDir = recoveryDir
		publications[i].backup = filepath.Join(recoveryDir, fmt.Sprintf("%d.original", i))
		publications[i].quarantine = filepath.Join(recoveryDir, fmt.Sprintf("%d.current", i))
	}
	return publications, nil
}
