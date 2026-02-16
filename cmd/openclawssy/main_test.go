package main

import (
	"context"
	"path/filepath"
	"testing"

	"openclawssy/internal/channels/chat"
	"openclawssy/internal/channels/discord"
	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
)

func TestChatAdaptersRouteBySource(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	sources := make([]string, 0, 2)
	connector := &chat.Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, agentID, message, source string) (chat.QueuedRun, error) {
			_ = ctx
			_ = agentID
			_ = message
			sources = append(sources, source)
			return chat.QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}

	handler := buildDiscordMessageHandler(connector, "default")
	resp, err := handler(context.Background(), discord.Message{UserID: "u1", RoomID: "c1", Text: "hello"})
	if err != nil {
		t.Fatalf("discord handler error: %v", err)
	}
	if resp.ID != "run-1" {
		t.Fatalf("unexpected discord run id: %q", resp.ID)
	}

	adapter := scopedChatAdapter{connector: connector, source: "dashboard", defaultAgentID: "default"}
	httpResp, err := adapter.HandleMessage(context.Background(), httpchannel.ChatMessage{UserID: "u1", RoomID: "dashboard", Message: "hello"})
	if err != nil {
		t.Fatalf("dashboard adapter error: %v", err)
	}
	if httpResp.ID != "run-1" {
		t.Fatalf("unexpected dashboard run id: %q", httpResp.ID)
	}

	if len(sources) != 2 {
		t.Fatalf("expected 2 queued calls, got %d", len(sources))
	}
	if sources[0] != "discord" || sources[1] != "dashboard" {
		t.Fatalf("unexpected source routing: %#v", sources)
	}
}
