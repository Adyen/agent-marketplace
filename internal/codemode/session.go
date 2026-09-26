package codemode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const marketplaceSourcePrefix = "marketplace:"

type SessionEntry struct {
	Timestamp  time.Time `json:"ts"`
	Session    string    `json:"session"`
	Event      string    `json:"event"`
	Ref        string    `json:"ref"`
	Revision   string    `json:"revision,omitempty"`
	Digest     string    `json:"digest,omitempty"`
	Source     string    `json:"source,omitempty"`
	SourcePath string    `json:"source_path,omitempty"`
}

func (e SessionEntry) MarketplaceProvenance() bool {
	return strings.HasPrefix(e.Source, marketplaceSourcePrefix)
}

type SessionLog struct {
	mu      sync.Mutex
	path    string
	session string
}

func NewSessionLog(stateRoot, session string) *SessionLog {
	if stateRoot == "" {
		stateRoot = defaultStateRoot()
	}
	return &SessionLog{
		path:    filepath.Join(stateRoot, "agent-marketplace", "mcp-sessions", session+".jsonl"),
		session: session,
	}
}

func defaultStateRoot() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state")
}

func (l *SessionLog) Path() string { return l.path }

func (l *SessionLog) Append(entry SessionEntry) error {
	if l == nil || l.path == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("session log dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("session log open: %w", err)
	}
	defer func() { _ = f.Close() }()
	entry.Timestamp = time.Now().UTC()
	entry.Session = l.session
	rec, _ := json.Marshal(entry)
	if _, err := f.Write(append(rec, '\n')); err != nil {
		return fmt.Errorf("session log write: %w", err)
	}
	return nil
}

func ReadSession(path string) ([]SessionEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []SessionEntry
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var e SessionEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("session log %s line %d: %w", path, i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}
