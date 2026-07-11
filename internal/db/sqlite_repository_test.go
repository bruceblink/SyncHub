package db

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
)

func TestSQLitePurgeExpiredDeletedFilesRemovesExpiredRootsAndTrees(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	user, err := repo.CreateUser(ctx, "trash-cleanup@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	old := time.Now().UTC().Add(-31 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-2 * 24 * time.Hour)
	if _, err = repo.db.ExecContext(ctx, `
		insert into file_nodes (id, user_id, parent_id, name, path, node_type, deleted_at) values
		('old-root', ?, null, 'old', '/old', 'directory', ?),
		('old-child', ?, 'old-root', 'child', '/old/child', 'directory', ?),
		('recent-root', ?, null, 'recent', '/recent', 'directory', ?)
	`, user.ID, old, user.ID, old, user.ID, recent); err != nil {
		t.Fatalf("insert deleted trees: %v", err)
	}

	count, err := repo.PurgeExpiredDeletedFiles(ctx, time.Now().UTC().Add(-30*24*time.Hour), 1)
	if err != nil || count != 1 {
		t.Fatalf("purge expired trash = %d, %v", count, err)
	}
	for _, id := range []string{"old-root", "old-child"} {
		var found int
		if err := repo.db.QueryRowContext(ctx, `select count(*) from file_nodes where id = ?`, id).Scan(&found); err != nil || found != 0 {
			t.Fatalf("purged node %s count = %d error = %v", id, found, err)
		}
	}
	var recentCount int
	if err := repo.db.QueryRowContext(ctx, `select count(*) from file_nodes where id = 'recent-root'`).Scan(&recentCount); err != nil || recentCount != 1 {
		t.Fatalf("recent node count = %d error = %v", recentCount, err)
	}
}

func TestSQLiteListDeletedFilesPaginatesByDeletedTimeAndID(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	user, err := repo.CreateUser(ctx, "trash-pages@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	newer := time.Now().UTC().Add(-time.Hour)
	older := newer.Add(-time.Hour)
	if _, err = repo.db.ExecContext(ctx, `
		insert into file_nodes (id, user_id, name, path, node_type, deleted_at) values
		('a-new', ?, 'a-new', '/a-new', 'directory', ?),
		('z-new', ?, 'z-new', '/z-new', 'directory', ?),
		('z-old', ?, 'z-old', '/z-old', 'directory', ?)
	`, user.ID, newer, user.ID, newer, user.ID, older); err != nil {
		t.Fatalf("insert deleted nodes: %v", err)
	}

	first, err := repo.ListDeletedFiles(ctx, user.ID, "", 1)
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != "z-new" || first.NextCursor != "z-new" {
		t.Fatalf("first page = %#v, error %v", first, err)
	}
	second, err := repo.ListDeletedFiles(ctx, user.ID, first.NextCursor, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != "a-new" || second.NextCursor != "a-new" {
		t.Fatalf("second page = %#v, error %v", second, err)
	}
	third, err := repo.ListDeletedFiles(ctx, user.ID, second.NextCursor, 1)
	if err != nil || len(third.Items) != 1 || third.Items[0].ID != "z-old" || third.NextCursor != "" {
		t.Fatalf("third page = %#v, error %v", third, err)
	}
}

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
	expired := createUploadSession(t, repo, user.ID, "/expired-chunks.txt", domain.UploadStatusPending, now.Add(-time.Hour))
	aborted := createUploadSession(t, repo, user.ID, "/aborted-chunks.txt", domain.UploadStatusPending, now.Add(time.Hour))
	pending := createUploadSession(t, repo, user.ID, "/pending-chunks.txt", domain.UploadStatusPending, now.Add(time.Hour))
	expiredChunk, err := repo.PutUploadChunk(ctx, expired.ID, 0, 4, "sha-expired", "staging/user/expired/0")
	if err != nil {
		t.Fatalf("put expired chunk: %v", err)
	}
	if _, err := repo.PutUploadChunk(ctx, pending.ID, 0, 4, "sha-pending", "staging/user/pending/0"); err != nil {
		t.Fatalf("put pending chunk: %v", err)
	}
	abortedChunk, err := repo.PutUploadChunk(ctx, aborted.ID, 0, 4, "sha-aborted", "staging/user/aborted/0")
	if err != nil {
		t.Fatalf("put aborted chunk: %v", err)
	}
	if _, err := repo.AbortUploadSession(ctx, user.ID, aborted.ID); err != nil {
		t.Fatalf("abort upload session: %v", err)
	}
	if count, err := repo.ExpireUploadSessions(ctx, now, 100); err != nil || count != 1 {
		t.Fatalf("expire upload sessions count = %d error = %v", count, err)
	}

	chunks, err := repo.ListExpiredUploadChunks(ctx, 100)
	if err != nil {
		t.Fatalf("list expired upload chunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expired chunks = %#v, want expired and aborted chunks", chunks)
	}
	chunkIDs := map[string]bool{chunks[0].ID: true, chunks[1].ID: true}
	if !chunkIDs[expiredChunk.ID] || !chunkIDs[abortedChunk.ID] {
		t.Fatalf("cleanup chunk ids = %#v, want %s and %s", chunkIDs, expiredChunk.ID, abortedChunk.ID)
	}
	if err := repo.DeleteUploadChunk(ctx, expiredChunk.ID); err != nil {
		t.Fatalf("delete upload chunk: %v", err)
	}
	if err := repo.DeleteUploadChunk(ctx, abortedChunk.ID); err != nil {
		t.Fatalf("delete aborted upload chunk: %v", err)
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

func TestSQLiteListDevices(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "devices@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	otherUser, err := repo.CreateUser(ctx, "other-devices@example.com", "hash")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	first, err := repo.CreateDevice(ctx, user.ID, "first", "windows")
	if err != nil {
		t.Fatalf("create first device: %v", err)
	}
	second, err := repo.CreateDevice(ctx, user.ID, "second", "linux")
	if err != nil {
		t.Fatalf("create second device: %v", err)
	}
	if _, err := repo.CreateDevice(ctx, otherUser.ID, "other", "linux"); err != nil {
		t.Fatalf("create other device: %v", err)
	}
	heartbeat, err := repo.HeartbeatDevice(ctx, user.ID, first.ID, "error", "connection timed out")
	if err != nil {
		t.Fatalf("heartbeat first device: %v", err)
	}
	if heartbeat.LastSyncAt == nil || heartbeat.LastSyncStatus == nil || *heartbeat.LastSyncStatus != "error" || heartbeat.LastSyncError == nil || *heartbeat.LastSyncError != "connection timed out" {
		t.Fatalf("failed sync heartbeat = %#v", heartbeat)
	}
	heartbeat, err = repo.HeartbeatDevice(ctx, user.ID, first.ID, "success", "ignored")
	if err != nil {
		t.Fatalf("successful heartbeat first device: %v", err)
	}
	if heartbeat.LastSyncStatus == nil || *heartbeat.LastSyncStatus != "success" || heartbeat.LastSyncError != nil {
		t.Fatalf("successful sync heartbeat = %#v", heartbeat)
	}
	heartbeat, err = repo.HeartbeatDevice(ctx, user.ID, first.ID, "", "")
	if err != nil {
		t.Fatalf("online heartbeat first device: %v", err)
	}
	if heartbeat.LastSyncStatus == nil || *heartbeat.LastSyncStatus != "success" || heartbeat.LastSyncError != nil {
		t.Fatalf("online heartbeat replaced sync result = %#v", heartbeat)
	}

	devices, err := repo.ListDevices(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices = %#v, want two", devices)
	}
	if devices[0].ID != first.ID || devices[0].LastSeenAt == nil {
		t.Fatalf("first listed device = %#v, want heartbeat device first", devices[0])
	}
	if devices[1].ID != second.ID {
		t.Fatalf("second listed device = %#v, want second device", devices[1])
	}
}

func TestSQLiteMoveDirectoryCascadesDescendantPaths(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "move-directory@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := repo.CreateDirectory(ctx, user.ID, "/workspace", "workspace", nil, nil)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	src, err := repo.CreateDirectory(ctx, user.ID, "/workspace/src", "src", &workspace.ID, nil)
	if err != nil {
		t.Fatalf("create src: %v", err)
	}
	pkgDir, err := repo.CreateDirectory(ctx, user.ID, "/workspace/src/pkg", "pkg", &src.ID, nil)
	if err != nil {
		t.Fatalf("create pkg: %v", err)
	}
	file := commitTestFile(t, repo, user.ID, "/workspace/src/pkg/main.go", "sha-main", "objects/user/main")
	archive, err := repo.CreateDirectory(ctx, user.ID, "/archive", "archive", nil, nil)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	deviceID := "dev-1"

	moved, err := repo.MoveFile(ctx, user.ID, src.ID, "/archive/src", "src", &archive.ID, nil, &deviceID)
	if err != nil {
		t.Fatalf("move directory: %v", err)
	}
	if moved.Path != "/archive/src" || moved.Version != 2 {
		t.Fatalf("moved directory = %#v", moved)
	}

	if _, err := repo.GetFileByPath(ctx, user.ID, "/workspace/src/pkg/main.go"); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("old child path error = %v, want not found", err)
	}
	movedPkg, err := repo.GetFileByID(ctx, user.ID, pkgDir.ID)
	if err != nil {
		t.Fatalf("get moved pkg by id: %v", err)
	}
	if movedPkg.Path != "/archive/src/pkg" {
		t.Fatalf("moved pkg path = %q", movedPkg.Path)
	}
	movedFile, err := repo.GetFileByID(ctx, user.ID, file.ID)
	if err != nil {
		t.Fatalf("get moved file by id: %v", err)
	}
	if movedFile.Path != "/archive/src/pkg/main.go" || movedFile.ParentID == nil || *movedFile.ParentID != pkgDir.ID {
		t.Fatalf("moved file = %#v", movedFile)
	}

	events, err := repo.ListChanges(ctx, user.ID, createTestDevice(t, repo, user.ID).ID, 0, 20)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	last := events[len(events)-1]
	if last.EventType != domain.EventMove || last.Path != "/archive/src" || last.OldPath == nil || *last.OldPath != "/workspace/src" {
		t.Fatalf("last change = %#v", last)
	}
	if last.SourceDeviceID == nil || *last.SourceDeviceID != deviceID {
		t.Fatalf("source device id = %#v, want %s", last.SourceDeviceID, deviceID)
	}
}

func TestSQLiteMoveDirectoryRejectsDescendantTarget(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "move-into-self@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := repo.CreateDirectory(ctx, user.ID, "/workspace", "workspace", nil, nil)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	src, err := repo.CreateDirectory(ctx, user.ID, "/workspace/src", "src", &workspace.ID, nil)
	if err != nil {
		t.Fatalf("create src: %v", err)
	}
	nested, err := repo.CreateDirectory(ctx, user.ID, "/workspace/src/nested", "nested", &src.ID, nil)
	if err != nil {
		t.Fatalf("create nested: %v", err)
	}

	_, err = repo.MoveFile(ctx, user.ID, src.ID, "/workspace/src/nested/src", "src", &nested.ID, nil, nil)
	if domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("move into descendant error = %v, want invalid argument", err)
	}
	node, err := repo.GetFileByID(ctx, user.ID, src.ID)
	if err != nil {
		t.Fatalf("get src: %v", err)
	}
	if node.Path != "/workspace/src" || node.Version != 1 {
		t.Fatalf("src after rejected move = %#v", node)
	}
}

func TestSQLiteMoveFileRejectsStaleBaseVersionAndRecordsConflict(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "stale-move@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := repo.CreateDirectory(ctx, user.ID, "/workspace", "workspace", nil, nil); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	file := commitTestFile(t, repo, user.ID, "/workspace/stale.txt", "sha-stale", "objects/user/stale")
	staleVersion := file.Version - 1

	_, err = repo.MoveFile(ctx, user.ID, file.ID, "/workspace/moved.txt", "moved.txt", nil, &staleVersion, nil)
	if domain.ErrorCodeOf(err) != domain.CodeFileConflict {
		t.Fatalf("move stale version error = %v, want file conflict", err)
	}
	current, err := repo.GetFileByID(ctx, user.ID, file.ID)
	if err != nil {
		t.Fatalf("get current file: %v", err)
	}
	if current.Path != "/workspace/stale.txt" || current.Version != file.Version {
		t.Fatalf("file changed after rejected move: %#v", current)
	}
	conflicts, err := repo.ListSyncConflicts(ctx, user.ID, domain.ConflictResolutionPending, 10)
	if err != nil {
		t.Fatalf("list conflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %#v, want one", conflicts)
	}
	if conflicts[0].Path != file.Path || conflicts[0].FileID == nil || *conflicts[0].FileID != file.ID {
		t.Fatalf("conflict target = %#v", conflicts[0])
	}
	if conflicts[0].LocalVersion == nil || *conflicts[0].LocalVersion != staleVersion {
		t.Fatalf("conflict local version = %#v", conflicts[0].LocalVersion)
	}
	if conflicts[0].RemoteVersion == nil || *conflicts[0].RemoteVersion != file.Version {
		t.Fatalf("conflict remote version = %#v", conflicts[0].RemoteVersion)
	}
}

func TestSQLiteDeleteDirectoryCascadesDescendants(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "delete-directory@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := repo.CreateDirectory(ctx, user.ID, "/workspace", "workspace", nil, nil)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	src, err := repo.CreateDirectory(ctx, user.ID, "/workspace/src", "src", &workspace.ID, nil)
	if err != nil {
		t.Fatalf("create src: %v", err)
	}
	file := commitTestFile(t, repo, user.ID, "/workspace/src/main.go", "sha-delete", "objects/user/delete")
	deviceID := "dev-1"

	if err := repo.DeleteFile(ctx, user.ID, src.ID, nil, &deviceID); err != nil {
		t.Fatalf("delete directory: %v", err)
	}
	if _, err := repo.GetFileByID(ctx, user.ID, src.ID); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("deleted directory error = %v, want not found", err)
	}
	if _, err := repo.GetFileByID(ctx, user.ID, file.ID); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("deleted child file error = %v, want not found", err)
	}
	children, err := repo.ListFiles(ctx, user.ID, &workspace.ID, "", 10)
	if err != nil {
		t.Fatalf("list workspace children: %v", err)
	}
	if len(children.Items) != 0 {
		t.Fatalf("workspace children after delete = %#v, want none", children.Items)
	}

	events, err := repo.ListChanges(ctx, user.ID, createTestDevice(t, repo, user.ID).ID, 0, 20)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	last := events[len(events)-1]
	if last.EventType != domain.EventDelete || last.Path != "/workspace/src" || last.Version == nil || *last.Version != 2 {
		t.Fatalf("last change = %#v", last)
	}
	if last.SourceDeviceID == nil || *last.SourceDeviceID != deviceID {
		t.Fatalf("source device id = %#v, want %s", last.SourceDeviceID, deviceID)
	}
}

