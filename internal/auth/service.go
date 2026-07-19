package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
)

type Repository interface {
	CreateUser(ctx context.Context, email, passwordHash string) (domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	GetUserByID(ctx context.Context, id string) (domain.User, error)
	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (domain.RefreshToken, error)
	GetRefreshToken(ctx context.Context, tokenHash string) (domain.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	ResolveOAuthUser(ctx context.Context, provider, providerUserID, email, login, avatarURL string) (domain.User, error)
	CreateOAuthLoginCode(ctx context.Context, userID, codeHash string, expiresAt time.Time) error
	ConsumeOAuthLoginCode(ctx context.Context, codeHash string, now time.Time) (domain.User, error)
}

type Service struct {
	repo            Repository
	jwtSecret       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Claims struct {
	UserID string `json:"sub"`
	jwt.RegisteredClaims
}

func NewService(repo Repository, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{repo: repo, jwtSecret: []byte(jwtSecret), accessTokenTTL: accessTTL, refreshTokenTTL: refreshTTL}
}

func (s *Service) Register(ctx context.Context, email, password string) (domain.User, TokenPair, error) {
	email = normalizeEmail(email)
	if email == "" || len(password) < 8 {
		return domain.User{}, TokenPair{}, domain.E(domain.CodeInvalidArgument, "email and password are required", nil)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return domain.User{}, TokenPair{}, domain.E(domain.CodeInternal, "failed to hash password", err)
	}
	user, err := s.repo.CreateUser(ctx, email, hash)
	if err != nil {
		return domain.User{}, TokenPair{}, err
	}
	tokens, err := s.issueTokens(ctx, user.ID)
	return user, tokens, err
}

func (s *Service) Login(ctx context.Context, email, password string) (domain.User, TokenPair, error) {
	user, err := s.repo.GetUserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return domain.User{}, TokenPair{}, domain.E(domain.CodeInvalidCredentials, "invalid email or password", err)
	}
	if user.PasswordHash == "" {
		return domain.User{}, TokenPair{}, domain.E(domain.CodeInvalidCredentials, "use the linked login provider", nil)
	}
	ok, err := VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		return domain.User{}, TokenPair{}, domain.E(domain.CodeInvalidCredentials, "invalid email or password", err)
	}
	tokens, err := s.issueTokens(ctx, user.ID)
	return user, tokens, err
}

func (s *Service) CompleteOAuthLogin(ctx context.Context, provider, providerUserID, email, login, avatarURL string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	email = normalizeEmail(email)
	if provider != "github" || strings.TrimSpace(providerUserID) == "" || email == "" {
		return "", domain.E(domain.CodeInvalidArgument, "verified OAuth identity is required", nil)
	}
	user, err := s.repo.ResolveOAuthUser(ctx, provider, providerUserID, email, strings.TrimSpace(login), strings.TrimSpace(avatarURL))
	if err != nil {
		return "", err
	}
	code, err := randomToken(32)
	if err != nil {
		return "", domain.E(domain.CodeInternal, "failed to create OAuth login code", err)
	}
	if err := s.repo.CreateOAuthLoginCode(ctx, user.ID, TokenHash(code), time.Now().Add(2*time.Minute)); err != nil {
		return "", err
	}
	return code, nil
}

func (s *Service) ExchangeOAuthLoginCode(ctx context.Context, code string) (domain.User, TokenPair, error) {
	if strings.TrimSpace(code) == "" {
		return domain.User{}, TokenPair{}, domain.E(domain.CodeUnauthenticated, "invalid OAuth login code", nil)
	}
	user, err := s.repo.ConsumeOAuthLoginCode(ctx, TokenHash(code), time.Now())
	if err != nil {
		return domain.User{}, TokenPair{}, domain.E(domain.CodeUnauthenticated, "invalid OAuth login code", err)
	}
	tokens, err := s.issueTokens(ctx, user.ID)
	return user, tokens, err
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	hash := TokenHash(refreshToken)
	stored, err := s.repo.GetRefreshToken(ctx, hash)
	if err != nil || stored.RevokedAt != nil || time.Now().After(stored.ExpiresAt) {
		return TokenPair{}, domain.E(domain.CodeUnauthenticated, "invalid refresh token", err)
	}
	if err := s.repo.RevokeRefreshToken(ctx, hash); err != nil {
		return TokenPair{}, err
	}
	return s.issueTokens(ctx, stored.UserID)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.repo.RevokeRefreshToken(ctx, TokenHash(refreshToken))
}

func (s *Service) VerifyAccessToken(tokenString string) (string, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return s.jwtSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !token.Valid || claims.UserID == "" {
		return "", domain.E(domain.CodeUnauthenticated, "invalid access token", err)
	}
	return claims.UserID, nil
}

func (s *Service) issueTokens(ctx context.Context, userID string) (TokenPair, error) {
	now := time.Now()
	expiresAt := now.Add(s.accessTokenTTL)
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return TokenPair{}, domain.E(domain.CodeInternal, "failed to sign access token", err)
	}
	refresh, err := randomToken(32)
	if err != nil {
		return TokenPair{}, domain.E(domain.CodeInternal, "failed to create refresh token", err)
	}
	if _, err := s.repo.CreateRefreshToken(ctx, userID, TokenHash(refresh), now.Add(s.refreshTokenTTL)); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresIn: int64(s.accessTokenTTL.Seconds())}, nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 2 {
		return false, nil
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return false, err
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
