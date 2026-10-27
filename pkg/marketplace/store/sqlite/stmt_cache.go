package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

// maxCachedStatements bounds the prepared read statements a store keeps. Query
// text whose shape depends on the request must not go through the cache (see
// [Store.uncached]); past the bound, further text runs unprepared.
const maxCachedStatements = 256

// stmtCache prepares each distinct read query once. SQLite plans a statement
// when it is prepared, and the public search and catalog queries join enough
// tables that planning costs milliseconds while executing them costs
// microseconds.
type stmtCache struct {
	db     *sql.DB
	mu     sync.Mutex
	stmts  map[string]*sql.Stmt
	closed bool
}

func newStmtCache(db *sql.DB) *stmtCache {
	return &stmtCache{db: db, stmts: make(map[string]*sql.Stmt)}
}

// cached returns the prepared statement for query, if one exists.
func (c *stmtCache) cached(query string) *sql.Stmt {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	return c.stmts[query]
}

// prepare returns the prepared statement for query, preparing it on the
// database when the cache has room. It returns nil when the query should run
// unprepared. It must not be called inside a transaction: the store's single
// pooled connection is held by the transaction, so preparing on the database
// would wait for it forever. The lock is never held while preparing, because
// preparing waits for that connection and a transaction holding it may need
// the lock to find its own statements.
func (c *stmtCache) prepare(ctx context.Context, query string) *sql.Stmt {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	stmt, ok := c.stmts[query]
	full := len(c.stmts) >= maxCachedStatements
	c.mu.Unlock()
	if ok {
		return stmt
	}
	if full {
		return nil
	}
	prepared, err := c.db.PrepareContext(ctx, query)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	stmt, ok = c.stmts[query]
	switch {
	case c.closed:
		stmt = nil
	case !ok && len(c.stmts) < maxCachedStatements:
		c.stmts[query] = prepared
		stmt, prepared = prepared, nil
	}
	c.mu.Unlock()
	if prepared != nil {
		_ = prepared.Close()
	}
	return stmt
}

func (c *stmtCache) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var errs []error
	for query, stmt := range c.stmts {
		errs = append(errs, stmt.Close())
		delete(c.stmts, query)
	}
	return errors.Join(errs...)
}

// cachedDB runs read queries on the database through prepared statements.
type cachedDB struct {
	*sql.DB
	cache *stmtCache
}

func (c cachedDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if stmt := c.cache.prepare(ctx, query); stmt != nil {
		return stmt.QueryContext(ctx, args...)
	}
	return c.DB.QueryContext(ctx, query, args...)
}

func (c cachedDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if stmt := c.cache.prepare(ctx, query); stmt != nil {
		return stmt.QueryRowContext(ctx, args...)
	}
	return c.DB.QueryRowContext(ctx, query, args...)
}

// cachedTx runs read queries inside a transaction through statements already
// prepared on the database, rebinding each to the transaction's connection once.
// It never prepares new statements on the database.
type cachedTx struct {
	*sql.Tx
	cache *stmtCache
	bound *txStmts
}

// txStmts holds the transaction-bound statements of one transaction, which
// close when it commits or rolls back.
type txStmts struct {
	mu    sync.Mutex
	stmts map[string]*sql.Stmt
}

func newCachedTx(tx *sql.Tx, cache *stmtCache) cachedTx {
	return cachedTx{Tx: tx, cache: cache, bound: &txStmts{stmts: make(map[string]*sql.Stmt)}}
}

func (c cachedTx) stmt(ctx context.Context, query string) *sql.Stmt {
	c.bound.mu.Lock()
	defer c.bound.mu.Unlock()
	if stmt, ok := c.bound.stmts[query]; ok {
		return stmt
	}
	parent := c.cache.cached(query)
	if parent == nil {
		return nil
	}
	stmt := c.StmtContext(ctx, parent)
	if ctx.Err() == nil {
		c.bound.stmts[query] = stmt
	}
	return stmt
}

func (c cachedTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if stmt := c.stmt(ctx, query); stmt != nil {
		return stmt.QueryContext(ctx, args...)
	}
	return c.Tx.QueryContext(ctx, query, args...)
}

func (c cachedTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if stmt := c.stmt(ctx, query); stmt != nil {
		return stmt.QueryRowContext(ctx, args...)
	}
	return c.Tx.QueryRowContext(ctx, query, args...)
}

// uncached returns the store's queries without the statement cache, for query
// text whose shape depends on the request, such as one condition per requested
// item. Caching such text would let requests fill the bounded cache.
func (s *Store) uncached() queryer {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}
