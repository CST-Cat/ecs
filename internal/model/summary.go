package model

func Summarize(report *Report) {
	var summary Summary
	for _, result := range report.Results {
		switch result.Status {
		case StatusOK:
			summary.OK++
		case StatusWarning:
			summary.Warnings++
		case StatusSkipped:
			summary.Skipped++
		case StatusError:
			summary.Errors++
		}
	}
	switch {
	case summary.Errors > 0:
		summary.Status = StatusError
		summary.Messages = append(summary.Messages, NewMessage("message.summary.withErrors", summary.OK, summary.Errors))
	case summary.Warnings > 0:
		summary.Status = StatusWarning
		summary.Messages = append(summary.Messages, NewMessage("message.summary.withWarnings", summary.OK, summary.Warnings))
	default:
		summary.Status = StatusOK
		summary.Messages = append(summary.Messages, NewMessage("message.summary.allOK", summary.OK))
	}
	if summary.Skipped > 0 {
		summary.Messages = append(summary.Messages, NewMessage("message.summary.skipped", summary.Skipped))
	}
	report.Summary = summary
}
