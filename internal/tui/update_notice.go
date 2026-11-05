package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Adyen/agent-marketplace/internal/updatecheck"
)

// upgradeNoticeWait bounds how long the notice command waits for its lookup.
const upgradeNoticeWait = 5 * time.Second

// upgradeNoticeDuration is how long the notice stays in the toast stack.
const upgradeNoticeDuration = 8 * time.Second

// WithUpgradeCheck shows a newer-release notice once as a bounded notification.
// The lookup runs as a command after the first frame, never in Init.
func WithUpgradeCheck(checker *updatecheck.Checker) Option {
	return func(model *Model) { model.upgradeNotice = upgradeNoticeCmd(checker) }
}

func upgradeNoticeCmd(checker *updatecheck.Checker) tea.Cmd {
	if checker == nil {
		return nil
	}
	return func() tea.Msg {
		text := checker.Start(context.Background()).Finish(upgradeNoticeWait)
		if text == "" {
			return nil
		}
		return toastMsg{text: text, duration: upgradeNoticeDuration}
	}
}
