package main

import (
	"testing"

	"tangentry/internal/jobs"
)

func TestJobEventQueueCoalescesProgressButKeepsTerminalState(t *testing.T) {
	queue := newJobEventQueue()
	queue.Put(jobs.Event{Kind: "download", ID: "one", Active: true})
	queue.Put(jobs.Event{Kind: "download", ID: "one", Active: true, Title: "updated"})
	queue.Put(jobs.Event{Kind: "download", ID: "one", Outcome: "completed", Terminal: true})
	queue.Put(jobs.Event{Kind: "download", ID: "two", Active: true})

	first := queue.Take()
	if first.ID != "one" || first.Outcome != "completed" || first.Active {
		t.Fatalf("first event = %+v", first)
	}
	second := queue.Take()
	if second.ID != "two" || !second.Active {
		t.Fatalf("second event = %+v", second)
	}
}

func TestJobEventQueueDoesNotReplaceTerminalStateWithStaleProgress(t *testing.T) {
	queue := newJobEventQueue()
	queue.Put(jobs.Event{Kind: "transcription", ID: "one", Outcome: "failed", Terminal: true})
	queue.Put(jobs.Event{Kind: "transcription", ID: "one", Active: true})

	event := queue.Take()
	if event.Outcome != "failed" || event.Active {
		t.Fatalf("terminal event was replaced: %+v", event)
	}
}

func TestJobEventQueueDoesNotReviveCanceledJob(t *testing.T) {
	queue := newJobEventQueue()
	queue.Put(jobs.Event{Kind: "download", ID: "one", Active: true})
	queue.Put(jobs.Event{Kind: "download", ID: "one", Terminal: true})
	queue.Put(jobs.Event{Kind: "download", ID: "one", Active: true, Title: "late update"})

	if event := queue.Take(); !event.Terminal || event.Active {
		t.Fatalf("canceled terminal event was lost: %+v", event)
	}
}
