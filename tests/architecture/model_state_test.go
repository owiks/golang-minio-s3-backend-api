package architecture

import (
	"reflect"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/plan"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/archives/zipread"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/folders"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/folders/share"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/objects"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/uploads/invite"
)

func TestLiveDomainModelsExposeNoMutableFields(t *testing.T) {
	for _, model := range []reflect.Type{reflect.TypeFor[archiveplan.ArchivePlan](), reflect.TypeFor[foldershare.FolderShare](), reflect.TypeFor[uploadinvite.UploadInvite](), reflect.TypeFor[uploadinvite.WriteLease](), reflect.TypeFor[runtime.Session](), reflect.TypeFor[runtime.ActionSnapshot](), reflect.TypeFor[objects.ChecksumRun](), reflect.TypeFor[archives.Download](), reflect.TypeFor[folders.SharedRead](), reflect.TypeFor[zipread.Index](), reflect.TypeFor[zipread.Archive]()} {
		t.Run(model.PkgPath()+"/"+model.Name(), func(t *testing.T) {
			for i := range model.NumField() {
				field := model.Field(i)
				if field.IsExported() {
					t.Errorf("live domain model exposes field %s", field.Name)
				}
			}
			pointer := reflect.PointerTo(model)
			for _, method := range []string{"Lock", "Unlock", "RLock", "RUnlock", "UnmarshalJSON", "ClientConfig", "NewS3", "NewAdmin", "RequestS3"} {
				if _, ok := pointer.MethodByName(method); ok {
					t.Errorf("live domain model exposes %s", method)
				}
			}
		})
	}
}
