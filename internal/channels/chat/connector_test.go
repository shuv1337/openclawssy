package chat

import (
	"context"
	"testing"
	"time"
)

func TestConnectorQueuesAllowedMessage(t *testing.T) {
	allow := NewAllowlist([]string{"u1"}, []string{"r1"})
	limiter := NewRateLimiter(5, time.Minute)
	connector := &Connector{
		Allowlist:      allow,
		RateLimiter:    limiter,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, agentID, message, source string) (QueuedRun, error) {
			_ = ctx
			if agentID != "default" || source != "chat" {
				t.Fatalf("unexpected queue args: agent=%s source=%s", agentID, source)
			}
			if message != "hello" {
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
		Queue: func(ctx context.Context, agentID, message, source string) (QueuedRun, error) {
			return QueuedRun{}, nil
		},
	}

	_, err := connector.HandleMessage(context.Background(), Message{UserID: "u2", Text: "hello"})
	if err == nil {
		t.Fatal("expected allowlist error")
	}
}
