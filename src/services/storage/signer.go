package storage

import (
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"

	"minio-web/api/src/services/apperror"
	"minio-web/api/src/services/messages"
)

// NewSigner constructs an official SDK client with a network-disabled request
// transport. Presigning may otherwise fetch the bucket's region through
// the public endpoint, outside the checked internal transport.
func NewSigner(c Config) (*minio.Client, error) {
	c.Region = strings.TrimSpace(c.Region)
	if c.Region == "" {
		return nil, apperror.New(400, "SigningRegionRequired", messages.StorageSigningRegionRequired)
	}
	return NewS3(c, offlineSigningTransport{})
}

type offlineSigningTransport struct{}

func (offlineSigningTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, apperror.New(502, "SigningNetworkDisabled", messages.StorageSigningNetworkDisabled)
}
