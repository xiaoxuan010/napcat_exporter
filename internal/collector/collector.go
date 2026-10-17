// Package collector exposes metrics built from a single request's status result.
package collector

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
	"github.com/xiaoxuan010/napcat_exporter/internal/napcat"
)

type Source interface {
	Collect(context.Context) napcat.Result
}
type Build struct{ Version, Revision string }

type Handler struct {
	source      Source
	logger      *slog.Logger
	self        *prometheus.Registry
	slots       chan struct{}
	mu          sync.Mutex
	lastSuccess time.Time
}

func New(source Source, logger *slog.Logger, build Build) *Handler {
	self := prometheus.NewRegistry()
	self.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	info := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: contract.BuildInfo, Help: "Build information for napcat_exporter itself; no upstream version query."}, []string{"version", "revision"})
	info.WithLabelValues(build.Version, build.Revision).Set(contract.Up)
	self.MustRegister(info)
	return &Handler{source: source, logger: logger, self: self, slots: make(chan struct{}, contract.MaxConcurrentScrapes)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "scrape concurrency limit reached", http.StatusServiceUnavailable)
		return
	}
	result := h.source.Collect(r.Context())
	h.mu.Lock()
	if result.Success && result.Completed.After(h.lastSuccess) {
		h.lastSuccess = result.Completed
	}
	last := h.lastSuccess
	h.mu.Unlock()
	if result.Success {
		h.logger.Debug("collection completed", "action", contract.Action, "duration_seconds", result.Duration.Seconds())
	} else {
		h.logger.Warn("collection failed", "action", contract.Action, "category", result.Category, "status_code", result.StatusCode, "duration_seconds", result.Duration.Seconds())
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(&snapshot{result: result, last: last})
	promhttp.HandlerFor(prometheus.Gatherers{registry, h.self}, promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(w, r)
}

var (
	apiDesc      = prometheus.NewDesc(contract.APIUp, "Whether this get_status response passed HTTP, OneBot and boolean online validation.", []string{contract.ActionLabel}, nil)
	onlineDesc   = prometheus.NewDesc(contract.Online, "QQ online state reported by NapCat get_status; omitted when unknown. Short outages between scrapes may be missed.", nil, nil)
	lastDesc     = prometheus.NewDesc(contract.LastSuccess, "Unix time of the last valid get_status response in this exporter process; omitted before first success.", nil, nil)
	durationDesc = prometheus.NewDesc(contract.Duration, "Seconds spent requesting and validating get_status for this scrape, including failures.", nil, nil)
)

type snapshot struct {
	result napcat.Result
	last   time.Time
}

func (s *snapshot) Describe(ch chan<- *prometheus.Desc) {
	ch <- apiDesc
	ch <- onlineDesc
	ch <- lastDesc
	ch <- durationDesc
}
func (s *snapshot) Collect(ch chan<- prometheus.Metric) {
	up := contract.Down
	if s.result.Success {
		up = contract.Up
		online := contract.Down
		if s.result.Online {
			online = contract.Up
		}
		ch <- prometheus.MustNewConstMetric(onlineDesc, prometheus.GaugeValue, online)
	}
	ch <- prometheus.MustNewConstMetric(apiDesc, prometheus.GaugeValue, up, contract.Action)
	ch <- prometheus.MustNewConstMetric(durationDesc, prometheus.GaugeValue, s.result.Duration.Seconds())
	if !s.last.IsZero() {
		ch <- prometheus.MustNewConstMetric(lastDesc, prometheus.GaugeValue, float64(s.last.UnixNano())/float64(time.Second))
	}
}
