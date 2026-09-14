package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/backup"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func validBackupTargets() []string {
	targets := make([]string, 0, len(harness.MakeInstallRoots()))
	for target := range harness.MakeInstallRoots() {
		targets = append(targets, string(target))
	}
	targets = append(targets, string(hcc.TargetCodex))
	sort.Strings(targets)
	return targets
}

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage backups of machine-scope harness directories",
	}

	cmd.AddCommand(
		newBackupCreateCmd(),
		newBackupRestoreCmd(),
		newBackupListCmd(),
	)

	return cmd
}

func newBackupCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Create an on-demand backup of machine-scope harness directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackupCreate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func runBackupCreate(ctx context.Context, out io.Writer, errOut io.Writer) error {
	repoRoot := config.FindRepoRoot()
	resolved := pipeline.Resolve(ctx, pipeline.ResolveOptions{RepoRoot: repoRoot})
	if len(resolved.Errors) > 0 {
		for _, e := range resolved.Errors {
			if _, err := fmt.Fprintf(errOut, "error: %v\n", e); err != nil {
				return fmt.Errorf("write stderr: %w", err)
			}
		}
		return fmt.Errorf("resolve failed")
	}

	backupCfg, err := resolved.MergedCfg.ToBackupConfig()
	if err != nil {
		return err
	}
	if !backupCfg.Enabled {
		if _, err := fmt.Fprintln(out, "Backup is disabled in config; skipping."); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	}

	managed := pipeline.CollectManagedPaths(resolved.Resolved, resolved.TargetHarnesses)
	codexPartitions, err := pipeline.CollectCodexBackupPartitions(resolved.Resolved)
	if err != nil {
		return fmt.Errorf("resolve Codex backup partitions: %w", err)
	}
	if len(managed) == 0 && len(codexPartitions) == 0 {
		if _, err := fmt.Fprintln(out, "No managed paths to back up."); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		return nil
	}

	bCfg := backup.Config{
		Enabled:   backupCfg.Enabled,
		MaxDays:   backupCfg.MaxDays,
		MaxPerDay: backupCfg.MaxPerDay,
	}
	if err := backup.Create(managed); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if len(codexPartitions) > 0 {
		if _, err := backup.CreateCodexSet(codexPartitions); err != nil {
			return fmt.Errorf("backup Codex set: %w", err)
		}
	}
	if err := backup.Rotate(bCfg); err != nil {
		return fmt.Errorf("backup rotate: %w", err)
	}

	targets := make([]string, 0, len(managed))
	for t := range managed {
		targets = append(targets, string(t))
	}
	if len(codexPartitions) > 0 {
		targets = append(targets, targetDisplayName(string(hcc.TargetCodex)))
	}
	sort.Strings(targets)
	if _, err := fmt.Fprintf(out, "Backup created for: %s\n", strings.Join(targets, ", ")); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func newBackupRestoreCmd() *cobra.Command {
	var target string

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Overlay the latest backup onto a harness target",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackupRestore(cmd.OutOrStdout(), target)
		},
	}

	cmd.Flags().StringVarP(&target, "target", "t", "", fmt.Sprintf("Harness target to restore (%s)", strings.Join(validBackupTargets(), ", ")))
	_ = cmd.MarkFlagRequired("target")

	return cmd
}

func newBackupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackupList(cmd.OutOrStdout())
		},
	}
}

func runBackupRestore(out io.Writer, target string) error {
	t := hcc.Target(strings.ToLower(target))
	if t == hcc.TargetCodex {
		partitions, err := pipeline.CodexBackupPartitions()
		if err != nil {
			return err
		}
		operation, err := backup.RestoreCodexSet(partitions)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "Overlay-restored %s from backup %s\n", targetDisplayName(string(t)), operation)
		return err
	}

	dstDir, ok := harness.MachineRoot(t)
	if !ok {
		return fmt.Errorf("unknown target %q; valid targets: %s", target, strings.Join(validBackupTargets(), ", "))
	}

	if info, err := os.Lstat(dstDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat target directory %s: %w", dstDir, err)
		}
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return fmt.Errorf("create target directory %s: %w", dstDir, err)
		}
	} else {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to restore through symlink %s", dstDir)
		}
		if !info.IsDir() {
			return fmt.Errorf("target directory %s is not a directory", dstDir)
		}
	}

	mappedFiles, err := harness.NativeAggregateBackupFiles(t)
	if err != nil {
		return err
	}
	if t == hcc.TargetDevin {
		ruleDir, err := harness.DevinMachineRuleDir()
		if err != nil {
			return err
		}
		if err := ensureRestoreDirectory(ruleDir); err != nil {
			return err
		}
		mappedFiles[harness.DevinMachineRuleArchivePrefix] = ruleDir
	}
	timestamp, err := backup.Restore(t, dstDir, mappedFiles)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "Overlay-restored %s from backup %s\n", target, timestamp)
	return err
}

func ensureRestoreDirectory(directory string) error {
	parent := filepath.Dir(directory)
	for _, candidate := range []string{parent, directory} {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			if err := os.Mkdir(candidate, 0o755); err != nil {
				return fmt.Errorf("create restore directory %s: %w", candidate, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("stat restore directory %s: %w", candidate, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing unsafe restore directory %s", candidate)
		}
	}
	return nil
}

func runBackupList(out io.Writer) error {
	dir, err := backup.Dir()
	if err != nil {
		return fmt.Errorf("backups directory: %w", err)
	}
	if _, err := fmt.Fprintf(out, "Backups directory: %s\n", dir); err != nil {
		return err
	}

	backups, err := backup.ListBackups()
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}

	if len(backups) == 0 {
		_, err := fmt.Fprintln(out, "No backups found.")
		return err
	}

	targets := make([]string, 0, len(backups))
	for target := range backups {
		targets = append(targets, string(target))
	}
	sort.Strings(targets)
	for _, target := range targets {
		timestamps := backups[hcc.Target(target)]
		if _, err := fmt.Fprintf(out, "\n%s (%d):\n", targetDisplayName(target), len(timestamps)); err != nil {
			return err
		}
		for _, ts := range timestamps {
			if _, err := fmt.Fprintf(out, "  %s\n", ts); err != nil {
				return err
			}
		}
	}

	return nil
}
