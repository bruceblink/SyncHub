package worker

import (
	"context"
	"testing"
	"time"
)

func TestCleanupExpiredUploadSessionsUsesDefaultLimit(t *testing.T) {
	repo := &fakeCleanupRepo{}
	service := NewService(repo)

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
	service := NewService(repo)

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
	service := NewService(repo)

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

type fakeCleanupRepo struct {
	now          time.Time
	limit        int32
	cutoff       time.Time
	minVersions  int64
	versionLimit int32
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
