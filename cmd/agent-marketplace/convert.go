package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var (
	conversionHarnesses = targetmeta.ConversionNames()
)

const (
	codexConversionHooksPath = ".codex/hooks.json"
	codexConversionMCPPath   = ".codex/config.toml"
)

var codexConversionSharedPaths = [...]string{
	codexConversionHooksPath,
	codexConversionMCPPath,
}

func newConvertCmd() *cobra.Command {
	var from string
	var to string
	var outDir string
	var autodetect bool
	var syncMode bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "convert --from <harness> --to <harness> <path>",
		Short: "Convert a harness-specific plugin directory to another harness layout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConvert(cmd.OutOrStdout(), args[0], from, to, outDir, autodetect, syncMode, dryRun)
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "Source harness: "+strings.Join(conversionHarnesses, ", "))
	cmd.Flags().StringVar(&to, "to", "", "Target harness: "+strings.Join(conversionHarnesses, ", "))
	cmd.Flags().StringVar(&outDir, "out", "", "Output directory (default: <path>-<to>)")
	cmd.Flags().BoolVar(&autodetect, "autodetect", false, "Autodetect source harness instead of using --from")
	cmd.Flags().BoolVar(&syncMode, "sync", false, "Use sync mode and prune stale output files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report planned writes without changing files")
	return cmd
}

func runConvert(out io.Writer, srcPath, from, to, outDir string, autodetect, syncMode, dryRun bool) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("--to is required")
	}
	if !targetmeta.IsConversionTarget(to) {
		return fmt.Errorf("unknown or unsupported target harness %q; must be one of: %v", to, conversionHarnesses)
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

	writeMode := hcc.WriteModeOverwrite
	matOpts := []hcc.WriteOption{hcc.WithWriteMode(writeMode)}
	if syncMode {
		matOpts = []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true)}
	}
	if dryRun {
		matOpts = append(matOpts, hcc.WithDryRun())
	}

	// A Result carrying error diagnostics is materialized, not refused: the
	// resources that compiled cleanly are complete and reach disk, while the
	// resources that failed contributed nothing. Report exactly what landed and
	// then fail, so a partial conversion can never read as a clean one.
	var writeReport *hcc.WriteReport
	if to == "codex" {
		writeReport, err = materializeCodexConversion(result, outDir, matOpts, dryRun)
	} else {
		writeReport, err = hcc.Materialize(result, outDir, matOpts...)
	}
	if err != nil && !errors.Is(err, hcc.ErrPartialResult) {
		return fmt.Errorf("materialize: %w", err)
	}

	if _, printErr := fmt.Fprintf(out, "Converted %s -> %s into %s\n", srcHarness, to, filepath.Clean(outDir)); printErr != nil {
		return printErr
	}
	if _, printErr := fmt.Fprintf(out, "Written %d, Skipped %d, Removed %d, Diagnostics %d\n", len(writeReport.Written), len(writeReport.Skipped), len(writeReport.Removed), len(result.Diagnostics)); printErr != nil {
		return printErr
	}
	if err != nil {
		for _, s := range writeReport.SkippedResources {
			if _, printErr := fmt.Fprintf(out, "skipped %s %q (%s)\n", s.Kind, s.Name, strings.Join(s.Codes, ", ")); printErr != nil {
				return printErr
			}
		}
		if writeReport.PruneSuppressed {
			if _, printErr := fmt.Fprintln(out, "pruning was skipped because the conversion was partial"); printErr != nil {
				return printErr
			}
		}
		return fmt.Errorf("materialize: %w", err)
	}
	return nil
}

