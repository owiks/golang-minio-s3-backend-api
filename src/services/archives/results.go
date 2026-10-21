package archives

type ZIPContents struct {
	Revision string    `json:"revision"`
	Items    []ZipItem `json:"items"`
}
type ExtractedEntry struct {
	OK   bool  `json:"ok"`
	Size int64 `json:"size"`
}
type GzipContent struct {
	Text       string `json:"text"`
	Compressed int    `json:"compressed"`
	Expanded   int    `json:"expanded"`
}
