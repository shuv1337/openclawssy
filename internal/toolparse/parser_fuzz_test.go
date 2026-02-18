package toolparse

import "testing"

func FuzzParseStrictNoPanic(f *testing.F) {
	seeds := []string{
		"plain text",
		"```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
		"```tool\n{\"tool_name\":\"tool.result\",\"arguments\":{}}\n```",
		"```json\n{not-json}\n```",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = ParseStrict(input, []string{"fs.list", "fs.read", "fs.write", "fs.delete", "fs.move", "config.get", "config.set", "secrets.get", "secrets.set", "secrets.list", "scheduler.list", "scheduler.add", "scheduler.remove", "scheduler.pause", "scheduler.resume", "session.list", "session.close", "run.list", "run.get", "http.request", "shell.exec"}, 1)
	})
}
