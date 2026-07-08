//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := flag.String("database-url", "", "PostgreSQL connection string")
	schema := flag.String("schema", "", "schema name")
	action := flag.String("action", "", "create or drop")
	flag.Parse()

	if *databaseURL == "" {
		exitf("database-url is required")
	}
	if *schema == "" {
		exitf("schema is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		exitf("connect postgres: %v", err)
	}
	defer pool.Close()

	quotedSchema := pgx.Identifier{*schema}.Sanitize()
	switch *action {
	case "create":
		_, err = pool.Exec(ctx, "create schema "+quotedSchema)
	case "drop":
		_, err = pool.Exec(ctx, "drop schema if exists "+quotedSchema+" cascade")
	default:
		exitf("action must be create or drop: %q", *action)
	}
	if err != nil {
		exitf("%s schema %s: %v", *action, *schema, err)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
