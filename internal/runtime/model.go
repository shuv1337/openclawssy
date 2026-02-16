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
	msg := strings.TrimSpace(req.Message)
	if strings.HasPrefix(msg, "/tool ") {
		if len(req.ToolResults) > 0 {
			return agent.ModelResponse{FinalText: toolResultsText(req.ToolResults)}, nil
		}
		return parseToolDirective(msg)
	}

	promptText := appendToolResultsPrompt(req.Prompt, req.ToolResults)

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

	// Check if the model's response contains tool calls in JSON blocks.
	toolCalls := parseToolCallsFromResponse(content)
	if len(toolCalls) == 0 {
		toolCalls = parseToolCallsFromResponse(stripThinkingTags(content))
	}
	if len(toolCalls) > 0 {
		return agent.ModelResponse{ToolCalls: toolCalls}, nil
	}

	return agent.ModelResponse{FinalText: stripThinkingTags(content)}, nil
}

func appendToolResultsPrompt(prompt string, results []agent.ToolCallResult) string {
	if len(results) == 0 {
		return prompt
	}

	var b strings.Builder
	b.WriteString(prompt)
	if prompt != "" && !strings.HasSuffix(prompt, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n## Tool Results\n")
	for _, tr := range results {
		b.WriteString("- id: ")
		b.WriteString(tr.ID)
		b.WriteString("\n")
		if tr.Error != "" {
			b.WriteString("  error: ")
			b.WriteString(tr.Error)
			b.WriteString("\n")
			continue
		}
		b.WriteString("  output:\n")
		b.WriteString("  ```\n")
		b.WriteString(tr.Output)
		if tr.Output != "" && !strings.HasSuffix(tr.Output, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("  ```\n")
	}

	return b.String()
}

func toolResultsText(results []agent.ToolCallResult) string {
	var b strings.Builder
	for i, tr := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("tool ")
		b.WriteString(tr.ID)
		if tr.Error != "" {
			b.WriteString(" error: ")
			b.WriteString(tr.Error)
			continue
		}
		b.WriteString(" output:\n")
		b.WriteString(tr.Output)
	}
	return strings.TrimSpace(b.String())
}

func stripThinkingTags(s string) string {
	block := regexp.MustCompile(`(?is)<think>.*?</think>`)
	s = block.ReplaceAllString(s, "")
	marker := regexp.MustCompile(`(?i)</?think>`)
	s = marker.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
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
	xmlTagRe := regexp.MustCompile(`(?is)<[^>]+>`)

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

	// Support plain JSON tool objects not wrapped in code blocks.
	jsonObjects := extractJSONObjectCandidates(content)
	for _, raw := range jsonObjects {
		if call, ok := parseJSONToolCall(raw, len(toolCalls)+1); ok {
			toolCalls = append(toolCalls, call)
		}
	}

	// Support XML function blocks occasionally emitted by models.
	// Pattern: <function=fs.list><path>.</path></function>
	xmlFunctionRe := regexp.MustCompile(`(?is)<function=(fs\.\w+|code\.\w+|shell\.\w+|time\.\w+)>\s*(.*?)\s*</function>`)
	xmlFunctionMatches := xmlFunctionRe.FindAllStringSubmatch(content, -1)
	for _, match := range xmlFunctionMatches {
		if len(match) < 2 {
			continue
		}
		toolName := strings.TrimSpace(match[1])
		argsBody := ""
		if len(match) >= 3 {
			argsBody = strings.TrimSpace(match[2])
		}

		args := map[string]any{}
		xmlArgRe := regexp.MustCompile(`(?is)<(\w+)>\s*([^<]*?)\s*</(\w+)>`)
		xmlArgMatches := xmlArgRe.FindAllStringSubmatch(argsBody, -1)
		for _, argMatch := range xmlArgMatches {
			if len(argMatch) < 4 {
				continue
			}
			key := strings.TrimSpace(argMatch[1])
			if !strings.EqualFold(key, strings.TrimSpace(argMatch[3])) {
				continue
			}
			value := strings.TrimSpace(argMatch[2])
			if key == "" {
				continue
			}
			args[key] = value
		}
		if len(args) == 0 {
			clean := xmlTagRe.ReplaceAllString(argsBody, " ")
			args = parseArgsString(clean)
		}

		argBytes, _ := json.Marshal(args)
		toolCalls = append(toolCalls, agent.ToolCallRequest{
			ID:        fmt.Sprintf("tool-xml-func-%d", len(toolCalls)+1),
			Name:      toolName,
			Arguments: argBytes,
		})
	}

	// Also support bracket-style directives occasionally emitted by models.
	// Pattern: [fs.list] path: .
	bracketRe := regexp.MustCompile(`(?m)^\s*\[(fs\.\w+|code\.\w+|shell\.\w+|time\.\w+)\]\s*(.*)$`)
	bracketMatches := bracketRe.FindAllStringSubmatch(content, -1)
	for _, match := range bracketMatches {
		if len(match) < 2 {
			continue
		}
		toolName := strings.TrimSpace(match[1])
		argsStr := ""
		if len(match) >= 3 {
			argsStr = strings.TrimSpace(match[2])
		}
		args := parseArgsString(argsStr)
		argBytes, _ := json.Marshal(args)
		toolCalls = append(toolCalls, agent.ToolCallRequest{
			ID:        fmt.Sprintf("tool-bracket-%d", len(toolCalls)+1),
			Name:      toolName,
			Arguments: argBytes,
		})
	}

	// Support one-line command style: fs.list /path
	toolLineRe := regexp.MustCompile(`(?m)^\s*(fs\.\w+|code\.\w+|shell\.\w+|time\.\w+)\s+([^\n]+)$`)
	toolLineMatches := toolLineRe.FindAllStringSubmatch(content, -1)
	for _, match := range toolLineMatches {
		if len(match) < 3 {
			continue
		}
		toolName := strings.TrimSpace(match[1])
		args := parseArgsString(strings.TrimSpace(match[2]))
		argBytes, _ := json.Marshal(args)
		toolCalls = append(toolCalls, agent.ToolCallRequest{
			ID:        fmt.Sprintf("tool-line-%d", len(toolCalls)+1),
			Name:      toolName,
			Arguments: argBytes,
		})
	}

	// Map common shell snippets to workspace-safe tools.
	for _, call := range parseShellSnippets(content, len(toolCalls)+1) {
		toolCalls = append(toolCalls, call)
	}

	// Also support XML-like tool markers occasionally emitted by models.
	// Pattern: <tool_call>fs.read ...
	xmlNameRe := regexp.MustCompile(`(?is)^\s*(fs\.\w+|code\.\w+|shell\.\w+|time\.\w+)\b`)
	segments := strings.Split(content, "<tool_call>")
	if len(segments) > 1 {
		for _, seg := range segments[1:] {
			match := xmlNameRe.FindStringSubmatch(seg)
			if len(match) < 2 {
				continue
			}
			toolName := strings.TrimSpace(match[1])
			argsStr := strings.TrimSpace(strings.TrimPrefix(seg, match[0]))
			argsStr = xmlTagRe.ReplaceAllString(argsStr, " ")
			args := parseArgsString(argsStr)
			argBytes, _ := json.Marshal(args)
			toolCalls = append(toolCalls, agent.ToolCallRequest{
				ID:        fmt.Sprintf("tool-xml-%d", len(toolCalls)+1),
				Name:      toolName,
				Arguments: argBytes,
			})
		}
	}

	return dedupeToolCalls(toolCalls)
}

func parseShellSnippets(content string, ordinalStart int) []agent.ToolCallRequest {
	listRe := regexp.MustCompile(`(?m)^\s*ls(?:\s+-[A-Za-z]+)*\s*(.*?)\s*$`)
	catRe := regexp.MustCompile(`(?m)^\s*cat\s+([^\s]+)\s*$`)

	calls := make([]agent.ToolCallRequest, 0, 4)
	nextOrdinal := ordinalStart

	listMatches := listRe.FindAllStringSubmatch(content, -1)
	for _, match := range listMatches {
		path := "."
		if len(match) >= 2 {
			rest := strings.TrimSpace(match[1])
			if rest != "" {
				parts := strings.Fields(rest)
				if len(parts) > 0 {
					path = parts[len(parts)-1]
				}
			}
		}
		argBytes, _ := json.Marshal(map[string]any{"path": path})
		calls = append(calls, agent.ToolCallRequest{ID: fmt.Sprintf("tool-shell-%d", nextOrdinal), Name: "fs.list", Arguments: argBytes})
		nextOrdinal++
	}

	catMatches := catRe.FindAllStringSubmatch(content, -1)
	for _, match := range catMatches {
		if len(match) < 2 {
			continue
		}
		path := strings.TrimSpace(match[1])
		if path == "" {
			continue
		}
		argBytes, _ := json.Marshal(map[string]any{"path": path})
		calls = append(calls, agent.ToolCallRequest{ID: fmt.Sprintf("tool-shell-%d", nextOrdinal), Name: "fs.read", Arguments: argBytes})
		nextOrdinal++
	}

	return calls
}

func dedupeToolCalls(calls []agent.ToolCallRequest) []agent.ToolCallRequest {
	if len(calls) < 2 {
		return calls
	}
	seen := make(map[string]struct{}, len(calls))
	out := make([]agent.ToolCallRequest, 0, len(calls))
	for _, call := range calls {
		key := call.Name + "|" + string(call.Arguments)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, call)
	}
	return out
}

