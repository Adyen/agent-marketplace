package telemetry

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/redact"

	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	maxCapturedLogRecords   = 2000
	maxCapturedLogBytes     = 4 << 20
	maxCapturedMessageBytes = 8 << 10
	maxCapturedValueBytes   = 2 << 10
	maxCapturedAttrs        = 32
	truncatedSuffix         = "…[truncated]"
)

var capturedAttrKeyRE = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

// LogCapture is a slog handler that keeps the records of a diagnostic
// invocation in memory at debug level and above, regardless of the configured
// log level. Handlers derived with WithAttrs or WithGroup share one buffer.
type LogCapture struct {
	shared *captureBuffer
	prefix string
	attrs  []capturedAttr
}

// captureBuffer is the state shared by a LogCapture and its derived handlers.
type captureBuffer struct {
	mu        sync.Mutex
	records   []capturedLog
	bytes     int
	full      bool
	dropped   int
	discarded bool
}

type capturedLog struct {
	timestamp   time.Time
	level       slog.Level
	message     string
	attrs       []capturedAttr
	spanContext oteltrace.SpanContext
}

type capturedAttr struct {
	key   string
	value string
}

// NewLogCapture returns an empty capture that accepts records until Discard.
func NewLogCapture() *LogCapture {
	return &LogCapture{shared: &captureBuffer{}}
}

func (c *LogCapture) Enabled(_ context.Context, level slog.Level) bool {
	c.shared.mu.Lock()
	defer c.shared.mu.Unlock()
	return level >= slog.LevelDebug && !c.shared.discarded
}

func (c *LogCapture) Handle(ctx context.Context, record slog.Record) error {
	attrs := slices.Clone(c.attrs)
	record.Attrs(func(a slog.Attr) bool {
		attrs = appendFlattenedAttr(attrs, c.prefix, a)
		return true
	})
	c.shared.add(capturedLog{
		timestamp:   record.Time,
		level:       record.Level,
		message:     truncateUTF8(record.Message, maxCapturedMessageBytes),
		attrs:       boundedCapturedAttrs(attrs),
		spanContext: oteltrace.SpanContextFromContext(ctx),
	})
	return nil
}

func (c *LogCapture) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return c
	}
	next := *c
	next.attrs = slices.Clone(c.attrs)
	for _, a := range attrs {
		next.attrs = appendFlattenedAttr(next.attrs, c.prefix, a)
	}
	return &next
}

func (c *LogCapture) WithGroup(name string) slog.Handler {
	if name == "" {
		return c
	}
	next := *c
	next.prefix = joinCapturedKey(c.prefix, name)
	return &next
}

// Discard empties the capture and stops it from accepting records.
func (c *LogCapture) Discard() {
	c.shared.mu.Lock()
	defer c.shared.mu.Unlock()
	c.shared.discarded = true
	c.shared.records = nil
	c.shared.bytes = 0
	c.shared.full = false
	c.shared.dropped = 0
}

func (c *LogCapture) snapshot() ([]capturedLog, int) {
	c.shared.mu.Lock()
	defer c.shared.mu.Unlock()
	return slices.Clone(c.shared.records), c.shared.dropped
}

// add keeps entry while the records fit both the count and the byte budget. Once
// one record does not fit the byte budget, it and every later record are dropped,
// so the kept records are always a prefix of the emission order.
func (b *captureBuffer) add(entry capturedLog) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.discarded {
		return
	}
	size := entry.size()
	if b.full || len(b.records) >= maxCapturedLogRecords {
		b.dropped++
		return
	}
	if b.bytes+size > maxCapturedLogBytes {
		b.full = true
		b.dropped++
		return
	}
	b.bytes += size
	b.records = append(b.records, entry)
}

// size counts the bounded message, attribute key, and attribute value bytes.
func (e capturedLog) size() int {
	n := len(e.message)
	for _, a := range e.attrs {
		n += len(a.key) + len(a.value)
	}
	return n
}

func appendFlattenedAttr(dst []capturedAttr, prefix string, a slog.Attr) []capturedAttr {
	if a.Equal(slog.Attr{}) {
		return dst
	}
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		groupPrefix := joinCapturedKey(prefix, a.Key)
		for _, inner := range a.Value.Group() {
			dst = appendFlattenedAttr(dst, groupPrefix, inner)
		}
		return dst
	}
	return append(dst, capturedAttr{key: joinCapturedKey(prefix, a.Key), value: a.Value.String()})
}

func joinCapturedKey(prefix, key string) string {
	switch {
	case prefix == "":
		return key
	case key == "":
		return prefix
	default:
		return prefix + "." + key
	}
}

// boundedCapturedAttrs keeps the first valid attributes in emission order and
// truncates their values to the capture limit.
func boundedCapturedAttrs(attrs []capturedAttr) []capturedAttr {
	out := make([]capturedAttr, 0, min(len(attrs), maxCapturedAttrs))
	for _, a := range attrs {
		if len(out) == maxCapturedAttrs {
			break
		}
		if !capturedAttrKeyRE.MatchString(a.key) {
			continue
		}
		out = append(out, capturedAttr{key: a.key, value: truncateUTF8(a.value, maxCapturedValueBytes)})
	}
	return out
}

// truncateUTF8 returns s unchanged when it fits in limit bytes. Otherwise it
// cuts s on a rune boundary and appends truncatedSuffix so the result still
// fits in limit bytes.
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - len(truncatedSuffix)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncatedSuffix
}

// diagnosticLogs converts captured records into redacted payload entries.
// Every record carries traceID. Its span is the record's own span when that
// span belongs to traceID, and rootSpanID otherwise.
func diagnosticLogs(records []capturedLog, traceID, rootSpanID string) []DiagnosticLog {
	out := make([]DiagnosticLog, 0, len(records))
	for _, r := range records {
		spanID := rootSpanID
		if r.spanContext.IsValid() && r.spanContext.TraceID().String() == traceID {
			spanID = r.spanContext.SpanID().String()
		}
		entry := DiagnosticLog{
			Timestamp: r.timestamp,
			Severity:  logSeverityText(r.level),
			Message:   redact.URL(r.message),
			TraceID:   traceID,
			SpanID:    spanID,
		}
		if len(r.attrs) > 0 {
			entry.Attributes = make(map[string]string, len(r.attrs))
			for _, a := range r.attrs {
				entry.Attributes[a.key] = redact.URL(a.value)
			}
		}
		out = append(out, entry)
	}
	return out
}

func logSeverityText(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
