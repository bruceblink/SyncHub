package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/bruceblink/SyncHub/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplyPostgresMigrationsCreatesConflictSchema(t *testing.T) {
	repo, pool := newPostgresMigrationTestRepository(t)
	ctx := context.Background()

	var tableExists bool
	if err := pool.QueryRow(ctx, `
		select exists(
			select 1
			from information_schema.tables
			where table_schema = current_schema()
				and table_name = 'sync_conflicts'
		)
	`).Scan(&tableExists); err != nil {
		t.Fatalf("check sync_conflicts table: %v", err)
	}
	if !tableExists {
		t.Fatal("sync_conflicts table was not created")
	}

	email := "postgres-conflict-" + uuid.NewString() + "@example.com"
	user, err := repo.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	localVersion := int64(1)
	remoteVersion := int64(2)
	created, err := repo.CreateSyncConflict(ctx, domain.SyncConflict{
		UserID:        user.ID,
		Path:          "/workspace/a.txt",
		LocalVersion:  &localVersion,
		RemoteVersion: &remoteVersion,
	})
	if err != nil {
		t.Fatalf("create sync conflict: %v", err)
	}
	conflicts, err := repo.ListSyncConflicts(ctx, user.ID, "", 100)
	if err != nil {
		t.Fatalf("list sync conflicts: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].ID != created.ID {
		t.Fatalf("conflicts = %#v, want created conflict", conflicts)
	}
	resolved, err := repo.UpdateSyncConflictResolution(ctx, user.ID, created.ID, domain.ConflictResolutionKeepBoth)
	if err != nil {
		t.Fatalf("resolve sync conflict: %v", err)
	}
	if resolved.Resolution != domain.ConflictResolutionKeepBoth || resolved.ResolvedAt == nil {
		t.Fatalf("resolved conflict = %#v, want keep_both with resolved_at", resolved)
	}
}

func TestPostgresListActivityAllowsEmptyFileFilter(t *testing.T) {
	repo, _ := newPostgresMigrationTestRepository(t)
	ctx := context.Background()
	user, err := repo.CreateUser(ctx, "postgres-activity-"+uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := repo.CreateDirectory(ctx, user.ID, "/docs", "docs", nil, nil); err != nil {
		t.Fatalf("create activity event: %v", err)
	}

	events, err := repo.ListActivity(ctx, user.ID, "", 0, 50)
	if err != nil {
		t.Fatalf("list activity without file filter: %v", err)
	}
	if len(events) != 1 || events[0].Path != "/docs" {
		t.Fatalf("activity events = %#v, want /docs create event", events)
	}
}

func newPostgresMigrationTestRepository(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "synchub_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, err := adminPool.Exec(ctx, "create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "drop schema if exists "+pgx.Identifier{schema}.Sanitize()+" cascade")
	})

	pool, err := Connect(ctx, dsn, schema)
	if err != nil {
		t.Fatalf("connect test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	var activeSchema string
	if err := pool.QueryRow(ctx, "select current_schema()").Scan(&activeSchema); err != nil {
		t.Fatalf("read active test schema: %v", err)
	}
	if activeSchema != schema {
		t.Fatalf("active test schema = %q, want %q", activeSchema, schema)
	}

	if err := ApplyPostgresMigrations(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return NewRepository(pool), pool
}
