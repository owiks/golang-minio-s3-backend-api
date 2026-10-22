package runtime

import (
	"context"

	"github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7"
)

func (c *Clients) ListBuckets(ctx context.Context) ([]minio.BucketInfo, error) {
	rows, err := c.S3Client.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	policy, err := c.session.livePolicy(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]minio.BucketInfo, 0, len(rows))
	for _, row := range rows {
		if policy.ListsBucket(row.Name) {
			out = append(out, row)
		}
	}
	return out, nil
}

// Restricted access cannot read accountinfo's whole-bucket statistics or
// native policy. Build only the permitted inventory using native S3 probes.
func (c *Clients) AccountInfo(ctx context.Context, opts madmin.AccountOpts) (madmin.AccountInfo, error) {
	p, err := c.session.livePolicy(ctx)
	if err != nil {
		return madmin.AccountInfo{}, err
	}
	if p.Full() {
		return c.AdminClient.AccountInfo(ctx, opts)
	}
	buckets, err := c.ListBuckets(ctx)
	if err != nil {
		return madmin.AccountInfo{}, err
	}
	result := madmin.AccountInfo{Buckets: []madmin.BucketAccessInfo{}}
	for _, b := range buckets {
		prefix := ""
		for _, r := range p.Resources {
			if r.Bucket == b.Name {
				prefix = r.Prefix
				break
			}
		}
		if p.Allows("objects:read") {
			if _, err = c.ListObjectPage(ctx, b.Name, prefix, "", "/", 1); err != nil {
				return madmin.AccountInfo{}, err
			}
		} else {
			exists, e := c.S3Client.BucketExists(ctx, b.Name)
			if e != nil {
				return madmin.AccountInfo{}, e
			}
			if !exists {
				continue
			}
		}
		result.Buckets = append(result.Buckets, madmin.BucketAccessInfo{Name: b.Name, Created: b.CreationDate, Access: madmin.AccountAccess{Read: true}})
	}
	return result, nil
}
