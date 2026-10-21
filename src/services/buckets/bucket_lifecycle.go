package buckets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"

	"minio-web/api/src/services/apperror"
	validationsvc "minio-web/api/src/services/validation"
)

type LifecycleTiering struct {
	Current        string `json:"current"`
	Days           int    `json:"days"`
	Date           string `json:"date"`
	Noncurrent     string `json:"noncurrent"`
	NoncurrentDays int    `json:"noncurrentDays"`
}
type LifecycleInput struct {
	Tiering        *LifecycleTiering `json:"tiering,omitempty"`
	ID             string            `json:"id"`
	Enabled        bool              `json:"enabled"`
	Prefix         string            `json:"prefix"`
	ExpirationDays int               `json:"expirationDays"`
	ExpirationDate string            `json:"expirationDate"`
	NoncurrentDays int               `json:"noncurrentDays"`
	KeepVersions   int               `json:"keepVersions"`
	DeleteMarkers  bool              `json:"deleteMarkers"`
}
type LifecycleRow struct {
	Index        int            `json:"index"`
	Status       string         `json:"status"`
	Rule         LifecycleInput `json:"rule"`
	Conditions   []string       `json:"conditions"`
	ExtraActions []string       `json:"extraActions"`
}
type LifecycleSnapshot struct {
	Rules    []LifecycleRow `json:"rules"`
	Revision string         `json:"revision"`
	Writable bool           `json:"writable"`
	Warning  string         `json:"warning,omitempty"`
}
type LifecycleState struct {
	Config *lifecycle.Configuration `json:"-"`
	View   LifecycleSnapshot        `json:"-"`
}

