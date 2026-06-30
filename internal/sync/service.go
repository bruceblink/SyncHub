package syncsvc

import (
	"context"
	"strings"

	"github.com/bruceblink/SyncHub/internal/domain"
)

type Repository interface {
	CreateDevice(ctx context.Context, userID, name, platform string) (domain.Device, error)
	HeartbeatDevice(ctx context.Context, userID, deviceID string) (domain.Device, error)
	ListChanges(ctx context.Context, userID, deviceID string, afterChangeID int64, limit int32) ([]domain.ChangeEvent, error)
	AckDevice(ctx context.Context, userID, deviceID string, lastAppliedChangeID int64) (domain.Device, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) RegisterDevice(ctx context.Context, userID, name, platform string) (domain.Device, error) {
	name = strings.TrimSpace(name)
	platform = strings.TrimSpace(platform)
	if name == "" || platform == "" {
		return domain.Device{}, domain.E(domain.CodeInvalidArgument, "name and platform are required", nil)
	}
	return s.repo.CreateDevice(ctx, userID, name, platform)
}

func (s *Service) Heartbeat(ctx context.Context, userID, deviceID string) (domain.Device, error) {
	if strings.TrimSpace(deviceID) == "" {
		return domain.Device{}, domain.E(domain.CodeInvalidArgument, "device id is required", nil)
	}
	return s.repo.HeartbeatDevice(ctx, userID, deviceID)
}

func (s *Service) Changes(ctx context.Context, userID, deviceID string, afterChangeID int64, limit int32) ([]domain.ChangeEvent, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, domain.E(domain.CodeInvalidArgument, "device id is required", nil)
	}
	if afterChangeID < 0 {
		return nil, domain.E(domain.CodeInvalidArgument, "after_change_id must be non-negative", nil)
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	return s.repo.ListChanges(ctx, userID, deviceID, afterChangeID, limit)
}

func (s *Service) Ack(ctx context.Context, userID, deviceID string, lastAppliedChangeID int64) (domain.Device, error) {
	if strings.TrimSpace(deviceID) == "" {
		return domain.Device{}, domain.E(domain.CodeInvalidArgument, "device id is required", nil)
	}
	if lastAppliedChangeID < 0 {
		return domain.Device{}, domain.E(domain.CodeInvalidArgument, "last_applied_change_id must be non-negative", nil)
	}
	return s.repo.AckDevice(ctx, userID, deviceID, lastAppliedChangeID)
}
