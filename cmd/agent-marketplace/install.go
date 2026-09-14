package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

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
			marketplaceCoordinate := marketplace != "" || marketplaceLocatorArg

			switch {
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
			return runInstall(cmd.Context(), cmd.OutOrStdout(), args, tag, installAll, registry, scope, cmd.Flags().Changed("scope"), kindFilter, fromURL, fromRef, fromPath, installedName, acceptRisk || assumeYes, marketplace)
		},
	}

	cmd.Flags().StringVar(&tag, "tag", "", "Install all skills matching a tag")
	cmd.Flags().BoolVar(&installAll, "all", false, "Install every discovered skill from the configured registries")
	cmd.Flags().StringVar(&registry, "registry", "", "Limit --all to one configured registry URL")
	cmd.Flags().StringVar(&scopeStr, "scope", "", "Config layer to write to: repo, user, or local (Marketplace endpoints default to user; resources default to repo)")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "Resource kind for name disambiguation: skill, command, subagent, hook, mcp, rule, or collection")
	cmd.Flags().StringVar(&fromURL, "from", "", "Install directly from a registry URL; skill name is inferred from web URLs pointing to a specific resource")
	cmd.Flags().StringVar(&fromRef, "ref", "", "Git ref (branch/tag/sha) for --from registry; requires --from")
	cmd.Flags().StringVar(&fromPath, "path", "", "Subdirectory within the --from registry; requires --from")
	cmd.Flags().StringVar(&installedName, "as", "", "Install a Marketplace resource under this local name")
	cmd.Flags().BoolVar(&acceptRisk, "accept-risk", false, "Accept Marketplace findings and active capabilities")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "Accept Marketplace findings and active capabilities (automation)")
	cmd.Flags().StringVar(&marketplace, "marketplace", "", "Marketplace alias or endpoint for a Marketplace locator")

	return cmd
}

func runInstall(ctx context.Context, out io.Writer, args []string, tag string, installAll bool, registry string, scope pipeline.Scope, scopeSpecified bool, kindFilter, fromURL, fromRef, fromPath, installedName string, acceptRisk bool, marketplace string) error {
	kind, hasKind, err := parseOptionalKind(kindFilter)
	if err != nil {
		return err
	}

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
	})
	if err != nil {
		var consentErr *pipeline.MarketplaceConsentError
		if errors.As(err, &consentErr) && consentErr.Preview != "" {
			if _, writeErr := fmt.Fprintln(out, consentErr.Preview); writeErr != nil {
				return writeErr
			}
		}
		return err
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

	if result.MarketplacePreview != "" {
		if _, err = fmt.Fprintln(out, result.MarketplacePreview); err != nil {
			return err
		}
	}

	if result.IsCollection {
		_, err = fmt.Fprintf(out, "Installed collection %s %s\n", result.CollectionLabel, pipeline.InstallScopeLabel(scope))
		return err
	}

	if _, err = fmt.Fprintf(out, "Installed %s %s\n", strings.Join(result.Installed, ", "), pipeline.InstallScopeLabel(scope)); err != nil {
		return err
	}
	return nil
}

// looksLikeRegistryURL reports whether the argument looks like a Git hosting
// web URL (GitLab/GitHub) that should be used with --from instead of as a
// literal resource name.
func looksLikeRegistryURL(arg string) bool {
	parsed := pipeline.ParseGitWebURL(arg)
	return parsed.Ok && parsed.Ref != ""
}

func looksLikeMarketplaceLocator(arg string) bool {
	_, err := marketplace.ParseMarketplaceLocator(arg)
	return err == nil
}
