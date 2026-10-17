package app

import (
	"tangentry/internal/downloads"
	"tangentry/internal/jobs"
	"tangentry/internal/transcription"
)

// JobEvent reports long-running work to the desktop shell, which keeps the
// computer awake while work is active and tells the user when it ends.
type JobEvent = jobs.Event

// ObserveJobs delivers job events from service goroutines. observe must not block.
func (a *Application) ObserveJobs(observe func(jobs.Event)) {
	a.runtime.observeJobs(observe)
}

func (r *runtimeState) observeJobs(observe func(jobs.Event)) {
	r.downloads.Observe(func(job downloads.Job) {
		event := JobEvent{Kind: "download", ID: job.ID, Title: job.Title, Active: job.State == "running"}
		if job.State == "completed" || job.State == "failed" {
			event.Outcome = job.State
		}
		event.Terminal = job.State == "completed" || job.State == "failed" || job.State == "cancelled" || job.State == "interrupted"
		observe(event)
	})
	r.transcription.Observe(func(job transcription.Job) {
		event := JobEvent{Kind: "transcription", ID: job.ID}
		switch job.State {
		case "completed", "failed":
			event.Outcome = job.State
		case "canceled":
			event.Terminal = true
		default:
			event.Active = true
		}
		if job.State == "completed" || job.State == "failed" {
			event.Terminal = true
		}
		observe(event)
	})
}