func parseJSONToolCall(raw string, ordinal int) (agent.ToolCallRequest, bool) {
	obj := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return agent.ToolCallRequest{}, false
	}

	toolName := strings.TrimSpace(firstString(obj, "tool_name", "tool", "tool_code", "name", "function", "function_name"))
	if toolName == "" {
		return agent.ToolCallRequest{}, false
	}
	if !regexp.MustCompile(`^(fs|code|shell|time)\.\w+$`).MatchString(toolName) {
		return agent.ToolCallRequest{}, false
	}

	args := map[string]any{}
	if nested, ok := obj["arguments"].(map[string]any); ok {
		for k, v := range nested {
			args[k] = v
		}
	} else if nested, ok := obj["args"].(map[string]any); ok {
		for k, v := range nested {
			args[k] = v
		}
	} else if nested, ok := obj["parameters"].(map[string]any); ok {
		for k, v := range nested {
			args[k] = v
		}
	} else {
		for k, v := range obj {
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "tool", "tool_name", "tool_code", "name", "function", "function_name", "id", "arguments", "args", "parameters":
				continue
			default:
				args[k] = v
			}
		}
	}

	argBytes, _ := json.Marshal(args)
	return agent.ToolCallRequest{
		ID:        fmt.Sprintf("tool-json-%d", ordinal),
		Name:      toolName,
		Arguments: argBytes,
	}, true
}

func extractJSONObjectCandidates(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if json.Valid([]byte(trimmed)) {
			return []string{trimmed}
		}
	}

	candidates := make([]string, 0, 4)
	start := -1
	depth := 0
	inString := false
	escaped := false

	for i, r := range content {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}

		if r == '"' {
			inString = true
			continue
		}

		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
			continue
		}

		if r == '}' && depth > 0 {
			depth--
			if depth == 0 && start >= 0 {
				raw := strings.TrimSpace(content[start : i+1])
				if json.Valid([]byte(raw)) {
					candidates = append(candidates, raw)
				}
				start = -1
			}
		}
	}

	return candidates
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := obj[key].(string); ok {
			return v
		}
	}
	return ""
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

	// Parse key=value or key:value pairs
	// Pattern: key="value", key='value', key=value, key:value, or just "value" (positional)
	re := regexp.MustCompile(`(\w+)\s*[:=]\s*("[^"]*"|'[^']*'|[^,\s]+)`)
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