func TestSQLiteDeleteFileRejectsStaleBaseVersionAndRecordsConflict(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "stale-delete@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := repo.CreateDirectory(ctx, user.ID, "/workspace", "workspace", nil, nil); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	file := commitTestFile(t, repo, user.ID, "/workspace/delete-stale.txt", "sha-delete-stale", "objects/user/delete-stale")
	staleVersion := file.Version - 1

	err = repo.DeleteFile(ctx, user.ID, file.ID, &staleVersion, nil)
	if domain.ErrorCodeOf(err) != domain.CodeFileConflict {
		t.Fatalf("delete stale version error = %v, want file conflict", err)
	}
	current, err := repo.GetFileByID(ctx, user.ID, file.ID)
	if err != nil {
		t.Fatalf("get current file: %v", err)
	}
	if current.DeletedAt != nil || current.Version != file.Version {
		t.Fatalf("file changed after rejected delete: %#v", current)
	}
	conflicts, err := repo.ListSyncConflicts(ctx, user.ID, domain.ConflictResolutionPending, 10)
	if err != nil {
		t.Fatalf("list conflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %#v, want one", conflicts)
	}
	if conflicts[0].Path != file.Path || conflicts[0].FileID == nil || *conflicts[0].FileID != file.ID {
		t.Fatalf("conflict target = %#v", conflicts[0])
	}
	if conflicts[0].LocalVersion == nil || *conflicts[0].LocalVersion != staleVersion {
		t.Fatalf("conflict local version = %#v", conflicts[0].LocalVersion)
	}
	if conflicts[0].RemoteVersion == nil || *conflicts[0].RemoteVersion != file.Version {
		t.Fatalf("conflict remote version = %#v", conflicts[0].RemoteVersion)
	}
}

