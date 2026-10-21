// Package mutation observes command effects independently of returned errors.
// It stores no request data and never retries or executes commands.
package mutation

import (
	"context"
	"sync/atomic"
)

type Effect uint32

const (
	Unchanged Effect = iota
	Changed
	Uncertain
)

type contextKey struct{}

// Scope aggregates effects within one use case/request. A later rejection must
// not erase an earlier change; any uncertain command keeps the result uncertain.
type Scope struct{ effect atomic.Uint32 }

func WithScope(ctx context.Context) (context.Context, *Scope) {
	s := &Scope{}
	return context.WithValue(ctx, contextKey{}, s), s
}

func (s *Scope) Effect() Effect { return Effect(s.effect.Load()) }

func Record(ctx context.Context, effect Effect) {
	s, _ := ctx.Value(contextKey{}).(*Scope)
	if s == nil || effect != Changed && effect != Uncertain {
		return
	}
	for {
		previous := s.effect.Load()
		if previous >= uint32(effect) || s.effect.CompareAndSwap(previous, uint32(effect)) {
			return
		}
	}
}