// materializeCodexConversion is the non-owning publication path for one-shot
// conversion. Sync owns shared configuration and reconciles it entry-by-entry;
// conversion instead requires an empty shared-file destination, stages a whole
// result, and publishes complete aggregates without replacement.
func materializeCodexConversion(result *hcc.Result, outDir string, matOpts []hcc.WriteOption, dryRun bool) (*hcc.WriteReport, error) {
	materializable, err := codexConversionResult(result)
	if err != nil {
		return nil, err
	}

	// A dry run has no publication race to serialize. Keeping it lock-free is
	// what preserves the flag's no-filesystem-mutation contract: acquiring the
	// durable advisory lock would create the lock file even though output is not
	// being written.
	if dryRun {
		if err := preflightCodexConversion(outDir); err != nil {
			return nil, err
		}
		return hcc.Materialize(materializable, outDir, matOpts...)
	}

	store, err := ownership.Open(outDir)
	if err != nil {
		return nil, fmt.Errorf("open Codex conversion destination lock: %w", err)
	}
	release, err := store.Lock()
	if err != nil {
		return nil, fmt.Errorf("lock Codex conversion destination: %w", err)
	}
	defer release()

	slog.Debug("Codex conversion publication started", "out_dir", filepath.Clean(outDir), "root_key", store.RootKey())
	if err := preflightCodexConversion(outDir); err != nil {
		slog.Warn("Codex conversion refused by destination preflight", "out_dir", filepath.Clean(outDir), "error", err)
		return nil, err
	}

	// Ask the generic materializer for the final-destination report without
	// allowing it to write. This preserves overwrite/sync/prune and partial-result
	// reporting while the real bytes are staged elsewhere.
	planOpts := append([]hcc.WriteOption(nil), matOpts...)
	planOpts = append(planOpts, hcc.WithDryRun())
	report, resultErr := hcc.Materialize(materializable, outDir, planOpts...)
	if resultErr != nil && !errors.Is(resultErr, hcc.ErrPartialResult) {
		return nil, resultErr
	}

	stageParent, err := codexConversionStageParent(outDir)
	if err != nil {
		return nil, err
	}
	stageDir, err := os.MkdirTemp(stageParent, ".agent-marketplace-convert-")
	if err != nil {
		return nil, fmt.Errorf("create Codex conversion staging directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(stageDir); removeErr != nil {
			slog.Warn("Codex conversion staging cleanup failed", "stage_dir", stageDir, "error", removeErr)
		}
	}()

	if _, stageErr := hcc.Materialize(materializable, stageDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); stageErr != nil && !errors.Is(stageErr, hcc.ErrPartialResult) {
		return nil, fmt.Errorf("stage Codex conversion: %w", stageErr)
	}
	if err := validateCodexConversionStage(stageDir); err != nil {
		return nil, err
	}
	if err := publishCodexConversion(stageDir, outDir, report.Written); err != nil {
		slog.Error("Codex conversion publication failed", "out_dir", filepath.Clean(outDir), "error", err)
		return nil, err
	}
	return finishCodexConversion(outDir, report, resultErr, pruneCodexConversionStale)
}

func finishCodexConversion(outDir string, report *hcc.WriteReport, resultErr error, prune func(string, []string) error) (*hcc.WriteReport, error) {
	// Publication is the commit boundary. Stale-file pruning follows it and is
	// deliberately not rolled back: once one stale file has been removed there
	// is no byte-safe way to recreate it without extending conversion into an
	// ownership/backup transaction. A prune failure is therefore reported as a
	// partial operation while the complete published result remains in place.
	if pruneErr := prune(outDir, report.Removed); pruneErr != nil {
		slog.Error("Codex conversion published with stale-prune failures", "out_dir", filepath.Clean(outDir), "error", pruneErr)
		return report, fmt.Errorf("codex conversion output was published, but stale output pruning failed: %w", pruneErr)
	}

	slog.Info("Codex conversion published", "out_dir", filepath.Clean(outDir), "written", len(report.Written), "removed", len(report.Removed), "partial", report.Partial)
	return report, resultErr
}

