package receiver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		if id != "" {
			c.seen[id] = struct{}{}
		}
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

	path := filepath.Join(c.dir, budgetFileName)
	tmp := path + ".tmp"
	data, err := json.Marshal(budgetFile{InstallationIDs: ids})
	if err != nil {
		return fmt.Errorf("marshal cardinality budget file: %w", err)
	}
	if err := os.WriteFile(tmp, data, recordFilePerm); err != nil {
		return fmt.Errorf("write cardinality budget file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename cardinality budget file: %w", err)
	}
	return nil
}
