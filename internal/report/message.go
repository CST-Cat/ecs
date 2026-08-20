package report

import (
	"fmt"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

// renderMessage is the presentation boundary for model.Message. The model only
// carries stable semantics; language selection and formatting happen here.
func renderMessage(message model.Message) string {
	if message.Key == "" {
		return ""
	}
	format := i18n.T(message.Key)
	if format == message.Key || len(message.Args) == 0 {
		return format
	}
	args := make([]any, len(message.Args))
	for index, arg := range message.Args {
		args[index] = arg
	}
	return fmt.Sprintf(format, args...)
}

func renderMessages(messages []model.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(renderMessage(message))
	}
	return builder.String()
}
