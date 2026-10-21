package buckets

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	identitiessvc "github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	keyssvc "github.com/owiks/golang-minio-s3-backend-api/src/services/keys"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/telemetry"
)

func ReadBucketAccess(ctx context.Context, s ReadBucketAccessClient, bucket string) (BucketAccessReport, error) {
	if s3utils.CheckValidBucketNameStrict(bucket) != nil {
		return BucketAccessReport{}, apperror.New(400, "InvalidBucketName", messages.BucketsInvalidBucketName)
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	policies, err := s.ListCannedPolicies(ctx)
	if err != nil {
		return BucketAccessReport{}, err
	}
	report := BucketAccessReport{Bucket: bucket, CheckedAt: time.Now().UTC(), Complete: true, Warnings: []messages.Ref{}, Rows: []BucketAccessRow{}}
	warn := func(message messages.Ref) {
		report.Complete = false
		report.Warnings = append(report.Warnings, message)
	}
	relevant := map[string][]BucketGrant{}
	names := []string{}
	for name, raw := range policies {
		grants, err := BucketGrants(raw, bucket)
		if err != nil {
			warn(messages.BucketsCouldNotParseOneOfTheIAM)
			continue
		}
		if len(grants) > 0 {
			relevant[name] = grants
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 1000 {
		names = names[:1000]
		warn(messages.BucketsShowingAssignmentsForTheFirst1000)
	}
	userPolicies, groupPolicies := map[string][]string{}, map[string][]string{}
	for offset := 0; offset < len(names); offset += 32 {
		entities, err := s.GetPolicyEntities(ctx, madmin.PolicyEntitiesQuery{Policy: names[offset:min(offset+32, len(names))]})
		if err != nil {
			warn(messages.BucketsIAMPolicyAssignmentsWereOnlyPartiallyRead.With(messages.Text("Cause", apperror.Public(err).Message)))
			break
		}
		for _, mapping := range entities.PolicyMappings {
			for _, name := range mapping.Users {
				userPolicies[name] = append(userPolicies[name], mapping.Policy)
			}
			for _, name := range mapping.Groups {
				groupPolicies[name] = append(groupPolicies[name], mapping.Policy)
			}
		}
	}
	grantsFor := func(names []string, source messages.Ref) []BucketGrant {
		result := []BucketGrant{}
		for _, name := range identitiessvc.SortedUnique(names) {
			for _, grant := range relevant[name] {
				grant.Policy = messages.Literal(name)
				grant.Source = source
				result = append(result, grant)
			}
		}
		return result
	}
	users, userErr := s.ListUsers(ctx)
	if userErr != nil {
		warn(messages.BucketsUsersWereOnlyPartiallyRead.With(messages.Text("Cause", apperror.Public(userErr).Message)))
	}
	groups, groupErr := s.ListGroups(ctx)
	if groupErr != nil {
		warn(messages.BucketsGroupsWereOnlyPartiallyRead.With(messages.Text("Cause", apperror.Public(groupErr).Message)))
	}
	groupDetails := map[string]madmin.GroupDesc{}
	sort.Strings(groups)
	if len(groups) > 500 {
		groups = groups[:500]
		warn(messages.BucketsCheckedTheFirst500Groups)
	}
	for _, name := range groups {
		if ctx.Err() != nil {
			warn(messages.BucketsTheGroupCheckTimedOut)
			break
		}
		group, err := s.GetGroupDescription(ctx, name)
		if err != nil {
			warn(messages.BucketsCouldNotRetrieveMembersAndStatusFor)
			continue
		}
		groupDetails[name] = *group
	}
	disabledGroups := map[string][]string{}
	userGrants := map[string][]BucketGrant{}
	for name := range users {
		userGrants[name] = grantsFor(userPolicies[name], messages.BucketsDirect)
	}
	for name, details := range groupDetails {
		grants := grantsFor(groupPolicies[name], messages.BucketsGroup.With(messages.String("Name", name)))
		if len(grants) > 0 {
			report.Rows = append(report.Rows, BucketAccessRow{Kind: "group", Name: name, Status: details.Status, Grants: grants, Notes: []messages.Ref{}})
		}
		for _, member := range details.Members {
			if details.Status != "enabled" {
				disabledGroups[member] = append(disabledGroups[member], name)
			}
			// Preserve disabled membership as a fact; do not simulate the native
			// authorization effects of disabling a group.
			inherited := grantsFor(groupPolicies[name], messages.BucketsGroup.With(messages.String("Name", name)))
			if details.Status != "enabled" {
				for i := range inherited {
					inherited[i].Source = messages.Buckets.With(messages.Text("Source", inherited[i].Source))
				}
			}
			userGrants[member] = append(userGrants[member], inherited...)
		}
	}
	for name, grants := range userGrants {
		if len(grants) == 0 {
			continue
		}
		status := "unknown"
		if user, ok := users[name]; ok {
			status = string(user.Status)
		}
		notes := []messages.Ref{}
		if len(disabledGroups[name]) > 0 {
			notes = append(notes, messages.BucketsMemberOfDisabledGroups.With(messages.String("Groups", strings.Join(identitiessvc.SortedUnique(disabledGroups[name]), ", "))))
		}
		report.Rows = append(report.Rows, BucketAccessRow{Kind: "user", Name: name, Status: status, Grants: grants, Notes: notes})
	}
	// Bulk enumeration is a native, independently authorized read. No owner IDs
	// are guessed and failed key inspection never falls back to parent access.
	accounts, keyErr := s.ListAccessKeysBulk(ctx, nil, madmin.ListAccessKeysOpts{ListType: madmin.AccessKeyListSvcaccOnly, All: true})
	if keyErr != nil {
		warn(messages.BucketsApplicationKeysAreUnavailable.With(messages.Text("Cause", apperror.Public(keyErr).Message)))
	} else {
		count := 0
		owners := []string{}
		for owner := range accounts {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		for _, owner := range owners {
			for _, key := range accounts[owner].ServiceAccounts {
				if count >= 500 {
					break
				}
				count++
				row := BucketAccessRow{Kind: "access-key", Name: key.AccessKey, Parent: owner, Status: "unknown", Grants: append([]BucketGrant{}, userGrants[owner]...), Notes: []messages.Ref{}}
				for i := range row.Grants {
					row.Grants[i].Source = messages.BucketsOwner.With(messages.String("Owner", owner), messages.Text("Source", row.Grants[i].Source))
				}
				info, err := s.InfoServiceAccount(ctx, key.AccessKey)
				if err != nil {
					row.RestrictionState = "unavailable"
					row.Notes = append(row.Notes, messages.BucketsTheKeyRestrictionsCouldNotBeRead)
					warn(messages.BucketsCouldNotReadAllKeyRestrictions)
				} else {
					row.Status = info.AccountStatus
					if info.ParentUser != owner {
						row.Grants = []BucketGrant{}
						row.RestrictionState = "unavailable"
						row.Notes = append(row.Notes, messages.BucketsTheKeyOwnerChangedRefreshTheDetails)
						warn(messages.BucketsAKeyOwnerChangedDuringTheCheck)
					} else if info.ImpliedPolicy {
						row.RestrictionState = "inherited"
					} else {
						row.RestrictionState = "restricted"
						row.Restrictions, err = BucketGrants(json.RawMessage(info.Policy), bucket)
						if err != nil {
							row.RestrictionState = "unavailable"
							warn(messages.BucketsCouldNotReadTheRestrictionPolicyFor)
						}
					}
					if expiry := keyssvc.KeyExpiration(info.Expiration); expiry != nil && expiry.Before(time.Now()) {
						row.Status = "expired"
					}
				}
				if len(disabledGroups[owner]) > 0 {
					row.Notes = append(row.Notes, messages.BucketsTheOwnerBelongsToDisabledGroups.With(messages.String("Groups", strings.Join(identitiessvc.SortedUnique(disabledGroups[owner]), ", "))))
				}
				if user, ok := users[owner]; ok && user.Status != madmin.AccountEnabled {
					row.Notes = append(row.Notes, messages.BucketsTheOwnerUserIsDisabled)
				}
				if _, ok := users[owner]; !ok {
					row.Notes = append(row.Notes, messages.BucketsTheOwnerSPermissionsAreNotListed)
				}
				if len(row.Grants) > 0 || len(row.Restrictions) > 0 || row.RestrictionState == "unavailable" || users[owner].Status == "" {
					report.Rows = append(report.Rows, row)
				}
			}
		}
		if count >= 500 {
			warn(messages.BucketsCheckedTheFirst500ApplicationKeys)
		}
	}
	// A bucket policy can add grants or Deny independently of IAM. Include its
	// statements as a separate source, without evaluating Principal or conditions.
	raw, bucketErr := s.GetBucketPolicy(ctx, bucket)
	if bucketErr != nil {
		warn(messages.BucketsTheBucketPolicyCouldNotBeRead.With(messages.Text("Cause", apperror.Public(bucketErr).Message)))
	} else if raw != "" {
		grants, err := BucketGrants(json.RawMessage(raw), bucket)
		if err != nil {
			warn(messages.BucketsTheBucketPolicyHasAnUnsupportedFormat)
		} else if len(grants) > 0 {
			// Principals are shown below in a safe, explicit summary; no policy bodies
			// or condition values (which may be sensitive) are returned.
			for i := range grants {
				grants[i].Policy = messages.BucketsBucketPolicy
				grants[i].Source = messages.BucketsPrincipalIsEvaluatedByMinIO
				grants[i].Uncertain = true
			}
			report.Rows = append(report.Rows, BucketAccessRow{Kind: "bucket-policy", Name: bucket, Status: "configured", Grants: grants, Notes: []messages.Ref{messages.BucketsTheseRulesMayAllowAnonymousAccessOr}})
		}
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		if report.Rows[i].Kind != report.Rows[j].Kind {
			return report.Rows[i].Kind < report.Rows[j].Kind
		}
		return report.Rows[i].Name < report.Rows[j].Name
	})
	report.Warnings = messages.SortedUnique(report.Warnings)
	if !report.Complete {
		telemetry.Outcome(ctx, "partial", "AccessReportPartial")
	}
	return report, nil
}
