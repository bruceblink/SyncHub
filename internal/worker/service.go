package worker

import (
	"context"
	"time"
)

type Repository interface {
	ExpireUploadSessions(ctx context.Context, now time.Time, limit int32) (int64, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CleanupExpiredUploadSessions(ctx context.Context, limit int32) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	return s.repo.ExpireUploadSessions(ctx, time.Now().UTC(), limit)
}

func (s *Service) RunUploadSessionCleanupLoop(ctx context.Context, interval time.Duration, limit int32, onError func(error)) {
	if interval <= 0 {
		return
	}
	s.cleanup(ctx, limit, onError)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup(ctx, limit, onError)
		}
	}
}

func (s *Service) cleanup(ctx context.Context, limit int32, onError func(error)) {
	if _, err := s.CleanupExpiredUploadSessions(ctx, limit); err != nil && onError != nil {
		onError(err)
	}
}
