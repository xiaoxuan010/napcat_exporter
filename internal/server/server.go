// Package server wires the two HTTP endpoints without upstream coupling in health checks.
package server

import (
	"net/http"

	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
)

func New(metricsPath string, metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET "+metricsPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != metricsPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		metrics.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET "+contract.HealthPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}
