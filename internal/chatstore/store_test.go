package chatstore

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCreateListGetAppendReadRecent(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	s1, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "dashboard",
		UserID:  "u1",
		RoomID:  "r1",
		Title:   "first",
	})
	if err != nil {
		t.Fatalf("create session 1: %v", err)
	}

	_, err = store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "dashboard",
		UserID:  "u2",
		RoomID:  "r1",
		Title:   "second",
	})
	if err != nil {
		t.Fatalf("create session 2: %v", err)
	}

	if err := store.AppendMessage(s1.SessionID, Message{Role: "user", Content: "one", TS: time.Now().UTC().Add(-2 * time.Second)}); err != nil {
		t.Fatalf("append one: %v", err)
	}
	if err := store.AppendMessage(s1.SessionID, Message{Role: "assistant", Content: "two", TS: time.Now().UTC().Add(-1 * time.Second)}); err != nil {
		t.Fatalf("append two: %v", err)
	}
	if err := store.AppendMessage(s1.SessionID, Message{Role: "user", Content: "three"}); err != nil {
		t.Fatalf("append three: %v", err)
	}

	sessions, err := store.ListSessions("default", "u1", "r1", "dashboard")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != s1.SessionID {
		t.Fatalf("unexpected listed session: %+v", sessions[0])
	}

	gotSession, err := store.GetSession(s1.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if gotSession.AgentID != "default" || gotSession.UserID != "u1" {
		t.Fatalf("unexpected session data: %+v", gotSession)
	}

	recent, err := store.ReadRecentMessages(s1.SessionID, 2)
	if err != nil {
		t.Fatalf("read recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent messages, got %d", len(recent))
	}
	if recent[0].Content != "two" || recent[1].Content != "three" {
		t.Fatalf("unexpected recent messages: %+v", recent)
	}
}

func TestPersistenceAcrossStoreRestart(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "discord",
		UserID:  "u1",
		RoomID:  "roomA",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.AppendMessage(session.SessionID, Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.SetActiveSessionPointer("default", "discord", "u1", "roomA", session.SessionID); err != nil {
		t.Fatalf("set active pointer: %v", err)
	}

	reloaded, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}

	gotSession, err := reloaded.GetSession(session.SessionID)
	if err != nil {
		t.Fatalf("get session after restart: %v", err)
	}
	if gotSession.SessionID != session.SessionID {
		t.Fatalf("unexpected session after restart: %+v", gotSession)
	}

	msgs, err := reloaded.ReadRecentMessages(session.SessionID, 10)
	if err != nil {
		t.Fatalf("read messages after restart: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("unexpected messages after restart: %+v", msgs)
	}

	active, err := reloaded.GetActiveSessionPointer("default", "discord", "u1", "roomA")
	if err != nil {
		t.Fatalf("get active pointer: %v", err)
	}
	if active != session.SessionID {
		t.Fatalf("unexpected active pointer: %s", active)
	}
}

func TestAppendMessageConcurrent(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session, err := store.CreateSession(CreateSessionInput{
		AgentID: "default",
		Channel: "dashboard",
		UserID:  "u1",
		RoomID:  "r1",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			if err := store.AppendMessage(session.SessionID, Message{Role: "user", Content: "m"}); err != nil {
				t.Errorf("append message %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	msgs, err := store.ReadRecentMessages(session.SessionID, n)
	if err != nil {
		t.Fatalf("read recent: %v", err)
	}
	if len(msgs) != n {
		t.Fatalf("expected %d messages, got %d", n, len(msgs))
	}
}

func TestGetActiveSessionPointerMissing(t *testing.T) {
	agentsRoot := filepath.Join(t.TempDir(), ".openclawssy", "agents")
	store, err := NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = store.GetActiveSessionPointer("default", "dashboard", "u1", "r1")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestClampHistoryCount(t *testing.T) {
	if got := ClampHistoryCount(10, 50); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
	if got := ClampHistoryCount(0, 50); got != 50 {
		t.Fatalf("expected 50 for zero requested, got %d", got)
	}
	if got := ClampHistoryCount(500, 50); got != 50 {
		t.Fatalf("expected clamp to 50, got %d", got)
	}
}
