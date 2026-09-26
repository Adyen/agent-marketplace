package codemode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SessionEntry struct {
	Timestamp time.Time `json:"ts"`
	Session   string    `json:"session"`
	Event     string    `json:"event"`
	Ref       string    `json:"ref"`
	Revision  string    `json:"revision,omitempty"`
	Digest    string    `json:"digest,omitempty"`
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

func (l *SessionLog) Append(event, ref, revision, digest string) error {
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
	rec, _ := json.Marshal(SessionEntry{
		Timestamp: time.Now().UTC(), Session: l.session,
		Event: event, Ref: ref, Revision: revision, Digest: digest,
	})
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
	for i, line := range splitLines(string(data)) {
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

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
