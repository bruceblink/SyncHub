package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"user":{"id":"u1","email":"user@example.com","status":"active"},"tokens":{"access_token":"access","refresh_token":"refresh","expires_in":900}}}`))
	}))
	defer server.Close()

	data, err := New(server.URL).Login(context.Background(), "user@example.com", "password123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if data.User.Email != "user@example.com" || data.Tokens.AccessToken != "access" {
		t.Fatalf("unexpected login data: %#v", data)
	}
}

func TestLoginReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"AUTH_INVALID_CREDENTIALS","message":"invalid email or password"}`))
	}))
	defer server.Close()

	_, err := New(server.URL).Login(context.Background(), "user@example.com", "wrong-password")
	if err == nil {
		t.Fatal("expected login error")
	}
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.Message != "invalid email or password" {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}
