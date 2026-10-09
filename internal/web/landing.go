package web

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

//go:embed templates/landing.html
var landingFiles embed.FS

func landing(logger *slog.Logger) http.Handler {
	page, parseErr := template.ParseFS(landingFiles, "templates/landing.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			plainResponse(w, r, logger, http.StatusNotFound, "Not Found")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			plainResponse(w, r, logger, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		if r.Body != nil {
			// ReadFull obtains at most one byte and distinguishes clean EOF from
			// an input failure. The server's read deadline bounds stalled streams.
			var input [1]byte
			n, err := io.ReadFull(r.Body, input[:])
			if err != nil && !errors.Is(err, io.EOF) {
				logger.WarnContext(r.Context(), "request body read failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", "body_read")
			}
			if n != 0 || !errors.Is(err, io.EOF) {
				plainResponse(w, r, logger, http.StatusBadRequest, "Bad Request")
				return
			}
		}
		var body bytes.Buffer
		if parseErr != nil {
			logger.ErrorContext(r.Context(), "landing render failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", "template_parse")
			plainResponse(w, r, logger, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		if err := page.Execute(&body, nil); err != nil {
			logger.ErrorContext(r.Context(), "landing render failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", "template_render")
			plainResponse(w, r, logger, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			if _, err := w.Write(body.Bytes()); err != nil {
				logger.WarnContext(r.Context(), "response write failed", "request_id", w.Header().Get("X-Request-ID"), "error_kind", "response_write")
			}
		}
	})
}
