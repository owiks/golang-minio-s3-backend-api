package runtime

import (
	"net/http"
	"sync/atomic"
)

type ResponseStatusTransport struct {
	Base   http.RoundTripper `json:"-"`
	Status atomic.Int32      `json:"-"`
}

func (t *ResponseStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.Base.RoundTrip(r)
	if resp != nil {
		t.Status.Store(int32(resp.StatusCode))
	}
	return resp, err
}