func codexConversionStageParent(outDir string) (string, error) {
	absOutDir, err := filepath.Abs(outDir)
	if err != nil {
		return "", fmt.Errorf("resolve Codex conversion output directory: %w", err)
	}
	for candidate := absOutDir; ; candidate = filepath.Dir(candidate) {
		info, statErr := os.Lstat(candidate)
		if statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("codex conversion staging parent %q is not a directory", candidate)
			}
			return candidate, nil
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect Codex conversion staging parent %q: %w", candidate, statErr)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", fmt.Errorf("no existing directory can host Codex conversion staging for %q", outDir)
		}
	}
}

func codexConversionResult(result *hcc.Result) (*hcc.Result, error) {
	if result == nil {
		return nil, errors.New("codex conversion result must not be nil")
	}
	clone := *result
	clone.Artifacts = append([]hcc.Artifact(nil), result.Artifacts...)
	clone.Diagnostics = append([]hcc.Diagnostic(nil), result.Diagnostics...)
	clone.Skipped = append([]hcc.SkippedResource(nil), result.Skipped...)
	for i := range clone.Artifacts {
		artifact := &clone.Artifacts[i]
		if !isCodexConversionSharedPath(artifact.Path) {
			if artifact.ExternalReconciliationRequired {
				return nil, fmt.Errorf("unexpected externally reconciled Codex artifact %q", artifact.Path)
			}
			continue
		}
		artifact.ExternalReconciliationRequired = false
		artifact.MergeWithExisting = false
		artifact.ManagedByField = ""
		artifact.ManagedByValue = ""
	}
	return &clone, nil
}

func validateCodexConversionStage(stageDir string) error {
	hooksPath := filepath.Join(stageDir, filepath.FromSlash(codexConversionHooksPath))
	if raw, err := os.ReadFile(hooksPath); err == nil {
		if _, err := jsonc.Parse(codexConversionHooksPath, raw); err != nil {
			return fmt.Errorf("validate staged Codex hooks: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read staged Codex hooks: %w", err)
	}

	mcpPath := filepath.Join(stageDir, filepath.FromSlash(codexConversionMCPPath))
	if raw, err := os.ReadFile(mcpPath); err == nil {
		var document map[string]any
		if err := toml.Unmarshal(raw, &document); err != nil {
			return fmt.Errorf("validate staged Codex MCP config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read staged Codex MCP config: %w", err)
	}
	return nil
}

func preflightCodexConversion(outDir string) error {
	for _, rel := range codexConversionSharedPaths {
		path := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Lstat(path); err == nil {
			return codexConversionSharedFileError(rel)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect Codex shared file %q: %w", rel, err)
		}
	}
	return nil
}

func codexConversionSharedFileError(rel string) error {
	return fmt.Errorf("codex shared file %q already exists; in-place conversion to Codex is unsupported; choose an empty destination", rel)
}

type codexConversionPublication struct {
	rel            string
	src            string
	dst            string
	backup         string
	quarantine     string
	recoveryDir    string
	sourceInfo     os.FileInfo
	sourceDigest   [sha256.Size]byte
	originalInfo   os.FileInfo
	originalDigest [sha256.Size]byte
	existed        bool
	shared         bool
}

func publishCodexConversion(stageDir, outDir string, written []string) error {
	return publishCodexConversionWith(stageDir, outDir, written, publishCodexConversionFile)
}

func publishCodexConversionWith(stageDir, outDir string, written []string, publishFile func(codexConversionPublication) error) error {
	publications, err := planCodexConversionPublication(stageDir, outDir, written)
	if err != nil {
		return err
	}
	defer removeEmptyCodexConversionRecoveryDirs(publications)

	createdDirs := make([]string, 0, 4)
	published := make([]codexConversionPublication, 0, len(publications))
	for _, publication := range publications {
		if err := ensureCodexConversionDirs(outDir, filepath.Dir(publication.dst), &createdDirs); err != nil {
			return rollbackCodexConversion(fmt.Errorf("prepare destination for %q: %w", publication.rel, err), published, createdDirs)
		}

		err = publishFile(publication)
		if err != nil {
			if publication.shared && os.IsExist(err) {
				err = codexConversionSharedFileError(publication.rel)
			} else {
				err = fmt.Errorf("publish Codex artifact %q: %w", publication.rel, err)
			}
			return rollbackCodexConversion(err, published, createdDirs)
		}
		published = append(published, publication)
	}
	return discardCodexConversionRecovery(publications)
}

func publishCodexConversionFile(publication codexConversionPublication) error {
	if publication.existed {
		return replaceCodexConversionFile(publication, os.Link)
	}
	// Staging shares the destination filesystem, so a hard link atomically
	// exposes the complete staged inode and fails if a writer won the path.
	return os.Link(publication.src, publication.dst)
}

func replaceCodexConversionFile(publication codexConversionPublication, linkFile func(string, string) error) error {
	if err := os.Rename(publication.dst, publication.backup); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("codex conversion publication conflict for %q: destination changed after planning", publication.rel)
		}
		return fmt.Errorf("quarantine prior Codex artifact %q: %w", publication.rel, err)
	}
	matches, err := codexConversionFileMatches(publication.backup, publication.originalInfo, publication.originalDigest)
	if err != nil || !matches {
		conflict := fmt.Errorf("codex conversion publication conflict for %q: destination changed after planning", publication.rel)
		if restoreErr := restoreCodexConversionQuarantine(publication.backup, publication.dst); restoreErr != nil {
			return errors.Join(conflict, codexConversionRecoveryError(publication.rel, publication.backup), restoreErr)
		}
		if err != nil {
			return errors.Join(conflict, err)
		}
		return conflict
	}
	if err := linkFile(publication.src, publication.dst); err != nil {
		if restoreErr := restoreCodexConversionQuarantine(publication.backup, publication.dst); restoreErr != nil {
			conflict := fmt.Errorf("codex conversion publication conflict for %q: destination changed during publication", publication.rel)
			return errors.Join(err, conflict, codexConversionRecoveryError(publication.rel, publication.backup), restoreErr)
		}
		return err
	}
	return nil
}

