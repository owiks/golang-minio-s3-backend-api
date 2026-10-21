package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/revision"
)

type PoolOperationRow struct {
	ID        int       `json:"id"`
	State     string    `json:"state"`
	Started   time.Time `json:"started"`
	Objects   int64     `json:"objects"`
	Failed    int64     `json:"failed"`
	Bytes     int64     `json:"bytes"`
	Remaining int64     `json:"remaining"`
}
type RebalanceRow struct {
	ID       int     `json:"id"`
	State    string  `json:"state"`
	Used     float64 `json:"used"`
	Objects  uint64  `json:"objects"`
	Versions uint64  `json:"versions"`
	Bytes    uint64  `json:"bytes"`
	Elapsed  float64 `json:"elapsed"`
	ETA      float64 `json:"eta"`
}
type PoolsView struct {
	CheckedAt      time.Time          `json:"checkedAt"`
	Supported      bool               `json:"supported"`
	Reason         messages.Ref       `json:"reason,omitempty"`
	Pools          []PoolOperationRow `json:"pools"`
	RebalanceID    string             `json:"rebalanceId"`
	Rebalancing    bool               `json:"rebalancing"`
	RebalanceKnown bool               `json:"rebalanceKnown"`
	Rebalance      []RebalanceRow     `json:"rebalance"`
	RebalanceError messages.Ref       `json:"rebalanceError,omitempty"`
	Revision       string             `json:"revision"`
}

func Draining(p PoolOperationRow) bool { return p.State == "draining" }
func RebalanceActive(state string) bool {
	switch strings.ToLower(state) {
	case "", "none", "completed", "stopped", "failed", "not-started":
		return false
	}
	return true
}
func ReadPools(ctx context.Context, s ReadPoolsClient) (PoolsView, []madmin.PoolStatus, error) {
	v := PoolsView{CheckedAt: time.Now().UTC(), Supported: true, Pools: []PoolOperationRow{}, Rebalance: []RebalanceRow{}}
	raw, e := s.ListPoolsStatus(ctx)
	if e != nil {
		if apperror.Public(ServerOperationError(e)).Status == 501 {
			if _, probe := s.ServerInfo(ctx); probe != nil {
				return v, nil, probe
			}
			v.Supported = false
			v.Reason = messages.ServerPoolManagementIsOnlyAvailableInA
			return v, nil, nil
		}
		return v, nil, ServerOperationError(e)
	}
	for _, p := range raw {
		row := PoolOperationRow{ID: p.ID + 1, State: "active"}
		if d := p.Decommission; d != nil && (!d.StartTime.IsZero() || d.Complete || d.Canceled || d.Failed) {
			row.Started = d.StartTime
			row.Objects = d.ObjectsDecommissioned
			row.Failed = d.ObjectsDecommissionFailed
			row.Bytes = d.BytesDone
			row.Remaining = d.CurrentSize
			row.State = "draining"
			if d.Complete {
				row.State = "complete"
			} else if d.Canceled {
				row.State = "canceled"
			} else if d.Failed {
				row.State = "failed"
			}
		}
		v.Pools = append(v.Pools, row)
	}
	status, e := s.RebalanceStatus(ctx)
	code := ""
	var native madmin.ErrorResponse
	if errors.As(e, &native) {
		code = native.Code
	}
	if e != nil && code != "XMinioAdminRebalanceNotStarted" && code != "XMinioRebalanceNotStarted" {
		v.RebalanceError = messages.ServerCouldNotCheckRebalancingChangesAreBlocked
	} else {
		v.RebalanceKnown = true
		v.RebalanceID = status.ID
		for _, p := range status.Pools {
			v.Rebalancing = v.Rebalancing || RebalanceActive(p.Status)
			v.Rebalance = append(v.Rebalance, RebalanceRow{ID: p.ID + 1, State: p.Status, Used: p.Used * 100, Objects: p.Progress.NumObjects, Versions: p.Progress.NumVersions, Bytes: p.Progress.Bytes, Elapsed: p.Progress.Elapsed.Seconds(), ETA: p.Progress.ETA.Seconds()})
		}
		if !status.StoppedAt.IsZero() {
			v.Rebalancing = false
			for i := range v.Rebalance {
				if RebalanceActive(v.Rebalance[i].State) {
					v.Rebalance[i].State = "Stopped"
				}
			}
		}
	}
	sort.Slice(v.Pools, func(i, j int) bool { return v.Pools[i].ID < v.Pools[j].ID })
	// Progress counters deliberately do not invalidate an open confirmation.
	states := []string{}
	for _, p := range v.Pools {
		states = append(states, fmt.Sprintf("%d:%s", p.ID, p.State))
	}
	topology := []string{}
	for _, p := range raw {
		topology = append(topology, fmt.Sprintf("%d:%s", p.ID, p.CmdLine))
	}
	b, _ := json.Marshal([]any{states, topology, v.RebalanceID, v.Rebalancing, v.RebalanceKnown})
	v.Revision = revision.Of(b)
	return v, raw, nil
}
func PoolHealth(info madmin.InfoMessage, target int) error {
	if len(info.Servers) == 0 {
		return apperror.New(409, "UnknownPoolHealth", messages.ServerUnknownPoolHealth)
	}
	hasTarget := false
	for _, n := range info.Servers {
		if n.State != "online" && n.State != "ok" {
			return apperror.New(409, "UnhealthyPools", messages.ServerUnhealthyPools)
		}
		for _, d := range n.Disks {
			if d.State != "ok" && d.State != "online" || d.Healing {
				return apperror.New(409, "UnhealthyPools", messages.ServerUnhealthyPoolsServerPools)
			}
			if d.PoolIndex+1 == target {
				hasTarget = true
			}
		}
	}
	if target > 0 && !hasTarget {
		return apperror.New(409, "UnknownPoolHealth", messages.ServerUnknownPoolHealthServerPools)
	}
	return nil
}
