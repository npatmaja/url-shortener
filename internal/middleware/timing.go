package middleware

import (
	"net/http"
	"strconv"
	"time"
)

// Timing is a middleware that adds X-Processing-Time-Micros header to all responses.
// The header value is the time taken to process the request in microseconds.
func Timing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &timingResponseWriter{
			ResponseWriter: w,
			start:          start,
		}

		next.ServeHTTP(wrapped, r)
	})
}

type timingResponseWriter struct {
	http.ResponseWriter
	start       time.Time
	wroteHeader bool
}

func (w *timingResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		micros := time.Since(w.start).Microseconds()
		w.Header().Set("X-Processing-Time-Micros", strconv.FormatInt(micros, 10))
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *timingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
