package app

import "strings"

// earlyFlagOccurrence is the small subset of flag information needed before a
// command-specific flag set exists.  Keeping the occurrences ordered lets
// callers apply their own notion of a valid value while preserving last-wins
// semantics.
type earlyFlagOccurrence struct {
	Name  string
	Value string
}

// scanEarlyFlags finds selected flag-value pairs without interpreting the
// command's complete flag set.  It intentionally scans past positional
// arguments because --lang is accepted before or after a command, but an exact
// -- ends the early scan just like the standard flag package.
//
// A separate value is accepted only when it is not option-like.  This prevents
// a missing value from consuming the next flag or delimiter.  Empty --name=
// values are ignored as invalid occurrences.
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
		name, value, hasValue := splitEarlyFlag(args[index])
		if _, ok := wanted[name]; !ok {
			continue
		}
		if hasValue {
			if value != "" {
				occurrences = append(occurrences, earlyFlagOccurrence{Name: name, Value: value})
			}
			continue
		}
		if index+1 >= len(args) || args[index+1] == "--" || strings.HasPrefix(args[index+1], "-") {
			continue
		}
		occurrences = append(occurrences, earlyFlagOccurrence{Name: name, Value: args[index+1]})
		index++
	}
	return occurrences
}

// lastExplicitLanguage returns the final --lang/-lang occurrence without
// interpreting its value.  Unlike scanEarlyFlags, it preserves an empty or
// missing value so the command entry point can reject it instead of silently
// falling back to the environment.
func lastExplicitLanguage(args []string) (value string, found, missing bool) {
	for index := 0; index < len(args); index++ {
		if args[index] == "--" {
			break
		}
		name, occurrenceValue, hasValue := splitEarlyFlag(args[index])
		if name != "lang" {
			continue
		}
		found = true
		if hasValue {
			value = occurrenceValue
			missing = false
			continue
		}
		if index+1 >= len(args) || args[index+1] == "--" || strings.HasPrefix(args[index+1], "-") {
			missing = true
			continue
		}
		value = args[index+1]
		missing = false
		index++
	}
	return value, found, missing
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
