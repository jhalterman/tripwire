package util

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"go.uber.org/zap"
)

type Server struct {
	logger *zap.SugaredLogger
	mux    *http.ServeMux
	port   int
	server *http.Server
}

func NewServer(mux *http.ServeMux, port int, logger *zap.SugaredLogger) *Server {
	return &Server{
		logger: logger,
		mux:    mux,
		port:   port,
	}
}

func (s *Server) Start() {
	go func() {
		s.server = &http.Server{
			Addr:    ":" + strconv.Itoa(s.port),
			Handler: s.mux,
		}
		if err := s.server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			s.logger.Info(err)
		}
	}()
}

func (s *Server) Shutdown() {
	if err := s.server.Shutdown(context.Background()); err != nil {
		s.logger.Fatal(err)
	}
}
