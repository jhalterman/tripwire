package policy

import (
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/timeout"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"gopkg.in/yaml.v3"

	"tripwire/pkg/metrics"
)

type Configs []*Config

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	type Alias Config
	tmp := (*Alias)(c)
	return value.Decode(tmp)
}

func (c *Config) ToPolicy(metrics *metrics.StrategyMetrics, logger *zap.Logger) (failsafe.Policy[*http.Response], time.Duration) {
	var policy failsafe.Policy[*http.Response]
	var timeOut time.Duration
	slogger := slog.New(zapslog.NewHandler(logger.Core()))
	limitChangedListener := func(e adaptivelimiter.LimitChangedEvent) {
		metrics.ConcurrencyLimit.Set(float64(e.NewLimit))
	}

	if c.Timeout != 0 {
		policy = timeout.New[*http.Response](c.Timeout)
		timeOut = c.Timeout
	} else if c.RateLimiterConfig != nil {
		pc := c.RateLimiterConfig
		metrics.RateLimit.Set(float64(pc.RPS))
		switch pc.Type {
		case Bursty:
			policy = ratelimiter.NewBurstyBuilder[*http.Response](pc.RPS, time.Second).
				WithMaxWaitTime(pc.MaxWaitTime).
				Build()
		case Smooth:
			fallthrough
		default:
			policy = ratelimiter.NewSmoothBuilder[*http.Response](pc.RPS, time.Second).
				WithMaxWaitTime(pc.MaxWaitTime).
				Build()
		}
	} else if c.BulkheadConfig != nil {
		pc := c.BulkheadConfig
		metrics.ConcurrencyLimit.Set(float64(pc.MaxConcurrency))
		policy = bulkhead.NewBuilder[*http.Response](pc.MaxConcurrency).
			WithMaxWaitTime(pc.MaxWaitTime).
			Build()
	} else if c.CircuitBreakerConfig != nil {
		pc := c.CircuitBreakerConfig
		builder := circuitbreaker.NewBuilder[*http.Response]()
		if pc.FailureThresholdingCapacity == 0 && pc.FailureThresholdingPeriod == 0 {
			builder.WithFailureThreshold(pc.FailureThreshold)
		} else if pc.FailureThresholdingCapacity != 0 && pc.FailureThresholdingPeriod == 0 {
			builder.WithFailureThresholdRatio(pc.FailureThreshold, pc.FailureThresholdingCapacity)
		} else if pc.FailureThresholdingPeriod != 0 && pc.FailureRateThreshold == 0 {
			builder.WithFailureThresholdPeriod(pc.FailureThreshold, pc.FailureThresholdingPeriod)
		} else if pc.FailureThresholdingPeriod != 0 && pc.FailureRateThreshold != 0 {
			builder.WithFailureRateThreshold(pc.FailureRateThreshold, pc.FailureExecutionThreshold, pc.FailureThresholdingPeriod)
		}
		policy = builder.WithDelay(pc.Delay).
			WithSuccessThresholdRatio(pc.SuccessThreshold, pc.SuccessThresholdingCapacity).
			OnOpen(func(event circuitbreaker.StateChangedEvent) {
				metrics.ThrottleProbability.Set(1)
			}).
			OnClose(func(event circuitbreaker.StateChangedEvent) {
				metrics.ThrottleProbability.Set(0)
			}).
			Build()
	} else if c.AdaptiveLimiterConfig != nil {
		pc := c.AdaptiveLimiterConfig
		metrics.ConcurrencyLimit.Set(float64(pc.InitialLimit))
		policy = adaptivelimiter.NewBuilder[*http.Response]().
			WithShortWindow(pc.ShortWindowMinDuration, pc.ShortWindowMaxDuration, pc.ShortWindowMinSamples).
			WithLongWindow(pc.LongWindowSize).
			WithLimits(pc.MinLimit, pc.MaxLimit, pc.InitialLimit).
			WithMaxLimitFactor(pc.MaxLimitFactor).
			WithMaxExecutionTime(pc.MaxExecutionTime).
			WithCorrelationWindow(pc.CorrelationWindowSize).
			WithVariationWindow(pc.VariationWindowSize).
			WithSmoothing(pc.SmoothingFactor).
			WithLogger(slog.New(zapslog.NewHandler(logger.Core()))).
			OnLimitChanged(func(e adaptivelimiter.LimitChangedEvent) {
				metrics.ConcurrencyLimit.Set(float64(e.NewLimit))
			}).
			Build()
	} else if c.VegasConfig != nil {
		metrics.ConcurrencyLimit.Set(float64(c.VegasConfig.InitialLimit))
		policy = c.VegasConfig.Build(slogger, limitChangedListener)
	} else if c.GradientConfig != nil {
		metrics.ConcurrencyLimit.Set(float64(c.GradientConfig.InitialLimit))
		policy = c.GradientConfig.Build(slogger, limitChangedListener)
	} else if c.Gradient2Config != nil {
		metrics.ConcurrencyLimit.Set(float64(c.Gradient2Config.InitialLimit))
		policy = c.Gradient2Config.Build(slogger, limitChangedListener)
	}

	return policy, timeOut
}

func (c Configs) ToExecutor(metrics *metrics.StrategyMetrics, logger *zap.Logger) (failsafe.Executor[*http.Response], time.Duration) {
	var minTimeout time.Duration
	var policies []failsafe.Policy[*http.Response]
	for _, config := range c {
		policy, policyTimeout := config.ToPolicy(metrics, logger)
		if policyTimeout != 0 {
			if minTimeout == 0 {
				minTimeout = policyTimeout
			} else {
				minTimeout = min(minTimeout, policyTimeout)
			}
		}
		policies = append(policies, policy)
	}

	executor := failsafe.NewExecutor(policies...)
	for _, policy := range policies {
		adaptiveLimiterType := reflect.TypeOf((*adaptivelimiter.AdaptiveLimiter[*http.Response])(nil)).Elem()
		if reflect.TypeOf(policy).Implements(adaptiveLimiterType) {
			p := policy.(adaptivelimiter.AdaptiveLimiter[*http.Response])
			executor = executor.OnDone(func(e failsafe.ExecutionDoneEvent[*http.Response]) {
				metrics.QueuedRequests.Set(float64(p.Blocked()))
			})
		}
	}

	return executor, minTimeout
}
