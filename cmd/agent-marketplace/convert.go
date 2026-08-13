package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var (
	conversionHarnesses = targetmeta.ConversionNames()
)

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
	writeReport, err := hcc.Materialize(result, outDir, matOpts...)
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