// Paths include sibling indexes to detect loss of repeated transitions/tags.
// Whitespace in keys, prefixes and tag values is significant and is preserved.
func LifecycleLeaves(raw []byte) (map[string]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	out := map[string]string{}
	type frame struct {
		Path, Name string          `json:"-"`
		Counts     map[string]int  `json:"-"`
		Text       strings.Builder `json:"-"`
		Children   bool            `json:"-"`
	}
	stack := []*frame{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			path := "/" + value.Name.Local
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = true
				parent.Counts[value.Name.Local]++
				path = parent.Path + "/" + value.Name.Local + "[" + strconv.Itoa(parent.Counts[value.Name.Local]) + "]"
			}
			item := &frame{Path: path, Name: value.Name.Local, Counts: map[string]int{}}
			for _, attr := range value.Attr {
				if attr.Name.Local != "xmlns" && attr.Name.Space != "xmlns" {
					out[path+"/@"+attr.Name.Local] = attr.Value
				}
			}
			if value.Name.Space != "" && value.Name.Space != "http://s3.amazonaws.com/doc/2006-03-01/" {
				out[path+"/@namespace"] = value.Name.Space
			}
			stack = append(stack, item)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text.Write(value)
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("invalid XML")
			}
			item := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			text := item.Text.String()
			if !item.Children && item.Path != "/LifecycleConfiguration/ExpiryUpdatedAt[1]" && !(text == "" && (item.Name == "LifecycleConfiguration" || item.Name == "Filter" || item.Name == "And")) {
				if item.Name == "Date" {
					if date, err := time.Parse(time.RFC3339Nano, text); err == nil {
						text = date.UTC().Format(time.RFC3339Nano)
					}
				}
				out[item.Path] = text
			}
		}
	}
	return out, nil
}
func LifecycleRoundTripSafe(raw []byte, config any) bool {
	encoded, err := xml.Marshal(config)
	if err != nil {
		return false
	}
	original, err := LifecycleLeaves(raw)
	if err != nil {
		return false
	}
	restored, err := LifecycleLeaves(encoded)
	if err != nil {
		return false
	}
	for key, value := range original {
		if actual, exists := restored[key]; !exists || actual != value {
			return false
		}
	}
	return true
}
func LifecycleRuleInput(r lifecycle.Rule) LifecycleInput {
	prefix := r.Prefix
	if !r.RuleFilter.And.IsEmpty() {
		prefix = r.RuleFilter.And.Prefix
	} else if r.RuleFilter.Prefix != "" {
		prefix = r.RuleFilter.Prefix
	}
	date := ""
	if !r.Expiration.IsDateNull() {
		date = r.Expiration.Date.UTC().Format("2006-01-02")
	}
	input := LifecycleInput{ID: r.ID, Enabled: r.Status == "Enabled", Prefix: prefix, ExpirationDays: int(r.Expiration.Days), ExpirationDate: date, NoncurrentDays: int(r.NoncurrentVersionExpiration.NoncurrentDays), KeepVersions: r.NoncurrentVersionExpiration.NewerNoncurrentVersions, DeleteMarkers: r.Expiration.DeleteMarker.IsEnabled()}
	if !r.Transition.IsNull() || !r.NoncurrentVersionTransition.IsStorageClassEmpty() {
		input.Tiering = &LifecycleTiering{Current: r.Transition.StorageClass, Days: int(r.Transition.Days), Noncurrent: r.NoncurrentVersionTransition.StorageClass, NoncurrentDays: int(r.NoncurrentVersionTransition.NoncurrentDays)}
		if !r.Transition.Date.IsZero() {
			input.Tiering.Date = r.Transition.Date.UTC().Format("2006-01-02")
		}
	}
	return input
}
func LifecycleRows(config *lifecycle.Configuration) []LifecycleRow {
	rows := []LifecycleRow{}
	for index, r := range config.Rules {
		conditions, extra := []string{}, []string{}
		f := r.RuleFilter
		tags := f.And.Tags
		if !f.Tag.IsEmpty() {
			tags = append(append([]lifecycle.Tag{}, tags...), f.Tag)
		}
		for _, tag := range tags {
			conditions = append(conditions, "Метка объекта: "+tag.Key+" = "+tag.Value)
		}
		lower, upper := f.ObjectSizeGreaterThan, f.ObjectSizeLessThan
		if !f.And.IsEmpty() {
			lower, upper = f.And.ObjectSizeGreaterThan, f.And.ObjectSizeLessThan
		}
		if lower > 0 {
			conditions = append(conditions, fmt.Sprintf("Размер больше %d байт", lower))
		}
		if upper > 0 {
			conditions = append(conditions, fmt.Sprintf("Размер меньше %d байт", upper))
		}

		if !r.AbortIncompleteMultipartUpload.IsDaysNull() {
			extra = append(extra, fmt.Sprintf("Очистка незавершённых загрузок через %d дн.", r.AbortIncompleteMultipartUpload.DaysAfterInitiation))
		}
		if r.Expiration.DeleteAll.IsEnabled() {
			extra = append(extra, "Удаление всех версий при истечении срока текущей версии")
		}
		if !r.AllVersionsExpiration.IsNull() {
			extra = append(extra, fmt.Sprintf("Удаление всех версий через %d дн.", r.AllVersionsExpiration.Days))
		}
		if !r.DelMarkerExpiration.IsNull() {
			extra = append(extra, fmt.Sprintf("Удаление маркера вместе с версиями через %d дн.", r.DelMarkerExpiration.Days))
		}
		if r.Compression != nil {
			extra = append(extra, "Сжатие текущей версии")
		}
		if r.NoncurrentVersionCompression != nil {
			extra = append(extra, "Сжатие старых версий")
		}
		rows = append(rows, LifecycleRow{Index: index, Status: r.Status, Rule: LifecycleRuleInput(r), Conditions: conditions, ExtraActions: extra})
	}
	return rows
}
func ReadLifecycle(ctx context.Context, s ReadLifecycleClient, bucket string) (LifecycleState, error) {
	read, err := s.ReadLifecycleConfig(ctx, bucket)
	config, raw := read.Value, read.Raw
	if minio.ToErrorResponse(err).Code == "NoSuchLifecycleConfiguration" {
		config = lifecycle.NewConfiguration()
		err = nil
		raw = []byte("<LifecycleConfiguration/>")
	}
	if err != nil {
		return LifecycleState{}, err
	}
	hash := sha256.Sum256(raw)
	safe := LifecycleRoundTripSafe(raw, config)
	view := LifecycleSnapshot{Rules: LifecycleRows(config), Revision: hex.EncodeToString(hash[:]), Writable: safe}
	if !safe {
		view.Warning = "В конфигурации есть параметры, которые текущий SDK не может сохранить без потерь. Изменения недоступны; исходные правила сохранены в MinIO."
	}
	return LifecycleState{Config: config, View: view}, nil
}
func LifecycleField(state LifecycleState, err error) DetailField {
	if err != nil {
		return Detail(nil, err)
	}
	return DetailField{Value: state.View, Revision: state.View.Revision}
}

