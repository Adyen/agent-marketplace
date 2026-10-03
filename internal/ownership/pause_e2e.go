//go:build e2e

package ownership

import (
	"errors"
	"io"
	"os"
)

func init() {
	path := os.Getenv("AGENT_MARKETPLACE_E2E_OWNERSHIP_PAUSE")
	if path != "" {
		defaultCrasher = func() Crasher { return &ownershipPause{path: path} }
	}
}

type ownershipPause struct {
	path  string
	fired bool
}

func (p *ownershipPause) Crash(point CrashPoint) error {
	if p.fired || point != CrashBeforeBackup {
		return nil
	}
	p.fired = true
	file, err := os.Open(p.path)
	if err != nil {
		return err
	}
	var signal [1]byte
	_, err = io.ReadFull(file, signal[:])
	return errors.Join(err, file.Close())
}
