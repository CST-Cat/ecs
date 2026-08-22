package model

// AddFailure appends or coalesces a structured failure. Machine dimensions
// form the identity; Message remains diagnostic context.
func (r *Result) AddFailure(failure Failure) {
	if failure.Category == "" {
		failure.Category = FailureUnknown
	}
	if failure.Count <= 0 {
		failure.Count = 1
	}
	for index := range r.Failures {
		current := &r.Failures[index]
		if current.Category == failure.Category && current.Stage == failure.Stage &&
			current.Target == failure.Target && current.Retryable == failure.Retryable &&
			current.Message == failure.Message {
			current.Count += failure.Count
			return
		}
	}
	r.Failures = append(r.Failures, failure)
}
