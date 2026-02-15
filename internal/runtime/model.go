package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/config"
)

type ProviderModel struct {
	providerName string
	modelName    string
	baseURL      string
	apiKey       string
	headers      map[string]string
	httpClient   *http.Client
}

type SecretLookup func(name string) (string, bool, error)

func NewProviderModel(cfg config.Config, lookup SecretLookup) (*ProviderModel, error) {
	pName := strings.ToLower(strings.TrimSpace(cfg.Model.Provider))
	endpoint, err := providerEndpoint(cfg, pName)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(endpoint.APIKey)
	if apiKey == "" && lookup != nil {
		if v, ok, err := lookup("provider/" + pName + "/api_key"); err == nil && ok {
			apiKey = strings.TrimSpace(v)
		}
	}
	if apiKey == "" && endpoint.APIKeyEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(endpoint.APIKeyEnv))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("model provider %q is missing API key (set %s or providers.%s.api_key)", pName, endpoint.APIKeyEnv, pName)
	}

	base := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("model provider %q requires a base_url", pName)
	}
	if strings.HasSuffix(base, "/chat/completions") {
		base = strings.TrimSuffix(base, "/chat/completions")
	}

	headers := map[string]string{}
	for k, v := range endpoint.Headers {
		headers[k] = v
	}

	return &ProviderModel{
		providerName: pName,
		modelName:    cfg.Model.Name,
		baseURL:      base,
		apiKey:       apiKey,
		headers:      headers,
		httpClient:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (m *ProviderModel) ProviderName() string { return m.providerName }
func (m *ProviderModel) ModelName() string    { return m.modelName }

func (m *ProviderModel) Generate(ctx context.Context, req agent.ModelRequest) (agent.ModelResponse, error) {
	if len(req.ToolResults) > 0 {
		var sb strings.Builder
		sb.WriteString("Tool results:\n")
		for _, tr := range req.ToolResults {
			if tr.Error != "" {
				sb.WriteString("- ")
				sb.WriteString(tr.ID)
				sb.WriteString(" error: ")
				sb.WriteString(tr.Error)
				sb.WriteString("\n")
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(tr.ID)
			sb.WriteString(" output: ")
			sb.WriteString(tr.Output)
			sb.WriteString("\n")
		}
		return agent.ModelResponse{FinalText: sb.String()}, nil
	}

	msg := strings.TrimSpace(req.Message)
	if strings.HasPrefix(msg, "/tool ") {
		return parseToolDirective(msg)
	}

	promptText := req.Prompt

	body := map[string]any{
		"model": m.modelName,
		"messages": []map[string]string{
			{"role": "system", "content": promptText},
			{"role": "user", "content": msg},
		},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return agent.ModelResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return agent.ModelResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range m.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return agent.ModelResponse{}, err
	}
	defer resp.Body.Close()

	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return agent.ModelResponse{}, err
	}
	if resp.StatusCode >= 300 {
		return agent.ModelResponse{}, fmt.Errorf("provider %s request failed: status=%d error=%v", m.providerName, resp.StatusCode, payload.Error)
	}
	if len(payload.Choices) == 0 {
		return agent.ModelResponse{}, errors.New("provider returned no choices")
	}
	return agent.ModelResponse{FinalText: strings.TrimSpace(payload.Choices[0].Message.Content)}, nil
}

func parseToolDirective(message string) (agent.ModelResponse, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(message, "/tool "))
	parts := strings.SplitN(rest, " ", 2)
	toolName := strings.TrimSpace(parts[0])
	if toolName == "" {
		return agent.ModelResponse{}, errors.New("tool name is required")
	}
	args := map[string]any{}
	if len(parts) == 2 {
		if err := json.Unmarshal([]byte(parts[1]), &args); err != nil {
			return agent.ModelResponse{}, fmt.Errorf("invalid tool args JSON: %w", err)
		}
	}
	argBytes, _ := json.Marshal(args)
	return agent.ModelResponse{ToolCalls: []agent.ToolCallRequest{{ID: "tool-1", Name: toolName, Arguments: argBytes}}}, nil
}

func providerEndpoint(cfg config.Config, provider string) (config.ProviderEndpointConfig, error) {
	switch provider {
	case "openai":
		return cfg.Providers.OpenAI, nil
	case "openrouter":
		return cfg.Providers.OpenRouter, nil
	case "requesty":
		return cfg.Providers.Requesty, nil
	case "zai":
		return cfg.Providers.ZAI, nil
	case "generic":
		return cfg.Providers.Generic, nil
	default:
		return config.ProviderEndpointConfig{}, fmt.Errorf("unsupported provider: %s", provider)
	}
}
