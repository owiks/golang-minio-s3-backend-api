package sessions

import "minio-web/api/src/services/runtime"

type ConnectionRegistry interface {
	AdmitIdentity(string, string) error
	AccessRevision(*runtime.Session) string
	StoreSession(*runtime.Session) error
}
