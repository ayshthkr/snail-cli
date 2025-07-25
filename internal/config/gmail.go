package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GmailAuthManager handles Gmail-specific authentication and configuration
type GmailAuthManager struct {
	credManager *CredentialManager
	config      *oauth2.Config
}

// OAuth2Token represents an OAuth2 token with additional metadata
type OAuth2Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	Scope        string    `json:"scope"`
}

// GmailOAuth2Config contains OAuth2 configuration for Gmail
type GmailOAuth2Config struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
}

// DefaultGmailScopes returns the default Gmail API scopes
func DefaultGmailScopes() []string {
	return []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.modify",
		"https://www.googleapis.com/auth/gmail.labels",
	}
}

// NewGmailAuthManager creates a new Gmail authentication manager
func NewGmailAuthManager(credManager *CredentialManager, oauth2Config *GmailOAuth2Config) *GmailAuthManager {
	if oauth2Config == nil {
		oauth2Config = &GmailOAuth2Config{
			RedirectURL: "http://localhost:8080/callback",
			Scopes:      DefaultGmailScopes(),
		}
	}

	config := &oauth2.Config{
		ClientID:     oauth2Config.ClientID,
		ClientSecret: oauth2Config.ClientSecret,
		RedirectURL:  oauth2Config.RedirectURL,
		Scopes:       oauth2Config.Scopes,
		Endpoint:     google.Endpoint,
	}

	return &GmailAuthManager{
		credManager: credManager,
		config:      config,
	}
}

// SetOAuth2Config updates the OAuth2 configuration
func (gam *GmailAuthManager) SetOAuth2Config(oauth2Config *GmailOAuth2Config) {
	gam.config.ClientID = oauth2Config.ClientID
	gam.config.ClientSecret = oauth2Config.ClientSecret
	gam.config.RedirectURL = oauth2Config.RedirectURL
	gam.config.Scopes = oauth2Config.Scopes
}

// GetAuthURL generates an OAuth2 authorization URL
func (gam *GmailAuthManager) GetAuthURL(state string) string {
	return gam.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// ExchangeCodeForToken exchanges an authorization code for an access token
func (gam *GmailAuthManager) ExchangeCodeForToken(ctx context.Context, code string) (*OAuth2Token, error) {
	token, err := gam.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}

	oauth2Token := &OAuth2Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
	}

	// Store the scope information if available
	if extra := token.Extra("scope"); extra != nil {
		if scope, ok := extra.(string); ok {
			oauth2Token.Scope = scope
		}
	}

	return oauth2Token, nil
}

// StoreToken stores an OAuth2 token for a user
func (gam *GmailAuthManager) StoreToken(username string, token *OAuth2Token) error {
	creds := &Credentials{
		Username:     username,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresAt:    token.Expiry.Unix(),
	}

	return gam.credManager.Store(username, creds)
}

// GetToken retrieves and validates an OAuth2 token for a user
func (gam *GmailAuthManager) GetToken(username string) (*OAuth2Token, error) {
	creds, err := gam.credManager.Retrieve(username)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve credentials: %w", err)
	}

	token := &OAuth2Token{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		TokenType:    creds.TokenType,
		Expiry:       time.Unix(creds.ExpiresAt, 0),
	}

	// Check if token needs refresh
	if token.IsExpired() && token.RefreshToken != "" {
		refreshedToken, err := gam.RefreshToken(username, token)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}
		return refreshedToken, nil
	}

	return token, nil
}

// RefreshToken refreshes an expired OAuth2 token
func (gam *GmailAuthManager) RefreshToken(username string, token *OAuth2Token) (*OAuth2Token, error) {
	oauth2Token := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
	}

	tokenSource := gam.config.TokenSource(context.Background(), oauth2Token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	refreshedToken := &OAuth2Token{
		AccessToken:  newToken.AccessToken,
		RefreshToken: newToken.RefreshToken,
		TokenType:    newToken.TokenType,
		Expiry:       newToken.Expiry,
	}

	// If refresh token is empty, keep the original one
	if refreshedToken.RefreshToken == "" {
		refreshedToken.RefreshToken = token.RefreshToken
	}

	// Store the refreshed token
	if err := gam.StoreToken(username, refreshedToken); err != nil {
		return nil, fmt.Errorf("failed to store refreshed token: %w", err)
	}

	return refreshedToken, nil
}

// ValidateToken validates an OAuth2 token by making a test API call
func (gam *GmailAuthManager) ValidateToken(token *OAuth2Token) error {
	oauth2Token := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
	}

	client := gam.config.Client(context.Background(), oauth2Token)
	
	// Make a simple API call to validate the token
	resp, err := client.Get("https://www.googleapis.com/gmail/v1/users/me/profile")
	if err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token validation failed with status: %d", resp.StatusCode)
	}

	return nil
}

