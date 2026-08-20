package model

import "fmt"

// Message stores ECS-generated human-facing semantics without choosing a
// presentation language. Key is stable machine identity; Args are opaque
// substitution values interpreted only by the presentation layer.
type Message struct {
	Key  string   `json:"key"`
	Args []string `json:"args,omitempty"`
}

// NewMessage builds a Message while keeping the serialized argument contract
// string-only. Converting at the producer boundary avoids teaching model about
// any language or formatting catalog.
func NewMessage(key string, args ...any) Message {
	message := Message{Key: key}
	if len(args) == 0 {
		return message
	}
	message.Args = make([]string, len(args))
	for index, arg := range args {
		message.Args[index] = fmt.Sprint(arg)
	}
	return message
}

func cloneMessages(messages []Message) []Message {
	if messages == nil {
		return nil
	}
	out := make([]Message, len(messages))
	for index, message := range messages {
		out[index] = message
		out[index].Args = append([]string(nil), message.Args...)
	}
	return out
}
