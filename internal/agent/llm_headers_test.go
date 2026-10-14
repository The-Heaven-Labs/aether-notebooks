package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
