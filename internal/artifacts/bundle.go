package artifacts

import (
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
