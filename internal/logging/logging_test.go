package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: logging/default-json-output-carries-invocation-context
func TestInitWritesJSONAndGatesDebug(t *testing.T) {
	clearLoggingEnv(t)
	defer resetDefaultLogger()

	logPath := filepath.Join(t.TempDir(), "agent-marketplace.log")
	logger, closeLog, err := Init(Options{
		Command:  "test command",
		Version:  "test-version",
		FilePath: logPath,
	})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	logger.Debug("hidden")
	logger.Info("visible")
	if err := closeLog(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	records := readJSONRecords(t, logPath)
	if len(records) != 1 {
		t.Fatalf("expected one info record, got %d records: %#v", len(records), records)
	}
	if records[0]["msg"] != "visible" {
		t.Fatalf("expected visible message, got %#v", records[0]["msg"])
	}
	for _, key := range []string{"run_id", "version", "command", "pid", "go_os", "go_arch"} {
		if _, ok := records[0][key]; !ok {
			t.Fatalf("expected base attr %q in record %#v", key, records[0])
		}
	}
}

// spec: logging/debug-records-reach-file-and-standard-error
func TestInitDebugMirrorsToStderr(t *testing.T) {
	clearLoggingEnv(t)
	defer resetDefaultLogger()

	var stderr bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "agent-marketplace.log")
	logger, closeLog, err := Init(Options{
		Debug:    true,
		Command:  "debug command",
		Version:  "test-version",
		FilePath: logPath,
		Stderr:   &stderr,
	})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	logger.Debug("debug-visible")
	if err := closeLog(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	records := readJSONRecords(t, logPath)
	if len(records) != 1 || records[0]["msg"] != "debug-visible" {
		t.Fatalf("expected debug record in file, got %#v", records)
	}
	if !strings.Contains(stderr.String(), "debug-visible") {
		t.Fatalf("expected debug record on stderr, got %q", stderr.String())
	}
}

// spec: logging/environment-selects-the-log-file-and-debug-level
func TestInitEnvOverridesFileAndDebug(t *testing.T) {
	clearLoggingEnv(t)
	defer resetDefaultLogger()

	dir := t.TempDir()
	envPath := filepath.Join(dir, "env.log")
	optsPath := filepath.Join(dir, "opts.log")
	t.Setenv("AGENT_MARKETPLACE_LOG_FILE", envPath)
	t.Setenv("AGENT_MARKETPLACE_DEBUG", "1")

	logger, closeLog, err := Init(Options{FilePath: optsPath})
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	logger.Debug("from-env")
	if err := closeLog(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	if _, err := os.Stat(optsPath); !os.IsNotExist(err) {
		t.Fatalf("expected opts path to be unused, stat err %v", err)
	}
	records := readJSONRecords(t, envPath)
	if len(records) != 1 || records[0]["msg"] != "from-env" {
		t.Fatalf("expected env-path debug record, got %#v", records)
	}
}

// spec: logging/unwritable-destination-retains-fallback-output
func TestInitFallsBackWhenFileCannotBeOpened(t *testing.T) {
	clearLoggingEnv(t)
	defer resetDefaultLogger()

	dir := t.TempDir()
	notDir := filepath.Join(dir, "not-dir")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	var stderr bytes.Buffer
	logger, closeLog, err := Init(Options{
		FilePath: filepath.Join(notDir, "agent-marketplace.log"),
		Stderr:   &stderr,
	})
	if err == nil {
		t.Fatal("expected fallback error")
	}
	logger.Info("fallback-visible")
	if err := closeLog(); err != nil {
		t.Fatalf("close fallback logger: %v", err)
	}
	if !strings.Contains(stderr.String(), "fallback-visible") {
		t.Fatalf("expected fallback log on stderr, got %q", stderr.String())
	}
}

func TestResolveIntHonorsExplicitZero(t *testing.T) {
	clearLoggingEnv(t)
	zero := 0
	five := 5

	// max_backups/max_age_days allow 0 ("keep everything" in lumberjack).
	if got := resolveInt("AGENT_MARKETPLACE_LOG_MAX_BACKUPS", &zero, 5, true); got != 0 {
		t.Fatalf("expected explicit 0 to be honored, got %d", got)
	}
	// max_size_mb does not allow 0; it falls back to the default.
	if got := resolveInt("AGENT_MARKETPLACE_LOG_MAX_SIZE_MB", &zero, 10, false); got != 10 {
		t.Fatalf("expected 0 to fall back when zero disallowed, got %d", got)
	}
	// A positive configured value wins over the fallback.
	if got := resolveInt("AGENT_MARKETPLACE_LOG_MAX_BACKUPS", &five, 10, true); got != 5 {
		t.Fatalf("expected configured value 5, got %d", got)
	}
	// nil means unset → fallback.
	if got := resolveInt("AGENT_MARKETPLACE_LOG_MAX_BACKUPS", nil, 7, true); got != 7 {
		t.Fatalf("expected fallback 7 for nil, got %d", got)
	}
}

func TestResolveIntEnvHonorsZero(t *testing.T) {
	clearLoggingEnv(t)
	t.Setenv("AGENT_MARKETPLACE_LOG_MAX_BACKUPS", "0")
	if got := resolveInt("AGENT_MARKETPLACE_LOG_MAX_BACKUPS", nil, 5, true); got != 0 {
		t.Fatalf("expected env 0 to be honored, got %d", got)
	}
}

func readJSONRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var records []map[string]any
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("parse log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func clearLoggingEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AGENT_MARKETPLACE_DEBUG",
		"AGENT_MARKETPLACE_LOG_COMPRESS",
		"AGENT_MARKETPLACE_LOG_ENABLED",
		"AGENT_MARKETPLACE_LOG_FILE",
		"AGENT_MARKETPLACE_LOG_LEVEL",
		"AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS",
		"AGENT_MARKETPLACE_LOG_MAX_BACKUPS",
		"AGENT_MARKETPLACE_LOG_MAX_SIZE_MB",
	} {
		t.Setenv(key, "")
	}
}

func resetDefaultLogger() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}
