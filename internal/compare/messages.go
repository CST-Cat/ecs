package compare

import (
	"fmt"
	"strings"
)

// canonicalNotice preserves machine message semantics without encoding them
// into a presentation string. Renderers consume Key and Args directly.
func canonicalNotice(key string, args ...string) Notice {
	return Notice{Key: key, Args: append([]string(nil), args...)}
}

// ValidationError is the language-independent error returned by Build for
// invalid comparison options. Error returns a concise key/argument expression;
// the CLI chooses how to render the key at its user-facing boundary.
type ValidationError struct {
	Key  string
	Args []any
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if len(e.Args) == 0 {
		return e.Key
	}
	values := make([]string, len(e.Args))
	for index, arg := range e.Args {
		values[index] = fmt.Sprint(arg)
	}
	return e.Key + "(" + strings.Join(values, ",") + ")"
}

func newValidationError(key string, args ...any) error {
	return &ValidationError{Key: key, Args: args}
}
