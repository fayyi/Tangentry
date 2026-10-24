package worker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
)

var ErrWorkerStopped = errors.New("automatic subtitle worker stopped")

// Config describes one local recognizer process. The executable is supplied by
// the desktop runtime so the domain package never depends on a bundled model
// or a platform-specific binary name.
type Config struct {
	Executable string
	Args       []string
	SessionID  string
}

// Manager owns one worker process and serializes writes to its framed stdin.
// Events are delivered in order; process failures are reported by Err().
type Manager struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	events      chan Event
	done        chan error
	stop        chan struct{}
	mu          sync.Mutex
	closed      bool
	closeEvents sync.Once
	finish      sync.Once
	waitOnce    sync.Once
	waitErr     error
}

func Start(ctx context.Context, config Config) (*Manager, error) {
	if config.Executable == "" || config.SessionID == "" {
		return nil, errors.New("worker executable and session are required")
	}
	cmd := exec.CommandContext(ctx, config.Executable, config.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	manager := &Manager{cmd: cmd, stdin: stdin, events: make(chan Event, 32), done: make(chan error, 1), stop: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	go manager.read(stdout)
	if err := manager.write(Event{Type: "start", SessionID: config.SessionID}); err != nil {
		_ = manager.Stop()
		return nil, err
	}
	return manager, nil
}

func (m *Manager) read(stdout io.ReadCloser) {
	reader := bufio.NewReader(stdout)
	for {
		event, pcm, err := ReadFrame(reader)
		if err != nil {
			result := ErrWorkerStopped
			if !errors.Is(err, io.EOF) {
				result = err
			}
			if processErr := m.wait(); processErr != nil && errors.Is(result, ErrWorkerStopped) {
				result = processErr
			}
			m.finish.Do(func() { m.done <- result; close(m.done) })
			m.closeEvents.Do(func() { close(m.events) })
			return
		}
		// Worker output is expected to be JSON. Ignore accidental PCM output
		// rather than exposing untrusted audio as a subtitle event.
		if pcm != nil {
			continue
		}
		select {
		case m.events <- event:
		case <-m.stop:
			m.closeEvents.Do(func() { close(m.events) })
			return
		}
	}
}

func (m *Manager) write(event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrWorkerStopped
	}
	return WriteJSON(m.stdin, event)
}

func (m *Manager) WritePCM(pcm []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrWorkerStopped
	}
	return WritePCM(m.stdin, pcm)
}

func (m *Manager) Pause() error  { return m.write(Event{Type: "pause"}) }
func (m *Manager) Resume() error { return m.write(Event{Type: "resume"}) }

func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.stop)
	_ = WriteJSON(m.stdin, Event{Type: "stop"})
	_ = m.stdin.Close()
	m.mu.Unlock()
	return m.wait()
}

func (m *Manager) wait() error {
	m.waitOnce.Do(func() { m.waitErr = m.cmd.Wait() })
	return m.waitErr
}

func (m *Manager) Events() <-chan Event { return m.events }
func (m *Manager) Err() <-chan error    { return m.done }
