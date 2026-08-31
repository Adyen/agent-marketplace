package receiver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	recordsDirPerm      = 0o700
	recordFilePerm      = 0o600
	recordRetentionDays = 30
)

// storage persists accepted telemetry requests as JSON-lines, one file per
// calendar day, with owner-only permissions.
type storage struct {
	dir string
	mu  sync.Mutex
}

// requestRecord preserves validated aggregates needed to rebuild dashboard and
// Prometheus state without retaining unapproved OTLP fields.
type requestRecord struct {
	ReceivedAt     time.Time          `json:"received_at"`
	InstallationID string             `json:"installation_id"`
	SchemaVersion  string             `json:"schema_version"`
	Commands       map[string]int64   `json:"commands,omitempty"`
	Harnesses      map[string]int64   `json:"harnesses,omitempty"`
	MetricCount    int64              `json:"metric_count"`
	RequestID      string             `json:"request_id,omitempty"`
	PayloadSHA256  string             `json:"payload_sha256,omitempty"`
	Resource       *storedResource    `json:"resource,omitempty"`
	Invocations    []storedInvocation `json:"invocations,omitempty"`
	Durations      []storedDuration   `json:"durations,omitempty"`
}

type storedResource struct {
	CLIVersion string `json:"cli_version"`
	OSName     string `json:"os_name"`
	Arch       string `json:"arch"`
}

type storedInvocation struct {
	Arch        string `json:"arch"`
	CLIVersion  string `json:"cli_version"`
	CommandName string `json:"command_name"`
	Managed     string `json:"managed"`
	OSName      string `json:"os_name"`
	OutcomeCode string `json:"outcome_code"`
	Count       int64  `json:"count"`
}

type storedDuration struct {
	CommandName string   `json:"command_name"`
	OutcomeCode string   `json:"outcome_code"`
	Buckets     []uint64 `json:"buckets"`
	Count       uint64   `json:"count"`
	Sum         float64  `json:"sum"`
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
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode record: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(s.dir, record.ReceivedAt.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, recordFilePerm)
	if err != nil {
		return fmt.Errorf("open record file: %w", err)
	}
	start, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("seek record file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Truncate(start)
		_ = f.Close()
		return fmt.Errorf("write record: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Truncate(start)
		_ = f.Sync()
		_ = f.Close()
		return fmt.Errorf("sync record file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close record file: %w", err)
	}
	return nil
}

func (s *storage) load(consume func(*requestRecord) error) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read storage directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		if err := s.loadFile(path, consume); err != nil {
			return err
		}
	}
	return nil
}

func (s *storage) loadFile(path string, consume func(*requestRecord) error) error {
	f, err := os.OpenFile(path, os.O_RDWR, recordFilePerm)
	if err != nil {
		return fmt.Errorf("open record file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(f)
	var offset int64
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		complete := len(line) > 0 && line[len(line)-1] == '\n'
		var record requestRecord
		if err := json.Unmarshal(bytes.TrimSpace(line), &record); err != nil {
			if readErr == io.EOF && !complete {
				if err := f.Truncate(offset); err != nil {
					return fmt.Errorf("truncate torn record file %q: %w", path, err)
				}
				return f.Sync()
			}
			return fmt.Errorf("decode record file %q: %w", path, err)
		}
		if err := consume(&record); err != nil {
			return fmt.Errorf("restore record file %q: %w", path, err)
		}
		offset += int64(len(line))
		if readErr == io.EOF {
			if _, err := f.WriteAt([]byte{'\n'}, offset); err != nil {
				return fmt.Errorf("repair record newline %q: %w", path, err)
			}
			return f.Sync()
		}
		if readErr != nil {
			return fmt.Errorf("read record file %q: %w", path, readErr)
		}
	}
	return f.Close()
}

func retentionCutoff(now time.Time) time.Time {
	today := now.UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -(recordRetentionDays - 1))
}

func (s *storage) prune(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := retentionCutoff(now)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read storage directory: %w", err)
	}
	changed := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		date, err := time.Parse("2006-01-02.jsonl", entry.Name())
		if err != nil || !date.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil {
			return fmt.Errorf("remove expired record file %q: %w", entry.Name(), err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return syncDirectory(s.dir)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
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
