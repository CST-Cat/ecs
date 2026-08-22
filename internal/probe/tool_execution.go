package probe

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
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

func binarySHA256(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func sanitizeCommandOutput(output []byte) string {
	text := strings.ReplaceAll(string(output), "\x00", "")
	text = ansiPattern.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

func tailText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}
