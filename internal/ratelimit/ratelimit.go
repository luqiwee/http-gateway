package ratelimit

import (
	"sync"
	"time"
)

type Manager struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewManager() *Manager {
	return &Manager{
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

func (m *Manager) Allow(key string, rate float64, burst int) bool {
	if rate <= 0 || burst <= 0 {
		return false
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.buckets[key]
	if !ok {
		m.buckets[key] = &bucket{tokens: float64(burst - 1), last: now}
		return true
	}

	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * rate
	if maxTokens := float64(burst); b.tokens > maxTokens {
		b.tokens = maxTokens
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buckets = make(map[string]*bucket)
}
