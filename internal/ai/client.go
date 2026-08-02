package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// chatClient is a tiny OpenAI-compatible chat-completions client. It is
// deliberately dependency-free (net/http only) so it works against any endpoint
// that speaks the OpenAI schema, including llama.cpp's server.
type chatClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func newChatClient(cfg *Config) *chatClient {
	return &chatClient{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		http:    &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	// ResponseFormat asks compatible servers to emit strict JSON. Endpoints that
	// don't understand it generally ignore it, and we parse defensively anyway.
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// complete sends a system+user prompt and returns the assistant's raw text.
func (c *chatClient) complete(ctx context.Context, system, user string) (string, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature:    0, // deterministic classification
		MaxTokens:      120,
		ResponseFormat: &responseFormat{Type: "json_object"},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read chat response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chat request returned %s: %s", res.Status, strings.TrimSpace(string(body)))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("failed to decode chat response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("chat endpoint error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("chat response contained no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
