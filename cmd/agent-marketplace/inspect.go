package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
)

type pluginInspection struct {
	entry    catalog.Entry
	source   config.RegistryEntry
	delivery string
}

func newInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect plugin/<name>",
		Short: "Inspect a configured plugin package without installing it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspectPlugin(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
}

func runInspectPlugin(ctx context.Context, out io.Writer, locator string) error {
	result, release, inspection, err := loadPluginForInspection(ctx, locator)
	if err != nil {
		return err
	}
	defer release()
	return writePluginInspection(out, inspection, result.LockMetadata)
}

func loadPluginForInspection(ctx context.Context, locator string) (pipeline.ResolveResult, func(), pluginInspection, error) {
	name, err := pluginNameFromLocator(locator)
	if err != nil {
		return pipeline.ResolveResult{}, nil, pluginInspection{}, err
	}
	if err := prepareDependencyTransactions(ctx, true); err != nil {
		return pipeline.ResolveResult{}, nil, pluginInspection{}, err
	}
	workspace, err := pipeline.NewWorkspace(config.FindRepoRoot(), config.FindLocalConfigDir())
	if err != nil {
		return pipeline.ResolveResult{}, nil, pluginInspection{}, err
	}
	result, release := pipeline.ResolveWithExactLockLease(ctx, pipeline.ResolveOptions{
		Workspace:                 workspace,
		CatalogMode:               catalog.LoadModeInspect,
		ReadOnlyExactLocks:        true,
		AllowMissingReadOnlyLocks: true,
	})
	if result.LoadedCfg == nil {
		release()
		if len(result.Errors) > 0 {
			return pipeline.ResolveResult{}, nil, pluginInspection{}, errors.Join(result.Errors...)
		}
		return pipeline.ResolveResult{}, nil, pluginInspection{}, errors.New("configuration could not be loaded")
	}
	inspection, err := findPluginForInspection(result, name)
	if err != nil {
		release()
		return pipeline.ResolveResult{}, nil, pluginInspection{}, err
	}
	return result, release, inspection, nil
}

func pluginNameFromLocator(locator string) (string, error) {
	const prefix = "plugin/"
	if !strings.HasPrefix(locator, prefix) || strings.TrimSpace(strings.TrimPrefix(locator, prefix)) == "" {
		return "", fmt.Errorf("plugin operation requires a locator in the form plugin/<name>")
	}
	return strings.TrimPrefix(locator, prefix), nil
}

func findPluginForInspection(result pipeline.ResolveResult, name string) (pluginInspection, error) {
	var matches []pluginInspection
	for key, loaded := range result.CatalogLoad.Catalogs {
		var source config.RegistryEntry
		for _, entry := range result.LoadedCfg.Registries {
			if entry.CatalogKey == key {
				source = entry
				break
			}
		}
		for _, entry := range loaded.Entries {
			if entry.Kind == ir.KindPlugin && entry.Name == name {
				inspection := pluginInspection{entry: entry, source: source, delivery: "auto"}
				for _, artifact := range result.Resolved {
					if artifact.Kind == ir.KindPlugin && artifact.Name == name &&
						artifact.Source == source.Source && artifact.CatalogKey == key &&
						artifact.Selection != nil && artifact.Selection.Delivery != "" {
						inspection.delivery = artifact.Selection.Delivery
						break
					}
				}
				matches = append(matches, inspection)
			}
		}
	}
	switch len(matches) {
	case 0:
		return pluginInspection{}, fmt.Errorf("plugin %q was not found in configured registries", name)
	case 1:
		return matches[0], nil
	default:
		return pluginInspection{}, fmt.Errorf("plugin %q is available from multiple configured registries", name)
	}
}

