package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginexport"
)

type exportTarget struct {
	path     string
	existing os.FileInfo
}

type exportRootFS struct{ root *os.Root }

func (f exportRootFS) Open(name string) (fs.File, error) { return f.root.Open(name) }

func newExportCmd() *cobra.Command {
	var format, out string
	var allowLoss bool
	cmd := &cobra.Command{
		Use:   "export plugin/<name>",
		Short: "Export a locked plugin package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExportPlugin(cmd.Context(), cmd.OutOrStdout(), args[0], format, out, allowLoss)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "Package format to export")
	cmd.Flags().StringVar(&out, "out", "", "Output directory")
	cmd.Flags().BoolVar(&allowLoss, "allow-loss", false, "Allow listed non-security omissions")
	_ = cmd.MarkFlagRequired("format")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func runExportPlugin(ctx context.Context, out io.Writer, locator, format, output string, allowLoss bool) error {
	if format != "agent-plugin" {
		return diag.NewCodedError(diag.CodePluginFormatUnsupported, diag.SourceRef{},
			fmt.Errorf("export format is not supported"))
	}
	target, err := validateExportTarget(output)
	if err != nil {
		return err
	}
	result, release, inspection, err := loadPluginForInspection(ctx, locator)
	if err != nil {
		return err
	}
	defer release()
	if !pluginHasExactLock(inspection.source, result.LockMetadata) {
		return errors.New("plugin is not locked; export requires an exact lock")
	}
	plugin, ok := inspection.entry.Resource.(*ir.Plugin)
	if !ok || plugin == nil {
		return errors.New("plugin package data is unavailable")
	}
	if inspection.entry.PackageRoot == "" {
		return errors.New("locked plugin package files are unavailable")
	}
	if exportTargetWithinPackage(target.path, inspection.entry.PackageRoot) {
		return errors.New("--out cannot be inside the locked plugin package")
	}
	source, err := os.OpenRoot(inspection.entry.PackageRoot)
	if err != nil {
		return errors.New("locked plugin package files are unavailable")
	}
	defer func() { _ = source.Close() }()
	exported, err := pluginexport.Export(plugin, exportRootFS{root: source}, pluginexport.Options{AllowLoss: allowLoss})
	if err != nil {
		return errors.Join(err, writeExportReport(out, exported, true))
	}
	if err := publishExportPackage(target, exported.Files); err != nil {
		return errors.Join(err, writeExportReport(out, exported, true))
	}
	return writeExportReport(out, exported, false)
}

func pluginHasExactLock(source config.RegistryEntry, locks []pipeline.LockMetadata) bool {
	for _, lock := range locks {
		if lock.SnapshotAvailable && lock.Matches(source.Source, source.URL, source.Ref) {
			return true
		}
	}
	return false
}

func exportTargetWithinPackage(output, packageRoot string) bool {
	source, err := filepath.EvalSymlinks(packageRoot)
	if err != nil {
		return true
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return true
	}
	candidate := filepath.Join(parent, filepath.Base(output))
	relative, err := filepath.Rel(source, candidate)
	return err != nil || relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func validateExportTarget(output string) (exportTarget, error) {
	if strings.TrimSpace(output) == "" {
		return exportTarget{}, errors.New("--out is required")
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return exportTarget{}, errors.New("cannot resolve --out directory")
	}
	parent, err := os.Stat(filepath.Dir(absolute))
	if err != nil || !parent.IsDir() {
		return exportTarget{}, errors.New("--out parent must be an existing directory")
	}
	target := exportTarget{path: absolute}
	info, err := os.Lstat(absolute)
	if errors.Is(err, fs.ErrNotExist) {
		return target, nil
	}
	if err != nil {
		return exportTarget{}, errors.New("cannot inspect --out path")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return exportTarget{}, errors.New("--out must be an absent path or a directory")
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return exportTarget{}, errors.New("cannot inspect --out directory")
	}
	if len(entries) != 0 {
		return exportTarget{}, diag.NewCodedError(diag.CodePluginExportOutNotEmpty, diag.SourceRef{},
			errors.New("export output directory is not empty"))
	}
	target.existing = info
	return target, nil
}

func writeExportReport(out io.Writer, result pluginexport.Result, failed bool) error {
	label := "OMITTED"
	if failed {
		label = "LOSS"
	}
	for _, loss := range result.Losses {
		if _, err := fmt.Fprintf(out, "%s: %s (%s)\n", label, terminalSafeText(loss.Path), terminalSafeText(loss.Reason)); err != nil {
			return err
		}
	}
	for _, restriction := range result.Restrictions {
		if _, err := fmt.Fprintf(out, "RESTRICTION: %s (%s)\n", terminalSafeText(restriction.Path), terminalSafeText(restriction.Reason)); err != nil {
			return err
		}
	}
	return nil
}

func publishExportPackage(target exportTarget, files []pluginexport.File) (err error) {
	stage, err := os.MkdirTemp(filepath.Dir(target.path), ".agent-marketplace-export-*")
	if err != nil {
		return fmt.Errorf("stage exported package: %w", err)
	}
	staged := true
	defer func() {
		if staged {
			if cleanupErr := os.RemoveAll(stage); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remove export staging directory: %w", cleanupErr))
			}
		}
	}()

	for _, file := range files {
		name := filepath.Join(stage, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return fmt.Errorf("create staged package directory: %w", err)
		}
		mode := fs.FileMode(0o644)
		if file.Executable {
			mode = 0o755
		}
		if err := os.WriteFile(name, file.Bytes, mode); err != nil {
			return fmt.Errorf("write staged package file: %w", err)
		}
		if err := os.Chmod(name, mode); err != nil {
			return fmt.Errorf("set staged package file mode: %w", err)
		}
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return fmt.Errorf("set staged package directory mode: %w", err)
	}
	if err := revalidateExportTarget(target); err != nil {
		return err
	}
	if target.existing != nil {
		if err := os.Remove(target.path); err != nil {
			return fmt.Errorf("replace empty export directory: %w", err)
		}
	}
	if err := os.Rename(stage, target.path); err != nil {
		publishErr := fmt.Errorf("publish exported package: %w", err)
		if target.existing != nil {
			if restoreErr := os.Mkdir(target.path, 0o755); restoreErr != nil {
				return errors.Join(publishErr, fmt.Errorf("restore empty export directory: %w", restoreErr))
			}
		}
		return publishErr
	}
	staged = false
	return nil
}

func revalidateExportTarget(target exportTarget) error {
	info, err := os.Lstat(target.path)
	if target.existing == nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return errors.New("--out path changed before package publication")
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(target.existing, info) {
		return errors.New("--out directory changed before package publication")
	}
	entries, err := os.ReadDir(target.path)
	if err != nil {
		return errors.New("cannot inspect --out directory before publication")
	}
	if len(entries) != 0 {
		return diag.NewCodedError(diag.CodePluginExportOutNotEmpty, diag.SourceRef{},
			errors.New("export output directory is not empty"))
	}
	return nil
}
