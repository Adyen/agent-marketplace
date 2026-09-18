package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	marketplacepkg "github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

const installOrange = "\x1b[38;5;208m"

func newInstallCmd() *cobra.Command {
	var tag string
	var installAll bool
	var registry string
	var scopeStr string
	var kindFilter string
	var fromURL string
	var fromRef string
	var fromPath string
	var installedName string
	var acceptRisk bool
	var assumeYes bool
	var marketplace string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "install [resource-name|marketplace-endpoint]",
		Short: "Install a resource or configure a Marketplace endpoint",
		Args: func(cmd *cobra.Command, args []string) error {
			tag = strings.TrimSpace(tag)
			registry = strings.TrimSpace(registry)
			kindFilter = strings.TrimSpace(kindFilter)
			fromURL = strings.TrimSpace(fromURL)
			fromRef = strings.TrimSpace(fromRef)
			fromPath = strings.TrimSpace(fromPath)
			installedName = strings.TrimSpace(installedName)
			marketplace = strings.TrimSpace(marketplace)
			marketplaceFlags := acceptRisk || assumeYes || installedName != ""
			marketplaceLocatorArg := len(args) == 1 && looksLikeMarketplaceLocator(args[0])
			marketplaceBrowserURL := len(args) == 1 && marketplacepkg.LooksLikeMarketplaceBrowserURL(args[0])
			marketplaceCoordinate := marketplace != "" || marketplaceLocatorArg

			switch {
			case marketplaceBrowserURL && !marketplaceLocatorArg:
				return fmt.Errorf("invalid Marketplace browser URL; use a canonical Marketplace resource URL")
			case marketplace != "" && (fromURL != "" || installAll || tag != ""):
				return fmt.Errorf("--marketplace is only supported for a single Marketplace resource")
			case marketplaceCoordinate && fromURL != "":
				return fmt.Errorf("marketplace locators cannot be combined with --from")
			case marketplaceFlags && (installAll || tag != ""):
				return fmt.Errorf("--as/--accept-risk/--yes are only supported for a single Marketplace resource")
			case marketplaceFlags && !marketplaceCoordinate:
				return fmt.Errorf("--as/--accept-risk/--yes are only supported for Marketplace locators")
			case installAll && tag != "":
				return fmt.Errorf("cannot specify both --all and --tag")
			case installAll && len(args) > 0:
				return fmt.Errorf("accepts only --all without a skill name argument")
			case registry != "" && !installAll:
				return fmt.Errorf("cannot specify --registry without --all")
			case fromURL != "" && tag != "":
				return fmt.Errorf("cannot specify both --from and --tag")
			case fromURL != "" && installAll:
				return fmt.Errorf("cannot specify both --from and --all")
			case fromRef != "" && fromURL == "":
				return fmt.Errorf("--ref requires --from")
			case fromPath != "" && fromURL == "":
				return fmt.Errorf("--path requires --from")
			case installAll:
				return nil
			case fromURL != "" && len(args) > 1:
				return fmt.Errorf("--from accepts at most one resource name argument")
			case fromURL != "":
				// --from with name installs one resource; without name installs all from that URL
				return nil
			case tag != "" && len(args) > 0:
				return fmt.Errorf("cannot specify both a skill name and --tag")
			case marketplace != "" && len(args) != 1:
				return fmt.Errorf("--marketplace accepts exactly one Marketplace resource locator")
			case tag == "" && len(args) != 1 && marketplace == "":
				return fmt.Errorf("accepts exactly one resource name or Marketplace endpoint, --tag, --all, or --from")
			case len(args) == 1 && looksLikeMarketplaceLocator(args[0]):
				return nil
			case marketplace != "" && len(args) == 1:
				return nil
			case tag == "" && len(args) == 1 && looksLikeRegistryURL(args[0]):
				return fmt.Errorf("argument %q looks like a registry URL; use --from %q instead", args[0], args[0])
			case (acceptRisk || assumeYes || installedName != "") && len(args) == 1 && !looksLikeMarketplaceLocator(args[0]):
				return fmt.Errorf("--as/--accept-risk/--yes are only supported for Marketplace locators")
			case tag != "" && len(args) != 0:
				return fmt.Errorf("accepts only --tag without a skill name argument")
			default:
				return nil
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := pipeline.ParseScope(scopeStr)
			if err != nil {
				return err
			}
			in := cmd.InOrStdin()
			fd, hasFD := in.(interface{ Fd() uintptr })
			interactive := hasFD && isTerminal(fd.Fd()) && !acceptRisk && !assumeYes
			return runInstall(cmd.Context(), cmd.OutOrStdout(), in, interactive, verbose, args, tag, installAll, registry, scope, cmd.Flags().Changed("scope"), kindFilter, fromURL, fromRef, fromPath, installedName, acceptRisk || assumeYes, marketplace)
		},
	}

	cmd.Flags().StringVar(&tag, "tag", "", "Install all skills matching a tag")
	cmd.Flags().BoolVar(&installAll, "all", false, "Install every discovered skill from the configured registries")
	cmd.Flags().StringVar(&registry, "registry", "", "Limit --all to one configured registry URL")
	cmd.Flags().StringVar(&scopeStr, "scope", "", "Config layer to write to: repo, user, or local (Marketplace endpoints default to user; resources default to repo)")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "Resource kind for name disambiguation: skill, command, subagent, hook, or mcp")
	cmd.Flags().StringVar(&fromURL, "from", "", "Install directly from a registry URL; skill name is inferred from web URLs pointing to a specific resource")
	cmd.Flags().StringVar(&fromRef, "ref", "", "Git ref (branch/tag/sha) for --from registry; requires --from")
	cmd.Flags().StringVar(&fromPath, "path", "", "Subdirectory within the --from registry; requires --from")
	cmd.Flags().StringVar(&installedName, "as", "", "Install a Marketplace resource under this local name")
	cmd.Flags().BoolVar(&acceptRisk, "accept-risk", false, "Accept Marketplace findings and active capabilities")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "Accept Marketplace findings and active capabilities (automation)")
	cmd.Flags().StringVar(&marketplace, "marketplace", "", "Marketplace alias or endpoint for a Marketplace locator")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show full Marketplace resource and security details")

	return cmd
}

