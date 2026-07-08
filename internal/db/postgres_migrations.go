package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresMigrationLockKey int64 = 747986671891337

type postgresMigration struct {
	Name    string
	Version string
	SQL     string
}

func ApplyPostgresMigrations(ctx context.Context, pool *pgxpool.Pool, migrationFS fs.FS) error {
	if pool == nil {
		return errors.New("postgres pool is nil")
	}
	migrations, err := loadPostgresMigrations(migrationFS)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return nil
	}

	if _, err := pool.Exec(ctx, `
		create table if not exists schema_migrations (
			version text primary key,
			applied_at timestamptz not null default now()
		)
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	for _, migration := range migrations {
		applied, err := postgresMigrationApplied(ctx, pool, migration.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyPostgresMigration(ctx, pool, migration); err != nil {
			return err
		}
	}
	return nil
}

func loadPostgresMigrations(migrationFS fs.FS) ([]postgresMigration, error) {
	entries, err := fs.ReadDir(migrationFS, ".")
	if err != nil {
		return nil, fmt.Errorf("read postgres migrations: %w", err)
	}
	migrations := make([]postgresMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		raw, err := fs.ReadFile(migrationFS, name)
		if err != nil {
			return nil, fmt.Errorf("read postgres migration %s: %w", name, err)
		}
		migrations = append(migrations, postgresMigration{
			Name:    name,
			Version: strings.TrimSuffix(name, ".up.sql"),
			SQL:     string(raw),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Name < migrations[j].Name
	})
	return migrations, nil
}

type postgresMigrationConn interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func postgresMigrationApplied(ctx context.Context, conn postgresMigrationConn, version string) (bool, error) {
	var applied bool
	if err := conn.QueryRow(ctx, `
		select exists(
			select 1
			from schema_migrations
			where version = $1
		)
	`, version).Scan(&applied); err != nil {
		return false, fmt.Errorf("check postgres migration %s: %w", version, err)
	}
	return applied, nil
}

func applyPostgresMigration(ctx context.Context, conn postgresMigrationConn, migration postgresMigration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin postgres migration %s: %w", migration.Version, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, postgresMigrationLockKey); err != nil {
		return fmt.Errorf("lock postgres migration %s: %w", migration.Version, err)
	}
	applied, err := postgresMigrationApplied(ctx, tx, migration.Version)
	if err != nil {
		return err
	}
	if applied {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, migration.SQL); err != nil {
		return fmt.Errorf("apply postgres migration %s: %w", migration.Name, err)
	}
	if _, err := tx.Exec(ctx, `
		insert into schema_migrations (version)
		values ($1)
	`, migration.Version); err != nil {
		return fmt.Errorf("record postgres migration %s: %w", migration.Version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres migration %s: %w", migration.Version, err)
	}
	return nil
}
