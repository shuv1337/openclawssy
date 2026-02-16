package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
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

	content := strings.TrimSpace(payload.Choices[0].Message.Content)

	// Check if the model's response contains tool calls in JSON blocks
	toolCalls := parseToolCallsFromResponse(content)
	if len(toolCalls) > 0 {
		return agent.ModelResponse{ToolCalls: toolCalls}, nil
	}

	return agent.ModelResponse{FinalText: content}, nil
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

func parseToolCallsFromResponse(content string) []agent.ToolCallRequest {
	var toolCalls []agent.ToolCallRequest

	// Look for JSON code blocks that might contain tool calls
	// Pattern: ```json ... ``` or ``` ... ```
	re := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(content, -1)

	for i, match := range matches {
		if len(match) < 2 {
			continue
		}
		jsonContent := strings.TrimSpace(match[1])

		// Try to parse as a tool call
		var toolCall struct {
			ToolName  string         `json:"tool_name"`
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Args      map[string]any `json:"args"`
		}

		if err := json.Unmarshal([]byte(jsonContent), &toolCall); err == nil {
			// Check if it has a tool name
			toolName := toolCall.ToolName
			if toolName == "" {
				toolName = toolCall.Name
			}
			if toolName != "" {
				// Get arguments
				args := toolCall.Arguments
				if args == nil {
					args = toolCall.Args
				}
				if args == nil {
					args = map[string]any{}
				}

				argBytes, _ := json.Marshal(args)
				toolCalls = append(toolCalls, agent.ToolCallRequest{
					ID:        fmt.Sprintf("tool-%d", i+1),
					Name:      toolName,
					Arguments: argBytes,
				})
				continue
			}
		}

		// Try parsing as direct tool call syntax: tool_name(args)
		// Pattern: fs.list(path=".") or fs.list(".")
		if call := parseFunctionCall(jsonContent); call != nil {
			toolCalls = append(toolCalls, *call)
		}
	}

	// Also look for inline function calls not in code blocks
	// Pattern: fs.list(path=".") or fs.list(".")
	inlineRe := regexp.MustCompile(`\b(fs\.\w+|code\.\w+|shell\.\w+|time\.\w+)\s*\(([^)]*)\)`)
	inlineMatches := inlineRe.FindAllStringSubmatch(content, -1)
	for _, match := range inlineMatches {
		if len(match) >= 2 {
			toolName := strings.TrimSpace(match[1])
			argsStr := ""
			if len(match) >= 3 {
				argsStr = match[2]
			}
			args := parseArgsString(argsStr)
			argBytes, _ := json.Marshal(args)
			toolCalls = append(toolCalls, agent.ToolCallRequest{
				ID:        fmt.Sprintf("tool-inline-%d", len(toolCalls)+1),
				Name:      toolName,
				Arguments: argBytes,
			})
		}
	}

	return toolCalls
}

func parseFunctionCall(content string) *agent.ToolCallRequest {
	// Parse function call syntax like: fs.list(path=".") or fs.list(".")
	re := regexp.MustCompile(`^(\w+\.\w+)\s*\((.*)\)$`)
	matches := re.FindStringSubmatch(strings.TrimSpace(content))
	if len(matches) < 2 {
		return nil
	}

	toolName := strings.TrimSpace(matches[1])
	argsStr := ""
	if len(matches) >= 3 {
		argsStr = matches[2]
	}

	args := parseArgsString(argsStr)
	argBytes, _ := json.Marshal(args)

	return &agent.ToolCallRequest{
		ID:        fmt.Sprintf("tool-func-%d", time.Now().UnixNano()),
		Name:      toolName,
		Arguments: argBytes,
	}
}

func parseArgsString(argsStr string) map[string]any {
	args := map[string]any{}
	argsStr = strings.TrimSpace(argsStr)

	if argsStr == "" {
		return args
	}

	// Try to parse as JSON first
	if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
		return args
	}

	// Parse key=value pairs
	// Pattern: key="value", key='value', key=value, or just "value" (positional)
	re := regexp.MustCompile(`(\w+)\s*=\s*("[^"]*"|'[^']*'|[^,\s]+)`)
	pairs := re.FindAllStringSubmatch(argsStr, -1)

	for _, pair := range pairs {
		if len(pair) >= 3 {
			key := strings.TrimSpace(pair[1])
			value := strings.TrimSpace(pair[2])
			// Remove quotes
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
				value = value[1 : len(value)-1]
			}
			args[key] = value
		}
	}

	// If no key=value pairs found, treat as single positional argument "path"
	if len(args) == 0 && argsStr != "" {
		value := strings.TrimSpace(argsStr)
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			value = value[1 : len(value)-1]
		}
		args["path"] = value
	}

	return args
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
