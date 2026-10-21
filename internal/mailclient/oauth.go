package mailclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type OAuthConfig struct{ Provider, ClientID, ClientSecret string }
type oauthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func oauthEndpoints(provider string) (auth, token, scope string, err error) {
	switch provider {
	case "Google":
		return "https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token", "https://mail.google.com/", nil
	case "Microsoft":
		return "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", "https://login.microsoftonline.com/common/oauth2/v2.0/token", "offline_access https://outlook.office.com/IMAP.AccessAsUser.All https://outlook.office.com/SMTP.Send", nil
	}
	return "", "", "", errors.New("choose Google or Microsoft")
}
func randomURLString() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Authorize opens the system browser and receives a single PKCE-protected loopback callback.
// openBrowser must marshal platform work to the UI thread when necessary.
func (s *Store) Authorize(ctx context.Context, account Account, config OAuthConfig, openBrowser func(string) error) error {
	if strings.TrimSpace(config.ClientID) == "" {
		return errors.New("enter the desktop application's OAuth client ID")
	}
	authURL, tokenURL, scope, err := oauthEndpoints(config.Provider)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	redirect := "http://" + listener.Addr().String() + "/oauth/callback"
	if config.Provider == "Microsoft" {
		redirect = "http://localhost:" + fmt.Sprint(listener.Addr().(*net.TCPAddr).Port) + "/oauth/callback"
	}
	state, verifier := randomURLString(), randomURLString()
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	result := make(chan string, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid authorization state.", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Authorization was denied. Return to Bentomail.", 400)
			once.Do(func() { result <- "" })
			return
		}
		once.Do(func() { result <- code })
		fmt.Fprint(w, "Signed in. You can close this tab and return to Bentomail.")
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(listener)
	defer srv.Close()
	values := url.Values{"client_id": {config.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {scope}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "login_hint": {account.Email}}
	if config.Provider == "Google" {
		values.Set("access_type", "offline")
		values.Set("prompt", "consent")
	}
	if err = openBrowser(authURL + "?" + values.Encode()); err != nil {
		return err
	}
	var code string
	select {
	case code = <-result:
	case <-ctx.Done():
		return ctx.Err()
	}
	if code == "" {
		return errors.New("authorization was denied")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {config.ClientID}, "code_verifier": {verifier}}
	if config.ClientSecret != "" {
		form.Set("client_secret", config.ClientSecret)
	}
	tokens, err := exchangeOAuth(ctx, tokenURL, form)
	if err != nil {
		return err
	}
	account.AuthMode = "oauth2"
	account.OAuthProvider = config.Provider
	account.OAuthClientID = config.ClientID
	if err = s.SaveAccount(account, ""); err != nil {
		return err
	}
	return s.saveOAuthTokens(account.ID, config.ClientSecret, tokens)
}
func exchangeOAuth(ctx context.Context, endpoint string, form url.Values) (oauthTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthTokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return oauthTokens{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return oauthTokens{}, err
	}
	if resp.StatusCode != 200 {
		return oauthTokens{}, fmt.Errorf("authorization server rejected the request (%d); verify the app registration and consent", resp.StatusCode)
	}
	var token oauthTokens
	if err = json.Unmarshal(data, &token); err != nil {
		return token, err
	}
	if token.AccessToken == "" {
		return token, errors.New("authorization response did not contain an access token")
	}
	return token, nil
}
func (s *Store) saveOAuthTokens(id, clientSecret string, t oauthTokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret := s.secrets[id]
	secret.AccessToken = t.AccessToken
	if t.RefreshToken != "" {
		secret.RefreshToken = t.RefreshToken
	}
	if clientSecret != "" {
		secret.ClientSecret = clientSecret
	}
	secret.Expires = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	s.secrets[id] = secret
	return s.persistSecretsLocked()
}
func (s *Store) oauthSecret(ctx context.Context, a Account) (string, error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	s.mu.RLock()
	secret := s.secrets[a.ID]
	s.mu.RUnlock()
	if secret.RefreshToken == "" || secret.Expires.IsZero() || time.Until(secret.Expires) > 2*time.Minute {
		return secret.AccessToken, nil
	}
	_, endpoint, scope, err := oauthEndpoints(a.OAuthProvider)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {secret.RefreshToken}, "client_id": {a.OAuthClientID}, "scope": {scope}}
	if secret.ClientSecret != "" {
		form.Set("client_secret", secret.ClientSecret)
	}
	token, err := exchangeOAuth(ctx, endpoint, form)
	if err != nil {
		return "", err
	}
	if err = s.saveOAuthTokens(a.ID, secret.ClientSecret, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}
