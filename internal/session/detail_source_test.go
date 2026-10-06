package session_test

import (
	"fmt"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/session"
)

// The truncation markers are literal constants; they must keep stating the
// capture limits they report.
func TestDetailTruncationMarkersMatchLimits(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{session.DetailTruncationMarker, fmt.Sprintf("[truncated: source exceeds %d KiB; showing the first part only]", session.MaxDetailSourceBytes/1024)},
		{session.DetailBudgetTruncationMarker, fmt.Sprintf("[truncated: snapshot detail capture exceeds %d KiB or %d entries]", session.MaxDetailCaptureBytes/1024, session.MaxDetailCaptureEntries)},
	} {
		if tc.got != tc.want {
			t.Errorf("marker = %q, want %q", tc.got, tc.want)
		}
	}
}
