package commandlog

import (
	"context"
	"time"
)

type Repository interface {
	// Reserve returns fresh=true only after exclusively owning a new command.
	// Existing records never grant execution, even after their lease expires.
	Reserve(context.Context, Scope, string, string, time.Time) (entry Entry, fresh bool, err error)
	// Finish stores one neutral result, or marks the outcome Unknown when nil.
	Finish(context.Context, string, Scope, *Response) error
	Export(context.Context) ([]Entry, error)
	// Import accepts only an empty store or an identical subset of the batch.
	// This allows a failed offline restore to resume without replacing fences.
	Import(context.Context, []Entry) error
	Ping(context.Context) error
	Close() error
}