func TestSQLiteListFilesCursorPagination(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	user, err := repo.CreateUser(ctx, "list-pagination@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace, err := repo.CreateDirectory(ctx, user.ID, "/workspace", "workspace", nil, nil)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	docs, err := repo.CreateDirectory(ctx, user.ID, "/workspace/docs", "docs", &workspace.ID, nil)
	if err != nil {
		t.Fatalf("create docs: %v", err)
	}
	file := commitTestFile(t, repo, user.ID, "/workspace/readme.txt", "sha-readme", "objects/user/readme")

	first, err := repo.ListFiles(ctx, user.ID, &workspace.ID, "", 1)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != docs.ID || first.NextCursor != docs.ID {
		t.Fatalf("first page = %#v, want docs with next cursor", first)
	}

	second, err := repo.ListFiles(ctx, user.ID, &workspace.ID, first.NextCursor, 1)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != file.ID || second.NextCursor != "" {
		t.Fatalf("second page = %#v, want file without next cursor", second)
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

func commitTestFile(t *testing.T, repo *SQLiteRepository, userID, targetPath, sha256sum, storageKey string) domain.FileNode {
	t.Helper()
	session, err := repo.CreateUploadSession(context.Background(), domain.UploadSession{
		UserID:     userID,
		TargetPath: targetPath,
		TotalSize:  1,
		ChunkSize:  1,
		SHA256:     sha256sum,
		Status:     domain.UploadStatusPending,
		ExpiresAt:  time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create upload session for %s: %v", targetPath, err)
	}
	node, _, err := repo.CommitUpload(context.Background(), userID, session.ID, storageKey)
	if err != nil {
		t.Fatalf("commit upload for %s: %v", targetPath, err)
	}
	return node
}

func createTestDevice(t *testing.T, repo *SQLiteRepository, userID string) domain.Device {
	t.Helper()
	device, err := repo.CreateDevice(context.Background(), userID, "test-device", "test")
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	return device
}