func planCodexConversionPublication(stageDir, outDir string, written []string) ([]codexConversionPublication, error) {
	ordered := orderCodexConversionPaths(written)
	publications := make([]codexConversionPublication, 0, len(ordered))
	for _, rel := range ordered {
		publication := codexConversionPublication{
			rel:    rel,
			src:    filepath.Join(stageDir, filepath.FromSlash(rel)),
			dst:    filepath.Join(outDir, filepath.FromSlash(rel)),
			shared: isCodexConversionSharedPath(rel),
		}
		info, digest, err := snapshotCodexConversionFile(publication.src)
		if err != nil {
			return nil, fmt.Errorf("inspect staged Codex artifact %q: %w", rel, err)
		}
		publication.sourceInfo = info
		publication.sourceDigest = digest

		info, err = os.Lstat(publication.dst)
		switch {
		case err == nil && publication.shared:
			return nil, codexConversionSharedFileError(rel)
		case err == nil && !info.Mode().IsRegular():
			return nil, fmt.Errorf("codex conversion destination %q is not a regular file", rel)
		case err == nil:
			publication.existed = true
			info, digest, err = snapshotCodexConversionFile(publication.dst)
			if err != nil {
				return nil, fmt.Errorf("inspect Codex conversion destination %q: %w", rel, err)
			}
			publication.originalInfo = info
			publication.originalDigest = digest
		case os.IsNotExist(err):
			// Every absent path, not only a shared path, is published without
			// replacement. This is stronger than the shared-file requirement and
			// prevents a concurrent ordinary file from being overwritten.
		case err != nil:
			return nil, fmt.Errorf("inspect Codex conversion destination %q: %w", rel, err)
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
		return nil, fmt.Errorf("create Codex conversion recovery directory: %w", err)
	}
	for i := range publications {
		publications[i].recoveryDir = recoveryDir
		publications[i].backup = filepath.Join(recoveryDir, fmt.Sprintf("%d.original", i))
		publications[i].quarantine = filepath.Join(recoveryDir, fmt.Sprintf("%d.current", i))
	}
	return publications, nil
}

func orderCodexConversionPaths(paths []string) []string {
	ordered := make([]string, 0, len(paths))
	for _, shared := range codexConversionSharedPaths {
		for _, path := range paths {
			if path == shared {
				ordered = append(ordered, path)
			}
		}
	}
	for _, path := range paths {
		if !isCodexConversionSharedPath(path) {
			ordered = append(ordered, path)
		}
	}
	return ordered
}

func isCodexConversionSharedPath(path string) bool {
	return path == codexConversionHooksPath || path == codexConversionMCPPath
}

func ensureCodexConversionDirs(outDir, dir string, created *[]string) error {
	root := filepath.Clean(outDir)
	rel, err := filepath.Rel(root, filepath.Clean(dir))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("directory %q escapes output root %q", dir, outDir)
	}

	dirs := []string{root}
	current := root
	if rel != "." {
		for _, component := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, component)
			dirs = append(dirs, current)
		}
	}
	for _, candidate := range dirs {
		info, statErr := os.Lstat(candidate)
		if statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%s is not a directory", candidate)
			}
			continue
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		if mkdirErr := os.Mkdir(candidate, 0o755); mkdirErr != nil {
			if !os.IsExist(mkdirErr) {
				return mkdirErr
			}
			info, statErr = os.Lstat(candidate)
			if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%s was created concurrently as a non-directory", candidate)
			}
			continue
		}
		*created = append(*created, candidate)
	}
	return nil
}

