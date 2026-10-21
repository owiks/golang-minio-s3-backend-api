package server

import (
	"context"
	"fmt"

	"github.com/minio/madmin-go/v3"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type PoolChangeRequest struct {
	Operation    string `json:"operation"`
	Pool         int    `json:"pool"`
	Revision     string `json:"revision"`
	Confirmation string `json:"confirmation"`
}

type PoolCommand interface {
	poolChange() (operation string, pool int, revision, confirmation string)
}
type StartRebalanceCommand struct{ Revision, Confirmation string }
type StopRebalanceCommand struct{ Revision, Confirmation string }
type DecommissionPoolCommand struct {
	Pool                   int
	Revision, Confirmation string
}
type CancelDecommissionCommand struct {
	Pool                   int
	Revision, Confirmation string
}

func (c StartRebalanceCommand) poolChange() (string, int, string, string) {
	return "start", 0, c.Revision, c.Confirmation
}
func (c StopRebalanceCommand) poolChange() (string, int, string, string) {
	return "stop", 0, c.Revision, c.Confirmation
}
func (c DecommissionPoolCommand) poolChange() (string, int, string, string) {
	return "decommission", c.Pool, c.Revision, c.Confirmation
}
func (c CancelDecommissionCommand) poolChange() (string, int, string, string) {
	return "cancel", c.Pool, c.Revision, c.Confirmation
}
func (b PoolChangeRequest) Command() (PoolCommand, error) {
	switch b.Operation {
	case "start":
		return StartRebalanceCommand{b.Revision, b.Confirmation}, nil
	case "stop":
		return StopRebalanceCommand{b.Revision, b.Confirmation}, nil
	case "decommission":
		return DecommissionPoolCommand{b.Pool, b.Revision, b.Confirmation}, nil
	case "cancel":
		return CancelDecommissionCommand{b.Pool, b.Revision, b.Confirmation}, nil
	}
	return nil, apperror.New(400, "InvalidPoolOperation", messages.ServerInvalidPoolOperation)
}

func ChangePools(ctx context.Context, s ChangePoolsClient, command PoolCommand) error {
	switch command.(type) {
	case StartRebalanceCommand, StopRebalanceCommand, DecommissionPoolCommand, CancelDecommissionCommand:
	default:
		return apperror.New(400, "InvalidPoolOperation", messages.ServerInvalidPoolOperationPoolChange)
	}
	operation, pool, revision, confirmation := command.poolChange()
	b := PoolChangeRequest{Operation: operation, Pool: pool, Revision: revision, Confirmation: confirmation}
	permission := "admin:Rebalance"
	phrase := ""
	switch b.Operation {
	case "start":
		phrase = "ПЕРЕРАСПРЕДЕЛИТЬ"
	case "stop":
		phrase = "ОСТАНОВИТЬ"
	case "decommission":
		permission = "admin:Decommission"
		phrase = fmt.Sprintf("ПУЛ %d", b.Pool)
	case "cancel":
		permission = "admin:Decommission"
		phrase = fmt.Sprintf("ОТМЕНИТЬ ПУЛ %d", b.Pool)
	default:
		return apperror.New(400, "InvalidPoolOperation", messages.ServerInvalidPoolOperation)
	}
	if b.Confirmation != phrase {
		return apperror.New(400, "ConfirmationRequired", messages.ServerConfirmationRequired)
	}
	if e := RequireServerAction(ctx, s, permission); e != nil {
		return e
	}
	v, raw, e := ReadPools(ctx, s)
	if e != nil {
		return e
	}
	if !v.Supported {
		return apperror.New(501, "ServerFeatureUnavailable", v.Reason)
	}
	if b.Revision != v.Revision {
		return apperror.New(409, "PoolsChanged", messages.ServerPoolsChanged)
	}
	var target *madmin.PoolStatus
	var current PoolOperationRow
	for i, p := range raw {
		if p.ID+1 == b.Pool {
			target = &raw[i]
		}
	}
	for _, p := range v.Pools {
		if p.ID == b.Pool {
			current = p
		}
	}
	if b.Operation == "decommission" || b.Operation == "cancel" {
		if target == nil {
			return apperror.New(404, "PoolNotFound", messages.ServerPoolNotFound)
		}
	}
	if b.Operation == "start" || b.Operation == "decommission" {
		if len(v.Pools) < 2 || !v.RebalanceKnown || v.Rebalancing {
			return apperror.New(409, "PoolsNotReady", messages.ServerPoolsNotReady)
		}
		for _, p := range v.Pools {
			if Draining(p) || p.State == "complete" {
				return apperror.New(409, "PoolBusy", messages.ServerPoolBusy)
			}
		}
		info, infoErr := s.ServerInfo(ctx, madmin.WithDriveMetrics(true))
		if infoErr != nil {
			return ServerOperationError(infoErr)
		}
		if e = PoolHealth(info, b.Pool); e != nil {
			return e
		}
		if b.Operation == "decommission" {
			if current.State != "active" && current.State != "canceled" && current.State != "failed" {
				return apperror.New(409, "PoolBusy", messages.ServerPoolBusyPoolChange)
			}
			// MinIO validates available destination space and erasure-set placement.
			// Do not turn physical filesystem totals into a fictitious usable capacity.
			e = s.DecommissionPool(ctx, target.CmdLine)
		} else {
			_, e = s.RebalanceStart(ctx)
		}
	} else if b.Operation == "stop" {
		if !v.RebalanceKnown || !v.Rebalancing {
			return apperror.New(409, "RebalanceInactive", messages.ServerRebalanceInactive)
		}
		e = s.RebalanceStop(ctx)
	} else {
		if !Draining(current) {
			return apperror.New(409, "DecommissionInactive", messages.ServerDecommissionInactive)
		}
		e = s.CancelDecommissionPool(ctx, target.CmdLine)
	}
	if e != nil {
		return ServerOperationError(e)
	}
	return nil
}
