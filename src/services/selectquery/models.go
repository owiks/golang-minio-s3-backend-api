// Package selectquery runs bounded server-side queries against one S3 object.
package selectquery

const (
	MaxExpressionBytes       = 16 << 10
	MaxRequestBytes          = 32 << 10
	MaxSourceBytes     int64 = 256 << 20
	MaxResultBytes     int64 = 32 << 20
	ChunkBytes               = 32 << 10
	MaxSeconds               = 30
)

type CSVInput struct {
	Header               string `json:"header,omitempty"`
	FieldDelimiter       string `json:"fieldDelimiter,omitempty"`
	RecordDelimiter      string `json:"recordDelimiter,omitempty"`
	QuoteCharacter       string `json:"quoteCharacter,omitempty"`
	QuoteEscapeCharacter string `json:"quoteEscapeCharacter,omitempty"`
	Comments             string `json:"comments,omitempty"`
}
type JSONInput struct {
	Type string `json:"type"`
}
type Input struct {
	CSV  *CSVInput  `json:"csv,omitempty"`
	JSON *JSONInput `json:"json,omitempty"`
}
type CSVOutput struct {
	FieldDelimiter       string `json:"fieldDelimiter,omitempty"`
	RecordDelimiter      string `json:"recordDelimiter,omitempty"`
	QuoteCharacter       string `json:"quoteCharacter,omitempty"`
	QuoteEscapeCharacter string `json:"quoteEscapeCharacter,omitempty"`
	QuoteFields          string `json:"quoteFields,omitempty"`
}
type JSONOutput struct {
	RecordDelimiter string `json:"recordDelimiter,omitempty"`
}
type Output struct {
	CSV  *CSVOutput  `json:"csv,omitempty"`
	JSON *JSONOutput `json:"json,omitempty"`
}
type Request struct {
	Expression     string `json:"expression"`
	Input          Input  `json:"input"`
	Output         Output `json:"output"`
	MaxResultBytes int64  `json:"maxResultBytes,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}
type Statistics struct {
	BytesScanned   int64 `json:"bytesScanned"`
	BytesProcessed int64 `json:"bytesProcessed"`
	BytesReturned  int64 `json:"bytesReturned"`
}
type Result struct {
	Complete   bool        `json:"complete"`
	Bytes      int64       `json:"bytes"`
	Statistics *Statistics `json:"statistics,omitempty"`
}