// RevokeToken revokes an OAuth2 token
func (gam *GmailAuthManager) RevokeToken(username string) error {
	token, err := gam.GetToken(username)
	if err != nil {
		return fmt.Errorf("failed to get token for revocation: %w", err)
	}

	// Revoke the token with Google
	revokeURL := "https://oauth2.googleapis.com/revoke"
	data := url.Values{}
	data.Set("token", token.AccessToken)

	resp, err := http.PostForm(revokeURL, data)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token revocation failed with status: %d", resp.StatusCode)
	}

	// Delete the stored credentials
	return gam.credManager.Delete(username)
}

// GetHTTPClient returns an HTTP client configured with OAuth2 authentication
func (gam *GmailAuthManager) GetHTTPClient(username string) (*http.Client, error) {
	token, err := gam.GetToken(username)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	oauth2Token := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
	}

	return gam.config.Client(context.Background(), oauth2Token), nil
}

// IsExpired checks if the OAuth2 token is expired
func (t *OAuth2Token) IsExpired() bool {
	if t.Expiry.IsZero() {
		return false
	}
	return t.Expiry.Before(time.Now().Add(-10 * time.Second)) // 10 second buffer
}

// ToJSON converts the OAuth2 token to JSON
func (t *OAuth2Token) ToJSON() ([]byte, error) {
	return json.Marshal(t)
}

// FromJSON creates an OAuth2 token from JSON
func OAuth2TokenFromJSON(data []byte) (*OAuth2Token, error) {
	var token OAuth2Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OAuth2 token: %w", err)
	}
	return &token, nil
}

// GmailConfigValidator validates Gmail-specific configuration
type GmailConfigValidator struct{}

// NewGmailConfigValidator creates a new Gmail configuration validator
func NewGmailConfigValidator() *GmailConfigValidator {
	return &GmailConfigValidator{}
}

// ValidateGmailConfig validates Gmail configuration settings
func (gcv *GmailConfigValidator) ValidateGmailConfig(config *GmailConfig) error {
	if config.Username == "" {
		return fmt.Errorf("Gmail username is required")
	}

	// Validate email format
	if !isValidEmail(config.Username) {
		return fmt.Errorf("invalid Gmail username format: %s", config.Username)
	}

	// Validate server settings
	if config.IMAPServer == "" {
		return fmt.Errorf("IMAP server is required")
	}
	if config.SMTPServer == "" {
		return fmt.Errorf("SMTP server is required")
	}

	// Validate ports
	if config.IMAPPort <= 0 || config.IMAPPort > 65535 {
		return fmt.Errorf("invalid IMAP port: %d", config.IMAPPort)
	}
	if config.SMTPPort <= 0 || config.SMTPPort > 65535 {
		return fmt.Errorf("invalid SMTP port: %d", config.SMTPPort)
	}

	// Validate auth method
	if config.AuthMethod != "oauth2" && config.AuthMethod != "password" {
		return fmt.Errorf("invalid auth method: %s (must be 'oauth2' or 'password')", config.AuthMethod)
	}

	return nil
}

// ValidateOAuth2Config validates OAuth2 configuration
func (gcv *GmailConfigValidator) ValidateOAuth2Config(config *GmailOAuth2Config) error {
	if config.ClientID == "" {
		return fmt.Errorf("OAuth2 client ID is required")
	}
	if config.ClientSecret == "" {
		return fmt.Errorf("OAuth2 client secret is required")
	}
	if config.RedirectURL == "" {
		return fmt.Errorf("OAuth2 redirect URL is required")
	}

	// Validate redirect URL format
	parsedURL, err := url.Parse(config.RedirectURL)
	if err != nil {
		return fmt.Errorf("invalid redirect URL: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("redirect URL must have scheme and host")
	}

	// Validate scopes
	if len(config.Scopes) == 0 {
		return fmt.Errorf("at least one OAuth2 scope is required")
	}

	return nil
}

// isValidEmail performs basic email validation
func isValidEmail(email string) bool {
	// Basic email validation - just check for @ symbol and basic structure
	if len(email) < 3 {
		return false
	}
	
	atIndex := -1
	for i, char := range email {
		if char == '@' {
			if atIndex != -1 {
				return false // Multiple @ symbols
			}
			atIndex = i
		}
	}
	
	if atIndex <= 0 || atIndex >= len(email)-1 {
		return false // @ at beginning or end
	}
	
	return true
}

// SetupGmailDefaults sets up default Gmail configuration
func SetupGmailDefaults(config *GmailConfig) {
	if config.IMAPServer == "" {
		config.IMAPServer = "imap.gmail.com"
	}
	if config.SMTPServer == "" {
		config.SMTPServer = "smtp.gmail.com"
	}
	if config.IMAPPort == 0 {
		config.IMAPPort = 993
	}
	if config.SMTPPort == 0 {
		config.SMTPPort = 587
	}
	if config.AuthMethod == "" {
		config.AuthMethod = "oauth2"
	}
	if !config.UseTLS && !config.UseStartTLS {
		config.UseTLS = true
		config.UseStartTLS = true
	}
}