func ApplyLifecycleInput(old lifecycle.Rule, input LifecycleInput) (lifecycle.Rule, error) {
	invalid := func(message string) (lifecycle.Rule, error) {
		return old, apperror.New(400, "InvalidLifecycleRule", message)
	}
	if input.ID == "" || len(input.ID) > 255 || !utf8.ValidString(input.ID) || strings.ContainsFunc(input.ID, unicode.IsControl) {
		return invalid("Имя правила: от 1 до 255 байт, без управляющих символов.")
	}
	if len(input.Prefix) > 1024 || !utf8.ValidString(input.Prefix) || strings.ContainsFunc(input.Prefix, unicode.IsControl) {
		return invalid("Путь: до 1024 байт, без управляющих символов. Пустой путь означает весь бакет.")
	}
	for _, days := range []int{input.ExpirationDays, input.NoncurrentDays, input.KeepVersions} {
		if days < 0 || days > 2147483647 {
			return invalid("Сроки и число сохраняемых версий должны быть целыми числами от 0 до 2 147 483 647.")
		}
	}

	count := 0
	if input.ExpirationDays > 0 {
		count++
	}
	if input.ExpirationDate != "" {
		count++
	}
	if input.DeleteMarkers {
		count++
	}
	if count > 1 {
		return invalid("Выберите для текущих версий срок в днях, дату или очистку маркеров удаления.")
	}
	if t := input.Tiering; t != nil {
		if t.Days < 0 || t.Days > 2147483647 || t.NoncurrentDays < 0 || t.NoncurrentDays > 2147483647 {
			return invalid("Укажите корректный возраст переноса в днях.")
		}
		for i, name := range []string{t.Current, t.Noncurrent} {
			previous := old.Transition.StorageClass
			if i == 1 {
				previous = old.NoncurrentVersionTransition.StorageClass
			}
			if name != "" && name != previous && !validationsvc.TierNamePattern.MatchString(name) {
				return invalid("Имя уровня: 3–32 заглавных латинских символа, цифры, _ и -.")
			}
		}
		old.Transition = lifecycle.Transition{}
		if t.Current != "" {
			old.Transition = lifecycle.Transition{StorageClass: t.Current, Days: lifecycle.ExpirationDays(t.Days)}
			if t.Date != "" {
				date, e := time.Parse("2006-01-02", t.Date)
				if e != nil || t.Days != 0 {
					return invalid("Укажите дату переноса или возраст в днях.")
				}
				old.Transition.Date = lifecycle.ExpirationDate{Time: date}
			}
		} else if t.Date != "" || t.Days != 0 {
			return invalid("Выберите уровень для переноса текущих версий.")
		}
		retained := old.NoncurrentVersionTransition.NewerNoncurrentVersions
		old.NoncurrentVersionTransition = lifecycle.NoncurrentVersionTransition{}
		if t.Noncurrent != "" {
			if t.NoncurrentDays < 1 {
				return invalid("Возраст переноса старых версий должен быть больше нуля.")
			}
			old.NoncurrentVersionTransition = lifecycle.NoncurrentVersionTransition{StorageClass: t.Noncurrent, NoncurrentDays: lifecycle.ExpirationDays(t.NoncurrentDays), NewerNoncurrentVersions: retained}
		} else if t.NoncurrentDays != 0 {
			return invalid("Выберите уровень для старых версий.")
		}
	}
	old.ID = input.ID
	old.Status = "Disabled"
	if input.Enabled {
		old.Status = "Enabled"
	}
	// Preserve conditions when changing only prefix. Normalize a single condition
	// back out of And: an And with just one Tag is invalid on S3/MinIO.
	f := old.RuleFilter
	tags := append([]lifecycle.Tag{}, f.And.Tags...)
	if !f.Tag.IsEmpty() {
		tags = append(tags, f.Tag)
	}
	lower, upper := f.ObjectSizeGreaterThan, f.ObjectSizeLessThan
	if !f.And.IsEmpty() {
		lower, upper = f.And.ObjectSizeGreaterThan, f.And.ObjectSizeLessThan
	}
	conditions := len(tags)
	if input.Prefix != "" {
		conditions++
	}
	if lower > 0 {
		conditions++
	}
	if upper > 0 {
		conditions++
	}
	old.Prefix = ""
	old.RuleFilter = lifecycle.Filter{}
	if conditions > 1 {
		old.RuleFilter.And = lifecycle.And{Prefix: input.Prefix, Tags: tags, ObjectSizeGreaterThan: lower, ObjectSizeLessThan: upper}
	} else {
		old.RuleFilter.Prefix = input.Prefix
		old.RuleFilter.ObjectSizeGreaterThan = lower
		old.RuleFilter.ObjectSizeLessThan = upper
		if len(tags) == 1 {
			old.RuleFilter.Tag = tags[0]
		}
	}
	exp := lifecycle.Expiration{Days: lifecycle.ExpirationDays(input.ExpirationDays), DeleteMarker: lifecycle.ExpireDeleteMarker(input.DeleteMarkers), DeleteAll: old.Expiration.DeleteAll}
	if input.ExpirationDate != "" {
		date, err := time.Parse("2006-01-02", input.ExpirationDate)
		if err != nil {
			return invalid("Укажите корректную дату удаления в формате ГГГГ-ММ-ДД.")
		}
		exp.Date = lifecycle.ExpirationDate{Time: date}
	}
	old.Expiration = exp
	old.NoncurrentVersionExpiration = lifecycle.NoncurrentVersionExpiration{NoncurrentDays: lifecycle.ExpirationDays(input.NoncurrentDays), NewerNoncurrentVersions: input.KeepVersions}
	if old.Expiration.IsNull() && old.NoncurrentVersionExpiration.NoncurrentDays == 0 && old.NoncurrentVersionExpiration.NewerNoncurrentVersions == 0 && old.Transition.IsNull() && old.NoncurrentVersionTransition.IsStorageClassEmpty() && old.AbortIncompleteMultipartUpload.IsDaysNull() && old.AllVersionsExpiration.IsNull() && old.DelMarkerExpiration.IsNull() && old.Compression == nil && old.NoncurrentVersionCompression == nil {
		return invalid("Добавьте хотя бы одно действие: удаление версий/маркеров или перенос в уровень хранения.")
	}
	return old, nil
}
