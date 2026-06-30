package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type User struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type LoginData struct {
	User   User      `json:"user"`
	Tokens TokenPair `json:"tokens"`
}

type InitUploadRequest struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ChunkSize   int64  `json:"chunk_size,omitempty"`
	BaseVersion *int64 `json:"base_version,omitempty"`
}

type UploadSession struct {
	UploadID       string        `json:"upload_id"`
	Path           string        `json:"path"`
	ChunkSize      int64         `json:"chunk_size"`
	ExpiresAt      time.Time     `json:"expires_at"`
	Status         string        `json:"status"`
	UploadedChunks []UploadChunk `json:"uploaded_chunks"`
}

type UploadChunk struct {
	ChunkIndex int32  `json:"chunk_index"`
	Size       int32  `json:"size"`
	SHA256     string `json:"sha256"`
}

type CommitUploadData struct {
	FileID   string `json:"file_id"`
	Version  int64  `json:"version"`
	ChangeID int64  `json:"change_id"`
}

type Error struct {
	StatusCode int
	Code       any
	Message    string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("request failed with status %d", e.StatusCode)
}

func New(baseURL string) *Client {
	return &Client{BaseURL: normalizeBaseURL(baseURL), HTTPClient: http.DefaultClient}
}

func (c *Client) Login(ctx context.Context, email, password string) (LoginData, error) {
	var data LoginData
	err := c.postJSON(ctx, "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, &data)
	return data, err
}

func (c *Client) InitUpload(ctx context.Context, accessToken string, req InitUploadRequest, idempotencyKey string) (UploadSession, error) {
	var data UploadSession
	headers := map[string]string{}
	if strings.TrimSpace(idempotencyKey) != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}
	err := c.postJSONAuth(ctx, "/api/v1/uploads", accessToken, req, headers, &data)
	return data, err
}

func (c *Client) PutUploadChunk(ctx context.Context, accessToken, uploadID string, chunkIndex int32, r io.Reader, sha256sum string) (UploadChunk, error) {
	var data UploadChunk
	path := fmt.Sprintf("/api/v1/uploads/%s/chunks/%d", url.PathEscape(uploadID), chunkIndex)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint(path), r)
	if err != nil {
		return data, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Chunk-Sha256", sha256sum)
	setBearerToken(req, accessToken)
	return data, c.doJSON(req, &data)
}

func (c *Client) UploadStatus(ctx context.Context, accessToken, uploadID string) (UploadSession, error) {
	var data UploadSession
	path := fmt.Sprintf("/api/v1/uploads/%s", url.PathEscape(uploadID))
	err := c.getJSONAuth(ctx, path, accessToken, &data)
	return data, err
}

func (c *Client) CommitUpload(ctx context.Context, accessToken, uploadID string) (CommitUploadData, error) {
	var data CommitUploadData
	path := fmt.Sprintf("/api/v1/uploads/%s/commit", url.PathEscape(uploadID))
	err := c.postJSONAuth(ctx, path, accessToken, map[string]any{}, nil, &data)
	return data, err
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	return c.postJSONAuth(ctx, path, "", body, nil, out)
}

func (c *Client) postJSONAuth(ctx context.Context, path, accessToken string, body any, headers map[string]string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	setBearerToken(req, accessToken)
	return c.doJSON(req, out)
}

func (c *Client) getJSONAuth(ctx context.Context, path, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path), nil)
	if err != nil {
		return err
	}
	setBearerToken(req, accessToken)
	return c.doJSON(req, out)
}

func (c *Client) doJSON(req *http.Request, out any) error {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var envelope struct {
		Code    any             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !isSuccessCode(envelope.Code) {
		return &Error{StatusCode: resp.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	if out == nil || len(envelope.Data) == 0 {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

func setBearerToken(req *http.Request, accessToken string) {
	if strings.TrimSpace(accessToken) != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
}

func (c *Client) endpoint(path string) string {
	base := normalizeBaseURL(c.BaseURL)
	u, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	}
	u.Path = singleJoiningSlash(u.Path, path)
	return u.String()
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func isSuccessCode(code any) bool {
	switch v := code.(type) {
	case nil:
		return true
	case float64:
		return v == 0
	case int:
		return v == 0
	case string:
		return v == "" || v == "0"
	default:
		return false
	}
}

func (t TokenPair) AccessTokenExpiresAt(now time.Time) time.Time {
	return now.Add(time.Duration(t.ExpiresIn) * time.Second)
}
