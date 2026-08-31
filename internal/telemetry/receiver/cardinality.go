package receiver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const budgetFileName = "cardinality.json"

// budgetFile is the persisted form of the installation-id cardinality set.
type budgetFile struct {
	InstallationIDs []string `json:"installation_ids"`
}

// cardinality tracks the set of installation IDs the receiver has accepted and
// enforces a maximum budget. It persists the set to a JSON file in the storage
// directory so the budget survives restarts.
type cardinality struct {
	max  int
	dir  string
	mu   sync.RWMutex
	seen map[string]struct{}
}

func newCardinality(dir string, max int) *cardinality {
	return &cardinality{dir: dir, max: max, seen: make(map[string]struct{})}
}

func (c *cardinality) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := filepath.Join(c.dir, budgetFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cardinality budget file: %w", err)
	}

	var file budgetFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse cardinality budget file: %w", err)
	}
	for _, id := range file.InstallationIDs {
		if !installationIDPattern.MatchString(id) {
			return fmt.Errorf("invalid installation id in cardinality budget")
		}
		if _, duplicate := c.seen[id]; duplicate {
			return fmt.Errorf("duplicate installation id in cardinality budget")
		}
		c.seen[id] = struct{}{}
	}
	return nil
}

// Add records id as seen. It returns accepted=true when id is already known or
// there is room in the budget. If the budget is already at capacity and id is
// new, accepted=false is returned with no error.
func (c *cardinality) Add(id string) (accepted bool, err error) {
	if id == "" {
		return false, errors.New("empty installation id")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.seen[id]; ok {
		return true, nil
	}
	if len(c.seen) >= c.max {
		return false, nil
	}

	c.seen[id] = struct{}{}
	if err := c.saveLocked(); err != nil {
		delete(c.seen, id)
		return false, err
	}
	return true, nil
}

func (c *cardinality) Contains(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.seen[id]
	return ok
}

func (c *cardinality) Remove(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.seen[id]; !ok {
		return nil
	}
	delete(c.seen, id)
	if err := c.saveLocked(); err != nil {
		c.seen[id] = struct{}{}
		return err
	}
	return nil
}

func (c *cardinality) Replace(ids map[string]struct{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(ids) > c.max {
		return fmt.Errorf("cardinality budget contains %d installations, maximum is %d", len(ids), c.max)
	}
	previous := c.seen
	c.seen = make(map[string]struct{}, len(ids))
	for id := range ids {
		c.seen[id] = struct{}{}
	}
	if err := c.saveLocked(); err != nil {
		c.seen = previous
		return err
	}
	return nil
}

// Count returns the current number of unique installation IDs.
func (c *cardinality) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.seen)
}

func (c *cardinality) saveLocked() error {
	if err := os.MkdirAll(c.dir, recordsDirPerm); err != nil {
		return err
	}

	ids := make([]string, 0, len(c.seen))
	for id := range c.seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	path := filepath.Join(c.dir, budgetFileName)
	tmp := path + ".tmp"
	defer func() { _ = os.Remove(tmp) }()

	data, err := json.Marshal(budgetFile{InstallationIDs: ids})
	if err != nil {
		return fmt.Errorf("marshal cardinality budget file: %w", err)
	}
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, recordFilePerm)
	if err != nil {
		return fmt.Errorf("open cardinality budget file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write cardinality budget file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync cardinality budget file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close cardinality budget file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename cardinality budget file: %w", err)
	}
	dir, err := os.Open(c.dir)
	if err != nil {
		return fmt.Errorf("open cardinality directory: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync cardinality directory: %w", err)
	}
	return nil
}
