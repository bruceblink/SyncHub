package worker

import (
	"context"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/bruceblink/SyncHub/internal/storage"
)

type Repository interface {
	ExpireUploadSessions(ctx context.Context, now time.Time, limit int32) (int64, error)
	DeleteExpiredFileVersions(ctx context.Context, cutoff time.Time, minVersions int64, limit int32) (int64, error)
	ListExpiredUploadChunks(ctx context.Context, limit int32) ([]domain.ExpiredUploadChunk, error)
	DeleteUploadChunk(ctx context.Context, chunkID string) error
}

type Service struct {
	repo  Repository
	store storage.Storage
}

func NewService(repo Repository, store storage.Storage) *Service {
	return &Service{repo: repo, store: store}
}

func (s *Service) CleanupExpiredUploadSessions(ctx context.Context, limit int32) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	return s.repo.ExpireUploadSessions(ctx, time.Now().UTC(), limit)
}

func (s *Service) CleanupExpiredFileVersions(ctx context.Context, minVersions int64, maxAge time.Duration, limit int32) (int64, error) {
	if minVersions <= 0 {
		minVersions = 20
	}
	if maxAge <= 0 {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	cutoff := time.Now().UTC().Add(-maxAge)
	return s.repo.DeleteExpiredFileVersions(ctx, cutoff, minVersions, limit)
}

func (s *Service) CleanupExpiredUploadChunks(ctx context.Context, limit int32) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	if s.store == nil {
		return 0, nil
	}
	chunks, err := s.repo.ListExpiredUploadChunks(ctx, limit)
	if err != nil {
		return 0, err
	}
	var deleted int64
	for _, chunk := range chunks {
		if err := s.store.Delete(ctx, chunk.StorageKey); err != nil {
			return deleted, err
		}
		if err := s.repo.DeleteUploadChunk(ctx, chunk.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
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

func (s *Service) RunUploadChunkCleanupLoop(ctx context.Context, interval time.Duration, limit int32, onError func(error)) {
	if interval <= 0 || s.store == nil {
		return
	}
	s.cleanupUploadChunks(ctx, limit, onError)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupUploadChunks(ctx, limit, onError)
		}
	}
}

func (s *Service) RunFileVersionCleanupLoop(ctx context.Context, interval time.Duration, minVersions int64, maxAge time.Duration, limit int32, onError func(error)) {
	if interval <= 0 || maxAge <= 0 {
		return
	}
	s.cleanupFileVersions(ctx, minVersions, maxAge, limit, onError)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupFileVersions(ctx, minVersions, maxAge, limit, onError)
		}
	}
}

func (s *Service) cleanup(ctx context.Context, limit int32, onError func(error)) {
	if _, err := s.CleanupExpiredUploadSessions(ctx, limit); err != nil && onError != nil {
		onError(err)
	}
}

func (s *Service) cleanupUploadChunks(ctx context.Context, limit int32, onError func(error)) {
	if _, err := s.CleanupExpiredUploadChunks(ctx, limit); err != nil && onError != nil {
		onError(err)
	}
}

func (s *Service) cleanupFileVersions(ctx context.Context, minVersions int64, maxAge time.Duration, limit int32, onError func(error)) {
	if _, err := s.CleanupExpiredFileVersions(ctx, minVersions, maxAge, limit); err != nil && onError != nil {
		onError(err)
	}
}
