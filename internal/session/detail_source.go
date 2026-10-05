package session

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// Detail capture bounds: each captured source is byte-capped, and one
// snapshot captures a bounded total so hostile or corrupted sources cannot
// exhaust surface memory.
const (
	MaxDetailSourceBytes    = 256 << 10 // per entry
	MaxDetailCaptureBytes   = 2 << 20   // across one snapshot refresh
	MaxDetailCaptureEntries = 64        // across one snapshot refresh
)

var (
	DetailTruncationMarker       = fmt.Sprintf("[truncated: source exceeds %d KiB; showing the first part only]", MaxDetailSourceBytes/1024)
	DetailBudgetTruncationMarker = fmt.Sprintf("[truncated: snapshot detail capture exceeds %d KiB or %d entries]", MaxDetailCaptureBytes/1024, MaxDetailCaptureEntries)
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

// PreviewTargetRead reads a captured preview destination; tests inject it to
// change the target between capture and read.
type PreviewTargetRead func(*fsboundary.Destination, func(io.Reader) error) error

func CaptureDetailSource(path string, readTargetWith ...PreviewTargetRead) *DetailSourceSnapshot {
	return captureDetailSourceLimit(path, MaxDetailSourceBytes, DetailTruncationMarker, readTargetWith...)
}

func (b *DetailCaptureBudget) Capture(path string) *DetailSourceSnapshot {
	if b.Entries >= MaxDetailCaptureEntries || b.Bytes >= MaxDetailCaptureBytes {
		return &DetailSourceSnapshot{JSON: isJSONPath(path), Omitted: true}
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

func captureDetailSourceLimit(path string, limit int, marker string, readTargetWith ...PreviewTargetRead) *DetailSourceSnapshot {
	content, truncated, err := ReadDetailSourceLimit(path, limit, readTargetWith...)
	if !truncated {
		marker = ""
	}
	return &DetailSourceSnapshot{Content: content, TruncationMarker: marker, JSON: isJSONPath(path), Err: err}
}

// attachDetailSources binds each row to its captured detail source copy.
func attachDetailSources(rows []Row, byPath map[string]*DetailSourceSnapshot) {
	for i := range rows {
		rows[i].DetailSource = byPath[rows[i].Artifact.Path]
	}
}

// ReadDetailSourceLimit reads a resource source file, cut at limit bytes;
// truncated reports whether the file exceeded it.
func ReadDetailSourceLimit(path string, limit int, readTargetWith ...PreviewTargetRead) (content string, truncated bool, err error) {
	target := path
	if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() {
		target = filepath.Join(path, "SKILL.md")
	}
	data, err := ReadPreviewSource(filepath.Dir(target), target, limit, readTargetWith...)
	if err != nil {
		return "", false, err
	}
	if len(data) > limit {
		return string(data[:limit]), true, nil
	}
	return string(data), false, nil
}

// ReadPreviewSource reads at most limit+1 bytes of a regular, non-symlink file
// under rootPath through the repository link policy.
func ReadPreviewSource(rootPath, filePath string, limit int, readTargetWith ...PreviewTargetRead) (data []byte, err error) {
	if limit < 0 || int64(limit) == math.MaxInt64 {
		return nil, fsboundary.ErrTooLarge
	}
	rootPath, err = filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	filePath, err = filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		return nil, err
	}
	root, err := fsboundary.OpenDir(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	source, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if source.Link() != nil {
		return nil, fmt.Errorf("%w: source preview is a symlink", fsboundary.ErrUnsafePath)
	}
	readTarget := (*fsboundary.Destination).ReadTargetWith
	if len(readTargetWith) > 0 && readTargetWith[0] != nil {
		readTarget = readTargetWith[0]
	}
	err = readTarget(source, func(reader io.Reader) error {
		var readErr error
		data, readErr = io.ReadAll(io.LimitReader(reader, int64(limit)+1))
		return readErr
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// isJSONPath returns true when the file extension suggests JSON content
// (e.g. hooks.json, mcp.json). This covers hooks and MCP server resources
// whose source files are fully JSON.
func isJSONPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}
