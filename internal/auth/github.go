package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubAPIURL       = "https://api.github.com"
)

type GitHubIdentity struct {
	UserID    string
	Login     string
	Email     string
	AvatarURL string
}

type GitHubOAuth struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
}

func (g *GitHubOAuth) Enabled() bool {
	return g != nil && strings.TrimSpace(g.ClientID) != "" && strings.TrimSpace(g.ClientSecret) != "" && strings.TrimSpace(g.RedirectURL) != ""
}

func (g *GitHubOAuth) AuthorizationURL(state string) string {
	query := url.Values{
		"client_id":    {g.ClientID},
		"redirect_uri": {g.RedirectURL},
		"scope":        {"read:user user:email"},
		"state":        {state},
	}
	return githubAuthorizeURL + "?" + query.Encode()
}

func (g *GitHubOAuth) Exchange(ctx context.Context, code string) (GitHubIdentity, error) {
	if !g.Enabled() || strings.TrimSpace(code) == "" {
		return GitHubIdentity{}, errors.New("GitHub OAuth is not configured")
	}
	client := g.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	token, err := g.exchangeToken(ctx, client, code)
	if err != nil {
		return GitHubIdentity{}, err
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := githubJSON(ctx, client, token, "/user", &user); err != nil {
		return GitHubIdentity{}, err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := githubJSON(ctx, client, token, "/user/emails", &emails); err != nil {
		return GitHubIdentity{}, err
	}
	verifiedEmail := ""
	for _, email := range emails {
		if email.Primary && email.Verified {
			verifiedEmail = strings.ToLower(strings.TrimSpace(email.Email))
			break
		}
	}
	if user.ID == 0 || verifiedEmail == "" {
		return GitHubIdentity{}, errors.New("GitHub account must have a verified primary email")
	}
	return GitHubIdentity{UserID: strconv.FormatInt(user.ID, 10), Login: user.Login, Email: verifiedEmail, AvatarURL: user.AvatarURL}, nil
}

func (g *GitHubOAuth) exchangeToken(ctx context.Context, client *http.Client, code string) (string, error) {
	form := url.Values{
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"code":          {code},
		"redirect_uri":  {g.RedirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var payload struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || payload.AccessToken == "" {
		return "", errors.New(strings.TrimSpace(payload.ErrorDescription + " " + payload.Error))
	}
	return payload.AccessToken, nil
}

func githubJSON(ctx context.Context, client *http.Client, token, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("GitHub API request failed")
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
