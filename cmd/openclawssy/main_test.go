package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/channels/chat"
	"openclawssy/internal/channels/cli"
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
	thinkingModes := make([]string, 0, 2)
	connector := &chat.Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, agentID, message, source, sessionID, thinkingMode string) (chat.QueuedRun, error) {
			_ = ctx
			_ = agentID
			_ = message
			if sessionID == "" {
				t.Fatal("expected session id")
			}
			sources = append(sources, source)
			thinkingModes = append(thinkingModes, thinkingMode)
			return chat.QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}

	handler := buildDiscordMessageHandler(connector, "default")
	resp, err := handler(context.Background(), discord.Message{UserID: "u1", RoomID: "c1", Text: "hello", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("discord handler error: %v", err)
	}
	if resp.ID != "run-1" {
		t.Fatalf("unexpected discord run id: %q", resp.ID)
	}

	adapter := scopedChatAdapter{connector: connector, source: "dashboard", defaultAgentID: "default"}
	httpResp, err := adapter.HandleMessage(context.Background(), httpchannel.ChatMessage{UserID: "u1", RoomID: "dashboard", Message: "hello", ThinkingMode: "on_error"})
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
	if thinkingModes[0] != "always" || thinkingModes[1] != "on_error" {
		t.Fatalf("unexpected thinking mode routing: %#v", thinkingModes)
	}
}

func TestCronServiceSupportsDeleteAndPauseResume(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	svc := cronService{}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "add", Args: []string{"-id", "job-1", "-schedule", "@every 1m", "-message", "ping"}}); err != nil {
		t.Fatalf("add job: %v", err)
	}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "pause"}); err != nil {
		t.Fatalf("pause scheduler: %v", err)
	}
	out, err := svc.Cron(context.Background(), cli.CronInput{Command: "list"})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if !strings.Contains(out, "scheduler=paused") {
		t.Fatalf("expected paused scheduler state, got %q", out)
	}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "resume", Args: []string{"-id", "job-1"}}); err != nil {
		t.Fatalf("resume job: %v", err)
	}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "delete", Args: []string{"-id", "job-1"}}); err != nil {
		t.Fatalf("delete job via alias: %v", err)
	}
	out, err = svc.Cron(context.Background(), cli.CronInput{Command: "list"})
	if err != nil {
		t.Fatalf("list jobs after delete: %v", err)
	}
	if !strings.Contains(out, "no jobs") {
		t.Fatalf("expected no jobs output, got %q", out)
	}
}

func TestScopedChatAdapterRateLimitIncludesCooldown(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	connector := &chat.Connector{
		Store:          store,
		DefaultAgentID: "default",
		GlobalLimiter:  chat.NewRateLimiterWithClock(1, time.Minute, clock),
		Queue: func(ctx context.Context, agentID, message, source, sessionID, thinkingMode string) (chat.QueuedRun, error) {
			_ = ctx
			_ = agentID
			_ = message
			_ = source
			_ = sessionID
			_ = thinkingMode
			return chat.QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}
	adapter := scopedChatAdapter{
		connector:      connector,
		source:         "dashboard",
		defaultAgentID: "default",
	}

	if _, err := adapter.HandleMessage(context.Background(), httpchannel.ChatMessage{UserID: "u1", RoomID: "dashboard", Message: "hello"}); err != nil {
		t.Fatalf("first message should pass: %v", err)
	}
	_, err = adapter.HandleMessage(context.Background(), httpchannel.ChatMessage{UserID: "u2", RoomID: "dashboard", Message: "hello"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !errors.Is(err, chat.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	var rateErr *chat.RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rateErr.RetryAfterSeconds < 1 {
		t.Fatalf("expected cooldown seconds, got %+v", rateErr)
	}
}
