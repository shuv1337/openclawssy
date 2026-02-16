package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

const (
	defaultProviderTimeout = 90 * time.Second
	providerMaxAttempts    = 2
	providerRetryBackoff   = 700 * time.Millisecond
	toolNamePattern        = `[A-Za-z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*`
)

var toolNameAliases = map[string]string{
	"fs.read":       "fs.read",
	"fs.list":       "fs.list",
	"fs.write":      "fs.write",
	"fs.edit":       "fs.edit",
	"code.search":   "code.search",
	"time.now":      "time.now",
	"shell.exec":    "shell.exec",
	"bash.exec":     "shell.exec",
	"terminal.exec": "shell.exec",
	"terminal.run":  "shell.exec",
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
		httpClient:   &http.Client{Timeout: defaultProviderTimeout},
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

	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	statusCode, err := m.doChatCompletionWithRetry(ctx, raw, &payload)
	if err != nil {
		return agent.ModelResponse{}, err
	}
	if statusCode >= 300 {
		return agent.ModelResponse{}, fmt.Errorf("provider %s request failed: status=%d error=%v", m.providerName, statusCode, payload.Error)
	}
	if len(payload.Choices) == 0 {
		return agent.ModelResponse{}, errors.New("provider returned no choices")
	}

	content := strings.TrimSpace(payload.Choices[0].Message.Content)

	// Check if the model's response contains tool calls in JSON blocks.
	toolCalls := parseToolCallsFromResponse(content, req.Message)
	if len(toolCalls) == 0 {
		toolCalls = parseToolCallsFromResponse(stripThinkingTags(content), req.Message)
	}
	if len(toolCalls) > 0 {
		return agent.ModelResponse{ToolCalls: toolCalls}, nil
	}

	return agent.ModelResponse{FinalText: stripThinkingTags(content)}, nil
}

func (m *ProviderModel) doChatCompletionWithRetry(ctx context.Context, raw []byte, payload any) (int, error) {
	if m.httpClient == nil {
		m.httpClient = &http.Client{Timeout: defaultProviderTimeout}
	}

	var lastErr error
	for attempt := 1; attempt <= providerMaxAttempts; attempt++ {
		statusCode, err := m.doChatCompletionOnce(ctx, raw, payload)
		if err == nil {
			return statusCode, nil
		}
		lastErr = err
		if !shouldRetryProviderError(err) || attempt == providerMaxAttempts {
			return 0, err
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(providerRetryBackoff):
		}
	}

	if lastErr != nil {
		return 0, lastErr
	}
	return 0, errors.New("provider request failed")
}