func rollbackCodexConversion(cause error, published []codexConversionPublication, createdDirs []string) error {
	var rollbackErrors []error
	for i := len(published) - 1; i >= 0; i-- {
		if err := rollbackCodexConversionPublication(published[i]); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	for i := len(createdDirs) - 1; i >= 0; i-- {
		if err := os.Remove(createdDirs[i]); err != nil && !os.IsNotExist(err) {
			// A non-empty directory now contains something this conversion did
			// not create. Preserve it and leave the original failure primary.
			if !errors.Is(err, os.ErrExist) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove directory %q: %w", createdDirs[i], err))
			}
		}
	}
	if len(rollbackErrors) == 0 {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("codex conversion rollback: %w", errors.Join(rollbackErrors...)))
}

func rollbackCodexConversionPublication(publication codexConversionPublication) error {
	if err := os.Rename(publication.dst, publication.quarantine); err != nil {
		conflict := fmt.Errorf("codex conversion rollback conflict for %q: destination changed after publication; current path preserved", publication.rel)
		if publication.existed {
			conflict = errors.Join(conflict, codexConversionRecoveryError(publication.rel, publication.backup))
		}
		if os.IsNotExist(err) {
			return conflict
		}
		return errors.Join(conflict, err)
	}

	matches, verifyErr := codexConversionFileMatches(publication.quarantine, publication.sourceInfo, publication.sourceDigest)
	if verifyErr != nil || !matches {
		conflict := fmt.Errorf("codex conversion rollback conflict for %q: destination changed after publication; current path preserved", publication.rel)
		if publication.existed {
			conflict = errors.Join(conflict, codexConversionRecoveryError(publication.rel, publication.backup))
		}
		if restoreErr := restoreCodexConversionQuarantine(publication.quarantine, publication.dst); restoreErr != nil {
			conflict = errors.Join(conflict, codexConversionRecoveryError(publication.rel, publication.quarantine), restoreErr)
		}
		if verifyErr != nil {
			conflict = errors.Join(conflict, verifyErr)
		}
		return conflict
	}

	if err := os.Remove(publication.quarantine); err != nil {
		return errors.Join(
			fmt.Errorf("codex conversion rollback conflict for %q: remove verified published artifact: %w", publication.rel, err),
			codexConversionRecoveryError(publication.rel, publication.quarantine),
		)
	}
	if !publication.existed {
		return nil
	}
	if err := restoreCodexConversionQuarantine(publication.backup, publication.dst); err != nil {
		return errors.Join(
			fmt.Errorf("codex conversion rollback conflict for %q: destination changed while restoring the prior artifact", publication.rel),
			codexConversionRecoveryError(publication.rel, publication.backup),
			err,
		)
	}
	return nil
}

