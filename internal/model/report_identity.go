package model

import (
	"fmt"
	"strings"
)

// ValidateReportIdentity checks the identity contract shared by report
// readers, writers, and consumers. Result IDs own their measurements, so a
// measurement key may repeat across results but must be unique within its
// owning result.
func ValidateReportIdentity(report Report) error {
	resultIndexes := make(map[string]int, len(report.Results))
	for resultIndex, result := range report.Results {
		if strings.TrimSpace(result.ID) == "" {
			return fmt.Errorf("report contains empty result ID at index %d", resultIndex)
		}
		if previous, ok := resultIndexes[result.ID]; ok {
			return fmt.Errorf("report contains duplicate result ID %q at indexes %d and %d", result.ID, previous, resultIndex)
		}
		resultIndexes[result.ID] = resultIndex

		measurementIndexes := make(map[string]int, len(result.Measurements))
		for measurementIndex, measurement := range result.Measurements {
			if strings.TrimSpace(measurement.Key) == "" {
				return fmt.Errorf("result %q contains empty measurement key at index %d", result.ID, measurementIndex)
			}
			if previous, ok := measurementIndexes[measurement.Key]; ok {
				return fmt.Errorf("result %q contains duplicate measurement key %q at indexes %d and %d", result.ID, measurement.Key, previous, measurementIndex)
			}
			measurementIndexes[measurement.Key] = measurementIndex
		}
	}
	return nil
}
