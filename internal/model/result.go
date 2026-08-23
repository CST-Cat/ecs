package model

import "time"

func NewResult(id, title string) Result {
	return Result{
		ID:        id,
		Title:     title,
		Status:    StatusOK,
		StartedAt: time.Now().UTC(),
	}
}

func (r *Result) Finish(start time.Time) {
	r.StartedAt = start.UTC()
	r.DurationMS = time.Since(start).Milliseconds()
}

func (r *Result) Skip(reason Message) {
	r.Status = StatusSkipped
	r.SummaryMessages = []Message{cloneMessage(reason)}
}

func (r *Result) Fail(err error) {
	r.Status = StatusError
	r.Error = err.Error()
	r.SummaryMessages = []Message{NewMessage("message.result.failed")}
}
