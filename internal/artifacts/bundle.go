package artifacts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const baseBundleDir = ".openclawssy"

// WriteRunBundle writes a run bundle under:
// .openclawssy/agents/<agentID>/runs/<runID>/
func WriteRunBundle(rootDir, agentID, runID string, input, output, toolCalls, meta any) (string, error) {
	runDir := filepath.Join(rootDir, baseBundleDir, "agents", agentID, "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("create run bundle dir: %w", err)
	}

	if err := writeJSONAtomic(filepath.Join(runDir, "input.json"), input); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "output.json"), output); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "tool_calls.json"), toolCalls); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "meta.json"), meta); err != nil {
		return "", err
	}

	return runDir, nil
}

type BundleV1Input struct {
	Input      any
	PromptMD   string
	ToolCalls  []string
	OutputMD   string
	Meta       map[string]any
	MirrorJSON bool
}

func WriteRunBundleV1(rootDir, agentID, runID string, payload BundleV1Input) (string, error) {
	runDir := filepath.Join(rootDir, baseBundleDir, "agents", agentID, "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("create run bundle dir: %w", err)
	}
	if payload.Meta == nil {
		payload.Meta = map[string]any{}
	}
	payload.Meta["bundle_version"] = 1

	if err := writeJSONAtomic(filepath.Join(runDir, "input.json"), payload.Input); err != nil {
		return "", err
	}
	if err := writeTextAtomic(filepath.Join(runDir, "prompt.md"), payload.PromptMD); err != nil {
		return "", err
	}
	if err := writeJSONLAtomic(filepath.Join(runDir, "toolcalls.jsonl"), payload.ToolCalls); err != nil {
		return "", err
	}
	if err := writeTextAtomic(filepath.Join(runDir, "output.md"), payload.OutputMD); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(runDir, "meta.json"), payload.Meta); err != nil {
		return "", err
	}

	if payload.MirrorJSON {
		_ = writeJSONAtomic(filepath.Join(runDir, "output.json"), map[string]any{"final_text": payload.OutputMD})
		_ = writeJSONAtomic(filepath.Join(runDir, "tool_calls.json"), payload.ToolCalls)
	}

	return runDir, nil
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	base := filepath.Base(path)

	tmp, err := os.CreateTemp(dir, base+".tmp-")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", base, err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp for %s: %w", base, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp for %s: %w", base, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp for %s: %w", base, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp for %s: %w", base, err)
	}

	return nil
}

func writeTextAtomic(path string, body string) error {
	if body != "" && body[len(body)-1] != '\n' {
		body += "\n"
	}
	return writeBytesAtomic(path, []byte(body))
}

func writeJSONLAtomic(path string, lines []string) error {
	buf := make([]byte, 0, len(lines)*64)
	for _, line := range lines {
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return writeBytesAtomic(path, buf)
}

func writeBytesAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, base+".tmp-")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", base, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	w := bufio.NewWriter(tmp)
	if _, err := w.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp for %s: %w", base, err)
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("flush temp for %s: %w", base, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp for %s: %w", base, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp for %s: %w", base, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp for %s: %w", base, err)
	}
	return nil
}
