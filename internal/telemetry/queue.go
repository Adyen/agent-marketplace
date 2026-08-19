package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
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
	_ = os.MkdirAll(root, 0o700)
	return &Queue{
		root:     root,
		maxBytes: defaultMaxQueueBytes,
		maxAge:   defaultMaxQueueAge,
	}
}

// Push stores data under the subdir selected by route.Signal. The caller is
// responsible for serializing data; Queue treats it as opaque bytes.
func (q *Queue) Push(ctx context.Context, route Route, data []byte) error {
	if route.Signal != queueSubdirMetrics && route.Signal != queueSubdirTraces {
		return fmt.Errorf("unsupported queue signal %q", route.Signal)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.evictLocked(ctx, int64(len(data))); err != nil {
		return err
	}

	subdir := filepath.Join(q.root, route.Signal)
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		return fmt.Errorf("create queue subdir: %w", err)
	}

	name := newQueueFilename()
	path := filepath.Join(subdir, name)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write queue file: %w", err)
	}

	return os.Rename(tmp, path)
}

// Drain iterates queued items oldest-first, calls send with the raw file bytes,
// and deletes items that send returns successfully. A per-file rename to
// ".sending" prevents concurrent drainers from double-sending the same item.
func (q *Queue) Drain(ctx context.Context, send func(data []byte) error) error {
	q.mu.Lock()
	files, err := q.listFilesLocked()
	if err != nil {
		q.mu.Unlock()
		return err
	}
	sortFiles(files)
	q.mu.Unlock()

	for _, f := range files {
		if err := ctx.Err(); err != nil {
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
			_ = os.Rename(sending, f.path)
			q.mu.Unlock()
			continue
		}
		q.mu.Unlock()

		err = send(data)

		q.mu.Lock()
		if err != nil {
			_ = os.Rename(sending, f.path)
			q.mu.Unlock()
			return err
		}
		_ = os.Remove(sending)
		q.mu.Unlock()
	}

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
			_ = os.Remove(f.path)
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