func runInstall(ctx context.Context, out io.Writer, in io.Reader, interactive, verbose bool, args []string, tag string, installAll bool, registry string, scope pipeline.Scope, scopeSpecified bool, kindFilter, fromURL, fromRef, fromPath, installedName string, acceptRisk bool, marketplace string) error {
	kind, hasKind, err := parseOptionalKind(kindFilter)
	if err != nil {
		return err
	}
	detailsShown := false

	repoRoot := config.FindRepoRoot()
	result, err := pipeline.Install(ctx, pipeline.InstallOptions{
		RepoRoot:            repoRoot,
		Args:                args,
		Tag:                 tag,
		InstallAll:          installAll,
		Registry:            registry,
		Kind:                kind,
		HasKind:             hasKind,
		Scope:               scope,
		ScopeSpecified:      scopeSpecified,
		FromURL:             fromURL,
		FromRef:             fromRef,
		FromPath:            fromPath,
		InstalledName:       installedName,
		AcceptRisk:          acceptRisk,
		Marketplace:         marketplace,
		DependencyReconcile: reconcileCommandDependencies,
		PrepareDependencies: prepareInstallDependencies,
	})
	if err != nil {
		var consentErr *pipeline.MarketplaceConsentError
		if !errors.As(err, &consentErr) {
			return err
		}
		if interactive && consentErr.Acceptable {
			result, err = confirmMarketplaceInstall(ctx, out, in, consentErr, verbose)
			if err != nil || result == nil {
				return err
			}
			detailsShown = true
		} else {
			if writeErr := writeMarketplaceConsentRequired(out, consentErr, verbose); writeErr != nil {
				return writeErr
			}
			return err
		}
	}

	if result.MarketplaceEndpoint != "" {
		if result.MarketplaceAlreadyConfigured {
			_, err = fmt.Fprintf(out, "Marketplace %s is already configured in %s\n", result.MarketplaceEndpoint, result.MarketplaceConfigPath)
		} else {
			_, err = fmt.Fprintf(out, "Added Marketplace %s to %s\n", result.MarketplaceEndpoint, result.MarketplaceConfigPath)
		}
		return err
	}

	if err := writeGroupedWarnings(out, result.RequirementWarnings, false); err != nil {
		return err
	}

	return writeInstallResult(out, result, scope, verbose, detailsShown)
}

