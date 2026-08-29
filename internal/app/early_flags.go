package app

import "strings"

// earlyFlagOccurrence is the small subset of flag information needed before a
// command-specific flag set exists. Position and End are indexes into the
// original argv, with End exclusive. HasEquals distinguishes --name=value
// from a separate value; HasValue remains true for an explicitly empty value,
// while Missing identifies a flag with no value at all.
type earlyFlagOccurrence struct {
	Name      string
	Value     string
	Position  int
	End       int
	HasValue  bool
	HasEquals bool
	Missing   bool
}

// scanEarlyFlags is the only argv scanner used by the app's early parsing.
// It finds selected flags without interpreting the command's complete flag
// set, records every occurrence (including missing and empty values), and
// stops at an exact --. A separate value is accepted only when it is not
// option-like, so a missing value never consumes another flag or delimiter.
// When a non-selected option may consume the next token, that token is left
// alone for the real parser; this is important when it happens to look like a
// global --lang flag.
func scanEarlyFlags(args []string, names ...string) []earlyFlagOccurrence {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimLeft(name, "-")
		if name != "" {
			wanted[name] = struct{}{}
		}
	}

	var occurrences []earlyFlagOccurrence
	pendingValue := false
	lastSelectedMissing := false
	for index := 0; index < len(args); index++ {
		if args[index] == "--" {
			break
		}
		name, value, hasEquals := splitEarlyFlag(args[index])
		if pendingValue {
			pendingValue = false
			// A formal value may look like --lang, but keep scanning other
			// selected names for the scanner's existing multi-name contract.
			// Once a selected occurrence was already malformed, continue to
			// record later occurrences so last-occurrence diagnostics remain
			// unchanged.
			if name == "lang" && !lastSelectedMissing {
				continue
			}
		}
		if _, ok := wanted[name]; ok {
			occurrence := earlyFlagOccurrence{
				Name: name, Value: value, Position: index, End: index + 1,
				HasEquals: hasEquals,
			}
			if hasEquals {
				occurrence.HasValue = true
				occurrences = append(occurrences, occurrence)
				lastSelectedMissing = false
				continue
			}
			if index+1 < len(args) && args[index+1] != "--" && !strings.HasPrefix(args[index+1], "-") {
				occurrence.Value = args[index+1]
				occurrence.HasValue = true
				occurrence.End = index + 2
				index++
			} else {
				occurrence.Missing = true
			}
			occurrences = append(occurrences, occurrence)
			lastSelectedMissing = occurrence.Missing
			continue
		}
		if name != "" && !hasEquals && earlyFlagHasNoValue(name) {
			continue
		}
		if name != "" && !hasEquals {
			pendingValue = true
		}
	}
	return occurrences
}

// earlyFlagHasNoValue only supplies the small exceptions needed to keep
// scanning a global language flag after a boolean option. It is not a second
// command parser: all other option names are conservatively treated as
// value-taking, and the real command FlagSet remains the grammar owner.
func earlyFlagHasNoValue(name string) bool {
	switch name {
	case "lang", "h", "help", "4", "6", "reveal", "no-color", "disk-multi",
		"interactive", "yes", "strict", "version", "annotate", "verbose":
		return true
	default:
		return false
	}
}

// stripExplicitLanguage removes every global language occurrence before the
// command-specific flag set sees argv.  Invalid occurrences are removed too:
// Main validates the final occurrence before dispatching, so a subcommand has
// no second language source and cannot report a different interpretation.
func stripExplicitLanguage(args []string, occurrences []earlyFlagOccurrence) []string {
	cleaned := make([]string, 0, len(args))
	position := 0
	for _, occurrence := range occurrences {
		cleaned = append(cleaned, args[position:occurrence.Position]...)
		position = occurrence.End
	}
	cleaned = append(cleaned, args[position:]...)
	return cleaned
}

func splitEarlyFlag(argument string) (name, value string, hasValue bool) {
	var raw string
	switch {
	case strings.HasPrefix(argument, "--"):
		raw = strings.TrimPrefix(argument, "--")
	case strings.HasPrefix(argument, "-"):
		raw = strings.TrimPrefix(argument, "-")
	default:
		return "", "", false
	}
	if raw == "" {
		return "", "", false
	}
	if equal := strings.IndexByte(raw, '='); equal >= 0 {
		if equal == 0 {
			return "", "", true
		}
		return raw[:equal], raw[equal+1:], true
	}
	return raw, "", false
}
