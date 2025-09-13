package policy

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/adaptivethrottler"
	"github.com/failsafe-go/failsafe-go/priority"

	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"

	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/timeout"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"gopkg.in/yaml.v3"

	"tripwire/pkg/client"
	"tripwire/pkg/metrics"
)

type Configs []*Config

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	type Alias Config
	tmp := (*Alias)(c)
	return value.Decode(tmp)
}

func (c *Config) ToPolicy(metrics *metrics.Metrics, strategyMetrics *metrics.StrategyMetrics, limiterPrioritizer priority.Prioritizer, throttlerPrioritizer priority.Prioritizer, workload, strategy string, logger *zap.Logger) failsafe.Policy[*http.Response] {
	slogger := slog.New(zapslog.NewHandler(logger.Core()))
	limitChangedListener := func(e adaptivelimiter.LimitChangedEvent) {
		metrics.WithConcurrencyLimit(workload, strategy).Set(float64(e.NewLimit))
	}

	if c.Timeout != 0 {
		return timeout.New[*http.Response](c.Timeout)
	} else if c.RateLimiterConfig != nil {
		pc := c.RateLimiterConfig
		strategyMetrics.RateLimit.Set(float64(pc.RPS))
		switch pc.Type {
		case Bursty:
			return ratelimiter.NewBurstyBuilder[*http.Response](pc.RPS, time.Second).
				WithMaxWaitTime(pc.MaxWaitTime).
				Build()
		case Smooth:
			fallthrough
		default:
			return ratelimiter.NewSmoothBuilder[*http.Response](pc.RPS, time.Second).
				WithMaxWaitTime(pc.MaxWaitTime).
				Build()
		}
	} else if c.BulkheadConfig != nil {
		pc := c.BulkheadConfig
		metrics.WithConcurrencyLimit(workload, strategy).Set(float64(pc.MaxConcurrency))
		return bulkhead.NewBuilder[*http.Response](pc.MaxConcurrency).
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
		return builder.WithDelay(pc.Delay).
			WithSuccessThresholdRatio(pc.SuccessThreshold, pc.SuccessThresholdingCapacity).
			OnOpen(func(event circuitbreaker.StateChangedEvent) {
				metrics.WithThrottleProbability(workload, strategy).Set(1)
			}).
			OnClose(func(event circuitbreaker.StateChangedEvent) {
				metrics.WithThrottleProbability(workload, strategy).Set(0)
			}).
			Build()
	} else if c.AdaptiveLimiterConfig != nil {
		lc := c.AdaptiveLimiterConfig
		metrics.WithConcurrencyLimit(workload, strategy).Set(float64(lc.InitialLimit))
		// log := slog.New(zapslog.NewHandler(logger.Core()))
		builder := adaptivelimiter.NewBuilder[*http.Response]().
			WithLimits(lc.MinLimit, lc.MaxLimit, lc.InitialLimit).
			WithMaxLimitFactor(lc.MaxLimitFactor).
			WithRecentWindow(lc.RecentWindowMinDuration, lc.RecentWindowMaxDuration, lc.RecentWindowMinSamples).
			WithRecentQuantile(lc.RecentQuantile).
			WithBaselineWindow(lc.BaselineWindowAge).
			WithCorrelationWindow(lc.CorrelationWindowSize).
			//WithLogger(log).
			OnLimitChanged(func(e adaptivelimiter.LimitChangedEvent) {
				metrics.WithConcurrencyLimit(workload, strategy).Set(float64(e.NewLimit))
			})
		if lc.InitialRejectionFactor > 0 && lc.MaxRejectionFactor > 0 {
			builder.WithQueueing(lc.InitialRejectionFactor, lc.MaxRejectionFactor)
		}
		if limiterPrioritizer != nil {
			return builder.
				// WithLogger(log.With("workload", workload)).
				BuildPrioritized(limiterPrioritizer)
		} else {
			return builder.Build()
		}
	} else if c.AdaptiveThrottlerConfig != nil {
		tc := c.AdaptiveThrottlerConfig
		builder := adaptivethrottler.NewBuilder[*http.Response]().
			WithFailureRateThreshold(tc.FailureRateThreshold, tc.ThresholdingPeriod).
			WithMaxRejectionRate(tc.MaxRejectionRate)
		if throttlerPrioritizer != nil {
			return builder.
				// WithLogger(log.With("workload", workload)).
				BuildPrioritized(throttlerPrioritizer)
		} else {
			return builder.Build()
		}
	} else if c.VegasConfig != nil {
		metrics.WithConcurrencyLimit(workload, strategy).Set(float64(c.VegasConfig.InitialLimit))
		return c.VegasConfig.Build(slogger, limitChangedListener)
	} else if c.GradientConfig != nil {
		metrics.WithConcurrencyLimit(workload, strategy).Set(float64(c.GradientConfig.InitialLimit))
		return c.GradientConfig.Build(slogger, limitChangedListener)
	} else if c.Gradient2Config != nil {
		metrics.WithConcurrencyLimit(workload, strategy).Set(float64(c.Gradient2Config.InitialLimit))
		return c.Gradient2Config.Build(slogger, limitChangedListener)
	}

	return nil
}

