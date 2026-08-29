package report

import "ecs/internal/model"

// resultTitle resolves the canonical title carried by the result. Renderer
// output must not maintain an ID-specific title table beside machine metadata.
func resultTitle(result model.Result) string {
	return displayKey(result.Title)
}
