package objects

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/objects/state"
	"minio-web/api/src/services/result"
	"minio-web/api/src/services/telemetry"
)

func PrepareChecksum(parent context.Context, s PrepareChecksumClient, bucket, key, expected string, calculate bool) (*ChecksumRun, error) {
	if strings.HasSuffix(key, "/") {
		return nil, apperror.New(400, "InvalidObject", "Выберите файл.")
	}
	if expected != "" {
		if value, e := hex.DecodeString(expected); e != nil || len(value) != 32 {
			return nil, apperror.New(400, "InvalidExpectedChecksum", "SHA-256 должен содержать 64 шестнадцатеричных символа.")
		}
	}
	ctx, cancel := context.WithCancel(parent)
	run := &ChecksumRun{ctx: ctx, cancel: cancel, client: s, bucket: bucket, key: key, expected: expected, calculate: calculate}
	prepared := false
	defer func() {
		if !prepared {
			run.Close()
		}
	}()
	info, err := s.StatObject(ctx, bucket, key, minio.StatObjectOptions{Checksum: true})
	warning := ""
	if err != nil {
		info, err = s.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
		warning = "Хранилище не вернуло контрольные суммы. Можно рассчитать SHA-256."
	}
	if err != nil {
		return nil, err
	}
	run.summary = ChecksumInfo{Size: info.Size, ETag: info.ETag, Version: info.VersionID, Checksums: AvailableChecksums(info), Warning: warning}
	run.info = info
	if !calculate {
		prepared = true
		return run, nil
	}
	opts := minio.GetObjectOptions{}
	_ = opts.SetMatchETag(info.ETag)
	object, err := s.GetObject(ctx, bucket, key, opts)
	if err != nil {
		return nil, err
	}
	run.object = object
	// Force the lazy GET before committing a streaming response, so denial stays an HTTP error.
	first := make([]byte, 128*1024)
	n, readErr := object.Read(first)
	if readErr != nil && readErr != io.EOF {
		return nil, readErr
	}
	run.first, run.n, run.readErr = first, n, readErr
	prepared = true
	return run, nil
}
func (c *ChecksumRun) stream(ctx context.Context, emit func(result.StreamEvent[ChecksumResult]) bool) {
	summary, info, object, first, n, readErr := c.summary, c.info, c.object, c.first, c.n, c.readErr
	send := func(v result.StreamEvent[ChecksumResult]) bool {
		if c.ctx.Err() != nil || ctx.Err() != nil {
			c.cancel()
			telemetry.Outcome(ctx, "cancelled", "ChecksumCancelled")
			return false
		}
		ok := emit(v)
		if !ok {
			c.cancel()
			telemetry.Outcome(ctx, "cancelled", "StreamWriteFailed")
		}
		return ok
	}
	fail := func(cause error) {
		telemetry.Outcome(ctx, "error", apperror.Public(cause).Code)
		_ = send(result.StreamEvent[ChecksumResult]{Error: apperror.Public(cause)})
	}
	hashes := map[string]hash.Hash{"SHA256": minio.ChecksumSHA256.Hasher()}
	for _, stored := range summary.Checksums {
		if stored.Type == "FULL_OBJECT" {
			if kind, ok := checksumAlgorithm(stored.Algorithm); ok {
				hashes[stored.Algorithm] = kind.Hasher()
			}
		}
	}
	count := int64(0)
	last := time.Now()
	if !send(result.StreamEvent[ChecksumResult]{Progress: &result.Progress{Bytes: 0, Total: info.Size}}) {
		return
	}
	for {
		if c.ctx.Err() != nil || ctx.Err() != nil {
			c.cancel()
			telemetry.Outcome(ctx, "cancelled", "ChecksumCancelled")
			return
		}
		if n > 0 {
			for _, h := range hashes {
				_, _ = h.Write(first[:n])
			}
			count += int64(n)
		}
		if time.Since(last) > 250*time.Millisecond {
			if !send(result.StreamEvent[ChecksumResult]{Progress: &result.Progress{Bytes: count, Total: info.Size}}) {
				return
			}
			last = time.Now()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			fail(readErr)
			return
		}
		n, readErr = object.Read(first)
	}
	if count != info.Size {
		fail(apperror.New(409, "IncompleteChecksumRead", "Получен неполный файл. Повторите проверку."))
		return
	}
	current, e := c.client.StatObject(c.ctx, c.bucket, c.key, minio.StatObjectOptions{})
	if e != nil {
		fail(e)
		return
	}
	if objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID}) != objectstate.DeletionRevision(objectstate.DeletionSnapshot{ETag: current.ETag, Size: current.Size, Modified: current.LastModified, Version: current.VersionID}) {
		fail(apperror.New(409, "ObjectChanged", "Файл изменился во время проверки. Запустите её заново."))
		return
	}
	sha256Sum := hashes["SHA256"].Sum(nil)
	computed := hex.EncodeToString(sha256Sum)
	status := "calculated"
	references := 0
	matched := true
	comparisons := []ChecksumComparison{}
	if expected := c.expected; expected != "" {
		references++
		// HEX letter case changes representation, not the digest bytes. Stored
		// Base64 below remains case-sensitive and must not be normalized.
		expectedSum, err := hex.DecodeString(expected)
		ok := err == nil && subtle.ConstantTimeCompare(expectedSum, sha256Sum) == 1
		matched = matched && ok
		comparisons = append(comparisons, ChecksumComparison{Algorithm: "SHA256", Source: "expected", Expected: expected, Computed: computed})
	}
	for _, stored := range summary.Checksums {
		h := hashes[stored.Algorithm]
		if stored.Type != "FULL_OBJECT" || h == nil {
			continue
		}
		actual := base64.StdEncoding.EncodeToString(h.Sum(nil))
		references++
		matched = matched && subtle.ConstantTimeCompare([]byte(actual), []byte(stored.Value)) == 1
		comparisons = append(comparisons, ChecksumComparison{Algorithm: stored.Algorithm, Source: "storage", Expected: stored.Value, Computed: actual})
	}
	if references > 0 {
		if matched {
			status = "matched"
		} else {
			status = "mismatch"
			telemetry.Outcome(ctx, "error", "ChecksumMismatch")
		}
	}
	if !send(result.StreamEvent[ChecksumResult]{Result: &ChecksumResult{SHA256: computed, Bytes: count, Version: info.VersionID, Status: status, Comparisons: comparisons}}) {
		telemetry.Outcome(ctx, "cancelled", "StreamWriteFailed")
	}
	return
}
