package batch

import (
	"github.com/minio/madmin-go/v3"
	"time"
)

// Job is the stable public contract, independent of SDK struct evolution.
type Job struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	User    string        `json:"user,omitempty"`
	Started time.Time     `json:"started"`
	Elapsed time.Duration `json:"elapsed,omitempty"`
}

func jobResult(value madmin.BatchJobResult) Job {
	return Job{ID: value.ID, Type: string(value.Type), User: value.User, Started: value.Started, Elapsed: value.Elapsed}
}
