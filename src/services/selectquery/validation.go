package selectquery

import (
	"strings"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

func invalidRequest() error {
	return apperror.New(400, "InvalidSelectQuery", messages.SelectqueryInvalidSelectQuery)
}
func oneChar(value string) bool {
	return value == "" || utf8.ValidString(value) && utf8.RuneCountInString(value) == 1 && !strings.ContainsAny(value, "\x00\r\n")
}
func recordDelimiter(value string) bool { return value == "" || value == "\n" || value == "\r\n" }

func validate(request Request) (minio.SelectObjectOptions, Request, error) {
	var options minio.SelectObjectOptions
	if len(request.Expression) == 0 || len(request.Expression) > MaxExpressionBytes || !utf8.ValidString(request.Expression) || strings.ContainsRune(request.Expression, 0) || strings.TrimSpace(request.Expression) == "" {
		return options, request, invalidRequest()
	}
	if (request.Input.CSV == nil) == (request.Input.JSON == nil) || (request.Output.CSV == nil) == (request.Output.JSON == nil) {
		return options, request, invalidRequest()
	}
	if request.TimeoutSeconds == 0 {
		request.TimeoutSeconds = 20
	}
	if request.MaxResultBytes == 0 {
		request.MaxResultBytes = 8 << 20
	}
	if request.TimeoutSeconds < 1 || request.TimeoutSeconds > MaxSeconds || request.MaxResultBytes < 1 || request.MaxResultBytes > MaxResultBytes {
		return options, request, invalidRequest()
	}
	options.Expression = request.Expression
	options.ExpressionType = minio.QueryExpressionTypeSQL
	options.InputSerialization.CompressionType = minio.SelectCompressionNONE
	if in := request.Input.CSV; in != nil {
		if in.Header != "" && in.Header != "USE" && in.Header != "IGNORE" && in.Header != "NONE" || !oneChar(in.FieldDelimiter) || !oneChar(in.QuoteCharacter) || !oneChar(in.QuoteEscapeCharacter) || !oneChar(in.Comments) || !recordDelimiter(in.RecordDelimiter) {
			return options, request, invalidRequest()
		}
		csv := &minio.CSVInputOptions{FileHeaderInfo: minio.CSVFileHeaderInfo(in.Header), FieldDelimiter: in.FieldDelimiter, RecordDelimiter: in.RecordDelimiter, QuoteCharacter: in.QuoteCharacter, QuoteEscapeCharacter: in.QuoteEscapeCharacter, Comments: in.Comments}
		options.InputSerialization.CSV = csv
	} else {
		if request.Input.JSON.Type != "DOCUMENT" && request.Input.JSON.Type != "LINES" {
			return options, request, invalidRequest()
		}
		options.InputSerialization.JSON = &minio.JSONInputOptions{Type: minio.JSONType(request.Input.JSON.Type)}
	}
	if out := request.Output.CSV; out != nil {
		if !oneChar(out.FieldDelimiter) || !oneChar(out.QuoteCharacter) || !oneChar(out.QuoteEscapeCharacter) || !recordDelimiter(out.RecordDelimiter) || out.QuoteFields != "" && out.QuoteFields != "Always" && out.QuoteFields != "AsNeeded" {
			return options, request, invalidRequest()
		}
		options.OutputSerialization.CSV = &minio.CSVOutputOptions{FieldDelimiter: out.FieldDelimiter, RecordDelimiter: out.RecordDelimiter, QuoteCharacter: out.QuoteCharacter, QuoteEscapeCharacter: out.QuoteEscapeCharacter, QuoteFields: minio.CSVQuoteFields(out.QuoteFields)}
	} else {
		if !recordDelimiter(request.Output.JSON.RecordDelimiter) {
			return options, request, invalidRequest()
		}
		options.OutputSerialization.JSON = &minio.JSONOutputOptions{RecordDelimiter: request.Output.JSON.RecordDelimiter}
	}
	return options, request, nil
}
