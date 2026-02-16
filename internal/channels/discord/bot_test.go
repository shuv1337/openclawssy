package discord

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeInboundMessage(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string
		want   string
	}{
		{name: "plain content with no prefix", input: "hello", want: "hello"},
		{name: "command prefix message", input: "!ask hello", prefix: "!ask", want: "hello"},
		{name: "slash command bypasses prefix", input: "/new", prefix: "!ask", want: "/new"},
		{name: "non prefixed ignored", input: "hello", prefix: "!ask", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeInboundMessage(tc.input, tc.prefix)
			if got != tc.want {
				t.Fatalf("normalizeInboundMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitDiscordMessage(t *testing.T) {
	parts := splitDiscordMessage("alpha\nbeta\ngamma", 7)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0] != "alpha" || parts[1] != "beta" || parts[2] != "gamma" {
		t.Fatalf("unexpected parts: %#v", parts)
	}

	long := strings.Repeat("x", 25)
	parts = splitDiscordMessage(long, 10)
	if len(parts) != 3 {
		t.Fatalf("expected 3 long chunks, got %d", len(parts))
	}
	for i, p := range parts {
		if len(p) > 10 {
			t.Fatalf("chunk %d too long: %d", i, len(p))
		}
	}
}

func TestWaitForTerminalRun(t *testing.T) {
	t.Run("completes after polling", func(t *testing.T) {
		calls := 0
		statusFn := func(ctx context.Context, runID string) (RunStatus, error) {
			_ = ctx
			_ = runID
			calls++
			if calls < 3 {
				return RunStatus{Status: "running"}, nil
			}
			return RunStatus{Status: "completed", Output: "done"}, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		run, err := waitForTerminalRun(ctx, "run-1", statusFn, time.Millisecond)
		if err != nil {
			t.Fatalf("waitForTerminalRun() error = %v", err)
		}
		if run.Status != "completed" || run.Output != "done" {
			t.Fatalf("unexpected run result: %+v", run)
		}
	})

	t.Run("returns error from status func", func(t *testing.T) {
		wantErr := errors.New("boom")
		_, err := waitForTerminalRun(context.Background(), "run-1", func(ctx context.Context, runID string) (RunStatus, error) {
			_ = ctx
			_ = runID
			return RunStatus{}, wantErr
		}, time.Millisecond)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})
}