func (c Configs) ToExecutors(strategy string, shareStrategies bool, stages []*client.Stage, workloads []*client.Workload, metrics *metrics.Metrics, strategyMetrics *metrics.StrategyMetrics, limiterPrioritizer priority.Prioritizer, throttlerPrioritizer priority.Prioritizer, logger *zap.Logger) (map[string]failsafe.Executor[*http.Response], time.Duration) {
	var minTimeout time.Duration
	var onDoneFuncs []func()
	workloadExecutors := make(map[string]failsafe.Executor[*http.Response])

	buildPolicies := func(name string) []failsafe.Policy[*http.Response] {
		metrics.WithThrottleProbability(name, strategy).Set(0)

		var policies []failsafe.Policy[*http.Response]
		for _, config := range c {
			policy := config.ToPolicy(metrics, strategyMetrics, limiterPrioritizer, throttlerPrioritizer, name, strategy, logger)
			policies = append(policies, policy)

			if config.Timeout != 0 {
				policyTimeout := config.Timeout
				if minTimeout == 0 {
					minTimeout = policyTimeout
				} else {
					minTimeout = min(minTimeout, policyTimeout)
				}
			} else if config.AdaptiveLimiterConfig != nil {
				onDoneFuncs = append(onDoneFuncs, func() {
					p := policy.(adaptivelimiter.Metrics)
					metrics.WithConcurrencyLimit(name, strategy).Set(float64(p.Limit()))
					metrics.WithQueueWorkload(name, strategy).Set(float64(p.Queued()))
				})
			} else if config.AdaptiveThrottlerConfig != nil {
				onDoneFuncs = append(onDoneFuncs, func() {
					p := policy.(adaptivethrottler.Metrics)
					metrics.WithThrottleProbability(name, strategy).Set(p.RejectionRate())
				})
			}
		}
		return policies
	}

	buildWorkloads := func(workload string, policies []failsafe.Policy[*http.Response]) {
		workloadExecutors[workload] = failsafe.NewExecutor(policies...).OnDone(func(e failsafe.ExecutionDoneEvent[*http.Response]) {
			for _, onDoneFunc := range onDoneFuncs {
				onDoneFunc()
			}
		})
	}

	if len(stages) > 0 {
		buildWorkloads("staged", buildPolicies("staged"))
	} else {
		if shareStrategies {
			policies := buildPolicies("shared")
			for _, workload := range workloads {
				buildWorkloads(workload.Name, policies)
			}
		} else {
			for _, workload := range workloads {
				buildWorkloads(workload.Name, buildPolicies(workload.Name))
			}
		}
	}

	return workloadExecutors, minTimeout
}
