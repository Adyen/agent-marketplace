package sourcerevision

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

var errRevisionSessionClosed = errors.New("source revision session is closed")

type revisionSessionContextKey struct{}

type revisionSessionKey struct {
	root    string
	schema  string
	harness string
}

type revisionSessionEntry struct {
	source     *fsboundary.SourceFS
	checkpoint fsboundary.SourceCheckpoint
	result     Result
}

type revisionSession struct {
	mu      sync.RWMutex
	closed  bool
	entries map[revisionSessionKey]*revisionSessionEntry
}

func WithSession(ctx context.Context) (context.Context, func() error) {
	if ctx == nil {
		ctx = context.Background()
	}
	session := &revisionSession{entries: make(map[revisionSessionKey]*revisionSessionEntry)}
	return context.WithValue(ctx, revisionSessionContextKey{}, session), session.close
}

func revisionSessionFrom(ctx context.Context) *revisionSession {
	if ctx == nil {
		return nil
	}
	session, _ := ctx.Value(revisionSessionContextKey{}).(*revisionSession)
	return session
}

func (session *revisionSession) build(input Input) (Result, error) {
	session.mu.RLock()
	closed := session.closed
	session.mu.RUnlock()
	if closed {
		return Result{}, errRevisionSessionClosed
	}
	input, contract, root, err := prepareBuildInput(input)
	if err != nil {
		return Result{}, err
	}
	if input.RevisionSchema == "plugin-v1" {
		return buildPluginRevision(input, contract)
	}
	source, err := fsboundary.OpenSourceFS(root)
	if err != nil {
		return Result{}, fmt.Errorf("capture revision source: %w", err)
	}
	checkpoint, err := source.CaptureCheckpoint()
	if err != nil {
		return Result{}, errors.Join(err, source.Close())
	}
	built, err := buildCaptured(input, root, source, contract)
	if err != nil {
		return Result{}, errors.Join(err, source.Close())
	}
	if err := source.VerifyCheckpoint(checkpoint); err != nil {
		return Result{}, errors.Join(fmt.Errorf("%w: validate revision source generation: %w", ErrCatalog, err), source.Close())
	}
	key := revisionSessionKey{root: filepath.Clean(root), schema: contract.Schema, harness: built.SourceHarness}
	if err := session.store(key, &revisionSessionEntry{source: source, checkpoint: checkpoint, result: built}); err != nil {
		return Result{}, err
	}
	return built, nil
}

func (session *revisionSession) store(key revisionSessionKey, entry *revisionSessionEntry) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return errors.Join(errRevisionSessionClosed, entry.source.Close())
	}
	old := session.entries[key]
	session.entries[key] = entry
	if old != nil {
		return old.source.Close()
	}
	return nil
}

func (session *revisionSession) find(root, schema, harness string) (Result, bool, error) {
	key := revisionSessionKey{root: filepath.Clean(root), schema: schema, harness: harness}
	session.mu.RLock()
	defer session.mu.RUnlock()
	if session.closed {
		return Result{}, false, errRevisionSessionClosed
	}
	entry, ok := session.entries[key]
	if !ok {
		return Result{}, false, nil
	}
	if refusal := entry.source.Refusal(); refusal != nil {
		return Result{}, true, fmt.Errorf("%w: source refused: %w", ErrCatalog, refusal)
	}
	if err := entry.source.VerifyCheckpoint(entry.checkpoint); err != nil {
		return Result{}, true, err
	}
	return entry.result, true, nil
}

func (session *revisionSession) close() error {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil
	}
	session.closed = true
	entries := session.entries
	session.entries = nil
	session.mu.Unlock()
	var err error
	for _, entry := range entries {
		err = errors.Join(err, entry.source.Close())
	}
	return err
}
