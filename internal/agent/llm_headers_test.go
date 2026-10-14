package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/the-heaven-labs/aether/internal/crypto"
)

func testMasterKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestLLMClientSendsCustomHeaders(t *testing.T) {
	masterKey := testMasterKey()
	apiKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	secret, _ := crypto.EncryptString("topsecret", masterKey)

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer srv.Close()

	client := NewLLMClient(srv.URL, "gpt-4", apiKey, nil,
		map[string]any{"X-Title": "Aether", "X-Api-Key": secret}, []string{"X-Api-Key"})

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, masterKey); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got.Get("X-Title") != "Aether" {
		t.Fatalf("expected X-Title header, got %q", got.Get("X-Title"))
	}
	if got.Get("X-Api-Key") != "topsecret" {
		t.Fatalf("expected decrypted secret header, got %q", got.Get("X-Api-Key"))
	}
	if got.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("expected bearer auth, got %q", got.Get("Authorization"))
	}
	if got.Get("Content-Type") != "application/json" {
		t.Fatalf("expected JSON content type, got %q", got.Get("Content-Type"))
	}
}

func TestLLMClientCustomHeaderOverridesAuthorization(t *testing.T) {
	masterKey := testMasterKey()
	apiKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer srv.Close()

	client := NewLLMClient(srv.URL, "gpt-4", apiKey, nil,
		map[string]any{"Authorization": "Custom gateway-auth"}, nil)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, masterKey); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got.Get("Authorization") != "Custom gateway-auth" {
		t.Fatalf("expected custom Authorization to win, got %q", got.Get("Authorization"))
	}
}

func TestLLMClientHeaderDecryptFailureFailsClosed(t *testing.T) {
	masterKey := testMasterKey()
	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(255 - i)
	}
	// The API key must decrypt with the key Chat receives; only the header
	// secret is encrypted under masterKey, so decryption fails.
	apiKey, _ := crypto.Encrypt([]byte("sk-test"), wrongKey)
	secret, _ := crypto.EncryptString("topsecret", masterKey)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewLLMClient(srv.URL, "gpt-4", apiKey, nil,
		map[string]any{"X-Api-Key": secret}, []string{"X-Api-Key"})

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, wrongKey); err == nil {
		t.Fatal("expected error when custom header decryption fails")
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("expected no HTTP requests, got %d", got)
	}
}

func TestLLMClientReappliesHeadersOnRetry(t *testing.T) {
	masterKey := testMasterKey()
	apiKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)

	var mu sync.Mutex
	var attempts []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts = append(attempts, r.Header.Clone())
		n := len(attempts)
		mu.Unlock()

		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer srv.Close()

	client := NewLLMClient(srv.URL, "gpt-4", apiKey, nil,
		map[string]any{"X-Trace": "retry-me"}, nil)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, masterKey); err != nil {
		t.Fatalf("chat: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}
	for i, h := range attempts {
		if h.Get("X-Trace") != "retry-me" {
			t.Fatalf("attempt %d missing custom header, got %q", i+1, h.Get("X-Trace"))
		}
	}
}

func TestLLMClientChatStreamSendsCustomHeaders(t *testing.T) {
	masterKey := testMasterKey()
	apiKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	secret, _ := crypto.EncryptString("topsecret", masterKey)

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		// Two whitespace-separated JSON objects: the shape ChatStream's token
		// decoder consumes (no SSE framing).
		w.Write([]byte(`{"choices":[{"delta":{"content":"ok"}}]}` + "\n" + `{"choices":[{"delta":{"content":"done"}}]}`))
	}))
	defer srv.Close()

	client := NewLLMClient(srv.URL, "gpt-4", apiKey, nil,
		map[string]any{"X-Title": "Aether", "X-Api-Key": secret}, []string{"X-Api-Key"})

	if err := client.ChatStream(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, masterKey, func(string) {}); err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	if got.Get("X-Title") != "Aether" {
		t.Fatalf("expected X-Title header, got %q", got.Get("X-Title"))
	}
	if got.Get("X-Api-Key") != "topsecret" {
		t.Fatalf("expected decrypted secret header, got %q", got.Get("X-Api-Key"))
	}
	if got.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("expected bearer auth, got %q", got.Get("Authorization"))
	}
}
