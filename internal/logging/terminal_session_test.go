package logging

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// While the TUI owns the alternate screen, stderr IS the live terminal:
// any mirrored record would corrupt the frame. BeginTerminalSession must
// route records to the managed log file only, and restore stderr mirroring
// after the screen is handed back.
func TestTerminalSessionRoutesRecordsAwayFromLiveStderr(t *testing.T) {
	clearLoggingEnv(t)
	defer resetDefaultLogger()

	var stderr bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "agent-marketplace.log")
	_, closeLog, err := Init(Options{
		Debug:    true, // debug mirror on stderr is the risky configuration
		Command:  "agent-marketplace tui",
		Version:  "test-version",
		FilePath: logPath,
		Stderr:   &stderr,
	})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	slog.Debug("before-tui")
	restore := BeginTerminalSession()
	slog.Debug("during-tui")
	slog.Error("during-tui-error")
	restore()
	slog.Debug("after-tui")
	if err := closeLog(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	stderrOut := stderr.String()
	if !strings.Contains(stderrOut, "before-tui") || !strings.Contains(stderrOut, "after-tui") {
		t.Fatalf("stderr mirror lost records outside the terminal session: %q", stderrOut)
	}
	if strings.Contains(stderrOut, "during-tui") {
		t.Fatalf("terminal session leaked records onto the live screen: %q", stderrOut)
	}

	// The managed file kept everything, including session records.
	var messages []string
	for _, record := range readJSONRecords(t, logPath) {
		messages = append(messages, record["msg"].(string))
	}
	for _, want := range []string{"before-tui", "during-tui", "during-tui-error", "after-tui"} {
		found := false
		for _, got := range messages {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("managed log file is missing %q: %v", want, messages)
		}
	}
}

// With logging disabled, debug mode mirrors to stderr only — there is no
// managed file to fall back to. The session must still keep the live screen
// clean by discarding records for its duration.
func TestTerminalSessionWithoutManagedFileDiscardsStderrMirror(t *testing.T) {
	clearLoggingEnv(t)
	defer resetDefaultLogger()

	disabled := false
	var stderr bytes.Buffer
	_, closeLog, err := Init(Options{
		Debug:   true,
		Enabled: &disabled,
		Command: "agent-marketplace tui",
		Stderr:  &stderr,
	})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	slog.Debug("before-tui")
	restore := BeginTerminalSession()
	slog.Debug("during-tui")
	restore()
	slog.Debug("after-tui")
	if err := closeLog(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	stderrOut := stderr.String()
	if !strings.Contains(stderrOut, "before-tui") || !strings.Contains(stderrOut, "after-tui") {
		t.Fatalf("stderr mirror lost records outside the terminal session: %q", stderrOut)
	}
	if strings.Contains(stderrOut, "during-tui") {
		t.Fatalf("terminal session leaked records onto the live screen: %q", stderrOut)
	}
}

func TestTerminalSessionRestoreIsIndependent(t *testing.T) {
	clearLoggingEnv(t)
	defer resetDefaultLogger()

	var stderr bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "agent-marketplace.log")
	_, closeLog, err := Init(Options{Debug: true, FilePath: logPath, Stderr: &stderr})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := closeLog(); err != nil {
			t.Errorf("close log: %v", err)
		}
	})

	first := BeginTerminalSession()
	slog.Debug("session-one")
	// Starting a nested session and restoring it must not resurrect the
	// stderr mirror while the outer session is still active.
	second := BeginTerminalSession()
	second()
	slog.Debug("nested-restored")
	first()
	slog.Debug("outer-restored")

	if strings.Contains(stderr.String(), "nested-restored") {
		t.Fatalf("nested restore re-enabled the stderr mirror early: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "outer-restored") {
		t.Fatalf("outer restore did not re-enable the stderr mirror: %q", stderr.String())
	}
}
