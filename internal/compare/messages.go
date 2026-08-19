package compare

import (
	"encoding/json"
	"fmt"
	"strings"
)

// canonicalNotice stores a stable message key and, when needed, JSON encoded
// string arguments. Notices are part of the machine comparison result, so
// their value is independent of presentation language. The separator cannot
// occur in a message key and remains intact through terminal sanitization.
func canonicalNotice(key string, args ...string) string {
	if len(args) == 0 {
		return key
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		// []string is always JSON encodable. Keep the key as the safest
		// canonical fallback if this ever changes.
		return key
	}
	return key + "::" + string(encoded)
}

// ParseNotice decodes the stable key and optional arguments carried by a
// comparison notice. It deliberately knows nothing about translations;
// presentation packages decide whether the returned key is displayable.
func ParseNotice(value string) (string, []string, bool) {
	key, encoded, hasArgs := strings.Cut(value, "::")
	if !hasArgs {
		if key == "" {
			return "", nil, false
		}
		return key, nil, true
	}
	if key == "" {
		return "", nil, false
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil {
		return "", nil, false
	}
	return key, args, true
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
