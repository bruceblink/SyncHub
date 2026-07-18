package metadata

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/domain"
)

func TestCreateAPIKeyCreatesApplicationBoundSecret(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)

	key, secret, err := service.CreateAPIKey(context.Background(), "user-1", "KVideo browser", "kvideo")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if key.Application != "kvideo" || key.KeyPrefix == "" || secret[:4] != "shk_" {
		t.Fatalf("unexpected key result: %#v secret=%q", key, secret)
	}
	if repo.keyHashes[key.ID] != auth.TokenHash(secret) {
		t.Fatal("api key secret must be stored as a hash")
	}

	userID, err := service.Authorize(context.Background(), secret, "kvideo")
	if err != nil || userID != "user-1" {
		t.Fatalf("authorize matching application: userID=%q err=%v", userID, err)
	}
	if _, err := service.Authorize(context.Background(), secret, "latestnews"); domain.ErrorCodeOf(err) != domain.CodeUnauthenticated {
		t.Fatalf("cross-application authorization error = %v", err)
	}
}

func TestAuthorizeRejectsInactiveSubscription(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	_, secret, err := service.CreateAPIKey(context.Background(), "user-1", "KVideo browser", "kvideo")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	repo.subscription.Status = "expired"
	if _, err := service.Authorize(context.Background(), secret, "kvideo"); domain.ErrorCodeOf(err) != domain.CodePermissionDenied {
		t.Fatalf("expired subscription error = %v", err)
	}
}

func TestMetadataDocumentsValidateApplicationCollections(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	payload := json.RawMessage(`[{"title":"Example"}]`)

	document, err := service.PutDocument(context.Background(), "user-1", "kvideo", "favorites", payload)
	if err != nil || document.Version != 1 {
		t.Fatalf("put document: %#v err=%v", document, err)
	}
	document, err = service.PutDocument(context.Background(), "user-1", "kvideo", "favorites", payload)
	if err != nil || document.Version != 2 {
		t.Fatalf("update document: %#v err=%v", document, err)
	}
	if _, err := service.PutDocument(context.Background(), "user-1", "latestnews", "watch-history", payload); domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("unsupported collection error = %v", err)
	}
}

type fakeRepository struct {
	keys         map[string]domain.APIKey
	keyHashes    map[string]string
	subscription domain.Subscription
	documents    map[string]domain.MetadataDocument
}

func newFakeRepository() *fakeRepository {
	now := time.Now()
	return &fakeRepository{keys: map[string]domain.APIKey{}, keyHashes: map[string]string{}, subscription: domain.Subscription{UserID: "user-1", Plan: "free", Status: "active", CreatedAt: now, UpdatedAt: now}, documents: map[string]domain.MetadataDocument{}}
}
func (r *fakeRepository) CreateAPIKey(_ context.Context, userID, name, application, keyPrefix, secretHash string) (domain.APIKey, error) {
	key := domain.APIKey{ID: string(rune(len(r.keys) + 1)), UserID: userID, Name: name, Application: application, KeyPrefix: keyPrefix, CreatedAt: time.Now()}
	r.keys[key.ID] = key
	r.keyHashes[key.ID] = secretHash
	return key, nil
}
func (r *fakeRepository) ListAPIKeys(_ context.Context, userID string) ([]domain.APIKey, error) {
	var keys []domain.APIKey
	for _, key := range r.keys {
		if key.UserID == userID {
			keys = append(keys, key)
		}
	}
	return keys, nil
}
func (r *fakeRepository) RevokeAPIKey(_ context.Context, userID, keyID string) error {
	key := r.keys[keyID]
	if key.UserID != userID {
		return domain.E(domain.CodeNotFound, "api key not found", nil)
	}
	now := time.Now()
	key.RevokedAt = &now
	r.keys[keyID] = key
	return nil
}
func (r *fakeRepository) GetAPIKeyBySecretHash(_ context.Context, secretHash string) (domain.APIKey, error) {
	for id, hash := range r.keyHashes {
		if hash == secretHash {
			return r.keys[id], nil
		}
	}
	return domain.APIKey{}, domain.E(domain.CodeNotFound, "api key not found", nil)
}
func (r *fakeRepository) TouchAPIKey(_ context.Context, keyID string) error {
	key := r.keys[keyID]
	now := time.Now()
	key.LastUsedAt = &now
	r.keys[keyID] = key
	return nil
}
func (r *fakeRepository) GetSubscription(_ context.Context, _ string) (domain.Subscription, error) {
	return r.subscription, nil
}
func (r *fakeRepository) GetMetadataDocument(_ context.Context, userID, application, collection string) (domain.MetadataDocument, error) {
	document, ok := r.documents[userID+application+collection]
	if !ok {
		return domain.MetadataDocument{}, domain.E(domain.CodeNotFound, "metadata document not found", nil)
	}
	return document, nil
}
func (r *fakeRepository) PutMetadataDocument(_ context.Context, userID, application, collection string, payload []byte) (domain.MetadataDocument, error) {
	key := userID + application + collection
	document := r.documents[key]
	document.UserID, document.Application, document.Collection, document.Payload = userID, application, collection, append([]byte(nil), payload...)
	document.Version++
	document.UpdatedAt = time.Now()
	r.documents[key] = document
	return document, nil
}
