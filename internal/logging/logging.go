package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Options controls operational logging initialization. The pointer fields
// (Enabled, MaxSizeMB, MaxBackups, MaxAgeDays, Compress) distinguish "unset"
// (nil) from an explicit value so that, for example, MaxBackups=0 ("keep all
// backups" in lumberjack) is honored instead of being treated as unset.
type Options struct {
	Debug      bool
	Command    string
	Version    string
	FilePath   string
	Enabled    *bool
	Level      string
	MaxSizeMB  *int
	MaxBackups *int
	MaxAgeDays *int
	Compress   *bool
	Stderr     io.Writer
}

// Init configures the process-wide slog default logger. File output uses JSON
// lines and debug mode also mirrors readable text records to stderr.
func Init(opts Options) (*slog.Logger, func() error, error) {
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	level := resolveLevel(opts)
	if !resolveEnabled(opts) {
		recordManagedTerminalHandler(nil)
		logger := slog.New(disabledHandler(level, opts.Debug || envBool("AGENT_MARKETPLACE_DEBUG"), stderr)).With(baseAttrs(opts)...)
		slog.SetDefault(logger)
		return logger, func() error { return nil }, nil
	}

	path := resolvePath(opts.FilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		wrapped := fmt.Errorf("initialize log directory %q: %w", filepath.Dir(path), err)
		_, writeErr := fmt.Fprintf(stderr, "agent-marketplace: %v; using stderr fallback\n", wrapped)
		logger := fallbackLogger(stderr, level, opts)
		recordManagedTerminalHandler(nil)
		slog.SetDefault(logger)
		return logger, func() error { return nil }, errors.Join(wrapped, writeErr)
	}
	if err := verifyWritable(path); err != nil {
		_, writeErr := fmt.Fprintf(stderr, "agent-marketplace: %v; using stderr fallback\n", err)
		logger := fallbackLogger(stderr, level, opts)
		recordManagedTerminalHandler(nil)
		slog.SetDefault(logger)
		return logger, func() error { return nil }, errors.Join(err, writeErr)
	}

	writer := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    resolveInt("AGENT_MARKETPLACE_LOG_MAX_SIZE_MB", opts.MaxSizeMB, config.DefaultLogMaxSizeMB, false),
		MaxBackups: resolveInt("AGENT_MARKETPLACE_LOG_MAX_BACKUPS", opts.MaxBackups, config.DefaultLogMaxBackups, true),
		MaxAge:     resolveInt("AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS", opts.MaxAgeDays, config.DefaultLogMaxAgeDays, true),
		Compress:   resolveCompress(opts),
	}

	handlers := []slog.Handler{
		slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}),
	}
	if opts.Debug || envBool("AGENT_MARKETPLACE_DEBUG") {
		handlers = append(handlers, slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	}

	recordManagedTerminalHandler(slog.New(handlers[0]).With(baseAttrs(opts)...).Handler())
	logger := slog.New(multiHandler{handlers: handlers}).With(baseAttrs(opts)...)
	slog.SetDefault(logger)
	return logger, writer.Close, nil
}

// terminalSession records the managed (non-stderr) handler from the most
// recent Init so a terminal session can keep logging to the managed file
// without touching the live screen.
var terminalSession struct {
	mu      sync.Mutex
	managed slog.Handler
}

func recordManagedTerminalHandler(handler slog.Handler) {
	terminalSession.mu.Lock()
	defer terminalSession.mu.Unlock()
	terminalSession.managed = handler
}

// BeginTerminalSession swaps the process-wide default logger so no record
// reaches stderr while a TUI owns the alternate screen: stderr IS the live
// terminal there, and mirrored log lines would corrupt the frame or inject
// untrusted bytes. Records keep flowing to the managed log file; when no
// managed file exists (logging disabled or unavailable), records are
// discarded for the duration of the session instead.
//
// The returned function restores the previous default logger. Restores are
// independent: nested sessions restore into their own predecessor.
func BeginTerminalSession() (restore func()) {
	previous := slog.Default()
	terminalSession.mu.Lock()
	managed := terminalSession.managed
	terminalSession.mu.Unlock()
	if managed == nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	} else {
		slog.SetDefault(slog.New(managed))
	}
	return func() { slog.SetDefault(previous) }
}

