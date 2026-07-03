package db

import (
	"context"
	"fmt"
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

func TestSQLiteExpiredUploadChunkRepository(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "chunk-cleanup@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now().UTC()
	expired := createUploadSession(t, repo, user.ID, "/expired-chunks.txt", domain.UploadStatusExpired, now.Add(-time.Hour))
	pending := createUploadSession(t, repo, user.ID, "/pending-chunks.txt", domain.UploadStatusPending, now.Add(time.Hour))
	expiredChunk, err := repo.PutUploadChunk(ctx, expired.ID, 0, 4, "sha-expired", "staging/user/expired/0")
	if err != nil {
		t.Fatalf("put expired chunk: %v", err)
	}
	if _, err := repo.PutUploadChunk(ctx, pending.ID, 0, 4, "sha-pending", "staging/user/pending/0"); err != nil {
		t.Fatalf("put pending chunk: %v", err)
	}

	chunks, err := repo.ListExpiredUploadChunks(ctx, 100)
	if err != nil {
		t.Fatalf("list expired upload chunks: %v", err)
	}
	if len(chunks) != 1 || chunks[0].ID != expiredChunk.ID || chunks[0].StorageKey != expiredChunk.StorageKey {
		t.Fatalf("expired chunks = %#v, want expired chunk %#v", chunks, expiredChunk)
	}
	if err := repo.DeleteUploadChunk(ctx, expiredChunk.ID); err != nil {
		t.Fatalf("delete upload chunk: %v", err)
	}
	chunks, err = repo.ListExpiredUploadChunks(ctx, 100)
	if err != nil {
		t.Fatalf("list expired upload chunks after delete: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expired chunks after delete = %#v, want none", chunks)
	}
	pendingChunks, err := repo.ListUploadChunks(ctx, pending.ID)
	if err != nil {
		t.Fatalf("list pending chunks: %v", err)
	}
	if len(pendingChunks) != 1 {
		t.Fatalf("pending chunks = %#v, want one", pendingChunks)
	}
}

func TestSQLiteDeleteExpiredFileVersionsKeepsCurrentPinnedAndRecent(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "version-cleanup@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now().UTC()
	fileID := "file-version-cleanup"
	_, err = repo.db.ExecContext(ctx, `
		insert into file_nodes (id, user_id, name, path, node_type, size, sha256, storage_key, version, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, fileID, user.ID, "cleanup.txt", "/cleanup.txt", domain.NodeTypeFile, 5, "sha5", "objects/user/sha5", 5, now.Add(-6*time.Hour), now)
	if err != nil {
		t.Fatalf("insert file node: %v", err)
	}
	for version := int64(1); version <= 5; version++ {
		var pinnedAt *time.Time
		if version == 2 {
			pinned := now.Add(-5 * time.Hour)
			pinnedAt = &pinned
		}
		_, err = repo.db.ExecContext(ctx, `
			insert into file_versions (id, file_id, user_id, version, size, sha256, storage_key, pinned_at, created_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, fmt.Sprintf("version-%d", version), fileID, user.ID, version, version, fmt.Sprintf("sha%d", version), fmt.Sprintf("objects/user/sha%d", version), pinnedAt, now.Add(time.Duration(-version)*time.Hour))
		if err != nil {
			t.Fatalf("insert file version %d: %v", version, err)
		}
	}
	_, err = repo.db.ExecContext(ctx, `
		update file_nodes
		set current_version_id = ?, storage_key = ?, sha256 = ?
		where id = ?
	`, "version-5", "objects/user/sha5", "sha5", fileID)
	if err != nil {
		t.Fatalf("set current version: %v", err)
	}

	deleted, err := repo.DeleteExpiredFileVersions(ctx, now.Add(-30*time.Minute), 2, 100)
	if err != nil {
		t.Fatalf("delete expired file versions: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted versions = %d, want 2", deleted)
	}

	versions, err := repo.ListFileVersions(ctx, user.ID, fileID, 10)
	if err != nil {
		t.Fatalf("list file versions: %v", err)
	}
	got := make([]int64, 0, len(versions))
	for _, version := range versions {
		got = append(got, version.Version)
	}
	want := []int64{5, 4, 2}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("remaining versions = %v, want %v", got, want)
	}
	if versions[0].PinnedAt != nil {
		t.Fatalf("current version unexpectedly pinned: %#v", versions[0])
	}
	if versions[2].Version != 2 || versions[2].PinnedAt == nil {
		t.Fatalf("pinned version was not preserved: %#v", versions[2])
	}
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

func TestSQLiteSyncConflictRepository(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "conflict@example.com", "hash")
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
	if created.ID == "" || created.Resolution != domain.ConflictResolutionPending {
		t.Fatalf("unexpected created conflict: %#v", created)
	}
	if created.LocalVersion == nil || *created.LocalVersion != localVersion {
		t.Fatalf("local version = %#v", created.LocalVersion)
	}
	if created.RemoteVersion == nil || *created.RemoteVersion != remoteVersion {
		t.Fatalf("remote version = %#v", created.RemoteVersion)
	}

	conflicts, err := repo.ListSyncConflicts(ctx, user.ID, "", 100)
	if err != nil {
		t.Fatalf("list pending sync conflicts: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].ID != created.ID {
		t.Fatalf("pending conflicts = %#v, want created conflict", conflicts)
	}

	resolved, err := repo.ListSyncConflicts(ctx, user.ID, domain.ConflictResolutionKeepBoth, 100)
	if err != nil {
		t.Fatalf("list keep-both sync conflicts: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("keep-both conflicts = %#v, want none", resolved)
	}

	updated, err := repo.UpdateSyncConflictResolution(ctx, user.ID, created.ID, domain.ConflictResolutionKeepBoth)
	if err != nil {
		t.Fatalf("update sync conflict resolution: %v", err)
	}
	if updated.Resolution != domain.ConflictResolutionKeepBoth || updated.ResolvedAt == nil {
		t.Fatalf("updated conflict = %#v, want keep_both with resolved_at", updated)
	}
	conflicts, err = repo.ListSyncConflicts(ctx, user.ID, "", 100)
	if err != nil {
		t.Fatalf("list pending sync conflicts after resolve: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("pending conflicts after resolve = %#v, want none", conflicts)
	}
	resolved, err = repo.ListSyncConflicts(ctx, user.ID, domain.ConflictResolutionKeepBoth, 100)
	if err != nil {
		t.Fatalf("list keep-both sync conflicts after resolve: %v", err)
	}
	if len(resolved) != 1 || resolved[0].ID != created.ID {
		t.Fatalf("keep-both conflicts after resolve = %#v, want updated conflict", resolved)
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
