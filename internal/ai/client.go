package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
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
	// plainRequests is set once the endpoint has rejected the reasoning-off
	// parameters, so subsequent calls stop sending them.
	plainRequests atomic.Bool
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
	// The next three all mean "don't think", spelled the way different
	// OpenAI-compatible servers expect it. See reasoningOff.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	Think              *bool          `json:"think,omitempty"`
	ReasoningEffort    string         `json:"reasoning_effort,omitempty"`
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

// httpError is a non-200 reply from the chat endpoint.
type httpError struct {
	code   int
	status string
	body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("chat request returned %s: %s", e.status, e.body)
}

// defaultMaxTokens bounds a single-label reply. Per-item calls need more and
// pass their own budget via completeWithTokens.
const defaultMaxTokens = 120

// complete sends a system+user prompt and returns the assistant's raw text.
func (c *chatClient) complete(ctx context.Context, system, user string) (string, error) {
	return c.completeWithTokens(ctx, system, user, defaultMaxTokens)
}

// completeWithTokens is complete with an explicit response budget. Too small a
// budget truncates the JSON mid-object and the reply fails to parse, so callers
// that ask for one answer per line must size this to the number of lines.
//
// Reasoning is switched off (see reasoningOff). If the endpoint rejects those
// parameters, the call is retried once without them and they are dropped for
// the rest of the run.
func (c *chatClient) completeWithTokens(ctx context.Context, system, user string, maxTokens int) (string, error) {
	if c.plainRequests.Load() {
		return c.post(ctx, system, user, maxTokens, false)
	}
	out, err := c.post(ctx, system, user, maxTokens, true)
	var he *httpError
	if errors.As(err, &he) && he.code == http.StatusBadRequest {
		// The reasoning-off knobs are the only non-standard thing we send, so a
		// rejected request is almost certainly them.
		c.plainRequests.Store(true)
		return c.post(ctx, system, user, maxTokens, false)
	}
	return out, err
}

// reasoningOff applies the "don't think" parameters to req. Thinking models
// otherwise spend the whole token budget on their scratchpad and return empty
// content, which fails to parse as JSON. Each server spells the switch
// differently and ignores the spellings it doesn't know, so all three go out:
// chat_template_kwargs is understood by llama.cpp/vLLM/SGLang-hosted templates
// (Qwen3 and friends), think by Ollama, and reasoning_effort by OpenAI-style
// APIs.
func reasoningOff(req *chatRequest) {
	no := false
	req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	req.Think = &no
	req.ReasoningEffort = "none"
}

func (c *chatClient) post(ctx context.Context, system, user string, maxTokens int, disableReasoning bool) (string, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature:    0, // deterministic classification
		MaxTokens:      maxTokens,
		ResponseFormat: &responseFormat{Type: "json_object"},
	}
	if disableReasoning {
		reasoningOff(&reqBody)
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
		return "", &httpError{code: res.StatusCode, status: res.Status, body: strings.TrimSpace(string(body))}
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
	// Servers that keep reasoning inside content need it stripped here; those
	// that ignored the switches entirely leave nothing behind but the thought.
	content := stripThinking(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("chat response contained no answer — the model likely spent its %d-token budget on reasoning; use a non-thinking model or raise the budget", maxTokens)
	}
	return content, nil
}

// thinkTags wrap the scratchpad a reasoning model emits when the server passes
// it through in the content field.
var thinkTags = []string{"think", "thinking", "reasoning"}

// stripThinking removes reasoning blocks from raw model text. An unclosed block
// (the reply was cut off mid-thought) takes everything after the opening tag
// with it.
func stripThinking(raw string) string {
	for _, tag := range thinkTags {
		open, closing := "<"+tag+">", "</"+tag+">"
		for {
			lower := strings.ToLower(raw)
			start := strings.Index(lower, open)
			if start < 0 {
				break
			}
			end := strings.Index(lower[start:], closing)
			if end < 0 {
				raw = raw[:start]
				break
			}
			raw = raw[:start] + raw[start+end+len(closing):]
		}
	}
	return strings.TrimSpace(raw)
}
