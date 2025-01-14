package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"tripwire/pkg/metrics"
)

type Config struct {
	Threads  uint `yaml:"threads"`
	Duration time.Duration
}

type Server struct {
	listener net.Listener
	metrics  *metrics.StrategyMetrics
	logger   *zap.SugaredLogger
	executor failsafe.Executor[*http.Response]

	availableThreads chan struct{}
	mtx              sync.RWMutex
	config           *Config // Guarded by mtx
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

	time.Sleep(s.config.Duration)
	s.logger.Infow("server stopping")
	_ = server.Shutdown(context.Background())
	s.metrics.ServerServiceTime.Set(0)
}

type Request struct {
	ServiceTime time.Duration `yaml:"service_time"`
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := yaml.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Error decoding YAML: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.recordServiceTime(req.ServiceTime)
	s.metrics.ServerInflightRequests.Inc()

	// Simulate servicing a request, performing work in increments to simulate context switching between workers
	workIncrement := req.ServiceTime / 100
	var workCompleted time.Duration
	for workCompleted < req.ServiceTime && r.Context().Err() == nil {
		<-s.availableThreads
		time.Sleep(workIncrement)
		s.availableThreads <- struct{}{}
		workCompleted += workIncrement
	}

	s.metrics.ServerInflightRequests.Dec()
}

func (s *Server) UpdateConfig(config *Config) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	oldThreads := s.config.Threads
	newThreads := config.Threads
	s.config.Threads = config.Threads

	if newThreads > oldThreads {
		for i := 0; i < int(newThreads-oldThreads); i++ {
			s.availableThreads <- struct{}{}
		}
	} else if newThreads < oldThreads {
		for i := 0; i < int(oldThreads-newThreads); i++ {
			<-s.availableThreads
		}
	}

	s.metrics.ServerThreads.Set(float64(newThreads))
	s.logger.Infow("Updated thread count", "oldThreads", oldThreads, "newThreads", newThreads)
}

func (s *Server) recordServiceTime(serviceTime time.Duration) {
	s.metrics.ServerServiceTime.Set(serviceTime.Seconds())
}
