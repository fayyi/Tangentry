package ai

import (
	"context"
	"sync"
)

// MemoryResults is a small ResultStore for tests and tools without a database.
// It deliberately implements the same port as the SQLite result adapter.
type MemoryResults struct {
	mu   sync.Mutex
	rows map[[3]string]StoredResult
}

func NewMemoryResults() *MemoryResults {
	return &MemoryResults{rows: map[[3]string]StoredResult{}}
}

func (m *MemoryResults) AIResult(_ context.Context, kind, sourceHash, target string) (StoredResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[[3]string{kind, sourceHash, target}]
	if !ok {
		return StoredResult{}, ErrNotFound
	}
	return row, nil
}

func (m *MemoryResults) AIResults(_ context.Context, kind, target string) ([]StoredResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []StoredResult{}
	for _, row := range m.rows {
		if row.Kind == kind && row.Target == target {
			out = append(out, row)
		}
	}
	return out, nil
}

func (m *MemoryResults) PutAIResult(_ context.Context, row StoredResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := [3]string{row.Kind, row.SourceHash, row.Target}
	if previous, ok := m.rows[key]; ok {
		row.CreatedAt = previous.CreatedAt
	}
	m.rows[key] = row
	return nil
}

func (m *MemoryResults) DeleteAIResult(_ context.Context, kind, sourceHash, target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, [3]string{kind, sourceHash, target})
	return nil
}

// Count reports stored rows, for tests.
func (m *MemoryResults) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}
