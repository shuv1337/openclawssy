package chat

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"openclawssy/internal/chatstore"
)

var (
	ErrNotAllowlisted = errors.New("chat sender is not allowlisted")
	ErrRateLimited    = errors.New("chat sender is rate limited")
)

type Message struct {
	UserID  string
	RoomID  string
	AgentID string
	Source  string
	Text    string
}

type Result struct {
	ID       string
	Status   string
	Response string
}

type QueuedRun struct {
	ID     string
	Status string
}

type QueueFunc func(ctx context.Context, agentID, message, source, sessionID string) (QueuedRun, error)

type Connector struct {
	Allowlist      *Allowlist
	RateLimiter    *RateLimiter
	Queue          QueueFunc
	DefaultAgentID string
	Store          *chatstore.Store
	HistoryLimit   int
}

func (c *Connector) HandleMessage(ctx context.Context, msg Message) (Result, error) {
	if c == nil || c.Queue == nil {
		return Result{}, errors.New("chat queue is not configured")
	}
	if c.Store == nil {
		return Result{}, errors.New("chat store is not configured")
	}
	if strings.TrimSpace(msg.UserID) == "" || strings.TrimSpace(msg.Text) == "" {
		return Result{}, errors.New("user id and text are required")
	}
	if c.Allowlist != nil && !c.Allowlist.MessageAllowed(msg.UserID, msg.RoomID) {
		return Result{}, ErrNotAllowlisted
	}
	if c.RateLimiter != nil {
		key := fmt.Sprintf("%s:%s", msg.UserID, msg.RoomID)
		if !c.RateLimiter.Allow(key) {
			return Result{}, ErrRateLimited
		}
	}

	agentID := strings.TrimSpace(msg.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(c.DefaultAgentID)
	}
	if agentID == "" {
		agentID = "default"
	}

	source := strings.TrimSpace(msg.Source)
	if source == "" {
		source = "chat"
	}
	roomID := strings.TrimSpace(msg.RoomID)
	if roomID == "" {
		roomID = source
	}

	text := strings.TrimSpace(msg.Text)
	if text == "/new" {
		session, err := c.createAndSetActiveSession(agentID, source, msg.UserID, roomID)
		if err != nil {
			return Result{}, err
		}
		return Result{Response: "Started new chat: " + session.SessionID}, nil
	}
	if strings.HasPrefix(text, "/resume") {
		parts := strings.Fields(text)
		if len(parts) != 2 {
			return Result{Response: "Usage: /resume <session_id>"}, nil
		}
		session, err := c.Store.GetSession(parts[1])
		if err != nil {
			if errors.Is(err, chatstore.ErrSessionNotFound) {
				return Result{Response: "Session not found: " + parts[1]}, nil
			}
			return Result{}, err
		}
		if session.AgentID != agentID || session.Channel != source || session.UserID != msg.UserID || session.RoomID != roomID {
			return Result{Response: "Session not available in this chat context"}, nil
		}
		if err := c.Store.SetActiveSessionPointer(agentID, source, msg.UserID, roomID, parts[1]); err != nil {
			return Result{}, err
		}
		return Result{Response: "Resumed chat: " + parts[1]}, nil
	}
	if text == "/chats" {
		sessions, err := c.Store.ListSessions(agentID, msg.UserID, roomID, source)
		if err != nil {
			return Result{}, err
		}
		if len(sessions) == 0 {
			return Result{Response: "No chats found"}, nil
		}
		if len(sessions) > 10 {
			sessions = sessions[:10]
		}
		lines := make([]string, 0, len(sessions)+1)
		lines = append(lines, "Recent chats:")
		for i, session := range sessions {
			lines = append(lines, strconv.Itoa(i+1)+". "+session.SessionID)
		}
		return Result{Response: strings.Join(lines, "\n")}, nil
	}

	session, err := c.resolveOrCreateActiveSession(agentID, source, msg.UserID, roomID)
	if err != nil {
		return Result{}, err
	}

	if err := c.Store.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: msg.Text}); err != nil {
		return Result{}, err
	}

	queued, err := c.Queue(ctx, agentID, msg.Text, source, session.SessionID)
	if err != nil {
		return Result{}, err
	}

	return Result{ID: queued.ID, Status: queued.Status}, nil
}

func (c *Connector) createAndSetActiveSession(agentID, source, userID, roomID string) (chatstore.Session, error) {
	session, err := c.Store.CreateSession(chatstore.CreateSessionInput{AgentID: agentID, Channel: source, UserID: userID, RoomID: roomID})
	if err != nil {
		return chatstore.Session{}, err
	}
	if err := c.Store.SetActiveSessionPointer(agentID, source, userID, roomID, session.SessionID); err != nil {
		return chatstore.Session{}, err
	}
	return session, nil
}

func (c *Connector) resolveOrCreateActiveSession(agentID, source, userID, roomID string) (chatstore.Session, error) {
	activeID, err := c.Store.GetActiveSessionPointer(agentID, source, userID, roomID)
	if err != nil {
		if errors.Is(err, chatstore.ErrSessionNotFound) {
			return c.createAndSetActiveSession(agentID, source, userID, roomID)
		}
		return chatstore.Session{}, err
	}

	session, err := c.Store.GetSession(activeID)
	if err != nil {
		if errors.Is(err, chatstore.ErrSessionNotFound) {
			return c.createAndSetActiveSession(agentID, source, userID, roomID)
		}
		return chatstore.Session{}, err
	}
	if session.AgentID != agentID || session.Channel != source || session.UserID != userID || session.RoomID != roomID {
		return c.createAndSetActiveSession(agentID, source, userID, roomID)
	}
	return session, nil
}
