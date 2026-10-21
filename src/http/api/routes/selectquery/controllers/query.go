package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/owiks/golang-minio-s3-backend-api/src/http/stream"
	"github.com/owiks/golang-minio-s3-backend-api/src/http/validation"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/apperror"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/messages"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/result"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/runtime"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/selectquery"
)

func HandleSelectObject(app *runtime.State, w http.ResponseWriter, r *http.Request, s *runtime.Session) error {
	if r.URL.Query().Has("versionId") || r.URL.Query().Has("version") || r.Header.Get("Range") != "" {
		return apperror.New(400, "InvalidSelectQuery", messages.HttpInvalidSelectQuery)
	}
	var body selectquery.Request
	if err := validation.DecodeLimit(w, r, &body, selectquery.MaxRequestBytes); err != nil {
		return err
	}
	q := r.URL.Query()
	run, err := selectquery.Prepare(r.Context(), runtime.NewClients(s), app.CapabilityObserver(s), q.Get("bucket"), q.Get("key"), body)
	if err != nil {
		return err
	}
	defer run.Close()
	stream.WriteNDJSON(r.Context(), w, func(ctx context.Context, send func(result.StreamEvent[selectquery.Result]) bool) error {
		err := run.Stream(ctx, func(event result.StreamEvent[selectquery.Result]) bool {
			// Bind only actual delivery to the shorter Select budget. Keeping
			// this hook active during upstream reads would expire the downstream
			// before WriteNDJSON can deliver the terminal timeout error.
			defer stream.InterruptOnCancel(run.Context(), w)()
			return send(event)
		})
		if err == nil || ctx.Err() != nil {
			return err
		}
		// A timeout may happen while reading upstream, outside an active send.
		// Give its terminal error a short delivery budget instead of either
		// suppressing it immediately or retaining the long HTTP transfer budget.
		delivery, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		defer stream.InterruptOnCancel(delivery, w)()
		send(result.StreamEvent[selectquery.Result]{Error: apperror.Public(err)})
		return nil
	})
	return nil
}
