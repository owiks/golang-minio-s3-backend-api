package buckets

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/s3utils"

	"minio-web/api/src/services/apperror"
	identitiessvc "minio-web/api/src/services/identities"
	keyssvc "minio-web/api/src/services/keys"
	"minio-web/api/src/services/telemetry"
)

func ReadBucketAccess(ctx context.Context, s ReadBucketAccessClient, bucket string) (BucketAccessReport, error) {
	if s3utils.CheckValidBucketNameStrict(bucket) != nil {
		return BucketAccessReport{}, apperror.New(400, "InvalidBucketName", "Укажите корректное имя бакета.")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	policies, err := s.ListCannedPolicies(ctx)
	if err != nil {
		return BucketAccessReport{}, err
	}
	report := BucketAccessReport{Bucket: bucket, CheckedAt: time.Now().UTC(), Complete: true, Warnings: []string{}, Rows: []BucketAccessRow{}}
	warn := func(message string) { report.Complete = false; report.Warnings = append(report.Warnings, message) }
	relevant := map[string][]BucketGrant{}
	names := []string{}
	for name, raw := range policies {
		grants, err := BucketGrants(raw, bucket)
		if err != nil {
			warn("Не удалось разобрать одну из IAM-политик.")
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
		warn("Показаны назначения первых 1000 связанных политик.")
	}
	userPolicies, groupPolicies := map[string][]string{}, map[string][]string{}
	for offset := 0; offset < len(names); offset += 32 {
		entities, err := s.GetPolicyEntities(ctx, madmin.PolicyEntitiesQuery{Policy: names[offset:min(offset+32, len(names))]})
		if err != nil {
			warn("Назначения IAM-политик прочитаны не полностью. " + apperror.Public(err).Message)
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
	grantsFor := func(names []string, source string) []BucketGrant {
		result := []BucketGrant{}
		for _, name := range identitiessvc.SortedUnique(names) {
			for _, grant := range relevant[name] {
				grant.Policy = name
				grant.Source = source
				result = append(result, grant)
			}
		}
		return result
	}
	users, userErr := s.ListUsers(ctx)
	if userErr != nil {
		warn("Пользователи прочитаны не полностью. " + apperror.Public(userErr).Message)
	}
	groups, groupErr := s.ListGroups(ctx)
	if groupErr != nil {
		warn("Группы прочитаны не полностью. " + apperror.Public(groupErr).Message)
	}
	groupDetails := map[string]madmin.GroupDesc{}
	sort.Strings(groups)
	if len(groups) > 500 {
		groups = groups[:500]
		warn("Проверены первые 500 групп.")
	}
	for _, name := range groups {
		if ctx.Err() != nil {
			warn("Время проверки групп истекло.")
			break
		}
		group, err := s.GetGroupDescription(ctx, name)
		if err != nil {
			warn("Участники и состояние одной из групп недоступны.")
			continue
		}
		groupDetails[name] = *group
	}
	disabledGroups := map[string][]string{}
	userGrants := map[string][]BucketGrant{}
	for name := range users {
		userGrants[name] = grantsFor(userPolicies[name], "Напрямую")
	}
	for name, details := range groupDetails {
		grants := grantsFor(groupPolicies[name], "Группа "+name)
		if len(grants) > 0 {
			report.Rows = append(report.Rows, BucketAccessRow{Kind: "group", Name: name, Status: details.Status, Grants: grants, Notes: []string{}})
		}
		for _, member := range details.Members {
			if details.Status != "enabled" {
				disabledGroups[member] = append(disabledGroups[member], name)
			}
			// Preserve disabled membership as a fact; do not simulate the native
			// authorization effects of disabling a group.
			inherited := grantsFor(groupPolicies[name], "Группа "+name)
			if details.Status != "enabled" {
				for i := range inherited {
					inherited[i].Source += " (отключена)"
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
		notes := []string{}
		if len(disabledGroups[name]) > 0 {
			notes = append(notes, "Состоит в отключённых группах: "+strings.Join(identitiessvc.SortedUnique(disabledGroups[name]), ", "))
		}
		report.Rows = append(report.Rows, BucketAccessRow{Kind: "user", Name: name, Status: status, Grants: grants, Notes: notes})
	}
	// Bulk enumeration is a native, independently authorized read. No owner IDs
	// are guessed and failed key inspection never falls back to parent access.
	accounts, keyErr := s.ListAccessKeysBulk(ctx, nil, madmin.ListAccessKeysOpts{ListType: madmin.AccessKeyListSvcaccOnly, All: true})
	if keyErr != nil {
		warn("Ключи приложений недоступны. " + apperror.Public(keyErr).Message)
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
				row := BucketAccessRow{Kind: "access-key", Name: key.AccessKey, Parent: owner, Status: "unknown", Grants: append([]BucketGrant{}, userGrants[owner]...), Notes: []string{}}
				for i := range row.Grants {
					row.Grants[i].Source = "Владелец " + owner + ": " + row.Grants[i].Source
				}
				info, err := s.InfoServiceAccount(ctx, key.AccessKey)
				if err != nil {
					row.RestrictionState = "unavailable"
					row.Notes = append(row.Notes, "Ограничения ключа не прочитаны; доступ не подтверждён.")
					warn("Не все ограничения ключей удалось прочитать.")
				} else {
					row.Status = info.AccountStatus
					if info.ParentUser != owner {
						row.Grants = []BucketGrant{}
						row.RestrictionState = "unavailable"
						row.Notes = append(row.Notes, "Владелец ключа изменился; обновите сведения.")
						warn("Владелец одного из ключей изменился во время проверки.")
					} else if info.ImpliedPolicy {
						row.RestrictionState = "inherited"
					} else {
						row.RestrictionState = "restricted"
						row.Restrictions, err = BucketGrants(json.RawMessage(info.Policy), bucket)
						if err != nil {
							row.RestrictionState = "unavailable"
							warn("Ограничивающая политика одного из ключей не прочитана.")
						}
					}
					if expiry := keyssvc.KeyExpiration(info.Expiration); expiry != nil && expiry.Before(time.Now()) {
						row.Status = "expired"
					}
				}
				if len(disabledGroups[owner]) > 0 {
					row.Notes = append(row.Notes, "Владелец состоит в отключённых группах: "+strings.Join(identitiessvc.SortedUnique(disabledGroups[owner]), ", "))
				}
				if user, ok := users[owner]; ok && user.Status != madmin.AccountEnabled {
					row.Notes = append(row.Notes, "Пользователь-владелец отключён.")
				}
				if _, ok := users[owner]; !ok {
					row.Notes = append(row.Notes, "Права владельца не представлены в списке встроенных пользователей; итог не рассчитан.")
				}
				if len(row.Grants) > 0 || len(row.Restrictions) > 0 || row.RestrictionState == "unavailable" || users[owner].Status == "" {
					report.Rows = append(report.Rows, row)
				}
			}
		}
		if count >= 500 {
			warn("Проверены первые 500 ключей приложений.")
		}
	}
	// A bucket policy can add grants or Deny independently of IAM. Include its
	// statements as a separate source, without evaluating Principal or conditions.
	raw, bucketErr := s.GetBucketPolicy(ctx, bucket)
	if bucketErr != nil {
		warn("Политика самого бакета не прочитана. " + apperror.Public(bucketErr).Message)
	} else if raw != "" {
		grants, err := BucketGrants(json.RawMessage(raw), bucket)
		if err != nil {
			warn("Политика самого бакета имеет неподдерживаемый формат.")
		} else if len(grants) > 0 {
			// Principals are shown below in a safe, explicit summary; no policy bodies
			// or condition values (which may be sensitive) are returned.
			for i := range grants {
				grants[i].Policy = "Политика бакета"
				grants[i].Source = "Principal проверяется MinIO"
				grants[i].Uncertain = true
			}
			report.Rows = append(report.Rows, BucketAccessRow{Kind: "bucket-policy", Name: bucket, Status: "configured", Grants: grants, Notes: []string{"Эти правила могут разрешать анонимный доступ или запрещать действия. Получатели заданы в Principal; документ доступен в разделе «Политика бакета»."}})
		}
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		if report.Rows[i].Kind != report.Rows[j].Kind {
			return report.Rows[i].Kind < report.Rows[j].Kind
		}
		return report.Rows[i].Name < report.Rows[j].Name
	})
	report.Warnings = identitiessvc.SortedUnique(report.Warnings)
	if !report.Complete {
		telemetry.Outcome(ctx, "partial", "AccessReportPartial")
	}
	return report, nil
}
