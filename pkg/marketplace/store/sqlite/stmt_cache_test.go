package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func openStmtCacheStore(t *testing.T) *Store {
	t.Helper()
	s := New(filepath.Join(t.TempDir(), "metadata.db"))
	if err := s.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func stmtCacheRegistry(id string) store.Registry {
	now := time.Now().UTC()
	return store.Registry{ID: store.RegistryID(id), Slug: id, Ref: "main", Origins: []string{"https://" + id + ".example/repo.git"}, Acquisition: store.AcquisitionMirror, Listed: true, CreatedAt: now, UpdatedAt: now}
}

func (c *stmtCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.stmts)
}

func TestStoreReusesPreparedReadStatements(t *testing.T) {
	ctx := context.Background()
	s := openStmtCacheStore(t)
	filter := store.ResourceSearchFilter{Marketplace: "mp", Query: "skill"}
	if _, err := s.SearchResourceRevisions(ctx, filter); err != nil {
		t.Fatal(err)
	}
	prepared := s.stmts.size()
	if prepared == 0 {
		t.Fatal("search did not prepare its statement")
	}
	if _, err := s.SearchResourceRevisions(ctx, filter); err != nil {
		t.Fatal(err)
	}
	if got := s.stmts.size(); got != prepared {
		t.Fatalf("repeated search prepared %d more statements", got-prepared)
	}
}

func TestTransactionReadsSeeTheirWritesThroughCachedStatements(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := openStmtCacheStore(t)
	if _, err := s.GetRegistry(ctx, "warm"); err == nil {
		t.Fatal("unexpected registry before the test writes one")
	}
	prepared := s.stmts.size()
	err := s.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if err := tx.PutRegistry(ctx, stmtCacheRegistry("in-tx")); err != nil {
			return err
		}
		got, err := tx.GetRegistry(ctx, "in-tx")
		if err != nil || got.ID != "in-tx" {
			return fmt.Errorf("cached transaction read of its own write: %+v, %w", got, err)
		}
		listed, err := tx.ListRegistries(ctx)
		if err != nil || len(listed) != 1 || listed[0].ID != "in-tx" {
			return fmt.Errorf("uncached transaction read of its own write: %+v, %w", listed, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.stmts.size(); got != prepared {
		t.Fatalf("transaction prepared %d statements on the pool", got-prepared)
	}
}

func TestTransactionDoesNotDeadlockWithAConcurrentFirstPrepare(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := openStmtCacheStore(t)
	searched := make(chan error, 1)
	err := s.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if err := tx.PutRegistry(ctx, stmtCacheRegistry("held")); err != nil {
			return err
		}
		go func() {
			_, err := s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "mp", Kind: "skill"})
			searched <- err
		}()
		time.Sleep(100 * time.Millisecond)
		_, err := tx.GetRegistry(ctx, "held")
		return err
	})
	if err != nil {
		t.Fatalf("transaction with a concurrent first prepare: %v", err)
	}
	if err := <-searched; err != nil {
		t.Fatalf("concurrent search: %v", err)
	}
}

func TestStatementCacheRunsQueriesUnpreparedPastItsBound(t *testing.T) {
	ctx := context.Background()
	s := openStmtCacheStore(t)
	for i := 0; s.stmts.size() < maxCachedStatements; i++ {
		row := s.q.QueryRowContext(ctx, fmt.Sprintf("SELECT %d", i))
		var got int
		if err := row.Scan(&got); err != nil || got != i {
			t.Fatalf("SELECT %d = %d, %v", i, got, err)
		}
	}
	if err := s.PutRegistry(ctx, stmtCacheRegistry("after-bound")); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRegistry(ctx, "after-bound")
	if err != nil || got.ID != "after-bound" {
		t.Fatalf("read past the cache bound: %+v, %v", got, err)
	}
	if size := s.stmts.size(); size != maxCachedStatements {
		t.Fatalf("cache holds %d statements, want the bound %d", size, maxCachedStatements)
	}
}

func TestRequestShapedQueriesDoNotFillTheStatementCache(t *testing.T) {
	ctx := context.Background()
	s := openStmtCacheStore(t)
	if _, err := s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "mp", Registries: []string{"r0"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.catalogCoordinatesVisible(ctx, []store.ResourceCoordinate{{Marketplace: "mp", Registry: "r", Kind: "skill", Name: "n0"}}); err != nil {
		t.Fatal(err)
	}
	prepared := s.stmts.size()
	var registries []string
	var coordinates []store.ResourceCoordinate
	var collections []store.CatalogProjectionResource
	for i := range 300 {
		registries = append(registries, fmt.Sprintf("r%d", i))
		coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "r", Kind: "collection", Name: fmt.Sprintf("n%d", i)}
		coordinates = append(coordinates, coordinate)
		collection := store.CatalogProjectionResource{}
		collection.Coordinate = coordinate
		collection.ContentDigest = store.RevisionDigest(fmt.Sprintf("sha256:%064x", i))
		collections = append(collections, collection)
		if _, err := s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "mp", Registries: registries}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.catalogCoordinatesVisible(ctx, coordinates); err != nil {
			t.Fatal(err)
		}
		if err := s.loadCatalogCollections(ctx, collections); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.stmts.size(); got != prepared {
		t.Fatalf("request-shaped search, visibility and collection queries grew the cache from %d to %d statements", prepared, got)
	}
}

func TestClosedStatementCacheNeitherServesNorCaches(t *testing.T) {
	ctx := context.Background()
	s := openStmtCacheStore(t)
	if stmt := s.stmts.prepare(ctx, "SELECT 1"); stmt == nil {
		t.Fatal("open cache did not prepare")
	}
	if err := s.stmts.close(); err != nil {
		t.Fatal(err)
	}
	if err := s.stmts.close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if s.stmts.cached("SELECT 1") != nil || s.stmts.prepare(ctx, "SELECT 2") != nil || s.stmts.size() != 0 {
		t.Fatalf("closed cache served or cached a statement; size %d", s.stmts.size())
	}
}

func TestTransactionBindsOneStatementPerQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := openStmtCacheStore(t)
	if err := s.PutRegistry(ctx, stmtCacheRegistry("bound")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRegistry(ctx, "bound"); err != nil {
		t.Fatal(err)
	}
	err := s.withTx(ctx, func(tx *Store) error {
		for range 5 {
			if _, err := tx.GetRegistry(ctx, "bound"); err != nil {
				return err
			}
		}
		bound := tx.q.(cachedTx).bound
		bound.mu.Lock()
		defer bound.mu.Unlock()
		if len(bound.stmts) != 1 {
			return fmt.Errorf("transaction bound %d statements for one repeated query", len(bound.stmts))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
