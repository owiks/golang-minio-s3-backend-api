package server

import (
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/minio/madmin-go/v3"
)

type DiagnosticRow struct {
	Time     string   `json:"time"`
	Node     string   `json:"node"`
	Level    string   `json:"level"`
	API      string   `json:"api"`
	Method   string   `json:"method,omitempty"`
	Path     string   `json:"path,omitempty"`
	Status   int      `json:"status,omitempty"`
	Duration *float64 `json:"durationMs,omitempty"`
	Input    *int     `json:"input,omitempty"`
	Output   *int     `json:"output,omitempty"`
}

func DiagnosticToken(v string) string {
	if len(v) > 128 {
		return "—"
	}
	for _, c := range v {
		if !(unicode.IsLetter(c) || unicode.IsDigit(c) || strings.ContainsRune("._-:[]", c)) {
			return "—"
		}
	}
	return v
}
func DiagnosticPath(v string) string {
	u, err := url.Parse(v)
	if err != nil {
		return "—"
	}
	p := u.Path
	if len(p) > 1024 {
		p = p[:1024]
	}
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, p)
}
func TraceRow(t madmin.TraceInfo) DiagnosticRow {
	duration := float64(t.Duration) / float64(time.Millisecond)
	row := DiagnosticRow{Time: t.Time.UTC().Format(time.RFC3339Nano), Node: DiagnosticToken(t.NodeName), API: DiagnosticToken(t.FuncName), Duration: &duration, Path: DiagnosticPath(t.Path), Level: "INFO"}
	if t.HTTP != nil {
		row.Method = DiagnosticToken(t.HTTP.ReqInfo.Method)
		row.Path = DiagnosticPath(t.HTTP.ReqInfo.Path)
		row.Status = t.HTTP.RespInfo.StatusCode
		input, output := t.HTTP.CallStats.InputBytes, t.HTTP.CallStats.OutputBytes
		row.Input = &input
		row.Output = &output
		if row.Status >= 400 {
			row.Level = "ERROR"
		}
	}
	return row
}
