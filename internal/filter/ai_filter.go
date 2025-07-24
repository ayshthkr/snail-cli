package filter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"snail-cli/internal/interfaces"
)

// AIFilter implements the Filter interface for AI-powered email processing
type AIFilter struct {
	name        string
	description string
	provider    string
	model       string
	prompt      string
	events      []interfaces.FilterEvent
	enabled     bool
	config      map[string]interface{}
	client      AIClient
}

// AIClient defines the interface for AI service providers
type AIClient interface {
	ProcessEmail(ctx context.Context, email *interfaces.Email, prompt string) (*AIResponse, error)
	GetProvider() string
	ValidateConfig(config map[string]interface{}) error
}

// AIResponse contains the response from an AI service
type AIResponse struct {
	Summary        string                 `json:"summary,omitempty"`
	Classification string                 `json:"classification,omitempty"`
	Labels         []string               `json:"labels,omitempty"`
	Actions        []interfaces.FilterAction `json:"actions,omitempty"`
	Confidence     float64                `json:"confidence,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// NewAIFilter creates a new AI filter
func NewAIFilter(config *interfaces.AIFilter) (*AIFilter, error) {
	if config == nil {
		return nil, fmt.Errorf("AI filter config cannot be nil")
	}

	if config.FilterName == "" {
		return nil, fmt.Errorf("filter name cannot be empty")
	}

	if config.Provider == "" {
		return nil, fmt.Errorf("AI provider cannot be empty")
	}

	if config.Model == "" {
		return nil, fmt.Errorf("AI model cannot be empty")
	}

	if config.Prompt == "" {
		return nil, fmt.Errorf("AI prompt cannot be empty")
	}

	// Create AI client based on provider
	client, err := createAIClient(config.Provider, config.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create AI client: %w", err)
	}

	return &AIFilter{
		name:        config.FilterName,
		description: config.FilterDesc,
		provider:    config.Provider,
		model:       config.Model,
		prompt:      config.Prompt,
		events:      config.FilterEvents,
		enabled:     config.Enabled,
		config:      config.Config,
		client:      client,
	}, nil
}

// Name returns the filter name
func (af *AIFilter) Name() string {
	return af.name
}

// Description returns the filter description
func (af *AIFilter) Description() string {
	return af.description
}

// Events returns the events this filter handles
func (af *AIFilter) Events() []interfaces.FilterEvent {
	return af.events
}

// Execute processes an email using AI
func (af *AIFilter) Execute(ctx context.Context, email *interfaces.Email, event interfaces.FilterEvent) (*interfaces.FilterResult, error) {
	start := time.Now()

	// Create timeout context for AI processing
	aiCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Process email with AI
	response, err := af.client.ProcessEmail(aiCtx, email, af.prompt)
	if err != nil {
		return &interfaces.FilterResult{
			Modified: false,
			Email:    email,
			Actions:  []interfaces.FilterAction{},
			Labels:   []string{},
			Error:    err,
			Duration: time.Since(start),
		}, err
	}

	// Create filter result from AI response
	result := &interfaces.FilterResult{
		Modified: len(response.Actions) > 0 || len(response.Labels) > 0,
		Email:    email,
		Actions:  response.Actions,
		Labels:   response.Labels,
		Duration: time.Since(start),
	}

	// Add AI metadata to email headers if summary is provided
	if response.Summary != "" {
		if email.Headers == nil {
			email.Headers = make(map[string]string)
		}
		email.Headers["X-AI-Summary"] = response.Summary
		email.Headers["X-AI-Provider"] = af.provider
		email.Headers["X-AI-Model"] = af.model
		email.Headers["X-AI-Confidence"] = fmt.Sprintf("%.2f", response.Confidence)
		result.Modified = true
	}

	// Add classification as label if provided
	if response.Classification != "" {
		result.Labels = append(result.Labels, fmt.Sprintf("ai-class:%s", response.Classification))
		result.Modified = true
	}

	return result, nil
}

// IsEnabled returns true if the filter is enabled
func (af *AIFilter) IsEnabled() bool {
	return af.enabled
}

// SetEnabled enables or disables the filter
func (af *AIFilter) SetEnabled(enabled bool) {
	af.enabled = enabled
}

// createAIClient creates an AI client based on the provider
func createAIClient(provider string, config map[string]interface{}) (AIClient, error) {
	switch strings.ToLower(provider) {
	case "openai":
		return NewOpenAIClient(config)
	case "anthropic":
		return NewAnthropicClient(config)
	case "mock":
		return NewMockAIClient(config)
	default:
		return nil, fmt.Errorf("unsupported AI provider: %s", provider)
	}
}

// OpenAIClient implements AIClient for OpenAI services
type OpenAIClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(config map[string]interface{}) (*OpenAIClient, error) {
	apiKey, ok := config["api_key"].(string)
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}

	baseURL, ok := config["base_url"].(string)
	if !ok || baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ProcessEmail processes an email using OpenAI
func (c *OpenAIClient) ProcessEmail(ctx context.Context, email *interfaces.Email, prompt string) (*AIResponse, error) {
	// Prepare email content for AI processing
	emailContent := fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n\n%s",
		c.formatAddress(email.From),
		c.formatAddresses(email.To),
		email.Subject,
		email.Body)

	// Create OpenAI request
	requestBody := map[string]interface{}{
		"model": "gpt-3.5-turbo",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": prompt,
			},
			{
				"role":    "user",
				"content": emailContent,
			},
		},
		"max_tokens":   500,
		"temperature":  0.3,
		"response_format": map[string]string{"type": "json_object"},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	// Parse AI response JSON
	var aiResponse AIResponse
	if err := json.Unmarshal([]byte(openAIResp.Choices[0].Message.Content), &aiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return &aiResponse, nil
}

// GetProvider returns the provider name
func (c *OpenAIClient) GetProvider() string {
	return "openai"
}

// ValidateConfig validates the OpenAI configuration
func (c *OpenAIClient) ValidateConfig(config map[string]interface{}) error {
	if _, ok := config["api_key"].(string); !ok {
		return fmt.Errorf("api_key is required for OpenAI")
	}
	return nil
}

// formatAddress formats an address for display
func (c *OpenAIClient) formatAddress(addr interfaces.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

// formatAddresses formats multiple addresses for display
func (c *OpenAIClient) formatAddresses(addrs []interfaces.Address) string {
	formatted := make([]string, len(addrs))
	for i, addr := range addrs {
		formatted[i] = c.formatAddress(addr)
	}
	return strings.Join(formatted, ", ")
}

// AnthropicClient implements AIClient for Anthropic services
type AnthropicClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewAnthropicClient creates a new Anthropic client
func NewAnthropicClient(config map[string]interface{}) (*AnthropicClient, error) {
	apiKey, ok := config["api_key"].(string)
	if !ok || apiKey == "" {
		return nil, fmt.Errorf("Anthropic API key is required")
	}

	baseURL, ok := config["base_url"].(string)
	if !ok || baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}

	return &AnthropicClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ProcessEmail processes an email using Anthropic
func (c *AnthropicClient) ProcessEmail(ctx context.Context, email *interfaces.Email, prompt string) (*AIResponse, error) {
	// Prepare email content for AI processing
	emailContent := fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n\n%s",
		c.formatAddress(email.From),
		c.formatAddresses(email.To),
		email.Subject,
		email.Body)

	// Create Anthropic request
	requestBody := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 500,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": fmt.Sprintf("%s\n\nEmail to process:\n%s", prompt, emailContent),
			},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var anthropicResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("no response from Anthropic")
	}

	// Parse AI response JSON
	var aiResponse AIResponse
	if err := json.Unmarshal([]byte(anthropicResp.Content[0].Text), &aiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return &aiResponse, nil
}

// GetProvider returns the provider name
func (c *AnthropicClient) GetProvider() string {
	return "anthropic"
}

// ValidateConfig validates the Anthropic configuration
func (c *AnthropicClient) ValidateConfig(config map[string]interface{}) error {
	if _, ok := config["api_key"].(string); !ok {
		return fmt.Errorf("api_key is required for Anthropic")
	}
	return nil
}

// formatAddress formats an address for display
func (c *AnthropicClient) formatAddress(addr interfaces.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

// formatAddresses formats multiple addresses for display
func (c *AnthropicClient) formatAddresses(addrs []interfaces.Address) string {
	formatted := make([]string, len(addrs))
	for i, addr := range addrs {
		formatted[i] = c.formatAddress(addr)
	}
	return strings.Join(formatted, ", ")
}

// MockAIClient implements AIClient for testing purposes
type MockAIClient struct {
	responses map[string]*AIResponse
	delay     time.Duration
}

// NewMockAIClient creates a new mock AI client
func NewMockAIClient(config map[string]interface{}) (*MockAIClient, error) {
	delay := 100 * time.Millisecond
	if d, ok := config["delay"].(time.Duration); ok {
		delay = d
	}

	responses := make(map[string]*AIResponse)
	
	// Default responses for testing
	responses["default"] = &AIResponse{
		Summary:        "This is a mock AI summary of the email.",
		Classification: "general",
		Labels:         []string{"ai-processed", "mock"},
		Actions:        []interfaces.FilterAction{},
		Confidence:     0.85,
		Metadata:       map[string]interface{}{"provider": "mock"},
	}

	return &MockAIClient{
		responses: responses,
		delay:     delay,
	}, nil
}

// ProcessEmail processes an email using mock AI
func (c *MockAIClient) ProcessEmail(ctx context.Context, email *interfaces.Email, prompt string) (*AIResponse, error) {
	// Simulate processing delay
	select {
	case <-time.After(c.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Return mock response based on email subject or default
	if response, ok := c.responses[email.Subject]; ok {
		return response, nil
	}

	return c.responses["default"], nil
}

// GetProvider returns the provider name
func (c *MockAIClient) GetProvider() string {
	return "mock"
}

// ValidateConfig validates the mock configuration
func (c *MockAIClient) ValidateConfig(config map[string]interface{}) error {
	return nil
}

// SetMockResponse sets a mock response for a specific email subject
func (c *MockAIClient) SetMockResponse(subject string, response *AIResponse) {
	c.responses[subject] = response
}