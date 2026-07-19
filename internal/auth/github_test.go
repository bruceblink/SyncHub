package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestGitHubOAuthExchangesVerifiedPrimaryEmail(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		switch req.URL.String() {
		case githubTokenURL:
			body = `{"access_token":"github-token"}`
		case githubAPIURL + "/user":
			if req.Header.Get("Authorization") != "Bearer github-token" {
				t.Fatalf("authorization header = %q", req.Header.Get("Authorization"))
			}
			body = `{"id":123,"login":"octocat","avatar_url":"https://avatars.example/octocat"}`
		case githubAPIURL + "/user/emails":
			body = `[{"email":"other@example.com","primary":false,"verified":true},{"email":"User@Example.com","primary":true,"verified":true}]`
		default:
			t.Fatalf("unexpected GitHub request %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	oauth := &GitHubOAuth{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://sync.example/callback", HTTPClient: client}

	identity, err := oauth.Exchange(context.Background(), "oauth-code")
	if err != nil {
		t.Fatalf("exchange GitHub identity: %v", err)
	}
	want := GitHubIdentity{UserID: "123", Login: "octocat", Email: "user@example.com", AvatarURL: "https://avatars.example/octocat"}
	if identity != want {
		t.Fatalf("identity = %#v, want %#v", identity, want)
	}
}

func TestGitHubOAuthRequiresVerifiedPrimaryEmail(t *testing.T) {
	responses := []string{
		`{"access_token":"github-token"}`,
		`{"id":123,"login":"octocat"}`,
		`[{"email":"user@example.com","primary":true,"verified":false}]`,
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := responses[0]
		responses = responses[1:]
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	oauth := &GitHubOAuth{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://sync.example/callback", HTTPClient: client}
	if _, err := oauth.Exchange(context.Background(), "code"); err == nil || !strings.Contains(err.Error(), "verified primary email") {
		t.Fatalf("unverified email error = %v", err)
	}
}

func TestGitHubAuthorizationURLIncludesStateAndEmailScope(t *testing.T) {
	oauth := &GitHubOAuth{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://sync.example/callback"}
	got := oauth.AuthorizationURL("state-value")
	for _, value := range []string{"client_id=client", "state=state-value", "user%3Aemail"} {
		if !strings.Contains(got, value) {
			t.Fatalf("authorization URL %q missing %q", got, value)
		}
	}
}
