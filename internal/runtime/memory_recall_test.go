package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/config"
	"openclawssy/internal/memory"
	memorystore "openclawssy/internal/memory/store"
)

func TestBuildMemoryRecallBlockIncludesRelevantItems(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	dbPath := filepath.Join(root, ".openclawssy", "agents", "default", "memory", "memory.db")
	store, err := memorystore.OpenSQLite(dbPath, "default")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer func() { _ = store.Close() }()
	_, _ = store.Upsert(context.Background(), memory.MemoryItem{Kind: "preference", Title: "Notifications", Content: "User prefers proactive notifications.", Importance: 4, Confidence: 0.9, UpdatedAt: time.Now().UTC()})
	_, _ = store.Upsert(context.Background(), memory.MemoryItem{Kind: "issue", Title: "Tool failure", Content: "Recent timeout in network call.", Importance: 3, Confidence: 0.8, UpdatedAt: time.Now().UTC().Add(-2 * time.Hour)})

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.MaxPromptTokens = 200
	cfg.Memory.MaxWorkingItems = 10

	block, err := e.buildMemoryRecallBlock(context.Background(), cfg, "default", "please keep proactive notifications", []agent.ChatMessage{{Role: "user", Content: "I prefer proactive notifications"}})
	if err != nil {
		t.Fatalf("build memory recall block: %v", err)
	}
	if !strings.Contains(block, "RELEVANT MEMORY") {
		t.Fatalf("expected recall header, got %q", block)
	}
	if !strings.Contains(block, "proactive notifications") {
		t.Fatalf("expected relevant memory content, got %q", block)
	}
}

func TestBuildMemoryRecallBlockRespectsSizeCap(t *testing.T) {
	items := []memory.MemoryItem{{ID: "mem_123456789", Content: strings.Repeat("x", 500), Importance: 5, UpdatedAt: time.Now().UTC()}}
	block := formatRecallBlock(items, 80)
	if len(block) > 80 {
		t.Fatalf("expected block length <= 80, got %d", len(block))
	}
}
