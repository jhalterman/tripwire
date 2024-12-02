package util

import (
	"context"
	"net/http"
	"strconv"

	"go.uber.org/zap"
)

type Server struct {
	logger *zap.SugaredLogger
	server *http.Server
}

func NewServer(mux *http.ServeMux, port int, logger *zap.SugaredLogger) *Server {
	s := &Server{
		logger: logger,
		server: &http.Server{
			Addr:    ":" + strconv.Itoa(port),
			Handler: mux,
		},
	}
	go func() {
		if err := s.server.ListenAndServe(); err != nil {
			s.logger.Info(err)
		}
	}()
	return s
}

func (s *Server) Shutdown() {
	if err := s.server.Shutdown(context.Background()); err != nil {
		s.logger.Fatal(err)
	}
}
