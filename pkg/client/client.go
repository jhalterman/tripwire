package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/timeout"
	"go.uber.org/zap"

	"tripwire/pkg/metrics"
)

type Config struct {
	Static *Static  `yaml:"static"`
	Stages []*Stage `yaml:"stages"`
}

type Static struct {
	RPS uint `yaml:"rps"`
}

type Stage struct {
	RPS      uint          `yaml:"rps"`
	Duration time.Duration `yaml:"duration"`
}

func (s *Stage) String() string {
	return fmt.Sprintf(`{RPS: %d, Duration: %ds}`, s.RPS, int(s.Duration.Seconds()))
}

type Client struct {
	config     *Config
	metrics    *metrics.Metrics
	logger     *zap.SugaredLogger
	httpClient *http.Client
	adaptive   bool

	mtx                sync.RWMutex
	cancelStageFn      func()
	stageResponseTimes *hdrhistogram.Histogram // Guarded by mtx. Tracks response times for an individual stage.
}

func NewClient(config *Config, metrics *metrics.Metrics, executor failsafe.Executor[*http.Response], logger *zap.SugaredLogger) *Client {
	return &Client{
		config:     config,
		metrics:    metrics,
		logger:     logger.With("runID", metrics.RunID),
		httpClient: &http.Client{Transport: failsafehttp.NewRoundTripperWithExecutor(http.DefaultTransport, executor)},
	}
}

func (c *Client) Start(wg *sync.WaitGroup) {
	defer wg.Done()

	c.metrics.ClientReqTimeouts.Add(0)

	if c.config.Static != nil {
		// Run static configuration
		for {
			c.logger.Infow("starting client with static configuration", "config", c.config.Static)
			ctx, cancelFn := context.WithCancel(context.Background())
			c.cancelStageFn = cancelFn
			stage := &Stage{
				RPS:      c.config.Static.RPS,
				Duration: 24 * time.Hour,
			}
			c.performStage(ctx, stage)
		}
	} else if c.config.Stages != nil {
		// Run staged configuration
		for _, stage := range c.config.Stages {
			c.logger.Infow("starting client stage", "stage", stage)
			c.performStage(context.Background(), stage)
		}

		c.logger.Infow("client stages finished")
	}

	c.metrics.ClientExpectedRps.Set(0)
}

func (c *Client) performStage(ctx context.Context, stage *Stage) {
	c.mtx.Lock()
	c.stageResponseTimes = hdrhistogram.New(-1, 60000, 5)
	c.mtx.Unlock()

	duration := time.After(stage.Duration)
	ticker := time.NewTicker(time.Second / time.Duration(stage.RPS))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-duration:
			return
		case <-ticker.C:
			c.metrics.ClientExpectedRps.Set(float64(stage.RPS))
			go c.sendRequest()
		}
	}
}

func (c *Client) sendRequest() {
	start := time.Now()
	req, err := http.NewRequest("GET", "http://127.0.0.1:9000", nil)
	if err != nil {
		c.logger.Errorw("error creating request", "error", err)
		return
	}
	req.Close = true

	c.metrics.ClientReqTotal.With(c.metrics.RunLabels).Inc()
	resp, err := c.httpClient.Do(req)

	// Handle errors
	if err != nil {
		// Handle rejections
		if errors.Is(err, ratelimiter.ErrExceeded) || errors.Is(err, adaptivelimiter.ErrExceeded) || errors.Is(err, bulkhead.ErrFull) || errors.Is(err, circuitbreaker.ErrOpen) {
			// Do not record response time for rejected requests
			c.metrics.ClientReqRejected.With(c.metrics.RunLabels).Inc()
		}
		// Handle timeouts
		var netErr net.Error
		if errors.Is(err, timeout.ErrExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			c.recordResponseTime(start)
			c.metrics.ClientReqTimeouts.Inc()
		}
		c.metrics.ClientReqFailures.Inc()
		return
	}

	if resp != nil {
		_ = resp.Body.Close()

		// Handle responses
		switch resp.StatusCode {
		case http.StatusOK:
			c.recordResponseTime(start)
			c.metrics.ClientReqSuccesses.With(c.metrics.RunLabels).Inc()
			return
		case http.StatusTooManyRequests:
			// Do not record response time for rejected requests
			c.metrics.ClientReqRejected.With(c.metrics.RunLabels).Inc()
		case http.StatusInternalServerError:
			// Do not record response time for internal server errors
		case http.StatusRequestTimeout, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			c.recordResponseTime(start)
			c.metrics.ClientReqTimeouts.Inc()
		default:
			c.logger.Fatalw("unknown response code", "status", resp.StatusCode)
		}
	}
	c.metrics.ClientReqFailures.Inc()
}

func (c *Client) StaticConfig() *Static {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.config.Static
}

func (c *Client) UpdateStaticConfig(staticConfig *Static) {
	c.mtx.Lock()
	c.config.Static = staticConfig
	c.cancelStageFn()
	c.mtx.Unlock()
}

func (c *Client) recordResponseTime(start time.Time) {
	responseTime := time.Since(start)
	c.metrics.ClientReqResponseTimes.With(c.metrics.RunLabels).Observe(responseTime.Seconds())
	c.stageResponseTimes.RecordValue(responseTime.Milliseconds())
	mean := float64(c.stageResponseTimes.Mean()) / 1000
	c.metrics.ClientAvgStageResponseTime.Set(mean)
}
