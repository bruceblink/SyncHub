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

type fakeCleanupRepo struct {
	now   time.Time
	limit int32
}

func (r *fakeCleanupRepo) ExpireUploadSessions(ctx context.Context, now time.Time, limit int32) (int64, error) {
	_ = ctx
	r.now = now
	r.limit = limit
	return 3, nil
}
