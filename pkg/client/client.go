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

type FileNode struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	NodeType  string    `json:"node_type"`
	Size      int64     `json:"size"`
	SHA256    *string   `json:"sha256"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

type Device struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Platform            string     `json:"platform"`
	LastSeenAt          *time.Time `json:"last_seen_at"`
	LastAppliedChangeID int64      `json:"last_applied_change_id"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ChangeEvent struct {
	ID             int64     `json:"id"`
	FileID         string    `json:"file_id"`
	EventType      string    `json:"event_type"`
	Version        *int64    `json:"version"`
	Path           string    `json:"path"`
	OldPath        *string   `json:"old_path"`
	SourceDeviceID *string   `json:"source_device_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type ChangeList struct {
	Items      []ChangeEvent `json:"items"`
	NextCursor int64         `json:"next_cursor"`
}

type DownloadOptions struct {
	Range       string
	IfNoneMatch string
}

type DownloadResult struct {
	Body          io.ReadCloser
	StatusCode    int
	ContentLength int64
	ETag          string
	ContentRange  string
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

func (c *Client) CreateDirectory(ctx context.Context, accessToken, path string) (FileNode, error) {
	var data FileNode
	err := c.postJSONAuth(ctx, "/api/v1/files/directories", accessToken, map[string]string{
		"path": path,
	}, nil, &data)
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

func (c *Client) DownloadFile(ctx context.Context, accessToken, fileID string, opts DownloadOptions) (DownloadResult, error) {
	path := fmt.Sprintf("/api/v1/files/%s/content", url.PathEscape(fileID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path), nil)
	if err != nil {
		return DownloadResult{}, err
	}
	setBearerToken(req, accessToken)
	if strings.TrimSpace(opts.Range) != "" {
		req.Header.Set("Range", opts.Range)
	}
	if strings.TrimSpace(opts.IfNoneMatch) != "" {
		req.Header.Set("If-None-Match", opts.IfNoneMatch)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return DownloadResult{}, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusNotModified {
		defer resp.Body.Close()
		return DownloadResult{}, decodeAPIError(resp)
	}
	return DownloadResult{
		Body:          resp.Body,
		StatusCode:    resp.StatusCode,
		ContentLength: resp.ContentLength,
		ETag:          resp.Header.Get("ETag"),
		ContentRange:  resp.Header.Get("Content-Range"),
	}, nil
}

func (c *Client) RegisterDevice(ctx context.Context, accessToken, name, platform string) (Device, error) {
	var data Device
	err := c.postJSONAuth(ctx, "/api/v1/devices", accessToken, map[string]string{
		"name":     name,
		"platform": platform,
	}, nil, &data)
	return data, err
}

func (c *Client) HeartbeatDevice(ctx context.Context, accessToken, deviceID string) (Device, error) {
	var data Device
	path := fmt.Sprintf("/api/v1/devices/%s/heartbeat", url.PathEscape(deviceID))
	err := c.postJSONAuth(ctx, path, accessToken, map[string]any{}, nil, &data)
	return data, err
}

func (c *Client) ListChanges(ctx context.Context, accessToken, deviceID string, afterChangeID int64, limit int32) (ChangeList, error) {
	var data ChangeList
	values := url.Values{}
	values.Set("device_id", deviceID)
	values.Set("after_change_id", fmt.Sprintf("%d", afterChangeID))
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	err := c.getJSONAuth(ctx, "/api/v1/sync/changes?"+values.Encode(), accessToken, &data)
	return data, err
}

func (c *Client) AckChanges(ctx context.Context, accessToken, deviceID string, lastAppliedChangeID int64) (Device, error) {
	var data Device
	err := c.postJSONAuth(ctx, "/api/v1/sync/ack", accessToken, map[string]any{
		"device_id":              deviceID,
		"last_applied_change_id": lastAppliedChangeID,
	}, nil, &data)
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
	resp, err := c.httpClient().Do(req)
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

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func decodeAPIError(resp *http.Response) error {
	var envelope struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return &Error{StatusCode: resp.StatusCode, Message: fmt.Sprintf("request failed with status %d", resp.StatusCode)}
	}
	return &Error{StatusCode: resp.StatusCode, Code: envelope.Code, Message: envelope.Message}
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
	pathPart := path
	rawQuery := ""
	if before, after, ok := strings.Cut(path, "?"); ok {
		pathPart = before
		rawQuery = after
	}
	u.Path = singleJoiningSlash(u.Path, pathPart)
	u.RawQuery = rawQuery
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
