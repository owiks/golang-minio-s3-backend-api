package coordination

import (
	"sync"
	"time"
)

// Admission enforces both process and principal budgets atomically.
// Principals are session IDs or hashed network addresses; never credentials.
type Admission struct {
	mu                   sync.Mutex
	active               int
	principals           map[string]int
	global, perPrincipal int
}

func NewAdmission(global, perPrincipal int) *Admission {
	return &Admission{global: global, perPrincipal: perPrincipal, principals: map[string]int{}}
}
func (a *Admission) Try(key string) (func(), bool) {
	a.mu.Lock()
	if a.active >= a.global || a.principals[key] >= a.perPrincipal {
		a.mu.Unlock()
		return nil, false
	}
	a.active++
	a.principals[key]++
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.active--
			a.principals[key]--
			if a.principals[key] == 0 {
				delete(a.principals, key)
			}
		})
	}, true
}

type bucket struct {
	tokens  float64
	updated time.Time
}

// Attempts uses a token bucket with a bounded table. When full it fails closed
// rather than evicting clients and allowing address churn to bypass limits.
type Attempts struct {
	mu                sync.Mutex
	entries           map[string]bucket
	burst, maxEntries int
	interval          time.Duration
}

func NewAttempts(burst, maxEntries int, interval time.Duration) *Attempts {
	return &Attempts{entries: map[string]bucket{}, burst: burst, maxEntries: maxEntries, interval: interval}
}
func (a *Attempts) Allow(key string, now time.Time) (bool, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, b := range a.entries {
		if now.Sub(b.updated) >= time.Duration(a.burst)*a.interval {
			delete(a.entries, id)
		}
	}
	b, exists := a.entries[key]
	if !exists {
		if len(a.entries) >= a.maxEntries {
			return false, a.interval
		}
		b = bucket{tokens: float64(a.burst), updated: now}
	}
	elapsed := max(0, now.Sub(b.updated).Seconds())
	b.tokens = min(float64(a.burst), b.tokens+elapsed/a.interval.Seconds())
	b.updated = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	a.entries[key] = b
	if allowed {
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(a.interval))
}
func (a *Admission) Active() int { a.mu.Lock(); defer a.mu.Unlock(); return a.active }
