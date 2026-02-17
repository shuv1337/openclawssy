package discord

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/channels/chat"
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

func TestFormatToolActivity(t *testing.T) {
	t.Run("renders tool summary", func(t *testing.T) {
		trace := map[string]any{
			"tool_execution_results": []any{
				map[string]any{"tool": "fs.list", "tool_call_id": "tool-1", "summary": "listed 1 entries in .", "output": `{"entries":["a.txt"]}`},
				map[string]any{"tool": "fs.read", "tool_call_id": "tool-2", "error": "not found"},
			},
		}
		out := formatToolActivity("run_1", trace)
		if !strings.Contains(out, "Tool activity for run `run_1`:") {
			t.Fatalf("unexpected header: %q", out)
		}
		if !strings.Contains(out, "1) fs.list [tool-1]") {
			t.Fatalf("missing first tool line: %q", out)
		}
		if !strings.Contains(out, "1) fs.list [tool-1] -> listed 1 entries in .") {
			t.Fatalf("expected summary-preferred tool line: %q", out)
		}
		if !strings.Contains(out, "2) fs.read [tool-2] -> error: not found") {
			t.Fatalf("missing error line: %q", out)
		}
	})

	t.Run("returns empty when no tools", func(t *testing.T) {
		out := formatToolActivity("run_1", map[string]any{})
		if out != "" {
			t.Fatalf("expected empty summary, got %q", out)
		}
	})
}

func TestParseThinkingOverride(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantText  string
		wantMode  string
		wantError bool
	}{
		{name: "slash ask with override", in: "/ask thinking=always summarize", wantText: "summarize", wantMode: "always"},
		{name: "prefix text with override", in: "thinking=on_error summarize", wantText: "summarize", wantMode: "on_error"},
		{name: "plain text", in: "hello", wantText: "hello"},
		{name: "pass through slash command", in: "/resume chat_1", wantText: "/resume chat_1"},
		{name: "invalid mode", in: "/ask thinking=maybe hi", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, mode, err := parseThinkingOverride(tc.in)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if text != tc.wantText || mode != tc.wantMode {
				t.Fatalf("got text=%q mode=%q want text=%q mode=%q", text, mode, tc.wantText, tc.wantMode)
			}
		})
	}
}

func TestFormatDiscordErrorRateLimited(t *testing.T) {
	msg := formatDiscordError(chat.NewRateLimitError("sender", 2300*time.Millisecond))
	if msg != "rate limited, retry in 3s" {
		t.Fatalf("unexpected rate limit format: %q", msg)
	}

	msg = formatDiscordError(errors.New("sender is rate limited"))
	if msg != "rate limited, try again soon" {
		t.Fatalf("unexpected generic rate limit format: %q", msg)
	}

	msg = formatDiscordError(errors.New("chat sender is not allowlisted"))
	if msg != "not allowed in this channel or user scope" {
		t.Fatalf("unexpected allowlist format: %q", msg)
	}

	msg = formatDiscordError(errors.New("httpchannel: run queue is full"))
	if msg != "run queue is full, retry shortly" {
		t.Fatalf("unexpected queue-full format: %q", msg)
	}
}
