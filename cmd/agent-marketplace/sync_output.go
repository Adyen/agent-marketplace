package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

const (
	syncGreen  = "\x1b[32m"
	syncYellow = "\x1b[33m"
	syncRed    = "\x1b[31m"
	syncReset  = "\x1b[0m"
)

type syncOutputStyle struct {
	color bool
}

func newSyncOutputStyle(out io.Writer) syncOutputStyle {
	fd, ok := out.(interface{ Fd() uintptr })
	_, noColor := os.LookupEnv("NO_COLOR")
	return syncOutputStyle{
		color: ok && term.IsTerminal(fd.Fd()) && !noColor && os.Getenv("CLICOLOR") != "0" && os.Getenv("TERM") != "dumb",
	}
}

func (s syncOutputStyle) paint(color, text string) string {
	if !s.color {
		return text
	}
	return color + text + syncReset
}

func (s syncOutputStyle) warningPrefix() string {
	return s.paint(syncYellow, "warning:")
}

func (s syncOutputStyle) count(label string, count int, severity string) string {
	color := syncGreen
	if count > 0 {
		switch severity {
		case "warning":
			color = syncYellow
		case "error":
			color = syncRed
		}
	}
	return s.paint(color, fmt.Sprintf("%s %d", label, count))
}

func writeSyncSummary(out io.Writer, style syncOutputStyle, written, removed, malformed, errors int) error {
	segments := []string{
		style.count("Installed", written, ""),
		style.count("Removed", removed, ""),
		style.count("Malformed", malformed, "warning"),
		style.count("Errors", errors, "error"),
	}
	_, err := fmt.Fprintln(out, strings.Join(segments, "  "))
	return err
}
