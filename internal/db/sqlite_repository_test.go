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
