package metadata

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/domain"
)

var allowedCollections = map[string]map[string]bool{
	"kvideo":     {"watch-history": true, "favorites": true},
	"latestnews": {"reading-history": true, "favorites": true},
}

var supportedApplications = map[string]bool{
	"kvideo":          true,
	"latestnews":      true,
	"synchub-desktop": true,
}

type Repository interface {
	CreateAPIKey(ctx context.Context, userID, name, application, keyPrefix, secretHash string) (domain.APIKey, error)
	ListAPIKeys(ctx context.Context, userID string) ([]domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, userID, keyID string) error
	GetAPIKeyBySecretHash(ctx context.Context, secretHash string) (domain.APIKey, error)
	TouchAPIKey(ctx context.Context, keyID string) error
	GetSubscription(ctx context.Context, userID string) (domain.Subscription, error)
	UpdateSubscriptionCancellation(ctx context.Context, userID string, cancelAtPeriodEnd bool) (domain.Subscription, error)
	GetMetadataDocument(ctx context.Context, userID, application, collection string) (domain.MetadataDocument, error)
	PutMetadataDocument(ctx context.Context, userID, application, collection string, payload []byte) (domain.MetadataDocument, error)
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) CreateAPIKey(ctx context.Context, userID, name, application string) (domain.APIKey, string, error) {
	application = normalizeApplication(application)
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || !supportedApplications[application] {
		return domain.APIKey{}, "", domain.E(domain.CodeInvalidArgument, "name and supported application are required", nil)
	}
	if err := s.requireActiveSubscription(ctx, userID); err != nil {
		return domain.APIKey{}, "", err
	}
	secret, err := randomAPIKey()
	if err != nil {
		return domain.APIKey{}, "", domain.E(domain.CodeInternal, "failed to generate api key", err)
	}
	key := "shk_" + secret
	stored, err := s.repo.CreateAPIKey(ctx, userID, name, application, key[:12], auth.TokenHash(key))
	return stored, key, err
}

func (s *Service) ListAPIKeys(ctx context.Context, userID string) ([]domain.APIKey, error) {
	return s.repo.ListAPIKeys(ctx, userID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	return s.repo.RevokeAPIKey(ctx, userID, keyID)
}

func (s *Service) Subscription(ctx context.Context, userID string) (domain.Subscription, error) {
	return s.repo.GetSubscription(ctx, userID)
}

func (s *Service) UpdateSubscriptionCancellation(ctx context.Context, userID string, cancel bool) (domain.Subscription, error) {
	subscription, err := s.repo.GetSubscription(ctx, userID)
	if err != nil {
		return domain.Subscription{}, err
	}
	if subscription.Plan == "free" {
		return domain.Subscription{}, domain.E(domain.CodeInvalidArgument, "free plan has no renewal to manage", nil)
	}
	return s.repo.UpdateSubscriptionCancellation(ctx, userID, cancel)
}

func (s *Service) Authorize(ctx context.Context, key, application string) (string, error) {
	application = normalizeApplication(application)
	if application == "" || !strings.HasPrefix(key, "shk_") {
		return "", domain.E(domain.CodeUnauthenticated, "invalid api key", nil)
	}
	stored, err := s.repo.GetAPIKeyBySecretHash(ctx, auth.TokenHash(key))
	if err != nil || stored.Application != application || stored.RevokedAt != nil {
		return "", domain.E(domain.CodeUnauthenticated, "invalid api key", err)
	}
	if err := s.requireActiveSubscription(ctx, stored.UserID); err != nil {
		return "", err
	}
	if err := s.repo.TouchAPIKey(ctx, stored.ID); err != nil {
		return "", err
	}
	return stored.UserID, nil
}

func (s *Service) GetDocument(ctx context.Context, userID, application, collection string) (domain.MetadataDocument, error) {
	if !validCollection(application, collection) {
		return domain.MetadataDocument{}, domain.E(domain.CodeInvalidArgument, "unsupported metadata collection", nil)
	}
	return s.repo.GetMetadataDocument(ctx, userID, application, collection)
}

func (s *Service) PutDocument(ctx context.Context, userID, application, collection string, payload json.RawMessage) (domain.MetadataDocument, error) {
	if !validCollection(application, collection) || !json.Valid(payload) {
		return domain.MetadataDocument{}, domain.E(domain.CodeInvalidArgument, "invalid metadata document", nil)
	}
	if len(payload) > 1024*1024 {
		return domain.MetadataDocument{}, domain.E(domain.CodeInvalidArgument, "metadata document exceeds 1 MiB", nil)
	}
	return s.repo.PutMetadataDocument(ctx, userID, application, collection, payload)
}

func (s *Service) requireActiveSubscription(ctx context.Context, userID string) error {
	subscription, err := s.repo.GetSubscription(ctx, userID)
	if err != nil {
		return err
	}
	if subscription.Status != "active" || (subscription.ExpiresAt != nil && time.Now().After(*subscription.ExpiresAt)) {
		return domain.E(domain.CodePermissionDenied, "an active subscription is required", nil)
	}
	return nil
}

func normalizeApplication(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func validCollection(application, collection string) bool {
	return allowedCollections[normalizeApplication(application)][strings.TrimSpace(collection)]
}

func randomAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
