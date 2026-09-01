package probe

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// commandVersion reads the first useful line from the standard version flags
// used by the external tools. A failed version probe is intentionally rendered
// as unknown; the caller still decides whether the tool can run.
func commandVersion(ctx context.Context, path string) string {
	for _, argument := range []string{"--version", "-V"} {
		versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		output, err := exec.CommandContext(versionCtx, path, argument).CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(sanitizeCommandOutput(output), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return "unknown"
}

func sanitizeCommandOutput(output []byte) string {
	text := strings.ReplaceAll(string(output), "\x00", "")
	text = ansiPattern.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// tailText keeps the last limit bytes, advancing to the next rune boundary so a
// multi-byte character is never cut in half. Probe diagnostics are frequently
// Chinese, where a byte-exact cut would emit a replacement character.
func tailText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	tail := text[len(text)-limit:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return tail
}
