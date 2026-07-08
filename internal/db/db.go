package db

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL, schema string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, nil
	}
	schema = strings.TrimSpace(schema)
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if schema != "" {
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = make(map[string]string)
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "set search_path to "+pgx.Identifier{schema}.Sanitize()+", public")
			return err
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
