package server

import (
	"sort"
	"time"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type NetworkLink struct {
	Endpoint string `json:"endpoint"`
	State    string `json:"state"`
}
type NodeGC struct {
	Count   int64     `json:"count"`
	PauseMS float64   `json:"pauseMs"`
	Last    time.Time `json:"last"`
}
type NodeInfo struct {
	Endpoint   string        `json:"endpoint"`
	State      string        `json:"state"`
	Version    string        `json:"version"`
	Uptime     int64         `json:"uptime"`
	Pools      []int         `json:"pools"`
	Memory     uint64        `json:"memory"`
	CPU        int           `json:"cpu"`
	GoMaxProcs int           `json:"goMaxProcs"`
	Runtime    string        `json:"runtime"`
	Commit     string        `json:"commit"`
	Edition    string        `json:"edition"`
	Network    []NetworkLink `json:"network"`
	GC         *NodeGC       `json:"gc"`
}
type DriveMetrics struct {
	Waiting  uint32 `json:"waiting"`
	Errors   uint64 `json:"errors"`
	Timeouts uint64 `json:"timeouts"`
}
type DriveInfo struct {
	Server     string        `json:"server"`
	Path       string        `json:"path"`
	State      string        `json:"state"`
	Total      uint64        `json:"total"`
	Used       uint64        `json:"used"`
	Available  uint64        `json:"available"`
	Healing    bool          `json:"healing"`
	Scanning   bool          `json:"scanning"`
	UUID       string        `json:"uuid"`
	Model      string        `json:"model"`
	Major      uint32        `json:"major"`
	Minor      uint32        `json:"minor"`
	Pool       int           `json:"pool"`
	Set        int           `json:"set"`
	Disk       int           `json:"disk"`
	UsedInodes uint64        `json:"usedInodes"`
	FreeInodes uint64        `json:"freeInodes"`
	Metrics    *DriveMetrics `json:"metrics"`
}
type PoolInfo struct {
	Number       int `json:"number"`
	Sets         int `json:"sets"`
	DrivesPerSet int `json:"drivesPerSet"`
}
type ScannerUsage struct {
	UpdatedAt time.Time `json:"updatedAt"`
	Buckets   uint64    `json:"buckets"`
	Objects   uint64    `json:"objects"`
	Size      uint64    `json:"size"`
}
type ServerSnapshot struct {
	CheckedAt      time.Time     `json:"checkedAt"`
	Mode           string        `json:"mode"`
	Region         string        `json:"region"`
	Backend        string        `json:"backend"`
	StandardParity int           `json:"standardParity"`
	ReducedParity  int           `json:"reducedParity"`
	Pools          []PoolInfo    `json:"pools"`
	Nodes          []NodeInfo    `json:"nodes"`
	Drives         []DriveInfo   `json:"drives"`
	Usage          *ScannerUsage `json:"usage"`
	UsageError     messages.Ref  `json:"usageError,omitempty"`
}

func ServerRows(info madmin.InfoMessage) ServerSnapshot {
	result := ServerSnapshot{CheckedAt: time.Now().UTC(), Mode: info.Mode, Region: info.Region, Backend: string(info.Backend.Type), StandardParity: info.Backend.StandardSCParity, ReducedParity: info.Backend.RRSCParity, Pools: []PoolInfo{}, Nodes: []NodeInfo{}, Drives: []DriveInfo{}}
	for index, sets := range info.Backend.TotalSets {
		perSet := 0
		if index < len(info.Backend.DrivesPerSet) {
			perSet = info.Backend.DrivesPerSet[index]
		}
		result.Pools = append(result.Pools, PoolInfo{Number: index + 1, Sets: sets, DrivesPerSet: perSet})
	}
	for _, node := range info.Servers {
		pools := append([]int{}, node.PoolNumbers...)
		row := NodeInfo{Endpoint: node.Endpoint, State: node.State, Version: node.Version, Uptime: node.Uptime, Pools: pools, Memory: node.MemStats.Alloc, CPU: node.NumCPU, GoMaxProcs: node.GoMaxProcs, Runtime: node.RuntimeVersion, Commit: node.CommitID, Edition: node.Edition, Network: []NetworkLink{}}
		for endpoint, state := range node.Network {
			// Do not forward arbitrary upstream error messages (which may contain secrets).
			switch state {
			case "online", "offline":
			case "connection attempt timedout":
				state = "timeout"
			default:
				state = "unknown"
			}
			row.Network = append(row.Network, NetworkLink{Endpoint: endpoint, State: state})
		}
		sort.Slice(row.Network, func(i, j int) bool { return row.Network[i].Endpoint < row.Network[j].Endpoint })
		if node.GCStats != nil {
			row.GC = &NodeGC{Count: node.GCStats.NumGC, PauseMS: float64(node.GCStats.PauseTotal) / float64(time.Millisecond), Last: node.GCStats.LastGC}
		}
		result.Nodes = append(result.Nodes, row)
		for _, disk := range node.Disks {
			drive := DriveInfo{Server: node.Endpoint, Path: disk.DrivePath, State: disk.State, Total: disk.TotalSpace, Used: disk.UsedSpace, Available: disk.AvailableSpace, Healing: disk.Healing, Scanning: disk.Scanning, UUID: disk.UUID, Model: disk.Model, Major: disk.Major, Minor: disk.Minor, Pool: disk.PoolIndex + 1, Set: disk.SetIndex + 1, Disk: disk.DiskIndex + 1, UsedInodes: disk.UsedInodes, FreeInodes: disk.FreeInodes}
			if disk.Metrics != nil {
				drive.Metrics = &DriveMetrics{Waiting: disk.Metrics.TotalWaiting, Errors: disk.Metrics.TotalErrorsAvailability, Timeouts: disk.Metrics.TotalErrorsTimeout}
			}
			result.Drives = append(result.Drives, drive)
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].Endpoint < result.Nodes[j].Endpoint })
	sort.Slice(result.Drives, func(i, j int) bool {
		a, b := result.Drives[i], result.Drives[j]
		return a.Server+"\x00"+a.Path < b.Server+"\x00"+b.Path
	})
	return result
}
