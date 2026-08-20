package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
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

	cmd := &cobra.Command{
		Use:   "install [resource-name]",
		Short: "Add a resource or tag to the repo config and sync it",
		Args: func(cmd *cobra.Command, args []string) error {
			tag = strings.TrimSpace(tag)
			registry = strings.TrimSpace(registry)
			kindFilter = strings.TrimSpace(kindFilter)
			fromURL = strings.TrimSpace(fromURL)
			fromRef = strings.TrimSpace(fromRef)
			fromPath = strings.TrimSpace(fromPath)

			switch {
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
			case tag == "" && len(args) != 1:
				return fmt.Errorf("accepts exactly one skill name argument, --tag, --all, or --from")
			case tag == "" && len(args) == 1 && looksLikeRegistryURL(args[0]):
				return fmt.Errorf("argument %q looks like a registry URL; use --from %q instead", args[0], args[0])
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
			return runInstall(cmd.Context(), cmd.OutOrStdout(), args, tag, installAll, registry, scope, kindFilter, fromURL, fromRef, fromPath)
		},
	}

	cmd.Flags().StringVar(&tag, "tag", "", "Install all skills matching a tag")
	cmd.Flags().BoolVar(&installAll, "all", false, "Install every discovered skill from the configured registries")
	cmd.Flags().StringVar(&registry, "registry", "", "Limit --all to one configured registry URL")
	cmd.Flags().StringVar(&scopeStr, "scope", "", "Config layer to write to: repo (default), user, or local")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "Resource kind for name disambiguation: skill, command, subagent, hook, or mcp")
	cmd.Flags().StringVar(&fromURL, "from", "", "Install directly from a registry URL; skill name is inferred from web URLs pointing to a specific resource")
	cmd.Flags().StringVar(&fromRef, "ref", "", "Git ref (branch/tag/sha) for --from registry; requires --from")
	cmd.Flags().StringVar(&fromPath, "path", "", "Subdirectory within the --from registry; requires --from")

	return cmd
}

func runInstall(ctx context.Context, out io.Writer, args []string, tag string, installAll bool, registry string, scope pipeline.Scope, kindFilter, fromURL, fromRef, fromPath string) error {
	kind, hasKind, err := parseOptionalKind(kindFilter)
	if err != nil {
		return err
	}

	repoRoot := config.FindRepoRoot()
	result, err := pipeline.Install(ctx, pipeline.InstallOptions{
		RepoRoot:   repoRoot,
		Args:       args,
		Tag:        tag,
		InstallAll: installAll,
		Registry:   registry,
		Kind:       kind,
		HasKind:    hasKind,
		Scope:      scope,
		FromURL:    fromURL,
		FromRef:    fromRef,
		FromPath:   fromPath,
	})
	if err != nil {
		return err
	}

	if err := writeGroupedWarnings(out, result.RequirementWarnings, false); err != nil {
		return err
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
	return parsed.Ok
}
