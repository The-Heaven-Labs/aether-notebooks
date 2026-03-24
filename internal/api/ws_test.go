package api_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketBroadcast(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ts2 := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("ws-broadcast-%d@example.com", ts2), "WS Org")
	nbID := createNotebook(t, srv, token, "WS Notebook")

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/notebooks/" + nbID
	header := map[string][]string{"Authorization": {"Bearer " + token}}

	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial ws conn1: %v", err)
	}
	defer conn1.Close()

	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial ws conn2: %v", err)
	}
	defer conn2.Close()

	testMsg := map[string]interface{}{
		"type":    "cell_output",
		"cell_id": "test-cell",
		"data":    "hello",
	}
	srv.Hub().Broadcast(nbID, testMsg)

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d read: %v", i+1, err)
		}
		var received map[string]interface{}
		if err := json.Unmarshal(data, &received); err != nil {
			t.Fatalf("client %d unmarshal: %v", i+1, err)
		}
		if received["type"] != "cell_output" {
			t.Fatalf("client %d: expected type cell_output, got %v", i+1, received["type"])
		}
	}
}

func TestWebSocketLeaveRoom(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	token := registerAndGetToken(t, srv, fmt.Sprintf("ws-leave-%d@example.com", time.Now().UnixNano()), "WS Leave Org")
	nbID := createNotebook(t, srv, token, "Leave NB")

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/notebooks/" + nbID
	header := map[string][]string{"Authorization": {"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	conn.Close()
	time.Sleep(50 * time.Millisecond)

	// Broadcast after client left — should not panic
	srv.Hub().Broadcast(nbID, map[string]string{"type": "ping"})
}
