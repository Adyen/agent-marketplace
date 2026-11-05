package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/updatecheck"
)

// upgradeNoticeWait bounds how long the process waits for a lookup after the command finishes.
const upgradeNoticeWait = time.Second

var (
	upgradeNoticeEndpoint = updatecheck.DefaultEndpoint
	upgradeNotice         *updatecheck.Run
)

// newUpgradeChecker returns the release checker for this process, or nil when
// the environment or configuration opts out or the state root is unavailable.
func newUpgradeChecker() *updatecheck.Checker {
	if !updatecheck.Enabled(os.Getenv) {
		return nil
	}
	enabled, err := config.UpdateCheckEnabled()
	if err != nil {
		slog.Debug("upgrade check disabled", "error", err)
		return nil
	}
	if !enabled {
		return nil
	}
	stateRoot, err := ownership.StateRoot()
	if err != nil {
		slog.Debug("upgrade check disabled", "error", err)
		return nil
	}
	checker := updatecheck.New(version, filepath.Join(stateRoot, updatecheck.StateFileName))
	checker.Endpoint = upgradeNoticeEndpoint
	return checker
}

// startUpgradeNotice begins the release lookup for cmd when the invocation
// has a terminal standard error and is not an excluded command.
func startUpgradeNotice(cmd *cobra.Command, rootTUI, showVersion bool) {
	upgradeNotice = nil
	if rootTUI || showVersion || !isTerminal(os.Stderr.Fd()) || upgradeNoticeCommandExcluded(cmd) {
		return
	}
	checker := newUpgradeChecker()
	if checker == nil {
		return
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	upgradeNotice = checker.Start(ctx)
}

// finishUpgradeNotice waits briefly for the lookup and prints its notice, if any.
func finishUpgradeNotice(w io.Writer) {
	if msg := upgradeNotice.Finish(upgradeNoticeWait); msg != "" {
		_, _ = fmt.Fprintln(w, msg)
	}
}

func upgradeNoticeCommandExcluded(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		name := c.Name()
		if name == "serve" || name == "tui" || name == "completion" || strings.HasPrefix(name, "__complete") {
			return true
		}
	}
	return false
}
