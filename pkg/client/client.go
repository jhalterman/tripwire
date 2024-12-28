package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/timeout"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"tripwire/pkg/metrics"
	"tripwire/pkg/server"
)

type Config struct {
	Workloads   []*Workload `yaml:"workloads"` // workloads run in parallel
	Stages      []*Stage    `yaml:"stages"`    // stages run in sequence
	MaxDuration time.Duration
}

type Workload struct {
	Name         string               `yaml:"name"`
	RPS          uint                 `yaml:"rps"`
	ServiceTimes WeightedServiceTimes `yaml:"service_times"`
	WeightSum    int
}

type Stage struct {
	Duration     time.Duration        `yaml:"duration"`
	RPS          uint                 `yaml:"rps"`           // can be carried over from the previous stage
	ServiceTimes WeightedServiceTimes `yaml:"service_times"` // can be carried over from the previous stage
	WeightSum    int
}

func (s *Stage) String() string {
	return fmt.Sprintf("RPS: %d, Duration: %ds, ServiceTimes: %s", s.RPS, int(s.Duration.Seconds()), s.ServiceTimes.String())
}

type WeightedServiceTime struct {
	ServiceTime time.Duration `yaml:"service_time"`
	Weight      uint          `yaml:"weight"`
}

func (w *WeightedServiceTime) String() string {
	return fmt.Sprintf(`{ServiceTime: %dms, Weight: %d}`, int(w.ServiceTime.Milliseconds()), w.Weight)
}

func (w *WeightedServiceTime) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type alias WeightedServiceTime
	raw := alias{
		Weight: 1,
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	*w = WeightedServiceTime(raw)
	return nil
}

type WeightedServiceTimes []*WeightedServiceTime

func (w WeightedServiceTimes) Sum() uint {
	sum := uint(0)
	for _, ld := range w {
		sum += ld.Weight
	}
	return sum
}

// Random selects a random service time based on the weightSum.
func (w WeightedServiceTimes) Random(weightSum int) time.Duration {
	return w.Weighted(rand.Intn(weightSum))
}

func (w WeightedServiceTimes) Weighted(weight int) time.Duration {
	for _, wl := range w {
		weight -= int(wl.Weight)
		if weight < 0 {
			return wl.ServiceTime
		}
	}
	return 0
}

func (w WeightedServiceTimes) String() string {
	var result string
	for _, v := range w {
		result += v.String() + ", "
	}
	if len(result) > 0 {
		result = "[" + result[:len(result)-2] + "]"
	} else {
		result = "[]"
	}
	return result
}

type Client struct {
	serverAddr string
	metrics    *metrics.StrategyMetrics
	logger     *zap.SugaredLogger
	httpClient *http.Client
	adaptive   bool

	mtx           sync.RWMutex
	config        *Config // Workloads is guarded by mtx
	cancelStageFn func()  // Guarded by mtx
}

func NewClient(serverAddr net.Addr, config *Config, metrics *metrics.StrategyMetrics, executor failsafe.Executor[*http.Response], logger *zap.SugaredLogger) *Client {
	return &Client{
		serverAddr: fmt.Sprintf("http://localhost:%d", serverAddr.(*net.TCPAddr).Port),
		config:     config,
		metrics:    metrics,
		logger:     logger.With("runID", metrics.RunID),
		httpClient: &http.Client{Transport: failsafehttp.NewRoundTripperWithExecutor(http.DefaultTransport, executor)},
	}
}

func (c *Client) Start(wg *sync.WaitGroup) {
	defer wg.Done()

	c.metrics.ClientReqTimeouts.Add(0)

	if c.config.Workloads != nil {
		for {
			ctx, cancelFn := context.WithCancel(context.Background())
			c.mtx.Lock()
			c.cancelStageFn = cancelFn
			c.mtx.Unlock()
			c.mtx.RLock()
			for _, workload := range c.config.Workloads {
				go c.performWorkload(ctx, workload)
			}
			c.mtx.RUnlock()
			select {
			case <-ctx.Done():
			}
		}
	} else if c.config.Stages != nil {
		for _, stage := range c.config.Stages {
			c.performStage(stage)
		}

		c.logger.Infow("client stages finished")
	}

	c.metrics.ClientExpectedRps.Set(0)
}

func (c *Client) performWorkload(ctx context.Context, workload *Workload) {
	c.logger.Infow("starting client workload", "workload", workload)
	ticker := time.NewTicker(time.Second / time.Duration(workload.RPS))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.metrics.ClientExpectedRps.Set(float64(workload.RPS))
			go c.sendRequest(workload.ServiceTimes.Random(workload.WeightSum))
		}
	}
}

func (c *Client) performStage(stage *Stage) {
	c.logger.Infow("starting client stage", "stage", stage)
	duration := time.After(stage.Duration)
	ticker := time.NewTicker(time.Second / time.Duration(stage.RPS))
	defer ticker.Stop()
	for {
		select {
		case <-duration:
			return
		case <-ticker.C:
			c.metrics.ClientExpectedRps.Set(float64(stage.RPS))
			go c.sendRequest(stage.ServiceTimes.Random(stage.WeightSum))
		}
	}
}

func (c *Client) sendRequest(serviceTime time.Duration) {
	start := time.Now()
	request := server.Request{ServiceTime: serviceTime}
	reqBody, err := yaml.Marshal(&request)
	if err != nil {
		c.logger.Fatalw("error marshalling YAML", "error", err)
		return
	}

	req, err := http.NewRequest("POST", c.serverAddr, bytes.NewBuffer(reqBody))
	if err != nil {
		c.logger.Errorw("error creating request", "error", err)
		return
	}
	req.Close = true

	c.metrics.ClientReqTotal.Inc()
	resp, err := c.httpClient.Do(req)

	// Handle errors
	if err != nil {
		// Handle rejections
		if errors.Is(err, ratelimiter.ErrExceeded) || errors.Is(err, adaptivelimiter.ErrExceeded) || errors.Is(err, bulkhead.ErrFull) || errors.Is(err, circuitbreaker.ErrOpen) {
			// Do not record response time for rejected requests
			c.metrics.ClientReqRejected.Inc()
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
			c.metrics.ClientReqSuccesses.Inc()
			return
		case http.StatusTooManyRequests:
			// Do not record response time for rejected requests
			c.metrics.ClientReqRejected.Inc()
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

func (c *Client) UpdateWorkloads(workloads []*Workload) {
	c.mtx.Lock()
	c.config.Workloads = workloads
	c.cancelStageFn()
	c.mtx.Unlock()
}

func (c *Client) recordResponseTime(start time.Time) {
	responseTime := time.Since(start)
	c.metrics.ClientReqResponseTimes.Observe(responseTime.Seconds())
}
