package policy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/common"
	"github.com/failsafe-go/failsafe-go/policy"
	"github.com/platinummonkey/go-concurrency-limits/core"
	"github.com/platinummonkey/go-concurrency-limits/limit"
	"github.com/platinummonkey/go-concurrency-limits/limiter"
	"github.com/platinummonkey/go-concurrency-limits/strategy"
	"gopkg.in/yaml.v3"
)

func (c *GradientConfig) UnmarshalYAML(value *yaml.Node) error {
	*c = GradientConfig{
		ShortWindowMinDuration: time.Second,
		ShortWindowMaxDuration: time.Second,
		ShortWindowMinSamples:  1,
		MinLimit:               1,
		MaxLimit:               200,
		InitialLimit:           20,
		SmoothingFactor:        0.1,
	}
	type Alias GradientConfig
	var alias = Alias(*c)
	if err := value.Decode(&alias); err != nil {
		return err
	}
	*c = GradientConfig(alias)
	return nil
}

func (c *GradientConfig) Build(slogger *slog.Logger, limitChangedListener func(adaptivelimiter.LimitChangedEvent)) GclLimiter[*http.Response] {
	logger := slogLogger{slogger}
	gLimit := limit.NewGradientLimitWithRegistry("tripwire", int(c.InitialLimit), int(c.MinLimit), int(c.MaxLimit),
		float64(c.SmoothingFactor), nil, 0, -1, logger, core.EmptyMetricRegistryInstance)
	gLimit.NotifyOnChange(func(limit int) {
		limitChangedListener(adaptivelimiter.LimitChangedEvent{NewLimit: uint(limit)})
	})
	gLimiter, err := limiter.NewDefaultLimiter(gLimit, int64(c.ShortWindowMinDuration), int64(c.ShortWindowMaxDuration), 1, int(c.ShortWindowMinSamples),
		strategy.NewSimpleStrategy(int(c.InitialLimit)), logger, core.EmptyMetricRegistryInstance)
	if err != nil {
		panic("failed to create gradient limiter " + err.Error())
	}
	return &gclLimiter[*http.Response]{gLimiter}
}

func (c *Gradient2Config) UnmarshalYAML(value *yaml.Node) error {
	*c = Gradient2Config{
		ShortWindowMinDuration: time.Second,
		ShortWindowMaxDuration: time.Second,
		ShortWindowMinSamples:  10,
		LongWindowSize:         60,
		MinLimit:               1,
		MaxLimit:               200,
		InitialLimit:           20,
		SmoothingFactor:        0.1,
	}
	type Alias Gradient2Config
	var alias = Alias(*c)
	if err := value.Decode(&alias); err != nil {
		return err
	}
	*c = Gradient2Config(alias)
	return nil
}

func (c *Gradient2Config) Build(slogger *slog.Logger, limitChangedListener func(adaptivelimiter.LimitChangedEvent)) GclLimiter[*http.Response] {
	logger := slogLogger{slogger}
	gLimit, err := limit.NewGradient2Limit("tripwire", int(c.InitialLimit), int(c.MaxLimit), int(c.MinLimit), nil,
		float64(c.SmoothingFactor), int(c.LongWindowSize), logger, core.EmptyMetricRegistryInstance)
	if err != nil {
		panic("failed to create gradient2 limit " + err.Error())
	}
	gLimit.NotifyOnChange(func(limit int) {
		limitChangedListener(adaptivelimiter.LimitChangedEvent{NewLimit: uint(limit)})
	})
	gLimiter, err := limiter.NewDefaultLimiter(gLimit, int64(c.ShortWindowMinDuration), int64(c.ShortWindowMaxDuration), 1, int(c.ShortWindowMinSamples),
		strategy.NewSimpleStrategy(int(c.InitialLimit)), logger, core.EmptyMetricRegistryInstance)
	if err != nil {
		panic("failed to create gradient2 limiter " + err.Error())
	}
	return &gclLimiter[*http.Response]{gLimiter}
}

