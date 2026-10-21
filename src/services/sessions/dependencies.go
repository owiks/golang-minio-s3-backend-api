package sessions

import "github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"

type ConnectionRegistry interface {
	AdmitIdentity(string, string) error
	AccessRevision(*runtime.Session) string
	StoreSession(*runtime.Session) error
}
