package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotAllowlisted = errors.New("chat sender is not allowlisted")
	ErrRateLimited    = errors.New("chat sender is rate limited")
)

type Message struct {
	UserID  string
	RoomID  string
	AgentID string
	Text    string
}

type QueuedRun struct {
	ID     string
	Status string
}

type QueueFunc func(ctx context.Context, agentID, message, source string) (QueuedRun, error)

type Connector struct {
	Allowlist      *Allowlist
	RateLimiter    *RateLimiter
	Queue          QueueFunc
	DefaultAgentID string
}

func (c *Connector) HandleMessage(ctx context.Context, msg Message) (QueuedRun, error) {
	if c == nil || c.Queue == nil {
		return QueuedRun{}, errors.New("chat queue is not configured")
	}
	if strings.TrimSpace(msg.UserID) == "" || strings.TrimSpace(msg.Text) == "" {
		return QueuedRun{}, errors.New("user id and text are required")
	}
	if c.Allowlist != nil && !c.Allowlist.MessageAllowed(msg.UserID, msg.RoomID) {
		return QueuedRun{}, ErrNotAllowlisted
	}
	if c.RateLimiter != nil {
		key := fmt.Sprintf("%s:%s", msg.UserID, msg.RoomID)
		if !c.RateLimiter.Allow(key) {
			return QueuedRun{}, ErrRateLimited
		}
	}

	agentID := strings.TrimSpace(msg.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(c.DefaultAgentID)
	}
	if agentID == "" {
		agentID = "default"
	}

	return c.Queue(ctx, agentID, msg.Text, "chat")
}
