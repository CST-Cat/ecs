package probe

import (
	"encoding/json"
	"strings"
)

// comparisonParameterRevision changes only when the rules used to build a
// module's machine comparison scope change. Measurement.Method remains the
// version for the workload itself.
const comparisonParameterRevision = "2"

// newComparisonParameters creates the language-independent scope map owned by
// a producer. The helper deliberately contains no module schema: producers
// decide which structured facts belong in their own scope.
func newComparisonParameters() map[string]string {
	return map[string]string{"scope_revision": comparisonParameterRevision}
}

func addComparisonParameter(parameters map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		parameters[key] = value
	}
}

// comparisonParameterJSON preserves ordered runtime inputs and tagged
// model.Value selections without hiding already-public values behind a hash.
func comparisonParameterJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func addComparisonParameterJSON(parameters map[string]string, key string, value any) {
	addComparisonParameter(parameters, key, comparisonParameterJSON(value))
}
