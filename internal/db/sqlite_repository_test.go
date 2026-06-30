package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
)

func TestSQLiteExpireUploadSessions(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "cleanup@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now().UTC()
	expired := createUploadSession(t, repo, user.ID, "/expired.txt", domain.UploadStatusPending, now.Add(-time.Hour))
	future := createUploadSession(t, repo, user.ID, "/future.txt", domain.UploadStatusPending, now.Add(time.Hour))
	committed := createUploadSession(t, repo, user.ID, "/committed.txt", domain.UploadStatusCommitted, now.Add(-time.Hour))

	count, err := repo.ExpireUploadSessions(ctx, now, 100)
	if err != nil {
		t.Fatalf("expire upload sessions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expired count = %d, want 1", count)
	}

	assertUploadStatus(t, repo, user.ID, expired.ID, domain.UploadStatusExpired)
	assertUploadStatus(t, repo, user.ID, future.ID, domain.UploadStatusPending)
	assertUploadStatus(t, repo, user.ID, committed.ID, domain.UploadStatusCommitted)
}

func TestSQLiteSchemaIncludesSyncConflicts(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	rows, err := repo.db.QueryContext(ctx, `pragma table_info(sync_conflicts)`)
	if err != nil {
		t.Fatalf("pragma sync_conflicts: %v", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for _, name := range []string{"id", "user_id", "file_id", "path", "local_version", "remote_version", "resolution", "created_at", "resolved_at"} {
		if !columns[name] {
			t.Fatalf("sync_conflicts missing column %s", name)
		}
	}

	var indexName string
	err = repo.db.QueryRowContext(ctx, `
		select name
		from sqlite_master
		where type = 'index' and name = 'sync_conflicts_user_resolution_idx'
	`).Scan(&indexName)
	if err != nil {
		t.Fatalf("sync conflict resolution index missing: %v", err)
	}
}

func createUploadSession(t *testing.T, repo *SQLiteRepository, userID, targetPath, status string, expiresAt time.Time) domain.UploadSession {
	t.Helper()
	session, err := repo.CreateUploadSession(context.Background(), domain.UploadSession{
		UserID:     userID,
		TargetPath: targetPath,
		TotalSize:  1,
		ChunkSize:  1,
		SHA256:     "sha",
		Status:     status,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		t.Fatalf("create upload session %s: %v", targetPath, err)
	}
	return session
}

func assertUploadStatus(t *testing.T, repo *SQLiteRepository, userID, uploadID, want string) {
	t.Helper()
	session, err := repo.GetUploadSession(context.Background(), userID, uploadID)
	if err != nil {
		t.Fatalf("get upload session %s: %v", uploadID, err)
	}
	if session.Status != want {
		t.Fatalf("upload session %s status = %q, want %q", uploadID, session.Status, want)
	}
}
