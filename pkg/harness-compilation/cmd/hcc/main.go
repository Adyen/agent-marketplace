// hcc is a thin CLI for the harness-compilation library.
//
// Usage:
//
//	hcc compile --from <dir> --target <t> --out <outDir>
//	hcc convert  --from <src-harness> --to <dst-harness> <dir>
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var (
	validHarnesses      = targetmeta.CompilerNames()
	conversionHarnesses = targetmeta.ConversionNames()
)

func isValidTarget(s string) bool {
	return targetmeta.IsCompilerTarget(s)
}

func main() {
	if err := hcc.ValidateTargetRegistrations(); err != nil {
		fmt.Fprintf(os.Stderr, "target registration validation failed: %v\n", err)
		os.Exit(1)
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: hcc <command> [options]")
		fmt.Fprintln(os.Stderr, "Commands: compile, convert")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "compile":
		compileCmd(os.Args[2:])
	case "convert":
		convertCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q; use 'compile' or 'convert'\n", os.Args[1])
		os.Exit(1)
	}
}

// compileCmd implements: hcc compile --from <dir> --target <t> --out <outDir>
func compileCmd(args []string) {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	from := fs.String("from", "", "Source Claude-plugin directory")
	target := fs.String("target", "", "Target harness: "+strings.Join(validHarnesses, ", "))
	out := fs.String("out", "", "Output directory")
	sync := fs.Bool("sync", false, "Use sync mode (overwrite + prune stale)")
	dryRun := fs.Bool("dry-run", false, "Dry run: report planned writes without I/O")
	_ = fs.Parse(args)

	if *from == "" || *target == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "Usage: hcc compile --from <dir> --target <t> --out <outDir>")
		fmt.Fprintln(os.Stderr, "Targets:", strings.Join(validHarnesses, ", "))
		os.Exit(1)
	}

	*target = strings.TrimSpace(*target)
	t := hcc.Target(*target)
	if !isValidTarget(*target) {
		fmt.Fprintf(os.Stderr, "unknown target %q; must be one of: %v\n", *target, validHarnesses)
		os.Exit(1)
	}

	src := os.DirFS(*from)
	result, err := hcc.CompileBundle(src, t, hcc.WithPluginRoot(*from))
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile: %v\n", err)
		os.Exit(1)
	}

	printDiagnostics(result.Diagnostics)
	materialize(result, *out, *sync, *dryRun)
}

// convertCmd implements: hcc convert --from <src> --to <dst> <dir>
func convertCmd(args []string) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	from := fs.String("from", "", "Source harness: "+strings.Join(conversionHarnesses, ", "))
	to := fs.String("to", "", "Target harness: "+strings.Join(conversionHarnesses, ", "))
	out := fs.String("out", "", "Output directory (defaults to ./<dir>-<target>)")
	sync := fs.Bool("sync", false, "Use sync mode (overwrite + prune stale)")
	dryRun := fs.Bool("dry-run", false, "Dry run: report planned writes without I/O")
	autodetect := fs.Bool("autodetect", false, "Autodetect source harness instead of --from")
	_ = fs.Parse(args)

	dir := fs.Arg(0)

	if dir == "" {
		fmt.Fprintln(os.Stderr, "Usage: hcc convert --from <src> --to <dst> <dir>")
		fmt.Fprintln(os.Stderr, "  <dir>    Source plugin directory")
		fmt.Fprintln(os.Stderr, "  --from   Source harness (or use --autodetect)")
		fmt.Fprintln(os.Stderr, "  --to     Target harness")
		fmt.Fprintln(os.Stderr, "  --out    Output directory (default: <dir>-<target>)")
		os.Exit(1)
	}

	*from = strings.TrimSpace(*from)
	*to = strings.TrimSpace(*to)
	srcHarness := hcc.Harness(*from)
	if *autodetect {
		srcFS := os.DirFS(dir)
		detected, ok := hcc.Autodetect(srcFS)
		if !ok {
			fmt.Fprintln(os.Stderr, "autodetect: could not identify source harness")
			os.Exit(1)
		}
		srcHarness = detected
		fmt.Fprintf(os.Stderr, "autodetect: source harness = %s\n", srcHarness)
	} else if *from == "" {
		fmt.Fprintln(os.Stderr, "error: --from is required (or use --autodetect)")
		os.Exit(1)
	}

	if !targetmeta.IsConversionTarget(string(srcHarness)) {
		fmt.Fprintf(os.Stderr, "unknown or unsupported source harness %q; must be one of: %v\n", srcHarness, conversionHarnesses)
		os.Exit(1)
	}

	if *to == "" {
		fmt.Fprintln(os.Stderr, "error: --to is required")
		os.Exit(1)
	}
	if !targetmeta.IsConversionTarget(*to) {
		fmt.Fprintf(os.Stderr, "unknown or unsupported target harness %q; must be one of: %v\n", *to, conversionHarnesses)
		os.Exit(1)
	}

	dstHarness := hcc.Harness(*to)

	outDir := *out
	if outDir == "" {
		outDir = dir + "-" + *to
	}

	srcFS := os.DirFS(dir)
	result, err := hcc.Convert(srcFS, srcHarness, dstHarness, hcc.WithPluginRoot(dir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: %v\n", err)
		os.Exit(1)
	}

	printDiagnostics(result.Diagnostics)
	materialize(result, outDir, *sync, *dryRun)
}

// printDiagnostics prints all diagnostics to stderr.
func printDiagnostics(diags []hcc.Diagnostic) {
	for _, d := range diags {
		prefix := "INFO"
		switch d.Severity {
		case hcc.SeverityError:
			prefix = "ERROR"
		case hcc.SeverityWarning:
			prefix = "WARN"
		}
		fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", prefix, d.Code, d.Message)
	}
}

// materialize writes artifacts to disk and prints a report.
func materialize(result *hcc.Result, outDir string, sync, dryRun bool) {
	writeMode := hcc.WriteModeOverwrite
	if sync {
		writeMode = hcc.WriteModeSync
	}

	opts := []hcc.WriteOption{hcc.WithWriteMode(writeMode)}
	if sync {
		opts = append(opts, hcc.WithPruneStale(true))
	}
	if dryRun {
		opts = append(opts, hcc.WithDryRun())
	}

	// A partial result still reaches disk for every resource that compiled
	// cleanly, so print the report before exiting non-zero. A non-partial
	// materialization failure may still leave partial disk changes from earlier
	// renames or stale-file removals.
	report, err := hcc.Materialize(result, outDir, opts...)
	partial := errors.Is(err, hcc.ErrPartialResult)
	if err != nil && !partial {
		fmt.Fprintf(os.Stderr, "materialize: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Written: %d  Skipped: %d  Removed: %d\n",
		len(report.Written), len(report.Skipped), len(report.Removed))

	for _, w := range report.Written {
		fmt.Printf("+ %s\n", w)
	}
	for _, s := range report.Skipped {
		fmt.Printf("= %s\n", s)
	}
	for _, r := range report.Removed {
		fmt.Printf("- %s\n", r)
	}

	if partial {
		for _, s := range report.SkippedResources {
			fmt.Fprintf(os.Stderr, "! skipped %s %q (%s)\n", s.Kind, s.Name, strings.Join(s.Codes, ", "))
		}
		if report.PruneSuppressed {
			fmt.Fprintln(os.Stderr, "! pruning was skipped because the result was partial")
		}
		fmt.Fprintf(os.Stderr, "materialize: %v\n", err)
		os.Exit(1)
	}
}
