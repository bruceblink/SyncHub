package auth

import (
	"context"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
)

func TestOAuthLoginIssuesSingleUseExchangeCode(t *testing.T) {
	repo := newAuthRepository()
	service := NewService(repo, "oauth-test-secret", 15*time.Minute, time.Hour)

	code, err := service.CompleteOAuthLogin(context.Background(), "github", "123", "User@Example.com", "octocat", "avatar")
	if err != nil {
		t.Fatalf("complete OAuth login: %v", err)
	}
	if repo.oauthProvider != "github" || repo.oauthUserID != "123" || repo.oauthEmail != "user@example.com" {
		t.Fatalf("resolved identity = %q %q %q", repo.oauthProvider, repo.oauthUserID, repo.oauthEmail)
	}
	user, tokens, err := service.ExchangeOAuthLoginCode(context.Background(), code)
	if err != nil || user.ID != "user-1" || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("exchange OAuth code: user=%#v tokens=%#v err=%v", user, tokens, err)
	}
	if _, _, err := service.ExchangeOAuthLoginCode(context.Background(), code); domain.ErrorCodeOf(err) != domain.CodeUnauthenticated {
		t.Fatalf("replayed OAuth code error = %v", err)
	}
}

type authRepository struct {
	user          domain.User
	oauthProvider string
	oauthUserID   string
	oauthEmail    string
	codes         map[string]time.Time
}

func newAuthRepository() *authRepository {
	return &authRepository{user: domain.User{ID: "user-1", Email: "user@example.com", Status: "active"}, codes: map[string]time.Time{}}
}

func (r *authRepository) CreateUser(context.Context, string, string) (domain.User, error) {
	return r.user, nil
}
func (r *authRepository) GetUserByEmail(context.Context, string) (domain.User, error) {
	return r.user, nil
}
func (r *authRepository) GetUserByID(context.Context, string) (domain.User, error) {
	return r.user, nil
}
func (r *authRepository) CreateRefreshToken(_ context.Context, userID, tokenHash string, expiresAt time.Time) (domain.RefreshToken, error) {
	return domain.RefreshToken{UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt}, nil
}
func (r *authRepository) GetRefreshToken(context.Context, string) (domain.RefreshToken, error) {
	return domain.RefreshToken{}, domain.E(domain.CodeNotFound, "not found", nil)
}
func (r *authRepository) RevokeRefreshToken(context.Context, string) error { return nil }
func (r *authRepository) ResolveOAuthUser(_ context.Context, provider, providerUserID, email, _, _ string) (domain.User, error) {
	r.oauthProvider, r.oauthUserID, r.oauthEmail = provider, providerUserID, email
	return r.user, nil
}
func (r *authRepository) CreateOAuthLoginCode(_ context.Context, userID, codeHash string, expiresAt time.Time) error {
	r.codes[codeHash] = expiresAt
	return nil
}
func (r *authRepository) ConsumeOAuthLoginCode(_ context.Context, codeHash string, now time.Time) (domain.User, error) {
	expiresAt, ok := r.codes[codeHash]
	if !ok || !expiresAt.After(now) {
		return domain.User{}, domain.E(domain.CodeNotFound, "not found", nil)
	}
	delete(r.codes, codeHash)
	return r.user, nil
}

var _ Repository = (*authRepository)(nil)
