package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxQueueBytes = 5 * 1024 * 1024
	defaultMaxQueueAge   = 7 * 24 * time.Hour

	queueSubdirMetrics = "metrics"
	queueSubdirTraces  = "traces"
)

// Route identifies where a queued payload should be stored and, when drained,
// which route it originally belonged to. Name is "primary" or "community" and
// Signal is "metrics" or "traces".
type Route struct {
	Name   string
	Signal string
}

func (r Route) String() string { return r.Name + "/" + r.Signal }

// Envelope is the on-disk wrapper for a queued OTLP/HTTP payload. The Payload
// field holds a base64-encoded OTLP export request body. URL and Headers are
// preserved so the payload can be replayed to the same endpoint during drain.
type Envelope struct {
	Route   string              `json:"route"`
	Signal  string              `json:"signal"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Payload string              `json:"payload"`
	Created int64               `json:"created"`
}

// Queue is a bounded, durable queue of serialized OTLP export requests.
type Queue struct {
	root     string
	maxBytes int64
	maxAge   time.Duration
	mu       sync.Mutex
}

// NewQueue creates a durable queue rooted at root. The queue caps total disk
// use at 5 MiB and discards items older than 7 days, evicting oldest first.
func NewQueue(root string) *Queue {
	q := &Queue{
		root:     root,
		maxBytes: defaultMaxQueueBytes,
		maxAge:   defaultMaxQueueAge,
	}

	if root == "" {
		return q
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		slog.Error("failed to create queue root", "root", root, "error", err)
		return q
	}
	if err := os.Chmod(root, 0o700); err != nil {
		slog.Error("failed to chmod queue root", "root", root, "error", err)
	}

	q.recoverCrash()
	return q
}

// recoverCrash renames any stale *.sending files back to their base name and
// removes any *.tmp files left behind by a crashed process.
func (q *Queue) recoverCrash() {
	slog.Info("recovering queue from previous crashes", "root", q.root)

	err := filepath.WalkDir(q.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			slog.Debug("skipping unreadable queue entry during crash recovery", "path", path, "error", err)
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		base := filepath.Base(path)
		switch {
		case strings.HasSuffix(base, ".tmp"):
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				slog.Error("failed to remove stale tmp queue file", "path", path, "error", err)
			} else {
				slog.Debug("removed stale tmp queue file", "path", path)
			}
		case strings.HasSuffix(base, ".sending"):
			recovered := strings.TrimSuffix(base, ".sending")
			dest := filepath.Join(filepath.Dir(path), recovered)
			if _, err := os.Stat(dest); err == nil {
				if err := os.Remove(path); err != nil {
					slog.Error("failed to remove stale sending duplicate", "path", path, "error", err)
				}
			} else {
				if err := os.Rename(path, dest); err != nil {
					slog.Error("failed to recover sending file", "path", path, "dest", dest, "error", err)
				} else {
					slog.Debug("recovered sending file", "path", path, "dest", dest)
				}
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("queue crash recovery walk failed", "root", q.root, "error", err)
	}
}

// Push stores data under the subdir selected by route.Signal. The caller is
// responsible for serializing data; Queue treats it as opaque bytes.
func (q *Queue) Push(ctx context.Context, route Route, data []byte) error {
	if route.Signal != queueSubdirMetrics && route.Signal != queueSubdirTraces {
		slog.Error("unsupported queue signal", "route", route, "signal", route.Signal)
		return fmt.Errorf("unsupported queue signal %q", route.Signal)
	}

	slog.Debug("pushing queue payload", "route", route.String(), "size", len(data))

	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.evictLocked(ctx, int64(len(data))); err != nil {
		slog.Error("queue eviction failed", "route", route.String(), "error", err)
		return err
	}

	subdir := filepath.Join(q.root, route.Signal)
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		slog.Error("failed to create queue subdir", "subdir", subdir, "error", err)
		return fmt.Errorf("create queue subdir: %w", err)
	}
	if err := os.Chmod(subdir, 0o700); err != nil {
		slog.Warn("failed to chmod queue subdir", "subdir", subdir, "error", err)
	}

	name := newQueueFilename()
	path := filepath.Join(subdir, name)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Error("failed to write queue file", "path", tmp, "error", err)
		return fmt.Errorf("write queue file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		slog.Error("failed to rename queue file", "tmp", tmp, "path", path, "error", err)
		return fmt.Errorf("rename queue file: %w", err)
	}

	slog.Debug("queue payload stored", "path", path)
	return nil
}

// Drain iterates queued items oldest-first, calls send with the raw file bytes,
// and deletes items that send returns successfully. A per-file rename to
// ".sending" prevents concurrent drainers from double-sending the same item.
func (q *Queue) Drain(ctx context.Context, send func(ctx context.Context, data []byte) error) error {
	q.mu.Lock()
	files, err := q.listFilesLocked()
	if err != nil {
		q.mu.Unlock()
		slog.Error("failed to list queued files", "root", q.root, "error", err)
		return err
	}
	sortFiles(files)
	q.mu.Unlock()

	slog.Info("draining queue", "root", q.root, "count", len(files))

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			slog.Warn("drain context canceled", "root", q.root, "error", err)
			return err
		}

		q.mu.Lock()
		sending := f.path + ".sending"
		if err := os.Rename(f.path, sending); err != nil {
			// Already being processed or removed.
			q.mu.Unlock()
			continue
		}
		data, err := os.ReadFile(sending)
		if err != nil {
			slog.Error("failed to read sending file", "path", sending, "error", err)
			_ = os.Rename(sending, f.path)
			q.mu.Unlock()
			continue
		}
		q.mu.Unlock()

		err = send(ctx, data)

		q.mu.Lock()
		if err != nil {
			slog.Error("send queued payload failed", "path", f.path, "error", err)
			_ = os.Rename(sending, f.path)
			q.mu.Unlock()
			return err
		}
		_ = os.Remove(sending)
		q.mu.Unlock()
	}

	slog.Info("queue drained", "root", q.root)
	return nil
}

// newQueueFilename returns a durable, sortable file name using UTC seconds,
// nanoseconds, and a random suffix. Names sort oldest-first lexicographically.
func newQueueFilename() string {
	now := time.Now().UTC()
	random := make([]byte, 4)
	_, _ = rand.Read(random)
	return fmt.Sprintf("%d-%09d-%s.json", now.Unix(), now.Nanosecond(), hex.EncodeToString(random))
}

type queueFile struct {
	path    string
	modTime time.Time
	size    int64
}

func sortFiles(files []queueFile) {
	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.Before(files[j].modTime)
		}
		return files[i].path < files[j].path
	})
}

func (q *Queue) listFilesLocked() ([]queueFile, error) {
	var files []queueFile

	err := filepath.Walk(q.root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".sending") || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		files = append(files, queueFile{path: path, modTime: info.ModTime(), size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

func (q *Queue) evictLocked(ctx context.Context, incoming int64) error {
	if incoming > q.maxBytes {
		return fmt.Errorf("queue payload %d bytes exceeds max %d bytes", incoming, q.maxBytes)
	}

	files, err := q.listFilesLocked()
	if err != nil {
		return err
	}

	now := time.Now()
	var active []queueFile
	var total int64

	for _, f := range files {
		if now.Sub(f.modTime) > q.maxAge {
			if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
				slog.Error("failed to remove aged queue file", "path", f.path, "error", err)
			}
			continue
		}
		active = append(active, f)
		total += f.size
	}

	sortFiles(active)

	for total+incoming > q.maxBytes && len(active) > 0 {
		oldest := active[0]
		active = active[1:]
		if err := os.Remove(oldest.path); err != nil && !os.IsNotExist(err) {
			slog.Error("failed to evict queue file", "path", oldest.path, "error", err)
			return err
		}
		total -= oldest.size
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return nil
}

// Size returns the current total size of queued items in bytes.
func (q *Queue) Size() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()

	files, err := q.listFilesLocked()
	if err != nil {
		return -1
	}

	var total int64
	for _, f := range files {
		total += f.size
	}
	return total
}

// Count returns the current number of queued items.
func (q *Queue) Count() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	files, err := q.listFilesLocked()
	if err != nil {
		return -1
	}
	return len(files)
}

// OldestAge returns the age of the oldest queued item and a boolean that is
// false when the queue is empty.
func (q *Queue) OldestAge() (time.Duration, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	files, err := q.listFilesLocked()
	if err != nil || len(files) == 0 {
		return 0, false
	}

	sortFiles(files)
	oldest := files[0].modTime
	return time.Since(oldest), true
}
