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

func TestChatSessionMessagesEndpointIncludesToolMetadata(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, chatstore.Message{
		Role:       "tool",
		Content:    `{"tool":"fs.list","id":"tool-json-1","output":"{\"entries\":[\"a.txt\"]}"}`,
		RunID:      "run_42",
		ToolCallID: "tool-json-1",
		ToolName:   "fs.list",
	}); err != nil {
		t.Fatalf("append tool message: %v", err)
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
	msg, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected message shape: %#v", msgs[0])
	}
	if msg["role"] != "tool" {
		t.Fatalf("expected role=tool, got %#v", msg["role"])
	}
	if msg["tool_name"] != "fs.list" || msg["tool_call_id"] != "tool-json-1" {
		t.Fatalf("expected tool metadata to round-trip, got %#v", msg)
	}
	if msg["run_id"] != "run_42" {
		t.Fatalf("expected run id to round-trip, got %#v", msg["run_id"])
	}
}

func TestChatSessionMessagesEndpointPreservesMultiStepOrder(t *testing.T) {
	root := t.TempDir()
	store, err := chatstore.NewStore(filepath.Join(root, ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sequence := []chatstore.Message{
		{Role: "user", Content: "list files"},
		{Role: "tool", Content: `{"tool":"fs.list","id":"tool-json-1","output":"{\"entries\":[\"a.txt\"]}"}`, ToolCallID: "tool-json-1", ToolName: "fs.list", RunID: "run_1"},
		{Role: "tool", Content: `{"tool":"fs.read","id":"tool-json-2","output":"hello"}`, ToolCallID: "tool-json-2", ToolName: "fs.read", RunID: "run_1"},
		{Role: "assistant", Content: "I found a.txt and read it."},
	}
	for _, msg := range sequence {
		if err := store.AppendMessage(session.SessionID, msg); err != nil {
			t.Fatalf("append message: %v", err)
		}
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
	if !ok || len(msgs) != 4 {
		t.Fatalf("expected four messages, got %#v", payload["messages"])
	}

	roleAt := func(i int) string {
		item, _ := msgs[i].(map[string]any)
		if item == nil {
			return ""
		}
		v, _ := item["role"].(string)
		return v
	}
	if roleAt(0) != "user" || roleAt(1) != "tool" || roleAt(2) != "tool" || roleAt(3) != "assistant" {
		t.Fatalf("unexpected message ordering: %#v", msgs)
	}
	tool1, _ := msgs[1].(map[string]any)
	tool2, _ := msgs[2].(map[string]any)
	if tool1["tool_call_id"] != "tool-json-1" || tool2["tool_call_id"] != "tool-json-2" {
		t.Fatalf("expected distinct tool call ids in order, got %#v and %#v", tool1, tool2)
	}
}