func writePluginInspection(out io.Writer, inspection pluginInspection, locks []pipeline.LockMetadata) error {
	entry := inspection.entry
	var report strings.Builder
	fmt.Fprintf(&report, "PLUGIN %s\n", terminalSafeText(entry.Name))
	if entry.Description != "" {
		fmt.Fprintf(&report, "DESCRIPTION: %s\n", terminalSafeText(entry.Description))
	}
	fmt.Fprintf(&report, "SOURCE: %s\n", terminalSafeText(redact.URL(inspection.source.URL)))
	if entry.Plugin != nil && entry.Plugin.Source.Path != "" {
		fmt.Fprintf(&report, "SOURCE PATH: %s\n", terminalSafeText(entry.Plugin.Source.Path))
	}
	fmt.Fprintf(&report, "REVISION: %s\n", terminalSafeText(pluginSourceRevision(inspection.source, locks)))
	format := ""
	if entry.Plugin != nil {
		format = entry.Plugin.Format
	}
	fmt.Fprintf(&report, "FORMAT: %s\n", terminalSafeText(format))

	plugin, _ := entry.Resource.(*ir.Plugin)
	if plugin == nil {
		fmt.Fprintln(&report, "MEMBERS:\n  none")
		fmt.Fprintln(&report, "FILES:\n  none")
		fmt.Fprintln(&report, "DECLARED DEPENDENCIES:\n  none")
	} else {
		if plugin.FormatVersion != "" {
			fmt.Fprintf(&report, "FORMAT VERSION: %s\n", terminalSafeText(plugin.FormatVersion))
		}
		fmt.Fprintln(&report, "MEMBERS:")
		if len(plugin.Members) == 0 {
			fmt.Fprintln(&report, "  none")
		}
		for _, member := range plugin.Members {
			if member.Resource != nil {
				fmt.Fprintf(&report, "  %s\n", terminalSafeText(member.Key()))
			}
		}
		fmt.Fprintln(&report, "FILES:")
		if len(plugin.Files) == 0 {
			fmt.Fprintln(&report, "  none")
		}
		for _, file := range plugin.Files {
			executable := ""
			if file.Executable {
				executable = " executable"
			}
			fmt.Fprintf(&report, "  %s sha256:%s%s\n", terminalSafeText(file.Path), file.Digest, executable)
		}
		dependencies := plugin.DeclaredDependencies()
		fmt.Fprintln(&report, "DECLARED DEPENDENCIES:")
		if len(dependencies) == 0 {
			fmt.Fprintln(&report, "  none")
		}
		for _, dependency := range dependencies {
			fmt.Fprintf(&report, "  %s\n", terminalSafeText(dependency))
		}
	}

	fmt.Fprintln(&report, "UNSUPPORTED CONTENT:")
	if plugin == nil || len(plugin.Invalid) == 0 {
		fmt.Fprintln(&report, "  none")
	} else {
		for _, invalid := range plugin.Invalid {
			fmt.Fprintf(&report, "  %s: %s (%s)\n", terminalSafeText(invalid.Path), terminalSafeText(invalid.Reason), terminalSafeText(invalid.Diagnostic))
		}
	}

	losses, blockers := pluginProjectionDiagnostics(plugin, entry)
	if entry.Plugin != nil && entry.Plugin.Refusal != nil {
		blockers = append(blockers, *entry.Plugin.Refusal)
	}
	if plugin != nil && plugin.HasDependencies() {
		blockers = append(blockers, diag.Diagnostic{
			Code:     diag.CodePluginFormatDependenciesUnsupported,
			Severity: diag.SeverityError,
			Resource: "plugin/" + plugin.Name(),
			Message:  "plugin declares dependencies that cannot be installed",
		})
	}
	fmt.Fprintln(&report, "TARGET DELIVERY:")
	for _, target := range harnesscompile.TargetNames() {
		state := "unavailable"
		if plugin != nil {
			state = "decomposed"
			if inspection.delivery == "native" {
				state = "native (pending)"
			}
		}
		for _, blocker := range blockers {
			if blocker.Target == "" || blocker.Target == target {
				state = "blocked (" + blocker.Code + ")"
				break
			}
		}
		fmt.Fprintf(&report, "  %s: %s\n", terminalSafeText(target), state)
	}
	if err := writePluginDiagnostics(&report, "LOSSES", losses); err != nil {
		return err
	}
	if err := writePluginDiagnostics(&report, "BLOCKERS", blockers); err != nil {
		return err
	}
	_, err := io.WriteString(out, report.String())
	return err
}

func pluginSourceRevision(source config.RegistryEntry, locks []pipeline.LockMetadata) string {
	if source.IsLocal() {
		return "local"
	}
	for _, lock := range locks {
		if lock.Matches(source.Source, source.URL, source.Ref) {
			return lock.Commit
		}
	}
	return "unlocked (" + source.Ref + ")"
}

func pluginProjectionDiagnostics(plugin *ir.Plugin, entry catalog.Entry) ([]diag.Diagnostic, []diag.Diagnostic) {
	if plugin == nil {
		return nil, nil
	}
	var losses, blockers []diag.Diagnostic
	for _, target := range harnesscompile.TargetNames() {
		projection, err := harnesscompile.CompilePackage(
			plugin, harnesscompile.Target(target), pluginpolicy.DeliveryDecompose,
			harnesscompile.ScopeProject, pluginpolicy.Roots{},
		)
		if err != nil {
			blockers = append(blockers, diag.Diagnostic{
				Code:     diag.CodePluginFormatUnsupported,
				Severity: diag.SeverityError,
				Target:   target,
				Resource: "plugin/" + entry.Name,
				Message:  err.Error(),
			})
			continue
		}
		for _, member := range projection.Members {
			for _, diagnostic := range member.Diagnostics {
				if diagnostic.Target == "" {
					diagnostic.Target = target
				}
				switch diagnostic.Severity {
				case diag.SeverityError:
					blockers = append(blockers, diagnostic)
				case diag.SeverityWarning:
					losses = append(losses, diagnostic)
				}
			}
		}
	}
	return losses, blockers
}

func writePluginDiagnostics(out io.Writer, heading string, diagnostics []diag.Diagnostic) error {
	if _, err := fmt.Fprintf(out, "%s:\n", heading); err != nil {
		return err
	}
	if len(diagnostics) == 0 {
		_, err := fmt.Fprintln(out, "  none")
		return err
	}
	for _, diagnostic := range diagnostics {
		if _, err := fmt.Fprintf(out, "  %s (%s): %s\n", terminalSafeText(diagnostic.Target), terminalSafeText(diagnostic.Code), terminalSafeText(diagnostic.Message)); err != nil {
			return err
		}
	}
	return nil
}
