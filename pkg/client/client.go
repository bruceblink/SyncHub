package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

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
