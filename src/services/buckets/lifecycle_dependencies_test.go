package buckets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"strconv"
	"testing"

	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/storage"
)

type lifecyclePort struct {
	raw            []byte
	status, writes int
}

func (f *lifecyclePort) ReadLifecycleConfig(context.Context, string) (storage.ConfigRead[*lifecycle.Configuration], error) {
	value := lifecycle.NewConfiguration()
	err := xml.Unmarshal(f.raw, value)
	return storage.ConfigRead[*lifecycle.Configuration]{Value: value, Raw: f.raw}, err
}
func (f *lifecyclePort) WriteLifecycleConfig(_ context.Context, _ string, value *lifecycle.Configuration) (int, error) {
	f.writes++
	if f.status == 204 {
		f.raw, _ = xml.Marshal(value)
	}
	return f.status, nil
}

func TestLifecyclePortPreservesRevisionAndWriteStatus(t *testing.T) {
	for _, status := range []int{204, 403, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			f := &lifecyclePort{status: status, raw: []byte(`<LifecycleConfiguration><Rule><ID>expire</ID><Status>Enabled</Status><Filter><Prefix>logs/</Prefix></Filter><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`)}
			state, err := ReadLifecycle(context.Background(), f, "test-bucket")
			hash := sha256.Sum256(f.raw)
			if err != nil || !state.View.Writable || state.View.Revision != hex.EncodeToString(hash[:]) {
				t.Fatal("raw revision or roundtrip safety lost", state, err)
			}
			index := 0
			result, err := ChangeLifecycle(context.Background(), f, "test-bucket", "delete", LifecycleChangeRequest{Revision: state.View.Revision, Index: &index, Confirmation: "expire"})
			if f.writes != 1 {
				t.Fatal("write not performed exactly once")
			}
			if status == 204 {
				stored, ok := result.Value.(LifecycleSnapshot)
				if err != nil || !ok || len(stored.Rules) != 0 || result.Revision == state.View.Revision {
					t.Fatal("readback lost", result, err)
				}
			} else if apperror.Public(err).Status != map[int]int{403: 403, 500: 502}[status] {
				t.Fatal("SDK nil-error DELETE status ignored", err)
			}
		})
	}
}

func TestLifecyclePortRejectsUnknownXMLBeforeWrite(t *testing.T) {
	f := &lifecyclePort{status: 204, raw: []byte(`<LifecycleConfiguration><FutureSetting>keep-me</FutureSetting></LifecycleConfiguration>`)}
	state, err := ReadLifecycle(context.Background(), f, "test-bucket")
	if err != nil || state.View.Writable {
		t.Fatal("unknown native field accepted for mutation", err)
	}
	_, err = ChangeLifecycle(context.Background(), f, "test-bucket", "create", LifecycleChangeRequest{Revision: state.View.Revision})
	if apperror.Public(err).Code != "LifecycleUnsupported" || f.writes != 0 {
		t.Fatal("unknown XML reached mutation adapter", err)
	}
}