func (c *VegasConfig) UnmarshalYAML(value *yaml.Node) error {
	*c = VegasConfig{
		ShortWindowMinDuration: time.Second,
		ShortWindowMaxDuration: time.Second,
		ShortWindowMinSamples:  1,
		MaxLimit:               200,
		InitialLimit:           20,
		SmoothingFactor:        0.1,
	}
	type Alias VegasConfig
	var alias = Alias(*c)
	if err := value.Decode(&alias); err != nil {
		return err
	}
	*c = VegasConfig(alias)
	return nil
}

func (c *VegasConfig) Build(slogger *slog.Logger, limitChangedListener func(adaptivelimiter.LimitChangedEvent)) GclLimiter[*http.Response] {
	logger := slogLogger{slogger}
	vLimit := limit.NewVegasLimitWithRegistry("tripwire", int(c.InitialLimit), nil, int(c.MaxLimit), float64(c.SmoothingFactor),
		nil, nil, nil, nil, nil, -1, logger, core.EmptyMetricRegistryInstance)
	vLimit.NotifyOnChange(func(limit int) {
		limitChangedListener(adaptivelimiter.LimitChangedEvent{NewLimit: uint(limit)})
	})
	vLimiter, err := limiter.NewDefaultLimiter(vLimit, int64(c.ShortWindowMinDuration), int64(c.ShortWindowMaxDuration), 1, int(c.ShortWindowMinSamples),
		strategy.NewSimpleStrategy(int(c.InitialLimit)), logger, core.EmptyMetricRegistryInstance)
	if err != nil {
		panic("failed to create vegas limiter " + err.Error())
	}
	return &gclLimiter[*http.Response]{vLimiter}
}

// GclLimiter is a go-concurrency-limits backed limiter.
type GclLimiter[R any] interface {
	failsafe.Policy[R]
	TryAcquirePermit() (adaptivelimiter.Permit, bool)
	Limit() int
	Inflight() int
	Blocked() int
}

type gclLimiter[R any] struct {
	*limiter.DefaultLimiter
}

func (l *gclLimiter[R]) TryAcquirePermit() (adaptivelimiter.Permit, bool) {
	if listener, ok := l.Acquire(context.Background()); !ok {
		return nil, false
	} else {
		return &delegatingPermit{listener}, true
	}
}

func (l *gclLimiter[R]) Limit() int {
	return l.EstimatedLimit()
}

func (l *gclLimiter[R]) Inflight() int {
	return 0
}

func (l *gclLimiter[R]) Blocked() int {
	return 0
}

func (l *gclLimiter[R]) ToExecutor(_ R) any {
	e := &gclExecutor[R]{
		BaseExecutor: &policy.BaseExecutor[R]{},
		GclLimiter:   l,
	}
	e.Executor = e
	return e
}

type delegatingPermit struct {
	core.Listener
}

func (p *delegatingPermit) Record() {
	p.Listener.OnSuccess()
}

func (p *delegatingPermit) Drop() {
	p.Listener.OnDropped()
}

type gclExecutor[R any] struct {
	*policy.BaseExecutor[R]
	GclLimiter[R]
}

var _ policy.Executor[any] = &gclExecutor[any]{}

func (e *gclExecutor[R]) Apply(innerFn func(failsafe.Execution[R]) *common.PolicyResult[R]) func(failsafe.Execution[R]) *common.PolicyResult[R] {
	return func(exec failsafe.Execution[R]) *common.PolicyResult[R] {
		if permit, ok := e.TryAcquirePermit(); !ok {
			return &common.PolicyResult[R]{
				Error: adaptivelimiter.ErrExceeded,
				Done:  true,
			}
		} else {
			execInternal := exec.(policy.ExecutionInternal[R])
			result := innerFn(exec)
			result = e.PostExecute(execInternal, result)
			permit.Record()
			return result
		}
	}
}

type slogLogger struct {
	logger *slog.Logger
}

func (l slogLogger) Debugf(msg string, params ...interface{}) {
	l.logger.Debug(fmt.Sprintf(msg, params...))
}

func (l slogLogger) IsDebugEnabled() bool {
	return true
}

func (l slogLogger) String() string {
	return "slogLogger{}"
}
