package server

import (
	"bytes"
	"context"
	"math"
	"sort"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

type ApiMetric struct {
	API            string             `json:"api"`
	Node           string             `json:"node,omitempty"`
	LatencyBuckets map[string]float64 `json:"latencyBuckets,omitempty"`
	Requests       *float64           `json:"requests"`
	Errors4xx      *float64           `json:"errors4xx"`
	Errors5xx      *float64           `json:"errors5xx"`
	Inflight       *float64           `json:"inflight"`
	DurationSum    *float64           `json:"durationSum"`
	DurationCount  *float64           `json:"durationCount"`
}
type LiveMetrics struct {
	Traffic   map[string]*TrafficCounter `json:"traffic,omitempty"`
	Limited   bool                       `json:"limited,omitempty"`
	CheckedAt time.Time                  `json:"checkedAt"`
	Input     *float64                   `json:"input"`
	Output    *float64                   `json:"output"`
	Waiting   *float64                   `json:"waiting"`
	APIs      []ApiMetric                `json:"apis"`
}
type TrafficCounter struct {
	Input  *float64 `json:"input"`
	Output *float64 `json:"output"`
}

func MetricAdd(dst **float64, value float64) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return
	}
	if *dst == nil {
		*dst = new(float64)
	}
	**dst += value
}
func LiveMetricRows(families map[string]*dto.MetricFamily) LiveMetrics {
	v := LiveMetrics{CheckedAt: time.Now().UTC(), APIs: []ApiMetric{}, Traffic: map[string]*TrafficCounter{}}
	rows := map[string]*ApiMetric{}
	for name, f := range families {
		if !strings.HasPrefix(name, "minio_s3_") {
			continue
		}
		for _, m := range f.Metric {
			value := m.GetCounter().GetValue()
			if m.Gauge != nil {
				value = m.GetGauge().GetValue()
			}
			trafficNode := ""
			for _, l := range m.Label {
				if l.GetName() == "server" {
					trafficNode = DiagnosticToken(l.GetValue())
				}
			}
			if name == "minio_s3_traffic_received_bytes" || name == "minio_s3_traffic_sent_bytes" {
				counter := v.Traffic[trafficNode]
				if counter == nil && len(v.Traffic) < 512 {
					counter = &TrafficCounter{}
					v.Traffic[trafficNode] = counter
				}
				if counter != nil {
					if name == "minio_s3_traffic_received_bytes" {
						MetricAdd(&counter.Input, value)
					} else {
						MetricAdd(&counter.Output, value)
					}
				} else {
					v.Limited = true
				}
			}
			switch name {
			case "minio_s3_traffic_received_bytes":
				MetricAdd(&v.Input, value)
				continue
			case "minio_s3_traffic_sent_bytes":
				MetricAdd(&v.Output, value)
				continue
			case "minio_s3_requests_waiting_total":
				MetricAdd(&v.Waiting, value)
				continue
			}
			api, node, bound := "", "", ""
			for _, l := range m.Label {
				if l.GetName() == "api" {
					api = l.GetValue()
				}
				if l.GetName() == "server" {
					node = DiagnosticToken(l.GetValue())
				}
				if l.GetName() == "le" {
					bound = l.GetValue()
				}
			}
			if api == "" || len(api) > 80 || DiagnosticToken(api) != api {
				continue
			}
			known := name == "minio_s3_requests_total" || name == "minio_s3_requests_4xx_errors_total" || name == "minio_s3_requests_5xx_errors_total" || name == "minio_s3_requests_inflight_total" || name == "minio_s3_requests_duration_seconds" || name == "minio_s3_requests_ttfb_seconds_distribution"
			if !known {
				continue
			}
			rowKey := api + "\x00" + node
			row := rows[rowKey]
			if row == nil {
				if len(rows) >= 512 {
					v.Limited = true
					continue
				}
				row = &ApiMetric{API: api, Node: node, LatencyBuckets: map[string]float64{}}
				rows[rowKey] = row
			}
			switch name {
			case "minio_s3_requests_total":
				MetricAdd(&row.Requests, value)
			case "minio_s3_requests_4xx_errors_total":
				MetricAdd(&row.Errors4xx, value)
			case "minio_s3_requests_5xx_errors_total":
				MetricAdd(&row.Errors5xx, value)
			case "minio_s3_requests_inflight_total":
				MetricAdd(&row.Inflight, value)
			case "minio_s3_requests_ttfb_seconds_distribution":
				if len(row.LatencyBuckets) < 64 && len(bound) < 20 && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
					row.LatencyBuckets[bound] = value
				}
			case "minio_s3_requests_duration_seconds":
				if m.Histogram != nil {
					MetricAdd(&row.DurationSum, m.Histogram.GetSampleSum())
					MetricAdd(&row.DurationCount, float64(m.Histogram.GetSampleCount()))
				}
			}
		}
	}
	for _, row := range rows {
		v.APIs = append(v.APIs, *row)
	}
	sort.Slice(v.APIs, func(i, j int) bool { return v.APIs[i].API+v.APIs[i].Node < v.APIs[j].API+v.APIs[j].Node })
	return v
}
func ReadLiveMetrics(ctx context.Context, s ReadLiveMetricsClient) (LiveMetrics, error) {
	raw, e := s.ReadS3Metrics(ctx)
	if e != nil {
		return LiveMetrics{}, ServerOperationError(e)
	}
	var parser expfmt.TextParser
	families, e := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if e != nil {
		return LiveMetrics{}, apperror.New(502, "InvalidMetrics", messages.ServerInvalidMetrics)
	}
	return LiveMetricRows(families), nil
}
