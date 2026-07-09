package db

import "testing"

func TestPostgresDatabaseURLForSchemaUsesDirectNeonHost(t *testing.T) {
	input := "postgresql://user:pass@ep-example-pooler.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
	want := "postgresql://user:pass@ep-example.c-2.ap-southeast-1.aws.neon.tech/neondb?sslmode=require&channel_binding=require"

	if got := postgresDatabaseURLForSchema(input); got != want {
		t.Fatalf("postgresDatabaseURLForSchema() = %q, want %q", got, want)
	}
}

func TestPostgresDatabaseURLForSchemaKeepsNonNeonHost(t *testing.T) {
	input := "postgresql://user:pass@postgres.example.com:5432/synchub?sslmode=require"

	if got := postgresDatabaseURLForSchema(input); got != input {
		t.Fatalf("postgresDatabaseURLForSchema() = %q, want %q", got, input)
	}
}
