package objects

import "time"

type ObjectUsageRow struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Kind     string    `json:"kind"`
}
type UsageCursor struct{ Bucket, Prefix, Marker, Version string }
