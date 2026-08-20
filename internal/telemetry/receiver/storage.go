package receiver

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	recordsDirPerm = 0o700
	recordFilePerm = 0o600
)

// storage persists accepted telemetry requests as JSON-lines, one file per
// calendar day, with owner-only permissions.
type storage struct {
	dir string
	mu  sync.Mutex
}

// requestRecord is the minimal shape persisted for each accepted OTLP metrics
// request. It preserves enough structure for dashboards and retention audits
// without storing full protobuf internals.
type requestRecord struct {
	ReceivedAt     time.Time        `json:"received_at"`
	InstallationID string           `json:"installation_id"`
	SchemaVersion  string           `json:"schema_version"`
	Commands       map[string]int64 `json:"commands,omitempty"`
	Harnesses      map[string]int64 `json:"harnesses,omitempty"`
	MetricCount    int64            `json:"metric_count"`
}

func newStorage(dir string) *storage {
	return &storage{dir: dir}
}

func (s *storage) ensureDir() error {
	if err := os.MkdirAll(s.dir, recordsDirPerm); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	// Ignored on filesystems without a Unix permission model.
	_ = os.Chmod(s.dir, recordsDirPerm)
	return nil
}

func (s *storage) append(record *requestRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return err
	}

	path := filepath.Join(s.dir, record.ReceivedAt.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, recordFilePerm)
	if err != nil {
		return fmt.Errorf("open record file: %w", err)
	}

	enc := json.NewEncoder(f)
	if err := enc.Encode(record); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode record: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync record file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close record file: %w", err)
	}
	return nil
}

func (s *storage) size() (int64, error) {
	var total int64
	err := filepath.WalkDir(s.dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}
