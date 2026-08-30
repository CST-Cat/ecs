package app

import "strings"

type languageFlagOccurrence struct {
	Value   string
	Missing bool
}

// globalLanguagePrefix consumes only consecutive --lang/-lang flags at the
// start of argv. Once another token is reached, the command's real parser owns
// the rest of argv, including option-looking string values and an exact --.
func globalLanguagePrefix(args []string) ([]languageFlagOccurrence, []string) {
	var occurrences []languageFlagOccurrence
	index := 0
	for index < len(args) {
		value, hasEquals, ok := splitLanguageFlag(args[index])
		if !ok {
			break
		}
		occurrence := languageFlagOccurrence{Value: value}
		if hasEquals {
			occurrences = append(occurrences, occurrence)
			index++
			continue
		}
		if index+1 < len(args) && args[index+1] != "--" && !strings.HasPrefix(args[index+1], "-") {
			occurrence.Value = args[index+1]
			index += 2
		} else {
			occurrence.Missing = true
			index++
		}
		occurrences = append(occurrences, occurrence)
	}
	return occurrences, args[index:]
}

func splitLanguageFlag(argument string) (value string, hasEquals, ok bool) {
	switch {
	case argument == "--lang", argument == "-lang":
		return "", false, true
	case strings.HasPrefix(argument, "--lang="):
		return strings.TrimPrefix(argument, "--lang="), true, true
	case strings.HasPrefix(argument, "-lang="):
		return strings.TrimPrefix(argument, "-lang="), true, true
	default:
		return "", false, false
	}
}
