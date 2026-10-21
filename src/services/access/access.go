package access

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/madmin-go/v3"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/messages"
	objectssvc "minio-web/api/src/services/objects"
	serversvc "minio-web/api/src/services/server"
)

type NavigationAccess struct {
	Sections          []string              `json:"sections"`
	Unknown           []string              `json:"unknown"`
	CheckedAt         time.Time             `json:"checkedAt"`
	DiagnosticSources []string              `json:"diagnosticSources"`
	MultipartBuckets  []string              `json:"multipartBuckets"`
	BucketUsage       bool                  `json:"bucketUsage"`
	ActionCheck       *iam.ActionCheckIssue `json:"actionCheck,omitempty"`
}

// Fixed worker count also bounds memory when an account has many buckets.
func ParallelAccessReads(ctx context.Context, count int, read func(int)) {
	var next atomic.Uint64
	var jobs sync.WaitGroup
	for range min(4, count) {
		jobs.Go(func() {
			for {
				if ctx.Err() != nil {
					return
				}
				i := int(next.Add(1)) - 1
				if i >= count {
					return
				}
				read(i)
			}
		})
	}
	jobs.Wait()
}

func AccessibleBuckets(ctx context.Context, s AccessibleBucketsClient) ([]objectssvc.BucketRow, error) {
	// AccountInfo checks effective bucket access in MinIO, including derived keys.
	// Its bucket inventory is cached, so reconcile it with a fresh S3 inventory
	// when allowed. Every additional name needs a successful read probe.
	account, accountErr := s.AccountInfo(ctx, madmin.AccountOpts{})
	known := map[string]objectssvc.BucketRow{}
	if accountErr == nil {
		for _, b := range account.Buckets {
			if b.Access.Read || b.Access.Write {
				known[b.Name] = objectssvc.BucketRow{Name: b.Name, Created: b.Created}
			}
		}
	}
	inventory, listErr := s.ListBuckets(ctx)
	if listErr != nil {
		if accountErr != nil {
			return nil, listErr
		}
		rows := make([]objectssvc.BucketRow, 0, len(known))
		for _, b := range known {
			rows = append(rows, b)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		return rows, nil
	}
	confirmed := make([]bool, len(inventory))
	failures := make([]error, len(inventory))
	ParallelAccessReads(ctx, len(inventory), func(i int) {
		b := inventory[i]
		if _, ok := known[b.Name]; ok {
			confirmed[i] = true
			return
		}
		_, e := s.ListObjectPage(ctx, b.Name, "", "", "/", 1)
		if e == nil {
			confirmed[i] = true
		} else if apperror.Public(e).Code != "AccessDenied" && apperror.Public(e).Status != 404 {
			failures[i] = e
		}
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	rows := make([]objectssvc.BucketRow, 0, len(inventory))
	for i, b := range inventory {
		if failures[i] != nil {
			return nil, failures[i]
		}
		if confirmed[i] {
			rows = append(rows, objectssvc.BucketRow{Name: b.Name, Created: b.CreationDate})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

type AccessProbe struct {
	Key  string                      `json:"-"`
	Read func(context.Context) error `json:"-"`
}

func DiscoverAccess(ctx context.Context, s DiscoverAccessClient) ([]objectssvc.BucketRow, NavigationAccess, error) {
	// Queueing is part of discovery's budget, not an unbounded prelude to it.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	release, err := s.LockAccess(ctx)
	if err != nil {
		return nil, NavigationAccess{}, err
	}
	defer release()
	if ctx.Err() != nil {
		return nil, NavigationAccess{}, ctx.Err()
	}
	// Start this check before bucket/server/diagnostic reads can consume the
	// shared deadline. Wait on every return and cancel it if discovery fails.
	var actionJob sync.WaitGroup
	var actionIssue *iam.ActionCheckIssue
	actionJob.Go(func() { actionIssue = s.RefreshActionPolicy(ctx) })
	defer func() { cancel(); actionJob.Wait() }()
	buckets, bucketErr := AccessibleBuckets(ctx, s)
	if bucketErr != nil {
		code := apperror.Public(bucketErr).Code
		if code == "InvalidAccessKeyId" || code == "SignatureDoesNotMatch" || code == "ExpiredToken" || code == "InvalidToken" {
			return nil, NavigationAccess{}, bucketErr
		}
	}
	multipartBuckets := []string{}
	multipartUncertain := false
	probes := []AccessProbe{
		{Key: "bucket-usage", Read: func(ctx context.Context) error { _, e := s.DataUsageInfo(ctx); return e }},
		{Key: "users", Read: func(ctx context.Context) error { _, e := s.ListUsers(ctx); return e }},
		{Key: "groups", Read: func(ctx context.Context) error { _, e := s.ListGroups(ctx); return e }},
		{Key: "policies", Read: func(ctx context.Context) error { _, e := s.ListCannedPolicies(ctx); return e }},
		{Key: "access-keys", Read: func(ctx context.Context) error { _, e := s.ListServiceAccounts(ctx, ""); return e }},
		{Key: "temporary-access", Read: func(ctx context.Context) error {
			_, e := s.ListAccessKeysBulk(ctx, nil, madmin.ListAccessKeysOpts{ListType: madmin.AccessKeyListSTSOnly})
			return e
		}},
		{Key: "tiers", Read: func(ctx context.Context) error { _, e := s.ListTiers(ctx); return e }},
		{Key: "server", Read: func(ctx context.Context) error { _, e := s.ServerInfo(ctx); return e }},
		{Key: "server-config", Read: func(ctx context.Context) error { _, e := serversvc.ReadServerConfig(ctx, s, "api"); return e }},
		{Key: "server-metrics", Read: func(ctx context.Context) error { _, e := serversvc.ReadLiveMetrics(ctx, s); return e }},
		{Key: "server-pools", Read: func(ctx context.Context) error { _, _, e := serversvc.ReadPools(ctx, s); return e }},
		{Key: "batch", Read: func(ctx context.Context) error {
			_, e := s.ListBatchJobs(ctx, &madmin.ListBatchJobsFilter{})
			return e
		}},
		{Key: "diagnostics-logs", Read: func(ctx context.Context) error { return ProbeDiagnostic(ctx, s, false) }},
		{Key: "diagnostics-trace", Read: func(ctx context.Context) error { return ProbeDiagnostic(ctx, s, true) }},
		{Key: "incomplete-uploads", Read: func(ctx context.Context) error {
			confirmed := make([]bool, len(buckets))
			failures := make([]error, len(buckets))
			ParallelAccessReads(ctx, len(buckets), func(i int) {
				_, e := s.ListMultipartUploads(ctx, buckets[i].Name, "", "", "", "", 1)
				if e == nil {
					confirmed[i] = true
				} else if apperror.Public(e).Code != "AccessDenied" {
					failures[i] = e
				}
			})
			var failure error = ctx.Err()
			for i, b := range buckets {
				if confirmed[i] {
					multipartBuckets = append(multipartBuckets, b.Name)
				}
				if failures[i] != nil {
					failure = failures[i]
				}
			}
			multipartUncertain = failure != nil
			if len(multipartBuckets) > 0 {
				return nil
			}
			if failure != nil {
				return failure
			}
			return apperror.New(403, "AccessDenied", messages.AccessAccessDenied)
		}},
	}
	results := make([]error, len(probes))
	var jobs sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, p := range probes {
		jobs.Go(func() {
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
				results[i] = p.Read(ctx)
			case <-ctx.Done():
				results[i] = ctx.Err()
			}
		})
	}
	jobs.Wait()
	access := NavigationAccess{Sections: []string{}, Unknown: []string{}, CheckedAt: time.Now().UTC(), DiagnosticSources: []string{}, MultipartBuckets: multipartBuckets}
	if bucketErr == nil {
		canList := len(buckets) > 0
		if !canList {
			_, e := s.ListBuckets(ctx)
			canList = e == nil
			if e != nil && apperror.Public(e).Code != "AccessDenied" {
				access.Unknown = append(access.Unknown, "buckets")
			}
		}
		if canList {
			access.Sections = append(access.Sections, "buckets")
		}
	} else if apperror.Public(bucketErr).Code != "AccessDenied" {
		access.Unknown = append(access.Unknown, "buckets")
	}
	if multipartUncertain && len(multipartBuckets) > 0 {
		access.Unknown = append(access.Unknown, "incomplete-uploads")
	}
	diagnosticAllowed := false
	for i, p := range probes {
		if results[i] == nil {
			if p.Key == "bucket-usage" {
				access.BucketUsage = true
			} else if p.Key == "diagnostics-logs" || p.Key == "diagnostics-trace" {
				diagnosticAllowed = true
				if p.Key == "diagnostics-logs" {
					access.DiagnosticSources = append(access.DiagnosticSources, "logs")
				} else {
					access.DiagnosticSources = append(access.DiagnosticSources, "trace")
				}
			} else {
				access.Sections = append(access.Sections, p.Key)
			}
		} else if apperror.Public(results[i]).Code != "AccessDenied" && apperror.Public(results[i]).Status != 501 {
			access.Unknown = append(access.Unknown, p.Key)
		}
	}
	if diagnosticAllowed {
		access.Sections = append(access.Sections, "diagnostics")
	}
	if bucketErr != nil && len(access.Sections) == 0 {
		return nil, access, bucketErr
	}
	if buckets == nil {
		buckets = []objectssvc.BucketRow{}
	}
	actionJob.Wait()
	if actionIssue != nil {
		access.ActionCheck = actionIssue
		access.Unknown = append(access.Unknown, "actions")
	}
	return buckets, access, nil
}

// Probe only the authenticated stream handshake; the adapter never exposes
// a client or collects diagnostic records during navigation discovery.
func ProbeDiagnostic(ctx context.Context, s ProbeDiagnosticClient, trace bool) error {
	return s.ProbeDiagnostic(ctx, trace)
}
