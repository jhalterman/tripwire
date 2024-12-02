package server

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"go.uber.org/zap"

	"tripwire/pkg/metrics"
)

type Config struct {
	Static  WeightedServiceTimes `yaml:"static"`
	Stages  []*Stage             `yaml:"stages"`
	Threads uint                 `yaml:"threads"`
}

type Stage struct {
	Duration     time.Duration        `yaml:"duration"`
	ServiceTimes WeightedServiceTimes `yaml:"service_times"`
	WeightSum    uint
}

func (s *Stage) String() string {
	return fmt.Sprintf("Duration: %ds, ServiceTimes: %s", int(s.Duration.Seconds()), s.ServiceTimes.String())
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

func (w WeightedServiceTimes) Sum() uint {
	sum := uint(0)
	for _, ld := range w {
		sum += ld.Weight
	}
	return sum
}

// randomServiceTime selects a random service time based on the configuration.
func (s *Stage) randomServiceTime() (time.Duration, error) {
	return s.serviceTime(rand.Intn(int(s.WeightSum)))
}

// serviceTime selects a serviceTime based on the weight and configuration.
func (s *Stage) serviceTime(weight int) (time.Duration, error) {
	for _, wl := range s.ServiceTimes {
		weight -= int(wl.Weight)
		if weight < 0 {
			return wl.ServiceTime, nil
		}
	}
	return 0, fmt.Errorf("failed to compute service time")
}

type Server struct {
	listener net.Listener
	config   *Config
	metrics  *metrics.StrategyMetrics
	logger   *zap.SugaredLogger
	executor failsafe.Executor[*http.Response]

	mtx              sync.RWMutex
	currentStage     *Stage // Guarded by mtx
	availableThreads chan struct{}
}

func NewServer(config *Config, metrics *metrics.StrategyMetrics, executor failsafe.Executor[*http.Response], logger *zap.SugaredLogger) (*Server, net.Addr) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		logger.Fatalw("failed to listen", "err", err)
	}
	return &Server{
		listener:         listener,
		config:           config,
		metrics:          metrics,
		logger:           logger.With("runID", metrics.RunID),
		executor:         executor,
		availableThreads: make(chan struct{}, config.Threads),
	}, listener.Addr()
}

func (s *Server) Start(wg *sync.WaitGroup) {
	defer wg.Done()

	// Prepare workers
	s.metrics.ServerThreads.Set(float64(s.config.Threads))
	for i := 0; i < int(s.config.Threads); i++ {
		s.availableThreads <- struct{}{}
	}

	// Listen for requests
	server := &http.Server{
		Handler:     failsafehttp.NewHandlerWithExecutor(http.HandlerFunc(s.handleRequest), s.executor),
		ReadTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Fatalw("server error", "error", err)
		}
	}()

	if s.config.Static != nil {
		// Run static configuration
		s.logger.Infow("starting server with static configuration", "config", s.config.Static)
		s.UpdateStage(&Stage{
			ServiceTimes: s.config.Static,
			WeightSum:    s.config.Static.Sum(),
		})
		time.Sleep(24 * time.Hour)
	} else {
		// Run staged configuration
		for _, stage := range s.config.Stages {
			s.UpdateStage(stage)
			time.Sleep(stage.Duration)
		}

		s.logger.Infow("server stages finished")
	}

	_ = server.Shutdown(context.Background())
	s.metrics.ServerServiceTime.Set(0)
}

func (s *Server) UpdateStage(stage *Stage) {
	s.mtx.Lock()
	s.logger.Infow("starting server stage", "stage", stage)
	s.currentStage = stage
	s.mtx.Unlock()
}

func (s *Server) StaticConfig() WeightedServiceTimes {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.config.Static
}

func (s *Server) UpdateStaticConfig(staticConfig WeightedServiceTimes) {
	if staticConfig != nil {
		s.UpdateStage(&Stage{
			ServiceTimes: staticConfig,
			WeightSum:    staticConfig.Sum(),
		})
	}
}

func (s *Server) handleRequest(_ http.ResponseWriter, req *http.Request) {
	s.mtx.RLock()
	currentStage := s.currentStage
	s.mtx.RUnlock()

	serviceTime, err := currentStage.randomServiceTime()
	if err != nil {
		s.logger.Fatal(err)
	}

	s.recordServiceTime(serviceTime)
	s.metrics.ServerInflightRequests.Inc()

	// Simulate servicing a request, performing work in increments to simulate context switching between workers
	workIncrement := serviceTime / 100
	var workCompleted time.Duration
	for workCompleted < serviceTime && req.Context().Err() == nil {
		<-s.availableThreads
		time.Sleep(workIncrement)
		s.availableThreads <- struct{}{}
		workCompleted += workIncrement
	}

	s.metrics.ServerInflightRequests.Dec()
}

func (s *Server) recordServiceTime(serviceTime time.Duration) {
	s.metrics.ServerServiceTime.Set(serviceTime.Seconds())
}
