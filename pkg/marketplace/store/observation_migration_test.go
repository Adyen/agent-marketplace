package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type observationMigrationDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type observationMigrationFixture struct {
	meta          store.MetadataStore
	close         func() error
	open          func() error
	reset         func() error
	countVersions func() (int, error)
	registryHead  func() (string, error)
	tableRows     func() (int, error)
}

type observationMigrationState struct {
	registry store.Registry
	occ      store.RegistryOccurrence
	job      store.Job
	head     string
}

func TestObservationMigrationSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "marketplace.db")
	meta := sqlite.New(path)
	fixture := &observationMigrationFixture{meta: meta, close: meta.Close, open: func() error { return meta.Open(ctx) }}
	fixture.reset = func() error {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		for _, statement := range []string{
			`DROP TABLE IF EXISTS host_budget_leases`,
			`DROP TABLE IF EXISTS registry_observations`,
			`DELETE FROM marketplace_metadata_v2_migrations WHERE version=9`,
		} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}
	fixture.countVersions = func() (int, error) {
		var count int
		err := withSQLiteMigrationDB(ctx, path, func(db observationMigrationDB) error {
			return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM marketplace_metadata_v2_migrations`).Scan(&count)
		})
		return count, err
	}
	fixture.registryHead = func() (string, error) {
		var head string
		err := withSQLiteMigrationDB(ctx, path, func(db observationMigrationDB) error {
			return db.QueryRowContext(ctx, `SELECT revision_commit FROM registry_heads WHERE registry_id=?`, "migration-registry").Scan(&head)
		})
		return head, err
	}
	fixture.tableRows = func() (int, error) {
		var count int
		err := withSQLiteMigrationDB(ctx, path, func(db observationMigrationDB) error {
			return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM registry_observations`).Scan(&count)
		})
		return count, err
	}
	if err := fixture.open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.close() })
	runObservationMigrationCases(t, fixture)
}

func TestObservationMigrationPostgres(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	schema := testSchemaName(t)
	dropObservationSchema(t, ctx, dsn, schema)
	meta := postgres.NewWithSchema(dsn, schema)
	fixture := &observationMigrationFixture{meta: meta, close: meta.Close, open: func() error { return meta.Open(ctx) }}
	fixture.reset = func() error {
		return withPostgresMigrationDB(ctx, dsn, schema, func(db observationMigrationDB) error {
			for _, statement := range []string{
				`DROP TABLE IF EXISTS host_budget_leases`,
				`DROP TABLE IF EXISTS registry_observations`,
				`DELETE FROM marketplace_metadata_v2_schema_migrations WHERE version=9`,
			} {
				if _, err := db.ExecContext(ctx, statement); err != nil {
					return err
				}
			}
			return nil
		})
	}
	fixture.countVersions = func() (int, error) {
		var count int
		err := withPostgresMigrationDB(ctx, dsn, schema, func(db observationMigrationDB) error {
			return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM marketplace_metadata_v2_schema_migrations`).Scan(&count)
		})
		return count, err
	}
	fixture.registryHead = func() (string, error) {
		var head string
		err := withPostgresMigrationDB(ctx, dsn, schema, func(db observationMigrationDB) error {
			return db.QueryRowContext(ctx, `SELECT revision_commit FROM registry_heads WHERE registry_id=$1`, "migration-registry").Scan(&head)
		})
		return head, err
	}
	fixture.tableRows = func() (int, error) {
		var count int
		err := withPostgresMigrationDB(ctx, dsn, schema, func(db observationMigrationDB) error {
			return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM registry_observations`).Scan(&count)
		})
		return count, err
	}
	if err := fixture.open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = fixture.close()
		dropObservationSchema(t, ctx, dsn, schema)
	})
	runObservationMigrationCases(t, fixture)
}

func dropObservationSchema(t *testing.T, ctx context.Context, dsn, schema string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
		t.Fatal(err)
	}
}

func withSQLiteMigrationDB(ctx context.Context, path string, fn func(observationMigrationDB) error) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return fn(db)
}

