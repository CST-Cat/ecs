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
func scanEarlyFlags(args []string, names ...string) []earlyFlagOccurrence {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimLeft(name, "-")
		if name != "" {
			wanted[name] = struct{}{}
		}
	}

	var occurrences []earlyFlagOccurrence
	for index := 0; index < len(args); index++ {
		if args[index] == "--" {
			break
		}
		name, value, hasEquals := splitEarlyFlag(args[index])
		if _, ok := wanted[name]; !ok {
			continue
		}
		occurrence := earlyFlagOccurrence{
			Name: name, Value: value, Position: index, End: index + 1,
			HasEquals: hasEquals,
		}
		if hasEquals {
			occurrence.HasValue = true
			occurrences = append(occurrences, occurrence)
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
	}
	return occurrences
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
