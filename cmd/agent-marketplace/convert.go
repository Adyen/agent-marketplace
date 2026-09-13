package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var (
	conversionHarnesses      = targetmeta.ConversionNames()
	codexConversionHooksPath = mustNativeAggregatePath(ir.KindHook)
	codexConversionMCPPath   = mustNativeAggregatePath(ir.KindMcpServer)
)

var codexConversionSharedPaths = [...]string{
	codexConversionHooksPath,
	codexConversionMCPPath,
}

// Devin conversion protects every native aggregate destination even when this
// source produces no fragment for one of them. In particular, config.json is
// the user-scope hook aggregate while the other two paths are project-scope
// aggregates; all three are unsafe stale-prune candidates in a conversion
// output root.
var devinConversionSharedPaths = [...]string{
	"config.json",
	"hooks.v1.json",
	"mcp_config.json",
}

func mustNativeAggregatePath(kind ir.ResourceKind) string {
	path, ok := targetmeta.NativeAggregatePath(targetmeta.TargetCodex, kind)
	if !ok {
		panic(fmt.Sprintf("Codex native aggregate path for %s is not registered", kind))
	}
	return path
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
	if autodetect {
		detected, ok := hcc.Autodetect(os.DirFS(srcPath))
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
	if err := rejectConversionSourceOutputOverlap(srcPath, outDir); err != nil {
		return err
	}
	canonicalSource, err := canonicalConversionPath(srcPath)
	if err != nil {
		return fmt.Errorf("resolve conversion source path %q: %w", srcPath, err)
	}
	canonicalOutput, err := canonicalConversionPath(outDir)
	if err != nil {
		return fmt.Errorf("resolve conversion output path %q: %w", outDir, err)
	}
	sourceRoot, err := snapshotConversionRoot(canonicalSource, true)
	if err != nil {
		return err
	}
	outputRoot, err := snapshotConversionRoot(canonicalOutput, false)
	if err != nil {
		return err
	}
	rootGuard := func() error {
		if err := revalidateConversionRoot(sourceRoot); err != nil {
			return fmt.Errorf("conversion source changed during conversion: %w", err)
		}
		if err := revalidateConversionRoot(outputRoot); err != nil {
			return fmt.Errorf("conversion output changed before publication: %w", err)
		}
		return nil
	}
	srcFS := os.DirFS(canonicalSource)

	result, err := hcc.Convert(srcFS, srcHarness, hcc.Harness(to), hcc.WithPluginRoot(canonicalSource))
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	if err := rootGuard(); err != nil {
		return err
	}

	writeMode := hcc.WriteModeOverwrite
	matOpts := []hcc.WriteOption{hcc.WithWriteMode(writeMode)}
	if syncMode {
		matOpts = []hcc.WriteOption{hcc.WithWriteMode(hcc.WriteModeSync), hcc.WithPruneStale(true)}
	}
	if dryRun {
		matOpts = append(matOpts, hcc.WithDryRun())
	}

	// Materialize results with error diagnostics so successful resources reach
	// disk while failed resources contribute nothing. Report the write and then
	// return the error so a partial conversion cannot look successful.
	var writeReport *hcc.WriteReport
	switch to {
	case "codex":
		writeReport, err = materializeCodexConversion(result, canonicalOutput, matOpts, dryRun, rootGuard)
	case "devin":
		writeReport, err = materializeDevinConversion(result, canonicalOutput, matOpts, dryRun, rootGuard)
	default:
		if err := rootGuard(); err != nil {
			return err
		}
		writeReport, err = hcc.Materialize(result, canonicalOutput, matOpts...)
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

// rejectConversionSourceOutputOverlap compares canonical filesystem paths
// before compilation or publication. The output may not equal, contain, or be
// an ancestor of the source: all three relationships can cause a conversion
// to read its own generated files or overwrite its input.
func rejectConversionSourceOutputOverlap(source, output string) error {
	canonicalSource, err := canonicalConversionPath(source)
	if err != nil {
		return fmt.Errorf("resolve conversion source path %q: %w", source, err)
	}
	canonicalOutput, err := canonicalConversionPath(output)
	if err != nil {
		return fmt.Errorf("resolve conversion output path %q: %w", output, err)
	}
	if conversionPathsOverlap(canonicalSource, canonicalOutput) {
		return fmt.Errorf("conversion source and output paths overlap: source %q, output %q; choose a separate destination", canonicalSource, canonicalOutput)
	}
	return nil
}

func canonicalConversionPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var missing []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(resolveErr) {
			return "", resolveErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", resolveErr
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func conversionPathsOverlap(source, output string) bool {
	for _, root := range []string{source, output} {
		other := output
		if root == output {
			other = source
		}
		rel, err := filepath.Rel(root, other)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return true
		}
	}
	return false
}

type conversionRootAncestor struct {
	path string
	info os.FileInfo
}

type conversionRootSnapshot struct {
	path      string
	exists    bool
	info      os.FileInfo
	ancestors []conversionRootAncestor
}

func snapshotConversionRoot(path string, required bool) (conversionRootSnapshot, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if required {
			return conversionRootSnapshot{}, fmt.Errorf("conversion source %q does not exist", path)
		}
	} else if err != nil {
		return conversionRootSnapshot{}, fmt.Errorf("inspect conversion root %q: %w", path, err)
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return conversionRootSnapshot{}, fmt.Errorf("conversion root %q is not a real directory", path)
	}

	snapshot := conversionRootSnapshot{path: path, exists: err == nil, info: info}
	for candidate := filepath.Dir(path); ; candidate = filepath.Dir(candidate) {
		ancestorInfo, ancestorErr := os.Lstat(candidate)
		switch {
		case ancestorErr == nil:
			if !ancestorInfo.IsDir() || ancestorInfo.Mode()&os.ModeSymlink != 0 {
				return conversionRootSnapshot{}, fmt.Errorf("conversion root ancestor %q is not a real directory", candidate)
			}
		case os.IsNotExist(ancestorErr):
			ancestorInfo = nil
		default:
			return conversionRootSnapshot{}, fmt.Errorf("inspect conversion root ancestor %q: %w", candidate, ancestorErr)
		}
		snapshot.ancestors = append(snapshot.ancestors, conversionRootAncestor{path: candidate, info: ancestorInfo})
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
	}
	return snapshot, nil
}

func revalidateConversionRoot(snapshot conversionRootSnapshot) error {
	info, err := os.Lstat(snapshot.path)
	if os.IsNotExist(err) {
		if snapshot.exists {
			return fmt.Errorf("root disappeared")
		}
	} else if err != nil {
		return err
	} else {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("root is no longer a real directory")
		}
		if !snapshot.exists || !os.SameFile(snapshot.info, info) {
			return fmt.Errorf("root identity changed")
		}
	}
	for _, ancestor := range snapshot.ancestors {
		current, currentErr := os.Lstat(ancestor.path)
		if ancestor.info == nil {
			if currentErr == nil {
				return fmt.Errorf("root ancestor %q was created", ancestor.path)
			}
			if os.IsNotExist(currentErr) {
				continue
			}
			return fmt.Errorf("inspect root ancestor %q: %w", ancestor.path, currentErr)
		}
		if currentErr != nil {
			return fmt.Errorf("root ancestor %q changed: %w", ancestor.path, currentErr)
		}
		if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(ancestor.info, current) {
			return fmt.Errorf("root ancestor %q identity changed", ancestor.path)
		}
	}
	return nil
}

// materializeCodexConversion publishes a one-shot conversion without adopting
// ownership. Sync reconciles shared configuration entry by entry; conversion
// requires an empty shared-file destination and publishes a staged result.
func materializeCodexConversion(result *hcc.Result, outDir string, matOpts []hcc.WriteOption, dryRun bool, rootGuard func() error) (*hcc.WriteReport, error) {
	return conversionPublisher{
		target:          "Codex",
		rootGuard:       rootGuard,
		shared:          codexConversionSharedPaths[:],
		clone:           codexConversionResult,
		preflight:       func(dir string) error { return preflightCodexConversion(dir) },
		validate:        func(dir string, _ []string) error { return validateCodexConversionStage(dir) },
		pruneCandidates: pruneCodexConversionStaleCandidates,
	}.materialize(result, outDir, matOpts, dryRun)
}

func finishCodexConversion(outDir string, report *hcc.WriteReport, resultErr error, prune func(string, []string) error) (*hcc.WriteReport, error) {
	return finishConversion(outDir, report, resultErr, prune)
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
		candidate := &clone.Artifacts[i]
		policy, policyErr := candidate.Policy()
		if policyErr != nil {
			return nil, fmt.Errorf("validate Codex conversion artifact %q: %w", candidate.Path, policyErr)
		}
		external := policy.IsExternal()
		if !isCodexConversionSharedPath(candidate.Path) {
			if external {
				return nil, fmt.Errorf("unexpected externally reconciled Codex artifact %q", candidate.Path)
			}
			continue
		}
		candidate.ExternalReconciliationRequired = false
		if external {
			candidate.Reconciliation = hcc.ReconciliationPolicy{Mode: hcc.ReconciliationReplace}
			candidate.MergeWithExisting = false
			candidate.ManagedByField = ""
			candidate.ManagedByValue = ""
		}
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

func publishCodexConversionWith(stageDir, outDir string, written []string, publishFile func(codexConversionPublication) error) error {
	return publishConversionWith(stageDir, outDir, written, codexConversionSharedPaths[:], "Codex", publishFile)
}

func publishCodexConversionFile(publication codexConversionPublication) error {
	if publication.existed {
		return replaceCodexConversionFile(publication, os.Link)
	}
	// Staging shares the destination filesystem, so a hard link exposes the
	// staged inode atomically and fails if another writer won the path.
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
			// Absent paths are published without replacement, including ordinary
			// files, so a concurrent writer cannot be overwritten.
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
			// Preserve a non-empty directory that now contains another writer's
			// content and keep the original failure primary.
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

func pruneCodexConversionStaleCandidates(outDir string, candidates []hcc.StaleFileCandidate) error {
	root := filepath.Clean(outDir)
	var pruneErrors []error
	for _, candidate := range candidates {
		rel := filepath.FromSlash(candidate.Path)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			pruneErrors = append(pruneErrors, fmt.Errorf("stale candidate %q escapes output root", candidate.Path))
			continue
		}
		path := filepath.Join(root, rel)
		if err := hcc.RemoveStaleFileCandidate(path, candidate); err != nil && !os.IsNotExist(err) {
			pruneErrors = append(pruneErrors, fmt.Errorf("remove stale file %q: %w", candidate.Path, err))
		}
	}
	return errors.Join(pruneErrors...)
}

func pruneCodexConversionStaleCandidatesWith(outDir string, candidates []hcc.StaleFileCandidate, removePath func(string) error) error {
	root := filepath.Clean(outDir)
	var pruneErrors []error
	for _, candidate := range candidates {
		rel := filepath.FromSlash(candidate.Path)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			pruneErrors = append(pruneErrors, fmt.Errorf("stale candidate %q escapes output root", candidate.Path))
			continue
		}
		path := filepath.Join(root, rel)
		unchanged, err := hcc.RevalidateStaleFileCandidate(path, candidate)
		if err != nil {
			pruneErrors = append(pruneErrors, fmt.Errorf("revalidate stale file %q: %w", candidate.Path, err))
			continue
		}
		if !unchanged {
			pruneErrors = append(pruneErrors, fmt.Errorf("%w: %s", hcc.ErrStalePruneConflict, candidate.Path))
			continue
		}
		if err := removePath(path); err != nil && !os.IsNotExist(err) {
			pruneErrors = append(pruneErrors, fmt.Errorf("remove stale file %q: %w", candidate.Path, err))
			continue
		}
		for dir := filepath.Dir(path); dir != root; {
			if err := removePath(dir); err != nil {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return errors.Join(pruneErrors...)
}

// materializeDevinConversion publishes a Devin conversion without adopting
// ownership. Shared aggregate paths come from artifact policies, are staged and
// syntax-validated, then exposed by hard link.
func materializeDevinConversion(result *hcc.Result, outDir string, matOpts []hcc.WriteOption, dryRun bool, rootGuard func() error) (*hcc.WriteReport, error) {
	shared, err := externalConversionPaths(result)
	if err != nil {
		return nil, err
	}
	return conversionPublisher{
		target:    "Devin",
		rootGuard: rootGuard,
		shared:    shared,
		clone: func(input *hcc.Result) (*hcc.Result, error) {
			return cloneNonOwningConversionResult(input, shared), nil
		},
		preflight:       func(dir string) error { return preflightConversionPaths(dir, shared, "Devin") },
		validate:        validateDevinConversionStage,
		pruneCandidates: pruneCodexConversionStaleCandidates,
	}.materialize(result, outDir, matOpts, dryRun)
}

func externalConversionPaths(result *hcc.Result) ([]string, error) {
	if result == nil {
		return nil, errors.New("devin conversion result must not be nil")
	}
	for _, a := range result.Artifacts {
		if _, err := a.Policy(); err != nil {
			return nil, fmt.Errorf("validate Devin conversion artifact %q: %w", a.Path, err)
		}
	}
	return append([]string(nil), devinConversionSharedPaths[:]...), nil
}

func cloneNonOwningConversionResult(result *hcc.Result, shared []string) *hcc.Result {
	clone := *result
	clone.Artifacts = append([]hcc.Artifact(nil), result.Artifacts...)
	for i := range clone.Artifacts {
		a := &clone.Artifacts[i]
		if containsString(shared, a.Path) {
			a.ExternalReconciliationRequired = false
			a.Reconciliation = hcc.ReconciliationPolicy{}
			a.MergeWithExisting = false
			a.ManagedByField, a.ManagedByValue = "", ""
		}
	}
	return &clone
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func preflightConversionPaths(outDir string, shared []string, target string) error {
	for _, rel := range shared {
		path := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s shared file %q already exists; in-place conversion to %s is unsupported; choose an empty destination", target, rel, target)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s shared file %q: %w", target, rel, err)
		}
	}
	return nil
}

func validateDevinConversionStage(stage string, paths []string) error {
	for _, rel := range paths {
		if filepath.Ext(rel) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Errorf("read staged Devin aggregate %q: %w", rel, err)
		}
		if _, err := jsonc.Parse(rel, raw); err != nil {
			return fmt.Errorf("validate staged Devin aggregate %q: %w", rel, err)
		}
	}
	return nil
}

func pruneCodexConversionStaleWith(outDir string, removed []string, removePath func(string) error) error {
	root := filepath.Clean(outDir)
	var pruneErrors []error
	for _, rel := range removed {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := removePath(path); err != nil && !os.IsNotExist(err) {
			pruneErrors = append(pruneErrors, fmt.Errorf("remove stale file %q: %w", rel, err))
			continue
		}
		for dir := filepath.Dir(path); dir != root; {
			if err := removePath(dir); err != nil {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return errors.Join(pruneErrors...)
}
