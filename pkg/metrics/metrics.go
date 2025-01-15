package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"tripwire/pkg/util"
)

type Metrics struct {
	*util.Server

	// Run metrics for things that must be distinguishable in the scenario result table
	ClientReqTotal         *prometheus.CounterVec
	ClientReqSuccesses     *prometheus.CounterVec
	ClientReqRejected      *prometheus.CounterVec
	ClientReqResponseTimes *prometheus.HistogramVec
	RunDuration            *prometheus.GaugeVec

	// Client metrics
	ClientReqFailures *prometheus.CounterVec
	ClientExpectedRps *prometheus.GaugeVec
	ClientReqTimeouts *prometheus.CounterVec

	// Server metrics
	ServerThreads          prometheus.Gauge
	ServerServiceTime      *prometheus.GaugeVec
	ServerInflightRequests *prometheus.GaugeVec

	// Policy metrics
	MinTimeout          *prometheus.GaugeVec
	RateLimit           *prometheus.GaugeVec
	ConcurrencyLimit    *prometheus.GaugeVec
	CircuitbreakerOpen  *prometheus.GaugeVec
	ThrottleProbability *prometheus.GaugeVec
	QueuedRequests      *prometheus.GaugeVec
}

func New(logger *zap.SugaredLogger) *Metrics {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return &Metrics{
		Server: util.NewServer(mux, 8080, logger),

		// Run metrics
		RunDuration: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "run_duration"},
			[]string{"run_id", "strategy"},
		),

		// Client metrics
		ClientReqTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_total"},
			[]string{"run_id", "workload", "strategy"},
		),
		ClientReqSuccesses: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_successes"},
			[]string{"run_id", "workload", "strategy"},
		),
		ClientReqRejected: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_rejected"},
			[]string{"run_id", "workload", "strategy"},
		),
		ClientReqResponseTimes: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:                            "client_req_response_times",
				NativeHistogramBucketFactor:     1.1,
				NativeHistogramMaxBucketNumber:  100,
				NativeHistogramMinResetDuration: 1 * time.Hour,
			},
			[]string{"run_id", "workload", "strategy"},
		),
		ClientReqFailures: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_failures"},
			[]string{"workload", "strategy"},
		),
		ClientExpectedRps: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "client_expected_rps"},
			[]string{"workload", "strategy"},
		),
		ClientReqTimeouts: promauto.NewCounterVec(
			prometheus.CounterOpts{Name: "client_req_timeouts"},
			[]string{"workload", "strategy"},
		),

		// Server metrics
		ServerThreads: promauto.NewGauge(
			prometheus.GaugeOpts{Name: "server_threads"},
		),
		ServerServiceTime: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "server_service_time"},
			[]string{"strategy"},
		),
		ServerInflightRequests: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "server_inflight_requests"},
			[]string{"strategy"},
		),

		// Policy metrics
		MinTimeout: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "min_timeout"},
			[]string{"strategy"},
		),
		RateLimit: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "rate_limit"},
			[]string{"strategy"},
		),
		ConcurrencyLimit: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "concurrency_limit"},
			[]string{"strategy"},
		),
		ThrottleProbability: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "throttle_probability"},
			[]string{"strategy"},
		),
		QueuedRequests: promauto.NewGaugeVec(
			prometheus.GaugeOpts{Name: "queued_requests"},
			[]string{"strategy"},
		),
	}
}

type WorkloadMetrics struct {
	RunID     string
	Labels    prometheus.Labels
	RunLabels prometheus.Labels

	// Client metrics
	ClientReqTotal         prometheus.Counter
	ClientReqSuccesses     prometheus.Counter
	ClientReqRejected      prometheus.Counter
	ClientReqResponseTimes prometheus.Observer
	ClientReqFailures      prometheus.Counter
	ClientExpectedRps      prometheus.Gauge
	ClientReqTimeouts      prometheus.Counter
}

func (m *Metrics) WithWorkload(runID string, workload string, strategy string) *WorkloadMetrics {
	labels := prometheus.Labels{"workload": workload, "strategy": strategy}
	runLabels := prometheus.Labels{"run_id": runID, "workload": workload, "strategy": strategy}

	return &WorkloadMetrics{
		RunID:     runID,
		Labels:    labels,
		RunLabels: runLabels,

		// Client metrics
		ClientReqTotal:         m.ClientReqTotal.With(runLabels),
		ClientReqSuccesses:     m.ClientReqSuccesses.With(runLabels),
		ClientReqRejected:      m.ClientReqRejected.With(runLabels),
		ClientReqResponseTimes: m.ClientReqResponseTimes.With(runLabels),
		ClientReqFailures:      m.ClientReqFailures.With(labels),
		ClientExpectedRps:      m.ClientExpectedRps.With(labels),
		ClientReqTimeouts:      m.ClientReqTimeouts.With(labels),
	}
}

type StrategyMetrics struct {
	RunID     string
	Labels    prometheus.Labels
	RunLabels prometheus.Labels

	// Run metrics for things that must be distinguishable in the scenario result table
	RunDuration prometheus.Gauge

	// Server metrics
	ServerThreads          prometheus.Gauge
	ServerServiceTime      prometheus.Gauge
	ServerInflightRequests prometheus.Gauge

	// Policy metrics
	MinTimeout          prometheus.Gauge
	RateLimit           prometheus.Gauge
	ConcurrencyLimit    prometheus.Gauge
	CircuitbreakerOpen  prometheus.Gauge
	ThrottleProbability prometheus.Gauge
	QueuedRequests      prometheus.Gauge
}

func (m *Metrics) WithStrategy(runID string, strategy string) *StrategyMetrics {
	labels := prometheus.Labels{"strategy": strategy}
	runLabels := prometheus.Labels{"run_id": runID, "strategy": strategy}

	return &StrategyMetrics{
		RunID:     runID,
		Labels:    labels,
		RunLabels: runLabels,

		// Run metrics
		RunDuration: m.RunDuration.With(runLabels),

		// Server metrics
		ServerThreads:          m.ServerThreads,
		ServerServiceTime:      m.ServerServiceTime.With(labels),
		ServerInflightRequests: m.ServerInflightRequests.With(labels),

		// Policy metrics
		MinTimeout:          m.MinTimeout.With(labels),
		RateLimit:           m.RateLimit.With(labels),
		ConcurrencyLimit:    m.ConcurrencyLimit.With(labels),
		ThrottleProbability: m.ThrottleProbability.With(labels),
		QueuedRequests:      m.QueuedRequests.With(labels),
	}
}