func withPostgresMigrationDB(ctx context.Context, dsn, schema string, fn func(observationMigrationDB) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SET search_path TO `+pgx.Identifier{schema}.Sanitize()); err != nil {
		return err
	}
	return fn(conn)
}

func runObservationMigrationCases(t *testing.T, fixture *observationMigrationFixture) {
	t.Helper()
	before := seedObservationMigrationState(t, fixture)
	applied, err := fixture.countVersions()
	if err != nil {
		t.Fatal(err)
	}
	var observation store.RegistryObservation
	// spec: marketplace-registry-observation/existing-metadata-database-upgrades-without-data-loss
	t.Run("upgrade", func(t *testing.T) {
		if err := fixture.close(); err != nil {
			t.Fatal(err)
		}
		if err := fixture.reset(); err != nil {
			t.Fatal(err)
		}
		if err := fixture.open(); err != nil {
			t.Fatal(err)
		}
		assertObservationMigrationState(t, fixture, before)
		if rows, err := fixture.tableRows(); err != nil || rows != 0 {
			t.Fatalf("registry observations after upgrade = %d, want empty table: %v", rows, err)
		}
		if count, err := fixture.countVersions(); err != nil || count != applied {
			t.Fatalf("applied migration versions = %d, want %d: %v", count, applied, err)
		}
		created, err := fixture.meta.CreateRegistryObservation(context.Background(), store.RegistryObservation{RegistryID: before.registry.ID})
		if err != nil {
			t.Fatalf("create observation for upgraded registry: %v", err)
		}
		observation = created
	})
	// spec: marketplace-registry-observation/repeating-the-schema-upgrade-is-a-no-op
	t.Run("repeat", func(t *testing.T) {
		if err := fixture.close(); err != nil {
			t.Fatal(err)
		}
		if err := fixture.open(); err != nil {
			t.Fatal(err)
		}
		assertObservationMigrationState(t, fixture, before)
		got, err := fixture.meta.GetRegistryObservation(context.Background(), observation.RegistryID)
		if err != nil || !reflect.DeepEqual(got, observation) {
			t.Fatalf("observation after repeated upgrade = %+v, %v; want %+v", got, err, observation)
		}
		if rows, err := fixture.tableRows(); err != nil || rows != 1 {
			t.Fatalf("registry observations after repeated upgrade = %d, want 1: %v", rows, err)
		}
		if count, err := fixture.countVersions(); err != nil || count != applied {
			t.Fatalf("applied migration versions after reopening = %d, want %d: %v", count, applied, err)
		}
	})
}

func seedObservationMigrationState(t *testing.T, fixture *observationMigrationFixture) observationMigrationState {
	t.Helper()
	ctx := context.Background()
	meta := fixture.meta
	registry := store.Registry{ID: "migration-registry", Slug: "migration-registry", Ref: "main", Acquisition: store.AcquisitionIndexOnly, Listed: true}
	if err := meta.PutRegistry(ctx, registry); err != nil {
		t.Fatal(err)
	}
	occ := store.RegistryOccurrence{
		RegistryID: "migration-registry", Ref: "main", Roots: []store.RegistryRoot{{Path: "skills"}},
		Commit: "migration-commit", RegistryRevisionManifestDigest: "migration-manifest",
		Acquisition: store.AcquisitionIndexOnly,
	}
	if err := meta.PutRegistryOccurrence(ctx, occ); err != nil {
		t.Fatal(err)
	}
	job := store.Job{ID: "migration-job", Kind: "migration-test", Payload: []byte(`{"migration":true}`), MaxAttempts: 2}
	if err := meta.EnqueueJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	registry, err := meta.GetRegistry(ctx, registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	occ, err = meta.GetRegistryOccurrence(ctx, occ.RegistryID, occ.Commit)
	if err != nil {
		t.Fatal(err)
	}
	job, err = meta.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	head, err := fixture.registryHead()
	if err != nil {
		t.Fatal(err)
	}
	return observationMigrationState{registry: registry, occ: occ, job: job, head: head}
}

func assertObservationMigrationState(t *testing.T, fixture *observationMigrationFixture, before observationMigrationState) {
	t.Helper()
	ctx := context.Background()
	registry, err := fixture.meta.GetRegistry(ctx, before.registry.ID)
	if err != nil {
		t.Fatal(err)
	}
	occ, err := fixture.meta.GetRegistryOccurrence(ctx, before.occ.RegistryID, before.occ.Commit)
	if err != nil {
		t.Fatal(err)
	}
	job, err := fixture.meta.GetJob(ctx, before.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	head, err := fixture.registryHead()
	if err != nil {
		t.Fatal(err)
	}
	after := observationMigrationState{registry: registry, occ: occ, job: job, head: head}
	if !reflect.DeepEqual(before.registry, after.registry) || !reflect.DeepEqual(before.occ, after.occ) ||
		!reflect.DeepEqual(before.job, after.job) || after.head != before.head {
		t.Fatalf("migration changed existing metadata: before=%+v after=%+v", before, after)
	}
}
