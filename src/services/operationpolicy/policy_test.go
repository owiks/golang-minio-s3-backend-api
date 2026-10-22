package operationpolicy

import (
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
)

func TestPolicySeparatesOperatorRestrictions(t *testing.T) {
	for _, p := range []Policy{{}, {ReadOnly: true}, {DisableUploads: true}, {DisableDeletes: true}, {DisableAdminChanges: true}} {
		for _, action := range []Action{0, Write, Upload, Delete, Admin, Upload | Delete, Admin | Delete} {
			want := action != 0 && p.ReadOnly || action&Upload != 0 && p.DisableUploads || action&Delete != 0 && p.DisableDeletes || action&Admin != 0 && p.DisableAdminChanges
			err := p.Check(action)
			if (err != nil) != want {
				t.Fatal(p, action, err)
			}
			if err != nil && (apperror.Public(err).Status != 503 || apperror.Public(err).Code != "OperationDisabled") {
				t.Fatal(err)
			}
		}
	}
}
