package worker

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/bruceblink/SyncHub/internal/storage"
)

func TestCleanupExpiredUploadSessionsUsesDefaultLimit(t *testing.T) {
	repo := &fakeCleanupRepo{}
	service := NewService(repo, nil)

	count, err := service.CleanupExpiredUploadSessions(context.Background(), 0)
	if err != nil {
		t.Fatalf("cleanup expired upload sessions: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if repo.limit != 1000 {
		t.Fatalf("limit = %d, want default 1000", repo.limit)
	}
	if repo.now.IsZero() {
		t.Fatal("cleanup timestamp was not set")
	}
}

func TestCleanupExpiredFileVersionsUsesPolicy(t *testing.T) {
	repo := &fakeCleanupRepo{}
	service := NewService(repo, nil)

	count, err := service.CleanupExpiredFileVersions(context.Background(), 3, 24*time.Hour, 0)
	if err != nil {
		t.Fatalf("cleanup expired file versions: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if repo.versionLimit != 1000 {
		t.Fatalf("version limit = %d, want default 1000", repo.versionLimit)
	}
	if repo.minVersions != 3 {
		t.Fatalf("min versions = %d, want 3", repo.minVersions)
	}
	if repo.cutoff.IsZero() || time.Since(repo.cutoff) < 23*time.Hour {
		t.Fatalf("cutoff = %s, want roughly 24h ago", repo.cutoff)
	}
}

func TestCleanupExpiredFileVersionsSkipsDisabledMaxAge(t *testing.T) {
	repo := &fakeCleanupRepo{}
	service := NewService(repo, nil)

	count, err := service.CleanupExpiredFileVersions(context.Background(), 3, 0, 100)
	if err != nil {
		t.Fatalf("cleanup expired file versions: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if !repo.cutoff.IsZero() {
		t.Fatalf("cutoff was set for disabled cleanup: %s", repo.cutoff)
	}
}

func TestCleanupExpiredTrashUsesRetentionPolicy(t *testing.T) {
	repo := &fakeCleanupRepo{}
	service := NewService(repo, nil)

	count, err := service.CleanupExpiredTrash(context.Background(), 30*24*time.Hour, 0)
	if err != nil {
		t.Fatalf("cleanup expired trash: %v", err)
	}
	if count != 4 {
		t.Fatalf("count = %d, want 4", count)
	}
	if repo.trashLimit != 1000 {
		t.Fatalf("trash limit = %d, want default 1000", repo.trashLimit)
	}
	if repo.trashCutoff.IsZero() || time.Since(repo.trashCutoff) < 29*24*time.Hour {
		t.Fatalf("trash cutoff = %s, want roughly 30 days ago", repo.trashCutoff)
	}
}

func TestCleanupExpiredTrashSkipsDisabledRetention(t *testing.T) {
	repo := &fakeCleanupRepo{}
	service := NewService(repo, nil)

	count, err := service.CleanupExpiredTrash(context.Background(), 0, 100)
	if err != nil || count != 0 || !repo.trashCutoff.IsZero() {
		t.Fatalf("disabled cleanup = count %d cutoff %s err %v", count, repo.trashCutoff, err)
	}
}

func TestCleanupExpiredUploadChunksDeletesObjectsBeforeMetadata(t *testing.T) {
	repo := &fakeCleanupRepo{
		expiredChunks: []domain.ExpiredUploadChunk{
			{ID: "chunk-1", StorageKey: "staging/u/upload/0"},
			{ID: "chunk-2", StorageKey: "staging/u/upload/1"},
		},
	}
	store := &fakeStorage{}
	service := NewService(repo, store)

	count, err := service.CleanupExpiredUploadChunks(context.Background(), 0)
	if err != nil {
		t.Fatalf("cleanup expired upload chunks: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if repo.chunkLimit != 1000 {
		t.Fatalf("chunk limit = %d, want default 1000", repo.chunkLimit)
	}
	if got := len(store.deletedKeys); got != 2 {
		t.Fatalf("deleted object count = %d, want 2", got)
	}
	if got := len(repo.deletedChunkIDs); got != 2 {
		t.Fatalf("deleted metadata count = %d, want 2", got)
	}
	if store.deletedKeys[0] != "staging/u/upload/0" || repo.deletedChunkIDs[0] != "chunk-1" {
		t.Fatalf("first deletion = key %q chunk %q", store.deletedKeys[0], repo.deletedChunkIDs[0])
	}
}

type fakeCleanupRepo struct {
	now          time.Time
	limit        int32
	cutoff       time.Time
	minVersions  int64
	versionLimit int32
	trashCutoff  time.Time
	trashLimit   int32

	chunkLimit      int32
	expiredChunks   []domain.ExpiredUploadChunk
	deletedChunkIDs []string
}

func (r *fakeCleanupRepo) ExpireUploadSessions(ctx context.Context, now time.Time, limit int32) (int64, error) {
	_ = ctx
	r.now = now
	r.limit = limit
	return 3, nil
}

func (r *fakeCleanupRepo) DeleteExpiredFileVersions(ctx context.Context, cutoff time.Time, minVersions int64, limit int32) (int64, error) {
	_ = ctx
	r.cutoff = cutoff
	r.minVersions = minVersions
	r.versionLimit = limit
	return 2, nil
}

func (r *fakeCleanupRepo) PurgeExpiredDeletedFiles(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	_ = ctx
	r.trashCutoff = cutoff
	r.trashLimit = limit
	return 4, nil
}

func (r *fakeCleanupRepo) ListExpiredUploadChunks(ctx context.Context, limit int32) ([]domain.ExpiredUploadChunk, error) {
	_ = ctx
	r.chunkLimit = limit
	return r.expiredChunks, nil
}

func (r *fakeCleanupRepo) DeleteUploadChunk(ctx context.Context, chunkID string) error {
	_ = ctx
	r.deletedChunkIDs = append(r.deletedChunkIDs, chunkID)
	return nil
}

type fakeStorage struct {
	deletedKeys []string
}

func (s *fakeStorage) PutChunk(ctx context.Context, key string, r io.Reader, checksum string) error {
	_, _, _, _ = ctx, key, r, checksum
	return nil
}

func (s *fakeStorage) Compose(ctx context.Context, targetKey string, chunkKeys []string) error {
	_, _, _ = ctx, targetKey, chunkKeys
	return nil
}

func (s *fakeStorage) Read(ctx context.Context, key string, br *storage.ByteRange) (io.ReadCloser, storage.ObjectInfo, error) {
	_, _, _ = ctx, key, br
	return nil, storage.ObjectInfo{}, nil
}

func (s *fakeStorage) Delete(ctx context.Context, key string) error {
	_ = ctx
	s.deletedKeys = append(s.deletedKeys, key)
	return nil
}
