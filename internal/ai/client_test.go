package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *chatClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newChatClient(&Config{BaseURL: srv.URL, Model: "test-model", TimeoutSeconds: 5})
}

func decodeRequest(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return got
}

func writeContent(w http.ResponseWriter, content string) {
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
	})
}

func TestCompleteDisablesReasoning(t *testing.T) {
	var got map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = decodeRequest(t, r)
		writeContent(w, `{"category":"Groceries"}`)
	})

	out, err := c.complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if out != `{"category":"Groceries"}` {
		t.Fatalf("unexpected output %q", out)
	}
	if got["reasoning_effort"] != "none" {
		t.Errorf("reasoning_effort = %v, want none", got["reasoning_effort"])
	}
	if got["think"] != false {
		t.Errorf("think = %v, want false", got["think"])
	}
	kwargs, ok := got["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != false {
		t.Errorf("chat_template_kwargs = %v, want enable_thinking false", got["chat_template_kwargs"])
	}
}

// A strict endpoint (OpenAI itself) rejects the non-standard knobs; the call
// must succeed anyway and later calls must skip them.
func TestCompleteRetriesWithoutReasoningKnobs(t *testing.T) {
	var requests []map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		got := decodeRequest(t, r)
		requests = append(requests, got)
		if _, ok := got["chat_template_kwargs"]; ok {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Unrecognized request argument supplied: chat_template_kwargs"}}`)
			return
		}
		writeContent(w, `{"category":"Groceries"}`)
	})

	for i := range 2 {
		out, err := c.complete(context.Background(), "sys", "user")
		if err != nil {
			t.Fatalf("complete #%d: %v", i+1, err)
		}
		if out != `{"category":"Groceries"}` {
			t.Fatalf("complete #%d: unexpected output %q", i+1, out)
		}
	}

	// rejected, retried without knobs, then a single plain request.
	if len(requests) != 3 {
		t.Fatalf("made %d requests, want 3", len(requests))
	}
	for i, req := range requests[1:] {
		if _, ok := req["think"]; ok {
			t.Errorf("request %d still carried reasoning knobs", i+2)
		}
	}
}

func TestCompleteSurfacesNonReasoningErrors(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "bad key")
	})

	if _, err := c.complete(context.Background(), "sys", "user"); err == nil ||
		!strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("err = %v, want the 401 body", err)
	}
}

// An all-reasoning reply used to surface as `could not parse model output ""`.
func TestCompleteRejectsEmptyAnswer(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeContent(w, "<think>The merchant looks like a supermarket, so")
	})

	_, err := c.complete(context.Background(), "sys", "user")
	if err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("err = %v, want a no-answer error", err)
	}
}

func TestStripThinking(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"category":"Food"}`, `{"category":"Food"}`},
		{"closed block", "<think>hmm</think>\n{\"category\":\"Food\"}", `{"category":"Food"}`},
		{"trailing block", `{"category":"Food"}<thinking>done</thinking>`, `{"category":"Food"}`},
		{"unclosed block", "{\"category\":\"Food\"}\n<think>wait, maybe", `{"category":"Food"}`},
		{"several blocks", "<think>a</think>{\"n\":1}<reasoning>b</reasoning>", `{"n":1}`},
		{"only thinking", "<think>a</think>", ""},
		{"uppercase tag", "<THINK>a</THINK>{\"n\":1}", `{"n":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripThinking(tt.in); got != tt.want {
				t.Errorf("stripThinking(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
