package archiveplan

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"minio-web/api/src/services/apperror"
	validationsvc "minio-web/api/src/services/validation"
)

func SafeArchivePath(key string) bool {
	return validationsvc.ValidFileKey(key) && !strings.HasPrefix(key, "/") && !strings.Contains(key, ":") && path.Clean(key) == key
}
func ArchiveFilename(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "minio-files.zip"
	}
	if !strings.HasSuffix(strings.ToLower(value), ".zip") {
		value += ".zip"
	}
	if len(value) > 180 || !utf8.ValidString(value) || strings.ContainsAny(value, `/\:*?"<>|`) || strings.ContainsFunc(value, unicode.IsControl) || value == ".zip" || strings.HasPrefix(value, ".") {
		return "", apperror.New(400, "InvalidArchiveName", "Укажите имя ZIP до 180 байт, без слешей, управляющих символов и знаков : * ? \" < > |.")
	}
	return value, nil
}
func ArchivePaths(items []ArchiveItem, base string) ([]string, error) {
	if len(base) > 1024 || !utf8.ValidString(base) || base != "" && !strings.HasSuffix(base, "/") {
		return nil, apperror.New(400, "InvalidArchiveBase", "Начало пути должно обозначать целую папку.")
	}
	paths := make([]string, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		if !strings.HasPrefix(item.Key, base) {
			return nil, apperror.New(400, "InvalidArchiveBase", "Выбранные файлы находятся вне указанной папки.")
		}
		name := strings.TrimPrefix(item.Key, base)
		if !SafeArchivePath(name) || !SafeArchivePath(item.Key) {
			paths[i] = ""
			continue
		}
		if seen[name] {
			return nil, apperror.New(400, "ArchivePathConflict", "В архиве совпали пути файлов. Выберите пути от корня бакета.")
		}
		seen[name] = true
		paths[i] = name
	}
	return paths, nil
}
