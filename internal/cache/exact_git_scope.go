package cache

import (
	"context"
	"sync"
)

// exactGitVerificationScope remembers which exact snapshots and object stores
// have already been verified by the current command or TUI action. It is
// opt-in through the context: without a scope every open verifies as before.
// Only successful results are recorded, so a failed or republished snapshot is
// verified again before it is read.
type exactGitVerificationScope struct {
	mu        sync.Mutex
	snapshots map[string]struct{}
	stores    map[string]struct{}
}

type exactGitVerificationScopeContextKey struct{}

// WithExactGitVerificationScope attaches a fresh verification scope to ctx, or
// returns ctx unchanged when a scope is already present.
func WithExactGitVerificationScope(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(exactGitVerificationScopeContextKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, exactGitVerificationScopeContextKey{}, &exactGitVerificationScope{
		snapshots: make(map[string]struct{}),
		stores:    make(map[string]struct{}),
	})
}

// ExactGitVerificationScopePresent reports whether ctx carries a verification
// scope. Commands that must keep the strict per-open path, such as serve, run
// without one.
func ExactGitVerificationScopePresent(ctx context.Context) bool {
	return exactGitVerificationScopeFrom(ctx) != nil
}

func exactGitVerificationScopeFrom(ctx context.Context) *exactGitVerificationScope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(exactGitVerificationScopeContextKey{}).(*exactGitVerificationScope)
	return scope
}

func (s *exactGitVerificationScope) snapshotVerified(path string, oid GitOID) bool {
	key := path + "\x00" + oid.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.snapshots[key]
	return ok
}

func (s *exactGitVerificationScope) recordSnapshot(path string, oid GitOID) {
	key := path + "\x00" + oid.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[key] = struct{}{}
}

func (s *exactGitVerificationScope) dropSnapshot(path string, oid GitOID) {
	key := path + "\x00" + oid.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.snapshots, key)
}

func (s *exactGitVerificationScope) storeVerified(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.stores[path]
	return ok
}

func (s *exactGitVerificationScope) recordStore(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stores[path] = struct{}{}
}

func (s *exactGitVerificationScope) dropStore(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stores, path)
}