var (
	commitMarketplaceInstall = pipeline.CommitPreparedMarketplaceInstall
	installOutputStyle       = newSyncOutputStyle
)

func confirmMarketplaceInstall(ctx context.Context, out io.Writer, in io.Reader, consent *pipeline.MarketplaceConsentError, verbose bool) (*pipeline.InstallResult, error) {
	if err := writeMarketplaceRiskSummary(out, consent, verbose); err != nil {
		return nil, err
	}
	if verbose && consent.Preview != "" {
		if _, err := fmt.Fprintf(out, "%s\n\n", consent.Preview); err != nil {
			return nil, err
		}
	}
	if _, err := fmt.Fprint(out, "Install despite these risks? [y/N]: "); err != nil {
		return nil, err
	}
	reply, readErr := bufio.NewReader(in).ReadString('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	if strings.EqualFold(strings.TrimSpace(reply), "y") || strings.EqualFold(strings.TrimSpace(reply), "yes") {
		slog.Info("Marketplace install risks accepted interactively", "resource", consent.Summary.Name, "findings", len(consent.Summary.Findings), "capabilities", len(consent.Summary.Capabilities))
		return commitMarketplaceInstall(ctx, consent.Prepared)
	}
	return nil, writeInstallStatus(out, syncRed, "Nothing has been installed.")
}

func writeMarketplaceConsentRequired(out io.Writer, consent *pipeline.MarketplaceConsentError, verbose bool) error {
	if err := writeMarketplaceRiskSummary(out, consent, verbose); err != nil {
		return err
	}
	if verbose && consent.Preview != "" {
		if _, err := fmt.Fprintf(out, "%s\n\n", consent.Preview); err != nil {
			return err
		}
	}
	return writeInstallStatus(out, syncRed, "Nothing has been installed.")
}

func writeMarketplaceRiskSummary(out io.Writer, consent *pipeline.MarketplaceConsentError, verbose bool) error {
	if err := writeMarketplaceInstallDetails(out, consent.Summary, verbose); err != nil {
		return err
	}
	prefix, color := "Warning", installOrange
	if !consent.Acceptable {
		prefix, color = "Blocked", syncRed
	}
	if len(consent.Summary.Findings) > 0 {
		if err := writeInstallStatus(out, color, fmt.Sprintf("%s: %s", prefix, countLabel(len(consent.Summary.Findings), "security finding"))); err != nil {
			return err
		}
		for _, finding := range consent.Summary.Findings {
			line := fmt.Sprintf("  %s  [%s] %s", strings.ToUpper(terminalSafeText(finding.Severity)), terminalSafeText(finding.Resource), terminalSafeText(finding.Message))
			if err := writeInstallStatus(out, color, line); err != nil {
				return err
			}
		}
	}
	if len(consent.Summary.Capabilities) > 0 {
		if err := writeInstallStatus(out, color, "Active capabilities: "+terminalSafeText(strings.Join(consent.Summary.Capabilities, ", "))); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out)
	return err
}

func writeMarketplaceInstallDetails(out io.Writer, summary pipeline.MarketplaceInstallSummary, verbose bool) error {
	if _, err := fmt.Fprintf(out, "Installing %s · %s\n", terminalSafeText(summary.Name), terminalSafeText(summary.Kind)); err != nil {
		return err
	}
	if summary.Kind == "collection" && len(summary.Resources) > 0 {
		if _, err := fmt.Fprintf(out, "\nIncludes %s:\n", marketplaceIncludedLabel(summary.Resources)); err != nil {
			return err
		}
		limit := len(summary.Resources)
		if !verbose && limit > 10 {
			limit = 10
		}
		mixedKinds := false
		for _, resource := range summary.Resources[1:] {
			mixedKinds = mixedKinds || resource.Kind != summary.Resources[0].Kind
		}
		lastKind := ""
		for _, resource := range summary.Resources[:limit] {
			indent := "  "
			if mixedKinds {
				indent = "    "
				if resource.Kind != lastKind {
					label := pluralLabel(resourceKindSingular(resource.Kind))
					if _, err := fmt.Fprintf(out, "  %s%s:\n", strings.ToUpper(label[:1]), label[1:]); err != nil {
						return err
					}
					lastKind = resource.Kind
				}
			}
			if _, err := fmt.Fprintf(out, "%s%s\n", indent, terminalSafeText(resource.Name)); err != nil {
				return err
			}
		}
		if hidden := len(summary.Resources) - limit; hidden > 0 {
			if _, err := fmt.Fprintf(out, "  … and %d more (use --verbose)\n", hidden); err != nil {
				return err
			}
		}
	}
	if len(summary.Sources) > 0 {
		label := "Source"
		if len(summary.Sources) > 1 {
			label = "Sources"
		}
		if _, err := fmt.Fprintf(out, "\n%s: %s\n", label, terminalSafeText(strings.Join(summary.Sources, ", "))); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out)
	return err
}

