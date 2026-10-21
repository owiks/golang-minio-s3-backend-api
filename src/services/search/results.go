package search

import objectssvc "minio-web/api/src/services/objects"

type FileSearchResult struct {
	Entries   []objectssvc.Entry `json:"entries"`
	Scanned   int                `json:"scanned"`
	NextToken string             `json:"nextToken"`
}
