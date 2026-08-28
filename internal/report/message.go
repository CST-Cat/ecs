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
		args[index] = renderMessageArg(message.Key, index, arg)
	}
	return fmt.Sprintf(format, args...)
}

// renderMessageArg handles the one message contract whose argument carries an
// explicit presentation key. All other message arguments remain raw text.
func renderMessageArg(messageKey string, index int, arg string) string {
	if index == 3 && (messageKey == "probe.network.summary.version" || messageKey == "probe.network.summary.version.additional") {
		return displayKey(arg)
	}
	return arg
}

func renderMessages(messages []model.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(renderMessage(message))
	}
	return builder.String()
}
