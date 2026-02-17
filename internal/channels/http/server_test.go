package httpchannel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testChatConnector struct {
	err      error
	response ChatResponse
}

func (c testChatConnector) HandleMessage(ctx context.Context, msg ChatMessage) (ChatResponse, error) {
	_ = ctx
	_ = msg
	if c.err != nil {
		return ChatResponse{}, c.err
	}
	if c.response.ID != "" || c.response.Response != "" || c.response.Status != "" {
		return c.response, nil
	}
	return ChatResponse{ID: "run-chat", Status: "queued"}, nil
}

func TestServer_DefaultAddrIsLoopback(t *testing.T) {
	s := NewServer(Config{BearerToken: "token"})
	if s.Addr() != "127.0.0.1:8080" {
		t.Fatalf("expected loopback default addr, got %q", s.Addr())
	}
}

func TestServer_AuthRequired(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore()})
	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run_1", nil)
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestServer_InvalidTokenRejected(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore()})
	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run_1", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestServer_PostAndGetRun(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}})

	body := bytes.NewBufferString(`{"agent_id":"agent-1","message":"hello"}`)
	postReq := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
	postReq.Header.Set("Authorization", "Bearer secret")
	postRR := httptest.NewRecorder()

	s.Handler().ServeHTTP(postRR, postReq)
	if postRR.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, postRR.Code)
	}

	var postRes postRunResponse
	if err := json.Unmarshal(postRR.Body.Bytes(), &postRes); err != nil {
		t.Fatalf("decode post response: %v", err)
	}
	if postRes.ID == "" {
		t.Fatal("expected run id in response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/runs/"+postRes.ID, nil)
	getReq.Header.Set("Authorization", "Bearer secret")
	getRR := httptest.NewRecorder()

	s.Handler().ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, getRR.Code)
	}

	var run Run
	if err := json.Unmarshal(getRR.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if run.ID != postRes.ID {
		t.Fatalf("expected run id %q, got %q", postRes.ID, run.ID)
	}
}

func TestServer_ListenAndServeRequiresToken(t *testing.T) {
	s := NewServer(Config{Store: NewInMemoryRunStore()})
	err := s.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("expected error when token is empty")
	}
}

func TestServer_ChatMessageEndpoint(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{}})
	body := bytes.NewBufferString(`{"user_id":"u1","room_id":"r1","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, rr.Code)
	}

	var resp ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if resp.ID != "run-chat" || resp.Status != "queued" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestServer_ChatMessageIncludesSessionIDWhenProvided(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{response: ChatResponse{ID: "run-chat", Status: "queued", SessionID: "session-1"}}})
	body := bytes.NewBufferString(`{"user_id":"u1","room_id":"r1","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, rr.Code)
	}

	var resp ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if resp.SessionID != "session-1" {
		t.Fatalf("expected session id in response, got %+v", resp)
	}
}

func TestServer_ChatMessageDenied(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{err: errors.New("denied")}})
	body := bytes.NewBufferString(`{"user_id":"u1","message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestServer_ChatMessageImmediateResponse(t *testing.T) {
	s := NewServer(Config{BearerToken: "secret", Store: NewInMemoryRunStore(), Executor: NopExecutor{}, Chat: testChatConnector{response: ChatResponse{Response: "Started new chat: s1"}}})
	body := bytes.NewBufferString(`{"user_id":"u1","message":"/new"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/messages", body)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}

	var resp ChatResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode chat response: %v", err)
	}
	if resp.Response == "" || resp.ID != "" {
		t.Fatalf("unexpected immediate response: %+v", resp)
	}
}