func fallbackLogger(stderr io.Writer, level slog.Level, opts Options) *slog.Logger {
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level})).With(baseAttrs(opts)...)
}

func disabledHandler(level slog.Level, debug bool, stderr io.Writer) slog.Handler {
	if debug {
		return slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level})
	}
	return slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: level})
}

func resolveEnabled(opts Options) bool {
	enabled := config.DefaultLogEnabled
	if opts.Enabled != nil {
		enabled = *opts.Enabled
	}
	if raw := strings.TrimSpace(os.Getenv("AGENT_MARKETPLACE_LOG_ENABLED")); raw != "" {
		if parsed, ok := parseBool(raw); ok {
			enabled = parsed
		}
	}
	return enabled
}

func resolveLevel(opts Options) slog.Level {
	level := parseLevel(opts.Level)
	if raw := strings.TrimSpace(os.Getenv("AGENT_MARKETPLACE_LOG_LEVEL")); raw != "" {
		level = parseLevel(raw)
	}
	if opts.Debug || envBool("AGENT_MARKETPLACE_DEBUG") {
		level = slog.LevelDebug
	}
	return level
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func resolvePath(path string) string {
	if envPath := strings.TrimSpace(os.Getenv("AGENT_MARKETPLACE_LOG_FILE")); envPath != "" {
		path = envPath
	}
	if strings.TrimSpace(path) == "" {
		return defaultLogPath()
	}
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			path = home
		}
	} else if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Clean(path)
}

// defaultLogPath returns the absolute default log file location under the
// user's home directory (~/.agent-marketplace/logs/agent-marketplace.log).
func defaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(config.DefaultLogDirName, config.DefaultLogSubdir, config.DefaultLogFileName)
	}
	return filepath.Join(home, config.DefaultLogDirName, config.DefaultLogSubdir, config.DefaultLogFileName)
}

// resolveInt resolves a rotation integer with precedence env > configured >
// fallback. When allowZero is true an explicit 0 is a valid value (lumberjack
// treats MaxBackups=0/MaxAge=0 as "keep everything"); when false, only
// positive values are accepted and 0/negative fall through to the fallback.
func resolveInt(env string, configured *int, fallback int, allowZero bool) int {
	accept := func(v int) bool { return v > 0 || (allowZero && v == 0) }

	value := fallback
	if configured != nil && accept(*configured) {
		value = *configured
	}
	if raw := strings.TrimSpace(os.Getenv(env)); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && accept(parsed) {
			value = parsed
		}
	}
	return value
}

func resolveCompress(opts Options) bool {
	compress := config.DefaultLogCompress
	if opts.Compress != nil {
		compress = *opts.Compress
	}
	if raw := strings.TrimSpace(os.Getenv("AGENT_MARKETPLACE_LOG_COMPRESS")); raw != "" {
		if parsed, ok := parseBool(raw); ok {
			compress = parsed
		}
	}
	return compress
}

func envBool(name string) bool {
	parsed, ok := parseBool(os.Getenv(name))
	return ok && parsed
}

func parseBool(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func verifyWritable(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", path, err)
	}
	return f.Close()
}

func baseAttrs(opts Options) []any {
	return []any{
		"run_id", runID(),
		"version", opts.Version,
		"command", opts.Command,
		"pid", os.Getpid(),
		"go_os", runtime.GOOS,
		"go_arch", runtime.GOARCH,
	}
}

func runID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

type multiHandler struct {
	handlers []slog.Handler
}

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		if err := handler.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		next = append(next, handler.WithAttrs(attrs))
	}
	return multiHandler{handlers: next}
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		next = append(next, handler.WithGroup(name))
	}
	return multiHandler{handlers: next}
}
