package archives

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/archives/plan"
	"minio-web/api/src/services/objects/state"
	searchsvc "minio-web/api/src/services/search"
	"minio-web/api/src/services/telemetry"
)

func PrepareArchive(ctx context.Context, store PrepareArchiveStore, s PrepareArchiveClient, body PrepareArchiveRequest) (archiveplan.ArchiveSnapshot, error) {
	modern := body.Entries != nil || body.Name != ""
	modes := 0
	if body.Keys != nil {
		modes++
	}
	if body.Prefix != nil {
		modes++
	}
	if body.Entries != nil {
		modes++
	}
	if body.Bucket == "" || len(body.Bucket) > 63 || modes != 1 || len(body.Keys) > MaxArchiveFiles || len(body.Entries) > MaxArchiveFiles || body.Prefix != nil && len(*body.Prefix) > 1024 {
		return archiveplan.ArchiveSnapshot{}, apperror.New(400, "InvalidArchive", "Выберите файлы или папки. Максимум 1000 файлов и 10 ГиБ на архив.")
	}
	name, err := archiveplan.ArchiveFilename(body.Name)
	if err != nil {
		return archiveplan.ArchiveSnapshot{}, err
	}
	draft := archiveplan.Config{SessionID: s.SessionID(), Bucket: body.Bucket, Name: name, Base: body.Base, RelativeBase: body.Base, Created: time.Now(), Issues: map[string]string{}}
	keys := map[string]bool{}
	excluded := map[string]bool{}
	if len(body.ExcludeKeys) > MaxArchiveFiles {
		return archiveplan.ArchiveSnapshot{}, apperror.New(400, "InvalidArchive", "Слишком много исключённых файлов.")
	}
	for _, key := range body.ExcludeKeys {
		excluded[key] = true
	}
	folders := []string{}
	for _, key := range body.Keys {
		if len(key) > 1024 || !utf8.ValidString(key) {
			return archiveplan.ArchiveSnapshot{}, apperror.New(400, "InvalidArchive", "Некорректный путь файла.")
		}
		if !excluded[key] {
			keys[key] = true
		}
	}
	if body.Prefix != nil {
		folders = append(folders, *body.Prefix)
	}
	for _, entry := range body.Entries {
		if len(entry.Key) > 1024 || !utf8.ValidString(entry.Key) {
			return archiveplan.ArchiveSnapshot{}, apperror.New(400, "InvalidArchive", "Некорректный путь элемента.")
		}
		switch entry.Kind {
		case "file":
			if entry.Key == "" {
				return archiveplan.ArchiveSnapshot{}, apperror.New(400, "InvalidArchive", "Пустой путь файла.")
			}
			if !excluded[entry.Key] {
				keys[entry.Key] = true
			}
		case "folder":
			if entry.Key != "" && !strings.HasSuffix(entry.Key, "/") {
				return archiveplan.ArchiveSnapshot{}, apperror.New(400, "InvalidArchive", "Путь папки должен заканчиваться слешем.")
			}
			folders = append(folders, entry.Key)
		default:
			return archiveplan.ArchiveSnapshot{}, apperror.New(400, "InvalidArchive", "Неизвестный тип элемента.")
		}
	}
	sort.Slice(folders, func(i, j int) bool { return len(folders[i]) < len(folders[j]) })
	roots := []string{}
	for _, folder := range folders {
		nested := false
		for _, root := range roots {
			if strings.HasPrefix(folder, root) {
				nested = true
				break
			}
		}
		if !nested {
			roots = append(roots, folder)
		}
	}
	for _, folder := range roots {
		token := ""
		guard := searchsvc.NewListingGuard(folder, MaxArchiveFiles+1)
		for {
			page, err := searchsvc.RecursivePage(ctx, s, body.Bucket, folder, token)
			if err != nil {
				if !modern {
					return archiveplan.ArchiveSnapshot{}, err
				}
				draft.Issues[folder] = apperror.Public(err).Message
				break
			}
			if err := guard.Check(ctx, token, page); err != nil {
				return archiveplan.ArchiveSnapshot{}, err
			}
			for _, obj := range page.Contents {
				if !strings.HasSuffix(obj.Key, "/") {
					if !excluded[obj.Key] {
						keys[obj.Key] = true
					}
				}
			}
			if len(keys) > MaxArchiveFiles {
				return archiveplan.ArchiveSnapshot{}, apperror.New(400, "ArchiveTooLarge", "Выбрано больше 1000 файлов. Уменьшите выделение.")
			}
			if !page.IsTruncated {
				break
			}
			token = page.NextContinuationToken
		}
	}
	if len(keys) > MaxArchiveFiles {
		return archiveplan.ArchiveSnapshot{}, apperror.New(400, "ArchiveTooLarge", "Выбрано больше 1000 файлов. Уменьшите выделение.")
	}
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	for _, key := range sorted {
		draft.Items = append(draft.Items, archiveplan.ArchiveItem{Key: key, State: "pending"})
	}
	paths, err := archiveplan.ArchivePaths(draft.Items, draft.Base)
	if err != nil {
		return archiveplan.ArchiveSnapshot{}, err
	}
	var total int64
	for i := range draft.Items {
		if err := ctx.Err(); err != nil {
			return archiveplan.ArchiveSnapshot{}, err
		}
		item := &draft.Items[i]
		item.Path = paths[i]
		if item.Path == "" {
			if !modern {
				return archiveplan.ArchiveSnapshot{}, apperror.New(400, "UnsafeArchivePath", "В списке есть небезопасный путь ZIP. Скачайте файл отдельно: "+item.Key)
			}
			item.State = "error"
			item.Message = "Путь небезопасно сохранять в ZIP. Скачайте файл отдельно."
			continue
		}
		info, err := s.StatObject(ctx, body.Bucket, item.Key, minio.StatObjectOptions{})
		if err != nil {
			if !modern {
				return archiveplan.ArchiveSnapshot{}, err
			}
			item.State = "error"
			item.Message = apperror.Public(err).Message
			continue
		}
		item.Size = info.Size
		item.Info = objectstate.DeletionSnapshot{ETag: info.ETag, Size: info.Size, Modified: info.LastModified, Version: info.VersionID}
		total += info.Size
		if total > MaxArchiveBytes {
			return archiveplan.ArchiveSnapshot{}, apperror.New(400, "ArchiveTooLarge", "Общий размер превышает 10 ГиБ. Выберите меньше файлов.")
		}
	}
	if len(draft.Items) == 0 && len(draft.Issues) == 0 && !modern {
		return archiveplan.ArchiveSnapshot{}, apperror.New(400, "EmptyArchive", "В выбранной папке нет файлов.")
	}
	if err := ctx.Err(); err != nil {
		return archiveplan.ArchiveSnapshot{}, err
	}
	plan := archiveplan.New(draft)
	ticket, err := store.Put(ctx, plan)
	if err != nil {
		return archiveplan.ArchiveSnapshot{}, err
	}
	if len(draft.Issues) > 0 {
		telemetry.Outcome(ctx, "partial", "ArchivePartial")
	} else {
		for _, item := range draft.Items {
			if item.State == "error" {
				telemetry.Outcome(ctx, "partial", "ArchivePartial")
				break
			}
		}
	}
	return plan.Snapshot(ticket), nil
}

type PrepareArchiveRequest struct {
	Bucket      string             `json:"bucket"`
	Keys        []string           `json:"keys"`
	Prefix      *string            `json:"prefix"`
	Entries     []ArchiveSelection `json:"entries"`
	Name        string             `json:"name"`
	Base        string             `json:"base"`
	ExcludeKeys []string           `json:"excludeKeys"`
}