func writeInstallResult(out io.Writer, result *pipeline.InstallResult, scope pipeline.Scope, verbose, detailsShown bool) error {
	if result.MarketplacePreview != "" || result.MarketplaceSummary.Name != "" {
		if !detailsShown {
			if err := writeMarketplaceInstallDetails(out, result.MarketplaceSummary, verbose); err != nil {
				return err
			}
			if verbose && result.MarketplacePreview != "" {
				if _, err := fmt.Fprintf(out, "%s\n\n", result.MarketplacePreview); err != nil {
					return err
				}
			}
		}
		name := result.MarketplaceSummary.Name
		if name == "" && len(result.Installed) > 0 {
			name = result.Installed[0]
		}
		if result.IsCollection {
			return writeInstallStatus(out, syncGreen, fmt.Sprintf("✓ Installed %s · %s", terminalSafeText(name), marketplaceIncludedLabel(result.MarketplaceSummary.Resources)))
		}
		return writeInstallStatus(out, syncGreen, fmt.Sprintf("✓ Installed %s", terminalSafeText(name)))
	}
	if result.IsCollection {
		_, err := fmt.Fprintf(out, "Installed collection %s %s\n", terminalSafeText(result.CollectionLabel), pipeline.InstallScopeLabel(scope))
		return err
	}
	_, err := fmt.Fprintf(out, "Installed %s %s\n", terminalSafeText(strings.Join(result.Installed, ", ")), pipeline.InstallScopeLabel(scope))
	return err
}

func writeInstallStatus(out io.Writer, color, text string) error {
	_, err := fmt.Fprintln(out, installOutputStyle(out).paint(color, text))
	return err
}

func marketplaceIncludedLabel(resources []pipeline.MarketplaceInstallResource) string {
	if len(resources) == 0 {
		return ""
	}
	kind := resources[0].Kind
	for _, resource := range resources[1:] {
		if resource.Kind != kind {
			return countLabel(len(resources), "resource")
		}
	}
	return countLabel(len(resources), resourceKindSingular(kind))
}

func resourceKindSingular(kind string) string {
	if singular := map[string]string{"skill": "skill", "command": "command", "subagent": "subagent", "hook": "hook", "mcpserver": "MCP server"}[kind]; singular != "" {
		return singular
	}
	return "resource"
}

func countLabel(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", count, pluralLabel(singular))
}

func pluralLabel(singular string) string {
	if strings.HasSuffix(singular, "y") {
		return strings.TrimSuffix(singular, "y") + "ies"
	}
	return singular + "s"
}

var terminalPrintableSyntax = strings.NewReplacer(`\\`, `\`, `\"`, `"`)

func terminalSafeText(value string) string {
	escaped := strings.TrimSuffix(strings.TrimPrefix(marketplace.QuoteTerminal(value), `"`), `"`)
	return terminalPrintableSyntax.Replace(escaped)
}

// looksLikeRegistryURL reports whether the argument looks like a Git hosting
// web URL (GitLab/GitHub) that should be used with --from instead of as a
// literal resource name.
func looksLikeRegistryURL(arg string) bool {
	parsed := pipeline.ParseGitWebURL(arg)
	return parsed.Ok && parsed.Ref != ""
}

func looksLikeMarketplaceLocator(arg string) bool {
	_, err := marketplacepkg.ParseMarketplaceLocator(arg)
	return err == nil
}
