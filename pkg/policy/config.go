package policy

import (
	"time"
)

type Config struct {
	Timeout                time.Duration `yaml:"timeout"`
	*RateLimiterConfig     `yaml:"ratelimiter"`
	*BulkheadConfig        `yaml:"bulkhead"`
	*CircuitBreakerConfig  `yaml:"circuitbreaker"`
	*AdaptiveLimiterConfig `yaml:"adaptivelimiter"`
	*VegasLimiter2Config   `yaml:"vegaslimiter2"`
	*VegasConfig           `yaml:"vegaslimiter"`
	*GradientConfig        `yaml:"gradientlimiter"`
	*Gradient2Config       `yaml:"gradient2limiter"`
}

type RateLimiterType int

const (
	Smooth RateLimiterType = iota
	Bursty RateLimiterType = iota
)

// See https://failsafe-go.dev/rate-limiter/ for details on how rate limiters work.
// See https://pkg.go.dev/github.com/failsafe-go/failsafe-go/ratelimiter#RateLimiterBuilder for details on how rate limiters are configured.
type RateLimiterConfig struct {
	Type        RateLimiterType `yaml:"type"`
	RPS         uint            `yaml:"rps"`
	MaxWaitTime time.Duration   `yaml:"max_wait_time"`
}

// See https://failsafe-go.dev/bulkhead/ for details on how bulkheads work.
// See https://pkg.go.dev/github.com/failsafe-go/failsafe-go/bulkhead#BulkheadBuilder for details on how bulkheads are configured.
type BulkheadConfig struct {
	MaxConcurrency uint          `yaml:"max_concurrency"`
	MaxWaitTime    time.Duration `yaml:"max_wait_time"`
}

// See https://failsafe-go.dev/circuit-breaker/ for details on how circuit breakers work.
// See https://pkg.go.dev/github.com/failsafe-go/failsafe-go/circuitbreaker#CircuitBreakerBuilder for details on how Failsafe-go circuit breakers are configured.
type CircuitBreakerConfig struct {
	Delay time.Duration `yaml:"delay"`

	FailureThreshold            uint          `yaml:"failure_threshold"`
	FailureRateThreshold        uint          `yaml:"failure_rate_threshold"`
	FailureThresholdingCapacity uint          `yaml:"failure_thresholding_capacity"`
	FailureExecutionThreshold   uint          `yaml:"failure_execution_threshold"`
	FailureThresholdingPeriod   time.Duration `yaml:"failure_thresholding_period"`

	SuccessThreshold            uint `yaml:"success_threshold"`
	SuccessThresholdingCapacity uint `yaml:"success_thresholding_capacity"`
}

type AdaptiveLimiterConfig struct {
	ShortWindowMinDuration time.Duration `yaml:"short_window_min_duration"`
	ShortWindowMaxDuration time.Duration `yaml:"short_window_max_duration"`
	ShortWindowMinSamples  uint          `yaml:"short_window_min_samples"`
	LongWindowSize         uint          `yaml:"long_window_size"`
	MinLimit               uint          `yaml:"min_limit"`
	MaxLimit               uint          `yaml:"max_limit"`
	InitialLimit           uint          `yaml:"initial_limit"`
	MaxLimitFactor         float32       `yaml:"max_limit_factor"`
	CorrelationWindowSize  uint          `yaml:"correlation_window_size"`
	InitialRejectionFactor float32       `yaml:"initial_rejection_factor"`
	MaxRejectionFactor     float32       `yaml:"max_rejection_factor"`
}

type VegasLimiter2Config struct {
	ShortWindowMinDuration time.Duration `yaml:"short_window_min_duration"`
	ShortWindowMaxDuration time.Duration `yaml:"short_window_max_duration"`
	ShortWindowMinSamples  uint          `yaml:"short_window_min_samples"`
	LongWindowSize         uint          `yaml:"long_window_size"`
	MinLimit               uint          `yaml:"min_limit"`
	MaxLimit               uint          `yaml:"max_limit"`
	InitialLimit           uint          `yaml:"initial_limit"`
	MaxLimitFactor         float32       `yaml:"max_limit_factor"`
	CorrelationWindowSize  uint          `yaml:"correlation_window_size"`
	InitialRejectionFactor float32       `yaml:"initial_rejection_factor"`
	MaxRejectionFactor     float32       `yaml:"max_rejection_factor"`
}

// See https://pkg.go.dev/github.com/platinummonkey/go-concurrency-limits@v0.8.0/limit#VegasLimit for details on how the Vegas limit works.
type VegasConfig struct {
	ShortWindowMinDuration time.Duration `yaml:"short_window_min_duration"`
	ShortWindowMaxDuration time.Duration `yaml:"short_window_max_duration"`
	ShortWindowMinSamples  uint          `yaml:"short_window_min_samples"`
	MaxLimit               uint          `yaml:"max_limit"`
	InitialLimit           uint          `yaml:"initial_limit"`
	SmoothingFactor        float32       `yaml:"smoothing_factor"`
}

// See https://pkg.go.dev/github.com/platinummonkey/go-concurrency-limits@v0.8.0/limit#GradientLimit for details on how the gradient limit works.
type GradientConfig struct {
	ShortWindowMinDuration time.Duration `yaml:"short_window_min_duration"`
	ShortWindowMaxDuration time.Duration `yaml:"short_window_max_duration"`
	ShortWindowMinSamples  uint          `yaml:"short_window_min_samples"`
	MinLimit               uint          `yaml:"min_limit"`
	MaxLimit               uint          `yaml:"max_limit"`
	InitialLimit           uint          `yaml:"initial_limit"`
	SmoothingFactor        float32       `yaml:"smoothing_factor"`
}

// See https://pkg.go.dev/github.com/platinummonkey/go-concurrency-limits@v0.8.0/limit#Gradient2Limit for details on how the gradient2 limit works.
type Gradient2Config struct {
	ShortWindowMinDuration time.Duration `yaml:"short_window_min_duration"`
	ShortWindowMaxDuration time.Duration `yaml:"short_window_max_duration"`
	ShortWindowMinSamples  uint          `yaml:"short_window_min_samples"`
	LongWindowSize         uint          `yaml:"long_window_size"`
	MinLimit               uint          `yaml:"min_limit"`
	MaxLimit               uint          `yaml:"max_limit"`
	InitialLimit           uint          `yaml:"initial_limit"`
	SmoothingFactor        float32       `yaml:"smoothing_factor"`
}
