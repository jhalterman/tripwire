package metrics

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type Metrics struct {
	logger *zap.SugaredLogger
	server *http.Server

	// Run metrics for things that must be distinguishable in the scenario result table
	RunID                  string
	RunLabels              prometheus.Labels
	ClientReqTotal         *prometheus.CounterVec
	ClientReqSuccesses     *prometheus.CounterVec
	ClientReqRejected      *prometheus.CounterVec
	ClientReqResponseTimes *prometheus.HistogramVec
	RunDuration            *prometheus.GaugeVec

	// Client metrics
	ClientReqFailures          prometheus.Counter
	ClientExpectedRps          prometheus.Gauge
	ClientAvgStageResponseTime prometheus.Gauge
	ClientReqTimeouts          prometheus.Counter

	// Server metrics
	ServerThreads        prometheus.Gauge
	ServerServiceTime    prometheus.Gauge
	ServerActiveRequests prometheus.Gauge

	// Policy metrics
	MinTimeout          prometheus.Gauge
	RateLimit           prometheus.Gauge
	ConcurrencyLimit    prometheus.Gauge
	CircuitbreakerOpen  prometheus.Gauge
	ThrottleProbability prometheus.Gauge
	QueuedRequests      prometheus.Gauge
}

func NewMetrics(logger *zap.SugaredLogger) *Metrics {
	http.Handle("/metrics", promhttp.Handler())
	return &Metrics{
		logger: logger,

		// Run metrics
		ClientReqTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_total"},
			[]string{"run_id"},
		),
		ClientReqSuccesses: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_successes"},
			[]string{"run_id"},
		),
		ClientReqRejected: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_rejected"},
			[]string{"run_id"},
		),
		ClientReqResponseTimes: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:                            "client_req_response_times",
				NativeHistogramBucketFactor:     1.1,
				NativeHistogramMaxBucketNumber:  100,
				NativeHistogramMinResetDuration: 1 * time.Hour,
			},
			[]string{"run_id"},
		),
		RunDuration: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "run_duration"},
			[]string{"run_id"},
		),

		// Client metrics
		ClientReqFailures: promauto.NewCounter(
			prometheus.CounterOpts{Name: "client_req_failures"},
		),
		ClientExpectedRps: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "client_expected_rps"},
		),
		ClientAvgStageResponseTime: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "client_avg_stage_response_time"},
		),
		ClientReqTimeouts: promauto.NewCounter(
			prometheus.CounterOpts{Name: "client_req_timeouts"},
		),

		// Server metrics
		ServerThreads: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "server_threads"},
		),
		ServerServiceTime: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "server_service_time"},
		),
		ServerActiveRequests: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "server_active_requests"},
		),

		// Policy metrics
		MinTimeout: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "min_timeout"},
		),
		RateLimit: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "rate_limit"},
		),
		ConcurrencyLimit: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "concurrency_limit"},
		),
		ThrottleProbability: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "throttle_probability"},
		),
		QueuedRequests: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "queued_requests"},
		),
	}
}

func (m *Metrics) Reset() {
	m.RateLimit.Set(math.NaN())
	m.ConcurrencyLimit.Set(math.NaN())
	m.ThrottleProbability.Set(math.NaN())
}

func (m *Metrics) Start(runID string, runDuration time.Duration) {
	m.server = &http.Server{
		Addr: ":8080",
	}
	m.RunID = runID
	m.RunLabels = prometheus.Labels{"run_id": runID}
	m.RunDuration.With(m.RunLabels).Set(runDuration.Seconds())
	go func() {
		if err := m.server.ListenAndServe(); err != nil {
			m.logger.Info(err)
		}
	}()
}

func (m *Metrics) Shutdown() {
	if err := m.server.Shutdown(context.Background()); err != nil {
		m.logger.Fatal(err)
	}
}
