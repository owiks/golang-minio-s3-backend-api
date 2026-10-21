package multipart

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/iam"
	"minio-web/api/src/services/objects/write"
	"minio-web/api/src/services/securetoken"
	"minio-web/api/src/services/uploads/integrity"
	"minio-web/api/src/services/validation"
)

var metadataName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func invalid(code, message string) error { return apperror.New(http.StatusBadRequest, code, message) }
func notFound() error {
	return apperror.New(404, "MultipartNotFound", "Загрузка не найдена в текущей сессии.")
}
func transitionError() error {
	return apperror.New(409, "MultipartStateConflict", "Действие недоступно в текущем состоянии загрузки. Получите её состояние.")
}

func canWrite(c Client, bucket, key string) error {
	if policy := c.ActionPolicySnapshot(); policy != nil && !policy.Allows(c.ActorName(), iam.Query{Action: "s3:PutObject", Bucket: bucket, Key: key}) {
		return apperror.New(403, "AccessDenied", "Нет прав на запись в это назначение.")
	}
	return nil
}

func Create(ctx context.Context, store Store, c Client, input CreateRequest) (Snapshot, error) {
	if !c.ConditionalCompletionConfigured() {
		return Snapshot{}, apperror.New(409, "ConditionalCompletionUnverified", "Условное завершение multipart не подтверждено для этого подключения. Администратор должен проверить сервер и явно разрешить его в конфигурации API.")
	}
	if input.Bucket == "" || len(input.Bucket) > 63 || !validation.ValidFileKey(input.Key) {
		return Snapshot{}, invalid("InvalidFileKey", "Некорректный бакет или путь файла.")
	}
	if input.Size < 0 || input.Size > objectwrite.MaxFileSize {
		return Snapshot{}, invalid("InvalidUploadSize", "Размер файла должен быть от 0 до 5 ГиБ.")
	}
	if len(input.ContentType) > 256 || strings.ContainsAny(input.ContentType, "\r\n") {
		return Snapshot{}, invalid("InvalidContentType", "Некорректный тип содержимого.")
	}
	if err := canWrite(c, input.Bucket, input.Key); err != nil {
		return Snapshot{}, err
	}
	metadata := make(map[string]string, len(input.Metadata)+2)
	total := 0
	if len(input.Metadata) > 32 {
		return Snapshot{}, invalid("InvalidMetadata", "Допускается до 32 метаданных.")
	}
	for k, v := range input.Metadata {
		name := strings.ToLower(k)
		if !metadataName.MatchString(name) || strings.HasPrefix(name, "x-amz-") || strings.HasPrefix(name, "x-minio-") || strings.HasPrefix(name, "minio-web-") || strings.ContainsAny(v, "\r\n") || len(v) > 2048 {
			return Snapshot{}, invalid("InvalidMetadata", "Некорректные метаданные файла.")
		}
		total += len(name) + len(v)
		metadata[name] = v
	}
	if total > 8192 {
		return Snapshot{}, invalid("InvalidMetadata", "Размер метаданных не должен превышать 8 КиБ.")
	}
	count := max(1, int((input.Size+PartSize-1)/PartSize))
	var checksum *integrity.Spec
	if input.Checksum != nil {
		spec, err := integrity.Normalize(*input.Checksum, "OBJECT")
		if err != nil {
			return Snapshot{}, err
		}
		if spec.Algorithm != "SHA256" || spec.Type != "COMPOSITE" || !strings.HasSuffix(spec.Value, "-"+partNumber(count)) {
			return Snapshot{}, apperror.New(422, "UnsupportedChecksumMode", "Для управляемой загрузки доступна SHA256 COMPOSITE с количеством частей плана.")
		}
		checksum = &spec
	}
	current, err := objectwrite.DestinationState(ctx, c, input.Bucket, input.Key)
	if err != nil {
		return Snapshot{}, err
	}
	opts, err := objectwrite.WriteOptions(current, input.Revision, input.ContentType)
	if err != nil {
		return Snapshot{}, err
	}
	now := time.Now()
	expires := minTime(now.Add(TTL), c.SessionExpiresAt())
	if !expires.After(now) {
		return Snapshot{}, apperror.New(401, "SessionExpired", "Сессия истекла. Подключитесь заново.")
	}
	op := &Operation{id: securetoken.New(), owner: c.SessionID(), bucket: input.Bucket, key: input.Key, size: input.Size, count: count, created: now, expires: expires, destination: current, metadata: metadata, contentType: input.ContentType, checksum: checksum, state: "creating", sealed: map[int]string{}, cleanup: c}
	// Publish ownership before the first modifying call; even an ambiguous
	// initiate response has a stable API operation ID and no automatic retry.
	release, err := op.locks.Acquire(ctx, "action")
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	if err = store.Add(op); err != nil {
		return Snapshot{}, err
	}
	metadata[markerKey] = op.id
	metadata["x-amz-checksum-algorithm"] = "SHA256"
	opts.UserMetadata = metadata
	callCtx, cancel := op.actionContext(ctx)
	defer cancel()
	uid, err := c.NewMultipartUpload(callCtx, op.bucket, op.key, opts)
	if err != nil && definitive(err) {
		observe(ctx, c, "create", op.bucket, op.key, err)
		op.mu.Lock()
		op.state = "failed"
		op.cleanup = nil
		op.mu.Unlock()
		store.Discard(op)
		return Snapshot{}, err
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if err != nil {
		observe(ctx, c, "create", op.bucket, op.key, err)
		op.state = "create_uncertain"
		op.reason = "CreateOutcomeUnknown"
		return op.snapshotLocked(), nil
	}
	if uid == "" || len(uid) > 4096 || strings.ContainsAny(uid, "\r\n") {
		observe(ctx, c, "create", op.bucket, op.key, apperror.New(502, "InvalidCreateResponse", "Некорректный ответ хранилища."))
		op.state = "create_uncertain"
		op.reason = "InvalidCreateResponse"
		return op.snapshotLocked(), nil
	}
	op.uploadID = uid
	op.state = "active"
	observe(ctx, c, "create", op.bucket, op.key, nil)
	return op.snapshotLocked(), nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func definitive(err error) bool {
	e := minio.ToErrorResponse(err)
	return e.Code != "" && e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != 408 && e.StatusCode != 429
}

func (o *Operation) actionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(ctx, o.expires)
	o.mu.Lock()
	o.cancel = cancel
	if o.stopped {
		cancel()
	}
	o.mu.Unlock()
	return ctx, func() { cancel(); o.mu.Lock(); o.cancel = nil; o.mu.Unlock() }
}

func acquire(ctx context.Context, store Store, c Client, id string) (*Operation, func(), error) {
	o := store.Get(id)
	if o == nil || o.owner != c.SessionID() {
		return nil, nil, notFound()
	}
	release, err := o.locks.Acquire(ctx, "action")
	if err != nil {
		return nil, nil, err
	}
	o.mu.Lock()
	stopped := o.stopped
	o.mu.Unlock()
	if stopped || !o.expires.After(time.Now()) {
		release()
		return nil, nil, apperror.New(410, "MultipartExpired", "Срок загрузки истёк. Создайте новую загрузку.")
	}
	return o, release, nil
}
