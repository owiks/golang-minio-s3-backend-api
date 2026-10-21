package server

import "time"

type DiagnosticsReport struct {
	Rows    []DiagnosticRow `json:"rows"`
	Started time.Time       `json:"started"`
	Ended   time.Time       `json:"ended"`
	Limited bool            `json:"limited"`
}
