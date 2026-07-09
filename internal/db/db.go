package db

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL, schema string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, nil
	}
	schema = strings.TrimSpace(schema)
	if schema != "" {
		// Schema-scoped connections must use session state; Neon pooler URLs do not retain it reliably.
		databaseURL = postgresDatabaseURLForSchema(databaseURL)
	}
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

func postgresDatabaseURLForSchema(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return databaseURL
	}
	if parsed.Host == "" {
		return databaseURL
	}
	host := parsed.Hostname()
	directHost := directNeonHost(host)
	if directHost == host {
		return databaseURL
	}
	if port := parsed.Port(); port != "" {
		parsed.Host = net.JoinHostPort(directHost, port)
	} else {
		parsed.Host = directHost
	}
	return parsed.String()
}

func directNeonHost(host string) string {
	if !strings.HasSuffix(host, ".neon.tech") {
		return host
	}
	return strings.Replace(host, "-pooler.", ".", 1)
}
