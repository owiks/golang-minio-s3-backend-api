package runtime

import (
	"context"
	"log/slog"

	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/telemetry"
)

// UI hints, never an authorization boundary. Keep policy documents on the API.
// Unknown conditions cannot prove an Allow; a conditional Deny is conservatively
// treated as applicable. Native MinIO remains authoritative for each operation.
type ActionSnapshot struct {
	actor  string
	policy *iam.Policy
	issue  *iam.ActionCheckIssue
}

func (snapshot ActionSnapshot) Known() bool { return snapshot.policy != nil }
func (snapshot ActionSnapshot) Allows(query iam.Query) bool {
	return snapshot.policy.Allows(snapshot.actor, query)
}
func (snapshot ActionSnapshot) Issue() *iam.ActionCheckIssue { return copyActionIssue(snapshot.issue) }

func copyActionIssue(issue *iam.ActionCheckIssue) *iam.ActionCheckIssue {
	if issue == nil {
		return nil
	}
	copy := *issue
	return &copy
}

func (s *Session) Actions() ActionSnapshot {
	s.actionMu.RLock()
	defer s.actionMu.RUnlock()
	return ActionSnapshot{actor: s.actor, policy: s.actionPolicy, issue: copyActionIssue(s.actionIssue)}
}

func (s *Session) RefreshActionPolicy(ctx context.Context) (issue *iam.ActionCheckIssue) {
	var result *iam.Policy
	defer func() {
		s.actionMu.Lock()
		previous := s.actionIssue
		s.actionPolicy = result
		s.actionIssue = copyActionIssue(issue)
		changed := issue != nil && (previous == nil || previous.Code != issue.Code)
		s.actionMu.Unlock()
		// No upstream error text, identity, credentials or policy documents in logs.
		if changed {
			id := ""
			if trace := telemetry.From(ctx); trace != nil {
				id = trace.ID
			}
			slog.WarnContext(ctx, "action permission check unavailable", "request_id", id, "reason", issue.Code, "cause", issue.Cause, "httpStatus", issue.HTTPStatus)
		}
	}()
	account, accountIssue := s.ActionAccountInfo(ctx)
	if accountIssue != nil {
		return accountIssue
	}
	result, issue = iam.ResolveActionPolicy(ctx, s.admin, s.actor, account)
	return issue
}
