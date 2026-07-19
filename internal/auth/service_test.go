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
	deletedUserID string
	revokedToken  string
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
	if r.deletedUserID != "" {
		return domain.User{}, domain.E(domain.CodeNotFound, "not found", nil)
	}
	return r.user, nil
}
func (r *authRepository) CreateRefreshToken(_ context.Context, userID, tokenHash string, expiresAt time.Time) (domain.RefreshToken, error) {
	return domain.RefreshToken{UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt}, nil
}
func (r *authRepository) GetRefreshToken(context.Context, string) (domain.RefreshToken, error) {
	return domain.RefreshToken{}, domain.E(domain.CodeNotFound, "not found", nil)
}
func (r *authRepository) RevokeRefreshToken(_ context.Context, tokenHash string) error {
	r.revokedToken = tokenHash
	return nil
}
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
func (r *authRepository) DeleteUser(_ context.Context, userID string) error {
	r.deletedUserID = userID
	return nil
}

func TestDeleteAccountRequiresEmailAndPasswordForPasswordUser(t *testing.T) {
	repo := newAuthRepository()
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repo.user.PasswordHash = hash
	service := NewService(repo, "delete-test-secret", 15*time.Minute, time.Hour)

	if err := service.DeleteAccount(context.Background(), repo.user.ID, "other@example.com", "password123"); domain.ErrorCodeOf(err) != domain.CodeInvalidCredentials {
		t.Fatalf("wrong email error = %v", err)
	}
	if err := service.DeleteAccount(context.Background(), repo.user.ID, repo.user.Email, "wrong-password"); domain.ErrorCodeOf(err) != domain.CodeInvalidCredentials {
		t.Fatalf("wrong password error = %v", err)
	}
	if err := service.DeleteAccount(context.Background(), repo.user.ID, "USER@example.com", "password123"); err != nil {
		t.Fatalf("delete password account: %v", err)
	}
	if repo.deletedUserID != repo.user.ID {
		t.Fatalf("deleted user = %q", repo.deletedUserID)
	}
}

func TestDeleteAccountAllowsPasswordlessOAuthUserWithEmailConfirmation(t *testing.T) {
	repo := newAuthRepository()
	service := NewService(repo, "delete-test-secret", 15*time.Minute, time.Hour)

	if err := service.DeleteAccount(context.Background(), repo.user.ID, repo.user.Email, ""); err != nil {
		t.Fatalf("delete OAuth account: %v", err)
	}
	if repo.deletedUserID != repo.user.ID {
		t.Fatalf("deleted user = %q", repo.deletedUserID)
	}
}

func TestDeletedAccountAccessTokenIsRejectedImmediately(t *testing.T) {
	repo := newAuthRepository()
	service := NewService(repo, "delete-token-test-secret", 15*time.Minute, time.Hour)
	tokens, err := service.issueTokens(context.Background(), repo.user.ID)
	if err != nil {
		t.Fatalf("issue tokens: %v", err)
	}
	if _, err := service.VerifyAccessToken(context.Background(), tokens.AccessToken); err != nil {
		t.Fatalf("verify active account token: %v", err)
	}
	if err := service.DeleteAccount(context.Background(), repo.user.ID, repo.user.Email, ""); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if _, err := service.VerifyAccessToken(context.Background(), tokens.AccessToken); domain.ErrorCodeOf(err) != domain.CodeUnauthenticated {
		t.Fatalf("deleted account token error = %v", err)
	}
}

func TestLogoutRevokesRefreshTokenByHash(t *testing.T) {
	repo := newAuthRepository()
	service := NewService(repo, "logout-test-secret", 15*time.Minute, time.Hour)

	if err := service.Logout(context.Background(), "refresh-token"); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if repo.revokedToken != TokenHash("refresh-token") {
		t.Fatalf("revoked token hash = %q", repo.revokedToken)
	}
}

var _ Repository = (*authRepository)(nil)
