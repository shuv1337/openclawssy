package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
)

func TestDebugRunTraceEndpoint(t *testing.T) {
	store := httpchannel.NewInMemoryRunStore()
	_, err := store.Create(context.Background(), httpchannel.Run{
		ID:        "run_1",
		AgentID:   "default",
		Message:   "hello",
		Status:    "completed",
		Trace:     map[string]any{"run_id": "run_1", "prompt_length": float64(42)},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	h := New(".", store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/debug/runs/run_1/trace", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	trace, ok := payload["trace"].(map[string]any)
	if !ok {
		t.Fatalf("expected trace map, got %#v", payload["trace"])
	}
	if trace["run_id"] != "run_1" {
		t.Fatalf("unexpected run_id in trace: %#v", trace["run_id"])
	}
}

func TestDebugRunTraceEndpointReturnsNotFoundWithoutTrace(t *testing.T) {
	store := httpchannel.NewInMemoryRunStore()
	_, err := store.Create(context.Background(), httpchannel.Run{ID: "run_2", AgentID: "default", Message: "hello", Status: "completed", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	h := New(".", store)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/debug/runs/run_2/trace", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestListChatSessionsEndpoint(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	_, err = store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions?agent_id=default&user_id=dashboard_user&room_id=dashboard&channel=dashboard", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	sessions, ok := payload["sessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("expected one session, got %#v", payload["sessions"])
	}
}

func TestChatSessionMessagesEndpoint(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	h := New(root, httpchannel.NewInMemoryRunStore())
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/chat/sessions/"+session.SessionID+"/messages?limit=10", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected one message, got %#v", payload["messages"])
	}
}
