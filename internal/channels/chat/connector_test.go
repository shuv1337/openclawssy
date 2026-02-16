package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/chatstore"
)

func TestConnectorQueuesAllowedMessage(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	allow := NewAllowlist([]string{"u1"}, []string{"r1"})
	limiter := NewRateLimiter(5, time.Minute)
	connector := &Connector{
		Allowlist:      allow,
		RateLimiter:    limiter,
		DefaultAgentID: "default",
		Store:          store,
		Queue: func(ctx context.Context, agentID, message, source string) (QueuedRun, error) {
			_ = ctx
			if agentID != "default" || source != "chat" {
				t.Fatalf("unexpected queue args: agent=%s source=%s", agentID, source)
			}
			if !strings.Contains(message, "hello") {
				t.Fatalf("unexpected message: %s", message)
			}
			return QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}

	run, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "r1", Text: "hello"})
	if err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if run.ID != "run-1" {
		t.Fatalf("unexpected run id: %s", run.ID)
	}
}

func TestConnectorRejectsUnallowlisted(t *testing.T) {
	connector := &Connector{
		Allowlist: NewAllowlist([]string{"u1"}, nil),
		Store: func() *chatstore.Store {
			store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
			if err != nil {
				t.Fatalf("new chat store: %v", err)
			}
			return store
		}(),
		Queue: func(ctx context.Context, agentID, message, source string) (QueuedRun, error) {
			return QueuedRun{}, nil
		},
	}

	_, err := connector.HandleMessage(context.Background(), Message{UserID: "u2", Text: "hello"})
	if err == nil {
		t.Fatal("expected allowlist error")
	}
}

func TestConnectorNewResumeAndChatsCommands(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	queued := 0
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, agentID, message, source string) (QueuedRun, error) {
			queued++
			return QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}

	res, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/new"})
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	if !strings.HasPrefix(res.Response, "Started new chat: ") {
		t.Fatalf("unexpected /new response: %q", res.Response)
	}
	if queued != 0 {
		t.Fatalf("expected no queued runs, got %d", queued)
	}

	sessionID := strings.TrimPrefix(res.Response, "Started new chat: ")
	res, err = connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/chats"})
	if err != nil {
		t.Fatalf("chats command: %v", err)
	}
	if !strings.Contains(res.Response, sessionID) {
		t.Fatalf("expected /chats to include session id, got: %q", res.Response)
	}

	res, err = connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "/resume " + sessionID})
	if err != nil {
		t.Fatalf("resume command: %v", err)
	}
	if res.Response != "Resumed chat: "+sessionID {
		t.Fatalf("unexpected /resume response: %q", res.Response)
	}
	if queued != 0 {
		t.Fatalf("expected no queued runs after commands, got %d", queued)
	}
}

func TestConnectorPrependsHistoryAndAppendsUserMessage(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	var queuedMessage string
	connector := &Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, agentID, message, source string) (QueuedRun, error) {
			queuedMessage = message
			return QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}

	if _, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "first"}); err != nil {
		t.Fatalf("first message: %v", err)
	}
	if _, err := connector.HandleMessage(context.Background(), Message{UserID: "u1", RoomID: "dashboard", Source: "dashboard", Text: "second"}); err != nil {
		t.Fatalf("second message: %v", err)
	}

	if !strings.Contains(queuedMessage, "CHAT_HISTORY:") {
		t.Fatalf("expected CHAT_HISTORY block, got: %q", queuedMessage)
	}
	if !strings.Contains(queuedMessage, "[USER] first") {
		t.Fatalf("expected previous message in history, got: %q", queuedMessage)
	}
	if !strings.HasSuffix(queuedMessage, "second") {
		t.Fatalf("expected original message at end, got: %q", queuedMessage)
	}

	sessions, err := store.ListSessions("default", "u1", "dashboard", "dashboard")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}

	msgs, err := store.ReadRecentMessages(sessions[0].SessionID, 10)
	if err != nil {
		t.Fatalf("read recent messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in store, got %d", len(msgs))
	}
	if msgs[0].Content != "first" || msgs[1].Content != "second" {
		t.Fatalf("unexpected stored messages: %+v", msgs)
	}
}
