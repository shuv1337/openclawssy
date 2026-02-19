package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openclawssy/internal/config"
)

func TestMemoryEmbedderSupportsOpenRouterEmbeddings(t *testing.T) {
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float64{0.1, 0.2, 0.3}}},
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.EmbeddingsEnabled = true
	cfg.Memory.EmbeddingProvider = "openrouter"
	cfg.Memory.EmbeddingModel = "text-embedding-3-small"
	cfg.Providers.OpenRouter.BaseURL = server.URL + "/v1"
	cfg.Providers.OpenRouter.APIKey = "test-openrouter-key"
	cfg.Providers.OpenRouter.APIKeyEnv = ""

	embedder, err := memoryEmbedderFromConfig(cfg)
	if err != nil {
		t.Fatalf("memoryEmbedderFromConfig: %v", err)
	}
	if embedder == nil {
		t.Fatal("expected embedder to be configured")
	}
	vec, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 embedding dimensions, got %d", len(vec))
	}
	if gotPath != "/v1/embeddings" {
		t.Fatalf("expected openrouter embeddings path, got %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer test-openrouter-key") {
		t.Fatalf("expected bearer auth header, got %q", gotAuth)
	}
}
