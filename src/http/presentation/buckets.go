package presentation

import (
	"github.com/owiks/golang-minio-s3-backend-api/src/services/buckets"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/localization"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
)

type BrowserSetting struct {
	buckets.BrowserSetting
	Warning string `json:"warning,omitempty"`
}

func presentBucketsBrowserSetting(localizer *localization.Localizer, v buckets.BrowserSetting) BrowserSetting {
	return BrowserSetting{BrowserSetting: v,
		Warning: localizer.Render(v.Warning),
	}
}

type BucketAccessReport struct {
	buckets.BucketAccessReport
	Warnings []string          `json:"warnings"`
	Rows     []BucketAccessRow `json:"rows"`
}

func presentBucketsBucketAccessReport(localizer *localization.Localizer, v buckets.BucketAccessReport) BucketAccessReport {
	return BucketAccessReport{BucketAccessReport: v,
		Warnings: mapSlice(v.Warnings, func(x messages.Ref) string { return localizer.Render(x) }),
		Rows: mapSlice(v.Rows, func(x buckets.BucketAccessRow) BucketAccessRow {
			return presentBucketsBucketAccessRow(localizer, x)
		}),
	}
}

type BucketAccessRow struct {
	buckets.BucketAccessRow
	Grants       []BucketGrant `json:"grants"`
	Restrictions []BucketGrant `json:"restrictions,omitempty"`
	Notes        []string      `json:"notes"`
}

func presentBucketsBucketAccessRow(localizer *localization.Localizer, v buckets.BucketAccessRow) BucketAccessRow {
	return BucketAccessRow{BucketAccessRow: v,
		Grants:       mapSlice(v.Grants, func(x buckets.BucketGrant) BucketGrant { return presentBucketsBucketGrant(localizer, x) }),
		Restrictions: mapSlice(v.Restrictions, func(x buckets.BucketGrant) BucketGrant { return presentBucketsBucketGrant(localizer, x) }),
		Notes:        mapSlice(v.Notes, func(x messages.Ref) string { return localizer.Render(x) }),
	}
}

type BucketDetails struct {
	buckets.BucketDetails
	Policy     *DetailField `json:"policy,omitempty"`
	Quota      *DetailField `json:"quota,omitempty"`
	Tags       *DetailField `json:"tags,omitempty"`
	Versioning *DetailField `json:"versioning,omitempty"`
	Locking    *DetailField `json:"locking,omitempty"`
	Lifecycle  *DetailField `json:"lifecycle,omitempty"`
	CORS       *DetailField `json:"cors,omitempty"`
	Encryption *DetailField `json:"encryption,omitempty"`
}

func presentBucketsBucketDetails(localizer *localization.Localizer, v buckets.BucketDetails) BucketDetails {
	return BucketDetails{BucketDetails: v,
		Policy:     mapPointer(v.Policy, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
		Quota:      mapPointer(v.Quota, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
		Tags:       mapPointer(v.Tags, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
		Versioning: mapPointer(v.Versioning, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
		Locking:    mapPointer(v.Locking, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
		Lifecycle:  mapPointer(v.Lifecycle, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
		CORS:       mapPointer(v.CORS, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
		Encryption: mapPointer(v.Encryption, func(x buckets.DetailField) DetailField { return presentBucketsDetailField(localizer, x) }),
	}
}

type BucketGrant struct {
	buckets.BucketGrant
	Principals    []string `json:"principals,omitempty"`
	NotPrincipals []string `json:"notPrincipals,omitempty"`
	Policy        string   `json:"policy"`
	Source        string   `json:"source"`
}

func presentBucketsBucketGrant(localizer *localization.Localizer, v buckets.BucketGrant) BucketGrant {
	return BucketGrant{BucketGrant: v,
		Principals:    mapSlice(v.Principals, func(x messages.Ref) string { return localizer.Render(x) }),
		NotPrincipals: mapSlice(v.NotPrincipals, func(x messages.Ref) string { return localizer.Render(x) }),
		Policy:        localizer.Render(v.Policy),
		Source:        localizer.Render(v.Source),
	}
}

type DetailField struct {
	buckets.DetailField
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

func presentBucketsDetailField(localizer *localization.Localizer, v buckets.DetailField) DetailField {
	return DetailField{DetailField: v,
		Value: present(localizer, v.Value),
		Error: localizer.Render(v.Error),
	}
}

type LifecycleRow struct {
	buckets.LifecycleRow
	Conditions   []string `json:"conditions"`
	ExtraActions []string `json:"extraActions"`
}

func presentBucketsLifecycleRow(localizer *localization.Localizer, v buckets.LifecycleRow) LifecycleRow {
	return LifecycleRow{LifecycleRow: v,
		Conditions:   mapSlice(v.Conditions, func(x messages.Ref) string { return localizer.Render(x) }),
		ExtraActions: mapSlice(v.ExtraActions, func(x messages.Ref) string { return localizer.Render(x) }),
	}
}

type LifecycleSnapshot struct {
	buckets.LifecycleSnapshot
	Rules   []LifecycleRow `json:"rules"`
	Warning string         `json:"warning,omitempty"`
}

func presentBucketsLifecycleSnapshot(localizer *localization.Localizer, v buckets.LifecycleSnapshot) LifecycleSnapshot {
	return LifecycleSnapshot{LifecycleSnapshot: v,
		Rules:   mapSlice(v.Rules, func(x buckets.LifecycleRow) LifecycleRow { return presentBucketsLifecycleRow(localizer, x) }),
		Warning: localizer.Render(v.Warning),
	}
}
