package session

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Detail capture bounds: each captured source is byte-capped, and one
// snapshot captures a bounded total so hostile or corrupted sources cannot
// exhaust surface memory.
const (
	MaxDetailSourceBytes    = 256 << 10 // per entry
	MaxDetailCaptureBytes   = 2 << 20   // across one snapshot refresh
	MaxDetailCaptureEntries = 64        // across one snapshot refresh

	DetailTruncationMarker       = "[truncated: source exceeds 256 KiB; showing the first part only]"
	DetailBudgetTruncationMarker = "[truncated: snapshot detail capture exceeds 2048 KiB or 64 entries]"
)

// DetailSourceSnapshot is an immutable bounded copy of a resource source
// captured while the exact snapshot lease is held.
type DetailSourceSnapshot struct {
	Content          string
	TruncationMarker string
	JSON             bool
	Omitted          bool
	Err              error
}

// DetailCaptureBudget bounds the total bytes and entries captured across
// one snapshot refresh.
type DetailCaptureBudget struct {
	Bytes   int
	Entries int
}

func CaptureDetailSource(path string) *DetailSourceSnapshot {
	return captureDetailSourceLimit(path, MaxDetailSourceBytes, DetailTruncationMarker)
}

func (b *DetailCaptureBudget) Capture(path string) *DetailSourceSnapshot {
	if b.Entries >= MaxDetailCaptureEntries || b.Bytes >= MaxDetailCaptureBytes {
		return &DetailSourceSnapshot{JSON: IsJSONPath(path), Omitted: true}
	}
	remaining := MaxDetailCaptureBytes - b.Bytes
	limit := min(MaxDetailSourceBytes, remaining)
	marker := DetailTruncationMarker
	if limit < MaxDetailSourceBytes {
		marker = DetailBudgetTruncationMarker
	}
	snapshot := captureDetailSourceLimit(path, limit, marker)
	b.Entries++
	b.Bytes += len(snapshot.Content)
	return snapshot
}

func captureDetailSourceLimit(path string, limit int, marker string) *DetailSourceSnapshot {
	content, truncated, err := ReadDetailSourceLimit(path, limit)
	if !truncated {
		marker = ""
	}
	return &DetailSourceSnapshot{Content: content, TruncationMarker: marker, JSON: IsJSONPath(path), Err: err}
}

// attachDetailSources binds each row to its captured detail source copy.
func attachDetailSources(rows []Row, byPath map[string]*DetailSourceSnapshot) {
	for i := range rows {
		rows[i].DetailSource = byPath[rows[i].Artifact.Path]
	}
}

// ReadDetailSourceLimit reads a resource source file (SKILL.md inside a
// directory, or the file itself for non-skill kinds) with a hard byte cap.
// truncated is true when the file exceeds limit; content is cut at limit so a
// hostile or corrupted multi-gigabyte source cannot exhaust surface memory or
// render time.
func ReadDetailSourceLimit(path string, limit int) (content string, truncated bool, err error) {
	target := path
	if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() {
		target = filepath.Join(path, "SKILL.md")
	}
	f, err := os.OpenFile(target, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("source is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > limit {
		return string(data[:limit]), true, nil
	}
	return string(data), false, nil
}

// IsJSONPath returns true when the file extension suggests JSON content
// (e.g. hooks.json, mcp.json). This covers hooks and MCP server resources
// whose source files are fully JSON.
func IsJSONPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}
