package claudedelivery

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

const withdrawnStatusMessage = "Previously installed copies and stored version history are unaffected."

func StatusMessage(row store.ClaudeDeliveryPlugin) string {
	if row.State == store.ClaudeDeliveryStateRetired {
		return withdrawnStatusMessage
	}
	if row.PublishedVersionID != "" && strings.EqualFold(row.ContentScanStatus, "processing") {
		return "awaiting its content scan"
	}
	if row.ServedVersionPinned && row.PublishedVersionID != "" && row.ServedVersionID != "" &&
		row.PublishedVersionID != row.ServedVersionID {
		return "a different version is pinned and still served"
	}
	return ""
}
