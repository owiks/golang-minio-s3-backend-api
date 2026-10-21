package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/minio/madmin-go/v3"
)

type serverInfoReader struct {
	t                 *testing.T
	infoErr, usageErr error
	usageCalls        int
}

func (f *serverInfoReader) ServerInfo(ctx context.Context, opts ...func(*madmin.ServerInfoOpts)) (madmin.InfoMessage, error) {
	f.t.Helper()
	if ctx.Err() != nil || len(opts) == 0 {
		f.t.Fatal("missing context or drive metrics options")
	}
	return madmin.InfoMessage{Region: "fixture-region"}, f.infoErr
}
func (f *serverInfoReader) DataUsageInfo(ctx context.Context) (madmin.DataUsageInfo, error) {
	f.t.Helper()
	f.usageCalls++
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 3*time.Second {
		f.t.Fatal("optional scanner read is not bounded")
	}
	return madmin.DataUsageInfo{LastUpdate: time.Unix(123, 0), ObjectsTotalCount: 12, ObjectsTotalSize: 1024}, f.usageErr
}

func TestServerInfoWithIndependentReader(t *testing.T) {
	for _, optionalFailure := range []bool{false, true} {
		name := "scanner available"
		if optionalFailure {
			name = "scanner denied"
		}
		t.Run(name, func(t *testing.T) {
			reader := &serverInfoReader{t: t}
			if optionalFailure {
				reader.usageErr = errors.New("scanner access denied")
			}
			value, err := ReadServerInfo(context.Background(), reader)
			if err != nil {
				t.Fatal(err)
			}
			result := value
			if result.Region != "fixture-region" || reader.usageCalls != 1 {
				t.Fatal("server information lost")
			}
			if optionalFailure {
				if result.Usage != nil || result.UsageError == "" {
					t.Fatal("scanner error must stay optional")
				}
			} else if result.Usage == nil || result.Usage.Objects != 12 || result.Usage.Size != 1024 {
				t.Fatal("scanner data lost")
			}
		})
	}
	t.Run("server error", func(t *testing.T) {
		upstream := errors.New("server unavailable")
		reader := &serverInfoReader{t: t, infoErr: upstream}
		if _, err := ReadServerInfo(context.Background(), reader); !errors.Is(err, upstream) || reader.usageCalls != 0 {
			t.Fatal("server error masked or optional request executed after failure")
		}
	})
}
