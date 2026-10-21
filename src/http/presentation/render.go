// Package presentation renders known API models explicitly, without reflecting
// over JSON or translating arbitrary strings from storage and user documents.
package presentation

import (
	"context"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/requestctx"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/access"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/files"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/iam"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/identities"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/search"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/server"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/sessions"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/transfers"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads"
)

func Render(ctx context.Context, value any) any { return present(requestctx.Localizer(ctx), value) }
func present(localizer *localization.Localizer, value any) any {
	switch v := value.(type) {
	case messages.Ref:
		return localizer.Render(v)
	case map[string]any:
		return mapValues(v, func(x any) any { return present(localizer, x) })
	case []any:
		return mapSlice(v, func(x any) any { return present(localizer, x) })
	case map[string]messages.Ref:
		return mapValues(v, func(x messages.Ref) string { return localizer.Render(x) })
	case []messages.Ref:
		return mapSlice(v, func(x messages.Ref) string { return localizer.Render(x) })
	case access.AccessSnapshot:
		return presentAccessAccessSnapshot(localizer, v)
	case *access.AccessSnapshot:
		if v == nil {
			return nil
		}
		return presentAccessAccessSnapshot(localizer, *v)
	case []access.AccessSnapshot:
		return mapSlice(v, func(x access.AccessSnapshot) AccessSnapshot {
			return presentAccessAccessSnapshot(localizer, x)
		})
	case map[string]access.AccessSnapshot:
		return mapValues(v, func(x access.AccessSnapshot) AccessSnapshot {
			return presentAccessAccessSnapshot(localizer, x)
		})
	case access.ActionAccess:
		return presentAccessActionAccess(localizer, v)
	case *access.ActionAccess:
		if v == nil {
			return nil
		}
		return presentAccessActionAccess(localizer, *v)
	case []access.ActionAccess:
		return mapSlice(v, func(x access.ActionAccess) ActionAccess { return presentAccessActionAccess(localizer, x) })
	case map[string]access.ActionAccess:
		return mapValues(v, func(x access.ActionAccess) ActionAccess { return presentAccessActionAccess(localizer, x) })
	case access.NavigationAccess:
		return presentAccessNavigationAccess(localizer, v)
	case *access.NavigationAccess:
		if v == nil {
			return nil
		}
		return presentAccessNavigationAccess(localizer, *v)
	case []access.NavigationAccess:
		return mapSlice(v, func(x access.NavigationAccess) NavigationAccess {
			return presentAccessNavigationAccess(localizer, x)
		})
	case map[string]access.NavigationAccess:
		return mapValues(v, func(x access.NavigationAccess) NavigationAccess {
			return presentAccessNavigationAccess(localizer, x)
		})
	case apperror.Error:
		return presentApperrorError(localizer, v)
	case *apperror.Error:
		if v == nil {
			return nil
		}
		return presentApperrorError(localizer, *v)
	case []apperror.Error:
		return mapSlice(v, func(x apperror.Error) Error { return presentApperrorError(localizer, x) })
	case map[string]apperror.Error:
		return mapValues(v, func(x apperror.Error) Error { return presentApperrorError(localizer, x) })
	case archives.Manifest:
		return presentArchivesManifest(localizer, v)
	case *archives.Manifest:
		if v == nil {
			return nil
		}
		return presentArchivesManifest(localizer, *v)
	case []archives.Manifest:
		return mapSlice(v, func(x archives.Manifest) Manifest { return presentArchivesManifest(localizer, x) })
	case map[string]archives.Manifest:
		return mapValues(v, func(x archives.Manifest) Manifest { return presentArchivesManifest(localizer, x) })
	case archives.ZIPContents:
		return presentArchivesZIPContents(localizer, v)
	case *archives.ZIPContents:
		if v == nil {
			return nil
		}
		return presentArchivesZIPContents(localizer, *v)
	case []archives.ZIPContents:
		return mapSlice(v, func(x archives.ZIPContents) ZIPContents { return presentArchivesZIPContents(localizer, x) })
	case map[string]archives.ZIPContents:
		return mapValues(v, func(x archives.ZIPContents) ZIPContents { return presentArchivesZIPContents(localizer, x) })
	case archives.ZipItem:
		return presentArchivesZipItem(localizer, v)
	case *archives.ZipItem:
		if v == nil {
			return nil
		}
		return presentArchivesZipItem(localizer, *v)
	case []archives.ZipItem:
		return mapSlice(v, func(x archives.ZipItem) ZipItem { return presentArchivesZipItem(localizer, x) })
	case map[string]archives.ZipItem:
		return mapValues(v, func(x archives.ZipItem) ZipItem { return presentArchivesZipItem(localizer, x) })
	case archiveplan.ArchiveItem:
		return presentArchiveplanArchiveItem(localizer, v)
	case *archiveplan.ArchiveItem:
		if v == nil {
			return nil
		}
		return presentArchiveplanArchiveItem(localizer, *v)
	case []archiveplan.ArchiveItem:
		return mapSlice(v, func(x archiveplan.ArchiveItem) ArchiveItem {
			return presentArchiveplanArchiveItem(localizer, x)
		})
	case map[string]archiveplan.ArchiveItem:
		return mapValues(v, func(x archiveplan.ArchiveItem) ArchiveItem {
			return presentArchiveplanArchiveItem(localizer, x)
		})
	case archiveplan.ArchiveSnapshot:
		return presentArchiveplanArchiveSnapshot(localizer, v)
	case *archiveplan.ArchiveSnapshot:
		if v == nil {
			return nil
		}
		return presentArchiveplanArchiveSnapshot(localizer, *v)
	case []archiveplan.ArchiveSnapshot:
		return mapSlice(v, func(x archiveplan.ArchiveSnapshot) ArchiveSnapshot {
			return presentArchiveplanArchiveSnapshot(localizer, x)
		})
	case map[string]archiveplan.ArchiveSnapshot:
		return mapValues(v, func(x archiveplan.ArchiveSnapshot) ArchiveSnapshot {
			return presentArchiveplanArchiveSnapshot(localizer, x)
		})
	case buckets.BrowserSetting:
		return presentBucketsBrowserSetting(localizer, v)
	case *buckets.BrowserSetting:
		if v == nil {
			return nil
		}
		return presentBucketsBrowserSetting(localizer, *v)
	case []buckets.BrowserSetting:
		return mapSlice(v, func(x buckets.BrowserSetting) BrowserSetting {
			return presentBucketsBrowserSetting(localizer, x)
		})
	case map[string]buckets.BrowserSetting:
		return mapValues(v, func(x buckets.BrowserSetting) BrowserSetting {
			return presentBucketsBrowserSetting(localizer, x)
		})
	case buckets.BucketAccessReport:
		return presentBucketsBucketAccessReport(localizer, v)
	case *buckets.BucketAccessReport:
		if v == nil {
			return nil
		}
		return presentBucketsBucketAccessReport(localizer, *v)
	case []buckets.BucketAccessReport:
		return mapSlice(v, func(x buckets.BucketAccessReport) BucketAccessReport {
			return presentBucketsBucketAccessReport(localizer, x)
		})
	case map[string]buckets.BucketAccessReport:
		return mapValues(v, func(x buckets.BucketAccessReport) BucketAccessReport {
			return presentBucketsBucketAccessReport(localizer, x)
		})
	case buckets.BucketAccessRow:
		return presentBucketsBucketAccessRow(localizer, v)
	case *buckets.BucketAccessRow:
		if v == nil {
			return nil
		}
		return presentBucketsBucketAccessRow(localizer, *v)
	case []buckets.BucketAccessRow:
		return mapSlice(v, func(x buckets.BucketAccessRow) BucketAccessRow {
			return presentBucketsBucketAccessRow(localizer, x)
		})
	case map[string]buckets.BucketAccessRow:
		return mapValues(v, func(x buckets.BucketAccessRow) BucketAccessRow {
			return presentBucketsBucketAccessRow(localizer, x)
		})
	case buckets.BucketDetails:
		return presentBucketsBucketDetails(localizer, v)
	case *buckets.BucketDetails:
		if v == nil {
			return nil
		}
		return presentBucketsBucketDetails(localizer, *v)
	case []buckets.BucketDetails:
		return mapSlice(v, func(x buckets.BucketDetails) BucketDetails {
			return presentBucketsBucketDetails(localizer, x)
		})
	case map[string]buckets.BucketDetails:
		return mapValues(v, func(x buckets.BucketDetails) BucketDetails {
			return presentBucketsBucketDetails(localizer, x)
		})
	case buckets.BucketGrant:
		return presentBucketsBucketGrant(localizer, v)
	case *buckets.BucketGrant:
		if v == nil {
			return nil
		}
		return presentBucketsBucketGrant(localizer, *v)
	case []buckets.BucketGrant:
		return mapSlice(v, func(x buckets.BucketGrant) BucketGrant { return presentBucketsBucketGrant(localizer, x) })
	case map[string]buckets.BucketGrant:
		return mapValues(v, func(x buckets.BucketGrant) BucketGrant { return presentBucketsBucketGrant(localizer, x) })
	case buckets.DetailField:
		return presentBucketsDetailField(localizer, v)
	case *buckets.DetailField:
		if v == nil {
			return nil
		}
		return presentBucketsDetailField(localizer, *v)
	case []buckets.DetailField:
		return mapSlice(v, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) })
	case map[string]buckets.DetailField:
		return mapValues(v, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) })
	case buckets.LifecycleRow:
		return presentBucketsLifecycleRow(localizer, v)
	case *buckets.LifecycleRow:
		if v == nil {
			return nil
		}
		return presentBucketsLifecycleRow(localizer, *v)
	case []buckets.LifecycleRow:
		return mapSlice(v, func(x buckets.LifecycleRow) LifecycleRow { return presentBucketsLifecycleRow(localizer, x) })
	case map[string]buckets.LifecycleRow:
		return mapValues(v, func(x buckets.LifecycleRow) LifecycleRow { return presentBucketsLifecycleRow(localizer, x) })
	case buckets.LifecycleSnapshot:
		return presentBucketsLifecycleSnapshot(localizer, v)
	case *buckets.LifecycleSnapshot:
		if v == nil {
			return nil
		}
		return presentBucketsLifecycleSnapshot(localizer, *v)
	case []buckets.LifecycleSnapshot:
		return mapSlice(v, func(x buckets.LifecycleSnapshot) LifecycleSnapshot {
			return presentBucketsLifecycleSnapshot(localizer, x)
		})
	case map[string]buckets.LifecycleSnapshot:
		return mapValues(v, func(x buckets.LifecycleSnapshot) LifecycleSnapshot {
			return presentBucketsLifecycleSnapshot(localizer, x)
		})
	case files.ContentSearchResult:
		return presentFilesContentSearchResult(localizer, v)
	case *files.ContentSearchResult:
		if v == nil {
			return nil
		}
		return presentFilesContentSearchResult(localizer, *v)
	case []files.ContentSearchResult:
		return mapSlice(v, func(x files.ContentSearchResult) ContentSearchResult {
			return presentFilesContentSearchResult(localizer, x)
		})
	case map[string]files.ContentSearchResult:
		return mapValues(v, func(x files.ContentSearchResult) ContentSearchResult {
			return presentFilesContentSearchResult(localizer, x)
		})
	case files.VersionProtection:
		return presentFilesVersionProtection(localizer, v)
	case *files.VersionProtection:
		if v == nil {
			return nil
		}
		return presentFilesVersionProtection(localizer, *v)
	case []files.VersionProtection:
		return mapSlice(v, func(x files.VersionProtection) VersionProtection {
			return presentFilesVersionProtection(localizer, x)
		})
	case map[string]files.VersionProtection:
		return mapValues(v, func(x files.VersionProtection) VersionProtection {
			return presentFilesVersionProtection(localizer, x)
		})
	case iam.ActionCheckIssue:
		return presentIamActionCheckIssue(localizer, v)
	case *iam.ActionCheckIssue:
		if v == nil {
			return nil
		}
		return presentIamActionCheckIssue(localizer, *v)
	case []iam.ActionCheckIssue:
		return mapSlice(v, func(x iam.ActionCheckIssue) ActionCheckIssue { return presentIamActionCheckIssue(localizer, x) })
	case map[string]iam.ActionCheckIssue:
		return mapValues(v, func(x iam.ActionCheckIssue) ActionCheckIssue { return presentIamActionCheckIssue(localizer, x) })
	case identities.GroupSummary:
		return presentIdentitiesGroupSummary(localizer, v)
	case *identities.GroupSummary:
		if v == nil {
			return nil
		}
		return presentIdentitiesGroupSummary(localizer, *v)
	case []identities.GroupSummary:
		return mapSlice(v, func(x identities.GroupSummary) GroupSummary {
			return presentIdentitiesGroupSummary(localizer, x)
		})
	case map[string]identities.GroupSummary:
		return mapValues(v, func(x identities.GroupSummary) GroupSummary {
			return presentIdentitiesGroupSummary(localizer, x)
		})
	case identities.PolicySummary:
		return presentIdentitiesPolicySummary(localizer, v)
	case *identities.PolicySummary:
		if v == nil {
			return nil
		}
		return presentIdentitiesPolicySummary(localizer, *v)
	case []identities.PolicySummary:
		return mapSlice(v, func(x identities.PolicySummary) PolicySummary {
			return presentIdentitiesPolicySummary(localizer, x)
		})
	case map[string]identities.PolicySummary:
		return mapValues(v, func(x identities.PolicySummary) PolicySummary {
			return presentIdentitiesPolicySummary(localizer, x)
		})
	case objects.ChecksumInfo:
		return presentObjectsChecksumInfo(localizer, v)
	case *objects.ChecksumInfo:
		if v == nil {
			return nil
		}
		return presentObjectsChecksumInfo(localizer, *v)
	case []objects.ChecksumInfo:
		return mapSlice(v, func(x objects.ChecksumInfo) ChecksumInfo { return presentObjectsChecksumInfo(localizer, x) })
	case map[string]objects.ChecksumInfo:
		return mapValues(v, func(x objects.ChecksumInfo) ChecksumInfo { return presentObjectsChecksumInfo(localizer, x) })
	case objects.Content:
		return presentObjectsContent(localizer, v)
	case *objects.Content:
		if v == nil {
			return nil
		}
		return presentObjectsContent(localizer, *v)
	case []objects.Content:
		return mapSlice(v, func(x objects.Content) Content { return presentObjectsContent(localizer, x) })
	case map[string]objects.Content:
		return mapValues(v, func(x objects.Content) Content { return presentObjectsContent(localizer, x) })
	case objects.ObjectProperties:
		return presentObjectsObjectProperties(localizer, v)
	case *objects.ObjectProperties:
		if v == nil {
			return nil
		}
		return presentObjectsObjectProperties(localizer, *v)
	case []objects.ObjectProperties:
		return mapSlice(v, func(x objects.ObjectProperties) ObjectProperties {
			return presentObjectsObjectProperties(localizer, x)
		})
	case map[string]objects.ObjectProperties:
		return mapValues(v, func(x objects.ObjectProperties) ObjectProperties {
			return presentObjectsObjectProperties(localizer, x)
		})
	case search.ContentIssue:
		return presentSearchContentIssue(localizer, v)
	case *search.ContentIssue:
		if v == nil {
			return nil
		}
		return presentSearchContentIssue(localizer, *v)
	case []search.ContentIssue:
		return mapSlice(v, func(x search.ContentIssue) ContentIssue { return presentSearchContentIssue(localizer, x) })
	case map[string]search.ContentIssue:
		return mapValues(v, func(x search.ContentIssue) ContentIssue { return presentSearchContentIssue(localizer, x) })
	case server.ConfigField:
		return presentServerConfigField(localizer, v)
	case *server.ConfigField:
		if v == nil {
			return nil
		}
		return presentServerConfigField(localizer, *v)
	case []server.ConfigField:
		return mapSlice(v, func(x server.ConfigField) ConfigField { return presentServerConfigField(localizer, x) })
	case map[string]server.ConfigField:
		return mapValues(v, func(x server.ConfigField) ConfigField { return presentServerConfigField(localizer, x) })
	case server.PoolsView:
		return presentServerPoolsView(localizer, v)
	case *server.PoolsView:
		if v == nil {
			return nil
		}
		return presentServerPoolsView(localizer, *v)
	case []server.PoolsView:
		return mapSlice(v, func(x server.PoolsView) PoolsView { return presentServerPoolsView(localizer, x) })
	case map[string]server.PoolsView:
		return mapValues(v, func(x server.PoolsView) PoolsView { return presentServerPoolsView(localizer, x) })
	case server.ServerConfigView:
		return presentServerServerConfigView(localizer, v)
	case *server.ServerConfigView:
		if v == nil {
			return nil
		}
		return presentServerServerConfigView(localizer, *v)
	case []server.ServerConfigView:
		return mapSlice(v, func(x server.ServerConfigView) ServerConfigView {
			return presentServerServerConfigView(localizer, x)
		})
	case map[string]server.ServerConfigView:
		return mapValues(v, func(x server.ServerConfigView) ServerConfigView {
			return presentServerServerConfigView(localizer, x)
		})
	case server.ServerSnapshot:
		return presentServerServerSnapshot(localizer, v)
	case *server.ServerSnapshot:
		if v == nil {
			return nil
		}
		return presentServerServerSnapshot(localizer, *v)
	case []server.ServerSnapshot:
		return mapSlice(v, func(x server.ServerSnapshot) ServerSnapshot {
			return presentServerServerSnapshot(localizer, x)
		})
	case map[string]server.ServerSnapshot:
		return mapValues(v, func(x server.ServerSnapshot) ServerSnapshot {
			return presentServerServerSnapshot(localizer, x)
		})
	case sessions.LoginResponse:
		return presentSessionsLoginResponse(localizer, v)
	case *sessions.LoginResponse:
		if v == nil {
			return nil
		}
		return presentSessionsLoginResponse(localizer, *v)
	case []sessions.LoginResponse:
		return mapSlice(v, func(x sessions.LoginResponse) LoginResponse {
			return presentSessionsLoginResponse(localizer, x)
		})
	case map[string]sessions.LoginResponse:
		return mapValues(v, func(x sessions.LoginResponse) LoginResponse {
			return presentSessionsLoginResponse(localizer, x)
		})
	case transfers.TransferResult:
		return presentTransfersTransferResult(localizer, v)
	case *transfers.TransferResult:
		if v == nil {
			return nil
		}
		return presentTransfersTransferResult(localizer, *v)
	case []transfers.TransferResult:
		return mapSlice(v, func(x transfers.TransferResult) TransferResult {
			return presentTransfersTransferResult(localizer, x)
		})
	case map[string]transfers.TransferResult:
		return mapValues(v, func(x transfers.TransferResult) TransferResult {
			return presentTransfersTransferResult(localizer, x)
		})
	case uploads.AbortUploadResult:
		return presentUploadsAbortUploadResult(localizer, v)
	case *uploads.AbortUploadResult:
		if v == nil {
			return nil
		}
		return presentUploadsAbortUploadResult(localizer, *v)
	case []uploads.AbortUploadResult:
		return mapSlice(v, func(x uploads.AbortUploadResult) AbortUploadResult {
			return presentUploadsAbortUploadResult(localizer, x)
		})
	case map[string]uploads.AbortUploadResult:
		return mapValues(v, func(x uploads.AbortUploadResult) AbortUploadResult {
			return presentUploadsAbortUploadResult(localizer, x)
		})
	case uploads.IncompleteUploadRow:
		return presentUploadsIncompleteUploadRow(localizer, v)
	case *uploads.IncompleteUploadRow:
		if v == nil {
			return nil
		}
		return presentUploadsIncompleteUploadRow(localizer, *v)
	case []uploads.IncompleteUploadRow:
		return mapSlice(v, func(x uploads.IncompleteUploadRow) IncompleteUploadRow {
			return presentUploadsIncompleteUploadRow(localizer, x)
		})
	case map[string]uploads.IncompleteUploadRow:
		return mapValues(v, func(x uploads.IncompleteUploadRow) IncompleteUploadRow {
			return presentUploadsIncompleteUploadRow(localizer, x)
		})
	case uploads.IncompleteUploadsPage:
		return presentUploadsIncompleteUploadsPage(localizer, v)
	case *uploads.IncompleteUploadsPage:
		if v == nil {
			return nil
		}
		return presentUploadsIncompleteUploadsPage(localizer, *v)
	case []uploads.IncompleteUploadsPage:
		return mapSlice(v, func(x uploads.IncompleteUploadsPage) IncompleteUploadsPage {
			return presentUploadsIncompleteUploadsPage(localizer, x)
		})
	case map[string]uploads.IncompleteUploadsPage:
		return mapValues(v, func(x uploads.IncompleteUploadsPage) IncompleteUploadsPage {
			return presentUploadsIncompleteUploadsPage(localizer, x)
		})
	default:
		return value
	}
}

func mapSlice[A, B any](values []A, render func(A) B) []B {
	if values == nil {
		return nil
	}
	out := make([]B, len(values))
	for i, v := range values {
		out[i] = render(v)
	}
	return out
}
func mapValues[K comparable, A, B any](values map[K]A, render func(A) B) map[K]B {
	if values == nil {
		return nil
	}
	out := make(map[K]B, len(values))
	for k, v := range values {
		out[k] = render(v)
	}
	return out
}
func mapPointer[A, B any](value *A, render func(A) B) *B {
	if value == nil {
		return nil
	}
	out := render(*value)
	return &out
}