func (m *ProviderModel) doChatCompletionOnce(ctx context.Context, raw []byte, payload any) (int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range m.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(payload); err != nil {
		return 0, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return resp.StatusCode, fmt.Errorf("retryable provider status: %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func shouldRetryProviderError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return strings.Contains(strings.ToLower(err.Error()), "retryable provider status") ||
		strings.Contains(strings.ToLower(err.Error()), "timeout") ||
		strings.Contains(strings.ToLower(err.Error()), "too many requests")
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
	toolName, ok := canonicalToolName(parts[0])
	if !ok {
		return agent.ModelResponse{}, fmt.Errorf("unsupported tool name: %s", strings.TrimSpace(parts[0]))
	}
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

func parseToolCallsFromResponse(content, userMessage string) []agent.ToolCallRequest {
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

		if call, ok := parseJSONToolCall(jsonContent, i+1); ok {
			toolCalls = append(toolCalls, call)
			continue
		}

		// Try parsing as direct tool call syntax: tool_name(args)
		// Pattern: fs.list(path=".") or fs.list(".")
		if call := parseFunctionCall(jsonContent); call != nil {
			toolCalls = append(toolCalls, *call)
		}
	}

	// Also look for inline function calls not in code blocks
	// Pattern: fs.list(path=".") or fs.list(".")
	inlineRe := regexp.MustCompile(`\b(` + toolNamePattern + `)\s*\(([^)]*)\)`)
	inlineMatches := inlineRe.FindAllStringSubmatch(content, -1)
	for _, match := range inlineMatches {
		if len(match) >= 2 {
			toolName, ok := canonicalToolName(match[1])
			if !ok {
				continue
			}
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

	// Support fenced bash/sh command snippets by mapping to shell.exec.
	for _, call := range parseBashCodeBlocks(content, len(toolCalls)+1) {
		toolCalls = append(toolCalls, call)
	}

	// Support XML function blocks occasionally emitted by models.
	// Pattern: <function=fs.list><path>.</path></function>
	xmlFunctionRe := regexp.MustCompile(`(?is)<function=(` + toolNamePattern + `)>\s*(.*?)\s*</function>`)
	xmlFunctionMatches := xmlFunctionRe.FindAllStringSubmatch(content, -1)
	for _, match := range xmlFunctionMatches {
		if len(match) < 2 {
			continue
		}
		toolName, ok := canonicalToolName(match[1])
		if !ok {
			continue
		}
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
	bracketRe := regexp.MustCompile(`(?m)^\s*\[(` + toolNamePattern + `)\]\s*(.*)$`)
	bracketMatches := bracketRe.FindAllStringSubmatch(content, -1)
	for _, match := range bracketMatches {
		if len(match) < 2 {
			continue
		}
		toolName, ok := canonicalToolName(match[1])
		if !ok {
			continue
		}
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
	toolLineRe := regexp.MustCompile(`(?m)^\s*(` + toolNamePattern + `)\s+([^\n]+)$`)
	toolLineMatches := toolLineRe.FindAllStringSubmatch(content, -1)
	for _, match := range toolLineMatches {
		if len(match) < 3 {
			continue
		}
		toolName, ok := canonicalToolName(match[1])
		if !ok {
			continue
		}
		args := parseArgsString(strings.TrimSpace(match[2]))
		argBytes, _ := json.Marshal(args)
		toolCalls = append(toolCalls, agent.ToolCallRequest{
			ID:        fmt.Sprintf("tool-line-%d", len(toolCalls)+1),
			Name:      toolName,
			Arguments: argBytes,
		})
	}

	// Map common shell snippets to workspace-safe tools.
	for _, call := range parseShellSnippets(removeFencedCodeBlocks(content), len(toolCalls)+1) {
		toolCalls = append(toolCalls, call)
	}

	if len(toolCalls) == 0 {
		if call, ok := synthesizeWriteCallFromResponse(content, userMessage, len(toolCalls)+1); ok {
			toolCalls = append(toolCalls, call)
		}
	}

	// Also support XML-like tool markers occasionally emitted by models.
	// Pattern: <tool_call>fs.read ...
	xmlNameRe := regexp.MustCompile(`(?is)^\s*(` + toolNamePattern + `)\b`)
	segments := strings.Split(content, "<tool_call>")
	if len(segments) > 1 {
		for _, seg := range segments[1:] {
			match := xmlNameRe.FindStringSubmatch(seg)
			if len(match) < 2 {
				continue
			}
			toolName, ok := canonicalToolName(match[1])
			if !ok {
				continue
			}
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

func synthesizeWriteCallFromResponse(content, userMessage string, ordinal int) (agent.ToolCallRequest, bool) {
	if !looksLikeCreateRequest(userMessage) {
		return agent.ToolCallRequest{}, false
	}

	path := extractFilenameFromText(userMessage)
	if path == "" {
		path = extractFilenameFromText(content)
	}
	if path == "" {
		return agent.ToolCallRequest{}, false
	}

	body := extractCodeBody(content)
	if strings.TrimSpace(body) == "" {
		return agent.ToolCallRequest{}, false
	}

	argBytes, _ := json.Marshal(map[string]any{
		"path":    path,
		"content": body,
	})
	return agent.ToolCallRequest{
		ID:        fmt.Sprintf("tool-synth-%d", ordinal),
		Name:      "fs.write",
		Arguments: argBytes,
	}, true
}

func looksLikeCreateRequest(message string) bool {
	m := strings.ToLower(strings.TrimSpace(message))
	if m == "" {
		return false
	}
	if !strings.Contains(m, "create") && !strings.Contains(m, "write") && !strings.Contains(m, "make") && !strings.Contains(m, "save") {
		return false
	}
	return regexp.MustCompile(`\.[A-Za-z0-9]{1,8}\b`).MatchString(m)
}

func extractFilenameFromText(text string) string {
	re := regexp.MustCompile(`([A-Za-z0-9_./-]+\.[A-Za-z0-9]{1,8})`)
	matches := re.FindAllString(text, -1)
	for _, m := range matches {
		m = strings.TrimSpace(strings.Trim(m, `"'`))
		if strings.HasPrefix(m, "http://") || strings.HasPrefix(m, "https://") {
			continue
		}
		if strings.Contains(m, "..") {
			continue
		}
		return m
	}
	return ""
}

func extractCodeBody(content string) string {
	fenced := regexp.MustCompile("```(?:[A-Za-z0-9_+-]+)?\\s*([\\s\\S]*?)```")
	blocks := fenced.FindAllStringSubmatch(content, -1)
	if len(blocks) > 0 {
		best := ""
		for _, block := range blocks {
			if len(block) < 2 {
				continue
			}
			candidate := strings.TrimSpace(block[1])
			if len(candidate) > len(best) {
				best = candidate
			}
		}
		if best != "" {
			return best
		}
	}

	lines := strings.Split(content, "\n")
	if len(lines) >= 4 {
		if strings.HasPrefix(strings.TrimSpace(lines[0]), "#") {
			return strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}

	return ""
}

func parseBashCodeBlocks(content string, ordinalStart int) []agent.ToolCallRequest {
	re := regexp.MustCompile("(?is)```(?:bash|sh)\\s*([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	calls := make([]agent.ToolCallRequest, 0, len(matches))
	nextOrdinal := ordinalStart
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		block := strings.TrimSpace(match[1])
		if block == "" {
			continue
		}

		lines := strings.Split(block, "\n")
		cleaned := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "$ ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "$ "))
			}
			if line == "" {
				continue
			}
			cleaned = append(cleaned, line)
		}
		if len(cleaned) == 0 {
			continue
		}

		script := strings.Join(cleaned, "\n")
		argBytes, _ := json.Marshal(map[string]any{
			"command": "bash",
			"args":    []string{"-lc", script},
		})
		calls = append(calls, agent.ToolCallRequest{
			ID:        fmt.Sprintf("tool-bash-%d", nextOrdinal),
			Name:      "shell.exec",
			Arguments: argBytes,
		})
		nextOrdinal++
	}

	return calls
}

func removeFencedCodeBlocks(content string) string {
	re := regexp.MustCompile("```(?:[A-Za-z0-9_+-]+)?\\s*[\\s\\S]*?```")
	return re.ReplaceAllString(content, "")
}

func parseShellSnippets(content string, ordinalStart int) []agent.ToolCallRequest {
	listRe := regexp.MustCompile(`(?m)^\s*(?:\$\s*)?ls(?:\s+-[A-Za-z]+)*\s*(.*?)\s*$`)
	catRe := regexp.MustCompile(`(?m)^\s*(?:\$\s*)?cat\s+([^\s]+)\s*$`)

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

	toolName, ok := canonicalToolName(firstString(obj, "tool_name", "tool", "tool_code", "name", "function", "function_name"))
	if !ok || toolName == "" {
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

	toolName, ok := canonicalToolName(matches[1])
	if !ok {
		return nil
	}
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

func canonicalToolName(name string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return "", false
	}
	canonical, ok := toolNameAliases[key]
	if !ok {
		return "", false
	}
	return canonical, true
}