func snapshotCodexConversionFile(path string) (os.FileInfo, [sha256.Size]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	if !info.Mode().IsRegular() {
		return nil, [sha256.Size]byte{}, errors.New("artifact is not a regular file")
	}
	digest, err := digestCodexConversionFile(path)
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	if !os.SameFile(info, current) {
		return nil, [sha256.Size]byte{}, errors.New("artifact changed while it was inspected")
	}
	return info, digest, nil
}

func codexConversionFileMatches(path string, expectedInfo os.FileInfo, expectedDigest [sha256.Size]byte) (bool, error) {
	current, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if expectedInfo == nil || !current.Mode().IsRegular() || !os.SameFile(expectedInfo, current) {
		return false, nil
	}
	digest, err := digestCodexConversionFile(path)
	if err != nil {
		return false, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return os.SameFile(current, after) && digest == expectedDigest, nil
}

func digestCodexConversionFile(path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return [sha256.Size]byte{}, errors.Join(copyErr, closeErr)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func restoreCodexConversionQuarantine(quarantine, dst string) error {
	if err := os.Link(quarantine, dst); err != nil {
		return fmt.Errorf("restore quarantined Codex artifact: %w", err)
	}
	if err := os.Remove(quarantine); err != nil {
		return fmt.Errorf("remove restored Codex artifact quarantine: %w", err)
	}
	return nil
}

func codexConversionRecoveryError(rel, path string) error {
	return fmt.Errorf("prior Codex artifact %q preserved for recovery at %q", rel, path)
}

func discardCodexConversionRecovery(publications []codexConversionPublication) error {
	var cleanupErrors []error
	for _, publication := range publications {
		if !publication.existed {
			continue
		}
		matches, err := codexConversionFileMatches(publication.backup, publication.originalInfo, publication.originalDigest)
		if err != nil || !matches {
			cleanupErrors = append(cleanupErrors, codexConversionRecoveryError(publication.rel, publication.backup))
			if err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
			continue
		}
		if err := os.Remove(publication.backup); err != nil {
			cleanupErrors = append(cleanupErrors, errors.Join(codexConversionRecoveryError(publication.rel, publication.backup), err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func removeEmptyCodexConversionRecoveryDirs(publications []codexConversionPublication) {
	seen := make(map[string]struct{}, 1)
	for _, publication := range publications {
		if publication.recoveryDir == "" {
			continue
		}
		if _, ok := seen[publication.recoveryDir]; ok {
			continue
		}
		seen[publication.recoveryDir] = struct{}{}
		_ = os.Remove(publication.recoveryDir)
	}
}

func pruneCodexConversionStale(outDir string, removed []string) error {
	return pruneCodexConversionStaleWith(outDir, removed, os.Remove)
}

func pruneCodexConversionStaleWith(outDir string, removed []string, removeFile func(string) error) error {
	root := filepath.Clean(outDir)
	var pruneErrors []error
	for _, rel := range removed {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := removeFile(path); err != nil && !os.IsNotExist(err) {
			pruneErrors = append(pruneErrors, fmt.Errorf("remove stale file %q: %w", rel, err))
			continue
		}
		for dir := filepath.Dir(path); dir != root; dir = filepath.Dir(dir) {
			if err := os.Remove(dir); err != nil {
				break
			}
		}
	}
	return errors.Join(pruneErrors...)
}
