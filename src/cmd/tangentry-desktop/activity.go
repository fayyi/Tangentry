package main

import (
	"sync"

	"github.com/egoist/mygo"

	"tangentry/internal/jobs"
)

// jobActivity keeps the computer awake while downloads or transcriptions run,
// and notifies when one ends while no Tangentry window is in front.
//
// Services report events from their own goroutines. MyGo calls wait for the
// main thread, which waits for those goroutines while the app closes, so
// events pass through a buffer to a goroutine of their own: a slow or
// stopped UI never holds up a service.
type jobActivity struct {
	events *jobEventQueue
	focus  func()
}

func startJobActivity(focus func()) *jobActivity {
	a := &jobActivity{events: newJobEventQueue(), focus: focus}
	go a.run()
	return a
}

// Observe never blocks. Pending updates for the same job are coalesced, so a
// slow native UI cannot lose the final state or retain a stale keep-awake hold.
func (a *jobActivity) Observe(event jobs.Event) {
	a.events.Put(event)
}

func (a *jobActivity) run() {
	active := make(map[string]bool)
	var release func()
	for {
		event := a.events.Take()
		key := event.Kind + ":" + event.ID
		if event.Active {
			active[key] = true
		} else {
			delete(active, key)
		}
		switch {
		case len(active) > 0 && release == nil:
			release = mygo.Power.KeepAwake("Tangentry 正在下载或转录", false)
		case len(active) == 0 && release != nil:
			release()
			release = nil
		}
		if event.Outcome != "" {
			a.notify(event)
		}
	}
}

type jobEventQueue struct {
	mu      sync.Mutex
	ready   *sync.Cond
	order   []string
	pending map[string]jobs.Event
}

func newJobEventQueue() *jobEventQueue {
	queue := &jobEventQueue{pending: make(map[string]jobs.Event)}
	queue.ready = sync.NewCond(&queue.mu)
	return queue
}

func (q *jobEventQueue) Put(event jobs.Event) {
	key := event.Kind + ":" + event.ID
	q.mu.Lock()
	if previous, exists := q.pending[key]; exists && previous.Terminal && !event.Terminal {
		q.mu.Unlock()
		return
	}
	if _, exists := q.pending[key]; !exists {
		q.order = append(q.order, key)
	}
	q.pending[key] = event
	q.ready.Signal()
	q.mu.Unlock()
}

func (q *jobEventQueue) Take() jobs.Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.order) == 0 {
		q.ready.Wait()
	}
	key := q.order[0]
	q.order[0] = ""
	q.order = q.order[1:]
	event := q.pending[key]
	delete(q.pending, key)
	return event
}

var jobMessages = map[string]map[string][2]string{
	"download": {
		"completed": {"下载完成", "已保存到下载目录。"},
		"failed":    {"下载失败", "打开下载页查看原因并重试。"},
	},
	"transcription": {
		"completed": {"转录完成", "字幕已保存，可以开始阅读。"},
		"failed":    {"转录失败", "打开阅读页查看原因并重试。"},
	},
}

func (a *jobActivity) notify(event jobs.Event) {
	message, ok := jobMessages[event.Kind][event.Outcome]
	if !ok || mygo.FocusedWindow() != nil || !mygo.NotificationsSupported() {
		return
	}
	title, body := message[0], message[1]
	if event.Title != "" {
		body = event.Title
	}
	notification := mygo.NewNotification(mygo.NotificationOptions{ID: event.Kind + "/" + event.ID, Group: event.Kind, Title: title, Body: body})
	notification.OnClick(func() {
		a.focus()
		notification.Close()
	})
	_ = notification.Show() // denied notifications only lose the reminder
}
