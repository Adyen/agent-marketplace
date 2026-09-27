//go:build e2e

package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// The E2E suite measures which public commands and flags the built binary
// really ran, and against which configuration schema. These hooks exist only
// in e2e builds and do nothing unless the suite sets their variables.
const (
	e2eCommandTreeEnv   = "AGENT_MARKETPLACE_E2E_COMMAND_TREE"
	e2eInvocationDirEnv = "AGENT_MARKETPLACE_E2E_INVOCATION_DIR"
)

type e2eCommandTree struct {
	Commands []string `json:"commands"`
	Flags    []string `json:"flags"`
}

type e2eInvocation struct {
	Command string            `json:"command"`
	Flags   []string          `json:"flags"`
	OK      bool              `json:"ok"`
	Schemas map[string]string `json:"schemas"`
}

// writeE2ECommandTree writes every public command path and flag, named as the
// behavior-witness guard names them, and reports whether it did so.
func writeE2ECommandTree(root *cobra.Command) bool {
	path := os.Getenv(e2eCommandTreeEnv)
	if path == "" {
		return false
	}
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var tree e2eCommandTree
	collect := func(owner string, flags *pflag.FlagSet) {
		flags.VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden && flag.Name != "help" {
				tree.Flags = append(tree.Flags, owner+" --"+flag.Name)
			}
		})
	}
	collect("root", root.PersistentFlags())
	collect("root", root.LocalFlags())
	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			if cmd.Hidden {
				continue
			}
			path := e2eCommandPath(root, cmd)
			tree.Commands = append(tree.Commands, path)
			collect(path, cmd.LocalFlags())
			walk(cmd)
		}
	}
	walk(root)
	slices.Sort(tree.Commands)
	slices.Sort(tree.Flags)
	tree.Flags = slices.Compact(tree.Flags)
	data, err := json.Marshal(tree)
	if err == nil {
		err = os.WriteFile(path, data, 0o644)
	}
	if err != nil {
		os.Stderr.WriteString("write E2E command tree: " + err.Error() + "\n")
		os.Exit(1)
	}
	return true
}

// e2eConfigSchemas reports the apiVersion of each configuration layer as the
// command starts, before it can migrate or create any of them.
func e2eConfigSchemas() map[string]string {
	if os.Getenv(e2eInvocationDirEnv) == "" {
		return nil
	}
	peek := func(path string) string {
		data, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return "absent"
		case err != nil:
			return "unreadable"
		}
		if version := config.PeekAPIVersion(data); version != "" {
			return strings.TrimPrefix(version, "agent-marketplace/")
		}
		return "unversioned"
	}
	schemas := map[string]string{"user": "absent", "repo": "absent", "local": "absent", "org": peek(config.OrganizationConfigPath())}
	if path, err := config.GlobalConfigPath(); err == nil {
		schemas["user"] = peek(path)
	}
	if repoRoot := config.FindRepoRoot(); !config.IsHomeDir(repoRoot) {
		schemas["repo"] = peek(config.ProjectConfigPath(repoRoot))
		schemas["local"] = peek(config.LocalConfigPath(config.FindLocalConfigDir()))
	}
	return schemas
}

// recordE2EInvocation writes one file per invocation so parallel E2E
// processes never share a file.
func recordE2EInvocation(root, cmd *cobra.Command, schemas map[string]string, err error) {
	dir := os.Getenv(e2eInvocationDirEnv)
	if dir == "" || cmd == nil {
		return
	}
	invocation := e2eInvocation{Command: e2eCommandPath(root, cmd), Flags: []string{}, OK: err == nil, Schemas: schemas}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		owner := cmd
		for owner != nil && owner.LocalFlags().Lookup(flag.Name) == nil {
			owner = owner.Parent()
		}
		if owner == nil {
			return
		}
		invocation.Flags = append(invocation.Flags, e2eCommandPath(root, owner)+" --"+flag.Name)
	})
	data, marshalErr := json.Marshal(invocation)
	if marshalErr != nil {
		return
	}
	file, createErr := os.CreateTemp(dir, "invocation-*.json")
	if createErr != nil {
		return
	}
	_, _ = file.Write(data)
	_ = file.Close()
}

func e2eCommandPath(root, cmd *cobra.Command) string {
	if cmd == root {
		return "root"
	}
	return strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
}
